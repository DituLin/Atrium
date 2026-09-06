package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// screenItem mirrors the screen DTO of design §8.
type screenItem struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Status          string  `json:"status"`
	Registered      bool    `json:"registered"`
	Online          bool    `json:"online"`
	ApprovedAt      string  `json:"approved_at"`
	LastSeenAt      *string `json:"last_seen_at"`
	ClientVersion   string  `json:"client_version"`
	LastSequence    int64   `json:"last_sequence"`
	AppliedSequence int64   `json:"applied_sequence"`
}

type screenList struct {
	Screens []screenItem `json:"screens"`
}

func newAdminScreensCmd(f *adminFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "screens", Short: "Inspect and revoke paired screens"}

	list := &cobra.Command{
		Use:   "list",
		Short: "List registered screens with their presence",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			var out screenList
			if err := c.Do(cmd.Context(), "GET", "/api/v1/screens", nil, &out); err != nil {
				return err
			}
			if f.jsonOut {
				return printJSON(cmd.OutOrStdout(), out)
			}
			t := newTable(cmd.OutOrStdout())
			fmt.Fprintln(t, "ID\tNAME\tSTATUS\tONLINE\tAPPLIED SEQ\tLAST SEEN")
			for _, s := range out.Screens {
				fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%d\t%s\n",
					s.ID, s.Name, s.Status, fmtBool(s.Online, "yes", "no"), s.AppliedSequence, fmtPtr(s.LastSeenAt))
			}
			if len(out.Screens) == 0 {
				fmt.Fprintln(t, "(no screens paired)\t\t\t\t\t")
			}
			return t.Flush()
		},
	}

	get := &cobra.Command{
		Use:   "get <screen-id>",
		Short: "Show one screen",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			var out screenItem
			if err := c.Do(cmd.Context(), "GET", "/api/v1/screens/"+args[0], nil, &out); err != nil {
				return err
			}
			if f.jsonOut {
				return printJSON(cmd.OutOrStdout(), out)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "id           %s\n", out.ID)
			fmt.Fprintf(w, "name         %s\n", out.Name)
			fmt.Fprintf(w, "status       %s\n", out.Status)
			fmt.Fprintf(w, "registered   %s\n", fmtBool(out.Registered, "yes", "no"))
			fmt.Fprintf(w, "online       %s\n", fmtBool(out.Online, "yes", "no"))
			fmt.Fprintf(w, "approved at  %s\n", out.ApprovedAt)
			fmt.Fprintf(w, "last seen    %s\n", fmtPtr(out.LastSeenAt))
			fmt.Fprintf(w, "sequence     issued %d, applied %d\n", out.LastSequence, out.AppliedSequence)
			return nil
		},
	}

	revoke := &cobra.Command{
		Use:   "revoke <screen-id>",
		Short: "Revoke a screen credential and close its session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			var out screenItem
			if err := c.Do(cmd.Context(), "DELETE", "/api/v1/screens/"+args[0], nil, &out); err != nil {
				return err
			}
			if f.jsonOut {
				return printJSON(cmd.OutOrStdout(), out)
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"revoked %s; its credential no longer authenticates and the pairing history is deleted\n", out.ID)
			return nil
		},
	}

	cmd.AddCommand(list, get, revoke)
	return cmd
}
