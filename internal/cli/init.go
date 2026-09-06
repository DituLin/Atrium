package cli

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/DituLin/Atrium/internal/app"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/auth/tlsgen"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/store"
)

func newInitCmd(g *globalFlags) *cobra.Command {
	var publicURL, timezone string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create the config file, data directory, TLS material and admin token",
		Long: "init is safe to re-run: an existing config file, certificate or admin token " +
			"is kept, and only the missing pieces are created.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd, g, publicURL, timezone)
		},
	}
	cmd.Flags().StringVar(&g.dataDir, "data-dir", "", "data directory (overrides the config value)")
	cmd.Flags().StringVar(&publicURL, "public-url", "", "URL the TV opens, e.g. https://192.168.1.10:8443")
	cmd.Flags().StringVar(&timezone, "timezone", "", "IANA home timezone; defaults to the system zone")
	return cmd
}

func runInit(cmd *cobra.Command, g *globalFlags, publicURL, timezone string) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	configPath := g.configPath
	if configPath == "" {
		configPath = os.Getenv(config.EnvConfig)
	}
	if configPath == "" {
		configPath = "config.yaml"
	}
	configPath, err := config.ExpandPath(configPath)
	if err != nil {
		return err
	}

	created, err := writeConfigIfMissing(configPath, g.dataDir, publicURL, timezone)
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintf(out, "wrote config     %s\n", configPath)
	} else {
		fmt.Fprintf(out, "config exists    %s\n", configPath)
	}

	g.configPath = configPath
	cfg, err := g.loadConfig()
	if err != nil {
		return err
	}

	layout := app.NewLayout(cfg.Storage.DataDir)
	if err := layout.Ensure(); err != nil {
		return err
	}
	fmt.Fprintf(out, "data directory   %s\n", layout.Root)

	if cfg.Server.TLS.Mode == config.TLSAuto {
		res, terr := tlsgen.Ensure(tlsgen.Params{
			Dir:       layout.TLS(),
			PublicURL: cfg.Server.PublicURL,
			ExtraSANs: cfg.Server.ExtraSANs,
			Now:       time.Now(),
		})
		if terr != nil {
			return terr
		}
		fmt.Fprintf(out, "tls certificate  %s\n", res.ServerCertPath)
		fmt.Fprintf(out, "tls ca           %s (install this on the TV)\n", res.CACertPath)
		fmt.Fprintf(out, "tls sans         %v %v\n", res.DNSNames, res.IPAddresses)
	} else {
		fmt.Fprintf(out, "tls mode         %s\n", cfg.Server.TLS.Mode)
	}

	db, err := store.Open(ctx, layout.DB())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	applied, err := db.Migrate(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "database         %s (%d migrations applied)\n", layout.DB(), applied)

	token, issued, err := auth.NewAdminService(db).EnsureInitial(ctx, "init", time.Now())
	if err != nil {
		return err
	}
	if issued {
		path, werr := auth.WriteAdminTokenFile(layout.Root, token)
		if werr != nil {
			return werr
		}
		fmt.Fprintf(out, "admin token      %s (mode 0600)\n", path)
		fmt.Fprintf(out, "\nAdmin token (store it safely, it is shown once):\n  %s\n", token)
	} else {
		fmt.Fprintf(out, "admin token      already present; run 'atrium token reset' to replace it\n")
	}

	fmt.Fprintf(out, "\nNext: atrium serve --config %s\n", configPath)
	return nil
}

// writeConfigIfMissing renders config.example.yaml with the operator's answers
// when no config file exists yet.
func writeConfigIfMissing(path, dataDir, publicURL, timezone string) (bool, error) {
	if _, err := os.Stat(path); err == nil { //nolint:gosec // operator-supplied config path
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil { //nolint:gosec // operator-supplied config path
		return false, fmt.Errorf("cli: create config directory: %w", err)
	}
	if timezone == "" {
		timezone = systemTimezone()
	}
	if publicURL == "" {
		publicURL = "https://127.0.0.1:8443"
	}
	if _, err := url.Parse(publicURL); err != nil {
		return false, fmt.Errorf("cli: --public-url is not a URL: %w", err)
	}
	if dataDir == "" {
		dataDir = "~/Library/Application Support/Atrium"
	}
	body := renderConfig(configTemplateValues{
		Timezone:  timezone,
		PublicURL: publicURL,
		DataDir:   dataDir,
	})
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil { //nolint:gosec // operator-supplied config path
		return false, fmt.Errorf("cli: write config: %w", err)
	}
	return true, nil
}

// systemTimezone reads the local zone name, defaulting to UTC when the system
// reports an unusable value.
func systemTimezone() string {
	name, _ := time.Now().Zone()
	if loc := time.Local.String(); loc != "" && loc != "Local" {
		return loc
	}
	if _, err := time.LoadLocation(name); err == nil && name != "" {
		return name
	}
	return "UTC"
}
