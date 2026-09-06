package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/store"
)

// AdminService issues, rotates and resets admin credentials.
type AdminService struct {
	db *store.DB
}

// NewAdminService builds the admin credential service.
func NewAdminService(db *store.DB) *AdminService { return &AdminService{db: db} }

// Issue creates a new admin token, stores its hash and returns the plaintext.
// The plaintext is never persisted or logged.
func (a *AdminService) Issue(ctx context.Context, label string, now time.Time) (string, *domain.AdminToken, error) {
	token, err := NewAdminToken()
	if err != nil {
		return "", nil, err
	}
	rec := &domain.AdminToken{TokenHash: HashToken(token), Label: label, CreatedAt: now}
	if err := a.db.AdminTokens().Create(ctx, rec); err != nil {
		return "", nil, err
	}
	return token, rec, nil
}

// EnsureInitial issues a token only when no live token exists. It reports
// whether a token was created; the plaintext is empty when one already existed.
func (a *AdminService) EnsureInitial(ctx context.Context, label string, now time.Time) (string, bool, error) {
	live, err := a.db.AdminTokens().CountLive(ctx)
	if err != nil {
		return "", false, err
	}
	if live > 0 {
		return "", false, nil
	}
	token, _, err := a.Issue(ctx, label, now)
	if err != nil {
		return "", false, err
	}
	return token, true, nil
}

// Reset revokes every live admin token and issues a replacement. It is the
// offline lockout recovery path used by `atrium token reset`.
func (a *AdminService) Reset(ctx context.Context, label string, now time.Time) (string, error) {
	if _, err := a.db.AdminTokens().RevokeAll(ctx, now); err != nil {
		return "", err
	}
	token, _, err := a.Issue(ctx, label, now)
	if err != nil {
		return "", err
	}
	return token, nil
}

// Rotate issues a replacement token and revokes the one that authorized the
// call. The caller must write the response before the old token stops working.
func (a *AdminService) Rotate(ctx context.Context, currentID, label string, now time.Time) (string, error) {
	token, _, err := a.Issue(ctx, label, now)
	if err != nil {
		return "", err
	}
	if currentID != "" {
		if err := a.db.AdminTokens().Revoke(ctx, currentID, now); err != nil {
			return "", fmt.Errorf("auth: revoke previous admin token: %w", err)
		}
	}
	return token, nil
}

// RevokeToken revokes one admin credential by ID. The rotate route calls it
// after the response carrying the replacement has been written, so a dropped
// response can never leave the operator without a working credential.
func (a *AdminService) RevokeToken(ctx context.Context, id string, now time.Time) error {
	if id == "" {
		return nil
	}
	if err := a.db.AdminTokens().Revoke(ctx, id, now); err != nil {
		return fmt.Errorf("auth: revoke admin token: %w", err)
	}
	return nil
}
