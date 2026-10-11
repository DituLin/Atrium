// Command archives is an opt-in synthetic acceptance runner. It refuses any
// database except its own marker-guarded private directory. No model answers
// or archive products are seeded: only original bytes and transport metadata.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/archiveworker"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
)

type fixture struct {
	ID      string                  `json:"id"`
	File    string                  `json:"file"`
	Kind    domain.ArchiveInputKind `json:"kind"`
	Private bool                    `json:"private"`
	SHA256  string                  `json:"sha256"`
}
type state struct {
	Home     string                                 `json:"home"`
	Actor    domain.IntegrationActor                `json:"actor"`
	Token    string                                 `json:"token"`
	Receipts map[string]domain.ArchiveIntakeReceipt `json:"receipts"`
}

const marker = "Atrium synthetic archive acceptance v1\n"

func must(e error) {
	if e != nil {
		panic(e)
	}
}
func writeJSON(path string, v any) {
	b, e := json.MarshalIndent(v, "", "  ")
	must(e)
	must(os.WriteFile(path, append(b, '\n'), 0600))
}
func readJSON(path string, v any) { b, e := os.ReadFile(path); must(e); must(json.Unmarshal(b, v)) }
func guard(dir string, create bool) error {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) == "/" {
		return errors.New("absolute private synthetic directory required")
	}
	p := filepath.Join(dir, "SYNTHETIC-ONLY")
	if create {
		if _, e := os.Lstat(dir); !os.IsNotExist(e) {
			return errors.New("init requires a new directory")
		}
		if e := os.Mkdir(dir, 0700); e != nil {
			return e
		}
		return os.WriteFile(p, []byte(marker), 0600)
	}
	i, e := os.Lstat(dir)
	if e != nil {
		return e
	}
	if !i.IsDir() || i.Mode().Perm()&0077 != 0 {
		return errors.New("private real directory required")
	}
	b, e := os.ReadFile(p)
	if e != nil || string(b) != marker {
		return errors.New("synthetic marker missing")
	}
	return nil
}
func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: archives init|intake|work|report|serve PRIVATE_DIR [FIXTURES_DIR [IDs]]")
		os.Exit(2)
	}
	action, dir := os.Args[1], os.Args[2]
	must(guard(dir, action == "init"))
	ctx := context.Background()
	d, e := store.Open(ctx, filepath.Join(dir, "core.sqlite"))
	must(e)
	defer d.Close()
	_, e = d.Migrate(ctx)
	must(e)
	var s state
	if action == "init" {
		h, e := d.LocalHousehold(ctx)
		must(e)
		screenToken, e := auth.NewScreenToken()
		must(e)
		must(d.Screens().Create(ctx, &domain.Screen{ID: "preview_browser", Name: "T15 合成浏览器", TokenHash: auth.HashToken(screenToken)}))
		must(os.WriteFile(filepath.Join(dir, "screen-token"), []byte(screenToken), 0600))
		_, token, e := auth.NewIntegrationService(d, nil).Issue(ctx, "synthetic T15 only", domain.IntegrationPolicy{Permissions: []string{"archive.ingest", "archive.read", "archive.organize", "archive.analyze", "screens.read", "screens.control", "commands.read"}, HouseholdIDs: []string{h.ID}, ScreenIDs: []string{"preview_browser"}}, time.Now().Add(48*time.Hour))
		must(e)
		p, c, e := d.Integrations().GetByTokenHash(ctx, auth.HashToken(token))
		must(e)
		s = state{Home: h.ID, Actor: domain.IntegrationActor{PrincipalID: p.ID, CredentialID: c.ID}, Token: token, Receipts: map[string]domain.ArchiveIntakeReceipt{}}
		writeJSON(filepath.Join(dir, "state.json"), s)
		fmt.Println("initialized empty synthetic household")
		return
	}
	readJSON(filepath.Join(dir, "state.json"), &s)
	blobs, e := archive.NewBlobStore(filepath.Join(dir, "originals"), 32<<20)
	must(e)
	defer blobs.Close()
	switch action {
	case "intake":
		if len(os.Args) < 4 {
			panic("fixtures directory required")
		}
		var manifest struct {
			Synthetic bool      `json:"synthetic_only"`
			Fixtures  []fixture `json:"fixtures"`
		}
		readJSON(filepath.Join(os.Args[3], "manifest.json"), &manifest)
		if !manifest.Synthetic {
			panic("synthetic fixture marker required")
		}
		ids := ","
		if len(os.Args) > 4 {
			ids = "," + os.Args[4] + ","
		}
		for _, f := range manifest.Fixtures {
			if ids != "," && !strings.Contains(ids, ","+f.ID+",") {
				continue
			}
			if filepath.Base(f.File) != f.File {
				panic("invalid fixture filename")
			}
			file, e := os.Open(filepath.Join(os.Args[3], f.File))
			must(e)
			ref, e := blobs.Put(ctx, s.Home, file, archive.PutOptions{})
			file.Close()
			must(e)
			if ref.SHA256 != f.SHA256 {
				panic("fixture hash mismatch")
			}
			intent := "inherit"
			if f.Private {
				intent = "private"
			}
			m := domain.ArchiveIntakeMetadata{ArchiveSourceIdentity: domain.ArchiveSourceIdentity{Kind: f.Kind, Channel: "synthetic-wechat", Account: "test", Conversation: "test", Sender: "test", ProviderMessageID: f.ID}, OriginalName: f.File, VisibilityIntent: intent}
			rec, e := d.ArchiveTasks().AcceptIntake(ctx, s.Actor, s.Home, 1, m, ref)
			must(e)
			s.Receipts[f.ID] = *rec
			writeJSON(filepath.Join(dir, "state.json"), s)
			fmt.Printf("%s saved task=%s\n", f.ID, rec.TaskID)
		}
	case "work":
		root, e := os.Getwd()
		must(e)
		node := os.Getenv("ATRIUM_E2E_NODE")
		if node == "" {
			node = "/opt/homebrew/bin/node"
		}
		plugin := os.Getenv("ATRIUM_E2E_PLUGIN")
		if plugin == "" {
			plugin = filepath.Join(root, "adapters/openclaw/archive-worker-plugin")
		}
		w := archiveworker.Worker{Tasks: d.ArchiveTasks(), Blobs: blobs, Household: s.Home, Native: archive.NativeParser{HelperPath: os.Getenv("ATRIUM_NATIVE_HELPER")}, Adapter: archive.OpenClawUnderstandingAdapter{NodePath: node, OpenClawPath: "/opt/homebrew/lib/node_modules/openclaw/openclaw.mjs", PluginPath: plugin, ProviderPath: os.Getenv("ATRIUM_DEEPSEEK_PLUGIN"), APIKey: os.Getenv("DEEPSEEK_API_KEY")}}
		if w.Adapter.APIKey == "" {
			panic("provider key required")
		}
		max := 128
		var workerErr error
		for i := 0; i < max; i++ {
			start := time.Now()
			worked, e := w.RunOne(ctx)
			fmt.Printf("worker worked=%v duration=%s error=%v\n", worked, time.Since(start).Round(time.Millisecond), e)
			if e != nil || !worked {
				workerErr = e
				break
			}
		}
		for {
			n, e := d.ArchiveTasks().IndexArchives(ctx, s.Home, 32)
			must(e)
			if n == 0 {
				break
			}
		}
		report(ctx, d, dir, s)
		must(workerErr)
	case "operations":
		if len(os.Args) != 4 {
			panic("operation phase required")
		}
		operations(ctx, d, blobs, dir, s, os.Args[3])
	case "diagnose":
		diagnose(ctx, d, dir)
	case "paired-screen":
		if len(os.Args) != 4 {
			panic("paired screen ID required")
		}
		p, token, e := auth.NewIntegrationService(d, nil).Issue(ctx, "synthetic browser and query", domain.IntegrationPolicy{Permissions: []string{"archive.read", "archive.analyze", "archive.organize", "screens.read", "screens.control", "commands.read"}, HouseholdIDs: []string{s.Home}, ScreenIDs: []string{os.Args[3]}}, time.Now().Add(48*time.Hour))
		must(e)
		writeJSON(filepath.Join(dir, "query-identity.json"), map[string]string{"principal_id": p.ID, "token": token, "screen_id": os.Args[3]})
	case "serve":
		serve(ctx, d, blobs, dir, s)
	case "report":
		report(ctx, d, dir, s)
	default:
		panic("unknown action")
	}
}
func report(ctx context.Context, d *store.DB, dir string, s state) {
	tasks := map[string]any{}
	for id, rec := range s.Receipts {
		t, e := d.ArchiveTasks().Get(ctx, s.Actor, s.Home, rec.TaskID)
		must(e)
		tasks[id] = t
		fmt.Printf("%s %s %s\n", id, t.Status, t.LastErrorCode)
	}
	writeJSON(filepath.Join(dir, "tasks.json"), tasks)
	for _, public := range []bool{false, true} {
		name := "authorized"
		if public {
			name = "public"
		}
		f := archive.SearchFilter{Limit: 25}
		var all []archive.SearchItem
		for {
			page, e := d.ArchiveTasks().SearchArchives(ctx, s.Actor, s.Home, public, f, []byte("synthetic-e2e-cursor-secret-at-least32"))
			must(e)
			all = append(all, page.Items...)
			if page.NextCursor == "" {
				break
			}
			f.Cursor = page.NextCursor
		}
		writeJSON(filepath.Join(dir, name+"-search.json"), all)
	}
	rows, e := d.SQL().QueryContext(ctx, `SELECT task_id,product_key,body FROM archive_products ORDER BY task_id,product_key`)
	must(e)
	defer rows.Close()
	products := map[string]map[string]json.RawMessage{}
	for rows.Next() {
		var task, key string
		var body json.RawMessage
		must(rows.Scan(&task, &key, &body))
		if products[task] == nil {
			products[task] = map[string]json.RawMessage{}
		}
		products[task][key] = body
	}
	must(rows.Err())
	writeJSON(filepath.Join(dir, "products.json"), products)
}
