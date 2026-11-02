package httpapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryPageAdvancesPastScannedPhotosWhenSourceIsRevokedDuringRead(t *testing.T) {
	// The eligible ID pool was read before a source was revoked. ListByIDs
	// then correctly removes both photos in this page, but a later page still
	// contains another source. An empty visible page must not end the round.
	pool := []rankedMemory{{id: "revoked_first"}, {id: "revoked_second"}, {id: "still_authorized"}}
	position := memoryCursor{Version: 1, Day: "2026-09-05", Screen: "memory_tv", Inclusive: true}
	next := nextMemoryCursor(pool, 2, position)
	require.NotNil(t, next, "advance past the scanned window even when all page rows became unauthorized")
	decoded, err := decodeMemoryCursor(*next, position.Screen)
	require.NoError(t, err)
	assert.Equal(t, "revoked_second", decoded.Anchor)
	assert.False(t, decoded.Inclusive)
	assert.Equal(t, position.Day, decoded.Day)
	assert.Equal(t, position.Screen, decoded.Screen)
}

func TestMemoryPageAdvancesPastFilteredTailAndEndsOnlyAfterPool(t *testing.T) {
	pool := []rankedMemory{{id: "visible_first"}, {id: "revoked_tail"}, {id: "later"}}
	position := memoryCursor{Version: 1, Day: "2026-09-05", Screen: "memory_tv", Inclusive: true}
	next := nextMemoryCursor(pool, 2, position)
	require.NotNil(t, next)
	decoded, err := decodeMemoryCursor(*next, position.Screen)
	require.NoError(t, err)
	assert.Equal(t, "revoked_tail", decoded.Anchor, "filtered rows were already scanned and must not be scanned again")
	assert.Nil(t, nextMemoryCursor(pool, len(pool), position))
	assert.Nil(t, nextMemoryCursor(nil, 0, position))
	assert.Nil(t, nextMemoryCursor(pool, 0, position))
}
