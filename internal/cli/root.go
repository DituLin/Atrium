// Package cli defines the cobra command tree of the atrium binary.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/DituLin/Atritum/internal/config"
	"github.com/DituLin/Atritum/internal/version"
)

// globalFlags are shared by the server-side commands.
type globalFlags struct {
	configPath string
	dataDir    string
	dev        bool
}

// NewRoot builds the command tree.
func NewRoot() *cobra.Command {
	g := &globalFlags{}

	root := &cobra.Command{
		Use:   "atrium",
		Short: "Atrium home hub core service",
		Long: "Atrium serves a local dashboard to a TV and indexes a read-only NAS photo share.\n" +
			"All state lives in a single data directory; nothing is exposed to the internet.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVarP(&g.configPath, "config", "c", "", "path to config.yaml (default $ATRIUM_CONFIG)")

	root.AddCommand(
		newVersionCmd(),
		newInitCmd(g),
		newServeCmd(g),
		newTLSCmd(g),
		newTokenCmd(g),
		newBackupCmd(g),
		newRestoreCmd(g),
		newAdminCmd(),
	)
	return root
}

// Execute runs the root command and maps errors to an exit status.
func Execute() int {
	if err := NewRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func newVersionCmd() *cobra.Command {
	var short bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version and build commit",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if short {
				fmt.Fprintln(cmd.OutOrStdout(), version.Version)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "atrium %s\n", version.String())
			if version.BuildDate != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "built %s\n", version.BuildDate)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&short, "short", false, "print only the semantic version")
	return cmd
}

// loadConfig resolves the configuration for a server-side command, applying the
// --data-dir override when present.
func (g *globalFlags) loadConfig() (*config.Config, error) {
	if g.dataDir != "" {
		if err := os.Setenv(config.EnvDataDir, g.dataDir); err != nil {
			return nil, fmt.Errorf("cli: set data dir override: %w", err)
		}
	}
	cfg, err := config.Load(g.configPath)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}
