package cli

import (
	"fmt"
	"io"
	"net/url"

	"github.com/spf13/cobra"
)

// scanRunItem mirrors the scan run DTO.
type scanRunItem struct {
	ID               string  `json:"id"`
	SourceID         string  `json:"source_id"`
	Mode             string  `json:"mode"`
	Status           string  `json:"status"`
	StartedAt        string  `json:"started_at"`
	FinishedAt       *string `json:"finished_at"`
	DurationMS       int64   `json:"duration_ms"`
	FilesSeen        int64   `json:"files_seen"`
	FilesNew         int64   `json:"files_new"`
	FilesChanged     int64   `json:"files_changed"`
	FilesMissing     int64   `json:"files_missing"`
	FilesRemoved     int64   `json:"files_removed"`
	FilesUnsupported int64   `json:"files_unsupported"`
	Errors           int64   `json:"errors"`
	Note             string  `json:"note"`
}

type scanList struct {
	Scans []scanRunItem `json:"scans"`
}

func printScanRun(w io.Writer, run scanRunItem) {
	fmt.Fprintf(w, "id           %s\n", run.ID)
	fmt.Fprintf(w, "source       %s\n", run.SourceID)
	fmt.Fprintf(w, "mode         %s\n", run.Mode)
	fmt.Fprintf(w, "status       %s%s\n", run.Status, detailSuffix(run.Note))
	fmt.Fprintf(w, "started      %s\n", run.StartedAt)
	fmt.Fprintf(w, "finished     %s (%d ms)\n", fmtPtr(run.FinishedAt), run.DurationMS)
	fmt.Fprintf(w, "files        seen %d, new %d, changed %d, missing %d, removed %d, unsupported %d\n",
		run.FilesSeen, run.FilesNew, run.FilesChanged, run.FilesMissing, run.FilesRemoved, run.FilesUnsupported)
	fmt.Fprintf(w, "errors       %d\n", run.Errors)
}

func newAdminScansCmd(f *adminFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "scans", Short: "Inspect scan runs"}

	var sourceID string
	var limit int
	list := &cobra.Command{
		Use:   "list",
		Short: "List recent scan runs, newest first",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			q := url.Values{}
			if sourceID != "" {
				q.Set("source_id", sourceID)
			}
			if limit > 0 {
				q.Set("limit", fmt.Sprint(limit))
			}
			path := "/api/v1/scans"
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			var out scanList
			if err := c.Do(cmd.Context(), "GET", path, nil, &out); err != nil {
				return err
			}
			if f.jsonOut {
				return printJSON(cmd.OutOrStdout(), out)
			}
			t := newTable(cmd.OutOrStdout())
			fmt.Fprintln(t, "ID\tSOURCE\tMODE\tSTATUS\tSEEN\tNEW\tCHANGED\tREMOVED\tERRORS\tSTARTED")
			for _, s := range out.Scans {
				fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%s\n",
					s.ID, s.SourceID, s.Mode, s.Status, s.FilesSeen, s.FilesNew,
					s.FilesChanged, s.FilesRemoved, s.Errors, s.StartedAt)
			}
			if len(out.Scans) == 0 {
				fmt.Fprintln(t, "(no scans recorded)\t\t\t\t\t\t\t\t\t")
			}
			return t.Flush()
		},
	}
	list.Flags().StringVar(&sourceID, "source", "", "limit to one source")
	list.Flags().IntVar(&limit, "limit", 0, "maximum rows (default 50)")

	get := &cobra.Command{
		Use:   "get <scan-id>",
		Short: "Show one scan run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			var out scanRunItem
			if err := c.Do(cmd.Context(), "GET", "/api/v1/scans/"+args[0], nil, &out); err != nil {
				return err
			}
			if f.jsonOut {
				return printJSON(cmd.OutOrStdout(), out)
			}
			printScanRun(cmd.OutOrStdout(), out)
			return nil
		},
	}

	cmd.AddCommand(list, get)
	return cmd
}
