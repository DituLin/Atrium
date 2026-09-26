package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/spf13/cobra"
)

const integrationAdminPath = "/api/v1/admin/integrations"

func newAdminIntegrationsCmd(f *adminFlags) *cobra.Command {
	root := &cobra.Command{Use: "integrations", Short: "Manage scoped service identities (credentials are written only to files)"}
	var label, permissions, screens, sources, expiry, outPath string
	policy := func() domain.IntegrationPolicy {
		return domain.IntegrationPolicy{Permissions: splitPolicy(permissions), ScreenIDs: splitPolicy(screens), SourceIDs: splitPolicy(sources)}
	}
	issue := &cobra.Command{Use: "issue", Args: cobra.NoArgs, Short: "Issue a scoped identity and write its credential to a new file", RunE: func(cmd *cobra.Command, _ []string) error {
		expires, err := time.Parse(time.RFC3339, expiry)
		if err != nil {
			return fmt.Errorf("--expires-at must be RFC3339")
		}
		p := policy()
		if err := p.Validate(); err != nil {
			return err
		}
		if strings.TrimSpace(label) == "" {
			return fmt.Errorf("--label is required")
		}
		return writeIntegrationCredential(cmd, f, integrationAdminPath, map[string]any{"label": label, "policy": p, "expires_at": expires}, outPath)
	}}
	issue.Flags().StringVar(&label, "label", "", "descriptive identity label")
	issue.Flags().StringVar(&permissions, "permissions", "", "comma-separated permissions; empty denies all")
	issue.Flags().StringVar(&screens, "screens", "", "comma-separated allowed screen IDs")
	issue.Flags().StringVar(&sources, "sources", "", "comma-separated allowed source IDs")
	issue.Flags().StringVar(&expiry, "expires-at", "", "required RFC3339 principal expiry")
	issue.Flags().StringVar(&outPath, "out", "", "required NEW token file (mode 0600; never overwritten)")
	var rotateOut, rotateExpiry string
	rotate := &cobra.Command{Use: "rotate <principal-id>", Args: cobra.ExactArgs(1), Short: "Replace a credential without changing its principal", RunE: func(cmd *cobra.Command, args []string) error {
		req := map[string]any{}
		if rotateExpiry != "" {
			expires, err := time.Parse(time.RFC3339, rotateExpiry)
			if err != nil {
				return fmt.Errorf("--expires-at must be RFC3339")
			}
			req["expires_at"] = expires
		}
		return writeIntegrationCredential(cmd, f, integrationAdminPath+"/"+args[0]+"/rotate", req, rotateOut)
	}}
	rotate.Flags().StringVar(&rotateOut, "out", "", "required NEW credential file")
	rotate.Flags().StringVar(&rotateExpiry, "expires-at", "", "credential expiry (defaults to principal expiry)")
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := f.client()
		if err != nil {
			return err
		}
		var result any
		if err := c.Do(cmd.Context(), "GET", integrationAdminPath, nil, &result); err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), result)
	}}
	revoke := &cobra.Command{Use: "revoke <principal-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := f.client()
		if err != nil {
			return err
		}
		if err := c.Do(cmd.Context(), "DELETE", integrationAdminPath+"/"+args[0], nil, nil); err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "revoked", args[0])
		return err
	}}
	var updatePermissions, updateScreens, updateSources string
	update := &cobra.Command{Use: "policy <principal-id>", Args: cobra.ExactArgs(1), Short: "Replace the complete permission policy; omitted allowlists grant no access", RunE: func(cmd *cobra.Command, args []string) error {
		if !cmd.Flags().Changed("permissions") {
			return fmt.Errorf("--permissions is required (use an empty value to revoke all permissions)")
		}
		p := domain.IntegrationPolicy{Permissions: splitPolicy(updatePermissions), ScreenIDs: splitPolicy(updateScreens), SourceIDs: splitPolicy(updateSources)}
		if err := p.Validate(); err != nil {
			return err
		}
		c, err := f.client()
		if err != nil {
			return err
		}
		var result domain.IntegrationPrincipal
		if err := c.Do(cmd.Context(), "PUT", integrationAdminPath+"/"+args[0]+"/policy", p, &result); err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), result)
	}}
	update.Flags().StringVar(&updatePermissions, "permissions", "", "complete permission list")
	update.Flags().StringVar(&updateScreens, "screens", "", "complete screen allowlist")
	update.Flags().StringVar(&updateSources, "sources", "", "complete source allowlist")
	root.AddCommand(issue, rotate, list, revoke, update)
	return root
}

func splitPolicy(value string) []string {
	if value == "" {
		return []string{}
	}
	return strings.Split(value, ",")
}

func writeIntegrationCredential(cmd *cobra.Command, f *adminFlags, path string, request any, outPath string) error {
	if outPath == "" {
		return fmt.Errorf("--out is required; credentials are never printed")
	}
	// Reserve a new, non-symlink file before issuing/revoking any credential.
	file, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // explicit maintainer destination; exclusive creation rejects existing files and symlinks
	if err != nil {
		return fmt.Errorf("reserve credential file: %w", err)
	}
	completed := false
	defer func() {
		_ = file.Close()
		if !completed {
			_ = os.Remove(outPath)
		}
	}()
	c, err := f.client()
	if err != nil {
		return err
	}
	var response struct {
		Principal domain.IntegrationPrincipal `json:"principal"`
		Token     string                      `json:"token"`
	}
	if err := c.Do(cmd.Context(), "POST", path, request, &response); err != nil {
		return err
	}
	if !auth.ValidToken(response.Token, auth.IntegrationPrefix) {
		return fmt.Errorf("server returned an invalid integration credential; reissue via admin")
	}
	if _, err := file.WriteString(response.Token + "\n"); err != nil {
		return fmt.Errorf("credential persistence failed; use admin to rotate again: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("credential sync failed; use admin to rotate again: %w", err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	completed = true
	if f.jsonOut {
		return printJSON(cmd.OutOrStdout(), response.Principal)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "principal %s; credential written to %s (0600)\n", response.Principal.ID, outPath)
	return err
}
