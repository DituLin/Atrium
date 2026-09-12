package indexer

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/DituLin/Atrium/internal/jobs"
	"github.com/DituLin/Atrium/internal/store"
)

// BatchSize is how many files one index transaction covers. Writing each file
// in its own auto-commit transaction takes the SQLite write lock tens of
// thousands of times during a first scan, which is what starves the job pool;
// one commit per BatchSize files keeps the lock hold short and the number of
// acquisitions two orders of magnitude lower. It matches ProgressFlushEvery so
// the scan counters are written between batches, never inside one.
const BatchSize = ProgressFlushEvery

// batch groups index writes into bounded transactions. While a batch is open
// the scanner's repositories are bound to it, so nothing in the apply path
// writes on the shared pool and blocks against the lock the batch holds.
type batch struct {
	scanner *Scanner
	ctx     context.Context //nolint:containedctx // the batch lives inside one scan call
	tx      *sql.Tx
	pending []func() error
}

func newBatch(ctx context.Context, s *Scanner) *batch {
	return &batch{scanner: s, ctx: ctx}
}

// run buffers reconciliation work while the caller performs network I/O.
// Only flush opens a transaction: never retain SQLite's writer lock across
// ReadDir, DirEntry.Info, Stat, or stability waits on a slow NAS.
func (b *batch) run(fn func() error) error {
	b.pending = append(b.pending, fn)
	if len(b.pending) >= BatchSize {
		return b.flush()
	}
	return nil
}

// flush applies an in-memory batch in a short database-only transaction.
func (b *batch) flush() error {
	if len(b.pending) == 0 {
		return nil
	}
	tx, err := b.scanner.db.BeginWrite(b.ctx)
	if err != nil {
		return err
	}
	b.tx, b.scanner.tx = tx, tx
	defer b.rollback()
	for _, fn := range b.pending {
		if err := fn(); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("indexer: commit batch: %w", err)
	}
	return nil
}

func (b *batch) rollback() {
	tx := b.tx
	b.tx, b.scanner.tx, b.pending = nil, nil, nil
	if tx != nil {
		_ = tx.Rollback()
	}
}

// photos returns the photo repository bound to the open batch, if any.
func (s *Scanner) photos() *store.Photos {
	if s.tx != nil {
		return s.db.Photos().WithTx(s.tx)
	}
	return s.db.Photos()
}

// previews returns the preview repository bound to the open batch, if any.
func (s *Scanner) previews() *store.Previews {
	if s.tx != nil {
		return s.db.Previews().WithTx(s.tx)
	}
	return s.db.Previews()
}

// queueIn returns the job queue bound to the open batch, if any.
func (s *Scanner) queueIn() *jobs.Queue {
	if s.tx != nil {
		return s.queue.WithTx(s.tx)
	}
	return s.queue
}
