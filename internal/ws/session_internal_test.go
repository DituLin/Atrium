package ws

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestSession builds a session with no connection. Everything the send
// queue decides happens before a byte reaches the socket, so the policy is
// testable without one.
func newTestSession() *Session {
	now := func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }
	return newSession(nil, "living_room_tv", "Living room", "127.0.0.1", now)
}

// A client that stops reading must not make the server grow without bound: the
// 65th non-coalescable message closes the session with 4004 (design §4.1).
func TestSendQueueOverflowClosesWithFourZeroZeroFour(t *testing.T) {
	s := newTestSession()
	for i := 0; i < SendQueueSize; i++ {
		require.NoError(t, s.Send(TypeHeartbeatAck, HeartbeatAck{ServerTime: "now"}),
			"message %d should fit in the queue", i)
	}
	err := s.Send(TypeHeartbeatAck, HeartbeatAck{ServerTime: "now"})
	require.ErrorIs(t, err, ErrQueueFull)
	assert.True(t, s.Closed())
	assert.Equal(t, CloseOverflow, s.closeCode)
	assert.EqualValues(t, 4004, CloseOverflow)
}

// data.changed carries no state of its own — the client re-fetches — so under
// pressure it is dropped rather than costing the session its connection.
func TestDataChangedIsDroppedInsteadOfClosing(t *testing.T) {
	s := newTestSession()
	for i := 0; i < SendQueueSize; i++ {
		require.NoError(t, s.Send(TypeHeartbeatAck, HeartbeatAck{ServerTime: "now"}))
	}
	for i := 0; i < 10; i++ {
		require.NoError(t, s.Send(TypeDataChanged, DataChanged{Topics: []string{"photos"}, Version: int64(i)}))
	}
	assert.False(t, s.Closed(), "a droppable message must never close the session")
	assert.EqualValues(t, 10, s.Dropped())
}

func TestSendOnClosedSessionReportsClosed(t *testing.T) {
	s := newTestSession()
	s.close(CloseRevoked, "revoked")
	require.ErrorIs(t, s.Send(TypeHeartbeatAck, HeartbeatAck{}), ErrSessionClosed)
}

// The close codes are a wire contract with the web client (design §9).
func TestCloseCodesMatchTheProtocol(t *testing.T) {
	assert.EqualValues(t, 1000, CloseNormal)
	assert.EqualValues(t, 1001, CloseShutdown)
	assert.EqualValues(t, 4001, CloseUnauthorized)
	assert.EqualValues(t, 4002, CloseRevoked)
	assert.EqualValues(t, 4003, CloseSuperseded)
	assert.EqualValues(t, 4004, CloseOverflow)
	assert.EqualValues(t, 4005, CloseProtocolErr)
	assert.Equal(t, 64*1024, MaxMessageBytes)
	assert.Equal(t, 64, SendQueueSize)
}
