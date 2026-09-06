package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/spf13/cobra"

	"github.com/DituLin/Atrium/internal/domain"
)

// ErrCommandNotApplied is the non-zero exit of `--wait` when the screen did
// not confirm the target state. It is deliberately distinguishable from a
// transport error so a script can tell "the TV said no" from "the API is down".
var ErrCommandNotApplied = errors.New("command did not reach applied")

// issueCommand posts one command and, with --wait, follows it to a terminal
// state. The API accepting a command is not the same as the screen applying
// it, which is exactly what FR-15 asks the CLI to make visible.
func issueCommand(cmd *cobra.Command, f *adminFlags, screenID string,
	kind domain.CommandKind, payload domain.CommandPayload, wait bool) error {
	c, err := f.client()
	if err != nil {
		return err
	}
	body := map[string]any{"kind": string(kind), "payload": payload}
	var issued commandItem
	err = c.Do(cmd.Context(), "POST", "/api/v1/screens/"+url.PathEscape(screenID)+"/commands", body, &issued)
	if err != nil {
		var offline *OfflineError
		if errors.As(err, &offline) {
			printCommand(cmd.OutOrStdout(), offline.Command, f.jsonOut)
			return fmt.Errorf("screen %s is offline; nothing was queued", screenID)
		}
		return err
	}
	if !wait {
		printCommand(cmd.OutOrStdout(), issued, f.jsonOut)
		return nil
	}
	final, elapsed, err := waitForCommand(cmd.Context(), c, issued.ID)
	if err != nil {
		return err
	}
	printCommand(cmd.OutOrStdout(), final, f.jsonOut)
	if !f.jsonOut {
		fmt.Fprintf(cmd.OutOrStdout(), "elapsed      %d ms\n", elapsed.Milliseconds())
	}
	if final.Status != string(domain.CommandApplied) {
		return fmt.Errorf("%w: %s%s", ErrCommandNotApplied, final.Status, codeSuffix(final.ErrorCode))
	}
	return nil
}

// waitForCommand polls every 250 ms up to 15 s (design §11).
func waitForCommand(ctx context.Context, c *Client, id string) (commandItem, time.Duration, error) {
	start := time.Now()
	deadline := start.Add(WaitTimeout)
	ticker := time.NewTicker(WaitPollInterval)
	defer ticker.Stop()
	var last commandItem
	for {
		if err := c.Do(ctx, "GET", "/api/v1/commands/"+url.PathEscape(id), nil, &last); err != nil {
			return last, time.Since(start), err
		}
		if domain.CommandStatus(last.Status).Terminal() {
			return last, time.Since(start), nil
		}
		if time.Now().After(deadline) {
			// The server owns expiry, so a still-accepted command here means
			// the expirer has not run yet; report what is known.
			return last, time.Since(start), nil
		}
		select {
		case <-ctx.Done():
			return last, time.Since(start), ctx.Err()
		case <-ticker.C:
		}
	}
}

func codeSuffix(code string) string {
	if code == "" {
		return ""
	}
	return " (" + code + ")"
}

func printCommand(w io.Writer, c commandItem, jsonOut bool) {
	if jsonOut {
		_ = printJSON(w, c)
		return
	}
	fmt.Fprintf(w, "id           %s\n", c.ID)
	fmt.Fprintf(w, "screen       %s\n", c.ScreenID)
	fmt.Fprintf(w, "kind         %s\n", c.Kind)
	fmt.Fprintf(w, "sequence     %d\n", c.Sequence)
	fmt.Fprintf(w, "status       %s%s\n", c.Status, codeSuffix(c.ErrorCode))
	fmt.Fprintf(w, "issued at    %s\n", c.IssuedAt)
	fmt.Fprintf(w, "expires at   %s\n", c.ExpiresAt)
	fmt.Fprintf(w, "delivered    %s\n", fmtPtr(c.DeliveredAt))
	fmt.Fprintf(w, "resolved     %s\n", fmtPtr(c.ResolvedAt))
	if c.Result != nil && c.Result.Route != nil {
		fmt.Fprintf(w, "final route  %s\n", routeText(c.Result.Route))
	}
	if c.Result != nil && c.Result.Observed != nil {
		fmt.Fprintf(w, "observed     screen reports sequence %d on route %s\n",
			c.Result.Observed.AppliedSequence, routeText(c.Result.Observed.Route))
	}
}

func routeText(r *domain.RouteState) string {
	if r == nil {
		return "-"
	}
	out := string(r.Name)
	if r.Collection != "" {
		out += "/" + r.Collection
	}
	if r.PhotoID != "" {
		out += "#" + r.PhotoID
	}
	return out
}

func newAdminCommandsCmd(f *adminFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "commands", Short: "Inspect issued screen commands"}

	get := &cobra.Command{
		Use:   "get <command-id>",
		Short: "Show one command and its result",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			var out commandItem
			if err := c.Do(cmd.Context(), "GET", "/api/v1/commands/"+url.PathEscape(args[0]), nil, &out); err != nil {
				return err
			}
			printCommand(cmd.OutOrStdout(), out, f.jsonOut)
			return nil
		},
	}

	var screenID, status string
	var limit int
	list := &cobra.Command{
		Use:   "list",
		Short: "List recent commands",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			q := url.Values{}
			if screenID != "" {
				q.Set("screen_id", screenID)
			}
			if status != "" {
				q.Set("status", status)
			}
			if limit > 0 {
				q.Set("limit", fmt.Sprint(limit))
			}
			path := "/api/v1/commands"
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			var out commandList
			if err := c.Do(cmd.Context(), "GET", path, nil, &out); err != nil {
				return err
			}
			if f.jsonOut {
				return printJSON(cmd.OutOrStdout(), out)
			}
			t := newTable(cmd.OutOrStdout())
			fmt.Fprintln(t, "ID\tSCREEN\tKIND\tSEQ\tSTATUS\tCODE\tISSUED")
			for _, c := range out.Commands {
				fmt.Fprintf(t, "%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
					c.ID, c.ScreenID, c.Kind, c.Sequence, c.Status, orDash(c.ErrorCode), c.IssuedAt)
			}
			if len(out.Commands) == 0 {
				fmt.Fprintln(t, "(no commands)\t\t\t\t\t\t")
			}
			return t.Flush()
		},
	}
	list.Flags().StringVar(&screenID, "screen", "", "filter by screen ID")
	list.Flags().StringVar(&status, "status", "",
		"filter by status: accepted, applied, failed, expired or unknown")
	list.Flags().IntVar(&limit, "limit", 0, "maximum rows (1-200)")

	cmd.AddCommand(get, list)
	return cmd
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
