package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"halfpos/internal/domain"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"
)

func (s *Store) ListStock(ctx context.Context, filter domain.StockFilter) (domain.StockPage, error) {
	limit := filter.Limit
	params := ListStockMovementsParams{
		ProductID:       optString(filter.ProductID),
		MovementType:    optString(filter.Type),
		Supplier:        optString(filter.Supplier),
		StartAt:         tsArg(filter.Start),
		EndBefore:       tsArg(filter.EndBefore),
		Search:          likePattern(filter.Search),
		CursorCreatedAt: tsArg(filter.CursorCreatedAt),
		CursorID:        blank(filter.CursorID),
		RowOffset:       filter.Offset,
		RowLimit:        &limit,
	}
	batch := s.pool.SendBatch(ctx, stockBatch(params))
	defer batch.Close()
	rows, err := batch.Query()
	if err != nil {
		return domain.StockPage{}, err
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[ListStockMovementsRow])
	if err != nil {
		return domain.StockPage{}, err
	}
	var total int64
	if err := batch.QueryRow().Scan(&total); err != nil {
		return domain.StockPage{}, err
	}
	out := make([]domain.StockMovement, 0, len(items))
	for _, row := range items {
		out = append(out, mapMovement(row))
	}
	return domain.StockPage{Data: out, Total: total, Limit: limit, Offset: filter.Offset}, nil
}

func (s *Store) CreateStockMovement(ctx context.Context, in domain.StockInput) (domain.StockMovement, error) {
	if in.Quantity <= 0 {
		return domain.StockMovement{}, BadRequest("Jumlah harus bilangan bulat lebih dari 0")
	}
	var created domain.StockMovement
	err := s.tx(ctx, func(q *Queries) error {
		if _, err := q.UserExists(ctx, in.OfficerID); errors.Is(err, pgx.ErrNoRows) {
			return BadRequest("Petugas tidak ditemukan")
		} else if err != nil {
			return err
		}
		product, err := q.LockProduct(ctx, in.ProductID)
		if errors.Is(err, pgx.ErrNoRows) {
			return BadRequest("Produk tidak ditemukan")
		}
		if err != nil {
			return err
		}

		var supplierID, supplierName *string
		if in.Type == "in" {
			if in.Supplier == "" {
				return BadRequest("Supplier wajib dipilih")
			}
			if in.PurchasePrice != nil && *in.PurchasePrice < 0 {
				return BadRequest("Harga beli tidak boleh negatif")
			}
			supplier, err := q.GetSupplierByName(ctx, in.Supplier)
			if errors.Is(err, pgx.ErrNoRows) {
				return BadRequest("Supplier tidak ditemukan")
			}
			if err != nil {
				return err
			}
			supplierID = &supplier.ID
			supplierName = &supplier.Name
		} else if int64(in.Quantity) > int64(product.Stock) {
			return BadRequest(fmt.Sprintf("Stok toko tidak mencukupi. Stok tersedia: %d", product.Stock))
		}
		if in.Type == "out" && in.Reason == "" {
			return BadRequest("Alasan stok keluar wajib dipilih")
		}

		var unitCode *string
		if in.Unit != "" {
			code, err := q.GetUnit(ctx, in.Unit)
			if errors.Is(err, pgx.ErrNoRows) {
				return BadRequest("Satuan tidak dikenal")
			}
			if err != nil {
				return err
			}
			unitCode = &code
		}

		// Stok masuk supplier → Stok Gudang. Stok keluar (rusak/sampel/dll) → Stok Toko.
		var before, after int32
		var locFrom, locTo, kind *string
		prefixKind := "STI"
		if in.Type == "in" {
			before = product.WarehouseStock
			after = before + in.Quantity
			locTo = strPtr("warehouse")
			kind = strPtr("WAREHOUSE_IN")
			if err := q.UpdateProductWarehouseStock(ctx, after, product.ID); err != nil {
				return err
			}
		} else {
			before = product.Stock
			after = before - in.Quantity
			prefixKind = "STO"
			locFrom = strPtr("store")
			kind = strPtr(damageMovementKind(in.Reason))
			if err := q.UpdateProductStock(ctx, UpdateProductStockParams{Stock: after, ID: product.ID}); err != nil {
				return err
			}
		}
		prefix, err := dailyPrefix(prefixKind, time.Now())
		if err != nil {
			return err
		}
		end, err := q.NextDocumentNumbers(ctx, NextDocumentNumbersParams{Prefix: prefix, Count: 1})
		if err != nil {
			return err
		}
		number := documentNumbers(prefix, end, 1)[0]
		var reason *string
		if in.Type == "out" {
			reason = &in.Reason
		}
		movementID := newID("stk-")
		inserted, err := q.InsertStockMovementExtended(ctx, InsertStockMovementExtendedParams{
			ID: movementID, TransactionNumber: number, MovementType: in.Type,
			ProductID: product.ID, ProductName: product.Name, CategoryName: product.CategoryName,
			SupplierID: supplierID, SupplierName: supplierName, UnitCode: unitCode,
			Quantity: in.Quantity, StockBefore: before, StockAfter: after,
			PurchasePrice: in.PurchasePrice, Reason: reason, Note: blank(in.Note),
			OfficerID: in.OfficerID, OfficerName: in.OfficerName,
			LocationFrom: locFrom, LocationTo: locTo, MovementKind: kind,
		})
		if err != nil {
			if isForeignKey(err) {
				return BadRequest("Petugas tidak ditemukan")
			}
			return err
		}
		created = domain.StockMovement{
			ID: movementID, TransactionNumber: number, Type: in.Type,
			ProductID: product.ID, ProductName: product.Name, Category: product.CategoryName,
			Supplier: supplierName, Unit: unitCode,
			StockBefore: before, StockAfter: after,
			PurchasePrice: in.PurchasePrice, TotalPrice: inserted.TotalPrice,
			Reason: reason, Note: blank(in.Note),
			OfficerID: in.OfficerID, OfficerName: in.OfficerName,
			CreatedAt: tsTime(inserted.CreatedAt),
		}
		if in.Type == "in" {
			created.QuantityIn = in.Quantity
		} else {
			created.QuantityOut = in.Quantity
		}
		return nil
	})
	return created, err
}

func strPtr(v string) *string { return &v }

func damageMovementKind(reason string) string {
	switch reason {
	case "Produk Rusak", "Kadaluarsa", "Produk Kedaluwarsa", "Kemasan Rusak",
		"Produk Tidak Layak Jual", "Kerusakan Lainnya":
		return "STOK_RUSAK"
	case "Penjualan":
		return "SALE"
	default:
		return "STORE_OUT"
	}
}

func (s *Store) StockOverview(ctx context.Context) (domain.StockOverview, error) {
	var products []ListProductsForOverviewRow
	var movements []MovementAggregatesRow
	group, groupCtx := errgroup.WithContext(ctx)
	q := New(s.pool)
	group.Go(func() error {
		rows, err := q.ListProductsForOverview(groupCtx)
		products = rows
		return err
	})
	group.Go(func() error {
		rows, err := q.MovementAggregates(groupCtx)
		movements = rows
		return err
	})
	if err := group.Wait(); err != nil {
		return domain.StockOverview{}, err
	}

	byProduct := make(map[string]MovementAggregatesRow, len(movements))
	var todayIn, todayOut int64
	for _, row := range movements {
		byProduct[row.ProductID] = row
		todayIn += row.TodayIn
		todayOut += row.TodayOut
	}
	info := make([]domain.ProductStockInfo, 0, len(products))
	var totalStock int64
	var low, out int
	for _, product := range products {
		agg := byProduct[product.ID]
		status := "safe"
		if product.Stock <= 0 {
			status = "out"
			out++
		} else if product.Stock <= product.MinStock {
			status = "low"
			low++
		}
		totalStock += int64(product.Stock)
		info = append(info, domain.ProductStockInfo{
			ProductID: product.ID, Name: product.Name, Category: product.CategoryName,
			CurrentStock: product.Stock, TotalIn: agg.TotalIn, TotalOut: agg.TotalOut,
			MinStock: product.MinStock, Status: status,
		})
	}
	return domain.StockOverview{
		Summary: domain.StockSummary{
			TotalProducts: len(products), TotalStock: totalStock,
			TotalStockInToday: todayIn, TotalStockOutToday: todayOut,
			LowStockCount: low, OutOfStockCount: out,
		},
		Products: info,
	}, nil
}

func (s *Store) Analytics(ctx context.Context) (domain.Analytics, error) {
	row, err := New(s.pool).DashboardStats(ctx)
	if err != nil {
		return domain.Analytics{}, err
	}
	return domain.Analytics{
		TotalRevenue: row.TotalRevenue, TotalOrders: row.TotalOrders,
		TotalProducts: row.TotalProducts, CompletedOrders: row.CompletedOrders,
		TodaySales: row.TodaySales, TodayOrderCount: row.TodayOrderCount,
		TodayItemsSold: row.TodayItemsSold,
	}, nil
}

func mapMovement(row ListStockMovementsRow) domain.StockMovement {
	movement := domain.StockMovement{
		ID: row.ID, TransactionNumber: row.TransactionNumber, Type: row.MovementType,
		ProductID: row.ProductID, ProductName: row.ProductName, Category: row.CategoryName,
		Supplier: row.SupplierName, Unit: row.UnitCode,
		StockBefore: row.StockBefore, StockAfter: row.StockAfter,
		PurchasePrice: row.PurchasePrice, TotalPrice: row.TotalPrice,
		Reason: row.Reason, Note: row.Note,
		OfficerID: row.OfficerID, OfficerName: row.OfficerName,
		CreatedAt: tsTime(row.CreatedAt),
	}
	if row.MovementType == "in" {
		movement.QuantityIn = row.Quantity
	} else {
		movement.QuantityOut = row.Quantity
	}
	return movement
}
