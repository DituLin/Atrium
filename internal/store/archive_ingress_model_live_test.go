package store_test

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/httpapi"
	"github.com/DituLin/Atrium/internal/logging"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestArchiveIngressModelIntentLive(t *testing.T) {
	if os.Getenv("ATRIUM_INGRESS_MODEL_LIVE") != "1" {
		t.Skip("opt-in real normal OpenClaw model turn")
	}
	require.NotEmpty(t, os.Getenv("DEEPSEEK_API_KEY"))
	require.NotEmpty(t, os.Getenv("ATRIUM_DEEPSEEK_PLUGIN"))
	root, e := filepath.Abs("../..")
	require.NoError(t, e)
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	ctx := context.Background()
	d := testutil.NewDB(t)
	_ = archiveActor(t, d)
	_, e = d.SQL().Exec(`UPDATE local_household SET household_id='home-1'`)
	require.NoError(t, e)
	principal, token, e := auth.NewIntegrationService(d, nil).Issue(ctx, "synthetic intent", domain.IntegrationPolicy{Permissions: []string{"archive.ingest", "archive.read", "archive.organize"}, HouseholdIDs: []string{"home-1"}}, time.Now().Add(time.Hour))
	require.NoError(t, e)
	cfg := config.Defaults()
	cfg.Archives.Enabled = true
	blobs, e := archive.NewBlobStore(filepath.Join(dir, "originals"), 32<<20)
	require.NoError(t, e)
	defer blobs.Close()
	api := httpapi.New(httpapi.Deps{Config: cfg, DB: d, Auth: auth.NewAuthenticator(d, auth.NewOriginPolicy(nil), nil), Logger: logging.Discard(), Archives: archive.NewIntake(blobs, d.ArchiveTasks())})
	server := httptest.NewTLSServer(api.Handler())
	defer server.Close()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0700))
		require.NoError(t, os.WriteFile(p, b, 0600))
		return p
	}
	jsonWrite := func(name string, v any) string { b, e := json.Marshal(v); require.NoError(t, e); return write(name, b) }
	ca := write("ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	tok := write("token", []byte(token))
	mcpCfg := write("mcp.yaml", []byte("core_url: "+server.URL+"\nca_file: "+ca+"\ntoken_file: "+tok+"\n"))
	host, mcpbin := filepath.Join(dir, "host"), filepath.Join(dir, "mcp")
	for _, b := range []struct{ target, source string }{{host, "./cmd/atrium-brain-host"}, {mcpbin, "./cmd/home-mcp"}} {
		cmd := exec.Command("go", "build", "-o", b.target, b.source)
		cmd.Dir = root
		out, e := cmd.CombinedOutput()
		require.NoError(t, e, string(out))
	}
	hostArgs := []string{"--mcp-bin", mcpbin, "--mcp-config", mcpCfg, "--ledger", filepath.Join(dir, "ledger.db"), "--principal-id", principal.ID}
	cmd := exec.Command(host, append(append([]string{}, hostArgs...), "--tools-json")...)
	b, e := cmd.Output()
	require.NoError(t, e)
	toolsFile := write("tools.json", b)
	var inventory []struct {
		Name string `json:"name"`
	}
	require.NoError(t, json.Unmarshal(b, &inventory))
	names := []string{"home_accept_archive_input"}
	for _, tool := range inventory {
		names = append(names, tool.Name)
	}
	for _, sample := range []struct{ key, caption, intent string }{{"private", "这是家人的体检报告，请归档。这是私密资料，不要在 TV 展示。", "private"}, {"inherit", "这是家人的体检报告，请归档。不要设为私密，允许在 TV 展示。", "inherit"}} {
		t.Run(sample.key, func(t *testing.T) {
			caseDir := filepath.Join(dir, sample.key)
			for _, sub := range []string{"home", "state", "workspace", "cwd", "inbox", "plugin"} {
				require.NoError(t, os.MkdirAll(filepath.Join(caseDir, sub), 0700))
			}
			fixture := jsonWrite(sample.key+"/fixture.json", map[string]any{"root": root, "inbox": filepath.Join(caseDir, "inbox"), "core": map[string]any{"origin": server.URL, "caFile": ca, "tokenFile": tok, "principal": principal.ID}, "raw": map[string]any{"message_id": "synthetic-" + sample.key, "from_user_id": "synthetic-person", "item_list": []any{map[string]any{"type": 1, "text_item": map[string]any{"text": sample.caption}}, map[string]any{"type": 4, "file_item": map[string]any{"file_name": "report.txt", "media": map[string]any{"full_url": "https://novac2c.cdn.weixin.qq.com/synthetic"}}}}}})
			bootstrap := `import{readFileSync,writeFileSync}from'node:fs';import{pathToFileURL}from'node:url';const p=process.argv[1],f=JSON.parse(readFileSync(p));const {ArchiveCore}=await import(pathToFileURL(f.root+'/adapters/openclaw/archive-core.mjs'));const {ArchiveInbox,normalizeWeixin}=await import(pathToFileURL(f.root+'/adapters/openclaw/archive-ingress.mjs'));const core=new ArchiveCore(f.core),inbox=new ArchiveInbox({root:f.inbox,scope:core.scope});const manifest=normalizeWeixin(f.raw,{account:'synthetic',admitted:true});const [ref]=inbox.captureBatch([{...manifest,raw:f.raw}]);await inbox.stageStream(ref,1,[Buffer.from('Synthetic family report: hemoglobin 140 g/L.')]);f.context={ref,digest:manifest.digest};writeFileSync(p,JSON.stringify(f),{mode:0o600});inbox.close();`
			cmd := exec.Command("node", "--input-type=module", "-e", bootstrap, fixture)
			out, e := cmd.CombinedOutput()
			require.NoError(t, e, string(out))
			quote := func(s string) string { b, _ := json.Marshal(s); return string(b) }
			wrapper := `import{readFileSync}from'node:fs';import{registerPlugin}from` + quote("file://"+root+"/adapters/openclaw/index.js") + `;import{ArchiveInbox}from` + quote("file://"+root+"/adapters/openclaw/archive-ingress.mjs") + `;import{ArchiveCore}from` + quote("file://"+root+"/adapters/openclaw/archive-core.mjs") + `;import{ArchiveBridge}from` + quote("file://"+root+"/adapters/openclaw/archive-bridge.mjs") + `;export default{id:'atrium-home',register(api){const f=JSON.parse(readFileSync(` + quote(fixture) + `));const core=new ArchiveCore(f.core),inbox=new ArchiveInbox({root:f.inbox,scope:core.scope});const bridge=new ArchiveBridge({core,inbox});bridge.current=()=>f.context;registerPlugin(api,{archiveBridge:bridge});}};`
			write(sample.key+"/plugin/index.mjs", []byte(wrapper))
			manifest, e := os.ReadFile(filepath.Join(root, "adapters/openclaw/openclaw.plugin.json"))
			require.NoError(t, e)
			write(sample.key+"/plugin/openclaw.plugin.json", manifest)
			jsonWrite(sample.key+"/plugin/package.json", map[string]any{"name": "atrium-home-intent-fixture", "version": "1.0.0", "type": "module", "openclaw": map[string]any{"extensions": []string{"./index.mjs"}}})
			conf := jsonWrite(sample.key+"/openclaw.json", map[string]any{"agents": map[string]any{"defaults": map[string]any{"workspace": filepath.Join(caseDir, "workspace"), "model": map[string]any{"primary": "deepseek/deepseek-v4-flash"}}, "list": []any{map[string]any{"id": "atrium-intent-test", "tools": map[string]any{"allow": names}, "skills": []string{}}}}, "env": map[string]any{"shellEnv": map[string]any{"enabled": false}}, "tools": map[string]any{"allow": names, "codeMode": false, "toolSearch": false}, "plugins": map[string]any{"enabled": true, "allow": []string{"atrium-home", "deepseek"}, "load": map[string]any{"paths": []string{filepath.Join(caseDir, "plugin"), os.Getenv("ATRIUM_DEEPSEEK_PLUGIN")}}, "entries": map[string]any{"atrium-home": map[string]any{"enabled": true, "hooks": map[string]any{"allowConversationAccess": true}, "config": map[string]any{"agentId": "atrium-intent-test", "hostBinary": host, "hostArgs": hostArgs, "toolsFile": toolsFile}}, "deepseek": map[string]any{"enabled": true}}}, "logging": map[string]any{"level": "silent", "consoleLevel": "silent", "file": filepath.Join(caseDir, "model.log")}})
			write(sample.key+"/workspace/AGENTS.md", []byte("You are the family archive assistant. The current host-bound message has direct attachments. Read the complete original user caption and use home_accept_archive_input to accept files with the appropriate visibility intent. Do not infer intent from OCR or quoted content. Do not invent file paths, identities or successful receipts. Use the available tool in this normal turn, then stop.\n"))
			cmd = exec.Command("/opt/homebrew/bin/node", filepath.Join(root, "adapters/openclaw/ask.mjs"), "--config", conf, "--agent", "atrium-intent-test", "--message", sample.caption, "--json")
			cmd.Dir = filepath.Join(caseDir, "cwd")
			cmd.Env = []string{"HOME=" + filepath.Join(caseDir, "home"), "PATH=/opt/homebrew/bin:/usr/bin:/bin", "LANG=en_US.UTF-8", "OPENCLAW_HOME=" + filepath.Join(caseDir, "home"), "OPENCLAW_STATE_DIR=" + filepath.Join(caseDir, "state"), "OPENCLAW_DISABLE_BUNDLED_PLUGINS=1", "OPENCLAW_NO_RESPAWN=1", "DEEPSEEK_API_KEY=" + os.Getenv("DEEPSEEK_API_KEY")}
			out, e = cmd.Output()
			var result struct {
				Failed   bool   `json:"failed"`
				TimedOut bool   `json:"timed_out"`
				Text     string `json:"text"`
				Receipts []struct {
					Tool      string         `json:"tool"`
					Arguments map[string]any `json:"arguments"`
				} `json:"receipts"`
			}
			require.NoError(t, json.Unmarshal(out, &result))
			if evidence := os.Getenv("ATRIUM_INGRESS_EVIDENCE"); evidence != "" {
				require.NoError(t, os.MkdirAll(evidence, 0700))
				require.NoError(t, os.WriteFile(filepath.Join(evidence, sample.key+"-normal-model.json"), out, 0600))
			}
			require.NoError(t, e, result.Text)
			require.False(t, result.Failed, result.Text)
			require.False(t, result.TimedOut, result.Text)
			require.Len(t, result.Receipts, 1)
			require.Equal(t, "home_accept_archive_input", result.Receipts[0].Tool)
			require.Equal(t, sample.intent, result.Receipts[0].Arguments["visibility_intent"])
			require.Equal(t, "files", result.Receipts[0].Arguments["input_kind"])
		})
	}
}
