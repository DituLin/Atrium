package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

// ObservationLease binds a probe to the source authorization it started under.
// Its opaque generation lives in settings so existing databases need no schema
// migration. Missing keys describe pre-upgrade sources until their first change.
type ObservationLease struct {
	Source     domain.Source
	generation string
}

func observationKey(id string) string { return "source_observation_generation:" + id }

func (s *Sources) observationGeneration(ctx context.Context, id string) (string, error) {
	var generation string
	err := s.ex.QueryRowContext(ctx, "SELECT value FROM settings WHERE key=?", observationKey(id)).Scan(&generation)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return generation, err
}

func (s *Sources) rotateObservation(ctx context.Context, id string, now time.Time) error {
	_, err := s.ex.ExecContext(ctx, `INSERT INTO settings (key,value,updated_at) VALUES (?,?,?)
  ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		observationKey(id), rand.Text(), FormatTime(now))
	return err
}

// resetObservation runs inside the same transaction as its lifecycle change.
// Identity remains bound: reauthorization must not silently trust another mount.
func (s *Sources) resetObservation(ctx context.Context, id string, now time.Time) error {
	_, err := s.ex.ExecContext(ctx, `UPDATE data_sources SET health='unknown', health_detail=NULL,
  last_check_at=NULL, last_success_at=NULL, share_total_bytes=NULL, share_free_bytes=NULL,
  share_stats_at=NULL WHERE id=?`, id)
	if err != nil {
		return err
	}
	return s.rotateObservation(ctx, id, now)
}

// BeginObservation checks authorization and root before any filesystem I/O.
func (s *Sources) BeginObservation(ctx context.Context, id, root string) (ObservationLease, error) {
	var lease ObservationLease
	err := s.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		repo := &Sources{db: s.db, ex: tx}
		row, err := repo.Get(ctx, id)
		if err != nil {
			return err
		}
		if row.Status != domain.SourceActive || row.RootPath != root {
			return domain.ErrConflict
		}
		generation, err := repo.observationGeneration(ctx, id)
		if err != nil {
			return err
		}
		lease = ObservationLease{Source: *row, generation: generation}
		return nil
	})
	return lease, err
}

// SourceObservation is one complete persisted probe result. Nil capacity keeps
// the previous capacity timestamp; callers supply it only for meaningful reads.
type SourceObservation struct {
	Health     domain.Health
	Detail     string
	Success    bool
	Identity   *domain.Identity
	TotalBytes *int64
	FreeBytes  *int64
	At         time.Time
}

// CommitObservation atomically rejects superseded authorization before changing
// identity, health, or capacity. First binding and rebinding supersede pending
// probes; late observations cannot move the persisted check time backwards.
func (s *Sources) CommitObservation(ctx context.Context, lease ObservationLease, observation SourceObservation, rebind bool) (bool, error) {
	committed := false
	err := s.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		// InWriteTx may retry after a rolled-back busy attempt.
		committed = false
		repo := &Sources{db: s.db, ex: tx}
		row, err := repo.Get(ctx, lease.Source.ID)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		generation, err := repo.observationGeneration(ctx, row.ID)
		if err != nil {
			return err
		}
		if generation != lease.generation || row.Status != domain.SourceActive || row.RootPath != lease.Source.RootPath {
			return nil
		}
		if row.LastCheckAt != nil && observation.At.Before(*row.LastCheckAt) {
			return nil
		}
		firstBinding := row.IdentityBound == nil && observation.Identity != nil
		if observation.Identity != nil {
			if err := repo.BindIdentity(ctx, row.ID, *observation.Identity, observation.At); err != nil {
				return err
			}
		}
		if err := repo.SetHealth(ctx, row.ID, observation.Health, observation.Detail, observation.Success, observation.At); err != nil {
			return err
		}
		if observation.TotalBytes != nil && observation.FreeBytes != nil {
			if err := repo.SetShareStats(ctx, row.ID, *observation.TotalBytes, *observation.FreeBytes, observation.At); err != nil {
				return err
			}
		}
		if rebind || firstBinding {
			if err := repo.rotateObservation(ctx, row.ID, observation.At); err != nil {
				return err
			}
		}
		committed = true
		return nil
	})
	return committed && err == nil, err
}
