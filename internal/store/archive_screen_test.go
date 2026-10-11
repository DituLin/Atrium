package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestScreenArchiveDetailAndOriginalCurrentAuthority(t *testing.T) {
	d := testutil.NewDB(t)
	ctx := context.Background()
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	rec, f, x := publicationFixture(t, d, r, "screen-detail", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, publicationBundle(t, "70"))
	_, err := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, err)
	_, err = d.SQL().Exec(`UPDATE local_household SET household_id='home-1'`)
	require.NoError(t, err)
	require.NoError(t, d.Screens().Create(ctx, &domain.Screen{ID: "tv", Name: "TV", TokenHash: "hash"}))
	detail, err := r.ArchiveDocumentForScreen(ctx, "tv", "hash", "home-1", rec.DocumentID)
	require.NoError(t, err)
	require.True(t, detail.OriginalSaved)
	require.Equal(t, "pending", detail.NASStatus)
	require.Equal(t, "ready", detail.Processing)
	require.Equal(t, "parsed", detail.TaskSummary.Parse)
	require.Equal(t, "complete", detail.TaskSummary.Understanding)
	raw, err := json.Marshal(detail)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "task_status")
	require.NotContains(t, string(raw), "provider_message")
	_, err = r.ArchiveOriginalForScreen(ctx, "tv", "hash", "home-1", rec.DocumentID, detail.Item.DocumentRevision)
	require.NoError(t, err)
	_, err = r.ArchiveOriginalForScreen(ctx, "tv", "hash", "home-1", rec.DocumentID, 1)
	require.Error(t, err)
	_, err = r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, detail.Item.DocumentRevision, store.ArchiveRevision{Kind: "private", ObservationID: detail.Item.Evidence[0].ObservationID, Reason: "test"})
	require.NoError(t, err)
	next, err := r.ArchiveDocumentForScreen(ctx, "tv", "hash", "home-1", rec.DocumentID)
	require.NoError(t, err)
	require.Equal(t, "restricted", next.Preview)
	_, err = r.ArchiveOriginalForScreen(ctx, "tv", "hash", "home-1", rec.DocumentID, next.Item.DocumentRevision)
	require.Error(t, err)
	require.NoError(t, d.Screens().Revoke(ctx, "tv", archiveNow))
	_, err = r.ArchiveDocumentForScreen(ctx, "tv", "hash", "home-1", rec.DocumentID)
	require.Error(t, err)
}

func TestArchiveScreenViewRequiresBoundQueryReceipt(t *testing.T) {
	d := testutil.NewDB(t)
	ctx := context.Background()
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	_, e := d.SQL().Exec(`UPDATE local_household SET household_id='home-1'`)
	require.NoError(t, e)
	require.NoError(t, d.Screens().Create(ctx, &domain.Screen{ID: "tv", Name: "TV", TokenHash: "hash"}))
	page, e := r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Domain: "finance"}, []byte("key"))
	require.NoError(t, e)
	require.NotEmpty(t, page.QueryRef)
	_, e = r.CreateArchiveScreenView(ctx, a, "home-1", page.QueryRef, "tv", domain.NewID())
	require.Error(t, e, "read-only principal cannot show")
	principal, e := d.Integrations().Get(ctx, a.PrincipalID)
	require.NoError(t, e)
	principal.Policy.Permissions = append(principal.Policy.Permissions, "screens.control")
	principal.Policy.ScreenIDs = []string{"tv"}
	require.NoError(t, d.Integrations().UpdatePolicy(ctx, principal.ID, principal.Policy, archiveNow))
	operation := domain.NewID()
	view, e := r.CreateArchiveScreenView(ctx, a, "home-1", page.QueryRef, "tv", operation)
	require.NoError(t, e)
	again, e := r.CreateArchiveScreenView(ctx, a, "home-1", page.QueryRef, "tv", operation)
	require.NoError(t, e)
	require.Equal(t, view.ID, again.ID)
	filter, e := r.ArchiveScreenViewFilter(ctx, "tv", "hash", "home-1", view.ID)
	require.NoError(t, e)
	require.Equal(t, "finance", filter.Domain)
	_, e = r.ArchiveScreenViewFilter(ctx, "other", "hash", "home-1", view.ID)
	require.Error(t, e)
	_, e = r.CreateArchiveScreenView(ctx, a, "home-1", "invented", "tv", domain.NewID())
	require.Error(t, e)
	principal.Policy.Permissions = []string{"archive.read"}
	require.NoError(t, d.Integrations().UpdatePolicy(ctx, principal.ID, principal.Policy, archiveNow))
	_, e = r.ArchiveScreenViewFilter(ctx, "tv", "hash", "home-1", view.ID)
	require.Error(t, e)
}

func TestArchiveAnalysisReceiptTVRecomputesPublicCurrencyGroups(t *testing.T) {
	d := testutil.NewDB(t)
	ctx := context.Background()
	c := &archiveClock{now: archiveNow}
	r := d.ArchiveTasksWithClock(c)
	editor := archiveActor(t, d)
	actor := newArchiveActor(t, d, "archive-view", []string{"archive.read", "archive.analyze", "screens.control"}, []string{"home-1"})
	p, e := d.Integrations().Get(ctx, actor.PrincipalID)
	require.NoError(t, e)
	p.Policy.ScreenIDs = []string{"tv"}
	_, e = d.SQL().Exec(`UPDATE local_household SET household_id='home-1'`)
	require.NoError(t, e)
	require.NoError(t, d.Screens().Create(ctx, &domain.Screen{ID: "tv", Name: "TV", TokenHash: "hash"}))
	require.NoError(t, d.Integrations().UpdatePolicy(ctx, p.ID, p.Policy, archiveNow))
	for i, b := range []store.ArchivePublicationBundle{analysisCSVBundle(t), analysisLabeledBundle(t, "费用", "50.00", "CNY"), analysisLabeledBundle(t, "费用", "20.00", "USD")} {
		rec, f, x := publicationFixture(t, d, r, fmt.Sprintf("tv-currency-%d", i), domain.ArchiveInputText)
		f = saveBundle(t, r, f, b)
		_, e = r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
		if i == 1 {
			_, e = r.ReviseArchive(ctx, editor, "home-1", rec.DocumentID, 2, store.ArchiveRevision{Kind: "private", Reason: "synthetic private"})
			require.NoError(t, e)
		}
	}
	receipt, e := r.AnalyzeArchives(ctx, actor, "home-1", false, archive.AnalysisQuery{Operation: "sum", Metric: "amount", Timezone: "Asia/Singapore"})
	require.NoError(t, e)
	require.Equal(t, "250.00", receipt.Groups[0].Value)
	view, e := r.CreateArchiveScreenView(ctx, actor, "home-1", receipt.QueryRef, "tv", domain.NewID())
	require.NoError(t, e)
	query, e := r.ArchiveScreenViewAnalysis(ctx, "tv", "hash", "home-1", view.ID)
	require.NoError(t, e)
	require.NotNil(t, query)
	require.Equal(t, "sum", query.Operation)
	public, e := r.AnalyzeArchivesForScreen(ctx, "tv", "hash", "home-1", *query)
	require.NoError(t, e)
	require.Equal(t, "200.00", public.Groups[0].Value)
	require.Equal(t, "CNY", public.Groups[0].Currency)
	require.Equal(t, "20.00", public.Groups[1].Value)
	require.Equal(t, "USD", public.Groups[1].Currency)
	raw, _ := json.Marshal(public)
	require.NotContains(t, string(raw), "250.00")
	require.NotContains(t, string(raw), "50.00")
	foreign := newArchiveActor(t, d, "foreign", []string{"archive.read", "screens.control"}, []string{"home-1"})
	fp, e := d.Integrations().Get(ctx, foreign.PrincipalID)
	require.NoError(t, e)
	fp.Policy.ScreenIDs = []string{"tv"}
	require.NoError(t, d.Integrations().UpdatePolicy(ctx, fp.ID, fp.Policy, archiveNow))
	_, e = r.CreateArchiveScreenView(ctx, foreign, "home-1", receipt.QueryRef, "tv", domain.NewID())
	require.Error(t, e)
	c.now = c.now.Add(3 * time.Minute)
	_, e = r.ArchiveScreenViewAnalysis(ctx, "tv", "hash", "home-1", view.ID)
	require.Error(t, e)
	c.now = c.now.Add(8 * time.Minute)
	_, e = r.CreateArchiveScreenView(ctx, actor, "home-1", receipt.QueryRef, "tv", domain.NewID())
	require.Error(t, e)
}

func TestScreenTerminalSummaryBypassesStaleSearchIndex(t *testing.T) {
	d := testutil.NewDB(t)
	ctx := context.Background()
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	rec, f, _ := publicationFixture(t, d, r, "terminal-summary", domain.ArchiveInputFile)
	_, e := indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	_, e = r.Finish(ctx, "home-1", f.TaskID, f.Token, domain.ArchiveTaskFailed, domain.ArchiveTaskUpdate{ExpectedStateVersion: f.StateVersion, Stage: "processing", StateJSON: json.RawMessage(`{}`), ResultJSON: json.RawMessage(`{}`), LastErrorCode: "parse_status_encrypted"})
	require.NoError(t, e)
	_, e = d.SQL().Exec(`UPDATE local_household SET household_id='home-1'`)
	require.NoError(t, e)
	require.NoError(t, d.Screens().Create(ctx, &domain.Screen{ID: "tv", Name: "TV", TokenHash: "hash"}))
	detail, e := r.ArchiveDocumentForScreen(ctx, "tv", "hash", "home-1", rec.DocumentID)
	require.NoError(t, e)
	page, e := r.SearchArchivesForScreen(ctx, "tv", "hash", "home-1", archive.SearchFilter{}, []byte("test"))
	require.NoError(t, e)
	require.Len(t, page.Items, 1)
	require.Equal(t, "saved_only", detail.Processing)
	require.Equal(t, detail.Processing, page.Items[0].Processing)
	require.Equal(t, detail.TaskSummary, *page.Items[0].TaskSummary)
	require.True(t, page.Items[0].OriginalSaved)
	wire, _ := json.Marshal(page)
	for _, secret := range []string{"task_status", "parse_status_encrypted", "failed", "provider_message"} {
		require.NotContains(t, string(wire), secret)
	}
}
