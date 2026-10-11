package cli

import (
	"encoding/json"
	"fmt"
	"github.com/DituLin/Atrium/internal/app"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/spf13/cobra"
	"os"
)

func newMaintenanceCmd(g *globalFlags) *cobra.Command {
	c := &cobra.Command{Use: "maintenance", Short: "Offline schema checks and resumable archive maintenance"}
	c.PersistentFlags().StringVar(&g.dataDir, "data-dir", "", "data directory")
	for _, name := range []string{"schema", "backfill"} {
		name := name
		sub := &cobra.Command{Use: name, RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, e := g.loadConfig()
			if e != nil {
				return e
			}
			path := app.NewLayout(cfg.Storage.DataDir).DB()
			if _, e = os.Stat(path); e != nil {
				return fmt.Errorf("maintenance: existing database required")
			}
			db, e := store.Open(cmd.Context(), path)
			if e != nil {
				return e
			}
			defer db.Close()
			v, e := db.ValidateSchema(cmd.Context())
			if e != nil {
				return e
			}
			if name == "schema" {
				ms, e := store.Migrations()
				if e != nil {
					return e
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"compatible": true, "schema": v, "latest": ms[len(ms)-1].Version})
			}
			if _, e = db.Migrate(cmd.Context()); e != nil {
				return e
			}
			s, e := db.BackfillArchives(cmd.Context())
			if e != nil {
				return fmt.Errorf("maintenance backfill incomplete: %w", e)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(s)
		}}
		c.AddCommand(sub)
	}
	return c
}
