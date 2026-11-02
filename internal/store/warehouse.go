package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"halfpos/internal/domain"

	"github.com/jackc/pgx/v5"
)

func IsWarehousePIC(role string) bool {
	switch role {
	case "admin", "pic_gudang":
		return true
	default:
		return false
	}
}

func IsWarehouseViewer(role string) bool {
	return IsWarehousePIC(role) || IsStockSupervisor(role) || role == "finance"
}

type WarehouseOverview struct {
	TotalProducts          int   `json:"totalProducts"`
	TotalWarehouseStock    int64 `json:"totalWarehouseStock"`
	StockInToday           int64 `json:"stockInToday"`
	DamageTotal            int64 `json:"damageTotal"`
	PendingReviewRequests  int64 `json:"pendingReviewRequests"`
	AwaitingShipmentCount  int64 `json:"awaitingShipmentCount"`
	Recent                 []WarehouseMovementRow `json:"recent"`
}

type WarehouseMovementRow struct {
	ID                string     `json:"id"`
	TransactionNumber string     `json:"transactionNumber"`
	CreatedAt         time.Time  `json:"createdAt"`
	ProductID         string     `json:"productId"`
	ProductName       string     `json:"productName"`
	Activity          string     `json:"activity"`
	ActivityLabel     string     `json:"activityLabel"`
	Quantity          int32      `json:"quantity"`
	Unit              string     `json:"unit"`
	Note              *string    `json:"note,omitempty"`
	Reason            *string    `json:"reason,omitempty"`
	OfficerID         string     `json:"officerId"`
	OfficerName       string     `json:"officerName"`
	StockBefore       int32      `json:"stockBefore"`
	StockAfter        int32      `json:"stockAfter"`
	VoidedAt          *time.Time `json:"voidedAt,omitempty"`
	CanEdit           bool       `json:"canEdit"`
}

type WarehouseMovementPage struct {
	Data   []WarehouseMovementRow `json:"data"`
	Total  int64                  `json:"total"`
	Limit  int32                  `json:"limit"`
	Offset int32                  `json:"offset"`
}

func warehouseActivityLabel(kind, movementType string) (string, string) {
	switch kind {
	case "WAREHOUSE_IN":
		return "in", "Stok Masuk"
	case "STOK_RUSAK", "WAREHOUSE_DAMAGE":
		return "damage", "Stok Rusak"
	case "TRANSFER_OUT":
		return "transfer_out", "Kirim ke Toko"
	case "TRANSFER_IN":
		return "transfer_in", "Terima di Toko"
	default:
		if movementType == "in" {
			return "in", "Stok Masuk"
		}
		return "out", "Stok Keluar"
	}
}

func (s *Store) WarehouseOverview(ctx context.Context) (WarehouseOverview, error) {
	var out WarehouseOverview
	err := s.pool.QueryRow(ctx, `
		SELECT
		  COUNT(*)::int,
		  COALESCE(SUM(warehouse_stock), 0)::bigint
		FROM products
	`).Scan(&out.TotalProducts, &out.TotalWarehouseStock)
	if err != nil {
		return out, err
	}
	err = s.pool.QueryRow(ctx, `
		WITH bounds AS (
		  SELECT
		    (date_trunc('day', now() AT TIME ZONE 'Asia/Jakarta') AT TIME ZONE 'Asia/Jakarta') AS start_at,
		    ((date_trunc('day', now() AT TIME ZONE 'Asia/Jakarta') + interval '1 day') AT TIME ZONE 'Asia/Jakarta') AS end_at
		)
		SELECT
		  COALESCE(SUM(CASE
		    WHEN m.movement_kind = 'WAREHOUSE_IN' AND m.voided_at IS NULL
		      AND m.created_at >= b.start_at AND m.created_at < b.end_at
		    THEN m.quantity ELSE 0 END), 0)::bigint,
		  COALESCE(SUM(CASE
		    WHEN m.movement_kind IN ('STOK_RUSAK','WAREHOUSE_DAMAGE')
		      AND COALESCE(m.location_from,'warehouse') = 'warehouse'
		      AND m.voided_at IS NULL
		    THEN m.quantity ELSE 0 END), 0)::bigint
		FROM stock_movements m
		CROSS JOIN bounds b
	`).Scan(&out.StockInToday, &out.DamageTotal)
	if err != nil {
		return out, err
	}
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM stock_requests WHERE status = 'pending'
	`).Scan(&out.PendingReviewRequests)
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM stock_requests WHERE status IN ('awaiting_shipment','approved')
	`).Scan(&out.AwaitingShipmentCount)

	page, err := s.ListWarehouseMovements(ctx, WarehouseMovementFilter{Limit: 8, Offset: 0})
	if err != nil {
		return out, err
	}
	out.Recent = page.Data
	return out, nil
}

type WarehouseMovementFilter struct {
	Search    string
	Activity  string // in | damage | all
	OfficerID string
	Start     *time.Time
	EndBefore *time.Time
	Limit     int32
	Offset    int32
	IncludeVoided bool
}

func (s *Store) ListWarehouseMovements(ctx context.Context, f WarehouseMovementFilter) (WarehouseMovementPage, error) {
	if f.Limit <= 0 {
		f.Limit = 20
	}
	activity := strings.TrimSpace(f.Activity)
	rows, err := s.pool.Query(ctx, `
		SELECT
		  id, transaction_number, created_at, product_id, product_name,
		  COALESCE(movement_kind,''), movement_type, quantity,
		  COALESCE(unit_code,'pcs'), note, reason, officer_id, officer_name,
		  stock_before, stock_after, voided_at,
		  COUNT(*) OVER() AS total
		FROM stock_movements
		WHERE
		  (
		    movement_kind IN ('WAREHOUSE_IN','WAREHOUSE_DAMAGE','STOK_RUSAK','TRANSFER_OUT')
		    OR (COALESCE(location_to,'') = 'warehouse' AND movement_type = 'in')
		    OR (COALESCE(location_from,'') = 'warehouse' AND movement_type = 'out')
		  )
		  AND ($1::boolean OR voided_at IS NULL)
		  AND (
		    $2::text IS NULL OR $2 = 'all'
		    OR ($2 = 'in' AND (movement_kind = 'WAREHOUSE_IN' OR (movement_type = 'in' AND COALESCE(location_to,'') = 'warehouse')))
		    OR ($2 = 'damage' AND movement_kind IN ('STOK_RUSAK','WAREHOUSE_DAMAGE'))
		  )
		  AND ($3::text IS NULL OR product_name ILIKE $3 OR transaction_number ILIKE $3)
		  AND ($4::text IS NULL OR officer_id = $4)
		  AND ($5::timestamptz IS NULL OR created_at >= $5)
		  AND ($6::timestamptz IS NULL OR created_at < $6)
		ORDER BY created_at DESC, id DESC
		LIMIT $7 OFFSET $8
	`, f.IncludeVoided, optString(activity), likePattern(f.Search), optString(f.OfficerID),
		tsArg(f.Start), tsArg(f.EndBefore), f.Limit, f.Offset)
	if err != nil {
		return WarehouseMovementPage{}, err
	}
	defer rows.Close()
	out := make([]WarehouseMovementRow, 0)
	var total int64
	for rows.Next() {
		var row WarehouseMovementRow
		var kind, mtype, unit string
		var voided *time.Time
		if err := rows.Scan(
			&row.ID, &row.TransactionNumber, &row.CreatedAt, &row.ProductID, &row.ProductName,
			&kind, &mtype, &row.Quantity, &unit, &row.Note, &row.Reason,
			&row.OfficerID, &row.OfficerName, &row.StockBefore, &row.StockAfter, &voided, &total,
		); err != nil {
			return WarehouseMovementPage{}, err
		}
		row.Unit = unit
		row.VoidedAt = voided
		row.Activity, row.ActivityLabel = warehouseActivityLabel(kind, mtype)
		row.CanEdit = voided == nil && (row.Activity == "in" || row.Activity == "damage")
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return WarehouseMovementPage{}, err
	}
	return WarehouseMovementPage{Data: out, Total: total, Limit: f.Limit, Offset: f.Offset}, nil
}

type WarehouseStockInInput struct {
	ProductID string
	Quantity  int32
	Unit      string
	Source    string
	Note      string
	Actor     Actor
}

func (s *Store) CreateWarehouseStockIn(ctx context.Context, in WarehouseStockInInput) (domain.StockMovement, error) {
	if !IsWarehousePIC(in.Actor.Role) {
		return domain.StockMovement{}, Forbidden("Hanya PIC Gudang yang dapat menambah stok gudang")
	}
	if in.Quantity <= 0 {
		return domain.StockMovement{}, BadRequest("Jumlah stok masuk harus lebih dari 0")
	}
	var created domain.StockMovement
	err := s.tx(ctx, func(q *Queries) error {
		product, err := q.LockProduct(ctx, in.ProductID)
		if errors.Is(err, pgx.ErrNoRows) {
			return BadRequest("Produk tidak ditemukan")
		}
		if err != nil {
			return err
		}
		unit := in.Unit
		if unit == "" {
			unit = "pcs"
		}
		if _, err := q.GetUnit(ctx, unit); errors.Is(err, pgx.ErrNoRows) {
			return BadRequest("Satuan tidak dikenal")
		} else if err != nil {
			return err
		}
		before := product.WarehouseStock
		after := before + in.Quantity
		if err := q.UpdateProductWarehouseStock(ctx, after, product.ID); err != nil {
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
		kind := "WAREHOUSE_IN"
		locTo := "warehouse"
		noteParts := []string{}
		if strings.TrimSpace(in.Source) != "" {
			noteParts = append(noteParts, "Sumber: "+strings.TrimSpace(in.Source))
		}
		if strings.TrimSpace(in.Note) != "" {
			noteParts = append(noteParts, strings.TrimSpace(in.Note))
		}
		var note *string
		if len(noteParts) > 0 {
			joined := strings.Join(noteParts, " — ")
			note = &joined
		}
		movementID := newID("stk-")
		inserted, err := q.InsertStockMovementExtended(ctx, InsertStockMovementExtendedParams{
			ID: movementID, TransactionNumber: number, MovementType: "in",
			ProductID: product.ID, ProductName: product.Name, CategoryName: product.CategoryName,
			UnitCode: &unit, Quantity: in.Quantity, StockBefore: before, StockAfter: after,
			Note: note, OfficerID: in.Actor.ID, OfficerName: in.Actor.Name,
			LocationTo: &locTo, MovementKind: &kind,
		})
		if err != nil {
			return err
		}
		created = domain.StockMovement{
			ID: movementID, TransactionNumber: number, Type: "in",
			ProductID: product.ID, ProductName: product.Name, Category: product.CategoryName,
			Unit: &unit, QuantityIn: in.Quantity, StockBefore: before, StockAfter: after,
			Note: note, OfficerID: in.Actor.ID, OfficerName: in.Actor.Name,
			CreatedAt: tsTime(inserted.CreatedAt),
		}
		return nil
	})
	return created, err
}

type WarehouseDamageInput struct {
	ProductID string
	Quantity  int32
	Unit      string
	Reason    string
	Note      string
	Actor     Actor
}

func (s *Store) CreateWarehouseDamage(ctx context.Context, in WarehouseDamageInput) (domain.StockMovement, error) {
	if !IsWarehousePIC(in.Actor.Role) {
		return domain.StockMovement{}, Forbidden("Hanya PIC Gudang yang dapat mencatat stok rusak gudang")
	}
	if in.Quantity <= 0 {
		return domain.StockMovement{}, BadRequest("Jumlah stok rusak harus lebih dari 0")
	}
	if strings.TrimSpace(in.Reason) == "" {
		return domain.StockMovement{}, BadRequest("Alasan kerusakan wajib diisi")
	}
	var created domain.StockMovement
	err := s.tx(ctx, func(q *Queries) error {
		product, err := q.LockProduct(ctx, in.ProductID)
		if errors.Is(err, pgx.ErrNoRows) {
			return BadRequest("Produk tidak ditemukan")
		}
		if err != nil {
			return err
		}
		if int64(in.Quantity) > int64(product.WarehouseStock) {
			return BadRequest(fmt.Sprintf("Stok rusak melebihi stok gudang. Tersedia: %d", product.WarehouseStock))
		}
		unit := in.Unit
		if unit == "" {
			unit = "pcs"
		}
		if _, err := q.GetUnit(ctx, unit); errors.Is(err, pgx.ErrNoRows) {
			return BadRequest("Satuan tidak dikenal")
		} else if err != nil {
			return err
		}
		before := product.WarehouseStock
		after := before - in.Quantity
		if err := q.UpdateProductWarehouseStock(ctx, after, product.ID); err != nil {
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
		kind := "WAREHOUSE_DAMAGE"
		locFrom := "warehouse"
		reason := in.Reason
		note := blank(in.Note)
		movementID := newID("stk-")
		inserted, err := q.InsertStockMovementExtended(ctx, InsertStockMovementExtendedParams{
			ID: movementID, TransactionNumber: number, MovementType: "out",
			ProductID: product.ID, ProductName: product.Name, CategoryName: product.CategoryName,
			UnitCode: &unit, Quantity: in.Quantity, StockBefore: before, StockAfter: after,
			Reason: &reason, Note: note, OfficerID: in.Actor.ID, OfficerName: in.Actor.Name,
			LocationFrom: &locFrom, MovementKind: &kind,
		})
		if err != nil {
			return err
		}
		created = domain.StockMovement{
			ID: movementID, TransactionNumber: number, Type: "out",
			ProductID: product.ID, ProductName: product.Name, Category: product.CategoryName,
			Unit: &unit, QuantityOut: in.Quantity, StockBefore: before, StockAfter: after,
			Reason: &reason, Note: note, OfficerID: in.Actor.ID, OfficerName: in.Actor.Name,
			CreatedAt: tsTime(inserted.CreatedAt),
		}
		return nil
	})
	return created, err
}

func (s *Store) VoidWarehouseMovement(ctx context.Context, id, reason string, actor Actor) error {
	if !IsWarehousePIC(actor.Role) {
		return Forbidden("Hanya PIC Gudang yang dapat membatalkan pencatatan gudang")
	}
	if strings.TrimSpace(reason) == "" {
		return BadRequest("Alasan pembatalan wajib diisi")
	}
	return s.tx(ctx, func(q *Queries) error {
		var productID, kind, mtype string
		var qty, before, after int32
		var voided *time.Time
		err := q.db.QueryRow(ctx, `
			SELECT product_id, COALESCE(movement_kind,''), movement_type, quantity, stock_before, stock_after, voided_at
			FROM stock_movements WHERE id = $1 FOR UPDATE
		`, id).Scan(&productID, &kind, &mtype, &qty, &before, &after, &voided)
		if errors.Is(err, pgx.ErrNoRows) {
			return NotFound("Pencatatan tidak ditemukan")
		}
		if err != nil {
			return err
		}
		if voided != nil {
			return BadRequest("Pencatatan sudah dibatalkan")
		}
		activity, _ := warehouseActivityLabel(kind, mtype)
		if activity != "in" && activity != "damage" {
			return BadRequest("Hanya stok masuk atau stok rusak gudang yang dapat dibatalkan")
		}
		product, err := q.LockProduct(ctx, productID)
		if err != nil {
			return err
		}
		wh := product.WarehouseStock
		if activity == "in" {
			if int64(qty) > int64(wh) {
				return BadRequest(fmt.Sprintf("Tidak dapat membatalkan: stok gudang tidak cukup untuk membalik (%d)", wh))
			}
			wh -= qty
		} else {
			wh += qty
		}
		if err := q.UpdateProductWarehouseStock(ctx, wh, product.ID); err != nil {
			return err
		}
		_, err = q.db.Exec(ctx, `
			UPDATE stock_movements
			SET voided_at = now(), voided_by = $2, voided_by_name = $3, void_reason = $4
			WHERE id = $1
		`, id, actor.ID, actor.Name, reason)
		return err
	})
}

func (s *Store) EditWarehouseMovement(ctx context.Context, id string, newQty int32, note, reason string, actor Actor) (domain.StockMovement, error) {
	if !IsWarehousePIC(actor.Role) {
		return domain.StockMovement{}, Forbidden("Hanya PIC Gudang yang dapat mengedit pencatatan gudang")
	}
	if newQty <= 0 {
		return domain.StockMovement{}, BadRequest("Jumlah baru harus lebih dari 0")
	}
	var created domain.StockMovement
	err := s.tx(ctx, func(q *Queries) error {
		var productID, kind, mtype, number, productName, category string
		var qty int32
		var unit, oldNote, oldReason *string
		var voided *time.Time
		err := q.db.QueryRow(ctx, `
			SELECT product_id, product_name, category_name, COALESCE(movement_kind,''), movement_type,
			       quantity, unit_code, note, reason, voided_at, transaction_number
			FROM stock_movements WHERE id = $1 FOR UPDATE
		`, id).Scan(&productID, &productName, &category, &kind, &mtype, &qty, &unit, &oldNote, &oldReason, &voided, &number)
		if errors.Is(err, pgx.ErrNoRows) {
			return NotFound("Pencatatan tidak ditemukan")
		}
		if err != nil {
			return err
		}
		if voided != nil {
			return BadRequest("Pencatatan sudah dibatalkan")
		}
		activity, _ := warehouseActivityLabel(kind, mtype)
		if activity != "in" && activity != "damage" {
			return BadRequest("Hanya stok masuk atau stok rusak gudang yang dapat diedit")
		}
		product, err := q.LockProduct(ctx, productID)
		if err != nil {
			return err
		}
		wh := product.WarehouseStock
		// Reverse old
		if activity == "in" {
			if int64(qty) > int64(wh) {
				return BadRequest("Stok gudang tidak cukup untuk membalik pencatatan lama")
			}
			wh -= qty
		} else {
			wh += qty
		}
		// Apply new
		if activity == "in" {
			wh += newQty
		} else {
			if int64(newQty) > int64(wh) {
				return BadRequest(fmt.Sprintf("Jumlah stok rusak melebihi stok gudang. Tersedia: %d", wh))
			}
			wh -= newQty
		}
		if err := q.UpdateProductWarehouseStock(ctx, wh, product.ID); err != nil {
			return err
		}
		_, err = q.db.Exec(ctx, `
			UPDATE stock_movements
			SET voided_at = now(), voided_by = $2, voided_by_name = $3,
			    void_reason = $4
			WHERE id = $1
		`, id, actor.ID, actor.Name, "Diedit — diganti pencatatan koreksi")
		if err != nil {
			return err
		}

		prefixKind := "STI"
		movementType := "in"
		kindNew := "WAREHOUSE_IN"
		var locFrom, locTo *string
		locTo = strPtr("warehouse")
		r := oldReason
		if activity == "damage" {
			prefixKind = "STO"
			movementType = "out"
			kindNew = "WAREHOUSE_DAMAGE"
			locFrom = strPtr("warehouse")
			locTo = nil
			if strings.TrimSpace(reason) != "" {
				r = &reason
			}
			if r == nil || *r == "" {
				return BadRequest("Alasan kerusakan wajib diisi")
			}
		}
		n := oldNote
		if strings.TrimSpace(note) != "" {
			n = blank(note)
		}
		prefix, err := dailyPrefix(prefixKind, time.Now())
		if err != nil {
			return err
		}
		end, err := q.NextDocumentNumbers(ctx, NextDocumentNumbersParams{Prefix: prefix, Count: 1})
		if err != nil {
			return err
		}
		newNumber := documentNumbers(prefix, end, 1)[0]
		unitCode := "pcs"
		if unit != nil && *unit != "" {
			unitCode = *unit
		}
		// stock before/after for the new movement relative to current after reverse+apply
		var stockBefore, stockAfter int32
		if activity == "in" {
			stockAfter = wh
			stockBefore = wh - newQty
		} else {
			stockAfter = wh
			stockBefore = wh + newQty
		}
		movementID := newID("stk-")
		inserted, err := q.InsertStockMovementExtended(ctx, InsertStockMovementExtendedParams{
			ID: movementID, TransactionNumber: newNumber, MovementType: movementType,
			ProductID: productID, ProductName: productName, CategoryName: category,
			UnitCode: &unitCode, Quantity: newQty, StockBefore: stockBefore, StockAfter: stockAfter,
			Reason: r, Note: n, OfficerID: actor.ID, OfficerName: actor.Name,
			LocationFrom: locFrom, LocationTo: locTo, MovementKind: &kindNew, ReferenceID: &id,
		})
		if err != nil {
			return err
		}
		_, _ = q.db.Exec(ctx, `UPDATE stock_movements SET replaces_id = $2 WHERE id = $1`, movementID, id)
		created = domain.StockMovement{
			ID: movementID, TransactionNumber: newNumber, Type: movementType,
			ProductID: productID, ProductName: productName, Category: category,
			Unit: &unitCode, StockBefore: stockBefore, StockAfter: stockAfter,
			Reason: r, Note: n, OfficerID: actor.ID, OfficerName: actor.Name,
			CreatedAt: tsTime(inserted.CreatedAt),
		}
		if movementType == "in" {
			created.QuantityIn = newQty
		} else {
			created.QuantityOut = newQty
		}
		return nil
	})
	return created, err
}
