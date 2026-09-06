package screen_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/logging"
	"github.com/DituLin/Atritum/internal/screen"
	"github.com/DituLin/Atritum/internal/store"
	"github.com/DituLin/Atritum/internal/store/testutil"
)

// fakeHub records deliveries and can simulate an offline or wedged session.
type fakeHub struct {
	mu        sync.Mutex
	online    bool
	failNext  error
	delivered []string
}

func (h *fakeHub) Online(string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.online
}

func (h *fakeHub) Deliver(_ string, cmd *domain.Command) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.failNext != nil {
		err := h.failNext
		h.failNext = nil
		return err
	}
	h.delivered = append(h.delivered, cmd.ID)
	return nil
}

func (h *fakeHub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.delivered)
}

type fixture struct {
	db    *store.DB
	hub   *fakeHub
	svc   *screen.Service
	clock *movingClock
}

type movingClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *movingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *movingClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.at = c.at.Add(d)
	c.mu.Unlock()
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := testutil.NewDB(t)
	clk := &movingClock{at: time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)}
	hub := &fakeHub{online: true}
	ctx := t.Context()

	require.NoError(t, db.Screens().Create(ctx, &domain.Screen{
		ID: "living_room_tv", Name: "Living room", TokenHash: "hash",
		Status: domain.ScreenActive, CreatedAt: clk.Now(), ApprovedAt: clk.Now(),
	}))
	require.NoError(t, db.Sources().Upsert(ctx, "family_photos", "Family", "/Volumes/photos/family", clk.Now()))

	svc := screen.NewService(screen.Options{
		DB: db, Hub: hub, Logger: logging.Discard(), Now: clk.Now, TTL: 10 * time.Second,
	})
	return &fixture{db: db, hub: hub, svc: svc, clock: clk}
}

// readyPhoto inserts a photo a `show` command may legitimately target.
func (f *fixture) readyPhoto(t *testing.T, rel string, preview domain.PreviewStatus) string {
	t.Helper()
	now := f.clock.Now()
	ph := &domain.Photo{
		SourceID: "family_photos", RelPath: rel, Ext: "jpg", SizeBytes: 100, MtimeUnix: now.Unix(),
		Status: domain.PhotoReady, FirstSeenAt: now, LastSeenAt: now, LastSeenGeneration: 1,
		MetaStatus: domain.MetaReady, PreviewStatus: preview, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, f.db.Photos().Insert(t.Context(), ph))
	return ph.ID
}

func TestNavigateAcceptsWhitelistedRoutes(t *testing.T) {
	f := newFixture(t)
	cmd, err := f.svc.Issue(t.Context(), "living_room_tv", domain.CommandNavigate,
		domain.CommandPayload{Route: domain.RoutePhotos, Collection: "recent"}, "admin")
	require.NoError(t, err)
	assert.Equal(t, domain.CommandAccepted, cmd.Status)
	assert.EqualValues(t, 1, cmd.Sequence)
	require.NotNil(t, cmd.DeliveredAt)
	assert.Equal(t, 1, f.hub.count())
}

func TestValidationMatrix(t *testing.T) {
	f := newFixture(t)
	ready := f.readyPhoto(t, "2026/ready.jpg", domain.PreviewReady)
	pending := f.readyPhoto(t, "2026/pending.jpg", domain.PreviewPending)

	cases := []struct {
		name    string
		kind    domain.CommandKind
		payload domain.CommandPayload
		wantErr bool
	}{
		{"navigate dashboard", domain.CommandNavigate, domain.CommandPayload{Route: domain.RouteDashboard}, false},
		{"navigate photos", domain.CommandNavigate, domain.CommandPayload{Route: domain.RoutePhotos}, false},
		{"navigate photo route", domain.CommandNavigate, domain.CommandPayload{Route: domain.RoutePhoto}, true},
		{"navigate pair route", domain.CommandNavigate, domain.CommandPayload{Route: domain.RoutePair}, true},
		{"navigate empty route", domain.CommandNavigate, domain.CommandPayload{}, true},
		{"navigate unknown collection", domain.CommandNavigate,
			domain.CommandPayload{Route: domain.RoutePhotos, Collection: "secret"}, true},
		{"navigate collection on dashboard", domain.CommandNavigate,
			domain.CommandPayload{Route: domain.RouteDashboard, Collection: "recent"}, true},
		{"show ready photo", domain.CommandShow, domain.CommandPayload{PhotoID: ready}, false},
		{"show photo without preview", domain.CommandShow, domain.CommandPayload{PhotoID: pending}, true},
		{"show unknown photo", domain.CommandShow, domain.CommandPayload{PhotoID: "01JNOPE"}, true},
		{"show without photo", domain.CommandShow, domain.CommandPayload{}, true},
		{"refresh", domain.CommandRefresh, domain.CommandPayload{}, false},
		{"refresh ignores stray fields", domain.CommandRefresh,
			domain.CommandPayload{Route: domain.RoutePhoto, PhotoID: "x"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.svc.Issue(t.Context(), "living_room_tv", tc.kind, tc.payload, "admin")
			if !tc.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			apiErr, ok := domain.AsError(err)
			require.True(t, ok)
			assert.Equal(t, domain.CodeInvalidCommand, apiErr.Code)
			assert.Equal(t, 400, apiErr.HTTPStatus())
		})
	}
}

func TestUnknownScreenIsNotFound(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Issue(t.Context(), "kitchen_tv", domain.CommandRefresh, domain.CommandPayload{}, "admin")
	require.Error(t, err)
	apiErr, ok := domain.AsError(err)
	require.True(t, ok)
	assert.Equal(t, domain.CodeNotFound, apiErr.Code)
}

func TestRevokedScreenIsNotFound(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.db.Screens().Revoke(t.Context(), "living_room_tv", f.clock.Now()))
	_, err := f.svc.Issue(t.Context(), "living_room_tv", domain.CommandRefresh, domain.CommandPayload{}, "admin")
	apiErr, ok := domain.AsError(err)
	require.True(t, ok)
	assert.Equal(t, domain.CodeNotFound, apiErr.Code)
}

// An offline screen records a failed command and returns 409: nothing is
// queued, so a TV that comes back does not replay yesterday's navigation.
func TestOfflineScreenRecordsFailedCommandAndConflicts(t *testing.T) {
	f := newFixture(t)
	f.hub.online = false

	cmd, err := f.svc.Issue(t.Context(), "living_room_tv", domain.CommandRefresh,
		domain.CommandPayload{}, "admin")
	require.Error(t, err)
	require.NotNil(t, cmd)
	apiErr, ok := domain.AsError(err)
	require.True(t, ok)
	assert.Equal(t, domain.CodeScreenOffline, apiErr.Code)
	assert.Equal(t, 409, apiErr.HTTPStatus())
	assert.Equal(t, domain.CommandFailed, cmd.Status)
	assert.Equal(t, screen.CodeScreenOffline, cmd.ErrorCode)
	assert.Zero(t, f.hub.count())

	stored, err := f.db.Commands().Get(t.Context(), cmd.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.CommandFailed, stored.Status)
	assert.Nil(t, stored.DeliveredAt)
}

func TestDeliveryFailureRecordsFailedCommand(t *testing.T) {
	f := newFixture(t)
	f.hub.failNext = assertAnError{}

	cmd, err := f.svc.Issue(t.Context(), "living_room_tv", domain.CommandRefresh,
		domain.CommandPayload{}, "admin")
	require.NoError(t, err)
	assert.Equal(t, domain.CommandFailed, cmd.Status)
	assert.Equal(t, screen.CodeDeliveryFailed, cmd.ErrorCode)
}

type assertAnError struct{}

func (assertAnError) Error() string { return "session closed" }

// Sequences are allocated inside the insert transaction, so concurrent issuers
// can never produce a duplicate or a gap that reorders on the client.
func TestSequencesAreMonotonicUnderConcurrency(t *testing.T) {
	f := newFixture(t)
	const n = 30

	var wg sync.WaitGroup
	seqs := make([]int64, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd, err := f.svc.Issue(context.Background(), "living_room_tv",
				domain.CommandRefresh, domain.CommandPayload{}, "admin")
			errs[i] = err
			if cmd != nil {
				seqs[i] = cmd.Sequence
			}
		}()
	}
	wg.Wait()

	seen := make(map[int64]bool, n)
	for i := 0; i < n; i++ {
		require.NoError(t, errs[i])
		require.Positive(t, seqs[i])
		require.False(t, seen[seqs[i]], "sequence %d was issued twice", seqs[i])
		seen[seqs[i]] = true
	}
	sc, err := f.db.Screens().Get(t.Context(), "living_room_tv")
	require.NoError(t, err)
	assert.EqualValues(t, n, sc.LastSequence)
}
