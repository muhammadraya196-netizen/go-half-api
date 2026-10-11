package httpapi

import (
	"net/http"
	"strings"
	"time"

	"halfpos/internal/auth"
)

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeMessage(w, http.StatusBadRequest, "Body JSON tidak valid")
		return
	}
	email := strings.TrimSpace(body.Email)
	if email == "" {
		writeMessage(w, http.StatusBadRequest, "Email wajib diisi")
		return
	}
	if !emailOK(email) {
		writeMessage(w, http.StatusBadRequest, "Format email tidak valid")
		return
	}
	if body.Password == "" {
		writeMessage(w, http.StatusBadRequest, "Password wajib diisi")
		return
	}
	if len(body.Password) < 6 {
		writeMessage(w, http.StatusBadRequest, "Password minimal 6 karakter")
		return
	}
	user, err := s.Store.Login(r.Context(), email, body.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	token, err := auth.Sign(s.Secret, user.ID, user.Role, time.Now().Add(s.TokenTTL))
	if err != nil {
		writeMessage(w, http.StatusInternalServerError, "Terjadi kesalahan server")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": map[string]string{
			"id":    user.ID,
			"name":  user.Name,
			"email": user.Email,
			"role":  user.Role,
		},
		"token": token,
	})
}
