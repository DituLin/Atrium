package auth

import (
	"context"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIntegrationLifecycle(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDB(t)
	now := time.Now().UTC()
	svc := NewIntegrationService(db, func() time.Time { return now })
	p, token, err := svc.Issue(ctx, "brain", domain.IntegrationPolicy{Permissions: []string{"home.read"}}, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	authenticate := func(token string, scope Scope) (*Identity, error) {
		r := httptest.NewRequest("GET", "https://core/", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		return NewAuthenticator(db, NewOriginPolicy(nil), func() time.Time { return now }).Authenticate(ctx, r, scope)
	}
	id, err := authenticate(token, ScopeIntegration)
	if err != nil || id.Integration.ID != p.ID {
		t.Fatalf("authenticate: %v %v", id, err)
	}
	for _, scope := range []Scope{ScopeNone, ScopeAdmin, ScopeScreen} {
		if _, err := authenticate(token, scope); err == nil {
			t.Fatalf("integration escaped into %s", scope)
		}
	}
	p2, token2, err := svc.Rotate(ctx, p.ID, now.Add(time.Hour))
	if err != nil || p2.ID != p.ID {
		t.Fatalf("rotate: %v", err)
	}
	if _, err := authenticate(token, ScopeIntegration); err == nil {
		t.Fatal("old token accepted")
	}
	if _, err := authenticate(token2, ScopeIntegration); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdatePolicy(ctx, p.ID, domain.IntegrationPolicy{}); err != nil {
		t.Fatal(err)
	}
	id, err = authenticate(token2, ScopeIntegration)
	if err != nil || id.Integration.Policy.Allows("home.read") || id.Integration.PolicyVersion != 2 {
		t.Fatalf("policy not current: %v %v", id, err)
	}
	now = now.Add(2 * time.Hour)
	if _, err := authenticate(token2, ScopeIntegration); err == nil {
		t.Fatal("expired token accepted")
	}
	now = now.Add(-2 * time.Hour)
	if err := svc.Revoke(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := authenticate(token2, ScopeIntegration); err == nil {
		t.Fatal("revoked principal accepted")
	}
	if _, _, err := svc.Rotate(ctx, p.ID, now.Add(time.Hour)); err == nil {
		t.Fatal("revoked principal resurrected")
	}
}
func TestIntegrationPolicyValidation(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDB(t)
	now := time.Now()
	svc := NewIntegrationService(db, nil)
	for _, p := range []domain.IntegrationPolicy{{Permissions: []string{"admin"}}, {Permissions: []string{"home.read", "home.read"}}, {SourceIDs: []string{"*"}}, {ScreenIDs: []string{"missing"}}} {
		if _, _, err := svc.Issue(ctx, "brain", p, now.Add(time.Hour)); err == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
	if _, _, err := svc.Issue(ctx, "brain", domain.IntegrationPolicy{}, now.Add(-time.Hour)); err == nil {
		t.Fatal("accepted past expiry")
	}
}

func TestIntegrationCredentialAndPrincipalChecks(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDB(t)
	now := time.Now()
	svc := NewIntegrationService(db, func() time.Time { return now })
	p, _, err := svc.Issue(ctx, "brain", domain.IntegrationPolicy{}, now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := svc.Rotate(ctx, p.ID, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	check := func() error {
		r := httptest.NewRequest("GET", "https://core/", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		_, err := NewAuthenticator(db, NewOriginPolicy(nil), func() time.Time { return now }).Authenticate(ctx, r, ScopeIntegration)
		return err
	}
	now = now.Add(time.Minute)
	if err := check(); err == nil {
		t.Fatal("credential expiration boundary accepted")
	}
	now = now.Add(-time.Minute)
	if _, err := db.SQL().ExecContext(ctx, `UPDATE integration_principals SET enabled=0 WHERE id=?`, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := check(); err == nil {
		t.Fatal("disabled principal accepted")
	}
	if _, err := db.SQL().ExecContext(ctx, `UPDATE integration_principals SET enabled=1 WHERE id=?`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `UPDATE integration_credentials SET revoked_at=? WHERE token_hash=?`, now.Format(time.RFC3339Nano), HashToken(token)); err != nil {
		t.Fatal(err)
	}
	if err := check(); err == nil {
		t.Fatal("revoked credential accepted")
	}
}

func TestIntegrationCannotAuthenticateWebSocket(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDB(t)
	now := time.Now()
	_, token, err := NewIntegrationService(db, nil).Issue(ctx, "brain", domain.IntegrationPolicy{Permissions: []string{"screens.control"}}, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"", "https://core"} {
		t.Run(origin, func(t *testing.T) {
			r := httptest.NewRequest("GET", "https://core/ws", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			if origin != "" {
				r.Header.Set("Origin", origin)
			}
			id, err := NewAuthenticator(db, NewOriginPolicy([]string{"https://core"}), nil).AuthenticateWebSocket(ctx, r)
			if err == nil || id != nil {
				t.Fatalf("integration admitted to screen websocket: %v %v", id, err)
			}
		})
	}
}

func TestIntegrationResourcePolicyValidationIsAtomic(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDB(t)
	now := time.Now()
	svc := NewIntegrationService(db, nil)
	if err := db.Sources().Upsert(ctx, "photos", "Photos", "/fixture/photos", now); err != nil {
		t.Fatal(err)
	}
	if err := db.Screens().Create(ctx, &domain.Screen{ID: "tv", Name: "TV", TokenHash: "fixture-hash"}); err != nil {
		t.Fatal(err)
	}
	policy := domain.IntegrationPolicy{Permissions: []string{"photos.read", "screens.control"}, SourceIDs: []string{"photos"}, ScreenIDs: []string{"tv"}}
	p, _, err := svc.Issue(ctx, "brain", policy, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("active resources rejected: %v", err)
	}
	assertUnchanged := func() {
		t.Helper()
		got, err := db.Integrations().Get(ctx, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.PolicyVersion != 1 || !got.Policy.Allows("photos.read") || !got.Policy.Allows("screens.control") || !got.Policy.AllowsSource("photos") || !got.Policy.AllowsScreen("tv") || got.Policy.Allows("home.read") {
			t.Fatalf("rejected update changed policy: %+v", got)
		}
	}
	if err := db.Sources().Revoke(ctx, "photos", "test", now); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdatePolicy(ctx, p.ID, domain.IntegrationPolicy{Permissions: []string{"home.read"}, SourceIDs: []string{"photos"}}); err == nil {
		t.Fatal("revoked source accepted")
	}
	assertUnchanged()
	if err := db.Screens().Revoke(ctx, "tv", now); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdatePolicy(ctx, p.ID, domain.IntegrationPolicy{Permissions: []string{"home.read"}, ScreenIDs: []string{"tv"}}); err == nil {
		t.Fatal("revoked screen accepted")
	}
	assertUnchanged()
}
