package store_test

import (
	"context"
	"encoding/json"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/clock"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/httpapi"
	"github.com/DituLin/Atrium/internal/logging"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestArchiveViewOwnershipScope(t *testing.T) {
	d := testutil.NewDB(t)
	ctx := context.Background()
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	_, f, x := publicationFixture(t, d, r, "confirmed-household", domain.ArchiveInputText)
	f = saveBundle(t, r, f, publicationBundle(t, "70"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	_, e = d.SQL().Exec(`UPDATE local_household SET household_id='home-1'`)
	require.NoError(t, e)
	token, e := auth.NewScreenToken()
	require.NoError(t, e)
	require.NoError(t, d.Screens().Create(ctx, &domain.Screen{ID: "tv", Name: "TV", TokenHash: auth.HashToken(token)}))
	p, e := d.Integrations().Get(ctx, a.PrincipalID)
	require.NoError(t, e)
	p.Policy.Permissions = append(p.Policy.Permissions, "screens.control", "archive.analyze")
	p.Policy.ScreenIDs = []string{"tv"}
	require.NoError(t, d.Integrations().UpdatePolicy(ctx, p.ID, p.Policy, archiveNow))
	_, e = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	r = d.ArchiveTasks()
	for _, scope := range []struct{ ownership, domain, from, want string }{{"waiting", "", "", "0"}, {"unassigned", "", "", "1"}, {"household", "", "", "0"}, {"unassigned", "finance", "", "0"}, {"unassigned", "", "2099-01-01", "0"}} {
		page, e := r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Ownership: scope.ownership, Domain: scope.domain, DateFrom: scope.from}, []byte("key"))
		require.NoError(t, e)
		analysis, e := r.AnalyzeArchives(ctx, a, "home-1", false, archive.AnalysisQuery{Operation: "count", Count: "documents", Ownership: scope.ownership, Domain: scope.domain, DateFrom: scope.from, Timezone: "Asia/Singapore"})
		require.NoError(t, e)
		require.Equal(t, scope.want, analysis.Groups[0].Value)
		saved, e := r.ArchiveAnalysisSnapshot(ctx, a, "home-1", false, analysis.SnapshotID)
		require.NoError(t, e)
		require.Equal(t, scope.ownership, saved.Query.Ownership)
		for _, queryRef := range []string{page.QueryRef, analysis.QueryRef} {
			v, e := r.CreateArchiveScreenView(ctx, a, "home-1", queryRef, "tv", domain.NewID())
			require.NoError(t, e)
			cfg := config.Defaults()
			cfg.Archives.Enabled = true
			home, e := clock.NewHome("Asia/Singapore", nil)
			require.NoError(t, e)
			api := httpapi.New(httpapi.Deps{Config: cfg, DB: d, Home: home, Archives: archive.NewIntake(nil, r), Auth: auth.NewAuthenticator(d, auth.NewOriginPolicy(nil), nil), Logger: logging.Discard()})
			req := httptest.NewRequest("GET", "https://core/api/v1/archives/views/"+v.ID, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			out := httptest.NewRecorder()
			api.Handler().ServeHTTP(out, req)
			require.Equal(t, 200, out.Code, out.Body.String())
			var wire struct {
				Data struct {
					Page     archive.SearchPage     `json:"page"`
					Analysis archive.AnalysisResult `json:"analysis"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(out.Body.Bytes(), &wire))
			t.Logf("scope=%+v list=%d", scope, len(wire.Data.Page.Items))
			require.Equal(t, scope.want, wire.Data.Analysis.Groups[0].Value)
			if scope.want == "0" {
				require.Empty(t, wire.Data.Page.Items)
				require.Empty(t, wire.Data.Analysis.Groups[0].Evidence)
			}
		}
	}
}
