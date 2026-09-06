package ws

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/DituLin/Atritum/internal/app/events"
	"github.com/DituLin/Atritum/internal/auth"
	"github.com/DituLin/Atritum/internal/domain"
)

// MonitorInterval is how often the heartbeat monitor runs (design §4.1).
const MonitorInterval = 5 * time.Second

// Handler receives the decoded client messages of one session. The command
// service implements it; the hub owns no persistence of its own.
type Handler interface {
	OnHeartbeat(ctx context.Context, screenID, ip string, hb Heartbeat) error
	OnState(ctx context.Context, screenID string, route *domain.RouteState) error
	OnAck(ctx context.Context, screenID string, ack CommandAck) error
	// PendingCommand returns the command to redeliver on connect, if any.
	PendingCommand(ctx context.Context, screenID string) (*domain.Command, error)
	// MarkOffline records that a session ended without a graceful goodbye.
	MarkOffline(ctx context.Context, screenID string) error
}

// Options configures a Hub.
type Options struct {
	Auth         *auth.Authenticator
	Bus          *events.Bus
	Logger       *slog.Logger
	Now          func() time.Time
	OfflineAfter time.Duration
	// CoalesceWindow overrides the 250 ms data.changed window in tests.
	CoalesceWindow time.Duration
	// MonitorInterval overrides the 5 s heartbeat sweep in tests.
	MonitorInterval time.Duration
}

// Hub owns every live session. One screen has at most one: a new connection
// supersedes the old one so two tabs never execute a command twice (§6.5).
type Hub struct {
	opts    Options
	handler Handler

	mu       sync.Mutex
	sessions map[string]*Session

	// wg tracks session goroutines so Shutdown can wait for them.
	wg sync.WaitGroup
}

// NewHub builds a hub. The handler is set separately because the command
// service needs the hub to deliver, so the two are mutually dependent.
func NewHub(opts Options) *Hub {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.OfflineAfter <= 0 {
		opts.OfflineAfter = 45 * time.Second
	}
	if opts.CoalesceWindow <= 0 {
		opts.CoalesceWindow = events.CoalesceWindow
	}
	if opts.MonitorInterval <= 0 {
		opts.MonitorInterval = MonitorInterval
	}
	return &Hub{opts: opts, sessions: make(map[string]*Session)}
}

// SetHandler installs the client-message handler.
func (h *Hub) SetHandler(handler Handler) { h.handler = handler }

// register adds a session, superseding any existing one for the same screen.
func (h *Hub) register(s *Session) {
	h.mu.Lock()
	old := h.sessions[s.ScreenID]
	h.sessions[s.ScreenID] = s
	h.mu.Unlock()

	if old != nil {
		_ = old.Send(TypeSessionSuperseded, struct{}{})
		old.close(CloseSuperseded, "superseded by a newer session")
		h.opts.Logger.Info("session superseded", "component", "ws",
			"event", "session_superseded", "screen_id", s.ScreenID)
	}
}

// unregister removes a session if it is still the current one.
func (h *Hub) unregister(s *Session) {
	h.mu.Lock()
	if cur, ok := h.sessions[s.ScreenID]; ok && cur == s {
		delete(h.sessions, s.ScreenID)
	}
	h.mu.Unlock()
}

// session returns the live session for a screen, or nil.
func (h *Hub) session(screenID string) *Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessions[screenID]
}

// Online reports presence: a live session whose last heartbeat is inside
// offline_after. This is what `registered` vs `online` means in §6.5.
func (h *Hub) Online(screenID string) bool {
	s := h.session(screenID)
	if s == nil || s.Closed() {
		return false
	}
	return h.opts.Now().Sub(s.LastSeen()) <= h.opts.OfflineAfter
}

// OnlineScreens lists the screens with a fresh session.
func (h *Hub) OnlineScreens() []string {
	h.mu.Lock()
	ids := make([]string, 0, len(h.sessions))
	for id := range h.sessions {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	out := ids[:0]
	for _, id := range ids {
		if h.Online(id) {
			out = append(out, id)
		}
	}
	return out
}

// Deliver queues a command for a screen. A missing or overflowing session is
// reported so the caller can record delivery_failed (design §6.5).
func (h *Hub) Deliver(screenID string, cmd *domain.Command) error {
	s := h.session(screenID)
	if s == nil || s.Closed() {
		return ErrSessionClosed
	}
	return s.Send(TypeScreenCommand, NewCommandMessage(cmd))
}

// Revoke tells a screen its credential is gone and closes with 4002.
func (h *Hub) Revoke(screenID, reason string) {
	s := h.session(screenID)
	if s == nil {
		return
	}
	_ = s.Send(TypeSessionRevoked, SessionRevoked{Reason: reason})
	s.close(CloseRevoked, reason)
	h.opts.Logger.Info("session revoked", "component", "ws",
		"event", "session_revoked", "screen_id", screenID, "reason", reason)
}

// Run owns the heartbeat monitor and closes every session when ctx ends.
func (h *Hub) Run(ctx context.Context) {
	ticker := time.NewTicker(h.opts.MonitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			h.shutdown()
			return
		case <-ticker.C:
			h.sweep(ctx)
		}
	}
}

// sweep closes sessions that stopped sending heartbeats. Presence already
// reads false for them; closing frees the connection and lets the client
// reconnect with a fresh snapshot.
func (h *Hub) sweep(ctx context.Context) {
	now := h.opts.Now()
	h.mu.Lock()
	stale := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		if now.Sub(s.LastSeen()) > h.opts.OfflineAfter {
			stale = append(stale, s)
		}
	}
	h.mu.Unlock()
	for _, s := range stale {
		h.opts.Logger.Info("session timed out", "component", "ws",
			"event", "heartbeat_timeout", "screen_id", s.ScreenID)
		s.close(CloseShutdown, "heartbeat timeout")
		if h.handler != nil {
			_ = h.handler.MarkOffline(ctx, s.ScreenID)
		}
	}
}

// shutdown closes every session with 1001 and waits for the goroutines.
func (h *Hub) shutdown() {
	h.mu.Lock()
	all := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		all = append(all, s)
	}
	h.mu.Unlock()
	for _, s := range all {
		s.close(CloseShutdown, "server shutting down")
	}
	h.wg.Wait()
}
