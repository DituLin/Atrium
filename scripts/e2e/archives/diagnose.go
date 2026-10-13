package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/store"
	"os"
	"path/filepath"
)

func diagnose(ctx context.Context, d *store.DB, dir string) {
	plans := map[string]archive.UnderstandingPlan{}
	rows, e := d.SQL().QueryContext(ctx, `SELECT body FROM archive_products WHERE product_key='plan'`)
	must(e)
	for rows.Next() {
		var raw []byte
		must(rows.Scan(&raw))
		var p archive.UnderstandingPlan
		must(json.Unmarshal(raw, &p))
		plans[p.ParseDigest] = p
	}
	must(rows.Close())
	files, e := filepath.Glob(filepath.Join(dir, "wire", "*-input.json"))
	must(e)
	for _, f := range files {
		var c archive.UnderstandingChunk
		readJSON(f, &c)
		var response archive.WorkerResponse
		raw, e := os.ReadFile(f[:len(f)-len("input.json")] + "output.json") // #nosec G703 -- The filename comes from Glob under the marker-guarded synthetic wire directory and only its fixed input.json suffix is replaced.
		must(e)
		if json.Unmarshal(raw, &response) != nil || response.Provider == "" {
			fmt.Println(filepath.Base(f), "worker rejected")
			continue
		}
		p := plans[c.ParseDigest]
		body, e := json.Marshal(response.Result)
		must(e)
		_, e = archive.ValidateChunk(p, p.Chunks[c.Index], body)
		fmt.Println(filepath.Base(f), "chunk", c.Index, "validation", e)
	}
}
