package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/DituLin/Atritum/internal/app"
	"github.com/DituLin/Atritum/internal/backup"
	"github.com/DituLin/Atritum/internal/logging"
	"github.com/DituLin/Atritum/internal/store"
)

// newBackupCmd implements `atrium backup now | list` (design §11). Both work
// against the database file directly, so they run whether or not the service
// is up: `VACUUM INTO` reads a consistent snapshot under WAL.
func newBackupCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "backup", Short: "Create and list SQLite snapshots"}
	cmd.PersistentFlags().StringVar(&g.dataDir, "data-dir", "", "data directory (overrides the config value)")

	now := &cobra.Command{
		Use:   "now",
		Short: "Take a snapshot immediately and apply retention",
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, closeDB, err := backupService(cmd, g)
			if err != nil {
				return err
			}
			defer closeDB()
			snap, err := svc.Run(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s (%d bytes)\nconfig copy  %s\n",
				snap.Name, snap.SizeBytes, fmtBool(snap.ConfigCopy, "yes", "no"))
			return nil
		},
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List stored snapshots, newest first",
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, closeDB, err := backupService(cmd, g)
			if err != nil {
				return err
			}
			defer closeDB()
			snaps, err := svc.List()
			if err != nil {
				return err
			}
			t := newTable(cmd.OutOrStdout())
			fmt.Fprintln(t, "NAME\tSIZE\tCREATED\tCONFIG")
			for _, s := range snaps {
				fmt.Fprintf(t, "%s\t%d\t%s\t%s\n", s.Name, s.SizeBytes,
					s.CreatedAt.Format(time.RFC3339), fmtBool(s.ConfigCopy, "yes", "no"))
			}
			if len(snaps) == 0 {
				fmt.Fprintln(t, "(no backups yet)\t\t\t")
			}
			return t.Flush()
		},
	}

	cmd.AddCommand(now, list)
	return cmd
}

// backupService opens the database and builds the service; the returned
// function closes the handle.
func backupService(cmd *cobra.Command, g *globalFlags) (*backup.Service, func(), error) {
	cfg, err := g.loadConfig()
	if err != nil {
		return nil, nil, err
	}
	if cfg.Backup.Dir == "" {
		return nil, nil, fmt.Errorf("cli: backup.dir is not configured")
	}
	layout := app.NewLayout(cfg.Storage.DataDir)
	db, err := store.Open(cmd.Context(), layout.DB())
	if err != nil {
		return nil, nil, err
	}
	svc := backup.New(backup.Options{
		Dir: cfg.Backup.Dir, Keep: cfg.Backup.Keep, ConfigPath: cfg.Path(),
		DB: db.SQL(), Now: time.Now,
		// The command reports its own result; the service log would only
		// duplicate it on stderr.
		Logger: logging.Discard(),
	})
	return svc, func() { _ = db.Close() }, nil
}

// newRestoreCmd implements `atrium restore --from <file>`. The server must be
// stopped; the current database is preserved rather than deleted (§6.10).
func newRestoreCmd(g *globalFlags) *cobra.Command {
	var from string
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Replace the database with a snapshot (the server must be stopped)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if from == "" {
				return fmt.Errorf("cli: --from is required")
			}
			cfg, err := g.loadConfig()
			if err != nil {
				return err
			}
			layout := app.NewLayout(cfg.Storage.DataDir)
			res, err := backup.Restore(cmd.Context(), backup.RestoreOptions{
				From: from, DataDir: layout.Root, DBPath: layout.DB(), Now: time.Now,
			})
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "restored %s (%d migrations)\n", from, res.Migrations)
			if res.PreservedPath != "" {
				fmt.Fprintf(w, "previous database kept at %s\n", res.PreservedPath)
			}
			fmt.Fprintln(w, "previews are not backed up and regenerate on demand; start the service now")
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "snapshot file to restore")
	cmd.Flags().StringVar(&g.dataDir, "data-dir", "", "data directory (overrides the config value)")
	return cmd
}
