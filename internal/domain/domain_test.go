package domain_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/domain"
)

func TestErrorCodeTableIsCompleteAndMapped(t *testing.T) {
	// Every code named in design §8 must exist and map to a status.
	expected := []domain.ErrorCode{
		"unauthorized", "forbidden", "not_found", "invalid_request", "invalid_command",
		"screen_offline", "screen_revoked", "pairing_expired", "pairing_claimed",
		"rate_limited", "preview_processing", "preview_unavailable", "source_offline",
		"identity_mismatch", "conflict", "internal",
	}
	require.ElementsMatch(t, expected, domain.AllErrorCodes())

	for _, c := range domain.AllErrorCodes() {
		require.GreaterOrEqual(t, c.HTTPStatus(), 200, "code %q must map to a status", c)
	}
	require.Equal(t, http.StatusUnauthorized, domain.CodeUnauthorized.HTTPStatus())
	require.Equal(t, http.StatusForbidden, domain.CodeForbidden.HTTPStatus())
	require.Equal(t, http.StatusConflict, domain.CodeScreenOffline.HTTPStatus())
	require.Equal(t, http.StatusGone, domain.CodePairingClaimed.HTTPStatus())
	require.Equal(t, http.StatusTooManyRequests, domain.CodeRateLimited.HTTPStatus())
	require.Equal(t, http.StatusAccepted, domain.CodePreviewProcessing.HTTPStatus())
	require.Equal(t, http.StatusServiceUnavailable, domain.CodePreviewUnavailable.HTTPStatus())
	require.Equal(t, http.StatusInternalServerError, domain.CodeInternal.HTTPStatus())
}

func TestErrorWrappingAndDetails(t *testing.T) {
	cause := errors.New("disk is on fire")
	err := domain.WrapErr(domain.CodeInternal, cause, "cannot read %s", "cache").
		WithDetails(map[string]any{"retry_after_seconds": 5})

	require.ErrorIs(t, err, cause, "the cause stays reachable with errors.Is")
	require.Contains(t, err.Error(), "cannot read cache")

	extracted, ok := domain.AsError(err)
	require.True(t, ok)
	require.Equal(t, domain.CodeInternal, extracted.Code)
	require.Equal(t, 5, extracted.Details["retry_after_seconds"])

	require.Equal(t, http.StatusTeapot, domain.Errorf(domain.CodeInternal, "x").
		WithStatus(http.StatusTeapot).HTTPStatus())

	_, ok = domain.AsError(errors.New("plain"))
	require.False(t, ok)
}

func TestEnumValidation(t *testing.T) {
	require.True(t, domain.HealthOnline.Valid())
	require.False(t, domain.Health("weird").Valid())
	require.True(t, domain.PhotoReady.Valid())
	require.False(t, domain.PhotoStatus("gone").Valid())
	require.True(t, domain.PreviewEvicted.Valid())
	require.True(t, domain.CommandApplied.Valid())
	require.True(t, domain.CommandApplied.Terminal())
	require.False(t, domain.CommandAccepted.Terminal())
	require.True(t, domain.RouteDashboard.NavigableRoute())
	require.False(t, domain.RoutePhoto.NavigableRoute(), "show targets a photo, navigate does not")
	require.True(t, domain.CollectionCapturedToday.Valid())
	require.True(t, domain.WidgetClock.Valid())
	require.False(t, domain.WidgetType("stocks").Valid(), "widget types are a closed whitelist")
	require.True(t, domain.JobBuildPreview.Valid())
	require.True(t, domain.MatchPrefix.Valid())
	require.True(t, domain.TopicPhotos.Valid())
	require.True(t, domain.VariantThumb.Valid())
	require.True(t, domain.ScanCompleted.Valid())
	require.True(t, domain.PairingClaimed.Valid())
	require.True(t, domain.MetaReady.Valid())
	require.True(t, domain.CapturedInferred.Valid())
	require.True(t, domain.SourceRevoked.Valid())
	require.True(t, domain.ScreenActive.Valid())
	require.True(t, domain.JobRunning.Valid())
	require.True(t, domain.ScanManual.Valid())
}

func TestSlugValidation(t *testing.T) {
	require.True(t, domain.ValidSlug("living_room_tv"))
	require.True(t, domain.ValidSlug("family_photos2"))
	require.False(t, domain.ValidSlug(""))
	require.False(t, domain.ValidSlug("Living_Room"), "upper case is rejected")
	require.False(t, domain.ValidSlug("living-room"), "hyphens are rejected")
	require.False(t, domain.ValidSlug("../etc"))
}

func TestIDsAreSortableAndUnique(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	var previous string
	for i := 0; i < 100; i++ {
		id := domain.NewID()
		require.Len(t, id, 26, "a ULID renders as 26 characters")
		_, dup := seen[id]
		require.False(t, dup)
		seen[id] = struct{}{}
		if previous != "" {
			require.GreaterOrEqual(t, id, previous[:10], "ULIDs sort by time")
		}
		previous = id
	}
}

func TestNormalizeExt(t *testing.T) {
	require.Equal(t, "jpg", domain.NormalizeExt(".JPG"))
	require.Equal(t, "heic", domain.NormalizeExt("HEIC"))
	require.Equal(t, "", domain.NormalizeExt(""))
}

func TestRouteStateValidity(t *testing.T) {
	require.True(t, domain.RouteState{Name: domain.RouteDashboard}.Valid())
	require.False(t, domain.RouteState{Name: "settings"}.Valid())
}

func TestIdentityEquality(t *testing.T) {
	a := domain.Identity{FSType: "smbfs", MountFromHash: "abc", Marker: true}
	require.True(t, a.Equal(domain.Identity{FSType: "smbfs", MountFromHash: "abc", Marker: true}))
	require.False(t, a.Equal(domain.Identity{FSType: "apfs", MountFromHash: "abc", Marker: true}))
	require.False(t, a.Equal(domain.Identity{FSType: "smbfs", MountFromHash: "xyz", Marker: true}))
}
