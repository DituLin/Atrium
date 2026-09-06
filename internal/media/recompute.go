package media

import (
	"context"

	"github.com/DituLin/Atrium/internal/domain"
)

// RecomputeBatch is the page size of the recompute_day job (design §6.2).
const RecomputeBatch = 500

// RecomputeDay rewrites captured_day for every photo with a known capture
// time. It runs after the home timezone changes: captured_at is an instant and
// stays correct, but "which day was that at home" does not.
func (p *Pipeline) RecomputeDay(ctx context.Context, _ *domain.Job) error {
	repo := p.opts.DB.Photos()
	now := p.now()
	var (
		after   string
		updated int
	)
	for {
		rows, err := repo.ListCapturedAfter(ctx, after, RecomputeBatch)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			day := p.opts.Home.HomeDay(row.CapturedAt)
			if err := repo.SetCapturedDay(ctx, row.ID, day, now); err != nil {
				return err
			}
			updated++
		}
		after = rows[len(rows)-1].ID
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	p.opts.Logger.Info("captured days recomputed", "component", "media",
		"event", "recompute_day", "photos", updated)
	p.publish(domain.TopicPhotos, domain.TopicHome)
	return nil
}
