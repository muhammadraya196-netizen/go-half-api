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
		Username string `json:"username"`
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeMessage(w, http.StatusBadRequest, "Body JSON tidak valid")
		return
	}
	loginID := strings.TrimSpace(body.Login)
	if loginID == "" {
		loginID = strings.TrimSpace(body.Email)
	}
	if loginID == "" {
		loginID = strings.TrimSpace(body.Username)
	}
	if loginID == "" {
		writeMessage(w, http.StatusBadRequest, "Email atau username wajib diisi")
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
	user, err := s.Store.Login(r.Context(), loginID, body.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(user.MenuAccess) == 0 {
		writeMessage(w, http.StatusForbidden, "Akun belum memiliki akses menu")
		return
	}
	token := auth.IssueToken(s.TokenSecret, user.ID, 12*time.Hour)
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user": map[string]any{
			"id":         user.ID,
			"name":       user.Name,
			"email":      user.Email,
			"username":   user.Username,
			"role":       user.Role,
			"menuAccess": user.MenuAccess,
		},
	})
}
