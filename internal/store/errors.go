package store

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// Error adalah kesalahan yang sudah punya kode HTTP.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

func BadRequest(message string) error { return &Error{Status: 400, Message: message} }
func Unauthorized(message string) error {
	return &Error{Status: 401, Message: message}
}
func Forbidden(message string) error { return &Error{Status: 403, Message: message} }
func NotFound(message string) error { return &Error{Status: 404, Message: message} }
func Conflict(message string) error { return &Error{Status: 409, Message: message} }

func mapUnique(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return nil
	}
	switch {
	case strings.Contains(pgErr.ConstraintName, "email"):
		return Conflict("Email sudah digunakan")
	case strings.Contains(pgErr.ConstraintName, "username"):
		return Conflict("Username sudah digunakan")
	case strings.Contains(pgErr.ConstraintName, "sku"):
		return Conflict("SKU sudah digunakan")
	default:
		return Conflict("Data sudah digunakan")
	}
}

func isForeignKey(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
