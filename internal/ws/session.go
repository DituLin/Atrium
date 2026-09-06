package ws

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/DituLin/Atritum/internal/domain"
)

// SendQueueSize is the per-session outbound bound from design §4.1.
const SendQueueSize = 64

// writeTimeout bounds one frame write so a wedged TV cannot pin a goroutine.
const writeTimeout = 10 * time.Second

// Close codes from design §9.
const (
	CloseNormal       = websocket.StatusNormalClosure // 1000
	CloseShutdown     = websocket.StatusGoingAway     // 1001
	CloseUnauthorized = websocket.StatusCode(4001)    // unauthorized
	CloseRevoked      = websocket.StatusCode(4002)    // revoked
	CloseSuperseded   = websocket.StatusCode(4003)    // superseded
	CloseOverflow     = websocket.StatusCode(4004)    // send-queue overflow
	CloseProtocolErr  = websocket.StatusCode(4005)    // protocol error
)

// ErrSessionClosed is returned when a message cannot be queued because the
// session is gone; the command service turns it into delivery_failed.
var ErrSessionClosed = errors.New("ws: session closed")

// ErrQueueFull is returned when a non-droppable message meets a full queue.
// The session is closed with 4004 as a side effect.
var ErrQueueFull = errors.New("ws: send queue overflow")

// outgoing is one queued frame plus whether it may be dropped under pressure.
type outgoing struct {
	data []byte
	// droppable marks a message whose loss is harmless because the client
	// re-fetches anyway: only data.changed qualifies.
	droppable bool
}

// Session is one authenticated WebSocket connection for one screen.
type Session struct {
	ScreenID   string
	ScreenName string
	RemoteIP   string

	conn *websocket.Conn
	send chan outgoing
	now  func() time.Time

	// closeOnce guards the close code and the channel shutdown.
	closeOnce sync.Once
	closed    chan struct{}
	closeCode websocket.StatusCode
	closeMsg  string

	mu sync.Mutex
	// lastSeen is the connection time until the first heartbeat lands.
	lastSeen time.Time
	// route and appliedSeq mirror the client's last report.
	route      *domain.RouteState
	appliedSeq int64
	dropped    int64
}

func newSession(conn *websocket.Conn, screenID, name, ip string, now func() time.Time) *Session {
	t := now()
	return &Session{
		ScreenID: screenID, ScreenName: name, RemoteIP: ip,
		conn: conn, send: make(chan outgoing, SendQueueSize), now: now,
		closed: make(chan struct{}), lastSeen: t,
	}
}

// enqueue appends a frame. A full queue drops a droppable message and closes
// the session with 4004 for anything else (design §4.1).
func (s *Session) enqueue(msg outgoing) error {
	select {
	case <-s.closed:
		return ErrSessionClosed
	default:
	}
	select {
	case s.send <- msg:
		return nil
	default:
	}
	if msg.droppable {
		s.mu.Lock()
		s.dropped++
		s.mu.Unlock()
		return nil
	}
	s.close(CloseOverflow, "send queue overflow")
	return ErrQueueFull
}

// Send encodes and queues one message.
func (s *Session) Send(msgType string, payload any) error {
	data, err := encode(msgType, s.now(), payload)
	if err != nil {
		return err
	}
	return s.enqueue(outgoing{data: data, droppable: msgType == TypeDataChanged})
}

// close records the close reason once and stops the writer.
func (s *Session) close(code websocket.StatusCode, reason string) {
	s.closeOnce.Do(func() {
		s.closeCode = code
		s.closeMsg = reason
		close(s.closed)
	})
}

// Closed reports whether the session has been asked to stop.
func (s *Session) Closed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

// touch records presence from a heartbeat or state report.
func (s *Session) touch(route *domain.RouteState, appliedSeq int64, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSeen = at
	if route != nil {
		s.route = route
	}
	if appliedSeq > s.appliedSeq {
		s.appliedSeq = appliedSeq
	}
}

// LastSeen returns the last heartbeat time, or the connection time.
func (s *Session) LastSeen() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSeen
}

// Dropped returns how many data.changed messages the queue shed.
func (s *Session) Dropped() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

// writeLoop drains the queue until the session closes.
func (s *Session) writeLoop(ctx context.Context) {
	defer func() {
		code, reason := s.closeCode, s.closeMsg
		if code == 0 {
			code = CloseNormal
		}
		_ = s.conn.Close(code, reason)
	}()
	for {
		select {
		case <-ctx.Done():
			s.close(CloseShutdown, "server shutting down")
			s.drain()
			return
		case <-s.closed:
			s.drain()
			return
		case msg := <-s.send:
			if err := s.write(ctx, msg.data); err != nil {
				s.close(CloseNormal, "write failed")
				return
			}
		}
	}
}

// drain flushes whatever is already queued so a final session.revoked or
// session.superseded reaches the client before the close frame.
func (s *Session) drain() {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), writeTimeout)
	defer cancel()
	for {
		select {
		case msg := <-s.send:
			if err := s.write(ctx, msg.data); err != nil {
				return
			}
		default:
			return
		}
	}
}

func (s *Session) write(ctx context.Context, data []byte) error {
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return s.conn.Write(wctx, websocket.MessageText, data)
}
