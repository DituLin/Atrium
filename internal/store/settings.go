package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/DituLin/Atritum/internal/domain"
)

// Well-known settings keys.
const (
	SettingHomeTimezone  = "home_timezone"
	SettingSchemaCreated = "schema_created_at"
)

// Settings reads and writes the key/value settings table.
type Settings struct{ db *DB }

// Settings returns the settings repository.
func (d *DB) Settings() *Settings { return &Settings{db: d} }

// Get returns the value for key, or domain.ErrNotFound.
func (s *Settings) Get(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.sql.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("store: get setting %q: %w", key, err)
	}
	return v, nil
}

// GetDefault returns the value for key or def when absent.
func (s *Settings) GetDefault(ctx context.Context, key, def string) (string, error) {
	v, err := s.Get(ctx, key)
	if errors.Is(err, domain.ErrNotFound) {
		return def, nil
	}
	return v, err
}

// Set upserts a value.
func (s *Settings) Set(ctx context.Context, key, value string) error {
	_, err := s.db.sql.ExecContext(ctx,
		`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, FormatTime(time.Now()))
	if err != nil {
		return fmt.Errorf("store: set setting %q: %w", key, err)
	}
	return nil
}

// GetInt returns an integer setting, or def when absent or unparsable.
func (s *Settings) GetInt(ctx context.Context, key string, def int64) (int64, error) {
	v, err := s.Get(ctx, key)
	if errors.Is(err, domain.ErrNotFound) {
		return def, nil
	}
	if err != nil {
		return def, err
	}
	n, convErr := strconv.ParseInt(v, 10, 64)
	if convErr != nil {
		return def, nil
	}
	return n, nil
}

// SetInt writes an integer setting.
func (s *Settings) SetInt(ctx context.Context, key string, v int64) error {
	return s.Set(ctx, key, strconv.FormatInt(v, 10))
}
