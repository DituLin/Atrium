package store_test

import (
	"context"
	"database/sql"
	"errors"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"testing"
	"time"
)

func TestIntegrationRotationRollback(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDB(t)
	now := time.Now()
	svc := auth.NewIntegrationService(db, nil)
	p, token, err := svc.Issue(ctx, "brain", domain.IntegrationPolicy{}, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	_, old, err := db.Integrations().GetByTokenHash(ctx, auth.HashToken(token))
	if err != nil {
		t.Fatal(err)
	}
	replacement := *old
	replacement.ID = domain.NewID() // duplicate token hash fails after revocation UPDATE
	if err := db.Integrations().Rotate(ctx, p.ID, &replacement, now); err == nil {
		t.Fatal("duplicate credential accepted")
	}
	_, current, err := db.Integrations().GetByTokenHash(ctx, auth.HashToken(token))
	if err != nil || !current.Active(now) {
		t.Fatalf("rotation did not roll back: %v %v", current, err)
	}
	sentinel := errors.New("rollback")
	err = db.InWriteTx(ctx, func(tx *sql.Tx) error {
		got, e := db.Integrations().WithTx(tx).Get(ctx, p.ID)
		if e != nil {
			return e
		}
		if got.ID != p.ID {
			t.Fatal("wrong principal")
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	var persisted string
	if err = db.SQL().QueryRowContext(ctx, `SELECT token_hash FROM integration_credentials WHERE id=?`, old.ID).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted == token || persisted != auth.HashToken(token) {
		t.Fatal("raw or incorrect token persisted")
	}
}
