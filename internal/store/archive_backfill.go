package store

import (
	"context"
)

// ArchiveBackfillStats reports completed legacy projections and search indexing work.
type ArchiveBackfillStats struct {
	Scanned          int `json:"scanned"`
	PendingProjected int `json:"pending_projected"`
	Indexed          int `json:"indexed"`
}

// BackfillArchives can be interrupted after any record. Each replay rereads the
// entire current revision chain under its writer lock; no high watermark hides
// corrections to previously scanned roots.
func (d *DB) BackfillArchives(ctx context.Context) (stats ArchiveBackfillStats, err error) {
	if _, err = d.ValidateSchema(ctx); err != nil {
		return
	}
	after := ""
	for {
		rr, e := d.sql.QueryContext(ctx, `SELECT id FROM family_records WHERE id>? ORDER BY id LIMIT 100`, after)
		if e != nil {
			return stats, e
		}
		ids := []string{}
		for rr.Next() {
			var id string
			if e = rr.Scan(&id); e != nil {
				_ = rr.Close() // Read-only query cursor cleanup.
				return stats, e
			}
			ids = append(ids, id)
		}
		e = rr.Err()
		_ = rr.Close() // Read-only query cursor cleanup.
		if e != nil {
			return stats, e
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			if e = d.ProjectLegacyArchive(ctx, id); e != nil {
				return stats, e
			}
			stats.Scanned++
			after = id
		}
	}
	for {
		n, e := d.ProjectPendingLegacyArchives(ctx, 100)
		stats.PendingProjected += n
		if e != nil {
			return stats, e
		}
		if n == 0 {
			break
		}
	}
	h, e := d.LocalHousehold(ctx)
	if e != nil {
		return stats, e
	}
	for {
		n, e := d.ArchiveTasks().IndexArchives(ctx, h.ID, 32)
		stats.Indexed += n
		if e != nil {
			return stats, e
		}
		if n == 0 {
			break
		}
	}
	return
}
