package auth

import (
	"context"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"strings"
	"time"
)

// IntegrationService manages least-privilege service identities. Raw tokens are
// returned exactly once to the caller and are never persisted by this service.
type IntegrationService struct {
	db  *store.DB
	now func() time.Time
}

// NewIntegrationService constructs the service with an injectable clock.
func NewIntegrationService(db *store.DB, now func() time.Time) *IntegrationService {
	if now == nil {
		now = time.Now
	}
	return &IntegrationService{db: db, now: now}
}

// Issue creates a principal and returns its initial raw credential.
func (s *IntegrationService) Issue(ctx context.Context, label string, policy domain.IntegrationPolicy, expiresAt time.Time) (*domain.IntegrationPrincipal, string, error) {
	now := s.now()
	if strings.TrimSpace(label) == "" || len(label) > 128 || !expiresAt.After(now) {
		return nil, "", domain.Errorf(domain.CodeInvalidRequest, "a label and future expiry are required")
	}
	if err := policy.Validate(); err != nil {
		return nil, "", err
	}
	token, err := NewIntegrationToken()
	if err != nil {
		return nil, "", err
	}
	p := &domain.IntegrationPrincipal{ID: domain.NewID(), Label: label, Enabled: true, ExpiresAt: expiresAt, Policy: policy, PolicyVersion: 1, CreatedAt: now, UpdatedAt: now}
	c := &domain.IntegrationCredential{ID: domain.NewID(), PrincipalID: p.ID, TokenHash: HashToken(token), CreatedAt: now, ExpiresAt: expiresAt}
	if err := s.db.Integrations().Create(ctx, p, c); err != nil {
		return nil, "", err
	}
	return p, token, nil
}

// Rotate replaces the credential without extending the principal lifetime.
func (s *IntegrationService) Rotate(ctx context.Context, id string, expiresAt time.Time) (*domain.IntegrationPrincipal, string, error) {
	now := s.now()
	if !expiresAt.After(now) {
		return nil, "", domain.Errorf(domain.CodeInvalidRequest, "future expiry required")
	}
	token, err := NewIntegrationToken()
	if err != nil {
		return nil, "", err
	}
	c := &domain.IntegrationCredential{ID: domain.NewID(), PrincipalID: id, TokenHash: HashToken(token), CreatedAt: now, ExpiresAt: expiresAt}
	if err = s.db.Integrations().Rotate(ctx, id, c, now); err != nil {
		return nil, "", err
	}
	p, err := s.db.Integrations().Get(ctx, id)
	if err != nil {
		return nil, "", err
	}
	return p, token, nil
}

// Revoke immediately disables the principal and its credentials.
func (s *IntegrationService) Revoke(ctx context.Context, id string) error {
	return s.db.Integrations().Revoke(ctx, id, s.now())
}

// UpdatePolicy replaces all allowlists and increments the policy version.
func (s *IntegrationService) UpdatePolicy(ctx context.Context, id string, policy domain.IntegrationPolicy) (*domain.IntegrationPrincipal, error) {
	if err := s.db.Integrations().UpdatePolicy(ctx, id, policy, s.now()); err != nil {
		return nil, err
	}
	return s.db.Integrations().Get(ctx, id)
}

// List returns principals without credential secrets.
func (s *IntegrationService) List(ctx context.Context) ([]domain.IntegrationPrincipal, error) {
	return s.db.Integrations().List(ctx)
}
