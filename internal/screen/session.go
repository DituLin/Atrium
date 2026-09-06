package screen

import (
	"context"
	"errors"

	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/ws"
)

// OnHeartbeat records presence and the client-reported state, then annotates
// any command left `unknown` with what the screen now reports (design §6.5).
func (s *Service) OnHeartbeat(ctx context.Context, screenID, ip string, hb ws.Heartbeat) error {
	now := s.now()
	if err := s.db.Screens().RecordHeartbeat(ctx, screenID, ip, hb.ClientVersion,
		hb.Route, hb.AppliedSequence, now); err != nil {
		return err
	}
	s.annotateUnknown(ctx, screenID, hb.Route, hb.AppliedSequence)
	return nil
}

// OnState records a route change the remote made on its own.
func (s *Service) OnState(ctx context.Context, screenID string, route *domain.RouteState) error {
	return s.db.Screens().RecordHeartbeat(ctx, screenID, "", "", route, 0, s.now())
}

// OnAck applies the client's decision. A terminal command is never rewritten;
// a duplicate or late acknowledgement is logged at debug and dropped.
func (s *Service) OnAck(ctx context.Context, screenID string, ack ws.CommandAck) error {
	cmd, err := s.db.Commands().Get(ctx, ack.CommandID)
	if errors.Is(err, domain.ErrNotFound) {
		s.log.Debug("ack for unknown command", "component", "screen",
			"event", "ack_unknown_command", "screen_id", screenID, "command_id", ack.CommandID)
		return nil
	}
	if err != nil {
		return err
	}
	if cmd.ScreenID != screenID {
		// A screen may only acknowledge its own commands.
		s.log.Warn("ack for another screen", "component", "screen",
			"event", "ack_wrong_screen", "screen_id", screenID, "command_id", ack.CommandID)
		return nil
	}

	status := domain.CommandFailed
	if ack.Status == domain.CommandApplied {
		status = domain.CommandApplied
	}
	result := &domain.CommandResult{Route: ack.Route, ResourceID: ack.ResourceID}
	changed, err := s.db.Commands().Resolve(ctx, cmd.ID, status, ack.ErrorCode, result, s.now())
	if err != nil {
		return err
	}
	if !changed {
		s.log.Debug("ack for a terminal command ignored", "component", "screen",
			"event", "ack_ignored", "command_id", cmd.ID, "status", string(cmd.Status))
		return nil
	}
	s.log.Info("command resolved", "component", "screen", "event", "command_resolved",
		"command_id", cmd.ID, "screen_id", screenID, "status", string(status), "code", ack.ErrorCode)
	return nil
}

// PendingCommand returns the newest still-open command so a reconnecting
// client can be told about it in session.ready. Redelivery is safe because the
// client dedupes by command_id and applies only a newer sequence.
func (s *Service) PendingCommand(ctx context.Context, screenID string) (*domain.Command, error) {
	cmd, err := s.db.Commands().NewestPending(ctx, screenID, s.now())
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil //nolint:nilnil // "no pending command" is not an error
	}
	if err != nil {
		return nil, err
	}
	if cmd.DeliveredAt == nil {
		if err := s.db.Commands().MarkDelivered(ctx, cmd.ID, s.now()); err != nil {
			return nil, err
		}
	}
	return cmd, nil
}

// MarkOffline is called when a session ends. Presence is owned by the hub, so
// nothing is written except the last-seen timestamp already recorded; the hook
// exists so the service can log and so tests can observe session teardown.
func (s *Service) MarkOffline(_ context.Context, screenID string) error {
	s.log.Debug("session ended", "component", "screen",
		"event", "session_ended", "screen_id", screenID)
	return nil
}

// annotateUnknown attaches result.observed to commands stuck at `unknown`.
// The status itself is never flipped: only the operator decides what an
// unverifiable outcome means (design §6.5).
func (s *Service) annotateUnknown(ctx context.Context, screenID string,
	route *domain.RouteState, appliedSeq int64) {
	rows, err := s.db.Commands().ListUnknownMissingObserved(ctx, screenID, 20)
	if err != nil || len(rows) == 0 {
		return
	}
	observed := &domain.ObservedState{AppliedSequence: appliedSeq, Route: route}
	for i := range rows {
		result := rows[i].Result
		if result == nil {
			result = &domain.CommandResult{}
		}
		result.Observed = observed
		if err := s.db.Commands().SetResult(ctx, rows[i].ID, result); err != nil {
			s.log.Warn("observed state not stored", "component", "screen",
				"event", "observed_write_failed", "command_id", rows[i].ID)
			return
		}
	}
}

// compile-time proof that the service satisfies the hub's handler contract.
var _ ws.Handler = (*Service)(nil)
