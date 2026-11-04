package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreviewCompletionDoesNotPublishTerminalOrRevokedPhoto(t *testing.T) {
	testPreviewWriteGuards(t, func(p *store.Photos, ctx context.Context, id string, now time.Time) error {
		return p.SetPreviewReady(ctx, id, now)
	})
}

func TestPreviewStateWritesDoNotOverwriteConcurrentTerminalState(t *testing.T) {
	for name, write := range map[string]func(*store.Photos, context.Context, string, time.Time) error{
		"processing": func(p *store.Photos, ctx context.Context, id string, now time.Time) error {
			return p.SetPreviewProcessing(ctx, id, now)
		},
		"retry": func(p *store.Photos, ctx context.Context, id string, now time.Time) error {
			return p.SetPreviewRetry(ctx, id, "late_io", now.Add(time.Minute), now)
		},
		"deferral": func(p *store.Photos, ctx context.Context, id string, now time.Time) error {
			return p.SetPreviewStatus(ctx, id, domain.PreviewPending, "", now)
		},
		"failure": func(p *store.Photos, ctx context.Context, id string, now time.Time) error {
			return p.SetPreviewFailed(ctx, id, "late_io", now)
		},
		"unsupported": func(p *store.Photos, ctx context.Context, id string, now time.Time) error {
			return p.SetUnsupported(ctx, id, "late_format", now)
		},
	} {
		t.Run(name, func(t *testing.T) { testPreviewWriteGuards(t, write) })
	}
}

func testPreviewWriteGuards(t *testing.T, write func(*store.Photos, context.Context, string, time.Time) error) {
	t.Helper()
	for _, state := range []string{"unsupported", "failed", "removed", "excluded", "revoked"} {
		t.Run(state, func(t *testing.T) {
			db := testutil.NewDB(t)
			ctx := context.Background()
			now := seedSource(t, db, "family_photos")
			photo := &domain.Photo{SourceID: "family_photos", RelPath: "late.jpg", Ext: "jpg", Status: domain.PhotoPending,
				MetaStatus: domain.MetaReady, PreviewStatus: domain.PreviewProcessing, FirstSeenAt: now, LastSeenAt: now, CreatedAt: now, UpdatedAt: now}
			require.NoError(t, db.Photos().Insert(ctx, photo))
			switch state {
			case "failed":
				require.NoError(t, db.Photos().SetPreviewFailed(ctx, photo.ID, "test", now))
			case "revoked":
				require.NoError(t, db.Sources().Revoke(ctx, photo.SourceID, "test", now))
			default:
				require.NoError(t, db.Photos().SetStatus(ctx, photo.ID, domain.PhotoStatus(state), now))
			}
			before, err := db.Photos().Get(ctx, photo.ID)
			require.NoError(t, err)
			err = write(db.Photos(), ctx, photo.ID, now)
			require.ErrorIs(t, err, domain.ErrNotFound, "a late build must not publish a no-longer-eligible row")
			after, err := db.Photos().Get(ctx, photo.ID)
			require.NoError(t, err)
			assert.Equal(t, before.Status, after.Status)
			assert.Equal(t, before.PreviewStatus, after.PreviewStatus)
		})
	}
}
