package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"halfpos/internal/auth"
	"halfpos/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const dummyPasswordHash = "scrypt$0123456789abcdef0123456789abcdef$7bfc2ad8e90d7252b24c322d25a3ef314bb6f3b0ad29738a3140d921aa2606024e5f3f7eef2b45dfb16dd5428705bb539af7384b2504308175cc714be9c428bf"

func (s *Store) Login(ctx context.Context, loginID, password string) (domain.AuthUser, error) {
	loginID = strings.TrimSpace(loginID)
	var (
		id, name, email, username, role, status, passwordHash string
		menuAccess []string
	)
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, email, username, role, status, password_hash, COALESCE(menu_access, '{}')
		FROM users
		WHERE lower(email) = lower($1) OR lower(username) = lower($1)
		LIMIT 1
	`, loginID).Scan(&id, &name, &email, &username, &role, &status, &passwordHash, &menuAccess)
	if errors.Is(err, pgx.ErrNoRows) {
		auth.VerifyPassword(password, dummyPasswordHash)
		return domain.AuthUser{}, Unauthorized("Username atau password salah")
	}
	if err != nil {
		return domain.AuthUser{}, err
	}
	if !auth.VerifyPassword(password, passwordHash) {
		return domain.AuthUser{}, Unauthorized("Username atau password salah")
	}
	if status != "active" {
		return domain.AuthUser{}, Unauthorized("Akun tidak aktif")
	}
	menus := NormalizeMenuAccess(role, menuAccess)
	// Persist default jika kosong agar konsisten setelah login
	if len(menuAccess) == 0 {
		_, _ = s.pool.Exec(ctx, `UPDATE users SET menu_access = $2 WHERE id = $1`, id, menus)
	}
	if err := New(s.pool).TouchLastLogin(ctx, id); err != nil {
		return domain.AuthUser{}, err
	}
	return domain.AuthUser{
		ID: id, Name: name, Email: email, Username: username, Role: role, MenuAccess: menus,
	}, nil
}

func (s *Store) ListUsers(ctx context.Context, filter domain.UserFilter) (domain.UserPage, error) {
	limit := filter.Limit
	params := ListUsersParams{
		Search:          likePattern(filter.Search),
		Role:            optString(filter.Role),
		Status:          optString(filter.Status),
		StartAt:         tsArg(filter.Start),
		EndBefore:       tsArg(filter.EndBefore),
		CursorCreatedAt: tsArg(filter.CursorCreatedAt),
		CursorID:        blank(filter.CursorID),
		RowOffset:       filter.Offset,
		RowLimit:        &limit,
	}
	batch := s.pool.SendBatch(ctx, userBatch(params))
	defer batch.Close()
	rows, err := batch.Query()
	if err != nil {
		return domain.UserPage{}, err
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[ListUsersRow])
	if err != nil {
		return domain.UserPage{}, err
	}
	var total int64
	if err := batch.QueryRow().Scan(&total); err != nil {
		return domain.UserPage{}, err
	}
	out := make([]domain.ManagedUser, 0, len(items))
	ids := make([]string, 0, len(items))
	for _, row := range items {
		out = append(out, mapUserRow(row.ID, row.Name, row.Username, row.Email, row.Phone, row.Role, row.Status, row.Avatar, row.CreatedAt, row.LastLogin))
		ids = append(ids, row.ID)
	}
	if err := s.attachMenuAccess(ctx, out); err != nil {
		return domain.UserPage{}, err
	}
	_ = ids
	return domain.UserPage{Data: out, Total: total, Limit: limit, Offset: filter.Offset}, nil
}

func (s *Store) GetUser(ctx context.Context, id string, activityLimit int32, cursorAt *time.Time, cursorID string) (domain.UserDetail, error) {
	if activityLimit <= 0 {
		activityLimit = 100
	}
	row, err := New(s.pool).GetUserWithActivities(ctx, GetUserWithActivitiesParams{
		ActivityCursorAt: tsArg(cursorAt),
		ActivityCursorID: blank(cursorID),
		ActivityLimit:    activityLimit,
		ID:               id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.UserDetail{}, NotFound("User tidak ditemukan")
	}
	if err != nil {
		return domain.UserDetail{}, err
	}
	activities, err := decodeActivities(id, row.Activities)
	if err != nil {
		return domain.UserDetail{}, err
	}
	user := mapUserRow(row.ID, row.Name, row.Username, row.Email, row.Phone, row.Role, row.Status, row.Avatar, row.CreatedAt, row.LastLogin)
	list := []domain.ManagedUser{user}
	if err := s.attachMenuAccess(ctx, list); err != nil {
		return domain.UserDetail{}, err
	}
	return domain.UserDetail{
		User:       list[0],
		Activities: activities,
	}, nil
}

func (s *Store) CreateUser(ctx context.Context, in domain.CreateUserInput) (domain.ManagedUser, error) {
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return domain.ManagedUser{}, err
	}
	var created domain.ManagedUser
	err = s.tx(ctx, func(q *Queries) error {
		row, err := q.InsertUser(ctx, InsertUserParams{
			ID:           newID("user-"),
			Name:         in.Name,
			Username:     in.Username,
			Email:        in.Email,
			Phone:        blank(in.Phone),
			Role:         in.Role,
			Status:       in.Status,
			Avatar:       blank(in.Avatar),
			PasswordHash: hash,
		})
		if err != nil {
			return err
		}
		if err := q.InsertUserActivity(ctx, InsertUserActivityParams{
			ID:     newID("act-"),
			UserID: row.ID,
			Action: "Akun dibuat",
		}); err != nil {
			return err
		}
		menus := NormalizeMenuAccess(in.Role, in.MenuAccess)
		if _, err := q.db.Exec(ctx, `UPDATE users SET menu_access = $2 WHERE id = $1`, row.ID, menus); err != nil {
			return err
		}
		created = mapUserRow(row.ID, row.Name, row.Username, row.Email, row.Phone, row.Role, row.Status, row.Avatar, row.CreatedAt, row.LastLogin)
		created.MenuAccess = menus
		return nil
	})
	if mapped := mapUnique(err); mapped != nil {
		return domain.ManagedUser{}, mapped
	}
	return created, err
}

func (s *Store) UpdateUser(ctx context.Context, id string, in domain.UpdateUserInput, password string) (domain.ManagedUser, error) {
	var updated domain.ManagedUser
	err := s.tx(ctx, func(q *Queries) error {
		row, err := q.UpdateUser(ctx, UpdateUserParams{
			Name:     in.Name,
			Username: in.Username,
			Email:    in.Email,
			Phone:    blank(in.Phone),
			Role:     in.Role,
			Status:   in.Status,
			Avatar:   blank(in.Avatar),
			ID:       id,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return NotFound("User tidak ditemukan")
		}
		if err != nil {
			return err
		}
		if password != "" {
			hash, err := auth.HashPassword(password)
			if err != nil {
				return err
			}
			reset, err := q.UpdateUserPassword(ctx, UpdateUserPasswordParams{PasswordHash: hash, ID: id})
			if err != nil {
				return err
			}
			row = UpdateUserRow{
				ID: reset.ID, Name: reset.Name, Username: reset.Username, Email: reset.Email,
				Phone: reset.Phone, Role: reset.Role, Status: reset.Status, Avatar: reset.Avatar,
				CreatedAt: reset.CreatedAt, LastLogin: reset.LastLogin,
			}
			if err := q.InsertUserActivity(ctx, InsertUserActivityParams{
				ID: newID("act-"), UserID: id, Action: "Password direset",
			}); err != nil {
				return err
			}
		}
		if err := q.InsertUserActivity(ctx, InsertUserActivityParams{
			ID: newID("act-"), UserID: id, Action: "Data user diperbarui",
		}); err != nil {
			return err
		}
		menus := NormalizeMenuAccess(in.Role, in.MenuAccess)
		if _, err := q.db.Exec(ctx, `UPDATE users SET menu_access = $2 WHERE id = $1`, id, menus); err != nil {
			return err
		}
		updated = mapUserRow(row.ID, row.Name, row.Username, row.Email, row.Phone, row.Role, row.Status, row.Avatar, row.CreatedAt, row.LastLogin)
		updated.MenuAccess = menus
		return nil
	})
	if mapped := mapUnique(err); mapped != nil {
		return domain.ManagedUser{}, mapped
	}
	return updated, err
}

func (s *Store) ResetPassword(ctx context.Context, id, password string) (domain.ManagedUser, error) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return domain.ManagedUser{}, err
	}
	var updated domain.ManagedUser
	err = s.tx(ctx, func(q *Queries) error {
		row, err := q.UpdateUserPassword(ctx, UpdateUserPasswordParams{PasswordHash: hash, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return NotFound("User tidak ditemukan")
		}
		if err != nil {
			return err
		}
		if err := q.InsertUserActivity(ctx, InsertUserActivityParams{
			ID: newID("act-"), UserID: id, Action: "Password direset",
		}); err != nil {
			return err
		}
		updated = mapUserRow(row.ID, row.Name, row.Username, row.Email, row.Phone, row.Role, row.Status, row.Avatar, row.CreatedAt, row.LastLogin)
		return nil
	})
	return updated, err
}

func (s *Store) SetUserStatus(ctx context.Context, id, status string) (domain.ManagedUser, error) {
	action := "Akun dinonaktifkan"
	if status == "active" {
		action = "Akun diaktifkan"
	}
	var updated domain.ManagedUser
	err := s.tx(ctx, func(q *Queries) error {
		row, err := q.UpdateUserStatus(ctx, UpdateUserStatusParams{Status: status, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return NotFound("User tidak ditemukan")
		}
		if err != nil {
			return err
		}
		if err := q.InsertUserActivity(ctx, InsertUserActivityParams{
			ID: newID("act-"), UserID: id, Action: action,
		}); err != nil {
			return err
		}
		updated = mapUserRow(row.ID, row.Name, row.Username, row.Email, row.Phone, row.Role, row.Status, row.Avatar, row.CreatedAt, row.LastLogin)
		return nil
	})
	return updated, err
}

func (s *Store) UserStatus(ctx context.Context, id string) (string, error) {
	row, err := New(s.pool).GetUserStatus(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", NotFound("User tidak ditemukan")
	}
	if err != nil {
		return "", err
	}
	return row.Status, nil
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	n, err := New(s.pool).DeleteUser(ctx, id)
	if err != nil {
		if isForeignKey(err) {
			return Conflict("User tidak dapat dihapus karena masih memiliki transaksi atau catatan stok")
		}
		return err
	}
	if n == 0 {
		return NotFound("User tidak ditemukan")
	}
	return nil
}


func (s *Store) attachMenuAccess(ctx context.Context, users []domain.ManagedUser) error {
	if len(users) == 0 {
		return nil
	}
	ids := make([]string, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, role, COALESCE(menu_access, '{}') FROM users WHERE id = ANY($1)
	`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	byID := map[string][]string{}
	for rows.Next() {
		var id, role string
		var menus []string
		if err := rows.Scan(&id, &role, &menus); err != nil {
			return err
		}
		byID[id] = NormalizeMenuAccess(role, menus)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range users {
		if menus, ok := byID[users[i].ID]; ok {
			users[i].MenuAccess = menus
		} else {
			users[i].MenuAccess = DefaultMenuAccess(users[i].Role)
		}
	}
	return nil
}

func (s *Store) WriteAccessAudit(ctx context.Context, userID, actorID, actorName, action, detail string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_access_audits (id, user_id, actor_id, actor_name, action, detail)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, newID("uaa-"), userID, actorID, actorName, action, detail)
	return err
}

func mapUserRow(id, name, username, email string, phone *string, role, status string, avatar *string, created, last pgtype.Timestamptz) domain.ManagedUser {
	return domain.ManagedUser{
		ID: id, Name: name, Username: username, Email: email, Phone: phone,
		Role: role, Status: status, Avatar: avatar,
		CreatedAt: tsTime(created), LastLogin: tsPtr(last),
	}
}

func decodeActivities(userID, raw string) ([]domain.UserActivity, error) {
	var rows []struct {
		ID        string    `json:"id"`
		Action    string    `json:"action"`
		CreatedAt time.Time `json:"createdAt"`
	}
	if raw == "" {
		return []domain.UserActivity{}, nil
	}
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, err
	}
	out := make([]domain.UserActivity, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.UserActivity{
			ID: row.ID, UserID: userID, Action: row.Action, CreatedAt: row.CreatedAt,
		})
	}
	return out, nil
}
