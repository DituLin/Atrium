package store

import (
	"database/sql"
	"strings"
	"time"
)

// timeLayout is the canonical on-disk timestamp format: UTC RFC 3339 with
// nanosecond precision so string ordering matches chronological ordering.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

// FormatTime renders t for storage.
func FormatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

// FormatTimePtr renders an optional timestamp.
func FormatTimePtr(t *time.Time) sql.NullString {
	if t == nil || t.IsZero() {
		return sql.NullString{}
	}
	return sql.NullString{String: FormatTime(*t), Valid: true}
}

// ParseTime parses a stored timestamp, accepting plain RFC 3339 too.
func ParseTime(s string) (time.Time, error) {
	if t, err := time.Parse(timeLayout, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

// timePtr converts a nullable stored timestamp.
func timePtr(ns sql.NullString) *time.Time {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	t, err := ParseTime(ns.String)
	if err != nil {
		return nil
	}
	return &t
}

// timeVal converts a non-null stored timestamp, zero on failure.
func timeVal(s string) time.Time {
	t, err := ParseTime(s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func int64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	out := v.Int64
	return &out
}

func nullIntPtr(v *int) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*v), Valid: true}
}

func intPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	out := int(v.Int64)
	return &out
}

// nullZeroInt stores 0 as NULL for optional integer columns.
func nullZeroInt(v int) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(v), Valid: true}
}

// boolInt renders a bool as SQLite's 0/1.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// isUniqueViolation reports whether err is a SQLite UNIQUE constraint failure.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed") ||
		strings.Contains(err.Error(), "constraint failed: UNIQUE")
}
