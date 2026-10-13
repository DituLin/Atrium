package store_test

import (
	"context"
	"encoding/json"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestAnalysisPostfixQualifiers(t *testing.T) {
	for _, tc := range []struct {
		text                   string
		qualified, denominator bool
	}{
		{"70左右 ml serum CNY 100 2026-10-04", true, false},
		{"70以上 ml serum CNY 100 2026-10-04", true, false},
		{"70 以下 ml serum CNY 100 2026-10-04", true, false},
		{"70ml左右 serum CNY 100 2026-10-04", true, false},
		{"70 ml 以上 serum CNY 100 2026-10-04", true, false},
		{"70 ml以下 serum CNY 100 2026-10-04", true, false},
		{"70 ml serum CNY 100左右 2026-10-04", true, true},
		{"70 ml serum CNY 100分左右 2026-10-04", true, true},
		{"70 ml serum CNY 100 分以下 2026-10-04", true, true},
		{"70 ml serum CNY 100 以下 2026-10-04", true, true},
		{"70;左右 ml serum CNY 100 2026-10-04", false, false},
		{"70 ml;左右 serum CNY 100 2026-10-04", false, false},
		{"70 ml\n左右 serum CNY 100 2026-10-04", false, false},
		{"70 ml，左右 serum CNY 100 2026-10-04", false, false},
		{"70 ml sample:左右 serum CNY 100 2026-10-04", false, false},
		{"70 ml serum左右 CNY 100 2026-10-04", false, false},
	} {
		t.Run(tc.text, func(t *testing.T) {
			d := testutil.NewDB(t)
			ctx := context.Background()
			r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
			editor := archiveActor(t, d)
			a := newArchiveActor(t, d, "analyst", []string{"archive.read", "archive.analyze"}, []string{"home-1"})
			rec, f, x := publicationFixture(t, d, r, "suffix", domain.ArchiveInputText)
			f = saveBundle(t, r, f, analysisReviewTextBundle(t, tc.text))
			_, e := r.PublishExtraction(ctx, f, x.ID)
			require.NoError(t, e)
			q := archive.AnalysisQuery{Operation: "sum", Metric: "volume", Timezone: "UTC"}
			if tc.denominator {
				q.Operation = "ratio"
			}
			result, e := r.AnalyzeArchives(ctx, a, "home-1", false, q)
			require.NoError(t, e)
			if !tc.qualified {
				require.Len(t, result.Groups, 1)
				require.Equal(t, "70", result.Groups[0].Value)
				return
			}
			require.Empty(t, result.Groups)
			require.False(t, result.Coverage.Complete)
			obs, e := r.CurrentArchiveObservations(ctx, editor, "home-1", rec.DocumentID, false)
			require.NoError(t, e)
			name, value := "volume", "80"
			if tc.denominator {
				name, value = "full_score", "100"
			}
			id := ""
			for _, o := range obs {
				var b struct{ Field archive.PublicationField }
				require.NoError(t, json.Unmarshal(o.Body, &b))
				if b.Field.Name == name {
					id = o.ID
				}
			}
			require.NotEmpty(t, id)
			_, e = r.ReviseArchive(ctx, editor, "home-1", rec.DocumentID, 2, store.ArchiveRevision{Kind: "value", ObservationID: id, Value: value, Reason: "explicit exact correction"})
			require.NoError(t, e)
			result, e = r.AnalyzeArchives(ctx, a, "home-1", false, q)
			require.NoError(t, e)
			require.Len(t, result.Groups, 1)
			expected := "80"
			if tc.denominator {
				expected = "70.00"
			}
			require.Equal(t, expected, result.Groups[0].Value)
			evidence, e := json.Marshal(result.Groups[0].Evidence)
			require.NoError(t, e)
			require.Contains(t, string(evidence), "assertion_id")
			require.True(t, strings.Contains(string(evidence), "70") || strings.Contains(string(evidence), "100"))
		})
	}
}
