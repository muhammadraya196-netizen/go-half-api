package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"halfpos/internal/store"

	"github.com/go-chi/chi/v5"
)

type Server struct {
	Store       *store.Store
	Timeout     time.Duration
	TokenSecret string
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(s.cors)
	r.Use(s.requestID)
	r.Use(s.logRequest)
	r.Use(s.recover)
	r.Use(s.timeout)
	r.Post("/api/auth/login", s.login)
	r.Group(func(r chi.Router) {
		r.Get("/api/products", s.listProducts)
		r.Post("/api/products", s.createProduct)
		r.Get("/api/products/{id}", s.getProduct)
		r.Put("/api/products/{id}", s.updateProduct)
		r.Delete("/api/products/{id}", s.deleteProduct)

		r.Get("/api/users", s.listUsers)
		r.Post("/api/users", s.createUser)
		r.Get("/api/users/{id}", s.getUser)
		r.Put("/api/users/{id}", s.updateUser)
		r.Patch("/api/users/{id}", s.patchUser)
		r.Delete("/api/users/{id}", s.deleteUser)

		r.Get("/api/orders", s.listOrders)
		r.Post("/api/orders", s.createOrder)

		r.Get("/api/stock/overview", s.stockOverview)
		r.Get("/api/stock", s.listStock)
		r.Post("/api/stock", s.createStockGuarded)

		r.Get("/api/inventory/store", s.listStoreStock)
		r.Post("/api/inventory/store/adjust", s.adjustStoreStock)
		r.Delete("/api/inventory/store/{id}", s.unlistStoreProduct)
		r.Get("/api/inventory/damage", s.listDamageHistory)

		r.Get("/api/stock-requests", s.listStockRequests)
		r.Post("/api/stock-requests", s.createStockRequest)
		r.Get("/api/stock-requests/{id}", s.getStockRequest)
		r.Patch("/api/stock-requests/{id}", s.patchStockRequest)
		r.Post("/api/stock-requests/{id}/review", s.reviewStockRequest)
		r.Post("/api/stock-requests/{id}/ship", s.shipStockRequest)
		r.Post("/api/stock-requests/{id}/receive", s.receiveStockRequest)

		r.Get("/api/warehouse/overview", s.warehouseOverview)
		r.Get("/api/warehouse/stock", s.listWarehouseStock)
		r.Get("/api/warehouse/movements", s.listWarehouseMovements)
		r.Post("/api/warehouse/in", s.createWarehouseIn)
		r.Post("/api/warehouse/damage", s.createWarehouseDamage)
		r.Patch("/api/warehouse/movements/{id}", s.editWarehouseMovement)
		r.Post("/api/warehouse/movements/{id}/void", s.voidWarehouseMovement)

		r.Get("/api/analytics", s.analytics)
	})
	return r
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			var buf [8]byte
			_, _ = rand.Read(buf[:])
			id = hex.EncodeToString(buf[:])
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type requestIDKey struct{}

func (s *Server) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = r.URL.Path
		}
		id, _ := r.Context().Value(requestIDKey{}).(string)
		slog.Info("request",
			"request_id", id,
			"method", r.Method,
			"route", route,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()
		headers.Set("Access-Control-Allow-Origin", "*")
		headers.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		allowHeaders := r.Header.Get("Access-Control-Request-Headers")
		if allowHeaders == "" {
			allowHeaders = "Content-Type, Accept, Authorization"
		}
		headers.Set("Access-Control-Allow-Headers", allowHeaders)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) timeout(next http.Handler) http.Handler {
	limit := s.Timeout
	if limit <= 0 {
		limit = 15 * time.Second
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), limit)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				writeMessage(w, http.StatusInternalServerError, "Terjadi kesalahan server")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
