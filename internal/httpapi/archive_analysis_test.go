package httpapi_test

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/brain"
	"github.com/DituLin/Atrium/internal/clock"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/homemcp"
	"github.com/DituLin/Atrium/internal/httpapi"
	"github.com/DituLin/Atrium/internal/integration"
	"github.com/DituLin/Atrium/internal/logging"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArchiveAnalysisHTTPAuthorityAndStrictQuery(t *testing.T) {
	h := newArchiveHTTP(t, true, "archive.read", "archive.analyze")
	home, e := clock.NewHome("Asia/Singapore", nil)
	require.NoError(t, e)
	server := httptest.NewServer(httpapi.New(httpapi.Deps{Config: h.cfg, DB: h.db, Archives: archive.NewIntake(nil, h.db.ArchiveTasks()), Home: home, Auth: auth.NewAuthenticator(h.db, auth.NewOriginPolicy(nil), nil), Logger: logging.Discard()}).Handler())
	t.Cleanup(server.Close)
	for _, tc := range []struct {
		query  string
		status int
	}{{"operation=count&count=documents", 200}, {"operation=sum", 400}, {"operation=count&count=documents&timezone=UTC", 400}, {"operation=count&count=documents&public=true", 400}, {"operation=count&count=documents&cursor=old", 400}, {"operation=count&operation=sum&count=documents", 400}} {
		req, e := http.NewRequest("GET", server.URL+"/api/v1/integrations/archives/analysis?"+tc.query, nil)
		require.NoError(t, e)
		req.Header.Set("Authorization", "Bearer "+h.token)
		resp, e := server.Client().Do(req)
		require.NoError(t, e)
		require.Equal(t, tc.status, resp.StatusCode, tc.query)
		if resp.StatusCode == 200 {
			var env integration.Response[archive.AnalysisResult]
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
			require.Equal(t, "archive-analysis/1", env.Data.SchemaVersion)
			require.Equal(t, "Asia/Singapore", env.Data.Query.Timezone)
			require.Equal(t, "archived_only", env.Data.Coverage.Scope)
		}
		resp.Body.Close()
	}
}
func TestArchiveAnalysisActualMCPThroughBrain(t *testing.T) {
	ctx := context.Background()
	h := newArchiveHTTP(t, true, "archive.read", "archive.analyze")
	home, e := clock.NewHome("Asia/Singapore", nil)
	require.NoError(t, e)
	require.NoError(t, h.db.FamilyRecords().PutMember(ctx, domain.FamilyMember{ID: "baby", Name: "合成宝宝"}, time.Now()))
	for i, value := range []float64{330, 120} {
		record, _, err := h.db.FamilyRecords().Record(ctx, "synthetic", fmt.Sprint(i), domain.FamilyRecordInput{MemberID: "baby", Category: "growth", Title: "喂养", OccurredAt: time.Now(), DisplayOnTV: true, Measurements: []domain.FamilyMeasurement{{Metric: "volume", Value: value, Unit: "ml"}}}, time.Now())
		require.NoError(t, err)
		require.NoError(t, h.db.ProjectLegacyArchive(ctx, record.ID))
	}
	core := httptest.NewTLSServer(httpapi.New(httpapi.Deps{Config: h.cfg, DB: h.db, Home: home, Archives: archive.NewIntake(nil, h.db.ArchiveTasks()), Auth: auth.NewAuthenticator(h.db, auth.NewOriginPolicy(nil), nil), Logger: logging.Discard()}).Handler())
	t.Cleanup(core.Close)
	dir := t.TempDir()
	ca, secret := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: core.Certificate().Raw}), 0600))
	require.NoError(t, os.WriteFile(secret, []byte(h.token), 0600))
	server, e := homemcp.NewServer(homemcp.Config{CoreURL: core.URL, CAFile: ca, TokenFile: secret})
	require.NoError(t, e)
	a, b := mcp.NewInMemoryTransports()
	ss, e := server.Connect(ctx, b, nil)
	require.NoError(t, e)
	t.Cleanup(func() { _ = ss.Close() })
	client, e := mcp.NewClient(&mcp.Implementation{Name: "analysis-contract", Version: "1"}, nil).Connect(ctx, a, nil)
	require.NoError(t, e)
	t.Cleanup(func() { _ = client.Close() })
	tools, e := client.ListTools(ctx, nil)
	require.NoError(t, e)
	inventory, e := brain.ModelTools(tools.Tools)
	require.NoError(t, e)
	found := false
	for _, tool := range inventory {
		found = found || tool.Name == "home_analyze_archives"
	}
	require.True(t, found)
	require.NoError(t, os.Chmod(dir, 0700))
	ledger, e := brain.OpenLedger(filepath.Join(dir, "ledger.db"), "synthetic", nil)
	require.NoError(t, e)
	t.Cleanup(func() { _ = ledger.Close() })
	turn := brain.NewExecutor(client, ledger).StartTurn(ctx)
	defer turn.Close()
	result, e := turn.Call("analysis1", "home_analyze_archives", map[string]any{"operation": "sum", "metric": "volume"})
	require.NoError(t, e)
	require.False(t, result.IsError)
	raw, e := json.Marshal(result.StructuredContent)
	require.NoError(t, e)
	var env integration.Response[archive.AnalysisResult]
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Len(t, env.Data.Groups, 1)
	require.Equal(t, "450", env.Data.Groups[0].Value)
	require.Equal(t, "ml", env.Data.Groups[0].Unit)
	require.Len(t, env.Data.Groups[0].Evidence, 2)
}
