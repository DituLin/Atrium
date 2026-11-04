package media_test

import (
	"context"
	"io"
	"testing"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/media"
	"github.com/DituLin/Atrium/internal/media/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnDemandMetadataPublishesWithoutGeneratingPreviews(t *testing.T) {
	fx := newFixture(t, func(o *media.Options) { o.Media.PreviewMode = "on_demand" })
	ctx := context.Background()
	photo := fx.seed(t, "album/cold.jpg", "jpg", jpegBytes(t, 32, 32, testutil.ExifOptions{}))
	require.NoError(t, fx.pipeline.ExtractMeta(ctx, fx.job(domain.JobExtractMeta, photo.ID)))
	got := fx.reload(t, photo.ID)
	assert.Equal(t, domain.PhotoReady, got.Status)
	assert.Equal(t, domain.MetaReady, got.MetaStatus)
	assert.Equal(t, domain.PreviewPending, got.PreviewStatus)
	open, err := fx.db.Jobs().HasOpen(ctx, domain.JobBuildPreview, photo.ID)
	require.NoError(t, err)
	assert.False(t, open, "indexing a cold library must not enqueue every preview")
	ids, err := fx.db.Photos().ListEligibleIDs(ctx, 100)
	require.NoError(t, err)
	assert.Equal(t, []string{photo.ID}, ids)

	// The same normal worker still produces demand-requested bytes.
	require.NoError(t, fx.pipeline.BuildPreview(ctx, fx.job(domain.JobBuildPreview, photo.ID)))
	assert.Equal(t, domain.PreviewReady, fx.reload(t, photo.ID).PreviewStatus)
	file, err := fx.db.Previews().Get(ctx, photo.ID, domain.VariantPreview)
	require.NoError(t, err)
	assert.True(t, fx.cache.Exists(file.RelPath))
}

type callbackMetadataReader struct{ read func() }

func (r callbackMetadataReader) Read(context.Context, io.ReadSeeker) (media.Metadata, error) {
	r.read()
	return media.Metadata{}, media.ErrNoMetadata
}

func TestOnDemandMetadataDoesNotResurrectTerminalOrHiddenPhotos(t *testing.T) {
	for _, state := range []string{"removed", "excluded", "unsupported", "failed", "revoked"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			var fx *fixture
			var id string
			fx = newFixture(t, func(o *media.Options) {
				o.Media.PreviewMode = "on_demand"
				o.Reader = callbackMetadataReader{read: func() {
					switch state {
					case "failed":
						require.NoError(t, fx.db.Photos().SetPreviewFailed(ctx, id, "test", fx.now))
					case "revoked":
						require.NoError(t, fx.db.Sources().Revoke(ctx, testSourceID, "test", fx.now))
					default:
						require.NoError(t, fx.db.Photos().SetStatus(ctx, id, domain.PhotoStatus(state), fx.now))
					}
				}}
			})
			photo := fx.seed(t, "album/hidden.jpg", "jpg", jpegBytes(t, 32, 32, testutil.ExifOptions{}))
			id = photo.ID
			require.NoError(t, fx.pipeline.ExtractMeta(ctx, fx.job(domain.JobExtractMeta, id)))
			got := fx.reload(t, id)
			if state != "revoked" {
				assert.NotEqual(t, domain.PhotoReady, got.Status)
			}
			open, err := fx.db.Jobs().HasOpen(ctx, domain.JobBuildPreview, id)
			require.NoError(t, err)
			assert.False(t, open)
			ids, err := fx.db.Photos().ListEligibleIDs(ctx, 100)
			require.NoError(t, err)
			assert.Empty(t, ids)
		})
	}
}

func TestDemandPreviewBuildSkipsTerminalAndRevokedPhotos(t *testing.T) {
	for _, state := range []string{"unsupported", "failed", "revoked"} {
		t.Run(state, func(t *testing.T) {
			fx := newFixture(t, nil)
			ctx := context.Background()
			photo := fx.seed(t, "album/terminal.jpg", "jpg", jpegBytes(t, 32, 32, testutil.ExifOptions{}))
			switch state {
			case "failed":
				require.NoError(t, fx.db.Photos().SetPreviewFailed(ctx, photo.ID, "test", fx.now))
			case "unsupported":
				require.NoError(t, fx.db.Photos().SetUnsupported(ctx, photo.ID, "test", fx.now))
			case "revoked":
				require.NoError(t, fx.db.Sources().Revoke(ctx, testSourceID, "test", fx.now))
			}
			require.NoError(t, fx.pipeline.BuildPreview(ctx, fx.job(domain.JobBuildPreview, photo.ID)))
			_, err := fx.db.Previews().Get(ctx, photo.ID, domain.VariantPreview)
			assert.ErrorIs(t, err, domain.ErrNotFound)
		})
	}
}
