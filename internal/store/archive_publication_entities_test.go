package store

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPublicationResolutionReusesPhysicalMentionLink(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "doc", true)
	in, p := entityPlan(t, d, "doc", entityMention("physical-name", "Alice", "2000-01-01"))
	first := applyEntity(t, d, in, p)
	in, p = entityPlan(t, d, "doc", entityMention("physical-name", "Alice", "2000-01-01"))
	second := applyEntity(t, d, in, p)
	require.Equal(t, first, second)
	var n int
	require.NoError(t, d.sql.QueryRow(`SELECT count(*) FROM archive_entity_links`).Scan(&n))
	require.Equal(t, 1, n)
}
