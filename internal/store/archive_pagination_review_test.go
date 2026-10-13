package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestSpecLargeEntityPageProgress(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "d", true)
	in, p := entityPlan(t, d, "d", entityMention("a", "王宁", "2000-01-02"))
	id := applyEntity(t, d, in, p)["a"]
	for i := 0; i < 16; i++ {
		require.NoError(t, d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
			return archiveAttributePutIn(context.Background(), tx, "h", id, "display_alias", fmt.Sprint(i)+strings.Repeat("房", 200), "", "d", "", []string{"alias"}, true, false)
		}))
	}
	require.NoError(t, d.Screens().Create(context.Background(), &domain.Screen{ID: "spec-tv", Name: "TV", TokenHash: "hash"}))
	r := d.ArchiveTasks()
	first, e := r.SearchArchiveEntitiesForScreen(context.Background(), "spec-tv", "hash", "h", archive.SearchFilter{}, []byte("key"))
	require.NoError(t, e)
	second, e := r.SearchArchiveEntitiesForScreen(context.Background(), "spec-tv", "hash", "h", archive.SearchFilter{Cursor: first.NextCursor}, []byte("key"))
	require.NoError(t, e)
	t.Logf("first items=%d next=%t second items=%d cursor_same=%t coverage=%+v", len(first.Items), first.NextCursor != "", len(second.Items), first.NextCursor == second.NextCursor, first.Coverage)
	require.NotEmpty(t, first.Items, "legal entity must be readable and pagination must progress")
}

func TestArchiveSummaryBoundPreservesRequestedEntityAndOriginalEvidence(t *testing.T) {
	item := archive.SearchItem{DocumentID: "d", EntityIDs: []string{"wanted"}, Evidence: []archive.SearchEvidence{{Excerpt: strings.Repeat("字", 4000), SegmentID: "unchanged"}}}
	for i := 0; i < 400; i++ {
		item.EntityIDs = append(item.EntityIDs, strings.Repeat("e", 60)+fmt.Sprint(i))
		item.Dates = append(item.Dates, "2026-01-01")
	}
	original := item.Evidence[0]
	boundArchiveSummary(&item, "wanted")
	require.True(t, item.Truncated)
	require.True(t, item.MetadataOnly)
	require.Contains(t, item.EntityIDs, "wanted")
	require.Equal(t, "unchanged", original.SegmentID)
	require.Len(t, []rune(original.Excerpt), 4000)
	b, e := json.Marshal(item)
	require.NoError(t, e)
	require.LessOrEqual(t, len(b), 6<<10)
}
