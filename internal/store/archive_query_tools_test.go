package store_test

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/brain"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/homemcp"
	"github.com/DituLin/Atrium/internal/httpapi"
	"github.com/DituLin/Atrium/internal/logging"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveQueryToolsRealCoreTypedFencesAndPermissions(t *testing.T) {
	ctx := context.Background()
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	actor := archiveActor(t, d)
	normal, f, x := publicationFixture(t, d, r, "tools-normal", domain.ArchiveInputText)
	f = saveBundle(t, r, f, namedPublicationBundle(t, "林明"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	pending, f, x := publicationFixture(t, d, r, "tools-pending", domain.ArchiveInputText)
	f = saveBundle(t, r, f, namedPublicationBundle(t, "妈妈"))
	_, e = r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	_, e = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	token, e := auth.NewIntegrationToken()
	require.NoError(t, e)
	_, e = d.SQL().Exec(`UPDATE integration_credentials SET token_hash=? WHERE id=?`, auth.HashToken(token), actor.CredentialID)
	require.NoError(t, e)
	_, e = d.SQL().Exec(`UPDATE local_household SET household_id='home-1'`)
	require.NoError(t, e)
	cfg := config.Defaults()
	cfg.Archives.Enabled = true
	blobs, e := archive.NewBlobStore(filepath.Join(dir, "originals"), 32<<20)
	require.NoError(t, e)
	defer blobs.Close()
	api := httpapi.New(httpapi.Deps{Config: cfg, DB: d, Auth: auth.NewAuthenticator(d, auth.NewOriginPolicy(nil), nil), Logger: logging.Discard(), Archives: archive.NewIntake(blobs, d.ArchiveTasks())})
	server := httptest.NewTLSServer(api.Handler())
	defer server.Close()
	ca := filepath.Join(dir, "ca.pem")
	tok := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600))
	require.NoError(t, os.WriteFile(tok, []byte(token), 0600))
	ms, e := homemcp.NewServerWithLogger(homemcp.Config{CoreURL: server.URL, CAFile: ca, TokenFile: tok}, logging.Discard())
	require.NoError(t, e)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ss, e := ms.Connect(ctx, serverTransport, nil)
	require.NoError(t, e)
	defer ss.Close()
	client, e := mcp.NewClient(&mcp.Implementation{Name: "archive-test", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	require.NoError(t, e)
	defer client.Close()
	ledger, e := brain.OpenLedger(filepath.Join(dir, "ledger.db"), "query-tools", nil)
	require.NoError(t, e)
	defer ledger.Close()
	executor := brain.NewExecutorWithLogger(client, ledger, logging.Discard())
	call := func(name string, args map[string]any, bad bool) map[string]any {
		turn := executor.StartTurn(ctx)
		defer turn.Close()
		result, e := turn.Call("call", name, args)
		require.NoError(t, e)
		require.Equal(t, bad, result.IsError, "%s: %+v", name, result)
		raw, e := json.Marshal(result.StructuredContent)
		require.NoError(t, e)
		var out map[string]any
		require.NoError(t, json.Unmarshal(raw, &out))
		return out
	}
	search := call("home_search_archives", map[string]any{"query": "70"}, false)
	require.Equal(t, "archive-query/1", search["data"].(map[string]any)["schema_version"])
	entities := call("home_search_archive_entities", map[string]any{"query": "林明"}, false)
	require.NotEmpty(t, entities["data"].(map[string]any)["items"])
	evidence := call("home_get_archive_evidence", map[string]any{"document_id": normal.DocumentID}, false)
	item := evidence["data"].(map[string]any)["items"].([]any)[0].(map[string]any)
	var observation string
	for _, ev := range item["evidence"].([]any) {
		if id, ok := ev.(map[string]any)["observation_id"].(string); ok {
			observation = id
			break
		}
	}
	require.NotEmpty(t, observation)
	task := call("home_get_archive_task", map[string]any{"task_id": pending.TaskID}, false)
	require.Equal(t, "waiting_input", task["data"].(map[string]any)["result"].(map[string]any)["status"])
	question := call("home_get_archive_question", map[string]any{"task_id": pending.TaskID}, false)["data"].(map[string]any)["result"].(map[string]any)
	answer := map[string]any{"task_id": pending.TaskID, "action": "create_subject", "name": "妈妈"}
	for _, k := range []string{"question_id", "question_version", "waiting_task_version", "extraction_id", "document_revision"} {
		answer[k] = question[k]
	}
	stale := map[string]any{}
	for k, v := range answer {
		stale[k] = v
	}
	stale["document_revision"] = 999
	call("home_answer_archive_question", stale, true)
	replied := call("home_answer_archive_question", answer, false)
	require.Equal(t, "queued", replied["data"].(map[string]any)["result"].(map[string]any)["status"])
	revised := call("home_revise_archive", map[string]any{"document_id": normal.DocumentID, "document_revision": item["document_revision"], "kind": "value", "observation_id": observation, "value": "80", "reason": "user correction"}, false)
	require.Equal(t, float64(3), revised["data"].(map[string]any)["result"].(map[string]any)["document_revision"])
	// Policy revocation is observed on the next real read, including cached rows.
	_, e = d.SQL().Exec(`UPDATE integration_principals SET enabled=0 WHERE id=?`, actor.PrincipalID)
	require.NoError(t, e)
	call("home_search_archives", map[string]any{}, true)
}
