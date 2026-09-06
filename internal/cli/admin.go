package cli

import (
	"encoding/json"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/DituLin/Atrium/internal/config"
)

// adminFlags are shared by every `atrium admin` subcommand.
type adminFlags struct {
	url        string
	token      string
	dataDir    string
	configPath string
	jsonOut    bool
}

// client builds the API client, resolving the token and CA from the data dir.
func (f *adminFlags) client() (*Client, error) {
	dataDir := f.dataDir
	baseURL := f.url
	if dataDir == "" || baseURL == "" {
		if cfg, err := f.loadConfigQuietly(); err == nil {
			if dataDir == "" {
				dataDir = cfg.Storage.DataDir
			}
			if baseURL == "" {
				baseURL = cfg.LocalBaseURL()
			}
		}
	}
	if baseURL == "" {
		baseURL = "https://127.0.0.1:8443"
	}
	return NewClient(ClientOptions{
		BaseURL: baseURL,
		Token:   ResolveToken(f.token, dataDir),
		CAFile:  CAFileFor(dataDir),
	})
}

// loadConfigQuietly resolves the config without failing the command when it is
// absent; the CLI can work from flags and the environment alone.
func (f *adminFlags) loadConfigQuietly() (*config.Config, error) {
	path := f.configPath
	if path == "" {
		path = os.Getenv(config.EnvConfig)
	}
	if path == "" {
		return nil, os.ErrNotExist
	}
	return config.Load(path)
}

func newAdminCmd() *cobra.Command {
	f := &adminFlags{}
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Control a running server over the loopback API",
	}
	cmd.PersistentFlags().StringVar(&f.url, "url", "", "server base URL (default: loopback from the config)")
	cmd.PersistentFlags().StringVar(&f.token, "token", "", "admin token (default: $ATRIUM_ADMIN_TOKEN or the data dir file)")
	cmd.PersistentFlags().StringVar(&f.dataDir, "data-dir", "", "data directory holding admin.token and tls/ca.crt")
	cmd.PersistentFlags().StringVar(&f.configPath, "config", "", "path to config.yaml, used to locate the data dir")
	cmd.PersistentFlags().BoolVar(&f.jsonOut, "json", false, "print raw JSON instead of a table")

	cmd.AddCommand(
		newAdminDiagCmd(f), newAdminPairCmd(f), newAdminScreensCmd(f),
		newAdminSourcesCmd(f), newAdminScansCmd(f), newAdminPhotosCmd(f), newAdminExclusionsCmd(f),
		newAdminScreenCmd(f), newAdminCommandsCmd(f), newAdminTokenCmd(f),
	)
	return cmd
}

// printJSON writes an indented JSON document.
func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// newTable returns a tab writer with the layout used by every listing.
func newTable(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
}

func fmtPtr(s *string) string {
	if s == nil {
		return "-"
	}
	return *s
}

func fmtBool(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}
