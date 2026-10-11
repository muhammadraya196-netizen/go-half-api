package httpapi

import (
	"net/http"

	"halfpos/internal/domain"
)

func (s *Server) listStock(w http.ResponseWriter, r *http.Request) {
	_, limit, offset, err := pageLimit(r)
	if err != nil {
		writeMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if limit == 0 {
		limit = 10
	}
	cursorAt, cursorID, err := cursorTime(r)
	if err != nil {
		writeMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	filter := domain.StockFilter{
		ProductID: r.URL.Query().Get("productId"), Type: r.URL.Query().Get("type"),
		Supplier: r.URL.Query().Get("supplier"), Search: r.URL.Query().Get("search"),
		Limit: limit, Offset: offset, CursorCreatedAt: cursorAt, CursorID: cursorID,
	}
	if start, _, err := dateBound(r.URL.Query().Get("startDate")); err != nil {
		writeMessage(w, http.StatusBadRequest, "Tanggal tidak valid")
		return
	} else {
		filter.Start = start
	}
	if _, end, err := dateBound(r.URL.Query().Get("endDate")); err != nil {
		writeMessage(w, http.StatusBadRequest, "Tanggal tidak valid")
		return
	} else {
		filter.EndBefore = end
	}
	page, err := s.Store.ListStock(r.Context(), filter)
	if err != nil {
		writeError(w, err)
		return
	}
	data := make([]movementJSON, 0, len(page.Data))
	for _, row := range page.Data {
		data = append(data, movementToJSON(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": data, "total": page.Total, "limit": page.Limit, "offset": page.Offset,
	})
}

func (s *Server) createStock(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Type          string  `json:"type"`
		ProductID     string  `json:"productId"`
		Supplier      string  `json:"supplier"`
		Unit          string  `json:"unit"`
		Quantity      *whole  `json:"quantity"`
		PurchasePrice *rupiah `json:"purchasePrice"`
		Reason        string  `json:"reason"`
		Note          string  `json:"note"`
		OfficerID     string  `json:"officerId"`
		OfficerName   string  `json:"officerName"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeMessage(w, http.StatusBadRequest, "Body JSON tidak valid")
		return
	}
	if body.Type != "in" && body.Type != "out" {
		writeMessage(w, http.StatusBadRequest, "Jenis mutasi tidak valid")
		return
	}
	if body.ProductID == "" {
		writeMessage(w, http.StatusBadRequest, "Produk wajib dipilih")
		return
	}
	if body.Quantity == nil || !body.Quantity.set || body.Quantity.value <= 0 {
		writeMessage(w, http.StatusBadRequest, "Jumlah harus lebih dari 0")
		return
	}
	qty, msg := fitInt32(body.Quantity.value, "Jumlah terlalu besar")
	if msg != "" {
		writeMessage(w, http.StatusBadRequest, msg)
		return
	}
	if body.OfficerID == "" || body.OfficerName == "" {
		writeMessage(w, http.StatusBadRequest, "Petugas wajib diisi")
		return
	}
	if body.Type == "in" && body.Supplier == "" {
		writeMessage(w, http.StatusBadRequest, "Supplier wajib dipilih")
		return
	}
	if body.Type == "out" {
		if _, ok := stockReasons[body.Reason]; !ok {
			writeMessage(w, http.StatusBadRequest, "Alasan stok keluar wajib dipilih")
			return
		}
	}
	var price *int64
	if body.PurchasePrice != nil {
		if !body.PurchasePrice.set || body.PurchasePrice.value < 0 {
			writeMessage(w, http.StatusBadRequest, "Harga beli tidak boleh negatif")
			return
		}
		price = &body.PurchasePrice.value
	}
	movement, err := s.Store.CreateStockMovement(r.Context(), domain.StockInput{
		Type: body.Type, ProductID: body.ProductID, Supplier: body.Supplier, Unit: body.Unit,
		Quantity: qty, PurchasePrice: price, Reason: body.Reason, Note: body.Note,
		OfficerID: body.OfficerID, OfficerName: body.OfficerName,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, movementToJSON(movement))
}

func (s *Server) stockOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := s.Store.StockOverview(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	products := make([]map[string]any, 0, len(overview.Products))
	for _, product := range overview.Products {
		products = append(products, map[string]any{
			"productId": product.ProductID, "name": product.Name, "category": product.Category,
			"currentStock": product.CurrentStock, "totalIn": product.TotalIn, "totalOut": product.TotalOut,
			"minStock": product.MinStock, "status": product.Status,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"summary": map[string]any{
			"totalProducts": overview.Summary.TotalProducts, "totalStock": overview.Summary.TotalStock,
			"totalStockInToday": overview.Summary.TotalStockInToday, "totalStockOutToday": overview.Summary.TotalStockOutToday,
			"lowStockCount": overview.Summary.LowStockCount, "outOfStockCount": overview.Summary.OutOfStockCount,
		},
		"products": products,
	})
}

func (s *Server) analytics(w http.ResponseWriter, r *http.Request) {
	stats, err := s.Store.Analytics(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	avg := int64(0)
	if stats.CompletedOrders > 0 {
		avg = stats.TotalRevenue / stats.CompletedOrders
	}
	todayAvg := int64(0)
	if stats.TodayOrderCount > 0 {
		todayAvg = stats.TodaySales / stats.TodayOrderCount
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"totalRevenue": stats.TotalRevenue, "totalOrders": stats.TotalOrders,
		"totalProducts": stats.TotalProducts, "averageOrderValue": avg,
		"revenueGrowth": 0, "ordersGrowth": 0,
		"todaySales": stats.TodaySales, "todayOrderCount": stats.TodayOrderCount,
		"todayItemsSold": stats.TodayItemsSold, "todayAverageSale": todayAvg,
		"salesByDay": []any{}, "salesByCategory": []any{}, "topProducts": []any{},
	})
}
