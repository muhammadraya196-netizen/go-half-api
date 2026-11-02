package httpapi

import (
	"net/http"
	"strings"
	"time"

	"halfpos/internal/domain"
)

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireUsersViewer(w, r); !ok {
		return
	}
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
	filter := domain.UserFilter{
		Search: r.URL.Query().Get("search"), Role: r.URL.Query().Get("role"),
		Status: r.URL.Query().Get("status"), Limit: limit, Offset: offset,
		CursorCreatedAt: cursorAt, CursorID: cursorID,
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
	page, err := s.Store.ListUsers(r.Context(), filter)
	if err != nil {
		writeError(w, err)
		return
	}
	data := make([]userJSON, 0, len(page.Data))
	for _, user := range page.Data {
		data = append(data, userToJSON(user))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": data, "total": page.Total, "limit": page.Limit, "offset": page.Offset,
	})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAdminActor(w, r)
	if !ok {
		return
	}
	var body userBody
	if err := decodeJSON(r, &body); err != nil {
		writeMessage(w, http.StatusBadRequest, "Body JSON tidak valid")
		return
	}
	in, msg := validateCreateUser(body)
	if msg != "" {
		writeMessage(w, http.StatusBadRequest, msg)
		return
	}
	user, err := s.Store.CreateUser(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = s.Store.WriteAccessAudit(r.Context(), user.ID, actor.ID, actor.Name, "create_user", "role="+user.Role)
	writeJSON(w, http.StatusCreated, userToJSON(user))
}

type userBody struct {
	Name            string   `json:"name"`
	Username        string   `json:"username"`
	Email           string   `json:"email"`
	Phone           string   `json:"phone"`
	Password        string   `json:"password"`
	ConfirmPassword string   `json:"confirmPassword"`
	Role            string   `json:"role"`
	Status          string   `json:"status"`
	Avatar          string   `json:"avatar"`
	MenuAccess      []string `json:"menuAccess"`
}

func validateCreateUser(body userBody) (domain.CreateUserInput, string) {
	in, msg := validateUserFields(body, true)
	if msg != "" {
		return domain.CreateUserInput{}, msg
	}
	if len(body.Password) < 8 {
		return domain.CreateUserInput{}, "Password minimal 8 karakter"
	}
	if body.ConfirmPassword == "" {
		return domain.CreateUserInput{}, "Konfirmasi password wajib diisi"
	}
	if body.Password != body.ConfirmPassword {
		return domain.CreateUserInput{}, "Password dan konfirmasi tidak sama"
	}
	return domain.CreateUserInput{
		Name: in.Name, Username: in.Username, Email: in.Email, Phone: body.Phone,
		Role: in.Role, Status: in.Status, Avatar: body.Avatar, Password: body.Password,
		MenuAccess: body.MenuAccess,
	}, ""
}

func validateUserFields(body userBody, creating bool) (domain.UpdateUserInput, string) {
	name, msg := requireText(body.Name, "Nama wajib diisi", 100)
	if msg != "" {
		return domain.UpdateUserInput{}, msg
	}
	username := strings.TrimSpace(body.Username)
	if !usernameRE.MatchString(username) {
		if len(username) < 3 {
			return domain.UpdateUserInput{}, "Username minimal 3 karakter"
		}
		return domain.UpdateUserInput{}, "Username hanya huruf, angka, . _ -"
	}
	email := strings.TrimSpace(body.Email)
	if email == "" {
		return domain.UpdateUserInput{}, "Email wajib diisi"
	}
	if !emailOK(email) {
		return domain.UpdateUserInput{}, "Format email tidak valid"
	}
	if body.Phone != "" && !phoneRE.MatchString(body.Phone) {
		return domain.UpdateUserInput{}, "Nomor HP tidak valid"
	}
	if _, ok := roles[body.Role]; !ok {
		return domain.UpdateUserInput{}, "Role wajib dipilih"
	}
	if _, ok := userStatuses[body.Status]; !ok {
		return domain.UpdateUserInput{}, "Status wajib dipilih"
	}
	_ = creating
	return domain.UpdateUserInput{
		Name: name, Username: username, Email: email, Phone: body.Phone,
		Role: body.Role, Status: body.Status, Avatar: body.Avatar,
		MenuAccess: body.MenuAccess,
	}, ""
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireUsersViewer(w, r); !ok {
		return
	}
	limit := int32(100)
	if n, ok, err := queryNumber(r.URL.Query().Get("activityLimit")); err != nil {
		writeMessage(w, http.StatusBadRequest, err.Error())
		return
	} else if ok && n > 0 && n <= 500 {
		limit = int32(n)
	}
	cursorAt, cursorID, err := activityCursor(r)
	if err != nil {
		writeMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	detail, err := s.Store.GetUser(r.Context(), chiID(r), limit, cursorAt, cursorID)
	if err != nil {
		writeError(w, err)
		return
	}
	activities := make([]activityJSON, 0, len(detail.Activities))
	for _, activity := range detail.Activities {
		activities = append(activities, activityJSON{
			ID: activity.ID, UserID: activity.UserID, Action: activity.Action, CreatedAt: iso(activity.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": userToJSON(detail.User), "activities": activities})
}

func activityCursor(r *http.Request) (*time.Time, string, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("activityCursorCreatedAt"))
	id := strings.TrimSpace(r.URL.Query().Get("activityCursorId"))
	if raw == "" && id == "" {
		return nil, "", nil
	}
	if raw == "" || id == "" {
		return nil, "", errCursorPair
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, "", errCursorTime
	}
	return &parsed, id, nil
}

var (
	errCursorPair = cursorError("activityCursorCreatedAt dan activityCursorId harus diisi bersamaan")
	errCursorTime = cursorError("activityCursorCreatedAt tidak valid")
)

type cursorError string

func (e cursorError) Error() string { return string(e) }

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAdminActor(w, r)
	if !ok {
		return
	}
	var body userBody
	if err := decodeJSON(r, &body); err != nil {
		writeMessage(w, http.StatusBadRequest, "Body JSON tidak valid")
		return
	}
	in, msg := validateUserFields(body, false)
	if msg != "" {
		writeMessage(w, http.StatusBadRequest, msg)
		return
	}
	password := body.Password
	if password != "" || body.ConfirmPassword != "" {
		if len(password) < 8 {
			writeMessage(w, http.StatusBadRequest, "Password minimal 8 karakter")
			return
		}
		if password != body.ConfirmPassword {
			writeMessage(w, http.StatusBadRequest, "Password dan konfirmasi tidak sama")
			return
		}
	}
	id := chiID(r)
	if id == actor.ID {
		writeMessage(w, http.StatusForbidden, "Tidak dapat mengubah role atau hak akses akun sendiri")
		return
	}
	user, err := s.Store.UpdateUser(r.Context(), id, in, password)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = s.Store.WriteAccessAudit(r.Context(), user.ID, actor.ID, actor.Name, "update_user", "role="+user.Role)
	writeJSON(w, http.StatusOK, userToJSON(user))
}

func (s *Server) patchUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAdminActor(w, r)
	if !ok {
		return
	}
	var body struct {
		Action   string `json:"action"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeMessage(w, http.StatusBadRequest, "Body JSON tidak valid")
		return
	}
	id := chiID(r)
	if id == actor.ID && body.Action == "toggle-status" {
		writeMessage(w, http.StatusForbidden, "Tidak dapat menonaktifkan akun sendiri")
		return
	}
	switch body.Action {
	case "reset-password":
		if len(body.Password) < 8 {
			writeMessage(w, http.StatusBadRequest, "Password minimal 8 karakter")
			return
		}
		user, err := s.Store.ResetPassword(r.Context(), id, body.Password)
		if err != nil {
			writeError(w, err)
			return
		}
		_ = s.Store.WriteAccessAudit(r.Context(), user.ID, actor.ID, actor.Name, "reset_password", "")
		writeJSON(w, http.StatusOK, userToJSON(user))
	case "toggle-status":
		current, err := s.Store.UserStatus(r.Context(), id)
		if err != nil {
			writeError(w, err)
			return
		}
		next := "active"
		if current == "active" {
			next = "inactive"
		}
		user, err := s.Store.SetUserStatus(r.Context(), id, next)
		if err != nil {
			writeError(w, err)
			return
		}
		_ = s.Store.WriteAccessAudit(r.Context(), user.ID, actor.ID, actor.Name, "toggle_status", "status="+next)
		writeJSON(w, http.StatusOK, userToJSON(user))
	default:
		writeMessage(w, http.StatusBadRequest, "Aksi tidak dikenal")
	}
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAdminActor(w, r)
	if !ok {
		return
	}
	id := chiID(r)
	if id == actor.ID {
		writeMessage(w, http.StatusForbidden, "Tidak dapat menghapus akun sendiri")
		return
	}
	if err := s.Store.DeleteUser(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	_ = s.Store.WriteAccessAudit(r.Context(), id, actor.ID, actor.Name, "delete_user", "")
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}
