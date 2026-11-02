package httpapi

import (
	"net/http"
	"strings"

	"halfpos/internal/store"
)

func (s *Server) listStoreStock(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireStockViewer(w, r); !ok {
		return
	}
	_, limit, offset, err := pageLimit(r)
	if err != nil {
		writeMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if limit == 0 {
		limit = 50
	}
	listedOnly := r.URL.Query().Get("includeHidden") != "1"
	page, err := s.Store.ListStoreStock(
		r.Context(),
		r.URL.Query().Get("search"),
		r.URL.Query().Get("category"),
		listedOnly,
		limit,
		offset,
	)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) adjustStoreStock(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireStockSupervisor(w, r)
	if !ok {
		return
	}
	var body struct {
		ProductID string `json:"productId"`
		Quantity  *whole `json:"quantity"`
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
	if body.Quantity.value > 2147483647 || body.Quantity.value < -2147483647 {
		writeMessage(w, http.StatusBadRequest, "Jumlah terlalu besar")
		return
	}
	qty := int32(body.Quantity.value)
	movement, err := s.Store.AdjustStoreStock(r.Context(), store.StoreAdjustInput{
		ProductID: body.ProductID,
		Quantity:  qty,
		Reason:    body.Reason,
		Note:      body.Note,
		Actor:     actor,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, movementToJSON(movement))
}

func (s *Server) unlistStoreProduct(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireStockSupervisor(w, r)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = decodeJSON(r, &body)
	if err := s.Store.UnlistStoreProduct(r.Context(), chiID(r), actor, body.Reason); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Produk dihapus dari daftar Stok Toko"})
}

func (s *Server) listStockRequests(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireStockViewer(w, r); !ok {
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
	page, err := s.Store.ListStockRequests(r.Context(), r.URL.Query().Get("status"), limit, offset)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) getStockRequest(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireStockViewer(w, r); !ok {
		return
	}
	req, err := s.Store.GetStockRequest(r.Context(), chiID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) createStockRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireStockSupervisor(w, r)
	if !ok {
		return
	}
	var body struct {
		Note  string `json:"note"`
		Items []struct {
			ProductID string `json:"productId"`
			Quantity  *whole `json:"quantity"`
			Unit      string `json:"unit"`
		} `json:"items"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeMessage(w, http.StatusBadRequest, "Body JSON tidak valid")
		return
	}
	items := make([]store.StockRequestItemInput, 0, len(body.Items))
	for _, item := range body.Items {
		if item.ProductID == "" || item.Quantity == nil || !item.Quantity.set {
			writeMessage(w, http.StatusBadRequest, "Setiap item wajib punya produk dan jumlah")
			return
		}
		qty, msg := fitInt32(item.Quantity.value, "Jumlah terlalu besar")
		if msg != "" {
			writeMessage(w, http.StatusBadRequest, msg)
			return
		}
		items = append(items, store.StockRequestItemInput{
			ProductID: item.ProductID,
			Quantity:  qty,
			UnitCode:  item.Unit,
		})
	}
	created, err := s.Store.CreateStockRequest(r.Context(), actor, body.Note, items)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) patchStockRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireStockSupervisor(w, r)
	if !ok {
		return
	}
	var body struct {
		Action     string `json:"action"`
		ReviewNote string `json:"reviewNote"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeMessage(w, http.StatusBadRequest, "Body JSON tidak valid")
		return
	}
	if strings.TrimSpace(body.Action) == "" {
		writeMessage(w, http.StatusBadRequest, "Aksi wajib diisi")
		return
	}
	updated, err := s.Store.UpdateStockRequestStatus(r.Context(), chiID(r), body.Action, body.ReviewNote, actor)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) listDamageHistory(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireStockViewer(w, r); !ok {
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
	page, err := s.Store.ListDamageHistory(
		r.Context(),
		r.URL.Query().Get("search"),
		startAt,
		endBefore,
		limit,
		offset,
	)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) createStockGuarded(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireStockSupervisor(w, r); !ok {
		return
	}
	s.createStock(w, r)
}
