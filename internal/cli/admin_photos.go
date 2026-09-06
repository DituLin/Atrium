package cli

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

// photoItem is the screen-facing DTO; photoAdminItem adds the fields only an
// administrator may see, including the relative path.
type photoItem struct {
	ID                 string  `json:"id"`
	SourceID           string  `json:"source_id"`
	CapturedAt         *string `json:"captured_at"`
	CapturedConfidence string  `json:"captured_confidence"`
	FirstSeenAt        string  `json:"first_seen_at"`
	IsBaseline         bool    `json:"is_baseline"`
	Width              int     `json:"width"`
	Height             int     `json:"height"`
	Preview            struct {
		Status string `json:"status"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"preview"`
}

type photoAdminItem struct {
	photoItem
	RelPath            string  `json:"rel_path"`
	Ext                string  `json:"ext"`
	SizeBytes          int64   `json:"size_bytes"`
	Fingerprint        string  `json:"fingerprint"`
	Status             string  `json:"status"`
	MetaStatus         string  `json:"meta_status"`
	MetaError          string  `json:"meta_error"`
	PreviewError       string  `json:"preview_error"`
	PreviewAttempts    int     `json:"preview_attempts"`
	PreviewNextRetryAt *string `json:"preview_next_retry_at"`
	MissingGenerations int     `json:"missing_generations"`
	RemovedAt          *string `json:"removed_at"`
	ExcludedAt         *string `json:"excluded_at"`
	ExcludeReason      string  `json:"exclude_reason"`
}

type photoList struct {
	Items []photoItem `json:"items"`
	Meta  struct {
		Collection           string `json:"collection"`
		UnknownCapturedCount int64  `json:"unknown_captured_count"`
		BaselineOnly         bool   `json:"baseline_only"`
		Day                  string `json:"day"`
	} `json:"meta"`
	NextCursor *string `json:"next_cursor"`
}

func newAdminPhotosCmd(f *adminFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "photos", Short: "Inspect and authorize individual photos"}

	get := &cobra.Command{
		Use:   "get <photo-id>",
		Short: "Show one photo including its relative path and error codes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			var out photoAdminItem
			if err := c.Do(cmd.Context(), "GET", "/api/v1/photos/"+args[0]+"/admin", nil, &out); err != nil {
				return err
			}
			if f.jsonOut {
				return printJSON(cmd.OutOrStdout(), out)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "id           %s\n", out.ID)
			fmt.Fprintf(w, "source       %s\n", out.SourceID)
			fmt.Fprintf(w, "path         %s\n", out.RelPath)
			fmt.Fprintf(w, "size         %d bytes (%s)\n", out.SizeBytes, out.Ext)
			fmt.Fprintf(w, "status       %s\n", out.Status)
			fmt.Fprintf(w, "captured     %s (%s)\n", fmtPtr(out.CapturedAt), out.CapturedConfidence)
			fmt.Fprintf(w, "first seen   %s%s\n", out.FirstSeenAt, baselineSuffix(out.IsBaseline))
			fmt.Fprintf(w, "dimensions   %dx%d\n", out.Width, out.Height)
			fmt.Fprintf(w, "metadata     %s%s\n", out.MetaStatus, detailSuffix(out.MetaError))
			fmt.Fprintf(w, "preview      %s%s, %d attempts, next retry %s\n",
				out.Preview.Status, detailSuffix(out.PreviewError), out.PreviewAttempts, fmtPtr(out.PreviewNextRetryAt))
			fmt.Fprintf(w, "missing gens %d\n", out.MissingGenerations)
			if out.ExcludedAt != nil {
				fmt.Fprintf(w, "excluded     %s%s\n", *out.ExcludedAt, detailSuffix(out.ExcludeReason))
			}
			return nil
		},
	}

	var collection string
	var limit int
	list := &cobra.Command{
		Use:   "list",
		Short: "List a collection as a screen would see it",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			q := url.Values{}
			if collection != "" {
				q.Set("collection", collection)
			}
			if limit > 0 {
				q.Set("limit", fmt.Sprint(limit))
			}
			path := "/api/v1/photos"
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			var out photoList
			if err := c.Do(cmd.Context(), "GET", path, nil, &out); err != nil {
				return err
			}
			if f.jsonOut {
				return printJSON(cmd.OutOrStdout(), out)
			}
			t := newTable(cmd.OutOrStdout())
			fmt.Fprintln(t, "ID\tSOURCE\tCAPTURED\tCONFIDENCE\tPREVIEW\tFIRST SEEN")
			for _, p := range out.Items {
				fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\t%s\n",
					p.ID, p.SourceID, fmtPtr(p.CapturedAt), p.CapturedConfidence, p.Preview.Status, p.FirstSeenAt)
			}
			if len(out.Items) == 0 {
				fmt.Fprintln(t, "(collection is empty)\t\t\t\t\t")
			}
			if err := t.Flush(); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "\n%d shown; %d photos have no known capture time\n",
				len(out.Items), out.Meta.UnknownCapturedCount)
			if out.Meta.BaselineOnly {
				fmt.Fprintln(w, "nothing new since the first import (baseline only)")
			}
			return nil
		},
	}
	list.Flags().StringVar(&collection, "collection", "recent", "recent | captured_today | random | all")
	list.Flags().IntVar(&limit, "limit", 0, "maximum items (default 50, max 100)")

	var reason string
	exclude := &cobra.Command{
		Use:   "exclude <photo-id>",
		Short: "Revoke one photo; the rule survives a full rescan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			body := map[string]string{"reason": reason}
			if err := c.Do(cmd.Context(), "POST", "/api/v1/photos/"+args[0]+"/exclude", body, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "excluded %s; its preview is deleted and the media route now returns 404\n", args[0])
			return nil
		},
	}
	exclude.Flags().StringVar(&reason, "reason", "", "why the photo is excluded (stored in the audit log)")

	cmd.AddCommand(get, list, exclude,
		photoActionCmd(f, "include", "Undo an exclude and re-queue the pipeline", "/include", "included %s\n"),
		photoActionCmd(f, "retry", "Clear the failure state and re-queue metadata and preview", "/retry", "re-queued %s\n"),
	)
	return cmd
}

func photoActionCmd(f *adminFlags, name, short, path, message string) *cobra.Command {
	return &cobra.Command{
		Use:   name + " <photo-id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			if err := c.Do(cmd.Context(), "POST", "/api/v1/photos/"+args[0]+path, nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), message, args[0])
			return nil
		},
	}
}

func baselineSuffix(isBaseline bool) string {
	if isBaseline {
		return " (baseline import)"
	}
	return ""
}
