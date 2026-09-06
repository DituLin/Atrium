package ws_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/ws"
)

func TestCookieHandshakeWithAllowedOriginSucceeds(t *testing.T) {
	f := newFixture(t)
	conn, _, err := f.dial(withCookie(f.token), withOrigin(testOrigin))
	require.NoError(t, err)
	defer func() { _ = conn.Close(coderws.StatusNormalClosure, "") }()

	env := readEnvelope(t, conn)
	require.Equal(t, ws.TypeSessionReady, env.Type)
	var ready ws.SessionReady
	require.NoError(t, json.Unmarshal(env.Payload, &ready))
	assert.Equal(t, "living_room_tv", ready.Screen.ID)
	assert.Equal(t, "Living room", ready.Screen.Name)
	assert.NotEmpty(t, ready.ServerTime)
	assert.Nil(t, ready.PendingCommand)
	assert.True(t, f.hub.Online("living_room_tv"))
}

// A non-browser client sends no Origin and must therefore prove itself with a
// bearer token, which no cross-site page can attach.
func TestBearerHandshakeWithoutOriginSucceeds(t *testing.T) {
	f := newFixture(t)
	conn, _, err := f.dial(withBearer(f.token))
	require.NoError(t, err)
	defer func() { _ = conn.Close(coderws.StatusNormalClosure, "") }()
	require.Equal(t, ws.TypeSessionReady, readEnvelope(t, conn).Type)
}

func TestCookieHandshakeWithoutOriginIsRefused(t *testing.T) {
	f := newFixture(t)
	_, resp, err := f.dial(withCookie(f.token))
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestForeignOriginIsRefused(t *testing.T) {
	f := newFixture(t)
	_, resp, err := f.dial(withCookie(f.token), withOrigin("https://evil.example"))
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestUnauthenticatedHandshakeIsRefused(t *testing.T) {
	f := newFixture(t)
	_, resp, err := f.dial(withOrigin(testOrigin))
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.False(t, f.hub.Online("living_room_tv"))
}

// One screen has one session: the older connection is told why and closed with
// 4003, so the same TV with two tabs never executes a command twice.
func TestSecondConnectionSupersedesTheFirst(t *testing.T) {
	f := newFixture(t)
	first, _, err := f.dial(withBearer(f.token))
	require.NoError(t, err)
	require.Equal(t, ws.TypeSessionReady, readEnvelope(t, first).Type)

	second, _, err := f.dial(withBearer(f.token))
	require.NoError(t, err)
	defer func() { _ = second.Close(coderws.StatusNormalClosure, "") }()
	require.Equal(t, ws.TypeSessionReady, readEnvelope(t, second).Type)

	assert.Equal(t, ws.TypeSessionSuperseded, readEnvelope(t, first).Type)
	assert.Equal(t, coderws.StatusCode(4003), expectClose(t, first))
	assert.True(t, f.hub.Online("living_room_tv"), "the newer session keeps the screen online")
}

func TestMalformedMessageClosesWithProtocolError(t *testing.T) {
	f := newFixture(t)
	conn, _, err := f.dial(withBearer(f.token))
	require.NoError(t, err)
	require.Equal(t, ws.TypeSessionReady, readEnvelope(t, conn).Type)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte("{not json")))
	assert.Equal(t, coderws.StatusCode(4005), expectClose(t, conn))
}

func TestOversizedMessageClosesWithProtocolError(t *testing.T) {
	f := newFixture(t)
	conn, _, err := f.dial(withBearer(f.token))
	require.NoError(t, err)
	require.Equal(t, ws.TypeSessionReady, readEnvelope(t, conn).Type)

	// One byte over the 64 KiB limit of design §9.
	huge := `{"schema_version":1,"type":"state","id":"x","sent_at":"","payload":{"route":{"name":"` +
		strings.Repeat("a", ws.MaxMessageBytes) + `"}}}`
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(huge)))
	assert.Equal(t, coderws.StatusCode(4005), expectClose(t, conn))
}

func TestHeartbeatIsAcknowledgedAndRouted(t *testing.T) {
	f := newFixture(t)
	conn, _, err := f.dial(withBearer(f.token))
	require.NoError(t, err)
	defer func() { _ = conn.Close(coderws.StatusNormalClosure, "") }()
	require.Equal(t, ws.TypeSessionReady, readEnvelope(t, conn).Type)

	send(t, conn, ws.TypeHeartbeat, ws.Heartbeat{
		Route:           &domain.RouteState{Name: domain.RoutePhotos, Collection: "recent"},
		AppliedSequence: 3,
		ClientVersion:   "0.1.0",
	})
	env := readEnvelope(t, conn)
	require.Equal(t, ws.TypeHeartbeatAck, env.Type)
	var ack ws.HeartbeatAck
	require.NoError(t, json.Unmarshal(env.Payload, &ack))
	assert.NotEmpty(t, ack.ServerTime)

	require.Eventually(t, func() bool {
		hb, _, _ := f.handler.counts()
		return hb == 1
	}, 2*time.Second, 10*time.Millisecond)
	f.handler.mu.Lock()
	defer f.handler.mu.Unlock()
	assert.Equal(t, int64(3), f.handler.heartbeats[0].AppliedSequence)
	assert.Equal(t, "0.1.0", f.handler.heartbeats[0].ClientVersion)
}

func TestStateAndAckReachTheHandler(t *testing.T) {
	f := newFixture(t)
	conn, _, err := f.dial(withBearer(f.token))
	require.NoError(t, err)
	defer func() { _ = conn.Close(coderws.StatusNormalClosure, "") }()
	require.Equal(t, ws.TypeSessionReady, readEnvelope(t, conn).Type)

	send(t, conn, ws.TypeState, ws.StateReport{Route: &domain.RouteState{Name: domain.RouteDashboard}})
	send(t, conn, ws.TypeCommandAck, ws.CommandAck{
		CommandID: "01JCMD", Status: domain.CommandFailed,
		Route: &domain.RouteState{Name: domain.RouteDashboard}, ErrorCode: "superseded",
	})
	require.Eventually(t, func() bool {
		_, states, acks := f.handler.counts()
		return states == 1 && acks == 1
	}, 2*time.Second, 10*time.Millisecond)
	f.handler.mu.Lock()
	defer f.handler.mu.Unlock()
	assert.Equal(t, "superseded", f.handler.acks[0].ErrorCode)
}

// A session that stops sending heartbeats is offline for presence purposes and
// is eventually closed by the monitor.
func TestHeartbeatTimeoutMarksOfflineAndCloses(t *testing.T) {
	f := newFixture(t)
	conn, _, err := f.dial(withBearer(f.token))
	require.NoError(t, err)
	require.Equal(t, ws.TypeSessionReady, readEnvelope(t, conn).Type)
	require.True(t, f.hub.Online("living_room_tv"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.hub.Run(ctx)

	f.clock.Advance(46 * time.Second)
	assert.False(t, f.hub.Online("living_room_tv"), "no heartbeat inside offline_after is offline")
	assert.Equal(t, coderws.StatusCode(1001), expectClose(t, conn))
}

func TestRevokeSendsReasonAndCloses(t *testing.T) {
	f := newFixture(t)
	conn, _, err := f.dial(withBearer(f.token))
	require.NoError(t, err)
	require.Equal(t, ws.TypeSessionReady, readEnvelope(t, conn).Type)

	f.hub.Revoke("living_room_tv", "screen_revoked")

	env := readEnvelope(t, conn)
	require.Equal(t, ws.TypeSessionRevoked, env.Type)
	var payload ws.SessionRevoked
	require.NoError(t, json.Unmarshal(env.Payload, &payload))
	assert.Equal(t, "screen_revoked", payload.Reason)
	assert.Equal(t, coderws.StatusCode(4002), expectClose(t, conn))
}

// session.ready redelivers the newest still-open command so a client that
// reconnected mid-command learns about it. Redelivery is safe: the client
// dedupes by command_id and applies only a newer sequence.
func TestSessionReadyCarriesPendingCommand(t *testing.T) {
	f := newFixture(t)
	f.handler.mu.Lock()
	f.handler.pending = &domain.Command{
		ID: "01JCMD", ScreenID: "living_room_tv", Sequence: 7,
		Kind: domain.CommandNavigate, Payload: domain.CommandPayload{Route: domain.RoutePhotos},
		IssuedAt: f.clock.Now(), ExpiresAt: f.clock.Now().Add(10 * time.Second),
		Status: domain.CommandAccepted,
	}
	f.handler.mu.Unlock()

	conn, _, err := f.dial(withBearer(f.token))
	require.NoError(t, err)
	defer func() { _ = conn.Close(coderws.StatusNormalClosure, "") }()

	env := readEnvelope(t, conn)
	require.Equal(t, ws.TypeSessionReady, env.Type)
	var ready ws.SessionReady
	require.NoError(t, json.Unmarshal(env.Payload, &ready))
	require.NotNil(t, ready.PendingCommand)
	assert.Equal(t, "01JCMD", ready.PendingCommand.CommandID)
	assert.EqualValues(t, 7, ready.PendingCommand.Sequence)
	assert.Equal(t, domain.CommandNavigate, ready.PendingCommand.Kind)
}

// A burst of publications must reach the client as a handful of messages, not
// a hundred: the coalescer merges everything inside its window (FR-17).
func TestChangeBurstIsCoalesced(t *testing.T) {
	f := newFixture(t)
	conn, _, err := f.dial(withBearer(f.token))
	require.NoError(t, err)
	defer func() { _ = conn.Close(coderws.StatusNormalClosure, "") }()
	require.Equal(t, ws.TypeSessionReady, readEnvelope(t, conn).Type)

	for i := 0; i < 100; i++ {
		f.bus.Publish(domain.TopicPhotos)
	}
	f.bus.Publish(domain.TopicNAS)

	env := readEnvelope(t, conn)
	require.Equal(t, ws.TypeDataChanged, env.Type)
	var changed ws.DataChanged
	require.NoError(t, json.Unmarshal(env.Payload, &changed))
	assert.Subset(t, []string{"home", "photos", "nas", "screen"}, changed.Topics)
	assert.Contains(t, changed.Topics, "photos")
	assert.Positive(t, changed.Version)

	// Drain whatever else arrived within a couple of windows; a hundred
	// publications must not become a hundred messages.
	extra := 0
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		_, _, rerr := conn.Read(ctx)
		cancel()
		if rerr != nil {
			break
		}
		extra++
	}
	assert.Less(t, extra, 5, "a burst of 100 events must coalesce into a handful of messages")
}
