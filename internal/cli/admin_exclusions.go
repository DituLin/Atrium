package cli

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

type exclusionItem struct {
	ID        string `json:"id"`
	SourceID  string `json:"source_id"`
	MatchKind string `json:"match_kind"`
	Pattern   string `json:"pattern"`
	Reason    string `json:"reason"`
	CreatedAt string `json:"created_at"`
}

type exclusionList struct {
	Exclusions []exclusionItem `json:"exclusions"`
}

func newAdminExclusionsCmd(f *adminFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exclusions",
		Short: "Manage the authorization rules that survive rescans",
	}

	var sourceFilter string
	list := &cobra.Command{
		Use:   "list",
		Short: "List exclusion rules",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			path := "/api/v1/exclusions"
			if sourceFilter != "" {
				path += "?" + url.Values{"source_id": {sourceFilter}}.Encode()
			}
			var out exclusionList
			if err := c.Do(cmd.Context(), "GET", path, nil, &out); err != nil {
				return err
			}
			if f.jsonOut {
				return printJSON(cmd.OutOrStdout(), out)
			}
			t := newTable(cmd.OutOrStdout())
			fmt.Fprintln(t, "ID\tSOURCE\tKIND\tPATTERN\tREASON\tCREATED")
			for _, e := range out.Exclusions {
				fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\t%s\n",
					e.ID, e.SourceID, e.MatchKind, e.Pattern, dash(e.Reason), e.CreatedAt)
			}
			if len(out.Exclusions) == 0 {
				fmt.Fprintln(t, "(no exclusions)\t\t\t\t\t")
			}
			return t.Flush()
		},
	}
	list.Flags().StringVar(&sourceFilter, "source", "", "limit to one source")

	var addSource, prefix, path, reason string
	add := &cobra.Command{
		Use:   "add",
		Short: "Add a prefix or path exclusion",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if addSource == "" {
				return fmt.Errorf("--source is required")
			}
			if prefix == "" && path == "" {
				return fmt.Errorf("--prefix or --path is required")
			}
			c, err := f.client()
			if err != nil {
				return err
			}
			body := map[string]string{
				"source_id": addSource, "prefix": prefix, "path": path, "reason": reason,
			}
			var out exclusionItem
			if err := c.Do(cmd.Context(), "POST", "/api/v1/exclusions", body, &out); err != nil {
				return err
			}
			if f.jsonOut {
				return printJSON(cmd.OutOrStdout(), out)
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"added %s: %s %q on %s; matching photos are hidden now and stay hidden after a full rescan\n",
				out.ID, out.MatchKind, out.Pattern, out.SourceID)
			return nil
		},
	}
	add.Flags().StringVar(&addSource, "source", "", "source ID the rule applies to")
	add.Flags().StringVar(&prefix, "prefix", "", "directory prefix relative to the root, e.g. private/")
	add.Flags().StringVar(&path, "path", "", "single relative path")
	add.Flags().StringVar(&reason, "reason", "", "why the rule exists")

	remove := &cobra.Command{
		Use:   "remove <exclusion-id>",
		Short: "Delete an exclusion rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			if err := c.Do(cmd.Context(), "DELETE", "/api/v1/exclusions/"+args[0], nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"removed %s; already-indexed photos stay excluded until they are included again\n", args[0])
			return nil
		},
	}

	cmd.AddCommand(list, add, remove)
	return cmd
}
