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

// Actor adalah pengguna yang memanggil endpoint inventory (dari header).
type Actor struct {
	ID         string
	Role       string
	Name       string
	MenuAccess []string
}

func IsStockSupervisor(role string) bool {
	switch role {
	case "admin", "manager", "supervisor":
		return true
	default:
		return false
	}
}

func IsStockViewer(role string) bool {
	switch role {
	case "admin", "manager", "supervisor", "cashier", "finance", "pic_gudang":
		return true
	default:
		return false
	}
}

type StoreStockRow struct {
	ProductID      string    `json:"productId"`
	Name           string    `json:"name"`
	Category       string    `json:"category"`
	SKU            string    `json:"sku"`
	StoreStock     int32     `json:"storeStock"`
	WarehouseStock int32     `json:"warehouseStock"`
	MinStock       int32     `json:"minStock"`
	Unit           string    `json:"unit"`
	Status         string    `json:"status"`
	StatusLabel    string    `json:"statusLabel"`
	StoreListed    bool      `json:"storeListed"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type StoreStockPage struct {
	Data   []StoreStockRow `json:"data"`
	Total  int64           `json:"total"`
	Limit  int32           `json:"limit"`
	Offset int32           `json:"offset"`
}

type StockRequestItemInput struct {
	ProductID string
	Quantity  int32
	UnitCode  string
}

type StockRequestItem struct {
	ID                string `json:"id"`
	ProductID         string `json:"productId"`
	ProductName       string `json:"productName"`
	SKU               string `json:"sku"`
	Quantity          int32  `json:"quantity"`
	ApprovedQuantity  *int32 `json:"approvedQuantity,omitempty"`
	ShippedQuantity   *int32 `json:"shippedQuantity,omitempty"`
	ReceivedQuantity  *int32 `json:"receivedQuantity,omitempty"`
	UnitCode          string `json:"unit"`
	WarehouseStock    *int32 `json:"warehouseStock,omitempty"`
}

type StockRequest struct {
	ID              string             `json:"id"`
	RequestNumber   string             `json:"requestNumber"`
	Status          string             `json:"status"`
	StatusLabel     string             `json:"statusLabel"`
	RequestedBy     string             `json:"requestedBy"`
	RequestedByName string             `json:"requestedByName"`
	ReviewedBy      *string            `json:"reviewedBy,omitempty"`
	ReviewedByName  *string            `json:"reviewedByName,omitempty"`
	Note            *string            `json:"note,omitempty"`
	ReviewNote      *string            `json:"reviewNote,omitempty"`
	ReviewedAt      *time.Time         `json:"reviewedAt,omitempty"`
	ShippedAt       *time.Time         `json:"shippedAt,omitempty"`
	ReceivedAt      *time.Time         `json:"receivedAt,omitempty"`
	ShippedByName   *string            `json:"shippedByName,omitempty"`
	ReceivedByName  *string            `json:"receivedByName,omitempty"`
	Items           []StockRequestItem `json:"items"`
	CreatedAt       time.Time          `json:"createdAt"`
	UpdatedAt       time.Time          `json:"updatedAt"`
}

type StockRequestPage struct {
	Data   []StockRequest `json:"data"`
	Total  int64          `json:"total"`
	Limit  int32          `json:"limit"`
	Offset int32          `json:"offset"`
}

var requestStatusLabels = map[string]string{
	"pending":           "Menunggu Review",
	"approved":          "Disetujui",
	"rejected":          "Ditolak",
	"processing":        "Diproses",
	"awaiting_shipment": "Menunggu Pengiriman",
	"shipped":           "Dikirim",
	"received":          "Diterima",
	"completed":         "Selesai",
	"cancelled":         "Dibatalkan",
}

func requestLabel(status string) string {
	if label, ok := requestStatusLabels[status]; ok {
		return label
	}
	return status
}

func storeStatus(stock, min int32) (code, label string) {
	if stock <= 0 {
		return "out", "Habis"
	}
	if stock <= min {
		return "low", "Stok Menipis"
	}
	return "safe", "Tersedia"
}

func (s *Store) ResolveActor(ctx context.Context, id, roleHint string) (Actor, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Actor{}, Unauthorized("Identitas pengguna wajib (X-User-Id)")
	}
	var name, role, status string
	var menus []string
	err := s.pool.QueryRow(ctx, `
		SELECT name, role, status, COALESCE(menu_access, '{}') FROM users WHERE id = $1
	`, id).Scan(&name, &role, &status, &menus)
	if errors.Is(err, pgx.ErrNoRows) {
		return Actor{}, Unauthorized("Pengguna tidak ditemukan")
	}
	if err != nil {
		return Actor{}, err
	}
	if status != "active" {
		return Actor{}, Forbidden("Akun tidak aktif")
	}
	if roleHint != "" && roleHint != role {
		return Actor{}, Forbidden("Role tidak cocok dengan data pengguna")
	}
	return Actor{ID: id, Role: role, Name: name, MenuAccess: NormalizeMenuAccess(role, menus)}, nil
}

func (s *Store) ListStoreStock(ctx context.Context, search, category string, listedOnly bool, limit, offset int32) (StoreStockPage, error) {
	if limit <= 0 {
		limit = 50
	}
	searchPat := likePattern(search)
	cat := optString(category)
	rows, err := s.pool.Query(ctx, `
		SELECT
		  p.id, p.name, c.name AS category, p.sku, p.stock, p.warehouse_stock,
		  p.min_stock, COALESCE(u.code, 'pcs') AS unit_code, p.store_listed, p.updated_at,
		  COUNT(*) OVER() AS total
		FROM products p
		JOIN categories c ON c.id = p.category_id
		LEFT JOIN LATERAL (
		  SELECT code FROM units WHERE code = 'pcs' LIMIT 1
		) u ON TRUE
		WHERE
		  ($1::boolean = FALSE OR p.store_listed = TRUE)
		  AND ($2::text IS NULL OR p.name ILIKE $2 OR p.sku ILIKE $2)
		  AND ($3::text IS NULL OR c.name = $3)
		ORDER BY p.name ASC
		LIMIT $4 OFFSET $5
	`, listedOnly, searchPat, cat, limit, offset)
	if err != nil {
		return StoreStockPage{}, err
	}
	defer rows.Close()
	out := make([]StoreStockRow, 0)
	var total int64
	for rows.Next() {
		var row StoreStockRow
		var unit string
		if err := rows.Scan(
			&row.ProductID, &row.Name, &row.Category, &row.SKU,
			&row.StoreStock, &row.WarehouseStock, &row.MinStock, &unit,
			&row.StoreListed, &row.UpdatedAt, &total,
		); err != nil {
			return StoreStockPage{}, err
		}
		row.Unit = unit
		row.Status, row.StatusLabel = storeStatus(row.StoreStock, row.MinStock)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return StoreStockPage{}, err
	}
	return StoreStockPage{Data: out, Total: total, Limit: limit, Offset: offset}, nil
}

type StoreAdjustInput struct {
	ProductID string
	Quantity  int32 // signed: + masuk, - keluar
	Reason    string
	Note      string
	Actor     Actor
}

func (s *Store) AdjustStoreStock(ctx context.Context, in StoreAdjustInput) (domain.StockMovement, error) {
	if !IsStockSupervisor(in.Actor.Role) {
		return domain.StockMovement{}, Forbidden("Hanya Supervisor yang dapat menyesuaikan stok toko")
	}
	if in.Quantity == 0 {
		return domain.StockMovement{}, BadRequest("Jumlah penyesuaian tidak boleh 0")
	}
	if strings.TrimSpace(in.Reason) == "" {
		return domain.StockMovement{}, BadRequest("Alasan penyesuaian wajib diisi")
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
		before := product.Stock
		after := before + in.Quantity
		if after < 0 {
			return BadRequest(fmt.Sprintf("Stok toko tidak mencukupi. Stok tersedia: %d", before))
		}
		if err := q.UpdateProductStock(ctx, UpdateProductStockParams{Stock: after, ID: product.ID}); err != nil {
			return err
		}
		movementType := "in"
		prefixKind := "STI"
		kind := "STORE_ADJUST_IN"
		var locFrom, locTo *string
		locTo = strPtr("store")
		qty := in.Quantity
		if in.Quantity < 0 {
			movementType = "out"
			prefixKind = "STO"
			kind = "STORE_ADJUST_OUT"
			locFrom = strPtr("store")
			locTo = nil
			qty = -in.Quantity
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
		reason := "Penyesuaian Stok Toko"
		note := blank(strings.TrimSpace(in.Note))
		if note == nil {
			note = blank(in.Reason)
		} else {
			combined := in.Reason + " — " + *note
			note = &combined
		}
		movementID := newID("stk-")
		inserted, err := q.InsertStockMovementExtended(ctx, InsertStockMovementExtendedParams{
			ID: movementID, TransactionNumber: number, MovementType: movementType,
			ProductID: product.ID, ProductName: product.Name, CategoryName: product.CategoryName,
			Quantity: qty, StockBefore: before, StockAfter: after,
			Reason: &reason, Note: note,
			OfficerID: in.Actor.ID, OfficerName: in.Actor.Name,
			LocationFrom: locFrom, LocationTo: locTo, MovementKind: &kind,
		})
		if err != nil {
			return err
		}
		created = domain.StockMovement{
			ID: movementID, TransactionNumber: number, Type: movementType,
			ProductID: product.ID, ProductName: product.Name, Category: product.CategoryName,
			StockBefore: before, StockAfter: after, Reason: &reason, Note: note,
			OfficerID: in.Actor.ID, OfficerName: in.Actor.Name, CreatedAt: tsTime(inserted.CreatedAt),
		}
		if movementType == "in" {
			created.QuantityIn = qty
		} else {
			created.QuantityOut = qty
		}
		return nil
	})
	return created, err
}

func (s *Store) UnlistStoreProduct(ctx context.Context, productID string, actor Actor, reason string) error {
	if !IsStockSupervisor(actor.Role) {
		return Forbidden("Hanya Supervisor yang dapat menghapus produk dari Stok Toko")
	}
	if strings.TrimSpace(reason) == "" {
		return BadRequest("Alasan penghapusan dari daftar wajib diisi")
	}
	return s.tx(ctx, func(q *Queries) error {
		product, err := q.LockProduct(ctx, productID)
		if errors.Is(err, pgx.ErrNoRows) {
			return BadRequest("Produk tidak ditemukan")
		}
		if err != nil {
			return err
		}
		if !product.StoreListed {
			return BadRequest("Produk sudah tidak ada di daftar Stok Toko")
		}
		_, err = q.db.Exec(ctx, `
			UPDATE products SET store_listed = FALSE, updated_at = now() WHERE id = $1
		`, product.ID)
		if err != nil {
			return err
		}
		// Catat audit tanpa mengubah qty bila stok sudah 0; bila masih ada, sesuaikan ke 0 dengan catatan.
		if product.Stock > 0 {
			before := product.Stock
			after := int32(0)
			if err := q.UpdateProductStock(ctx, UpdateProductStockParams{Stock: after, ID: product.ID}); err != nil {
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
			r := "Penyesuaian Stok Toko"
			kind := "STORE_UNLIST"
			note := reason
			locFrom := "store"
			_, err = q.InsertStockMovementExtended(ctx, InsertStockMovementExtendedParams{
				ID: newID("stk-"), TransactionNumber: number, MovementType: "out",
				ProductID: product.ID, ProductName: product.Name, CategoryName: product.CategoryName,
				Quantity: before, StockBefore: before, StockAfter: after,
				Reason: &r, Note: &note,
				OfficerID: actor.ID, OfficerName: actor.Name,
				LocationFrom: &locFrom, MovementKind: &kind,
			})
			return err
		}
		return nil
	})
}

func (s *Store) CreateStockRequest(ctx context.Context, actor Actor, note string, items []StockRequestItemInput) (StockRequest, error) {
	if !IsStockSupervisor(actor.Role) {
		return StockRequest{}, Forbidden("Hanya Supervisor yang dapat mengajukan request stok")
	}
	if len(items) == 0 {
		return StockRequest{}, BadRequest("Minimal satu produk harus diminta")
	}
	var created StockRequest
	err := s.tx(ctx, func(q *Queries) error {
		prefix, err := dailyPrefix("REQ", time.Now())
		if err != nil {
			return err
		}
		end, err := q.NextDocumentNumbers(ctx, NextDocumentNumbersParams{Prefix: prefix, Count: 1})
		if err != nil {
			return err
		}
		number := documentNumbers(prefix, end, 1)[0]
		reqID := newID("req-")
		now := time.Now().UTC()
		_, err = q.db.Exec(ctx, `
			INSERT INTO stock_requests (
			  id, request_number, status, requested_by, requested_by_name, note, created_at, updated_at
			) VALUES ($1, $2, 'pending', $3, $4, $5, $6, $6)
		`, reqID, number, actor.ID, actor.Name, blank(note), now)
		if err != nil {
			return err
		}
		outItems := make([]StockRequestItem, 0, len(items))
		for _, item := range items {
			if item.Quantity <= 0 {
				return BadRequest("Jumlah request harus lebih dari 0")
			}
			product, err := q.LockProduct(ctx, item.ProductID)
			if errors.Is(err, pgx.ErrNoRows) {
				return BadRequest("Produk tidak ditemukan")
			}
			if err != nil {
				return err
			}
			if int64(item.Quantity) > int64(product.WarehouseStock) {
				return BadRequest(fmt.Sprintf(
					"Jumlah melebihi stok gudang untuk %s. Stok gudang: %d",
					product.Name, product.WarehouseStock,
				))
			}
			unit := item.UnitCode
			if unit == "" {
				unit = "pcs"
			}
			if _, err := q.GetUnit(ctx, unit); errors.Is(err, pgx.ErrNoRows) {
				return BadRequest("Satuan tidak dikenal")
			} else if err != nil {
				return err
			}
			itemID := newID("rqi-")
			_, err = q.db.Exec(ctx, `
				INSERT INTO stock_request_items (
				  id, request_id, product_id, product_name, sku, quantity, unit_code
				) VALUES ($1, $2, $3, $4, $5, $6, $7)
			`, itemID, reqID, product.ID, product.Name, product.SKU, item.Quantity, unit)
			if err != nil {
				return err
			}
			outItems = append(outItems, StockRequestItem{
				ID: itemID, ProductID: product.ID, ProductName: product.Name,
				SKU: product.SKU, Quantity: item.Quantity, UnitCode: unit,
			})
		}
		created = StockRequest{
			ID: reqID, RequestNumber: number, Status: "pending", StatusLabel: requestLabel("pending"),
			RequestedBy: actor.ID, RequestedByName: actor.Name, Note: blank(note),
			Items: outItems, CreatedAt: now, UpdatedAt: now,
		}
		return nil
	})
	return created, err
}

func (s *Store) ListStockRequests(ctx context.Context, status string, limit, offset int32) (StockRequestPage, error) {
	if limit <= 0 {
		limit = 20
	}
	statusFilter := optString(status)
	rows, err := s.pool.Query(ctx, `
		SELECT
		  id, request_number, status, requested_by, requested_by_name,
		  reviewed_by, reviewed_by_name, note, review_note, created_at, updated_at,
		  COUNT(*) OVER() AS total
		FROM stock_requests
		WHERE ($1::text IS NULL OR status = $1)
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, statusFilter, limit, offset)
	if err != nil {
		return StockRequestPage{}, err
	}
	defer rows.Close()
	list := make([]StockRequest, 0)
	var total int64
	ids := make([]string, 0)
	for rows.Next() {
		var req StockRequest
		if err := rows.Scan(
			&req.ID, &req.RequestNumber, &req.Status, &req.RequestedBy, &req.RequestedByName,
			&req.ReviewedBy, &req.ReviewedByName, &req.Note, &req.ReviewNote,
			&req.CreatedAt, &req.UpdatedAt, &total,
		); err != nil {
			return StockRequestPage{}, err
		}
		req.StatusLabel = requestLabel(req.Status)
		req.Items = []StockRequestItem{}
		list = append(list, req)
		ids = append(ids, req.ID)
	}
	if err := rows.Err(); err != nil {
		return StockRequestPage{}, err
	}
	if len(ids) > 0 {
		itemRows, err := s.pool.Query(ctx, `
			SELECT i.id, i.request_id, i.product_id, i.product_name, i.sku, i.quantity,
			       i.approved_quantity, i.shipped_quantity, i.received_quantity, i.unit_code,
			       p.warehouse_stock
			FROM stock_request_items i
			JOIN products p ON p.id = i.product_id
			WHERE i.request_id = ANY($1)
			ORDER BY i.product_name ASC
		`, ids)
		if err != nil {
			return StockRequestPage{}, err
		}
		defer itemRows.Close()
		byReq := map[string][]StockRequestItem{}
		for itemRows.Next() {
			var it StockRequestItem
			var requestID string
			var wh int32
			if err := itemRows.Scan(&it.ID, &requestID, &it.ProductID, &it.ProductName, &it.SKU, &it.Quantity,
				&it.ApprovedQuantity, &it.ShippedQuantity, &it.ReceivedQuantity, &it.UnitCode, &wh); err != nil {
				return StockRequestPage{}, err
			}
			it.WarehouseStock = &wh
			byReq[requestID] = append(byReq[requestID], it)
		}
		if err := itemRows.Err(); err != nil {
			return StockRequestPage{}, err
		}
		for i := range list {
			list[i].Items = byReq[list[i].ID]
			if list[i].Items == nil {
				list[i].Items = []StockRequestItem{}
			}
		}
	}
	return StockRequestPage{Data: list, Total: total, Limit: limit, Offset: offset}, nil
}

func (s *Store) GetStockRequest(ctx context.Context, id string) (StockRequest, error) {
	var foundID string
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM stock_requests WHERE id = $1 OR request_number = $1 LIMIT 1
	`, id).Scan(&foundID)
	if errors.Is(err, pgx.ErrNoRows) {
		return StockRequest{}, NotFound("Request stok tidak ditemukan")
	}
	if err != nil {
		return StockRequest{}, err
	}
	return s.getStockRequestTx(ctx, New(s.pool), foundID)
}

func (s *Store) UpdateStockRequestStatus(ctx context.Context, id, action, reviewNote string, actor Actor) (StockRequest, error) {
	if !IsStockSupervisor(actor.Role) {
		return StockRequest{}, Forbidden("Hanya Supervisor/Gudang yang dapat memproses request stok")
	}
	action = strings.ToLower(strings.TrimSpace(action))
	var result StockRequest
	err := s.tx(ctx, func(q *Queries) error {
		var req struct {
			ID            string
			RequestNumber string
			Status        string
			RequestedBy   string
			RequestedName string
			Note          *string
		}
		err := q.db.QueryRow(ctx, `
			SELECT id, request_number, status, requested_by, requested_by_name, note
			FROM stock_requests WHERE id = $1 FOR UPDATE
		`, id).Scan(&req.ID, &req.RequestNumber, &req.Status, &req.RequestedBy, &req.RequestedName, &req.Note)
		if errors.Is(err, pgx.ErrNoRows) {
			return NotFound("Request stok tidak ditemukan")
		}
		if err != nil {
			return err
		}

		next := req.Status
		switch action {
		case "approve":
			if req.Status != "pending" {
				return BadRequest("Hanya request menunggu yang dapat disetujui")
			}
			next = "approved"
		case "reject":
			if req.Status != "pending" && req.Status != "approved" {
				return BadRequest("Request tidak dapat ditolak pada status ini")
			}
			next = "rejected"
		case "process":
			if req.Status != "approved" {
				return BadRequest("Request harus disetujui sebelum diproses")
			}
			next = "processing"
		case "cancel":
			if req.Status != "pending" && req.Status != "approved" {
				return BadRequest("Request tidak dapat dibatalkan pada status ini")
			}
			next = "cancelled"
		case "complete":
			return BadRequest("Gunakan endpoint penerimaan (/receive) setelah PIC mengirim stok")
		default:
			return BadRequest("Aksi request tidak dikenal")
		}

		itemRows, err := q.db.Query(ctx, `
			SELECT id, product_id, product_name, sku, quantity, unit_code
			FROM stock_request_items WHERE request_id = $1
		`, req.ID)
		if err != nil {
			return err
		}
		defer itemRows.Close()
		items := make([]StockRequestItem, 0)
		for itemRows.Next() {
			var it StockRequestItem
			if err := itemRows.Scan(&it.ID, &it.ProductID, &it.ProductName, &it.SKU, &it.Quantity, &it.UnitCode); err != nil {
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

		if next == "completed" {
			for _, it := range items {
				product, err := q.LockProduct(ctx, it.ProductID)
				if errors.Is(err, pgx.ErrNoRows) {
					return BadRequest("Produk tidak ditemukan: " + it.ProductName)
				}
				if err != nil {
					return err
				}
				if int64(it.Quantity) > int64(product.WarehouseStock) {
					return BadRequest(fmt.Sprintf(
						"Stok gudang tidak mencukupi untuk %s. Tersedia: %d, diminta: %d",
						product.Name, product.WarehouseStock, it.Quantity,
					))
				}
				whBefore := product.WarehouseStock
				whAfter := whBefore - it.Quantity
				storeBefore := product.Stock
				storeAfter := storeBefore + it.Quantity
				if err := q.UpdateProductWarehouseStock(ctx, whAfter, product.ID); err != nil {
					return err
				}
				if err := q.UpdateProductStock(ctx, UpdateProductStockParams{Stock: storeAfter, ID: product.ID}); err != nil {
					return err
				}
				// Pastikan produk kembali tampil di Stok Toko setelah transfer.
				_, _ = q.db.Exec(ctx, `UPDATE products SET store_listed = TRUE WHERE id = $1`, product.ID)

				outPrefix, err := dailyPrefix("STO", time.Now())
				if err != nil {
					return err
				}
				outEnd, err := q.NextDocumentNumbers(ctx, NextDocumentNumbersParams{Prefix: outPrefix, Count: 1})
				if err != nil {
					return err
				}
				outNumber := documentNumbers(outPrefix, outEnd, 1)[0]
				inPrefix, err := dailyPrefix("STI", time.Now())
				if err != nil {
					return err
				}
				inEnd, err := q.NextDocumentNumbers(ctx, NextDocumentNumbersParams{Prefix: inPrefix, Count: 1})
				if err != nil {
					return err
				}
				inNumber := documentNumbers(inPrefix, inEnd, 1)[0]

				reason := "Transfer Gudang ke Toko"
				note := "Request " + req.RequestNumber
				ref := req.ID
				locWH, locStore := "warehouse", "store"
				kindOut, kindIn := "TRANSFER_OUT", "TRANSFER_IN"
				unit := it.UnitCode
				_, err = q.InsertStockMovementExtended(ctx, InsertStockMovementExtendedParams{
					ID: newID("stk-"), TransactionNumber: outNumber, MovementType: "out",
					ProductID: product.ID, ProductName: product.Name, CategoryName: product.CategoryName,
					UnitCode: &unit, Quantity: it.Quantity, StockBefore: whBefore, StockAfter: whAfter,
					Reason: &reason, Note: &note, OfficerID: actor.ID, OfficerName: actor.Name,
					LocationFrom: &locWH, LocationTo: &locStore, MovementKind: &kindOut, ReferenceID: &ref,
				})
				if err != nil {
					return err
				}
				_, err = q.InsertStockMovementExtended(ctx, InsertStockMovementExtendedParams{
					ID: newID("stk-"), TransactionNumber: inNumber, MovementType: "in",
					ProductID: product.ID, ProductName: product.Name, CategoryName: product.CategoryName,
					UnitCode: &unit, Quantity: it.Quantity, StockBefore: storeBefore, StockAfter: storeAfter,
					Reason: &reason, Note: &note, OfficerID: actor.ID, OfficerName: actor.Name,
					LocationFrom: &locWH, LocationTo: &locStore, MovementKind: &kindIn, ReferenceID: &ref,
				})
				if err != nil {
					return err
				}
			}
		}

		_, err = q.db.Exec(ctx, `
			UPDATE stock_requests
			SET status = $2,
			    reviewed_by = $3,
			    reviewed_by_name = $4,
			    review_note = COALESCE(NULLIF($5, ''), review_note),
			    updated_at = now()
			WHERE id = $1
		`, req.ID, next, actor.ID, actor.Name, reviewNote)
		if err != nil {
			return err
		}
		result = StockRequest{
			ID: req.ID, RequestNumber: req.RequestNumber, Status: next, StatusLabel: requestLabel(next),
			RequestedBy: req.RequestedBy, RequestedByName: req.RequestedName,
			ReviewedBy: &actor.ID, ReviewedByName: &actor.Name, Note: req.Note,
			ReviewNote: blank(reviewNote), Items: items, UpdatedAt: time.Now().UTC(),
		}
		return nil
	})
	return result, err
}

type DamageHistoryRow struct {
	ID                string    `json:"id"`
	TransactionNumber string    `json:"transactionNumber"`
	CreatedAt         time.Time `json:"createdAt"`
	ProductID         string    `json:"productId"`
	ProductName       string    `json:"productName"`
	Quantity          int32     `json:"quantity"`
	Reason            string    `json:"reason"`
	Note              *string   `json:"note,omitempty"`
	OfficerName       string    `json:"officerName"`
}

type DamageHistoryPage struct {
	Data   []DamageHistoryRow `json:"data"`
	Total  int64              `json:"total"`
	Limit  int32              `json:"limit"`
	Offset int32              `json:"offset"`
}

func (s *Store) ListDamageHistory(ctx context.Context, search string, start, end *time.Time, limit, offset int32) (DamageHistoryPage, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT
		  id, transaction_number, created_at, product_id, product_name, quantity,
		  COALESCE(reason, ''), note, officer_name,
		  COUNT(*) OVER() AS total
		FROM stock_movements
		WHERE movement_type = 'out'
		  AND (
		    movement_kind = 'STOK_RUSAK'
		    OR reason IN (
		      'Produk Rusak', 'Kadaluarsa', 'Produk Kedaluwarsa', 'Kemasan Rusak',
		      'Produk Tidak Layak Jual', 'Kerusakan Lainnya'
		    )
		  )
		  AND ($1::text IS NULL OR product_name ILIKE $1 OR transaction_number ILIKE $1)
		  AND ($2::timestamptz IS NULL OR created_at >= $2)
		  AND ($3::timestamptz IS NULL OR created_at < $3)
		ORDER BY created_at DESC, id DESC
		LIMIT $4 OFFSET $5
	`, likePattern(search), tsArg(start), tsArg(end), limit, offset)
	if err != nil {
		return DamageHistoryPage{}, err
	}
	defer rows.Close()
	out := make([]DamageHistoryRow, 0)
	var total int64
	for rows.Next() {
		var row DamageHistoryRow
		if err := rows.Scan(
			&row.ID, &row.TransactionNumber, &row.CreatedAt, &row.ProductID, &row.ProductName,
			&row.Quantity, &row.Reason, &row.Note, &row.OfficerName, &total,
		); err != nil {
			return DamageHistoryPage{}, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return DamageHistoryPage{}, err
	}
	return DamageHistoryPage{Data: out, Total: total, Limit: limit, Offset: offset}, nil
}
