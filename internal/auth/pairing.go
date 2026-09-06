package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/store"
)

// PairingTTL is how long a code stays usable (design §6.8).
const PairingTTL = 5 * time.Minute

// PairingPollIntervalMS is the poll cadence the server suggests to the TV.
const PairingPollIntervalMS = 3000

// maxCodeAttempts bounds the search for a free six-digit code.
const maxCodeAttempts = 50

// PairingService implements the pairing state machine.
type PairingService struct {
	db  *store.DB
	now func() time.Time
}

// NewPairingService builds the pairing service.
func NewPairingService(db *store.DB, now func() time.Time) *PairingService {
	if now == nil {
		now = time.Now
	}
	return &PairingService{db: db, now: now}
}

// Start creates a pending pairing with a unique six-digit code.
func (p *PairingService) Start(ctx context.Context, clientHint, remoteIP string) (*domain.Pairing, error) {
	now := p.now()
	if _, err := p.db.Pairings().ExpireOverdue(ctx, now); err != nil {
		return nil, err
	}
	for attempt := 0; attempt < maxCodeAttempts; attempt++ {
		code, err := NewPairingCode()
		if err != nil {
			return nil, err
		}
		taken, err := p.db.Pairings().CodeExists(ctx, code)
		if err != nil {
			return nil, err
		}
		if taken {
			continue
		}
		pr := &domain.Pairing{
			Code:       code,
			Status:     domain.PairingPending,
			ClientHint: clientHint,
			RemoteIP:   remoteIP,
			CreatedAt:  now,
			ExpiresAt:  now.Add(PairingTTL),
		}
		err = p.db.Pairings().Create(ctx, pr)
		if errors.Is(err, domain.ErrConflict) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return pr, nil
	}
	return nil, fmt.Errorf("auth: no free pairing code after %d attempts", maxCodeAttempts)
}

// Get returns a pairing, marking it expired when its TTL has passed.
func (p *PairingService) Get(ctx context.Context, id string) (*domain.Pairing, error) {
	pr, err := p.db.Pairings().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return p.expireIfOverdue(ctx, pr)
}

// GetByCode returns a pairing by its six-digit code.
func (p *PairingService) GetByCode(ctx context.Context, code string) (*domain.Pairing, error) {
	pr, err := p.db.Pairings().GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	return p.expireIfOverdue(ctx, pr)
}

func (p *PairingService) expireIfOverdue(ctx context.Context, pr *domain.Pairing) (*domain.Pairing, error) {
	live := pr.Status == domain.PairingPending || pr.Status == domain.PairingApproved
	if live && !p.now().Before(pr.ExpiresAt) {
		if err := p.db.Pairings().SetStatus(ctx, pr.ID, domain.PairingExpired); err != nil {
			return nil, err
		}
		pr.Status = domain.PairingExpired
	}
	return pr, nil
}

// ApproveResult carries the credential produced by an approval.
type ApproveResult struct {
	Pairing *domain.Pairing
	Screen  *domain.Screen
}

// Approve registers the screen and binds it to the pairing. The screen token is
// created here but is only handed out at claim time.
func (p *PairingService) Approve(ctx context.Context, pairingID, screenID, name string) (*ApproveResult, error) {
	now := p.now()
	pr, err := p.Get(ctx, pairingID)
	if err != nil {
		return nil, err
	}
	switch pr.Status {
	case domain.PairingPending:
	case domain.PairingApproved:
		return nil, domain.Errorf(domain.CodeConflict, "pairing is already approved")
	case domain.PairingClaimed:
		return nil, domain.Errorf(domain.CodePairingClaimed, "pairing is already claimed")
	default:
		return nil, domain.Errorf(domain.CodePairingExpired, "pairing is %s", pr.Status)
	}
	if !domain.ValidSlug(screenID) {
		return nil, domain.Errorf(domain.CodeInvalidRequest, "screen id must match [a-z0-9_]+")
	}
	if name == "" {
		name = screenID
	}

	// A placeholder hash keeps the row unusable until the claim issues the real
	// credential, so no plaintext token is ever held between the two calls.
	placeholder, err := NewScreenToken()
	if err != nil {
		return nil, err
	}
	sc := &domain.Screen{
		ID: screenID, Name: name, TokenHash: HashToken(placeholder),
		Status: domain.ScreenActive, CreatedAt: now, ApprovedAt: now,
	}
	if err := p.db.Screens().Create(ctx, sc); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil, domain.Errorf(domain.CodeConflict, "screen %q already exists", screenID)
		}
		return nil, err
	}
	if err := p.db.Pairings().Approve(ctx, pr.ID, screenID, now); err != nil {
		return nil, err
	}
	pr.Status = domain.PairingApproved
	pr.ApprovedScreenID = screenID
	pr.ApprovedAt = &now
	return &ApproveResult{Pairing: pr, Screen: sc}, nil
}

// ClaimResult is the credential handed to the screen exactly once.
type ClaimResult struct {
	Screen *domain.Screen
	Token  string
}

// Claim issues the screen credential for an approved pairing. A second claim
// fails with pairing_claimed so a replayed request cannot mint a new token.
func (p *PairingService) Claim(ctx context.Context, pairingID string) (*ClaimResult, error) {
	now := p.now()
	pr, err := p.Get(ctx, pairingID)
	if err != nil {
		return nil, err
	}
	switch pr.Status {
	case domain.PairingApproved:
	case domain.PairingClaimed:
		return nil, domain.Errorf(domain.CodePairingClaimed, "pairing was already claimed")
	case domain.PairingPending:
		return nil, domain.Errorf(domain.CodeConflict, "pairing is not approved yet")
	default:
		return nil, domain.Errorf(domain.CodePairingExpired, "pairing is %s", pr.Status)
	}
	// Claim first: the UPDATE is conditional on the approved state, so two
	// concurrent claims cannot both issue a credential.
	if err := p.db.Pairings().Claim(ctx, pr.ID, now); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.Errorf(domain.CodePairingClaimed, "pairing was already claimed")
		}
		return nil, err
	}
	sc, err := p.db.Screens().Get(ctx, pr.ApprovedScreenID)
	if err != nil {
		return nil, err
	}
	token, err := NewScreenToken()
	if err != nil {
		return nil, err
	}
	if err := p.db.Screens().SetTokenHash(ctx, sc.ID, HashToken(token)); err != nil {
		return nil, err
	}
	sc.TokenHash = HashToken(token)
	return &ClaimResult{Screen: sc, Token: token}, nil
}

// Reject deletes a pairing.
func (p *PairingService) Reject(ctx context.Context, id string) error {
	return p.db.Pairings().Delete(ctx, id)
}

// List returns recent pairings.
func (p *PairingService) List(ctx context.Context, limit int) ([]domain.Pairing, error) {
	if _, err := p.db.Pairings().ExpireOverdue(ctx, p.now()); err != nil {
		return nil, err
	}
	return p.db.Pairings().List(ctx, limit)
}

// PruneOlderThan applies the one-day pairing retention rule.
func (p *PairingService) PruneOlderThan(ctx context.Context, age time.Duration) (int64, error) {
	return p.db.Pairings().DeleteOlderThan(ctx, p.now().Add(-age))
}

// NewPairingCode returns a uniformly random six-digit code.
func NewPairingCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", fmt.Errorf("auth: generate pairing code: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
