package video

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/source"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
)

type stubProcessor struct {
	afterProbe func()
	afterCover func()
	probeErr   error
}

func (p stubProcessor) Probe(context.Context, *os.File) (domain.VideoMetadata, error) {
	if p.afterProbe != nil {
		p.afterProbe()
	}
	return domain.VideoMetadata{Container: "mp4", VideoCodec: "h264", Width: 10, Height: 10, DurationMS: 1000}, p.probeErr
}
func (p stubProcessor) Cover(context.Context, *os.File) ([]byte, error) {
	var b bytes.Buffer
	_ = jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 10, 10)), nil)
	if p.afterCover != nil {
		p.afterCover()
	}
	return b.Bytes(), nil
}

func workerFixture(t *testing.T, p stubProcessor, budget int64) (*Worker, *store.DB, string, string) {
	t.Helper()
	ctx := context.Background()
	db := testutil.NewDB(t)
	root := t.TempDir()
	file := filepath.Join(root, "a.mp4")
	require.NoError(t, os.WriteFile(file, []byte("video"), 0600))
	info, err := os.Stat(file)
	require.NoError(t, err)
	cfg := &config.Config{Sources: []config.Source{{ID: "mixed", Name: "mixed", Root: root, IncludeExtensions: []string{"mp4"}, Identity: config.Identity{AllowLocal: true}}}}
	require.NoError(t, db.Sources().Upsert(ctx, "mixed", "mixed", root, time.Now()))
	sources, err := source.NewManager(source.ManagerOptions{Config: cfg, DB: db})
	require.NoError(t, err)
	require.Equal(t, domain.HealthOnline, sources.Probe(ctx, sources.Get("mixed")).Health)
	v, err := db.Videos().Observe(ctx, domain.VideoObservation{SourceID: "mixed", RelPath: "a.mp4", SizeBytes: info.Size(), MtimeUnix: info.ModTime().Unix(), Generation: 1}, time.Now())
	require.NoError(t, err)
	cache := NewCoverCache(t.TempDir(), budget, 0, nil)
	worker, err := NewWorker(WorkerOptions{DB: db, Sources: sources, Processor: p, Cache: cache})
	require.NoError(t, err)
	return worker, db, v.ID, file
}

func TestWorkerPublishesAndDiscardsChangedOrRevokedResults(t *testing.T) {
	for _, scenario := range []string{"success", "changed", "revoked", "budget"} {
		t.Run(scenario, func(t *testing.T) {
			budget := int64(1 << 20)
			if scenario == "budget" {
				budget = 1
			}
			w, db, id, file := workerFixture(t, stubProcessor{}, budget)
			p := stubProcessor{}
			if scenario == "changed" {
				p.afterCover = func() { require.NoError(t, os.WriteFile(file, []byte("changed-video"), 0600)) }
			}
			if scenario == "revoked" {
				p.afterProbe = func() { require.NoError(t, db.Sources().Revoke(context.Background(), "mixed", "test", time.Now())) }
			}
			w.opts.Processor = p
			worked, err := w.RunOnce(context.Background())
			require.True(t, worked)
			v, getErr := db.Videos().Get(context.Background(), id)
			require.NoError(t, getErr)
			used, cacheErr := w.opts.Cache.Bytes()
			require.NoError(t, cacheErr)
			if scenario == "success" {
				require.NoError(t, err)
				require.Equal(t, domain.VideoReady, v.Status)
				require.Positive(t, used)
			} else {
				require.Error(t, err)
				require.Equal(t, domain.VideoPending, v.Status)
				require.Zero(t, used)
			}
		})
	}
}

func TestWorkerTimeoutKeepsSlotUntilProcessingExits(t *testing.T) {
	exit := make(chan struct{})
	w, _, _, _ := workerFixture(t, stubProcessor{afterProbe: func() { <-exit }}, 1<<20)
	w.opts.Timeout = 30 * time.Millisecond
	_, err := w.RunOnce(context.Background())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	_, err = w.RunOnce(context.Background())
	require.ErrorIs(t, err, ErrToolBusy)
	close(exit)
	require.Eventually(t, func() bool { return len(w.slot) == 0 }, time.Second, time.Millisecond)
	used, err := w.opts.Cache.Bytes()
	require.NoError(t, err)
	require.Zero(t, used)
}

func TestWorkerRebuildsMissingCover(t *testing.T) {
	w, db, _, _ := workerFixture(t, stubProcessor{}, 1<<20)
	ctx := context.Background()
	worked, err := w.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, worked)
	tokens, err := db.VideoWork().RetainedTokens(ctx, time.Now())
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	for token := range tokens {
		require.NoError(t, w.opts.Cache.Remove(token))
	}
	worked, err = w.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, worked, "missing published cover must become claimable again")
	replacement, err := db.VideoWork().RetainedTokens(ctx, time.Now())
	require.NoError(t, err)
	require.Len(t, replacement, 1)
	for token := range replacement {
		require.False(t, tokens[token], "rebuild must publish a fresh token")
		data, err := w.opts.Cache.Read(token)
		require.NoError(t, err)
		require.NotEmpty(t, data)
	}
	worked, err = w.RunOnce(ctx)
	require.NoError(t, err)
	require.False(t, worked, "intact cover must not be rebuilt")
}

func TestWorkerStopsInvalidMetadataUntilExplicitRetry(t *testing.T) {
	w, db, id, _ := workerFixture(t, stubProcessor{probeErr: ErrMetadata}, 1<<20)
	ctx := context.Background()
	worked, err := w.RunOnce(ctx)
	require.True(t, worked)
	require.ErrorIs(t, err, ErrMetadata)
	v, err := db.Videos().Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, domain.VideoUnsupported, v.Status)
	worked, err = w.RunOnce(ctx)
	require.NoError(t, err)
	require.False(t, worked)
	ok, err := db.VideoWork().RequestRetry(ctx, id, v.Revision, time.Now())
	require.NoError(t, err)
	require.True(t, ok)
	w.opts.Processor = stubProcessor{}
	worked, err = w.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, worked)
	v, err = db.Videos().Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, domain.VideoReady, v.Status)
}

func TestWorkerDoesNotMarkChangedFileOrUnavailableToolUnsupported(t *testing.T) {
	for _, scenario := range []string{"changed", "tool"} {
		t.Run(scenario, func(t *testing.T) {
			w, db, id, file := workerFixture(t, stubProcessor{}, 1<<20)
			p := stubProcessor{probeErr: ErrToolUnavailable}
			if scenario == "changed" {
				p.probeErr = ErrMetadata
				p.afterProbe = func() { require.NoError(t, os.WriteFile(file, []byte("new file bytes"), 0600)) }
			}
			w.opts.Processor = p
			worked, err := w.RunOnce(context.Background())
			require.True(t, worked)
			require.Error(t, err)
			v, err := db.Videos().Get(context.Background(), id)
			require.NoError(t, err)
			require.Equal(t, domain.VideoPending, v.Status)
			worked, err = w.RunOnce(context.Background())
			require.NoError(t, err)
			require.False(t, worked, "transient errors must back off")
		})
	}
}
