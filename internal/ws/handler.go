package ws

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/DituLin/Atrium/internal/app/events"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
)

// ServeHTTP upgrades an authenticated screen request into a session. An
// unauthenticated handshake is refused before the upgrade when possible, and
// otherwise closed with 4001, because a browser only observes the close code.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := h.opts.Auth.AuthenticateWebSocket(r.Context(), r)
	if err != nil || id == nil || id.Screen == nil {
		h.opts.Logger.Info("websocket handshake refused", "component", "ws",
			"event", "handshake_unauthorized", "remote_ip", auth.ClientIP(r))
		// The screen client reads the HTTP status when the upgrade never
		// happens; 4001 is what it sees once a socket exists.
		http.Error(w, `{"error":{"code":"unauthorized","message":"screen credential required"}}`,
			http.StatusUnauthorized)
		return
	}

	// The origin was already validated by AuthenticateWebSocket, so the
	// library's own check is redundant and would reject non-browser clients
	// that legitimately send no Origin with a bearer token.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		CompressionMode:    websocket.CompressionDisabled,
	})
	if err != nil {
		h.opts.Logger.Warn("websocket upgrade failed", "component", "ws",
			"event", "upgrade_failed", "screen_id", id.Screen.ID)
		return
	}
	// The read limit is one byte over the protocol maximum so an oversized
	// frame is detected here and closed with 4005 rather than the library's
	// generic 1009.
	conn.SetReadLimit(MaxMessageBytes + 1)

	s := newSession(conn, id.Screen.ID, id.Screen.Name, auth.ClientIP(r), h.opts.Now)
	h.register(s)
	h.serve(r.Context(), s)
}

// serve runs one session: the writer, the change-push coalescer and the read
// loop, which owns the goroutine until the connection ends.
func (h *Hub) serve(reqCtx context.Context, s *Session) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(reqCtx))
	defer cancel()

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		s.writeLoop(ctx)
	}()
	if h.opts.Bus != nil {
		sub := h.opts.Bus.Subscribe("ws:"+s.ScreenID, SendQueueSize)
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			defer sub.Close()
			events.Coalesce(ctx, sub, h.opts.CoalesceWindow, func(ev events.Event) {
				topics := make([]string, 0, len(ev.Topics))
				for _, t := range ev.Topics {
					topics = append(topics, string(t))
				}
				_ = s.Send(TypeDataChanged, DataChanged{Topics: topics, Version: ev.Version})
			})
		}()
	}

	if err := h.sendReady(ctx, s); err != nil {
		s.close(CloseNormal, "ready failed")
	}
	h.readLoop(ctx, s)

	cancel()
	h.unregister(s)
	if h.handler != nil {
		_ = h.handler.MarkOffline(context.WithoutCancel(reqCtx), s.ScreenID)
	}
}

// sendReady emits session.ready, redelivering the newest still-open command.
func (h *Hub) sendReady(ctx context.Context, s *Session) error {
	ready := SessionReady{
		Screen:     ScreenRef{ID: s.ScreenID, Name: s.ScreenName},
		ServerTime: h.opts.Now().UTC().Format(time.RFC3339),
	}
	if h.opts.Bus != nil {
		ready.HomeVersion = h.opts.Bus.Version()
	}
	if h.handler != nil {
		cmd, err := h.handler.PendingCommand(ctx, s.ScreenID)
		if err == nil && cmd != nil {
			ready.PendingCommand = NewCommandMessage(cmd)
		}
	}
	return s.Send(TypeSessionReady, ready)
}

// readLoop consumes client frames until the connection or the session ends.
func (h *Hub) readLoop(ctx context.Context, s *Session) {
	for {
		if s.Closed() || ctx.Err() != nil {
			return
		}
		data, err := readMessage(ctx, s.conn)
		if err != nil {
			if errors.Is(err, errTooLarge) || errors.Is(err, errBadFrame) {
				s.close(CloseProtocolErr, "protocol error")
			} else {
				s.close(CloseNormal, "client closed")
			}
			return
		}
		if err := h.dispatch(ctx, s, data); err != nil {
			s.close(CloseProtocolErr, "protocol error")
			return
		}
	}
}

var (
	errTooLarge = errors.New("ws: message exceeds 64 KiB")
	errBadFrame = errors.New("ws: unsupported frame type")
)

// readMessage reads one text frame, bounded at MaxMessageBytes.
func readMessage(ctx context.Context, conn *websocket.Conn) ([]byte, error) {
	typ, r, err := conn.Reader(ctx)
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageText {
		return nil, errBadFrame
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxMessageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxMessageBytes {
		return nil, errTooLarge
	}
	return data, nil
}

// dispatch decodes one envelope and routes it to the handler. A malformed
// message is a protocol error (4005), never a silently ignored frame.
func (h *Hub) dispatch(ctx context.Context, s *Session, data []byte) error {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	now := h.opts.Now()
	switch env.Type {
	case TypeHeartbeat:
		var hb Heartbeat
		if err := json.Unmarshal(env.Payload, &hb); err != nil {
			return err
		}
		s.touch(hb.Route, hb.AppliedSequence, now)
		if h.handler != nil {
			_ = h.handler.OnHeartbeat(ctx, s.ScreenID, s.RemoteIP, hb)
		}
		return s.Send(TypeHeartbeatAck, HeartbeatAck{ServerTime: now.UTC().Format(time.RFC3339)})
	case TypeState:
		var st StateReport
		if err := json.Unmarshal(env.Payload, &st); err != nil {
			return err
		}
		s.touch(st.Route, 0, now)
		if h.handler != nil {
			_ = h.handler.OnState(ctx, s.ScreenID, st.Route)
		}
		return nil
	case TypeCommandAck:
		var ack CommandAck
		if err := json.Unmarshal(env.Payload, &ack); err != nil {
			return err
		}
		if ack.CommandID == "" {
			return errBadFrame
		}
		s.touch(ack.Route, 0, now)
		if h.handler != nil {
			_ = h.handler.OnAck(ctx, s.ScreenID, ack)
		}
		return nil
	default:
		// An unknown type from a newer client is not fatal; the design keeps
		// the type list closed, so it is logged and ignored.
		h.opts.Logger.Debug("unknown message type", "component", "ws",
			"event", "unknown_type", "screen_id", s.ScreenID, "type", env.Type)
		return nil
	}
}

// compile-time proof that the hub satisfies the delivery contract the command
// service depends on.
var _ interface {
	Deliver(string, *domain.Command) error
	Online(string) bool
} = (*Hub)(nil)
