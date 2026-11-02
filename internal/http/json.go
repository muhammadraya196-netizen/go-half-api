package httpapi

import (
	"log/slog"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"halfpos/internal/domain"
	"halfpos/internal/store"
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeMessage(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"message": message})
}

func writeError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	if se, ok := err.(*store.Error); ok {
		writeMessage(w, se.Status, se.Message)
		return
	}
	slog.Error("api error", "err", err)
	writeMessage(w, http.StatusInternalServerError, "Terjadi kesalahan server")
}

func decodeJSON(r *http.Request, dest any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dest); err != nil {
		return fmt.Errorf("Body JSON tidak valid")
	}
	return nil
}

type rupiah struct {
	value int64
	set   bool
}

func (m *rupiah) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		return fmt.Errorf("nominal wajib diisi")
	}
	if bytes.ContainsAny(data, ".eE") {
		return fmt.Errorf("nominal harus bilangan bulat rupiah")
	}
	n, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return fmt.Errorf("nominal tidak valid")
	}
	m.value = n
	m.set = true
	return nil
}

type whole struct {
	value int64
	set   bool
}

func (m *whole) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		return fmt.Errorf("angka wajib diisi")
	}
	if bytes.ContainsAny(data, ".eE") {
		return fmt.Errorf("harus bilangan bulat")
	}
	n, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return fmt.Errorf("angka tidak valid")
	}
	m.value = n
	m.set = true
	return nil
}

func iso(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func queryNumber(raw string) (int64, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	if strings.ContainsAny(raw, ".eE") {
		return 0, false, fmt.Errorf("harus bilangan bulat")
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("angka tidak valid")
	}
	return n, true, nil
}

func pageLimit(r *http.Request) (present bool, limit, offset int32, err error) {
	offRaw, offOK, err := queryNumber(r.URL.Query().Get("offset"))
	if err != nil {
		return false, 0, 0, err
	}
	if offOK && offRaw > 0 {
		if offRaw > math.MaxInt32 {
			return false, 0, 0, fmt.Errorf("offset tidak valid")
		}
		offset = int32(offRaw)
	}
	if _, ok := r.URL.Query()["limit"]; !ok {
		return false, 0, offset, nil
	}
	n, ok, err := queryNumber(r.URL.Query().Get("limit"))
	if err != nil {
		return true, 0, offset, err
	}
	if !ok || n <= 0 {
		n = 10
	}
	if n > 500 {
		n = 500
	}
	return true, int32(n), offset, nil
}

func cursorTime(r *http.Request) (*time.Time, string, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("cursorCreatedAt"))
	id := strings.TrimSpace(r.URL.Query().Get("cursorId"))
	if raw == "" && id == "" {
		return nil, "", nil
	}
	if raw == "" || id == "" {
		return nil, "", fmt.Errorf("cursorCreatedAt dan cursorId harus diisi bersamaan")
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, "", fmt.Errorf("cursorCreatedAt tidak valid")
		}
	}
	return &parsed, id, nil
}

func dateBound(value string) (*time.Time, *time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil, nil
	}
	start, end, err := store.DayBounds(value)
	if err != nil {
		return nil, nil, err
	}
	return &start, &end, nil
}

type productJSON struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Description        string   `json:"description"`
	Price              int64    `json:"price"`
	Stock              int32    `json:"stock"`
	WarehouseStock     int32    `json:"warehouseStock"`
	Category           string   `json:"category"`
	Image              *string  `json:"image,omitempty"`
	SKU                string   `json:"sku"`
	Rating             *float64 `json:"rating,omitempty"`
	IsBestSeller       bool     `json:"isBestSeller"`
	IsCustomerFavorite bool     `json:"isCustomerFavorite"`
	Badge              *string  `json:"badge,omitempty"`
	MinStock           int32    `json:"minStock"`
	CreatedAt          string   `json:"createdAt"`
	UpdatedAt          string   `json:"updatedAt"`
}

func productToJSON(p domain.Product) productJSON {
	return productJSON{
		ID: p.ID, Name: p.Name, Description: p.Description, Price: p.Price, Stock: p.Stock,
		WarehouseStock: p.WarehouseStock,
		Category: p.Category, Image: p.Image, SKU: p.SKU, Rating: p.Rating,
		IsBestSeller: p.IsBestSeller, IsCustomerFavorite: p.IsCustomerFavorite,
		Badge: p.Badge, MinStock: p.MinStock, CreatedAt: iso(p.CreatedAt), UpdatedAt: iso(p.UpdatedAt),
	}
}

type userJSON struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Username   string   `json:"username"`
	Email      string   `json:"email"`
	Phone      *string  `json:"phone,omitempty"`
	Role       string   `json:"role"`
	Status     string   `json:"status"`
	Avatar     *string  `json:"avatar,omitempty"`
	MenuAccess []string `json:"menuAccess"`
	CreatedAt  string   `json:"createdAt"`
	LastLogin  *string  `json:"lastLogin,omitempty"`
}

func userToJSON(u domain.ManagedUser) userJSON {
	var last *string
	if u.LastLogin != nil {
		value := iso(*u.LastLogin)
		last = &value
	}
	menus := u.MenuAccess
	if menus == nil {
		menus = []string{}
	}
	return userJSON{
		ID: u.ID, Name: u.Name, Username: u.Username, Email: u.Email, Phone: u.Phone,
		Role: u.Role, Status: u.Status, Avatar: u.Avatar, MenuAccess: menus,
		CreatedAt: iso(u.CreatedAt), LastLogin: last,
	}
}

type activityJSON struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	Action    string `json:"action"`
	CreatedAt string `json:"createdAt"`
}

type componentJSON struct {
	ProductID   string `json:"productId"`
	ProductName string `json:"productName"`
	Quantity    int32  `json:"quantity"`
}

type orderItemJSON struct {
	ProductID        string          `json:"productId"`
	ProductName      string          `json:"productName"`
	Quantity         int32           `json:"quantity"`
	Price            int64           `json:"price"`
	Subtotal         int64           `json:"subtotal"`
	BundleComponents []componentJSON `json:"bundleComponents,omitempty"`
}

type paymentJSON struct {
	Method       string `json:"method"`
	Amount       int64  `json:"amount"`
	CashReceived *int64 `json:"cashReceived,omitempty"`
}

type orderJSON struct {
	ID            string          `json:"id"`
	OrderNumber   string          `json:"orderNumber"`
	Items         []orderItemJSON `json:"items"`
	Subtotal      int64           `json:"subtotal"`
	Tax           int64           `json:"tax"`
	Discount      int64           `json:"discount"`
	Total         int64           `json:"total"`
	PaymentMethod string          `json:"paymentMethod"`
	Payments      []paymentJSON   `json:"payments,omitempty"`
	Status        string          `json:"status"`
	CashierID     string          `json:"cashierId"`
	CashierName   string          `json:"cashierName"`
	CustomerName  *string         `json:"customerName,omitempty"`
	Notes         *string         `json:"notes,omitempty"`
	CashReceived  *int64          `json:"cashReceived,omitempty"`
	ChangeAmount  int64           `json:"changeAmount"`
	CreatedAt     string          `json:"createdAt"`
}

func orderToJSON(o domain.Order) orderJSON {
	items := make([]orderItemJSON, 0, len(o.Items))
	for _, item := range o.Items {
		mapped := orderItemJSON{
			ProductID: item.ProductID, ProductName: item.ProductName,
			Quantity: item.Quantity, Price: item.Price, Subtotal: item.Subtotal,
		}
		if len(item.BundleComponents) > 0 {
			mapped.BundleComponents = make([]componentJSON, 0, len(item.BundleComponents))
			for _, part := range item.BundleComponents {
				mapped.BundleComponents = append(mapped.BundleComponents, componentJSON{
					ProductID: part.ProductID, ProductName: part.ProductName, Quantity: part.Quantity,
				})
			}
		}
		items = append(items, mapped)
	}
	var payments []paymentJSON
	if len(o.Payments) > 0 {
		payments = make([]paymentJSON, 0, len(o.Payments))
		for _, payment := range o.Payments {
			payments = append(payments, paymentJSON{
				Method: payment.Method, Amount: payment.Amount, CashReceived: payment.CashReceived,
			})
		}
	}
	return orderJSON{
		ID: o.ID, OrderNumber: o.OrderNumber, Items: items, Subtotal: o.Subtotal, Tax: o.Tax,
		Discount: o.Discount, Total: o.Total, PaymentMethod: o.PaymentMethod, Payments: payments,
		Status: o.Status, CashierID: o.CashierID, CashierName: o.CashierName,
		CustomerName: o.CustomerName, Notes: o.Notes, CashReceived: o.CashReceived,
		ChangeAmount: o.ChangeAmount, CreatedAt: iso(o.CreatedAt),
	}
}

type movementJSON struct {
	ID                string  `json:"id"`
	TransactionNumber string  `json:"transactionNumber"`
	Type              string  `json:"type"`
	ProductID         string  `json:"productId"`
	ProductName       string  `json:"productName"`
	Category          string  `json:"category"`
	Supplier          *string `json:"supplier,omitempty"`
	Unit              *string `json:"unit,omitempty"`
	QuantityIn        int32   `json:"quantityIn"`
	QuantityOut       int32   `json:"quantityOut"`
	StockBefore       int32   `json:"stockBefore"`
	StockAfter        int32   `json:"stockAfter"`
	PurchasePrice     *int64  `json:"purchasePrice,omitempty"`
	TotalPrice        *int64  `json:"totalPrice,omitempty"`
	Reason            *string `json:"reason,omitempty"`
	Note              *string `json:"note,omitempty"`
	OfficerID         string  `json:"officerId"`
	OfficerName       string  `json:"officerName"`
	CreatedAt         string  `json:"createdAt"`
}

func movementToJSON(m domain.StockMovement) movementJSON {
	return movementJSON{
		ID: m.ID, TransactionNumber: m.TransactionNumber, Type: m.Type,
		ProductID: m.ProductID, ProductName: m.ProductName, Category: m.Category,
		Supplier: m.Supplier, Unit: m.Unit, QuantityIn: m.QuantityIn, QuantityOut: m.QuantityOut,
		StockBefore: m.StockBefore, StockAfter: m.StockAfter, PurchasePrice: m.PurchasePrice,
		TotalPrice: m.TotalPrice, Reason: m.Reason, Note: m.Note,
		OfficerID: m.OfficerID, OfficerName: m.OfficerName, CreatedAt: iso(m.CreatedAt),
	}
}
