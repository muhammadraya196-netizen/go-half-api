package httpapi

import (
	"net/http"
	"strings"

	"halfpos/internal/auth"
	"halfpos/internal/store"
)

func (s *Server) requireActor(w http.ResponseWriter, r *http.Request) (store.Actor, bool) {
	actor, err := s.Store.ResolveActor(
		r.Context(),
		r.Header.Get("X-User-Id"),
		r.Header.Get("X-User-Role"),
	)
	if err != nil {
		writeError(w, err)
		return store.Actor{}, false
	}
	return actor, true
}

func (s *Server) requireStockViewer(w http.ResponseWriter, r *http.Request) (store.Actor, bool) {
	actor, ok := s.requireSessionActor(w, r)
	if !ok {
		return store.Actor{}, false
	}
	if actor.Role == "admin" || store.HasMenu(actor.MenuAccess, "stock_store") || store.HasMenu(actor.MenuAccess, "stock_warehouse") || store.HasMenu(actor.MenuAccess, "stock_request") || store.HasMenu(actor.MenuAccess, "stock_damage") || store.IsStockViewer(actor.Role) {
		return actor, true
	}
	writeError(w, store.Forbidden("Tidak memiliki akses melihat stok"))
	return store.Actor{}, false
}

func (s *Server) requireStockSupervisor(w http.ResponseWriter, r *http.Request) (store.Actor, bool) {
	actor, ok := s.requireSessionActor(w, r)
	if !ok {
		return store.Actor{}, false
	}
	if actor.Role == "admin" || store.IsStockSupervisor(actor.Role) || store.HasMenu(actor.MenuAccess, "stock_request") {
		return actor, true
	}
	writeError(w, store.Forbidden("Hanya Supervisor yang diizinkan"))
	return store.Actor{}, false
}

func (s *Server) requireAdminActor(w http.ResponseWriter, r *http.Request) (store.Actor, bool) {
	actor, ok := s.requireSessionActor(w, r)
	if !ok {
		return store.Actor{}, false
	}
	if actor.Role != "admin" {
		writeError(w, store.Forbidden("Hanya Admin yang diizinkan mengelola user"))
		return store.Actor{}, false
	}
	if !store.HasMenu(actor.MenuAccess, "users") && actor.Role != "admin" {
		writeError(w, store.Forbidden("Tidak memiliki akses Management User"))
		return store.Actor{}, false
	}
	return actor, true
}

func (s *Server) requireUsersViewer(w http.ResponseWriter, r *http.Request) (store.Actor, bool) {
	actor, ok := s.requireSessionActor(w, r)
	if !ok {
		return store.Actor{}, false
	}
	if actor.Role == "admin" || store.HasMenu(actor.MenuAccess, "users") {
		return actor, true
	}
	writeError(w, store.Forbidden("Tidak memiliki akses Management User"))
	return store.Actor{}, false
}

func (s *Server) requireMenu(w http.ResponseWriter, r *http.Request, menu string) (store.Actor, bool) {
	actor, ok := s.requireSessionActor(w, r)
	if !ok {
		return store.Actor{}, false
	}
	if actor.Role == "admin" || store.HasMenu(actor.MenuAccess, menu) {
		return actor, true
	}
	writeError(w, store.Forbidden("Tidak memiliki akses menu yang diminta"))
	return store.Actor{}, false
}

func (s *Server) requireSessionActor(w http.ResponseWriter, r *http.Request) (store.Actor, bool) {
	// Prioritas: Bearer token yang diverifikasi backend, lalu header sebagai fallback kompatibilitas.
	authz := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		token := strings.TrimSpace(authz[7:])
		userID, err := auth.ParseToken(s.TokenSecret, token)
		if err != nil {
			writeMessage(w, http.StatusUnauthorized, "Sesi tidak valid atau berakhir")
			return store.Actor{}, false
		}
		actor, err := s.Store.ResolveActor(r.Context(), userID, "")
		if err != nil {
			writeError(w, err)
			return store.Actor{}, false
		}
		return actor, true
	}
	return s.requireActor(w, r)
}
