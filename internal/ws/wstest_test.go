package ws_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/app/events"
	"github.com/DituLin/Atritum/internal/auth"
	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/logging"
	"github.com/DituLin/Atritum/internal/store"
	"github.com/DituLin/Atritum/internal/store/testutil"
	"github.com/DituLin/Atritum/internal/ws"
)

const testOrigin = "https://127.0.0.1:8443"

// fixture is a hub behind an httptest server with one registered screen.
type fixture struct {
	t       *testing.T
	db      *store.DB
	hub     *ws.Hub
	bus     *events.Bus
	server  *httptest.Server
	token   string
	handler *recordingHandler
	clock   *fakeClock
}

type fakeClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.at = c.at.Add(d)
	c.mu.Unlock()
}

// recordingHandler captures what the hub routes to the command service.
type recordingHandler struct {
	mu         sync.Mutex
	heartbeats []ws.Heartbeat
	states     []*domain.RouteState
	acks       []ws.CommandAck
	offline    []string
	pending    *domain.Command
}

func (h *recordingHandler) OnHeartbeat(_ context.Context, _, _ string, hb ws.Heartbeat) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.heartbeats = append(h.heartbeats, hb)
	return nil
}

func (h *recordingHandler) OnState(_ context.Context, _ string, route *domain.RouteState) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.states = append(h.states, route)
	return nil
}

func (h *recordingHandler) OnAck(_ context.Context, _ string, ack ws.CommandAck) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.acks = append(h.acks, ack)
	return nil
}

func (h *recordingHandler) PendingCommand(context.Context, string) (*domain.Command, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pending, nil
}

func (h *recordingHandler) MarkOffline(_ context.Context, screenID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.offline = append(h.offline, screenID)
	return nil
}

func (h *recordingHandler) counts() (int, int, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.heartbeats), len(h.states), len(h.acks)
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := testutil.NewDB(t)
	clk := &fakeClock{at: time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)}
	now := clk.Now

	token, err := auth.NewScreenToken()
	require.NoError(t, err)
	require.NoError(t, db.Screens().Create(t.Context(), &domain.Screen{
		ID: "living_room_tv", Name: "Living room", TokenHash: auth.HashToken(token),
		Status: domain.ScreenActive, CreatedAt: now(), ApprovedAt: now(),
	}))

	bus := events.NewBus()
	hub := ws.NewHub(ws.Options{
		Auth:            auth.NewAuthenticator(db, auth.NewOriginPolicy([]string{testOrigin}), now),
		Bus:             bus,
		Logger:          logging.Discard(),
		Now:             now,
		OfflineAfter:    45 * time.Second,
		CoalesceWindow:  30 * time.Millisecond,
		MonitorInterval: 20 * time.Millisecond,
	})
	handler := &recordingHandler{}
	hub.SetHandler(handler)

	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/screens/connect", hub)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &fixture{t: t, db: db, hub: hub, bus: bus, server: srv, token: token, handler: handler, clock: clk}
}

// dial opens a client connection with the given request options.
func (f *fixture) dial(opts ...func(http.Header)) (*coderws.Conn, *http.Response, error) {
	f.t.Helper()
	header := http.Header{}
	for _, o := range opts {
		o(header)
	}
	url := strings.Replace(f.server.URL, "http://", "ws://", 1) + "/api/v1/screens/connect"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	f.t.Cleanup(cancel)
	return coderws.Dial(ctx, url, &coderws.DialOptions{HTTPHeader: header})
}

func withCookie(token string) func(http.Header) {
	return func(h http.Header) { h.Set("Cookie", auth.ScreenCookieName+"="+token) }
}

func withBearer(token string) func(http.Header) {
	return func(h http.Header) { h.Set("Authorization", "Bearer "+token) }
}

func withOrigin(o string) func(http.Header) {
	return func(h http.Header) { h.Set("Origin", o) }
}

// readEnvelope reads one message and decodes its envelope.
func readEnvelope(t *testing.T, conn *coderws.Conn) ws.Envelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	require.NoError(t, err)
	var env ws.Envelope
	require.NoError(t, json.Unmarshal(data, &env))
	require.Equal(t, ws.SchemaVersion, env.SchemaVersion)
	return env
}

// send writes one client message.
func send(t *testing.T, conn *coderws.Conn, msgType string, payload any) {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	env, err := json.Marshal(ws.Envelope{
		SchemaVersion: ws.SchemaVersion, Type: msgType,
		ID: "msg_test", SentAt: time.Now().UTC().Format(time.RFC3339), Payload: body,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, conn.Write(ctx, coderws.MessageText, env))
}

// expectClose reads until the connection closes and returns the close code.
func expectClose(t *testing.T, conn *coderws.Conn) coderws.StatusCode {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, _, err := conn.Read(ctx)
		cancel()
		if err != nil {
			return coderws.CloseStatus(err)
		}
	}
	t.Fatal("connection did not close")
	return 0
}
