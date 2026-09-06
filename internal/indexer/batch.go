package indexer

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/DituLin/Atritum/internal/jobs"
	"github.com/DituLin/Atritum/internal/store"
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
	n       int
}

func newBatch(ctx context.Context, s *Scanner) *batch {
	return &batch{scanner: s, ctx: ctx}
}

// run executes fn inside the current batch, opening one if needed and
// committing once BatchSize writes have accumulated.
func (b *batch) run(fn func() error) error {
	if b.tx == nil {
		tx, err := b.scanner.db.BeginWrite(b.ctx)
		if err != nil {
			return err
		}
		b.tx = tx
		b.scanner.tx = tx
	}
	if err := fn(); err != nil {
		b.rollback()
		return err
	}
	b.n++
	if b.n >= BatchSize {
		return b.flush()
	}
	return nil
}

// flush commits whatever the batch holds; it is safe to call when empty.
func (b *batch) flush() error {
	tx := b.tx
	b.tx, b.scanner.tx, b.n = nil, nil, 0
	if tx == nil {
		return nil
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("indexer: commit batch: %w", err)
	}
	return nil
}

// rollback discards the open batch. The files it covered are simply not
// indexed by this run; the next scan sees them again.
func (b *batch) rollback() {
	tx := b.tx
	b.tx, b.scanner.tx, b.n = nil, nil, 0
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
