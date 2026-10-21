package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestArchiveOverviewHTTPRejectsQueriesAndNonScreenCredentials(t *testing.T) {
	h := newArchiveHTTP(t, true, "archive.read")
	token, err := auth.NewScreenToken()
	require.NoError(t, err)
	require.NoError(t, h.db.Screens().Create(context.Background(), &domain.Screen{ID: "overview-tv", Name: "Synthetic TV", TokenHash: auth.HashToken(token)}))
	for _, tc := range []struct {
		query, token string
		status       int
	}{
		{"", token, http.StatusOK},
		{"?entity_id=private", token, http.StatusBadRequest},
		{"?limit=1", token, http.StatusBadRequest},
		{"?query=a&query=b", token, http.StatusBadRequest},
		{"?%zz", token, http.StatusBadRequest},
		{"", h.token, http.StatusForbidden},
		{"", "", http.StatusUnauthorized},
	} {
		req, err := http.NewRequest(http.MethodGet, h.server.URL+"/api/v1/archives/overview"+tc.query, nil)
		require.NoError(t, err)
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		resp, err := h.server.Client().Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, tc.status, resp.StatusCode, tc.query)
		if tc.status == http.StatusOK {
			require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
		}
	}
	require.NoError(t, h.db.Screens().Revoke(context.Background(), "overview-tv", time.Now()))
	req, err := http.NewRequest(http.MethodGet, h.server.URL+"/api/v1/archives/overview", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := h.server.Client().Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusGone, resp.StatusCode)
}

func TestArchiveOverviewGrowthProfilesOnlyExposeMatchingPublicEntities(t *testing.T) {
	ctx := context.Background()
	h := newArchiveHTTP(t, true, "archive.read")
	token, err := auth.NewScreenToken()
	require.NoError(t, err)
	require.NoError(t, h.db.Screens().Create(ctx, &domain.Screen{ID: "profile-tv", Name: "Synthetic TV", TokenHash: auth.HashToken(token)}))
	entities := map[bool]string{}
	for _, visible := range []bool{true, false} {
		member := "synthetic-public"
		if !visible {
			member = "synthetic-private"
		}
		require.NoError(t, h.db.FamilyRecords().PutMember(ctx, domain.FamilyMember{ID: member, Name: member}, time.Now()))
		record, _, err := h.db.FamilyRecords().Record(ctx, "synthetic", member, domain.FamilyRecordInput{MemberID: member, Category: "growth", Title: "Synthetic growth", OccurredAt: time.Now(), DisplayOnTV: visible}, time.Now())
		require.NoError(t, err)
		require.NoError(t, h.db.ProjectLegacyArchive(ctx, record.ID))
		var entity string
		require.NoError(t, h.db.SQL().QueryRow(`SELECT entity_id FROM archive_legacy_entities WHERE legacy_member_id=?`, member).Scan(&entity))
		entities[visible] = entity
	}
	h.cfg.Archives.GrowthProfiles = []config.GrowthProfile{
		{EntityID: entities[true], BirthDate: "2000-01-01", Sex: "female", HeightMeasurement: "length"},
		{EntityID: entities[false], BirthDate: "2000-02-01", Sex: "male"},
		{EntityID: "unmatched-synthetic", BirthDate: "2000-03-01", Sex: "male"},
	}
	status, body, wire := h.request(t, http.MethodGet, "/api/v1/archives/overview", "", nil, token)
	require.Equal(t, http.StatusOK, status)
	data := body["data"].(map[string]any)
	require.Equal(t, []any{map[string]any{"entity_id": entities[true], "birth_date": "2000-01-01", "sex": "female", "height_measurement": "length"}}, data["growth_profiles"])
	require.NotContains(t, wire, entities[false])
	require.NotContains(t, wire, "2000-02-01")
	require.NotContains(t, wire, "unmatched-synthetic")
	require.NotContains(t, wire, "2000-03-01")
	// Integration credentials still cannot obtain either document counts or profiles.
	status, _, wire = h.request(t, http.MethodGet, "/api/v1/archives/overview", "", nil, h.token)
	require.Equal(t, http.StatusForbidden, status)
	require.NotContains(t, wire, "2000-01-01")
	status, _, wire = h.request(t, http.MethodGet, "/api/v1/archives/overview", "", nil, "")
	require.Equal(t, http.StatusUnauthorized, status)
	require.NotContains(t, wire, "2000-01-01")
	// A partial snapshot cannot authorize a complete profile projection even
	// when its inspected subset still contains the matched public entity.
	_, _, err = h.db.FamilyRecords().Record(ctx, "synthetic", "not-projected", domain.FamilyRecordInput{MemberID: "synthetic-public", Category: "growth", Title: "Synthetic pending", OccurredAt: time.Now(), DisplayOnTV: true}, time.Now())
	require.NoError(t, err)
	status, body, wire = h.request(t, http.MethodGet, "/api/v1/archives/overview", "", nil, token)
	require.Equal(t, http.StatusOK, status)
	data = body["data"].(map[string]any)
	require.Equal(t, false, data["coverage"].(map[string]any)["complete"])
	require.NotEmpty(t, data["entities"], "fixture must retain the previously public entity")
	require.NotContains(t, data, "growth_profiles")
	require.NotContains(t, wire, "2000-01-01")
	// Revocation suppresses profiles as well as all other archive payload.
	require.NoError(t, h.db.Screens().Revoke(ctx, "profile-tv", time.Now()))
	status, _, wire = h.request(t, http.MethodGet, "/api/v1/archives/overview", "", nil, token)
	require.Equal(t, http.StatusGone, status)
	require.NotContains(t, wire, "2000-01-01")
}

func TestArchiveOverviewWithoutProfilesKeepsExistingResponseShape(t *testing.T) {
	h := newArchiveHTTP(t, true, "archive.read")
	token, err := auth.NewScreenToken()
	require.NoError(t, err)
	require.NoError(t, h.db.Screens().Create(context.Background(), &domain.Screen{ID: "empty-profile-tv", Name: "Synthetic TV", TokenHash: auth.HashToken(token)}))
	for _, profiles := range [][]config.GrowthProfile{nil, {{EntityID: "unmatched-synthetic", BirthDate: "2000-01-01", Sex: "female"}}} {
		h.cfg.Archives.GrowthProfiles = profiles
		status, body, _ := h.request(t, http.MethodGet, "/api/v1/archives/overview", "", nil, token)
		require.Equal(t, http.StatusOK, status)
		wire, err := json.Marshal(body["data"])
		require.NoError(t, err)
		require.NotContains(t, string(wire), "growth_profiles")
	}
}
