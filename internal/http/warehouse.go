package httpapi

import (
	"net/http"
	"strings"

	"halfpos/internal/store"
)

func (s *Server) requireWarehousePIC(w http.ResponseWriter, r *http.Request) (store.Actor, bool) {
	actor, ok := s.requireSessionActor(w, r)
	if !ok {
		return store.Actor{}, false
	}
	if !store.IsWarehousePIC(actor.Role) {
		writeError(w, store.Forbidden("Hanya PIC Gudang yang diizinkan"))
		return store.Actor{}, false
	}
	return actor, true
}

func (s *Server) requireWarehouseViewer(w http.ResponseWriter, r *http.Request) (store.Actor, bool) {
	actor, ok := s.requireSessionActor(w, r)
	if !ok {
		return store.Actor{}, false
	}
	if !store.IsWarehouseViewer(actor.Role) {
		writeError(w, store.Forbidden("Tidak memiliki akses Stok Gudang"))
		return store.Actor{}, false
	}
	return actor, true
}

func (s *Server) warehouseOverview(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireWarehouseViewer(w, r); !ok {
		return
	}
	out, err := s.Store.WarehouseOverview(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listWarehouseMovements(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireWarehouseViewer(w, r); !ok {
		return
	}
	_, limit, offset, err := pageLimit(r)
	if err != nil {
		writeMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if limit == 0 {
		limit = 20
	}
	startAt, _, err := dateBound(r.URL.Query().Get("startDate"))
	if err != nil {
		writeMessage(w, http.StatusBadRequest, "Tanggal tidak valid")
		return
	}
	_, endBefore, err := dateBound(r.URL.Query().Get("endDate"))
	if err != nil {
		writeMessage(w, http.StatusBadRequest, "Tanggal tidak valid")
		return
	}
	page, err := s.Store.ListWarehouseMovements(r.Context(), store.WarehouseMovementFilter{
		Search: r.URL.Query().Get("search"), Activity: r.URL.Query().Get("activity"),
		OfficerID: r.URL.Query().Get("officerId"), Start: startAt, EndBefore: endBefore,
		Limit: limit, Offset: offset, IncludeVoided: r.URL.Query().Get("includeVoided") == "1",
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) createWarehouseIn(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireWarehousePIC(w, r)
	if !ok {
		return
	}
	var body struct {
		ProductID string `json:"productId"`
		Quantity  *whole `json:"quantity"`
		Unit      string `json:"unit"`
		Source    string `json:"source"`
		Note      string `json:"note"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeMessage(w, http.StatusBadRequest, "Body JSON tidak valid")
		return
	}
	if body.ProductID == "" || body.Quantity == nil || !body.Quantity.set {
		writeMessage(w, http.StatusBadRequest, "Produk dan jumlah wajib diisi")
		return
	}
	qty, msg := fitInt32(body.Quantity.value, "Jumlah terlalu besar")
	if msg != "" {
		writeMessage(w, http.StatusBadRequest, msg)
		return
	}
	created, err := s.Store.CreateWarehouseStockIn(r.Context(), store.WarehouseStockInInput{
		ProductID: body.ProductID, Quantity: qty, Unit: body.Unit,
		Source: body.Source, Note: body.Note, Actor: actor,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, movementToJSON(created))
}

func (s *Server) createWarehouseDamage(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireWarehousePIC(w, r)
	if !ok {
		return
	}
	var body struct {
		ProductID string `json:"productId"`
		Quantity  *whole `json:"quantity"`
		Unit      string `json:"unit"`
		Reason    string `json:"reason"`
		Note      string `json:"note"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeMessage(w, http.StatusBadRequest, "Body JSON tidak valid")
		return
	}
	if body.ProductID == "" || body.Quantity == nil || !body.Quantity.set {
		writeMessage(w, http.StatusBadRequest, "Produk dan jumlah wajib diisi")
		return
	}
	qty, msg := fitInt32(body.Quantity.value, "Jumlah terlalu besar")
	if msg != "" {
		writeMessage(w, http.StatusBadRequest, msg)
		return
	}
	created, err := s.Store.CreateWarehouseDamage(r.Context(), store.WarehouseDamageInput{
		ProductID: body.ProductID, Quantity: qty, Unit: body.Unit,
		Reason: body.Reason, Note: body.Note, Actor: actor,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, movementToJSON(created))
}

func (s *Server) voidWarehouseMovement(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireWarehousePIC(w, r)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = decodeJSON(r, &body)
	if err := s.Store.VoidWarehouseMovement(r.Context(), chiID(r), body.Reason, actor); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Pencatatan dibatalkan"})
}

func (s *Server) editWarehouseMovement(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireWarehousePIC(w, r)
	if !ok {
		return
	}
	var body struct {
		Quantity *whole `json:"quantity"`
		Note     string `json:"note"`
		Reason   string `json:"reason"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeMessage(w, http.StatusBadRequest, "Body JSON tidak valid")
		return
	}
	if body.Quantity == nil || !body.Quantity.set {
		writeMessage(w, http.StatusBadRequest, "Jumlah wajib diisi")
		return
	}
	qty, msg := fitInt32(body.Quantity.value, "Jumlah terlalu besar")
	if msg != "" {
		writeMessage(w, http.StatusBadRequest, msg)
		return
	}
	created, err := s.Store.EditWarehouseMovement(r.Context(), chiID(r), qty, body.Note, body.Reason, actor)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, movementToJSON(created))
}

func (s *Server) reviewStockRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireWarehousePIC(w, r)
	if !ok {
		return
	}
	var body struct {
		Action     string `json:"action"`
		ReviewNote string `json:"reviewNote"`
		Items      []struct {
			ItemID           string `json:"itemId"`
			ApprovedQuantity *whole `json:"approvedQuantity"`
		} `json:"items"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeMessage(w, http.StatusBadRequest, "Body JSON tidak valid")
		return
	}
	items := make([]store.ReviewItemInput, 0, len(body.Items))
	for _, row := range body.Items {
		if row.ApprovedQuantity == nil || !row.ApprovedQuantity.set {
			continue
		}
		qty, msg := fitInt32(row.ApprovedQuantity.value, "Jumlah terlalu besar")
		if msg != "" {
			writeMessage(w, http.StatusBadRequest, msg)
			return
		}
		items = append(items, store.ReviewItemInput{ItemID: row.ItemID, ApprovedQuantity: qty})
	}
	updated, err := s.Store.ReviewStockRequest(r.Context(), store.ReviewStockRequestInput{
		RequestID: chiID(r), Action: body.Action, ReviewNote: body.ReviewNote, Items: items, Actor: actor,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) shipStockRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireWarehousePIC(w, r)
	if !ok {
		return
	}
	updated, err := s.Store.ShipStockRequest(r.Context(), chiID(r), actor)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) receiveStockRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireSessionActor(w, r)
	if !ok {
		return
	}
	if !store.IsStockSupervisor(actor.Role) && !store.IsWarehousePIC(actor.Role) {
		writeError(w, store.Forbidden("Hanya Supervisor yang dapat mengonfirmasi penerimaan"))
		return
	}
	var body struct {
		Items []struct {
			ItemID           string `json:"itemId"`
			ReceivedQuantity *whole `json:"receivedQuantity"`
		} `json:"items"`
	}
	_ = decodeJSON(r, &body)
	items := make([]store.ReceiveItemInput, 0, len(body.Items))
	for _, row := range body.Items {
		if row.ReceivedQuantity == nil || !row.ReceivedQuantity.set {
			continue
		}
		qty, msg := fitInt32(row.ReceivedQuantity.value, "Jumlah terlalu besar")
		if msg != "" {
			writeMessage(w, http.StatusBadRequest, msg)
			return
		}
		items = append(items, store.ReceiveItemInput{ItemID: row.ItemID, ReceivedQuantity: qty})
	}
	updated, err := s.Store.ReceiveStockRequest(r.Context(), chiID(r), items, actor)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) listWarehouseStock(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireWarehouseViewer(w, r); !ok {
		return
	}
	_, limit, offset, err := pageLimit(r)
	if err != nil {
		writeMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if limit == 0 {
		limit = 100
	}
	page, err := s.Store.ListStoreStock(r.Context(), r.URL.Query().Get("search"), r.URL.Query().Get("category"), false, limit, offset)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// silence unused import if any
var _ = strings.TrimSpace
