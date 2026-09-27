package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestMediaTicketIsBoundToVideoKeyAndExpiry(t *testing.T) {
	now := time.Date(2026, 9, 27, 8, 0, 0, 0, time.UTC)
	tickets := NewMediaTickets(func() time.Time { return now })
	ticket, expires := tickets.Issue("living_room_tv", "vid_a")
	require.Equal(t, now.Add(MediaTicketTTL), expires)

	screen, err := tickets.Verify(ticket, "vid_a")
	require.NoError(t, err)
	require.Equal(t, "living_room_tv", screen)

	code := func(err error) domain.ErrorCode {
		var e *domain.Error
		require.True(t, errors.As(err, &e), err)
		return e.Code
	}
	_, err = tickets.Verify(ticket, "vid_b")
	require.Equal(t, domain.CodeForbidden, code(err))

	parts := strings.Split(ticket, ".")
	forged := parts[0] + "." + parts[1] + "." + strings.Repeat("A", len(parts[2]))
	_, err = tickets.Verify(forged, "vid_a")
	require.Equal(t, domain.CodeUnauthorized, code(err))

	other := NewMediaTickets(func() time.Time { return now })
	_, err = other.Verify(ticket, "vid_a")
	require.Equal(t, domain.CodeUnauthorized, code(err), "a restart voids every ticket")

	for _, bad := range []string{"", "amt1", "amt1.x.y", "other." + parts[1] + "." + parts[2]} {
		_, err = tickets.Verify(bad, "vid_a")
		require.Equal(t, domain.CodeUnauthorized, code(err), bad)
	}

	now = expires
	_, err = tickets.Verify(ticket, "vid_a")
	require.Equal(t, domain.CodeUnauthorized, code(err))
}
