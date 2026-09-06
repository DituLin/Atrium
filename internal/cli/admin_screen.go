package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/DituLin/Atrium/internal/domain"
)

// commandItem mirrors the command DTO of design §8.
type commandItem struct {
	ID          string                `json:"id"`
	ScreenID    string                `json:"screen_id"`
	Sequence    int64                 `json:"sequence"`
	Kind        string                `json:"kind"`
	Payload     domain.CommandPayload `json:"payload"`
	IssuedAt    string                `json:"issued_at"`
	ExpiresAt   string                `json:"expires_at"`
	Status      string                `json:"status"`
	DeliveredAt *string               `json:"delivered_at"`
	ResolvedAt  *string               `json:"resolved_at"`
	ErrorCode   string                `json:"error_code"`
	Result      *domain.CommandResult `json:"result"`
}

type commandList struct {
	Commands []commandItem `json:"commands"`
}

// WaitPollInterval and WaitTimeout implement the `--wait` contract of §11.
const (
	WaitPollInterval = 250 * time.Millisecond
	WaitTimeout      = 15 * time.Second
)

func newAdminScreenCmd(f *adminFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "screen",
		Short: "Control what a paired screen is showing",
	}
	cmd.AddCommand(newNavigateCmd(f), newShowCmd(f), newRefreshCmd(f))
	return cmd
}

func newNavigateCmd(f *adminFlags) *cobra.Command {
	var route, collection string
	var wait bool
	cmd := &cobra.Command{
		Use:   "navigate <screen-id>",
		Short: "Send the screen to a whitelisted local page",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := domain.CommandPayload{
				Route:      domain.RouteName(route),
				Collection: collection,
			}
			return issueCommand(cmd, f, args[0], domain.CommandNavigate, payload, wait)
		},
	}
	cmd.Flags().StringVar(&route, "route", "dashboard", "target route: dashboard or photos")
	cmd.Flags().StringVar(&collection, "collection", "",
		"collection for route photos: recent, captured_today, random or all")
	addWaitFlag(cmd, &wait)
	return cmd
}

func newShowCmd(f *adminFlags) *cobra.Command {
	var photoID string
	var wait bool
	cmd := &cobra.Command{
		Use:   "show <screen-id>",
		Short: "Display one authorized photo and pause the slideshow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if photoID == "" {
				return fmt.Errorf("cli: --photo is required")
			}
			payload := domain.CommandPayload{PhotoID: photoID}
			return issueCommand(cmd, f, args[0], domain.CommandShow, payload, wait)
		},
	}
	cmd.Flags().StringVar(&photoID, "photo", "", "photo ID from `admin photos list`")
	addWaitFlag(cmd, &wait)
	return cmd
}

func newRefreshCmd(f *adminFlags) *cobra.Command {
	var wait bool
	cmd := &cobra.Command{
		Use:   "refresh <screen-id>",
		Short: "Re-read data and re-render, keeping the current page",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return issueCommand(cmd, f, args[0], domain.CommandRefresh, domain.CommandPayload{}, wait)
		},
	}
	addWaitFlag(cmd, &wait)
	return cmd
}

func addWaitFlag(cmd *cobra.Command, wait *bool) {
	cmd.Flags().BoolVar(wait, "wait", false,
		"poll until the command is terminal (15 s) and exit non-zero unless it was applied")
}
