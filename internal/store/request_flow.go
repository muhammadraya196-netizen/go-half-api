package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type ReviewItemInput struct {
	ItemID           string
	ApprovedQuantity int32
}

type ReviewStockRequestInput struct {
	RequestID  string
	Action     string // approve | reject
	ReviewNote string
	Items      []ReviewItemInput
	Actor      Actor
}

func (s *Store) ReviewStockRequest(ctx context.Context, in ReviewStockRequestInput) (StockRequest, error) {
	if !IsWarehousePIC(in.Actor.Role) {
		return StockRequest{}, Forbidden("Hanya PIC Gudang yang dapat mereview request")
	}
	action := strings.ToLower(strings.TrimSpace(in.Action))
	if action != "approve" && action != "reject" {
		return StockRequest{}, BadRequest("Aksi review harus approve atau reject")
	}
	var result StockRequest
	err := s.tx(ctx, func(q *Queries) error {
		var req struct {
			ID, Number, Status, RequestedBy, RequestedName string
			Note                                           *string
		}
		err := q.db.QueryRow(ctx, `
			SELECT id, request_number, status, requested_by, requested_by_name, note
			FROM stock_requests WHERE id = $1 FOR UPDATE
		`, in.RequestID).Scan(&req.ID, &req.Number, &req.Status, &req.RequestedBy, &req.RequestedName, &req.Note)
		if errors.Is(err, pgx.ErrNoRows) {
			return NotFound("Request stok tidak ditemukan")
		}
		if err != nil {
			return err
		}
		if req.Status != "pending" {
			return BadRequest("Hanya request menunggu review yang dapat diproses")
		}

		itemRows, err := q.db.Query(ctx, `
			SELECT i.id, i.product_id, i.product_name, i.sku, i.quantity, i.unit_code, p.warehouse_stock
			FROM stock_request_items i
			JOIN products p ON p.id = i.product_id
			WHERE i.request_id = $1
			FOR UPDATE OF i
		`, req.ID)
		if err != nil {
			return err
		}
		defer itemRows.Close()
		type itemRow struct {
			ID, ProductID, Name, SKU, Unit string
			Qty, WH                        int32
		}
		items := make([]itemRow, 0)
		for itemRows.Next() {
			var it itemRow
			if err := itemRows.Scan(&it.ID, &it.ProductID, &it.Name, &it.SKU, &it.Qty, &it.Unit, &it.WH); err != nil {
				return err
			}
			items = append(items, it)
		}
		if err := itemRows.Err(); err != nil {
			return err
		}
		if len(items) == 0 {
			return BadRequest("Request tidak memiliki item")
		}

		next := "rejected"
		if action == "approve" {
			next = "awaiting_shipment"
			byID := map[string]int32{}
			for _, row := range in.Items {
				byID[row.ItemID] = row.ApprovedQuantity
			}
			for _, it := range items {
				approved, ok := byID[it.ID]
				if !ok {
					approved = it.Qty
				}
				if approved <= 0 {
					return BadRequest("Jumlah disetujui harus lebih dari 0")
				}
				if approved > it.Qty {
					return BadRequest(fmt.Sprintf("Jumlah disetujui tidak boleh melebihi permintaan (%s: %d)", it.Name, it.Qty))
				}
				if approved > it.WH {
					return BadRequest(fmt.Sprintf("Jumlah disetujui melebihi stok gudang %s (tersedia %d)", it.Name, it.WH))
				}
				if _, err := q.db.Exec(ctx, `
					UPDATE stock_request_items SET approved_quantity = $2 WHERE id = $1
				`, it.ID, approved); err != nil {
					return err
				}
			}
		}

		_, err = q.db.Exec(ctx, `
			UPDATE stock_requests
			SET status = $2, reviewed_by = $3, reviewed_by_name = $4,
			    review_note = NULLIF($5,''), reviewed_at = now(), updated_at = now()
			WHERE id = $1
		`, req.ID, next, in.Actor.ID, in.Actor.Name, in.ReviewNote)
		if err != nil {
			return err
		}
		result, err = s.getStockRequestTx(ctx, q, req.ID)
		return err
	})
	return result, err
}

func (s *Store) ShipStockRequest(ctx context.Context, requestID string, actor Actor) (StockRequest, error) {
	if !IsWarehousePIC(actor.Role) {
		return StockRequest{}, Forbidden("Hanya PIC Gudang yang dapat mengirim stok")
	}
	var result StockRequest
	err := s.tx(ctx, func(q *Queries) error {
		var req struct {
			ID, Number, Status string
		}
		err := q.db.QueryRow(ctx, `
			SELECT id, request_number, status FROM stock_requests WHERE id = $1 FOR UPDATE
		`, requestID).Scan(&req.ID, &req.Number, &req.Status)
		if errors.Is(err, pgx.ErrNoRows) {
			return NotFound("Request stok tidak ditemukan")
		}
		if err != nil {
			return err
		}
		if req.Status != "awaiting_shipment" && req.Status != "approved" {
			return BadRequest("Request harus menunggu pengiriman sebelum dikirim")
		}
		// Idempotency: already shipped?
		if req.Status == "shipped" || req.Status == "completed" || req.Status == "received" {
			return BadRequest("Request sudah dikirim")
		}

		type shipItem struct {
			ID, ProductID, Name, SKU, Unit string
			Approved                       int32
		}
		items := make([]shipItem, 0)
		itemRows2, err := q.db.Query(ctx, `
			SELECT id, product_id, product_name, sku, quantity,
			       COALESCE(approved_quantity, quantity) AS approved_quantity, unit_code
			FROM stock_request_items WHERE request_id = $1
		`, req.ID)
		if err != nil {
			return err
		}
		defer itemRows2.Close()
		for itemRows2.Next() {
			var it shipItem
			var requested int32
			if err := itemRows2.Scan(&it.ID, &it.ProductID, &it.Name, &it.SKU, &requested, &it.Approved, &it.Unit); err != nil {
				return err
			}
			items = append(items, it)
		}
		if err := itemRows2.Err(); err != nil {
			return err
		}
		if len(items) == 0 {
			return BadRequest("Request tidak memiliki item")
		}

		for _, it := range items {
			product, err := q.LockProduct(ctx, it.ProductID)
			if errors.Is(err, pgx.ErrNoRows) {
				return BadRequest("Produk tidak ditemukan: " + it.Name)
			}
			if err != nil {
				return err
			}
			if int64(it.Approved) > int64(product.WarehouseStock) {
				return BadRequest(fmt.Sprintf(
					"Stok gudang tidak mencukupi untuk %s. Tersedia: %d, dikirim: %d",
					product.Name, product.WarehouseStock, it.Approved,
				))
			}
			whBefore := product.WarehouseStock
			whAfter := whBefore - it.Approved
			if err := q.UpdateProductWarehouseStock(ctx, whAfter, product.ID); err != nil {
				return err
			}
			prefix, err := dailyPrefix("STO", time.Now())
			if err != nil {
				return err
			}
			end, err := q.NextDocumentNumbers(ctx, NextDocumentNumbersParams{Prefix: prefix, Count: 1})
			if err != nil {
				return err
			}
			number := documentNumbers(prefix, end, 1)[0]
			reason := "Transfer Gudang ke Toko"
			note := "Request " + req.Number + " — dikirim"
			ref := req.ID
			locWH, locStore := "warehouse", "store"
			kind := "TRANSFER_OUT"
			unit := it.Unit
			_, err = q.InsertStockMovementExtended(ctx, InsertStockMovementExtendedParams{
				ID: newID("stk-"), TransactionNumber: number, MovementType: "out",
				ProductID: product.ID, ProductName: product.Name, CategoryName: product.CategoryName,
				UnitCode: &unit, Quantity: it.Approved, StockBefore: whBefore, StockAfter: whAfter,
				Reason: &reason, Note: &note, OfficerID: actor.ID, OfficerName: actor.Name,
				LocationFrom: &locWH, LocationTo: &locStore, MovementKind: &kind, ReferenceID: &ref,
			})
			if err != nil {
				return err
			}
			if _, err := q.db.Exec(ctx, `
				UPDATE stock_request_items SET shipped_quantity = $2 WHERE id = $1
			`, it.ID, it.Approved); err != nil {
				return err
			}
		}

		_, err = q.db.Exec(ctx, `
			UPDATE stock_requests
			SET status = 'shipped', shipped_by = $2, shipped_by_name = $3,
			    shipped_at = now(), updated_at = now()
			WHERE id = $1 AND status IN ('awaiting_shipment','approved')
		`, req.ID, actor.ID, actor.Name)
		if err != nil {
			return err
		}
		result, err = s.getStockRequestTx(ctx, q, req.ID)
		return err
	})
	return result, err
}

type ReceiveItemInput struct {
	ItemID            string
	ReceivedQuantity  int32
}

func (s *Store) ReceiveStockRequest(ctx context.Context, requestID string, items []ReceiveItemInput, actor Actor) (StockRequest, error) {
	if !IsStockSupervisor(actor.Role) && !IsWarehousePIC(actor.Role) {
		return StockRequest{}, Forbidden("Hanya Supervisor Toko yang dapat mengonfirmasi penerimaan")
	}
	var result StockRequest
	err := s.tx(ctx, func(q *Queries) error {
		var req struct {
			ID, Number, Status string
		}
		err := q.db.QueryRow(ctx, `
			SELECT id, request_number, status FROM stock_requests WHERE id = $1 FOR UPDATE
		`, requestID).Scan(&req.ID, &req.Number, &req.Status)
		if errors.Is(err, pgx.ErrNoRows) {
			return NotFound("Request stok tidak ditemukan")
		}
		if err != nil {
			return err
		}
		if req.Status != "shipped" {
			return BadRequest("Hanya request yang sudah dikirim yang dapat diterima")
		}

		byID := map[string]int32{}
		for _, row := range items {
			byID[row.ItemID] = row.ReceivedQuantity
		}

		itemRows, err := q.db.Query(ctx, `
			SELECT id, product_id, product_name, sku, quantity,
			       COALESCE(shipped_quantity, COALESCE(approved_quantity, quantity)), unit_code
			FROM stock_request_items WHERE request_id = $1
		`, req.ID)
		if err != nil {
			return err
		}
		defer itemRows.Close()
		type recvItem struct {
			ID, ProductID, Name, SKU, Unit string
			Shipped                        int32
		}
		list := make([]recvItem, 0)
		for itemRows.Next() {
			var it recvItem
			var requested int32
			if err := itemRows.Scan(&it.ID, &it.ProductID, &it.Name, &it.SKU, &requested, &it.Shipped, &it.Unit); err != nil {
				return err
			}
			list = append(list, it)
		}
		if err := itemRows.Err(); err != nil {
			return err
		}

		for _, it := range list {
			received, ok := byID[it.ID]
			if !ok {
				received = it.Shipped
			}
			if received < 0 {
				return BadRequest("Jumlah diterima tidak valid")
			}
			if received > it.Shipped {
				return BadRequest(fmt.Sprintf("Jumlah diterima tidak boleh melebihi yang dikirim (%s: %d)", it.Name, it.Shipped))
			}
			product, err := q.LockProduct(ctx, it.ProductID)
			if err != nil {
				return err
			}
			if received > 0 {
				storeBefore := product.Stock
				storeAfter := storeBefore + received
				if err := q.UpdateProductStock(ctx, UpdateProductStockParams{Stock: storeAfter, ID: product.ID}); err != nil {
					return err
				}
				_, _ = q.db.Exec(ctx, `UPDATE products SET store_listed = TRUE WHERE id = $1`, product.ID)
				prefix, err := dailyPrefix("STI", time.Now())
				if err != nil {
					return err
				}
				end, err := q.NextDocumentNumbers(ctx, NextDocumentNumbersParams{Prefix: prefix, Count: 1})
				if err != nil {
					return err
				}
				number := documentNumbers(prefix, end, 1)[0]
				reason := "Transfer Gudang ke Toko"
				note := "Request " + req.Number + " — diterima"
				ref := req.ID
				locWH, locStore := "warehouse", "store"
				kind := "TRANSFER_IN"
				unit := it.Unit
				_, err = q.InsertStockMovementExtended(ctx, InsertStockMovementExtendedParams{
					ID: newID("stk-"), TransactionNumber: number, MovementType: "in",
					ProductID: product.ID, ProductName: product.Name, CategoryName: product.CategoryName,
					UnitCode: &unit, Quantity: received, StockBefore: storeBefore, StockAfter: storeAfter,
					Reason: &reason, Note: &note, OfficerID: actor.ID, OfficerName: actor.Name,
					LocationFrom: &locWH, LocationTo: &locStore, MovementKind: &kind, ReferenceID: &ref,
				})
				if err != nil {
					return err
				}
			}
			// Selisih: barang tidak sampai → kembalikan ke gudang
			diff := it.Shipped - received
			if diff > 0 {
				product2, err := q.LockProduct(ctx, it.ProductID)
				if err != nil {
					return err
				}
				whBefore := product2.WarehouseStock
				whAfter := whBefore + diff
				if err := q.UpdateProductWarehouseStock(ctx, whAfter, product2.ID); err != nil {
					return err
				}
				prefix, err := dailyPrefix("STI", time.Now())
				if err != nil {
					return err
				}
				end, err := q.NextDocumentNumbers(ctx, NextDocumentNumbersParams{Prefix: prefix, Count: 1})
				if err != nil {
					return err
				}
				number := documentNumbers(prefix, end, 1)[0]
				reason := "Transfer Gudang ke Toko"
				note := fmt.Sprintf("Request %s — selisih pengiriman dikembalikan ke gudang (%d)", req.Number, diff)
				ref := req.ID
				locTo := "warehouse"
				kind := "WAREHOUSE_IN"
				unit := it.Unit
				_, err = q.InsertStockMovementExtended(ctx, InsertStockMovementExtendedParams{
					ID: newID("stk-"), TransactionNumber: number, MovementType: "in",
					ProductID: product2.ID, ProductName: product2.Name, CategoryName: product2.CategoryName,
					UnitCode: &unit, Quantity: diff, StockBefore: whBefore, StockAfter: whAfter,
					Reason: &reason, Note: &note, OfficerID: actor.ID, OfficerName: actor.Name,
					LocationTo: &locTo, MovementKind: &kind, ReferenceID: &ref,
				})
				if err != nil {
					return err
				}
			}
			if _, err := q.db.Exec(ctx, `
				UPDATE stock_request_items SET received_quantity = $2 WHERE id = $1
			`, it.ID, received); err != nil {
				return err
			}
		}

		_, err = q.db.Exec(ctx, `
			UPDATE stock_requests
			SET status = 'completed', received_by = $2, received_by_name = $3,
			    received_at = now(), updated_at = now()
			WHERE id = $1 AND status = 'shipped'
		`, req.ID, actor.ID, actor.Name)
		if err != nil {
			return err
		}
		result, err = s.getStockRequestTx(ctx, q, req.ID)
		return err
	})
	return result, err
}

func (s *Store) getStockRequestTx(ctx context.Context, q *Queries, id string) (StockRequest, error) {
	var req StockRequest
	err := q.db.QueryRow(ctx, `
		SELECT id, request_number, status, requested_by, requested_by_name,
		       reviewed_by, reviewed_by_name, note, review_note,
		       reviewed_at, shipped_at, received_at, shipped_by_name, received_by_name,
		       created_at, updated_at
		FROM stock_requests WHERE id = $1
	`, id).Scan(
		&req.ID, &req.RequestNumber, &req.Status, &req.RequestedBy, &req.RequestedByName,
		&req.ReviewedBy, &req.ReviewedByName, &req.Note, &req.ReviewNote,
		&req.ReviewedAt, &req.ShippedAt, &req.ReceivedAt, &req.ShippedByName, &req.ReceivedByName,
		&req.CreatedAt, &req.UpdatedAt,
	)
	if err != nil {
		return req, err
	}
	req.StatusLabel = requestLabel(req.Status)
	rows, err := q.db.Query(ctx, `
		SELECT i.id, i.product_id, i.product_name, i.sku, i.quantity,
		       i.approved_quantity, i.shipped_quantity, i.received_quantity, i.unit_code,
		       p.warehouse_stock
		FROM stock_request_items i
		JOIN products p ON p.id = i.product_id
		WHERE i.request_id = $1
		ORDER BY i.product_name
	`, id)
	if err != nil {
		return req, err
	}
	defer rows.Close()
	req.Items = []StockRequestItem{}
	for rows.Next() {
		var it StockRequestItem
		var wh int32
		if err := rows.Scan(
			&it.ID, &it.ProductID, &it.ProductName, &it.SKU, &it.Quantity,
			&it.ApprovedQuantity, &it.ShippedQuantity, &it.ReceivedQuantity, &it.UnitCode, &wh,
		); err != nil {
			return req, err
		}
		it.WarehouseStock = &wh
		req.Items = append(req.Items, it)
	}
	return req, rows.Err()
}
