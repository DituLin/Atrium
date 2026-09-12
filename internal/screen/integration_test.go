package screen_test

import (
	"context"
	"database/sql"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/screen"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func integrationFixture(t *testing.T) (*fixture, string) {
	f := newFixture(t)
	now := f.clock.Now()
	p := &domain.IntegrationPrincipal{ID: "brain", PolicyVersion: 1, Label: "Brain", Enabled: true, ExpiresAt: now.Add(30 * 24 * time.Hour), Policy: domain.IntegrationPolicy{Permissions: []string{"screens.control", "commands.read"}, ScreenIDs: []string{"living_room_tv"}, SourceIDs: []string{"family_photos"}}, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, f.db.Integrations().Create(t.Context(), p, &domain.IntegrationCredential{ID: "credential", TokenHash: "secret-hash", PrincipalID: p.ID, CreatedAt: now, ExpiresAt: p.ExpiresAt}))
	return f, p.ID
}
func TestIntegrationConcurrentOperation(t *testing.T) {
	f, p := integrationFixture(t)
	op := domain.NewIDAt(f.clock.Now())
	var wg sync.WaitGroup
	cmds := make([]*domain.Command, 20)
	errs := make([]error, 20)
	for i := range cmds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmds[i], errs[i] = f.svc.IssueIntegration(t.Context(), p, op, "living_room_tv", domain.CommandRefresh, domain.CommandPayload{})
		}()
	}
	wg.Wait()
	for i := range cmds {
		require.NoError(t, errs[i])
		require.Equal(t, cmds[0].ID, cmds[i].ID)
		require.EqualValues(t, 1, cmds[i].Sequence)
	}
	require.Equal(t, 1, f.hub.count())
	var operationCount, commandCount, maxSequence int
	require.NoError(t, f.db.SQL().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM integration_operations").Scan(&operationCount))
	require.NoError(t, f.db.SQL().QueryRowContext(t.Context(), "SELECT COUNT(*),MAX(sequence) FROM screen_commands").Scan(&commandCount, &maxSequence))
	require.Equal(t, 1, operationCount)
	require.Equal(t, 1, commandCount)
	require.Equal(t, 1, maxSequence)
	_, err := f.svc.IssueIntegration(t.Context(), p, op, "living_room_tv", domain.CommandNavigate, domain.CommandPayload{Route: domain.RouteDashboard})
	require.Error(t, err)
}

func TestIntegrationOfflineAndRestartRecovery(t *testing.T) {
	f, p := integrationFixture(t)
	op := domain.NewIDAt(f.clock.Now())
	f.hub.online = false
	a, err := f.svc.IssueIntegration(t.Context(), p, op, "living_room_tv", domain.CommandRefresh, domain.CommandPayload{})
	require.Error(t, err)
	f.hub.online = true
	b, err := f.svc.IssueIntegration(t.Context(), p, op, "living_room_tv", domain.CommandRefresh, domain.CommandPayload{})
	require.Error(t, err)
	require.Equal(t, a.ID, b.ID)
	require.Equal(t, domain.CommandFailed, b.Status)
	require.Equal(t, 0, f.hub.count())
	op = domain.NewIDAt(f.clock.Now())
	a, err = f.svc.IssueIntegration(t.Context(), p, op, "living_room_tv", domain.CommandRefresh, domain.CommandPayload{})
	require.NoError(t, err)
	_, err = f.db.Commands().MarkAllAcceptedUnknown(t.Context(), "server_restart", f.clock.Now())
	require.NoError(t, err)
	b, err = f.svc.IssueIntegration(t.Context(), p, op, "living_room_tv", domain.CommandRefresh, domain.CommandPayload{})
	require.NoError(t, err)
	require.Equal(t, a.ID, b.ID)
	require.Equal(t, domain.CommandUnknown, b.Status)
	require.Equal(t, 1, f.hub.count())
	got, err := f.svc.GetIntegrationOperation(t.Context(), p, op)
	require.NoError(t, err)
	require.Equal(t, domain.CommandUnknown, got.Status)
	_, err = f.svc.GetIntegrationCommand(t.Context(), "other", a.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
}
func TestIntegrationPolicyAndAge(t *testing.T) {
	f, p := integrationFixture(t)
	ctx := t.Context()
	op := domain.NewIDAt(f.clock.Now())
	photo := f.readyPhoto(t, "photo.jpg", domain.PreviewReady)
	a, err := f.svc.IssueIntegration(ctx, p, op, "living_room_tv", domain.CommandShow, domain.CommandPayload{PhotoID: photo})
	require.NoError(t, err)
	_, err = f.db.Commands().Resolve(ctx, a.ID, domain.CommandApplied, "", nil, f.clock.Now())
	require.NoError(t, err)
	got, err := f.svc.GetIntegrationOperation(ctx, p, op)
	require.NoError(t, err)
	require.Equal(t, domain.CommandApplied, got.Status)
	f.clock.Advance(25 * time.Hour)
	got, err = f.svc.IssueIntegration(ctx, p, op, "living_room_tv", domain.CommandShow, domain.CommandPayload{PhotoID: photo})
	require.NoError(t, err)
	require.Equal(t, a.ID, got.ID)
	for _, at := range []time.Time{f.clock.Now().Add(-25 * time.Hour), f.clock.Now().Add(6 * time.Minute)} {
		_, err = f.svc.IssueIntegration(ctx, p, domain.NewIDAt(at), "living_room_tv", domain.CommandRefresh, domain.CommandPayload{})
		require.Error(t, err)
		de, ok := domain.AsError(err)
		require.True(t, ok)
		require.Equal(t, domain.CodeOperationExpired, de.Code)
	}
	principal, err := f.db.Integrations().Get(ctx, p)
	require.NoError(t, err)
	principal.Policy.SourceIDs = nil
	require.NoError(t, f.db.Integrations().UpdatePolicy(ctx, p, principal.Policy, f.clock.Now()))
	_, err = f.svc.IssueIntegration(ctx, p, op, "living_room_tv", domain.CommandShow, domain.CommandPayload{PhotoID: photo})
	require.Error(t, err)
	_, err = f.svc.GetIntegrationCommand(ctx, p, a.ID)
	require.Error(t, err)
	_, err = f.svc.IssueIntegration(ctx, p, domain.NewIDAt(f.clock.Now()), "living_room_tv", domain.CommandRefresh, domain.CommandPayload{})
	require.Error(t, err)
}
func TestIntegrationRejectExtraPayload(t *testing.T) {
	f, p := integrationFixture(t)
	for _, kind := range []domain.CommandKind{domain.CommandRefresh, domain.CommandNavigate, domain.CommandShow} {
		_, err := f.svc.IssueIntegration(t.Context(), p, domain.NewIDAt(f.clock.Now()), "living_room_tv", kind, domain.CommandPayload{PhotoID: "p", Route: domain.RouteDashboard})
		require.Error(t, err)
	}
	require.Zero(t, f.hub.count())
}
func TestIntegrationRetentionCannotReplay(t *testing.T) {
	f, p := integrationFixture(t)
	f.clock.Advance(-30 * 24 * time.Hour)
	// Existing fixture principal remains valid; old operation simulates a historic command.
	op := domain.NewIDAt(f.clock.Now())
	a, err := f.svc.IssueIntegration(t.Context(), p, op, "living_room_tv", domain.CommandRefresh, domain.CommandPayload{})
	require.NoError(t, err)
	n, err := f.db.Commands().DeleteOlderThan(t.Context(), f.clock.Now().Add(24*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	f.clock.Advance(30 * 24 * time.Hour)
	_, err = f.svc.GetIntegrationCommand(t.Context(), p, a.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = f.svc.IssueIntegration(t.Context(), p, op, "living_room_tv", domain.CommandRefresh, domain.CommandPayload{})
	require.Error(t, err)
	require.Equal(t, 1, f.hub.count())
}

func TestIntegrationRevokeAndExpiredPrincipal(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "expired", true: "revoked"}[revoke], func(t *testing.T) {
			f, p := integrationFixture(t)
			op := domain.NewIDAt(f.clock.Now())
			cmd, err := f.svc.IssueIntegration(t.Context(), p, op, "living_room_tv", domain.CommandRefresh, domain.CommandPayload{})
			require.NoError(t, err)
			if revoke {
				require.NoError(t, f.db.Integrations().Revoke(t.Context(), p, f.clock.Now()))
			} else {
				f.clock.Advance(31 * 24 * time.Hour)
			}
			_, err = f.svc.IssueIntegration(t.Context(), p, op, "living_room_tv", domain.CommandRefresh, domain.CommandPayload{})
			require.Error(t, err)
			_, err = f.svc.GetIntegrationCommand(t.Context(), p, cmd.ID)
			require.Error(t, err)
			require.Equal(t, 1, f.hub.count())
		})
	}
}

func TestIntegrationTTLRotationAndCounts(t *testing.T) {
	f, p := integrationFixture(t)
	ctx := t.Context()
	op := domain.NewIDAt(f.clock.Now())
	cmd, err := f.svc.IssueIntegration(ctx, p, op, "living_room_tv", domain.CommandRefresh, domain.CommandPayload{})
	require.NoError(t, err)
	f.clock.Advance(time.Second)
	now := f.clock.Now()
	require.NoError(t, f.db.Integrations().Rotate(ctx, p, &domain.IntegrationCredential{ID: "replacement", PrincipalID: p, TokenHash: "replacement-hash", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, now))
	again, err := f.svc.IssueIntegration(ctx, p, op, "living_room_tv", domain.CommandRefresh, domain.CommandPayload{})
	require.NoError(t, err)
	require.Equal(t, cmd.ExpiresAt, again.ExpiresAt)
	require.Equal(t, cmd.ID, again.ID)
	_, err = f.svc.IssueIntegration(ctx, p, op, "living_room_tv", domain.CommandNavigate, domain.CommandPayload{Route: domain.RouteDashboard})
	de, ok := domain.AsError(err)
	require.True(t, ok)
	require.Equal(t, domain.CodeIdempotencyConflict, de.Code)
	var count int
	require.NoError(t, f.db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM integration_operations").Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, f.db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM screen_commands").Scan(&count))
	require.Equal(t, 1, count)
}
func TestIntegrationRetainsMappingExactlySevenDays(t *testing.T) {
	f, p := integrationFixture(t)
	ctx := t.Context()
	now := f.clock.Now()
	op := domain.NewIDAt(now)
	cmd, err := f.svc.IssueIntegration(ctx, p, op, "living_room_tv", domain.CommandRefresh, domain.CommandPayload{})
	require.NoError(t, err)
	for _, delta := range []time.Duration{6 * 24 * time.Hour, 7 * 24 * time.Hour} {
		n, err := f.db.Commands().DeleteOlderThanAt(ctx, now.Add(time.Hour), now.Add(delta))
		require.NoError(t, err)
		require.Zero(t, n)
		_, err = f.svc.GetIntegrationOperation(ctx, p, op)
		require.NoError(t, err)
	}
	n, err := f.db.Commands().DeleteOlderThanAt(ctx, now.Add(time.Hour), now.Add(7*24*time.Hour+time.Millisecond))
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	_, err = f.db.Commands().Get(ctx, cmd.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
}
func TestIntegrationULIDInclusiveBoundaries(t *testing.T) {
	f, p := integrationFixture(t)
	for _, delta := range []time.Duration{-24 * time.Hour, 5 * time.Minute} {
		_, err := f.svc.IssueIntegration(t.Context(), p, domain.NewIDAt(f.clock.Now().Add(delta)), "living_room_tv", domain.CommandRefresh, domain.CommandPayload{})
		require.NoError(t, err)
	}
	_, err := f.svc.IssueIntegration(t.Context(), p, domain.NewIDAt(f.clock.Now()), "living_room_tv", domain.CommandNavigate, domain.CommandPayload{Route: domain.RoutePhotos})
	require.Error(t, err)
}
func TestIntegrationCurrentAuthorizationFailures(t *testing.T) {
	for _, scenario := range []string{"permission", "screen", "source", "photo"} {
		t.Run(scenario, func(t *testing.T) {
			f, p := integrationFixture(t)
			ctx := t.Context()
			photo := f.readyPhoto(t, "visible.jpg", domain.PreviewReady)
			op := domain.NewIDAt(f.clock.Now())
			cmd, err := f.svc.IssueIntegration(ctx, p, op, "living_room_tv", domain.CommandShow, domain.CommandPayload{PhotoID: photo})
			require.NoError(t, err)
			switch scenario {
			case "permission":
				principal, e := f.db.Integrations().Get(ctx, p)
				require.NoError(t, e)
				principal.Policy.Permissions = nil
				require.NoError(t, f.db.Integrations().UpdatePolicy(ctx, p, principal.Policy, f.clock.Now()))
			case "screen":
				_, err = f.db.SQL().ExecContext(ctx, "UPDATE screens SET status='revoked' WHERE id='living_room_tv'")
			case "source":
				err = f.db.Sources().Revoke(ctx, "family_photos", "test", f.clock.Now())
			case "photo":
				_, err = f.db.SQL().ExecContext(ctx, "UPDATE photos SET preview_status='pending' WHERE id=?", photo)
			}
			require.NoError(t, err)
			_, err = f.svc.IssueIntegration(ctx, p, op, "living_room_tv", domain.CommandShow, domain.CommandPayload{PhotoID: photo})
			require.Error(t, err)
			_, err = f.svc.GetIntegrationCommand(ctx, p, cmd.ID)
			require.Error(t, err)
		})
	}
}

type transactionProbeHub struct {
	db  *store.DB
	ctx context.Context
	err error
}

func (h *transactionProbeHub) Online(string) bool { return true }
func (h *transactionProbeHub) Deliver(_ string, _ *domain.Command) error {
	h.err = h.db.InWriteTx(h.ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(h.ctx, "UPDATE screens SET name=name WHERE id='living_room_tv'")
		return err
	})
	return h.err
}
func TestIntegrationDeliveryOutsideTransaction(t *testing.T) {
	f, p := integrationFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	hub := &transactionProbeHub{db: f.db, ctx: ctx}
	svc := screen.NewService(screen.Options{DB: f.db, Hub: hub, Now: f.clock.Now})
	_, err := svc.IssueIntegration(ctx, p, domain.NewIDAt(f.clock.Now()), "living_room_tv", domain.CommandRefresh, domain.CommandPayload{})
	require.NoError(t, err)
	require.NoError(t, hub.err)
}
