package ws_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/ws"
)

// readyWriteGate holds the server's Write after ready reaches the client,
// while the WebSocket library still has its write-context cancellation armed.
type readyWriteGate struct {
	written     chan struct{}
	release     chan struct{}
	closed      chan struct{}
	writtenOnce sync.Once
	releaseOnce sync.Once
	closedOnce  sync.Once
}

func (g *readyWriteGate) unblock() { g.releaseOnce.Do(func() { close(g.release) }) }

type gatedReadyListener struct {
	net.Listener
	gate *readyWriteGate
}

func (l *gatedReadyListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &gatedReadyConn{Conn: conn, gate: l.gate}, nil
}

type gatedReadyConn struct {
	net.Conn
	gate *readyWriteGate
}

func (c *gatedReadyConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if err == nil && bytes.Contains(p[:n], []byte(`"type":"session.ready"`)) {
		c.gate.writtenOnce.Do(func() { close(c.gate.written) })
		select {
		case <-c.gate.release:
		case <-c.gate.closed:
		}
	}
	return n, err
}

func (c *gatedReadyConn) Close() error {
	c.gate.closedOnce.Do(func() { close(c.gate.closed) })
	return c.Conn.Close()
}

func TestProtocolCloseWaitsForReadyWriteToFinish(t *testing.T) {
	f := newFixture(t)
	f.server.Close()
	gate := &readyWriteGate{written: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
	server := httptest.NewUnstartedServer(f.server.Config.Handler)
	server.Listener = &gatedReadyListener{Listener: server.Listener, gate: gate}
	server.Start()
	t.Cleanup(server.Close)
	// Release independently of the handler/close handshake, including on failure.
	t.Cleanup(gate.unblock)
	f.server = server

	conn, _, err := f.dial(withBearer(f.token))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })
	require.Equal(t, ws.TypeSessionReady, readEnvelope(t, conn).Type)
	select {
	case <-gate.written:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not reach the ready write gate")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte("{not json")))
	require.Eventually(t, func() bool { return !f.hub.Online("living_room_tv") }, 3*time.Second, time.Millisecond,
		"server should classify the malformed message while ready Write is still returning")

	// Keep the already-written ready frame in flight long enough to observe
	// cancellation closing TCP before the writer can send the protocol close.
	prematureClose := false
	select {
	case <-gate.closed:
		prematureClose = true
	case <-time.After(100 * time.Millisecond):
	}
	gate.unblock()
	assert.False(t, prematureClose, "transport closed before the ready Write finished")

	readCtx, readCancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer readCancel()
	_, _, err = conn.Read(readCtx)
	require.Error(t, err)
	assert.Equal(t, coderws.StatusCode(4005), coderws.CloseStatus(err), "unexpected close: %v", err)
}

func readServerFrame(t *testing.T, reader *bufio.Reader) (byte, []byte) {
	t.Helper()
	var header [2]byte
	_, err := io.ReadFull(reader, header[:])
	require.NoError(t, err)
	require.Zero(t, header[1]&0x80, "server frames must not be masked")
	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		var size [2]byte
		_, err = io.ReadFull(reader, size[:])
		require.NoError(t, err)
		length = uint64(binary.BigEndian.Uint16(size[:]))
	case 127:
		var size [8]byte
		_, err = io.ReadFull(reader, size[:])
		require.NoError(t, err)
		length = binary.BigEndian.Uint64(size[:])
	}
	require.LessOrEqual(t, length, uint64(ws.MaxMessageBytes))
	payload := make([]byte, int(length))
	_, err = io.ReadFull(reader, payload)
	require.NoError(t, err)
	return header[0] & 0x0f, payload
}

func upgradeRequest(t *testing.T, address, token string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, address+"/api/v1/screens/connect", nil)
	require.NoError(t, err)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Sec-WebSocket-Version", "13")
	request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

func TestUpgradeWithoutHijackerKeepsNotImplementedResponse(t *testing.T) {
	f := newFixture(t)
	response := httptest.NewRecorder()
	f.hub.ServeHTTP(response, upgradeRequest(t, f.server.URL, f.token))
	require.Equal(t, http.StatusNotImplemented, response.Code)
	assert.False(t, f.hub.Online("living_room_tv"))
}

type failedHijackWriter struct {
	http.ResponseWriter
	conn    net.Conn
	flushed bool
}

func (w *failedHijackWriter) WriteHeaderNow() { w.flushed = true }

func (w *failedHijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, nil, errors.New("synthetic hijack failure")
}

func TestUpgradeFailureClosesCapturedTransportAndPreservesFlush(t *testing.T) {
	f := newFixture(t)
	serverConn, peer := net.Pipe()
	t.Cleanup(func() { _ = serverConn.Close(); _ = peer.Close() })
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(3*time.Second)))
	response := httptest.NewRecorder()
	writer := &failedHijackWriter{ResponseWriter: response, conn: serverConn}
	f.hub.ServeHTTP(writer, upgradeRequest(t, f.server.URL, f.token))
	assert.True(t, writer.flushed, "Accept's existing WriteHeaderNow hook must remain visible")
	assert.Contains(t, response.Body.String(), http.StatusText(http.StatusInternalServerError))
	_, err := peer.Read(make([]byte, 1))
	require.ErrorIs(t, err, io.EOF, "a failed upgrade must release any captured connection")
	assert.False(t, f.hub.Online("living_room_tv"))
}

// Partial rejected frames must not pin either the HTTP handler or Hub shutdown.
// Reading raw TLS frames avoids replying to the server's close handshake, and
// the client deliberately withholds the rest of its advertised payload.
func TestPartialRejectedFrameCannotPinShutdown(t *testing.T) {
	for _, tc := range []struct {
		name           string
		opcode         byte
		declared, sent int
	}{
		{name: "binary", opcode: 2, declared: 8, sent: 1},
		{name: "oversized-text", opcode: 1, declared: ws.MaxMessageBytes + 2, sent: ws.MaxMessageBytes + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.server.Close()
			requestDone := make(chan struct{})
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(requestDone)
				f.hub.ServeHTTP(w, r)
			}))
			server.StartTLS()
			t.Cleanup(server.Close)
			transport, ok := server.Client().Transport.(*http.Transport)
			require.True(t, ok)
			conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", server.Listener.Addr().String(), transport.TLSClientConfig)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			require.NoError(t, conn.SetDeadline(time.Now().Add(3*time.Second)))
			request := upgradeRequest(t, server.URL, f.token)
			require.NoError(t, request.Write(conn))
			reader := bufio.NewReader(conn)
			response, err := http.ReadResponse(reader, request)
			require.NoError(t, err)
			require.Equal(t, http.StatusSwitchingProtocols, response.StatusCode)
			opcode, ready := readServerFrame(t, reader)
			require.Equal(t, byte(1), opcode)
			require.Contains(t, string(ready), `"type":"session.ready"`)

			frame := []byte{0x80 | tc.opcode}
			if tc.declared < 126 {
				frame = append(frame, 0x80|byte(tc.declared))
			} else {
				frame = append(frame, 0x80|127)
				frame = binary.BigEndian.AppendUint64(frame, uint64(tc.declared))
			}
			mask := [4]byte{1, 2, 3, 4}
			frame = append(frame, mask[:]...)
			for i := 0; i < tc.sent; i++ {
				frame = append(frame, 'a'^mask[i%len(mask)])
			}
			_, err = conn.Write(frame)
			require.NoError(t, err)
			opcode, closing := readServerFrame(t, reader)
			require.Equal(t, byte(8), opcode)
			require.GreaterOrEqual(t, len(closing), 2)
			require.EqualValues(t, 4005, binary.BigEndian.Uint16(closing[:2]))
			// No remaining payload or close reply is sent; only the server's
			// teardown cap can complete this still-open TLS connection.
			require.NoError(t, conn.SetDeadline(time.Time{}))
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			hubDone := make(chan struct{})
			go func() { f.hub.Run(ctx); close(hubDone) }()
			cancel()
			// Match Core's shutdown budget: the separate 5 s teardown cap must
			// release both the HTTP handler and Hub within these same 10 s.
			finishCtx, finishCancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer finishCancel()
			for _, done := range []<-chan struct{}{requestDone, hubDone} {
				select {
				case <-done:
				case <-finishCtx.Done():
					t.Fatal("partial peer payload pinned the HTTP handler or Hub shutdown")
				}
			}
		})
	}
}
