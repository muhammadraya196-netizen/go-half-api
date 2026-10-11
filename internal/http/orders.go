package httpapi

import (
	"net/http"
	"strings"

	"halfpos/internal/domain"
)

func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	present, limit, offset, err := pageLimit(r)
	if err != nil {
		writeMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	cursorAt, cursorID, err := cursorTime(r)
	if err != nil {
		writeMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	filter := domain.OrderFilter{
		Status: r.URL.Query().Get("status"), Search: r.URL.Query().Get("search"),
		SortBy: r.URL.Query().Get("sortBy"), SortDir: r.URL.Query().Get("sortDir"),
		Offset: offset, CursorCreatedAt: cursorAt, CursorID: cursorID,
	}
	if present {
		filter.Limit = &limit
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
	orders, total, err := s.Store.ListOrders(r.Context(), filter)
	if err != nil {
		writeError(w, err)
		return
	}
	data := make([]orderJSON, 0, len(orders))
	for _, order := range orders {
		data = append(data, orderToJSON(order))
	}
	if !present {
		writeJSON(w, http.StatusOK, data)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": data, "total": total, "limit": limit, "offset": offset,
	})
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	var body orderBody
	if err := decodeJSON(r, &body); err != nil {
		writeMessage(w, http.StatusBadRequest, "Body JSON tidak valid")
		return
	}
	in, msg := validateOrder(body)
	if msg != "" {
		writeMessage(w, http.StatusBadRequest, msg)
		return
	}
	order, err := s.Store.CreateOrder(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, orderToJSON(order))
}

type orderItemBody struct {
	ProductID   string  `json:"productId"`
	ProductName string  `json:"productName"`
	Quantity    *whole  `json:"quantity"`
	Price       *rupiah `json:"price"`
}

type paymentBody struct {
	Method       string  `json:"method"`
	Amount       *rupiah `json:"amount"`
	CashReceived *rupiah `json:"cashReceived"`
}

type orderBody struct {
	Items         []orderItemBody `json:"items"`
	Subtotal      *rupiah         `json:"subtotal"`
	Tax           *rupiah         `json:"tax"`
	Discount      *rupiah         `json:"discount"`
	Total         *rupiah         `json:"total"`
	PaymentMethod string          `json:"paymentMethod"`
	Payments      []paymentBody   `json:"payments"`
	CashierID     string          `json:"cashierId"`
	CashierName   string          `json:"cashierName"`
	CustomerName  string          `json:"customerName"`
	Notes         string          `json:"notes"`
	CashReceived  *rupiah         `json:"cashReceived"`
	ChangeAmount  *rupiah         `json:"changeAmount"`
}

func validateOrder(body orderBody) (domain.CreateOrderInput, string) {
	if len(body.Items) == 0 {
		return domain.CreateOrderInput{}, "Keranjang kosong"
	}
	if _, ok := paymentMethods[body.PaymentMethod]; !ok {
		return domain.CreateOrderInput{}, "Metode pembayaran tidak sesuai"
	}
	if strings.TrimSpace(body.CashierID) == "" || strings.TrimSpace(body.CashierName) == "" {
		return domain.CreateOrderInput{}, "Kasir wajib diisi"
	}
	subtotal, msg := requiredMoney(body.Subtotal, "Subtotal tidak sesuai")
	if msg != "" {
		return domain.CreateOrderInput{}, msg
	}
	tax, msg := requiredMoney(body.Tax, "Pajak tidak boleh negatif")
	if msg != "" {
		return domain.CreateOrderInput{}, msg
	}
	discount, msg := requiredMoney(body.Discount, "Diskon tidak boleh negatif")
	if msg != "" {
		return domain.CreateOrderInput{}, msg
	}
	total, msg := requiredMoney(body.Total, "Total tidak sesuai")
	if msg != "" {
		return domain.CreateOrderInput{}, msg
	}
	items := make([]domain.OrderItemInput, 0, len(body.Items))
	for _, item := range body.Items {
		if strings.TrimSpace(item.ProductID) == "" {
			return domain.CreateOrderInput{}, "Produk wajib dipilih"
		}
		if item.Quantity == nil || !item.Quantity.set || item.Quantity.value <= 0 {
			return domain.CreateOrderInput{}, "Jumlah harus lebih dari 0"
		}
		qty, msg := fitInt32(item.Quantity.value, "Jumlah terlalu besar")
		if msg != "" {
			return domain.CreateOrderInput{}, msg
		}
		if item.Price == nil || !item.Price.set || item.Price.value < 0 {
			return domain.CreateOrderInput{}, "Harga tidak boleh negatif"
		}
		items = append(items, domain.OrderItemInput{
			ProductID: item.ProductID, ProductName: strings.TrimSpace(item.ProductName),
			Quantity: qty, Price: item.Price.value,
		})
	}
	payments := make([]domain.PaymentInput, 0, len(body.Payments))
	for _, payment := range body.Payments {
		if _, ok := paymentMethods[payment.Method]; !ok {
			return domain.CreateOrderInput{}, "Metode pembayaran tidak sesuai"
		}
		if payment.Amount == nil || !payment.Amount.set || payment.Amount.value <= 0 {
			return domain.CreateOrderInput{}, "Nominal harus lebih besar dari 0"
		}
		line := domain.PaymentInput{Method: payment.Method, Amount: payment.Amount.value}
		if payment.CashReceived != nil {
			if !payment.CashReceived.set || payment.CashReceived.value < 0 {
				return domain.CreateOrderInput{}, "Nominal pembayaran kurang dari total tagihan"
			}
			line.CashReceived = &payment.CashReceived.value
		}
		payments = append(payments, line)
	}
	var change int64
	if body.ChangeAmount != nil {
		if !body.ChangeAmount.set || body.ChangeAmount.value < 0 {
			return domain.CreateOrderInput{}, "Perhitungan kembalian tidak valid"
		}
		change = body.ChangeAmount.value
	}
	var headerCash *int64
	if body.CashReceived != nil {
		if !body.CashReceived.set || body.CashReceived.value < 0 {
			return domain.CreateOrderInput{}, "Nominal pembayaran kurang dari total tagihan"
		}
		headerCash = &body.CashReceived.value
	}
	if msg := checkPayments(body.PaymentMethod, payments, total, headerCash, change); msg != "" {
		return domain.CreateOrderInput{}, msg
	}
	return domain.CreateOrderInput{
		Items: items, Subtotal: subtotal, Tax: tax, Discount: discount, Total: total,
		PaymentMethod: body.PaymentMethod, Payments: payments,
		CashierID: strings.TrimSpace(body.CashierID), CashierName: strings.TrimSpace(body.CashierName),
		CustomerName: body.CustomerName, Notes: body.Notes,
		CashReceived: headerCash, ChangeAmount: change,
	}, ""
}

func requiredMoney(value *rupiah, message string) (int64, string) {
	if value == nil || !value.set || value.value < 0 {
		return 0, message
	}
	return value.value, ""
}

func checkPayments(method string, payments []domain.PaymentInput, total int64, headerCash *int64, change int64) string {
	if len(payments) > 0 {
		if payments[0].Method != method {
			return "Metode pembayaran tidak sesuai"
		}
		var paid int64
		for _, line := range payments {
			paid += line.Amount
		}
		var expected int64
		if len(payments) == 1 && payments[0].Method == "cash" {
			tendered := payments[0].Amount
			if payments[0].CashReceived != nil {
				tendered = *payments[0].CashReceived
			}
			if tendered < total {
				return "Nominal pembayaran kurang dari total tagihan"
			}
			if payments[0].Amount != total {
				return "Nominal pembayaran tidak sesuai total tagihan"
			}
			expected = tendered - total
		} else if paid > total {
			return "Total pembayaran melebihi total tagihan."
		} else if paid < total {
			return "Nominal pembayaran kurang dari total tagihan"
		} else {
			for _, line := range payments {
				if line.Method != "cash" {
					continue
				}
				tendered := line.Amount
				if line.CashReceived != nil {
					tendered = *line.CashReceived
				}
				if tendered < line.Amount {
					return "Uang diterima kurang dari jumlah pembayaran tunai"
				}
				expected += tendered - line.Amount
			}
		}
		if change != expected {
			return "Perhitungan kembalian tidak valid"
		}
		return ""
	}
	if method == "cash" {
		if headerCash == nil {
			return "Uang dibayar wajib diisi untuk pembayaran tunai"
		}
		if *headerCash < total {
			return "Nominal pembayaran kurang dari total tagihan"
		}
		if change != *headerCash-total {
			return "Perhitungan kembalian tidak valid"
		}
	}
	return ""
}
