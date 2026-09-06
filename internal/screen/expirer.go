package screen

import (
	"context"
	"time"

	"github.com/DituLin/Atritum/internal/domain"
)

// RunExpirer ticks once a second and closes out commands whose time is up.
func (s *Service) RunExpirer(ctx context.Context) {
	ticker := time.NewTicker(ExpirerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.ExpireDue(ctx); err != nil && ctx.Err() == nil {
				s.log.Warn("command expiry pass failed", "component", "screen",
					"event", "expiry_failed", "error", err.Error())
			}
		}
	}
}

// ExpireDue resolves every accepted command whose deadline has passed and
// returns how many it closed. The distinction matters operationally: a command
// that never reached the screen is `expired` and definitely had no effect,
// while one that was delivered without an acknowledgement is `unknown` and may
// have been applied (FR-15).
func (s *Service) ExpireDue(ctx context.Context) (int, error) {
	now := s.now()
	open, err := s.db.Commands().OpenBefore(ctx, now)
	if err != nil {
		return 0, err
	}
	closed := 0
	for i := range open {
		cmd := &open[i]
		switch {
		case cmd.DeliveredAt == nil:
			s.resolve(ctx, cmd, domain.CommandExpired, CodeExpired, nil)
		case now.Sub(cmd.ExpiresAt) >= AckGrace:
			s.resolve(ctx, cmd, domain.CommandUnknown, CodeAckTimeout, nil)
		default:
			// Delivered and still inside the acknowledgement grace window.
			continue
		}
		if cmd.Status.Terminal() {
			closed++
			s.log.Info("command closed without an ack", "component", "screen",
				"event", "command_"+string(cmd.Status), "command_id", cmd.ID,
				"screen_id", cmd.ScreenID, "code", cmd.ErrorCode)
		}
	}
	return closed, nil
}

// MarkRestartUnknown implements the startup rule of design §4.2: a command
// still `accepted` when the process died has an unverifiable outcome.
func (s *Service) MarkRestartUnknown(ctx context.Context) (int64, error) {
	return s.db.Commands().MarkAllAcceptedUnknown(ctx, CodeServerRestart, s.now())
}
