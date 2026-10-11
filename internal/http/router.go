package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"halfpos/internal/auth"
	"halfpos/internal/store"

	"github.com/go-chi/chi/v5"
)

type Server struct {
	Store    *store.Store
	Secret   []byte
	TokenTTL time.Duration
	Timeout  time.Duration
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(s.requestID)
	r.Use(s.logRequest)
	r.Use(s.recover)
	r.Use(s.timeout)
	r.Post("/api/auth/login", s.login)
	r.Group(func(r chi.Router) {
		r.Use(s.authenticate)
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
		r.Post("/api/stock", s.createStock)

		r.Get("/api/analytics", s.analytics)
	})
	return r
}

type actorKey struct{}

func withActor(ctx context.Context, actor auth.Claims) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

func actorFrom(ctx context.Context) (auth.Claims, bool) {
	actor, ok := ctx.Value(actorKey{}).(auth.Claims)
	return actor, ok
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
			"route", route,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
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

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		token, ok := cutBearer(header)
		if !ok {
			writeMessage(w, http.StatusUnauthorized, "Token tidak valid")
			return
		}
		claims, err := auth.Parse(s.Secret, token, time.Now())
		if err != nil {
			writeMessage(w, http.StatusUnauthorized, "Token tidak valid")
			return
		}
		next.ServeHTTP(w, r.WithContext(withActor(r.Context(), claims)))
	})
}

func cutBearer(header string) (string, bool) {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return "", false
	}
	token := header[len(prefix):]
	if token == "" {
		return "", false
	}
	return token, true
}
