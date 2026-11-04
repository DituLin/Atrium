package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/jobs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoriesAndRandomPoolsAreIndependentOfPreviewCache(t *testing.T) {
	h := newPhotoHarness(t)
	token := memoryScreen(t, h, "memory_tv")
	ctx := context.Background()
	var expected []string
	for i, status := range []domain.PreviewStatus{domain.PreviewReady, domain.PreviewPending, domain.PreviewProcessing, domain.PreviewEvicted, domain.PreviewUnavailable} {
		photo := h.seedPhoto(t, seedOptions{RelPath: fmt.Sprintf("cold-%d.jpg", i), Preview: status})
		expected = append(expected, photo.ID)
	}
	h.seedPhoto(t, seedOptions{RelPath: "failed.jpg", Preview: domain.PreviewFailed})
	h.seedPhoto(t, seedOptions{RelPath: "unsupported.jpg", Status: domain.PhotoUnsupported})
	h.seedPhoto(t, seedOptions{RelPath: "removed.jpg", Status: domain.PhotoRemoved})
	h.seedPhoto(t, seedOptions{RelPath: "excluded.jpg", Status: domain.PhotoExcluded})
	h.seedPhoto(t, seedOptions{RelPath: "unindexed.jpg", Status: domain.PhotoPending, Preview: domain.PreviewPending})
	first := getMemories(t, h, token, "?limit=100")
	assert.ElementsMatch(t, expected, memoryIDs(first))
	assert.Equal(t, len(expected), first.Meta.Total)
	random := h.list(t, "?collection=random&limit=100", token)
	var randomIDs []string
	for _, item := range random.Items {
		randomIDs = append(randomIDs, item.ID)
	}
	assert.ElementsMatch(t, expected, randomIDs)
	require.NotNil(t, random.Meta.Total)
	assert.EqualValues(t, len(expected), *random.Meta.Total)
	for _, id := range expected {
		require.NoError(t, h.db.Photos().SetPreviewStatus(ctx, id, domain.PreviewEvicted, "", h.clock.Now()))
	}
	second := getMemories(t, h, token, "?limit=100")
	assert.Equal(t, memoryIDs(first), memoryIDs(second), "cache reclamation cannot drop or reorder the daily pool")
	counts, err := h.db.Jobs().Counts(ctx)
	require.NoError(t, err)
	assert.Zero(t, counts[domain.JobQueued], "listing pages must not prebuild the whole page")

	// Only the actual media request creates one guarded, deduplicated job.
	for i := 0; i < 2; i++ {
		resp := h.request(http.MethodGet, mediaPath(expected[0], "preview"), "", bearer(token))
		assert.Equal(t, http.StatusAccepted, resp.StatusCode)
		_ = resp.Body.Close()
	}
	counts, err = h.db.Jobs().Counts(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, counts[domain.JobQueued])
	require.NoError(t, h.db.Sources().Revoke(ctx, photoSourceID, "test", h.clock.Now()))
	assert.Empty(t, getMemories(t, h, token, "?limit=100").Items)
	assert.Empty(t, h.list(t, "?collection=random&limit=100", token).Items)
}

func TestMediaDoesNotServeTerminalPhotosEvenWithCachedBytes(t *testing.T) {
	h := newPhotoHarness(t)
	token := memoryScreen(t, h, "memory_tv")
	for _, opts := range []seedOptions{
		{RelPath: "failed-cached.jpg", Preview: domain.PreviewFailed, WithFile: true},
		{RelPath: "unsupported-cached.jpg", Status: domain.PhotoUnsupported, WithFile: true},
	} {
		photo := h.seedPhoto(t, opts)
		resp := h.request(http.MethodGet, mediaPath(photo.ID, "preview"), "", bearer(token))
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		_ = resp.Body.Close()
	}
}

func TestOnDemandMediaRequestTakesPriorityOverBulkMetadata(t *testing.T) {
	for _, mode := range []string{"eager", "on_demand"} {
		t.Run(mode, func(t *testing.T) {
			h := newPhotoHarness(t)
			h.cfg.Media.PreviewMode = mode
			token := memoryScreen(t, h, "memory_tv")
			ctx := context.Background()
			bulk := h.seedPhoto(t, seedOptions{RelPath: "metadata.jpg", Status: domain.PhotoPending, Preview: domain.PreviewPending})
			requested := h.seedPhoto(t, seedOptions{RelPath: "requested.jpg", Preview: domain.PreviewEvicted})
			require.NoError(t, h.deps.Queue.EnqueuePhoto(ctx, domain.JobExtractMeta, bulk.ID, bulk.SourceID, jobs.PriorityExtractMeta))
			resp := h.request(http.MethodGet, mediaPath(requested.ID, "preview"), "", bearer(token))
			require.Equal(t, http.StatusAccepted, resp.StatusCode)
			_ = resp.Body.Close()
			job, err := h.db.Jobs().Claim(ctx, "test-worker", h.clock.Now())
			require.NoError(t, err)
			if mode == "on_demand" {
				assert.Equal(t, requested.ID, job.PhotoID)
				assert.Equal(t, domain.JobBuildPreview, job.Kind)
				assert.Equal(t, 20, job.Priority)
			} else {
				assert.Equal(t, bulk.ID, job.PhotoID, "the compatibility mode keeps metadata-first priorities")
			}
		})
	}
}
