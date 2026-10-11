package store_test

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/httpapi"
	"github.com/DituLin/Atrium/internal/logging"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
)

// Real local TLS + Core SQL + native Node inbox, using only synthetic records.
func TestArchiveIngressRealCoreAndHostInventory(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node required")
	}
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	d := testutil.NewDB(t)
	_ = archiveActor(t, d)
	_, err = d.SQL().Exec(`UPDATE local_household SET household_id='home-1'`)
	require.NoError(t, err)
	principal, token, err := auth.NewIntegrationService(d, nil).Issue(context.Background(), "synthetic ingress", domain.IntegrationPolicy{Permissions: []string{"archive.ingest"}, HouseholdIDs: []string{"home-1"}}, time.Now().Add(time.Hour))
	require.NoError(t, err)
	cfg := config.Defaults()
	cfg.Archives.Enabled = true
	blobs, err := archive.NewBlobStore(filepath.Join(dir, "originals"), 32<<20)
	require.NoError(t, err)
	defer blobs.Close()
	api := httpapi.New(httpapi.Deps{Config: cfg, DB: d, Auth: auth.NewAuthenticator(d, auth.NewOriginPolicy(nil), nil), Logger: logging.Discard(), Archives: archive.NewIntake(blobs, d.ArchiveTasks())})
	server := httptest.NewTLSServer(api.Handler())
	defer server.Close()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, b, 0600))
		return p
	}
	ca := write("ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	tok := write("token", []byte(token))
	script := `import assert from 'node:assert/strict';import {mkdirSync} from 'node:fs';import {pathToFileURL} from 'node:url';
 const [root,dir,origin,caFile,tokenFile,principal]=process.argv.slice(1);
 const {ArchiveCore}=await import(pathToFileURL(root+'/adapters/openclaw/archive-core.mjs'));
 const {ArchiveInbox,normalizeDiscord,normalizeWeixin}=await import(pathToFileURL(root+'/adapters/openclaw/archive-ingress.mjs'));
 const core=new ArchiveCore({origin,caFile,tokenFile,principal});mkdirSync(dir+'/inbox',{mode:0o700});const inbox=new ArchiveInbox({root:dir+'/inbox',scope:core.scope});
 try{const bytes=Buffer.from('Synthetic family archive report.');const wx=normalizeWeixin({message_id:'wx-1',from_user_id:'synthetic-person',item_list:[{type:4,file_item:{file_name:'report.txt',media:{full_url:'https://novac2c.cdn.weixin.qq.com/test'}}}]},{account:'synthetic',admitted:true});
 const dc=normalizeDiscord({id:'123456789012345678',channel_id:'223456789012345678',author:{id:'323456789012345678'},attachments:[{id:'423456789012345678',filename:'report.txt',url:'https://cdn.discordapp.com/test'}]},{account:'synthetic',admitted:true});
 const refs=inbox.captureBatch([wx,dc]);await inbox.stageStream(refs[0],0,[bytes]);await inbox.stageStream(refs[1],0,[bytes]);await inbox.reconcile(core,{force:true});
 for(const ref of refs){const s=inbox.describe(ref).slots[0];assert.equal(s.state,'core_saved',JSON.stringify(s));assert.equal(s.receipt.original_status,'saved');assert.equal(Object.hasOwn(s.receipt,'document_id'),false);}
 const privateMessage=normalizeWeixin({message_id:'wx-private',from_user_id:'synthetic-person',item_list:[{type:1,text_item:{text:'这是私密档案'}},{type:4,file_item:{file_name:'private.txt',media:{full_url:'https://novac2c.cdn.weixin.qq.com/test'}}}]},{account:'synthetic',admitted:true});const [p]=inbox.captureBatch([privateMessage]);await inbox.stageStream(p,1,[bytes]);inbox.setIntent(p,'private',privateMessage.digest);await inbox.reconcile(core,{force:true});assert.equal(inbox.describe(p).slots[0].error,'archive_forbidden');assert.equal(inbox.describe(p).intent,'private');
 console.log(JSON.stringify({saved:refs.length,private_denied:true}));}finally{inbox.close();}`
	cmd := exec.Command("node", "--input-type=module", "-e", script, root, dir, server.URL, ca, tok, principal.ID)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), `"saved":2`)
	var docs int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_documents`).Scan(&docs))
	require.Equal(t, 1, docs, "same bytes retain two sources and one document")
	mcpCfg := write("mcp.yaml", []byte("core_url: "+server.URL+"\nca_file: "+ca+"\ntoken_file: "+tok+"\n"))
	host, mcpbin := filepath.Join(dir, "host"), filepath.Join(dir, "mcp")
	for _, b := range []struct{ target, source string }{{host, "./cmd/atrium-brain-host"}, {mcpbin, "./cmd/home-mcp"}} {
		cmd := exec.Command("go", "build", "-o", b.target, b.source)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	cmd = exec.Command(host, "--mcp-bin", mcpbin, "--mcp-config", mcpCfg, "--tools-json")
	out, err = cmd.Output()
	require.NoError(t, err)
	var tools []struct {
		Name string `json:"name"`
	}
	require.NoError(t, json.Unmarshal(out, &tools))
	require.Len(t, tools, 21)
	found := false
	for _, tool := range tools {
		if tool.Name == "home_analyze_archives" {
			found = true
		}
	}
	require.True(t, found)
}
