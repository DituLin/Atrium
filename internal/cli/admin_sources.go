package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// sourceItem mirrors the admin source DTO. It carries no root path by design.
type sourceItem struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Status          string  `json:"status"`
	Health          string  `json:"health"`
	HealthDetail    string  `json:"health_detail"`
	IdentityBound   bool    `json:"identity_bound"`
	IdentityFSType  string  `json:"identity_fstype"`
	IdentityMatches bool    `json:"identity_matches"`
	LastCheckAt     *string `json:"last_check_at"`
	LastSuccessAt   *string `json:"last_success_at"`
	ScanGeneration  int64   `json:"scan_generation"`
	LastScanAt      *string `json:"last_scan_at"`
	BaselineDoneAt  *string `json:"baseline_completed_at"`
	ShareFreeBytes  *int64  `json:"share_free_bytes"`
	ShareTotalBytes *int64  `json:"share_total_bytes"`
	StuckOps        int64   `json:"stuck_ops"`
	SkippedSymlinks int64   `json:"skipped_symlinks"`
	RevokeReason    string  `json:"revoke_reason"`
	Photos          struct {
		Ready       int64 `json:"ready"`
		Pending     int64 `json:"pending"`
		Unsupported int64 `json:"unsupported"`
		Removed     int64 `json:"removed"`
		Excluded    int64 `json:"excluded"`
	} `json:"photos"`
}

type sourceList struct {
	Sources []sourceItem `json:"sources"`
}

type scanResult struct {
	Run    *scanRunItem `json:"scan_run"`
	Merged bool         `json:"merged"`
}

func newAdminSourcesCmd(f *adminFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "sources", Short: "Inspect and control authorized photo roots"}

	list := &cobra.Command{
		Use:   "list",
		Short: "List sources with health, identity and index counts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			var out sourceList
			if err := c.Do(cmd.Context(), "GET", "/api/v1/sources", nil, &out); err != nil {
				return err
			}
			if f.jsonOut {
				return printJSON(cmd.OutOrStdout(), out)
			}
			t := newTable(cmd.OutOrStdout())
			fmt.Fprintln(t, "ID\tSTATUS\tHEALTH\tDETAIL\tIDENTITY\tREADY\tPENDING\tUNSUPP\tREMOVED\tGEN\tLAST SCAN")
			for _, s := range out.Sources {
				fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%s\n",
					s.ID, s.Status, s.Health, dash(s.HealthDetail), identityLabel(s),
					s.Photos.Ready, s.Photos.Pending, s.Photos.Unsupported, s.Photos.Removed,
					s.ScanGeneration, fmtPtr(s.LastScanAt))
			}
			if len(out.Sources) == 0 {
				fmt.Fprintln(t, "(no sources configured)\t\t\t\t\t\t\t\t\t\t")
			}
			return t.Flush()
		},
	}

	var full bool
	scan := &cobra.Command{
		Use:   "scan <source-id>",
		Short: "Run a scan now; --full re-derives every file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			mode := "incremental"
			if full {
				mode = "full"
			}
			var out scanResult
			if err := c.Do(cmd.Context(), "POST", "/api/v1/sources/"+args[0]+"/scan",
				map[string]string{"mode": mode}, &out); err != nil {
				return err
			}
			if f.jsonOut {
				return printJSON(cmd.OutOrStdout(), out)
			}
			w := cmd.OutOrStdout()
			if out.Merged {
				fmt.Fprintln(w, "a scan was already running; the request merged into it")
			}
			if out.Run == nil {
				fmt.Fprintln(w, "scan requested")
				return nil
			}
			printScanRun(w, *out.Run)
			return nil
		},
	}
	scan.Flags().BoolVar(&full, "full", false, "re-stat every file and clear the stability set")

	cmd.AddCommand(list, scan,
		sourceActionCmd(f, "revoke", "Hide every photo of a source without deleting the index", "/revoke"),
		sourceActionCmd(f, "restore", "Undo a revoke for a source still present in the config", "/restore"),
		sourceActionCmd(f, "rebind-identity", "Accept the mount currently behind the root", "/rebind-identity"),
	)
	return cmd
}

// sourceActionCmd builds the three POST-only source commands, which differ
// only in their path and wording.
func sourceActionCmd(f *adminFlags, name, short, path string) *cobra.Command {
	return &cobra.Command{
		Use:   name + " <source-id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			var out sourceItem
			if err := c.Do(cmd.Context(), "POST", "/api/v1/sources/"+args[0]+path, nil, &out); err != nil {
				return err
			}
			if f.jsonOut {
				return printJSON(cmd.OutOrStdout(), out)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: status %s, health %s%s\n",
				out.ID, out.Status, out.Health, detailSuffix(out.HealthDetail))
			return nil
		},
	}
}

func identityLabel(s sourceItem) string {
	switch {
	case !s.IdentityBound:
		return "unbound"
	case !s.IdentityMatches:
		return "MISMATCH"
	case s.IdentityFSType != "":
		return "bound/" + s.IdentityFSType
	default:
		return "bound"
	}
}

func detailSuffix(detail string) string {
	if detail == "" {
		return ""
	}
	return " (" + detail + ")"
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
