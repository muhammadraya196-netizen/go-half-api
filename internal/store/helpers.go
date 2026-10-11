package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func newID(prefix string) string {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	return prefix + strconv.FormatInt(time.Now().UnixMilli(), 10) + "-" + hex.EncodeToString(buf[:])
}

func blank(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func likePattern(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
	pattern := "%" + escaped + "%"
	return &pattern
}

func optString(value string) *string {
	if value == "" || value == "all" {
		return nil
	}
	return &value
}

func tsArg(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

func tsTime(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}

func tsPtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

func jakartaLocation() (*time.Location, error) {
	return time.LoadLocation("Asia/Jakarta")
}

// DayBounds mengubah tanggal YYYY-MM-DD menjadi rentang [awal, akhir) di Asia/Jakarta.
func DayBounds(date string) (time.Time, time.Time, error) {
	loc, err := jakartaLocation()
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("tanggal tidak valid")
	}
	return start, start.AddDate(0, 0, 1), nil
}

func dailyPrefix(kind string, now time.Time) (string, error) {
	loc, err := jakartaLocation()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-", kind, now.In(loc).Format("20060102")), nil
}

func documentNumbers(prefix string, end int32, count int) []string {
	start := int(end) - count + 1
	out := make([]string, count)
	for i := 0; i < count; i++ {
		out[i] = fmt.Sprintf("%s%03d", prefix, start+i)
	}
	return out
}

var categoryIDs = map[string]string{
	"Cake":           "cake",
	"Bread":          "bread",
	"Pastry":         "pastry",
	"Cookies":        "cookies",
	"Dessert":        "dessert",
	"Coffee & Drink": "coffee-drink",
}

func categoryKey(name string) string {
	if id, ok := categoryIDs[name]; ok {
		return id
	}
	return name
}

func parseRating(value any) *float64 {
	var raw string
	switch typed := value.(type) {
	case string:
		raw = typed
	case []byte:
		raw = string(typed)
	default:
		return nil
	}
	if raw == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}
	return &parsed
}
