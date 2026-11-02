package httpapi

import (
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strings"

	"halfpos/internal/domain"

	"github.com/go-chi/chi/v5"
)

var (
	usernameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{3,30}$`)
	emailRE    = regexp.MustCompile(`^[^[:space:]@]+@[^[:space:]@]+[.][^[:space:]@]+$`)
	phoneRE    = regexp.MustCompile(`^[0-9+[:space:]-]{1,30}$`)
)

var paymentMethods = map[string]struct{}{
	"cash": {}, "debit": {}, "credit": {}, "card": {}, "qris": {}, "transfer": {},
}

var roles = map[string]struct{}{
	"admin": {}, "finance": {}, "cashier": {}, "manager": {}, "supervisor": {}, "pic_gudang": {},
}

var userStatuses = map[string]struct{}{
	"active": {}, "inactive": {},
}

var stockReasons = map[string]struct{}{
	"Penjualan": {}, "Produk Rusak": {}, "Kadaluarsa": {}, "Sampel": {}, "Retur": {}, "Lainnya": {},
	"Produk Kedaluwarsa": {}, "Kemasan Rusak": {}, "Produk Tidak Layak Jual": {}, "Kerusakan Lainnya": {},
	"Transfer Gudang ke Toko": {}, "Penyesuaian Stok Toko": {}, "Penyesuaian Stok Gudang": {},
}

func chiID(r *http.Request) string {
	return chi.URLParam(r, "id")
}

func emailOK(email string) bool {
	return emailRE.MatchString(email) && len(email) <= 254
}

func requireText(value, emptyMessage string, max int) (string, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", emptyMessage
	}
	if max > 0 && len([]rune(value)) > max {
		return "", fmt.Sprintf("Maksimal %d karakter", max)
	}
	return value, ""
}

func fitInt32(n int64, message string) (int32, string) {
	if n < 0 || n > math.MaxInt32 {
		return 0, message
	}
	return int32(n), ""
}

type productBody struct {
	Name               *string  `json:"name"`
	Description        *string  `json:"description"`
	Price              *rupiah  `json:"price"`
	Stock              *whole   `json:"stock"`
	Category           *string  `json:"category"`
	SKU                *string  `json:"sku"`
	Image              *string  `json:"image"`
	Rating             *float64 `json:"rating"`
	IsBestSeller       *bool    `json:"isBestSeller"`
	IsCustomerFavorite *bool    `json:"isCustomerFavorite"`
	Badge              *string  `json:"badge"`
	MinStock           *whole   `json:"minStock"`
}

func (s *Server) listProducts(w http.ResponseWriter, r *http.Request) {
	present, limit, offset, err := pageLimit(r)
	if err != nil {
		writeMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	filter := domain.ProductFilter{
		Search: r.URL.Query().Get("search"), Category: r.URL.Query().Get("category"),
		Status: r.URL.Query().Get("status"), Offset: offset,
	}
	if present {
		filter.Limit = &limit
	}
	if n, ok, err := queryNumber(r.URL.Query().Get("minPrice")); err != nil {
		writeMessage(w, http.StatusBadRequest, "Harga tidak valid")
		return
	} else if ok {
		filter.MinPrice = &n
	}
	if n, ok, err := queryNumber(r.URL.Query().Get("maxPrice")); err != nil {
		writeMessage(w, http.StatusBadRequest, "Harga tidak valid")
		return
	} else if ok {
		filter.MaxPrice = &n
	}
	if n, ok, err := queryNumber(r.URL.Query().Get("minStock")); err != nil {
		writeMessage(w, http.StatusBadRequest, "Stok tidak valid")
		return
	} else if ok {
		stock, msg := fitInt32(n, "Stok tidak valid")
		if msg != "" {
			writeMessage(w, http.StatusBadRequest, msg)
			return
		}
		filter.MinStock = &stock
	}
	page, err := s.Store.ListProducts(r.Context(), filter)
	if err != nil {
		writeError(w, err)
		return
	}
	data := make([]productJSON, 0, len(page.Data))
	for _, item := range page.Data {
		data = append(data, productToJSON(item))
	}
	if !present {
		writeJSON(w, http.StatusOK, data)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": data, "total": page.Total, "limit": page.Limit, "offset": page.Offset,
	})
}

func (s *Server) createProduct(w http.ResponseWriter, r *http.Request) {
	var body productBody
	if err := decodeJSON(r, &body); err != nil {
		writeMessage(w, http.StatusBadRequest, "Body JSON tidak valid")
		return
	}
	name, sku, category, price, stock, description, msg := validateNewProduct(body)
	if msg != "" {
		writeMessage(w, http.StatusBadRequest, msg)
		return
	}
	product, err := s.Store.CreateProduct(r.Context(), name, sku, description, category, price, stock)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, productToJSON(product))
}

func validateNewProduct(body productBody) (name, sku, category string, price int64, stock int32, description, msg string) {
	if body.Name == nil {
		return "", "", "", 0, 0, "", "Nama produk wajib diisi"
	}
	name, msg = requireText(*body.Name, "Nama produk wajib diisi", 100)
	if msg != "" {
		return "", "", "", 0, 0, "", msg
	}
	if body.SKU == nil {
		return "", "", "", 0, 0, "", "SKU wajib diisi"
	}
	sku, msg = requireText(*body.SKU, "SKU wajib diisi", 20)
	if msg != "" {
		return "", "", "", 0, 0, "", msg
	}
	if body.Category == nil || strings.TrimSpace(*body.Category) == "" {
		return "", "", "", 0, 0, "", "Kategori wajib dipilih"
	}
	category = strings.TrimSpace(*body.Category)
	if body.Price == nil || !body.Price.set || body.Price.value < 0 {
		return "", "", "", 0, 0, "", "Harga tidak boleh negatif"
	}
	price = body.Price.value
	if body.Stock == nil || !body.Stock.set {
		return "", "", "", 0, 0, "", "Stok tidak boleh negatif"
	}
	stock, msg = fitInt32(body.Stock.value, "Stok tidak boleh negatif")
	if msg != "" || body.Stock.value < 0 {
		return "", "", "", 0, 0, "", "Stok tidak boleh negatif"
	}
	if body.Description != nil {
		description = *body.Description
		if len([]rune(description)) > 500 {
			return "", "", "", 0, 0, "", "Maksimal 500 karakter"
		}
	}
	return name, sku, category, price, stock, description, ""
}

func (s *Server) getProduct(w http.ResponseWriter, r *http.Request) {
	product, err := s.Store.GetProduct(r.Context(), chiID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, productToJSON(product))
}

func (s *Server) updateProduct(w http.ResponseWriter, r *http.Request) {
	var body productBody
	if err := decodeJSON(r, &body); err != nil {
		writeMessage(w, http.StatusBadRequest, "Body JSON tidak valid")
		return
	}
	in, msg := validateProductUpdate(body)
	if msg != "" {
		writeMessage(w, http.StatusBadRequest, msg)
		return
	}
	product, err := s.Store.UpdateProduct(r.Context(), chiID(r), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, productToJSON(product))
}

func validateProductUpdate(body productBody) (domain.ProductInput, string) {
	var in domain.ProductInput
	if body.Name != nil {
		name, msg := requireText(*body.Name, "Nama produk wajib diisi", 100)
		if msg != "" {
			return in, msg
		}
		in.Name = &name
	}
	if body.SKU != nil {
		sku, msg := requireText(*body.SKU, "SKU wajib diisi", 20)
		if msg != "" {
			return in, msg
		}
		in.SKU = &sku
	}
	if body.Category != nil {
		category := strings.TrimSpace(*body.Category)
		if category == "" {
			return in, "Kategori wajib dipilih"
		}
		in.Category = &category
	}
	if body.Price != nil {
		if !body.Price.set || body.Price.value < 0 {
			return in, "Harga tidak boleh negatif"
		}
		in.Price = &body.Price.value
	}
	if body.Stock != nil {
		stock, msg := fitInt32(body.Stock.value, "Stok tidak boleh negatif")
		if msg != "" || body.Stock.value < 0 {
			return in, "Stok tidak boleh negatif"
		}
		in.Stock = &stock
	}
	if body.Description != nil {
		if len([]rune(*body.Description)) > 500 {
			return in, "Maksimal 500 karakter"
		}
		in.DescriptionSet = true
		in.Description = blankOptional(*body.Description)
	}
	if body.Image != nil {
		in.ImageSet = true
		in.Image = blankOptional(*body.Image)
	}
	if body.MinStock != nil {
		minStock, msg := fitInt32(body.MinStock.value, "Stok minimum tidak valid")
		if msg != "" || body.MinStock.value < 0 {
			return in, "Stok minimum tidak valid"
		}
		in.MinStock = &minStock
	}
	if body.Rating != nil {
		if *body.Rating < 0 || *body.Rating > 5 {
			return in, "Rating tidak valid"
		}
		in.RatingSet = true
		in.Rating = body.Rating
	}
	if body.Badge != nil {
		in.BadgeSet = true
		badge := strings.TrimSpace(*body.Badge)
		if badge == "" {
			in.Badge = nil
		} else if badge != "new" && badge != "limited" {
			return in, "Badge tidak valid"
		} else {
			in.Badge = &badge
		}
	}
	in.IsBestSeller = body.IsBestSeller
	in.IsCustomerFavorite = body.IsCustomerFavorite
	return in, ""
}

func blankOptional(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func (s *Server) deleteProduct(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteProduct(r.Context(), chiID(r)); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}
