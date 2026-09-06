package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/DituLin/Atrium/internal/diag"
)

// The diagnostics document is decoded straight into the server's own type, so
// the CLI cannot drift from design §6.11 without failing to compile.
func newAdminDiagCmd(f *adminFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "diag",
		Short: "Print server diagnostics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			var out diag.Document
			if err := c.Do(cmd.Context(), "GET", "/api/v1/diagnostics", nil, &out); err != nil {
				return err
			}
			if f.jsonOut {
				return printJSON(cmd.OutOrStdout(), out)
			}
			return printDiagnostics(cmd.OutOrStdout(), out)
		},
	}
}

// printDiagnostics renders the whole document as tables. It prints every
// section, including the empty ones: "no failed jobs" is information.
func printDiagnostics(w io.Writer, d diag.Document) error {
	fmt.Fprintf(w, "version   %s\n", d.Version)
	fmt.Fprintf(w, "uptime    %ds\n", d.UptimeSeconds)
	fmt.Fprintf(w, "transport %s\n", fmtBool(d.Insecure, "INSECURE (tls off)", "tls"))
	fmt.Fprintf(w, "database  %d bytes (+%d wal), %d migrations\n",
		d.DB.SizeBytes, d.DB.WALBytes, d.DB.Migrations)
	fmt.Fprintf(w, "photos    %s\n", counterLine(d.Photos,
		"ready", "pending", "unsupported", "removed", "excluded", "preview_failed", "preview_evicted"))
	fmt.Fprintf(w, "jobs      queued %d, running %d, failed %d, done %d, oldest queued %s\n",
		d.Jobs.Queued, d.Jobs.Running, d.Jobs.Failed, d.Jobs.Done, fmtPtr(d.Jobs.OldestQueued))
	fmt.Fprintf(w, "cache     %d of %d bytes, %d free on disk, paused %s\n",
		d.Cache.Bytes, d.Cache.BudgetBytes, d.Cache.FreeDiskBytes, fmtPtr(d.Cache.PausedReason))
	fmt.Fprintf(w, "commands  last 24h: %s\n", counterLine(d.Commands.Last24h,
		"applied", "failed", "expired", "unknown", "accepted"))
	fmt.Fprintf(w, "pairings  %s\n", counterLine(d.Pairings,
		"pending", "approved", "claimed", "expired", "rejected"))

	t := newTable(w)
	fmt.Fprintln(t, "\nSOURCE\tHEALTH\tIDENTITY\tSTUCK\tLAST SCAN\tLAST SUCCESS")
	for _, s := range d.Sources {
		scan := "-"
		if s.LastScan != nil {
			scan = fmt.Sprintf("%s %d files in %d ms", s.LastScan.Status, s.LastScan.FilesSeen, s.LastScan.DurationMS)
		}
		fmt.Fprintf(t, "%s\t%s\t%s\t%d\t%s\t%s\n", s.ID, healthText(s),
			fmtBool(s.IdentityBound, "bound", "unbound"), s.StuckOps, scan, fmtPtr(s.LastSuccessAt))
	}
	if len(d.Sources) == 0 {
		fmt.Fprintln(t, "(none)\t\t\t\t\t")
	}

	fmt.Fprintln(t, "\nSCREEN\tREGISTERED\tONLINE\tROUTE\tAPPLIED SEQ\tOPEN CMDS\tLAST SEEN")
	for _, s := range d.Screens {
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%d\t%d\t%s\n", s.ID,
			fmtBool(s.Registered, "yes", "no"), fmtBool(s.Online, "yes", "no"),
			routeText(s.Route), s.AppliedSequence, s.OpenCommands, fmtPtr(s.LastSeenAt))
	}
	if len(d.Screens) == 0 {
		fmt.Fprintln(t, "(none)\t\t\t\t\t\t")
	}

	fmt.Fprintln(t, "\nRECENT ERROR\tCOMPONENT\tCODE\tSUBJECT")
	for _, e := range d.RecentErrors {
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\n", e.At.Format(time.RFC3339), e.Component, e.Code,
			orDash(e.PhotoID+e.SourceID+e.ScreenID))
	}
	if len(d.RecentErrors) == 0 {
		fmt.Fprintln(t, "(none)\t\t\t")
	}
	if err := t.Flush(); err != nil {
		return err
	}
	if len(d.Widgets) > 0 {
		raw, err := json.Marshal(d.Widgets)
		if err != nil {
			return fmt.Errorf("cli: render widgets: %w", err)
		}
		fmt.Fprintf(w, "\nwidgets   %s\n", raw)
	}
	return nil
}

func healthText(s diag.SourceSection) string {
	if s.HealthDetail == "" {
		return s.Health
	}
	return s.Health + "/" + s.HealthDetail
}

// counterLine renders a counter map in a fixed key order so two runs are
// comparable by eye.
func counterLine(m map[string]int64, keys ...string) string {
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

type pairingItem struct {
	ID               string  `json:"id"`
	Code             string  `json:"code"`
	Status           string  `json:"status"`
	ClientHint       string  `json:"client_hint"`
	CreatedAt        string  `json:"created_at"`
	ExpiresAt        string  `json:"expires_at"`
	ApprovedScreenID string  `json:"approved_screen_id"`
	ClaimedAt        *string `json:"claimed_at"`
}

type pairingList struct {
	Pairings []pairingItem `json:"pairings"`
}

func newAdminPairCmd(f *adminFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "pair", Short: "Inspect and resolve pairing requests"}

	list := &cobra.Command{
		Use:   "list",
		Short: "List recent pairing requests",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			var out pairingList
			if err := c.Do(cmd.Context(), "GET", "/api/v1/pairings", nil, &out); err != nil {
				return err
			}
			if f.jsonOut {
				return printJSON(cmd.OutOrStdout(), out)
			}
			t := newTable(cmd.OutOrStdout())
			fmt.Fprintln(t, "CODE\tSTATUS\tSCREEN\tEXPIRES\tID")
			for _, p := range out.Pairings {
				screen := p.ApprovedScreenID
				if screen == "" {
					screen = "-"
				}
				fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\n", p.Code, p.Status, screen, p.ExpiresAt, p.ID)
			}
			if len(out.Pairings) == 0 {
				fmt.Fprintln(t, "(no pairing requests)\t\t\t\t")
			}
			return t.Flush()
		},
	}

	var screenID, screenName string
	approve := &cobra.Command{
		Use:   "approve <code|pairing-id>",
		Short: "Approve a pairing request and register the screen",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if screenID == "" {
				return fmt.Errorf("--id is required")
			}
			c, err := f.client()
			if err != nil {
				return err
			}
			id, err := resolvePairingRef(cmd, c, args[0])
			if err != nil {
				return err
			}
			body := map[string]string{"screen_id": screenID, "name": screenName}
			var out struct {
				Pairing pairingItem `json:"pairing"`
				Screen  screenItem  `json:"screen"`
			}
			if err := c.Do(cmd.Context(), "POST", "/api/v1/pairings/"+id+"/approve", body, &out); err != nil {
				return err
			}
			if f.jsonOut {
				return printJSON(cmd.OutOrStdout(), out)
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"approved pairing %s for screen %s (%s)\nthe screen receives its credential on its next poll\n",
				out.Pairing.Code, out.Screen.ID, out.Screen.Name)
			return nil
		},
	}
	approve.Flags().StringVar(&screenID, "id", "", "screen slug, e.g. living_room_tv")
	approve.Flags().StringVar(&screenName, "name", "", "human-readable screen name")

	reject := &cobra.Command{
		Use:   "reject <code|pairing-id>",
		Short: "Delete a pairing request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			id, err := resolvePairingRef(cmd, c, args[0])
			if err != nil {
				return err
			}
			if err := c.Do(cmd.Context(), "DELETE", "/api/v1/pairings/"+id, nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "rejected pairing %s\n", id)
			return nil
		},
	}

	cmd.AddCommand(list, approve, reject)
	return cmd
}

// resolvePairingRef accepts either a pairing ID or a six-digit code and returns
// the ID, so the operator can type what the TV shows.
func resolvePairingRef(cmd *cobra.Command, c *Client, ref string) (string, error) {
	if len(ref) != 6 || !isDigits(ref) {
		return ref, nil
	}
	var out pairingList
	if err := c.Do(cmd.Context(), "GET", "/api/v1/pairings", nil, &out); err != nil {
		return "", err
	}
	for _, p := range out.Pairings {
		if p.Code == ref {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("no pairing with code %s", ref)
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}
