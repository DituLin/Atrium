package media_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/jobs"
	"github.com/DituLin/Atrium/internal/media"
	"github.com/DituLin/Atrium/internal/media/testutil"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type callbackDisk struct{ once func() }

func (d *callbackDisk) Free(string) (int64, int64, error) {
	if d.once != nil {
		f := d.once
		d.once = nil
		f()
	}
	return 1 << 40, 1 << 41, nil
}

func TestPreviewTerminalTransitionBeforeProcessingStopsBuild(t *testing.T) {
	ctx := context.Background()
	disk := &callbackDisk{}
	fx := newFixture(t, func(o *media.Options) { o.Media.PreviewMode = "on_demand"; o.Disk = disk })
	photo := fx.seed(t, "synthetic/race.jpg", "jpg", jpegBytes(t, 32, 32, testutil.ExifOptions{}))
	require.NoError(t, fx.pipeline.ExtractMeta(ctx, fx.job(domain.JobExtractMeta, photo.ID)))
	disk.once = func() { require.NoError(t, fx.db.Photos().SetPreviewFailed(ctx, photo.ID, "terminal", fx.now)) }
	require.NoError(t, fx.pipeline.BuildPreview(ctx, fx.job(domain.JobBuildPreview, photo.ID)))
	assert.Equal(t, domain.PreviewFailed, fx.reload(t, photo.ID).PreviewStatus)
	_, err := fx.db.Previews().Get(ctx, photo.ID, domain.VariantPreview)
	assert.ErrorIs(t, err, domain.ErrNotFound, "a denied processing transition must stop decoding and writing")
}

type callbackFailureConverter struct {
	callback func()
	failure  error
}

func (c *callbackFailureConverter) Enabled() bool { return true }
func (c *callbackFailureConverter) CreationTime(context.Context, string) (time.Time, error) {
	return time.Time{}, errors.New("synthetic")
}
func (c *callbackFailureConverter) Convert(context.Context, string, string, int) (string, error) {
	c.callback()
	return "", c.failure
}

func TestPreviewTerminalTransitionDuringDecodeSurvivesFailureHandling(t *testing.T) {
	for name, cause := range map[string]error{
		"retry":       media.Coded(media.ErrCodeIO, errors.New("synthetic transient")),
		"deferral":    jobs.Defer(time.Minute, errors.New("synthetic source offline")),
		"unsupported": media.Coded(media.ErrCodeUnsupported, errors.New("synthetic unsupported")),
		"failure":     jobs.Permanent(media.Coded(media.ErrCodeTooLarge, errors.New("synthetic limit"))),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			converter := &callbackFailureConverter{failure: cause}
			fx := newFixture(t, func(o *media.Options) { o.Media.PreviewMode = "on_demand"; o.Converter = converter })
			photo := fx.seed(t, "synthetic/race.heic", "heic", jpegBytes(t, 32, 32, testutil.ExifOptions{}))
			require.NoError(t, fx.db.Photos().SetMeta(ctx, photo.ID, store.MetaResult{CapturedConfidence: domain.CapturedUnknown, PublishWithoutPreview: true}, fx.now))
			converter.callback = func() { require.NoError(t, fx.db.Photos().SetPreviewFailed(ctx, photo.ID, "terminal", fx.now)) }
			require.Error(t, fx.pipeline.BuildPreview(ctx, fx.job(domain.JobBuildPreview, photo.ID)))
			got := fx.reload(t, photo.ID)
			assert.Equal(t, domain.PreviewFailed, got.PreviewStatus)
			assert.Equal(t, "terminal", got.PreviewError)
			assert.Equal(t, 1, got.PreviewAttempts, "a late failure handler must not consume another attempt")
			assert.Equal(t, domain.PhotoReady, got.Status, "a late format verdict must not replace the terminal state")
		})
	}
}
