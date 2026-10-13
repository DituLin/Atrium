package screen_test

import (
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/ws"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestArchiveCommandNeedsCurrentAuthorityAndRenderedView(t *testing.T) {
	for _, scenario := range []string{"applied", "wrong_result", "revoked", "screen_revoked", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			f, p := integrationFixture(t)
			ctx := t.Context()
			h, e := f.db.LocalHousehold(ctx)
			require.NoError(t, e)
			principal, e := f.db.Integrations().Get(ctx, p)
			require.NoError(t, e)
			principal.Policy.Permissions = []string{"archive.read", "screens.control", "commands.read"}
			principal.Policy.HouseholdIDs = []string{h.ID}
			principal.Policy.SourceIDs = nil
			require.NoError(t, f.db.Integrations().UpdatePolicy(ctx, p, principal.Policy, f.clock.Now()))
			r := f.db.ArchiveTasksWithClock(f.clock)
			actor := domain.IntegrationActor{PrincipalID: p, CredentialID: "credential"}
			receipt, e := r.SearchArchives(ctx, actor, h.ID, false, archive.SearchFilter{}, []byte("test"))
			require.NoError(t, e)
			operation := domain.NewIDAt(f.clock.Now())
			view, e := r.CreateArchiveScreenView(ctx, actor, h.ID, receipt.QueryRef, "living_room_tv", operation)
			require.NoError(t, e)
			cmd, e := f.svc.IssueIntegration(ctx, p, operation, "living_room_tv", domain.CommandArchive, domain.CommandPayload{ArchiveViewID: view.ID})
			require.NoError(t, e, "archive display must not need NAS sources")
			require.Equal(t, domain.CommandAccepted, cmd.Status)
			ack := ws.CommandAck{CommandID: cmd.ID, Status: domain.CommandApplied, Route: &domain.RouteState{Name: domain.RouteFamily}, ResourceID: view.ID}
			switch scenario {
			case "wrong_result":
				ack.ResourceID = "another-view"
			case "revoked":
				principal.Policy.Permissions = []string{"commands.read"}
				require.NoError(t, f.db.Integrations().UpdatePolicy(ctx, p, principal.Policy, f.clock.Now()))
			case "screen_revoked":
				require.NoError(t, f.db.Screens().Revoke(ctx, "living_room_tv", f.clock.Now()))
			case "expired":
				f.clock.Advance(11 * time.Second)
			}
			require.NoError(t, f.svc.OnAck(ctx, "living_room_tv", ack))
			saved, e := f.db.Commands().Get(ctx, cmd.ID)
			require.NoError(t, e)
			if scenario == "applied" {
				require.Equal(t, domain.CommandApplied, saved.Status)
				require.Equal(t, view.ID, saved.Result.ResourceID)
			} else {
				require.Equal(t, domain.CommandFailed, saved.Status)
				require.Nil(t, saved.Result)
			}
		})
	}
}
