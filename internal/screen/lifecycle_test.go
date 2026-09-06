package screen_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/screen"
	"github.com/DituLin/Atritum/internal/ws"
)

func TestAckAppliedResolvesOnce(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	cmd, err := f.svc.Issue(ctx, "living_room_tv", domain.CommandRefresh, domain.CommandPayload{}, "admin")
	require.NoError(t, err)

	route := &domain.RouteState{Name: domain.RouteDashboard}
	require.NoError(t, f.svc.OnAck(ctx, "living_room_tv", ws.CommandAck{
		CommandID: cmd.ID, Status: domain.CommandApplied, Route: route,
	}))

	stored, err := f.db.Commands().Get(ctx, cmd.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.CommandApplied, stored.Status)
	require.NotNil(t, stored.ResolvedAt)
	require.NotNil(t, stored.Result)
	require.NotNil(t, stored.Result.Route)
	assert.Equal(t, domain.RouteDashboard, stored.Result.Route.Name)

	// A re-delivered command is acked twice by a careless client; the second
	// acknowledgement must not overwrite the recorded outcome.
	require.NoError(t, f.svc.OnAck(ctx, "living_room_tv", ws.CommandAck{
		CommandID: cmd.ID, Status: domain.CommandFailed, ErrorCode: "superseded",
	}))
	again, err := f.db.Commands().Get(ctx, cmd.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.CommandApplied, again.Status)
	assert.Empty(t, again.ErrorCode)
}

func TestAckFailedCarriesTheClientErrorCode(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	cmd, err := f.svc.Issue(ctx, "living_room_tv", domain.CommandNavigate,
		domain.CommandPayload{Route: domain.RoutePhotos}, "admin")
	require.NoError(t, err)

	require.NoError(t, f.svc.OnAck(ctx, "living_room_tv", ws.CommandAck{
		CommandID: cmd.ID, Status: domain.CommandFailed, ErrorCode: "photo_unavailable",
	}))
	stored, err := f.db.Commands().Get(ctx, cmd.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.CommandFailed, stored.Status)
	assert.Equal(t, "photo_unavailable", stored.ErrorCode)
}

// A screen may only close out its own commands.
func TestAckFromAnotherScreenIsIgnored(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	cmd, err := f.svc.Issue(ctx, "living_room_tv", domain.CommandRefresh, domain.CommandPayload{}, "admin")
	require.NoError(t, err)

	require.NoError(t, f.svc.OnAck(ctx, "kitchen_tv", ws.CommandAck{
		CommandID: cmd.ID, Status: domain.CommandApplied,
	}))
	stored, err := f.db.Commands().Get(ctx, cmd.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.CommandAccepted, stored.Status)
}

// Never delivered by its deadline: the command definitely had no effect.
func TestUndeliveredCommandExpires(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	f.hub.failNext = assertAnError{}
	// Issue one that failed delivery, then clear the failure and craft an
	// undelivered accepted command directly.
	_, err := f.svc.Issue(ctx, "living_room_tv", domain.CommandRefresh, domain.CommandPayload{}, "admin")
	require.NoError(t, err)

	now := f.clock.Now()
	pending := &domain.Command{
		ScreenID: "living_room_tv", Kind: domain.CommandRefresh, IssuedBy: "admin",
		IssuedAt: now, ExpiresAt: now.Add(10 * time.Second), Status: domain.CommandAccepted,
	}
	require.NoError(t, f.db.Commands().Issue(ctx, pending))

	f.clock.Advance(11 * time.Second)
	n, err := f.svc.ExpireDue(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	stored, err := f.db.Commands().Get(ctx, pending.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.CommandExpired, stored.Status)
	assert.Equal(t, screen.CodeExpired, stored.ErrorCode)
}

// Delivered but never acknowledged: the outcome is unverifiable, so the state
// is `unknown` and nothing claims success or failure (FR-15).
func TestDeliveredCommandWithoutAckBecomesUnknown(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	cmd, err := f.svc.Issue(ctx, "living_room_tv", domain.CommandRefresh, domain.CommandPayload{}, "admin")
	require.NoError(t, err)
	require.NotNil(t, cmd.DeliveredAt)

	// Inside the grace window nothing changes yet.
	f.clock.Advance(11 * time.Second)
	n, err := f.svc.ExpireDue(ctx)
	require.NoError(t, err)
	assert.Zero(t, n)
	stored, err := f.db.Commands().Get(ctx, cmd.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.CommandAccepted, stored.Status)

	// Past expires_at + 2 s it becomes unknown.
	f.clock.Advance(2 * time.Second)
	n, err = f.svc.ExpireDue(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	stored, err = f.db.Commands().Get(ctx, cmd.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.CommandUnknown, stored.Status)
	assert.Equal(t, screen.CodeAckTimeout, stored.ErrorCode)
}

// The next heartbeat annotates an unknown command with what the screen
// reports, without ever flipping the status itself (design §6.5).
func TestHeartbeatAttachesObservedStateToUnknownCommands(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	cmd, err := f.svc.Issue(ctx, "living_room_tv", domain.CommandNavigate,
		domain.CommandPayload{Route: domain.RoutePhotos}, "admin")
	require.NoError(t, err)

	f.clock.Advance(13 * time.Second)
	_, err = f.svc.ExpireDue(ctx)
	require.NoError(t, err)

	route := &domain.RouteState{Name: domain.RoutePhotos, Collection: "recent"}
	require.NoError(t, f.svc.OnHeartbeat(ctx, "living_room_tv", "127.0.0.1", ws.Heartbeat{
		Route: route, AppliedSequence: cmd.Sequence, ClientVersion: "0.1.0",
	}))

	stored, err := f.db.Commands().Get(ctx, cmd.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.CommandUnknown, stored.Status, "unknown is never flipped automatically")
	require.NotNil(t, stored.Result)
	require.NotNil(t, stored.Result.Observed)
	assert.Equal(t, cmd.Sequence, stored.Result.Observed.AppliedSequence)
	require.NotNil(t, stored.Result.Observed.Route)
	assert.Equal(t, domain.RoutePhotos, stored.Result.Observed.Route.Name)

	// The heartbeat is also presence and state for the screen row.
	sc, err := f.db.Screens().Get(ctx, "living_room_tv")
	require.NoError(t, err)
	require.NotNil(t, sc.CurrentRoute)
	assert.Equal(t, domain.RoutePhotos, sc.CurrentRoute.Name)
	assert.Equal(t, cmd.Sequence, sc.AppliedSequence)
	assert.Equal(t, "0.1.0", sc.ClientVersion)
	assert.Equal(t, "127.0.0.1", sc.LastIP)
	require.NotNil(t, sc.LastSeenAt)
}

// A process that dies mid-command cannot know what happened (design §4.2).
func TestRestartMarksOpenCommandsUnknown(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	cmd, err := f.svc.Issue(ctx, "living_room_tv", domain.CommandRefresh, domain.CommandPayload{}, "admin")
	require.NoError(t, err)

	n, err := f.svc.MarkRestartUnknown(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	stored, err := f.db.Commands().Get(ctx, cmd.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.CommandUnknown, stored.Status)
	assert.Equal(t, screen.CodeServerRestart, stored.ErrorCode)
}

// The newest still-open command is what a reconnecting client is told about.
func TestPendingCommandReturnsTheNewestUnexpired(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	first, err := f.svc.Issue(ctx, "living_room_tv", domain.CommandRefresh, domain.CommandPayload{}, "admin")
	require.NoError(t, err)
	second, err := f.svc.Issue(ctx, "living_room_tv", domain.CommandNavigate,
		domain.CommandPayload{Route: domain.RouteDashboard}, "admin")
	require.NoError(t, err)

	pending, err := f.svc.PendingCommand(ctx, "living_room_tv")
	require.NoError(t, err)
	require.NotNil(t, pending)
	assert.Equal(t, second.ID, pending.ID)
	assert.Greater(t, pending.Sequence, first.Sequence)

	// Past the TTL nothing is redelivered: the expirer owns it.
	f.clock.Advance(11 * time.Second)
	pending, err = f.svc.PendingCommand(ctx, "living_room_tv")
	require.NoError(t, err)
	assert.Nil(t, pending)
}

func TestPendingCommandIsNilWithoutOpenCommands(t *testing.T) {
	f := newFixture(t)
	pending, err := f.svc.PendingCommand(t.Context(), "living_room_tv")
	require.NoError(t, err)
	assert.Nil(t, pending)
}
