package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/DituLin/Atrium/internal/auth"
)

type tokenRotateResult struct {
	Token     string `json:"token"`
	CreatedAt string `json:"created_at"`
	Note      string `json:"note"`
}

func newAdminTokenCmd(f *adminFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "token", Short: "Manage the admin credential"}

	rotate := &cobra.Command{
		Use:   "rotate",
		Short: "Issue a new admin token, revoke the current one and rewrite the token file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			var out tokenRotateResult
			if err := c.Do(cmd.Context(), "POST", "/api/v1/admin/token/rotate", nil, &out); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			dataDir := f.resolveDataDir()
			if dataDir != "" {
				path, werr := auth.WriteAdminTokenFile(dataDir, out.Token)
				if werr != nil {
					// The token is already live; failing to persist it is a
					// warning, not a reason to hide it from the operator.
					fmt.Fprintf(cmd.ErrOrStderr(),
						"warning: could not rewrite the token file: %v\n", werr)
				} else {
					fmt.Fprintf(w, "wrote %s (mode 0600)\n", path)
				}
			}
			if f.jsonOut {
				return printJSON(w, out)
			}
			fmt.Fprintf(w, "created at   %s\n", out.CreatedAt)
			fmt.Fprintf(w, "\nNew admin token:\n  %s\n\n%s\n", out.Token, out.Note)
			return nil
		},
	}

	cmd.AddCommand(rotate)
	return cmd
}

// resolveDataDir finds the data directory the same way client() does, so the
// rotated token lands next to the CA the CLI already trusts.
func (f *adminFlags) resolveDataDir() string {
	if f.dataDir != "" {
		return f.dataDir
	}
	if cfg, err := f.loadConfigQuietly(); err == nil {
		return cfg.Storage.DataDir
	}
	return ""
}
