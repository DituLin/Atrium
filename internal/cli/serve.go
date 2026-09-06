package cli

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"github.com/DituLin/Atrium/internal/app"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/auth/tlsgen"
	"github.com/DituLin/Atrium/internal/diag"
	"github.com/DituLin/Atrium/internal/logging"
	"github.com/DituLin/Atrium/internal/store"
)

func newServeCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the HTTP server and background workers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := g.loadConfig()
			if err != nil {
				return err
			}
			layout := app.NewLayout(cfg.Storage.DataDir)
			if err := layout.Ensure(); err != nil {
				return err
			}
			// The ring is a log handler, so every component feeds
			// `recent_errors` in diagnostics without knowing it exists.
			errRing := diag.NewErrors()
			log, err := logging.New(logging.Options{
				Level:      cfg.Logging.Level,
				Dir:        layout.Logs(),
				Console:    g.dev,
				RetainDays: cfg.Logging.RetainDays,
				MaxTotalMB: cfg.Logging.MaxTotalMB,
				Extra:      []slog.Handler{errRing},
			})
			if err != nil {
				return err
			}
			defer func() { _ = log.Close() }()

			rt, err := app.New(cmd.Context(), app.Options{
				Config: cfg, Logger: log.Logger, Errors: errRing,
			})
			if err != nil {
				return err
			}
			return rt.ServeWithSignals(cmd.Context())
		},
	}
	cmd.Flags().StringVar(&g.dataDir, "data-dir", "", "data directory (overrides the config value)")
	cmd.Flags().BoolVar(&g.dev, "dev", false, "also log to the console")
	return cmd
}

func newTLSCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tls",
		Short: "Manage the local certificate authority and the server certificate",
	}
	cmd.PersistentFlags().StringVar(&g.dataDir, "data-dir", "", "data directory (overrides the config value)")

	renew := &cobra.Command{
		Use:   "renew",
		Short: "Regenerate the server certificate, keeping the existing CA",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := g.loadConfig()
			if err != nil {
				return err
			}
			layout := app.NewLayout(cfg.Storage.DataDir)
			if err := layout.Ensure(); err != nil {
				return err
			}
			res, err := tlsgen.Renew(tlsgen.Params{
				Dir:       layout.TLS(),
				PublicURL: cfg.Server.PublicURL,
				ExtraSANs: cfg.Server.ExtraSANs,
				Now:       time.Now(),
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "renewed %s\nsans %v %v\nrestart the service to load it\n",
				res.ServerCertPath, res.DNSNames, res.IPAddresses)
			return nil
		},
	}

	exportCA := &cobra.Command{
		Use:   "export-ca",
		Short: "Print the local CA certificate for installation on the TV",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := g.loadConfig()
			if err != nil {
				return err
			}
			pemBytes, err := tlsgen.ExportCA(app.NewLayout(cfg.Storage.DataDir).TLS())
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(pemBytes)
			return err
		},
	}

	cmd.AddCommand(renew, exportCA)
	return cmd
}

func newTokenCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Offline admin token recovery",
	}
	cmd.PersistentFlags().StringVar(&g.dataDir, "data-dir", "", "data directory (overrides the config value)")

	reset := &cobra.Command{
		Use:   "reset",
		Short: "Revoke every admin token and issue a new one (server must be stopped)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := g.loadConfig()
			if err != nil {
				return err
			}
			layout := app.NewLayout(cfg.Storage.DataDir)
			db, err := store.Open(cmd.Context(), layout.DB())
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			if _, err := db.Migrate(cmd.Context()); err != nil {
				return err
			}
			token, err := auth.NewAdminService(db).Reset(cmd.Context(), "token reset", time.Now())
			if err != nil {
				return err
			}
			path, err := auth.WriteAdminTokenFile(layout.Root, token)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"every previous admin token is revoked\nwrote %s (mode 0600)\n\nNew admin token:\n  %s\n",
				path, token)
			return nil
		},
	}
	cmd.AddCommand(reset)
	return cmd
}
