package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"halfpos/internal/domain"

	"github.com/jackc/pgx/v5"
)

func (s *Store) ListOrders(ctx context.Context, filter domain.OrderFilter) ([]domain.Order, int64, error) {
	sortBy := filter.SortBy
	if sortBy != "total" {
		sortBy = "date"
	}
	sortDir := filter.SortDir
	if sortDir != "asc" {
		sortDir = "desc"
	}
	var cursor *time.Time
	cursorID := ""
	if sortBy == "date" {
		cursor = filter.CursorCreatedAt
		cursorID = filter.CursorID
	}
	params := ListOrdersParams{
		Status:          optString(filter.Status),
		StartAt:         tsArg(filter.Start),
		EndBefore:       tsArg(filter.EndBefore),
		Search:          likePattern(filter.Search),
		CursorCreatedAt: tsArg(cursor),
		SortBy:          sortBy,
		SortDir:         sortDir,
		CursorID:        blank(cursorID),
		RowOffset:       filter.Offset,
		RowLimit:        filter.Limit,
	}
	batch := s.pool.SendBatch(ctx, orderBatch(params))
	defer batch.Close()
	rows, err := batch.Query()
	if err != nil {
		return nil, 0, err
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[ListOrdersRow])
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := batch.QueryRow().Scan(&total); err != nil {
		return nil, 0, err
	}
	out := make([]domain.Order, 0, len(items))
	for _, row := range items {
		order, err := mapOrderRow(row)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, order)
	}
	return out, total, nil
}

func (s *Store) CreateOrder(ctx context.Context, in domain.CreateOrderInput) (domain.Order, error) {
	if len(in.Items) == 0 {
		return domain.Order{}, BadRequest("Keranjang kosong")
	}
	if in.Tax < 0 || in.Discount < 0 || in.Subtotal < 0 || in.Total < 0 {
		return domain.Order{}, BadRequest("Nominal tidak boleh negatif")
	}
	var lineSum int64
	for _, item := range in.Items {
		if item.Quantity <= 0 {
			return domain.Order{}, BadRequest("Jumlah harus lebih dari 0")
		}
		if item.Price < 0 {
			return domain.Order{}, BadRequest("Harga tidak boleh negatif")
		}
		if item.ProductName == "" {
			return domain.Order{}, BadRequest("Nama produk wajib diisi")
		}
		line := int64(item.Quantity) * item.Price
		if item.Price != 0 && line/item.Price != int64(item.Quantity) {
			return domain.Order{}, BadRequest("Subtotal tidak sesuai")
		}
		lineSum += line
	}
	if lineSum != in.Subtotal {
		return domain.Order{}, BadRequest("Subtotal tidak sesuai")
	}
	expected := in.Subtotal + in.Tax - in.Discount
	if expected < 0 {
		expected = 0
	}
	if expected != in.Total {
		return domain.Order{}, BadRequest("Total tidak sesuai")
	}

	var created domain.Order
	err := s.tx(ctx, func(q *Queries) error {
		if _, err := q.UserExists(ctx, in.CashierID); errors.Is(err, pgx.ErrNoRows) {
			return BadRequest("Kasir tidak ditemukan")
		} else if err != nil {
			return err
		}

		itemIDs := make([]string, len(in.Items))
		for i, item := range in.Items {
			itemIDs[i] = item.ProductID
		}

		// 1. Kunci produk yang stoknya bisa berkurang, termasuk komponen paket.
		lockedRows, err := q.LockProductsForCart(ctx, itemIDs)
		if err != nil {
			return err
		}
		locked := make(map[string]LockProductsForCartRow, len(lockedRows))
		for _, row := range lockedRows {
			locked[row.ID] = row
		}

		// Klasifikasi paket sekali, sebelum saldo ditolak dan sebelum baris ditulis.
		bundles, err := q.ListPromoBundles(ctx, itemIDs)
		if err != nil {
			return err
		}
		bundleSet := make(map[string]string, len(bundles))
		bundleIDs := make([]string, 0, len(bundles))
		for _, bundle := range bundles {
			bundleSet[bundle.ID] = bundle.Name
			bundleIDs = append(bundleIDs, bundle.ID)
		}
		recipes, err := q.ListBundleRecipes(ctx, bundleIDs)
		if err != nil {
			return err
		}
		recipeByBundle := map[string][]PromoBundleComponent{}
		for _, recipe := range recipes {
			recipeByBundle[recipe.BundleID] = append(recipeByBundle[recipe.BundleID], recipe)
		}

		demand := map[string]int64{}
		for _, item := range in.Items {
			if _, isBundle := bundleSet[item.ProductID]; isBundle {
				parts := recipeByBundle[item.ProductID]
				if len(parts) == 0 {
					return BadRequest("Paket tidak memiliki komponen")
				}
				for _, part := range parts {
					next := demand[part.ProductID] + int64(part.Quantity)*int64(item.Quantity)
					if next > math.MaxInt32 {
						return BadRequest("Jumlah terlalu besar")
					}
					demand[part.ProductID] = next
				}
				continue
			}
			if _, ok := locked[item.ProductID]; !ok {
				return BadRequest("Produk tidak ditemukan")
			}
			next := demand[item.ProductID] + int64(item.Quantity)
			if next > math.MaxInt32 {
				return BadRequest("Jumlah terlalu besar")
			}
			demand[item.ProductID] = next
		}

		// 2. Tolak bila stok kurang. Belum ada baris pesanan.
		for productID, qty := range demand {
			product, ok := locked[productID]
			if !ok || int64(product.Stock) < qty {
				name := productID
				if ok {
					name = product.Name
				}
				return BadRequest(fmt.Sprintf("Stok tidak cukup untuk %s", name))
			}
		}

		now := time.Now()
		invPrefix, err := dailyPrefix("INV", now)
		if err != nil {
			return err
		}
		// 3. Satu nomor INV.
		invEnd, err := q.NextDocumentNumbers(ctx, NextDocumentNumbersParams{Prefix: invPrefix, Count: 1})
		if err != nil {
			return err
		}
		orderNumber := documentNumbers(invPrefix, invEnd, 1)[0]
		orderID := newID("ord-")

		// 4. INSERT orders.
		createdAt, err := q.InsertOrder(ctx, InsertOrderParams{
			ID:            orderID,
			OrderNumber:   orderNumber,
			Subtotal:      in.Subtotal,
			Tax:           in.Tax,
			Discount:      in.Discount,
			Total:         in.Total,
			PaymentMethod: in.PaymentMethod,
			Status:        "completed",
			CashierID:     in.CashierID,
			CashierName:   in.CashierName,
			CustomerName:  blank(in.CustomerName),
			Notes:         blank(in.Notes),
			CashReceived:  in.CashReceived,
			ChangeAmount:  in.ChangeAmount,
		})
		if err != nil {
			if isForeignKey(err) {
				return BadRequest("Kasir tidak ditemukan")
			}
			return err
		}

		savedItems := make([]domain.OrderItem, 0, len(in.Items))
		itemRows := make([]orderItemRow, 0, len(in.Items))
		componentRows := make([]componentRow, 0)
		for index, item := range in.Items {
			itemID := fmt.Sprintf("%s-%d", orderID, index+1)
			line := int64(item.Quantity) * item.Price
			row := orderItemRow{
				ID:          itemID,
				ProductName: item.ProductName,
				Quantity:    item.Quantity,
				UnitPrice:   item.Price,
				Subtotal:    line,
				SortOrder:   int32(index),
			}
			saved := domain.OrderItem{
				ProductID: item.ProductID, ProductName: item.ProductName,
				Quantity: item.Quantity, Price: item.Price, Subtotal: line,
			}
			if _, isBundle := bundleSet[item.ProductID]; isBundle {
				row.BundleID = &item.ProductID
				parts := make([]domain.BundleComponent, 0, len(recipeByBundle[item.ProductID]))
				for _, part := range recipeByBundle[item.ProductID] {
					product := locked[part.ProductID]
					parts = append(parts, domain.BundleComponent{
						ProductID: part.ProductID, ProductName: product.Name, Quantity: part.Quantity,
					})
					componentRows = append(componentRows, componentRow{
						OrderItemID: itemID, ProductID: part.ProductID,
						ProductName: product.Name, Quantity: part.Quantity,
					})
				}
				saved.BundleComponents = parts
			} else {
				row.ProductID = &item.ProductID
			}
			itemRows = append(itemRows, row)
			savedItems = append(savedItems, saved)
		}

		itemJSON, err := json.Marshal(itemRows)
		if err != nil {
			return err
		}
		if err := q.InsertOrderItems(ctx, InsertOrderItemsParams{OrderID: orderID, Rows: itemJSON}); err != nil {
			return err
		}
		componentJSON, err := json.Marshal(componentRows)
		if err != nil {
			return err
		}
		if err := q.InsertOrderItemComponents(ctx, componentJSON); err != nil {
			return err
		}

		payments := in.Payments
		if len(payments) == 0 && in.Total > 0 {
			payments = []domain.PaymentInput{{
				Method: in.PaymentMethod, Amount: in.Total, CashReceived: in.CashReceived,
			}}
		}
		paymentRows := make([]paymentRow, 0, len(payments))
		savedPayments := make([]domain.OrderPayment, 0, len(payments))
		for index, payment := range payments {
			if payment.Amount <= 0 {
				continue
			}
			var cash *int64
			saved := domain.OrderPayment{Method: payment.Method, Amount: payment.Amount}
			if payment.Method == "cash" {
				received := payment.Amount
				if payment.CashReceived != nil {
					received = *payment.CashReceived
				}
				cash = &received
				saved.CashReceived = cash
			}
			paymentRows = append(paymentRows, paymentRow{
				ID: fmt.Sprintf("%s-pay-%d", orderID, index+1), SortOrder: int32(index),
				Method: payment.Method, Amount: payment.Amount, CashReceived: cash,
			})
			savedPayments = append(savedPayments, saved)
		}
		paymentJSON, err := json.Marshal(paymentRows)
		if err != nil {
			return err
		}
		if err := q.InsertOrderPayments(ctx, InsertOrderPaymentsParams{OrderID: orderID, Rows: paymentJSON}); err != nil {
			return err
		}

		productIDs := make([]string, 0, len(demand))
		for id := range demand {
			productIDs = append(productIDs, id)
		}
		sort.Strings(productIDs)
		stockRows := make([]stockUpdateRow, 0, len(productIDs))
		for _, id := range productIDs {
			product := locked[id]
			after := product.Stock - int32(demand[id])
			stockRows = append(stockRows, stockUpdateRow{ID: id, Stock: after})
			product.Stock = after
			locked[id] = product
		}
		stockJSON, err := json.Marshal(stockRows)
		if err != nil {
			return err
		}
		// 7. Satu UPDATE saldo untuk semua produk yang berubah.
		if err := q.UpdateProductStocks(ctx, stockJSON); err != nil {
			return err
		}

		stoPrefix, err := dailyPrefix("STO", now)
		if err != nil {
			return err
		}
		// 8. N nomor STO sekaligus, lalu satu INSERT jurnal keluar.
		stoEnd, err := q.NextDocumentNumbers(ctx, NextDocumentNumbersParams{Prefix: stoPrefix, Count: int32(len(productIDs))})
		if err != nil {
			return err
		}
		numbers := documentNumbers(stoPrefix, stoEnd, len(productIDs))
		moveRows := make([]saleMovementRow, 0, len(productIDs))
		for index, id := range productIDs {
			product := locked[id]
			qty := int32(demand[id])
			before := product.Stock + qty
			moveRows = append(moveRows, saleMovementRow{
				ID:                fmt.Sprintf("stk-%s-%s", orderID, id),
				TransactionNumber: numbers[index],
				ProductID:         id,
				ProductName:       product.Name,
				CategoryName:      product.CategoryName,
				Quantity:          qty,
				StockBefore:       before,
				StockAfter:        product.Stock,
				Note:              "Penjualan " + orderNumber,
				OfficerID:         in.CashierID,
				OfficerName:       in.CashierName,
				OrderID:           orderID,
			})
		}
		moveJSON, err := json.Marshal(moveRows)
		if err != nil {
			return err
		}
		if err := q.InsertSaleStockMovements(ctx, moveJSON); err != nil {
			return err
		}

		created = domain.Order{
			ID: orderID, OrderNumber: orderNumber, Items: savedItems,
			Subtotal: in.Subtotal, Tax: in.Tax, Discount: in.Discount, Total: in.Total,
			PaymentMethod: in.PaymentMethod, Payments: savedPayments, Status: "completed",
			CashierID: in.CashierID, CashierName: in.CashierName,
			CustomerName: blank(in.CustomerName), Notes: blank(in.Notes),
			CashReceived: in.CashReceived, ChangeAmount: in.ChangeAmount,
			CreatedAt: tsTime(createdAt),
		}
		return nil
	})
	return created, err
}

type orderItemRow struct {
	ID          string  `json:"id"`
	ProductID   *string `json:"product_id"`
	BundleID    *string `json:"bundle_id"`
	ProductName string  `json:"product_name"`
	Quantity    int32   `json:"quantity"`
	UnitPrice   int64   `json:"unit_price"`
	Subtotal    int64   `json:"subtotal"`
	SortOrder   int32   `json:"sort_order"`
}

type componentRow struct {
	OrderItemID string `json:"order_item_id"`
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	Quantity    int32  `json:"quantity"`
}

type paymentRow struct {
	ID           string `json:"id"`
	SortOrder    int32  `json:"sort_order"`
	Method       string `json:"method"`
	Amount       int64  `json:"amount"`
	CashReceived *int64 `json:"cash_received"`
}

type stockUpdateRow struct {
	ID    string `json:"id"`
	Stock int32  `json:"stock"`
}

type saleMovementRow struct {
	ID                string `json:"id"`
	TransactionNumber string `json:"transaction_number"`
	ProductID         string `json:"product_id"`
	ProductName       string `json:"product_name"`
	CategoryName      string `json:"category_name"`
	Quantity          int32  `json:"quantity"`
	StockBefore       int32  `json:"stock_before"`
	StockAfter        int32  `json:"stock_after"`
	Note              string `json:"note"`
	OfficerID         string `json:"officer_id"`
	OfficerName       string `json:"officer_name"`
	OrderID           string `json:"order_id"`
}

func mapOrderRow(row ListOrdersRow) (domain.Order, error) {
	var items []struct {
		ProductID        string `json:"productId"`
		ProductName      string `json:"productName"`
		Quantity         int32  `json:"quantity"`
		Price            int64  `json:"price"`
		Subtotal         int64  `json:"subtotal"`
		BundleComponents []struct {
			ProductID   string `json:"productId"`
			ProductName string `json:"productName"`
			Quantity    int32  `json:"quantity"`
		} `json:"bundleComponents"`
	}
	if err := json.Unmarshal([]byte(row.Items), &items); err != nil {
		return domain.Order{}, err
	}
	var payments []struct {
		Method       string `json:"method"`
		Amount       int64  `json:"amount"`
		CashReceived *int64 `json:"cashReceived"`
	}
	if err := json.Unmarshal([]byte(row.Payments), &payments); err != nil {
		return domain.Order{}, err
	}
	order := domain.Order{
		ID: row.ID, OrderNumber: row.OrderNumber, Subtotal: row.Subtotal, Tax: row.Tax,
		Discount: row.Discount, Total: row.Total, PaymentMethod: row.PaymentMethod,
		Status: row.Status, CashierID: row.CashierID, CashierName: row.CashierName,
		CustomerName: row.CustomerName, Notes: row.Notes, CashReceived: row.CashReceived,
		ChangeAmount: row.ChangeAmount, CreatedAt: tsTime(row.CreatedAt),
	}
	order.Items = make([]domain.OrderItem, 0, len(items))
	for _, item := range items {
		mapped := domain.OrderItem{
			ProductID: item.ProductID, ProductName: item.ProductName,
			Quantity: item.Quantity, Price: item.Price, Subtotal: item.Subtotal,
		}
		if len(item.BundleComponents) > 0 {
			mapped.BundleComponents = make([]domain.BundleComponent, 0, len(item.BundleComponents))
			for _, part := range item.BundleComponents {
				mapped.BundleComponents = append(mapped.BundleComponents, domain.BundleComponent{
					ProductID: part.ProductID, ProductName: part.ProductName, Quantity: part.Quantity,
				})
			}
		}
		order.Items = append(order.Items, mapped)
	}
	if len(payments) > 0 {
		order.Payments = make([]domain.OrderPayment, 0, len(payments))
		for _, payment := range payments {
			order.Payments = append(order.Payments, domain.OrderPayment{
				Method: payment.Method, Amount: payment.Amount, CashReceived: payment.CashReceived,
			})
		}
	}
	return order, nil
}
