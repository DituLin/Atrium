package family_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/clock"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/family"
	"github.com/stretchr/testify/require"
)

type sourceReader struct {
	rows []domain.Source
	err  error
}

func (s *sourceReader) List(context.Context) ([]domain.Source, error) { return s.rows, s.err }

func houseFixture(t *testing.T) (*family.HouseComposer, *sourceReader, *clock.Fake, *config.Config) {
	t.Helper()
	cfg := config.Defaults()
	cfg.Home.Timezone = "Asia/Singapore"
	cfg.Sources = []config.Source{{ID: "family", Name: "Family photos"}, {ID: "second", Name: "Second"}}
	fake := clock.NewFake(time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC))
	home, err := clock.NewHome(cfg.Home.Timezone, fake)
	require.NoError(t, err)
	reader := &sourceReader{}
	return family.NewHouseComposer(cfg, reader, home), reader, fake, cfg
}

func TestHouseFreshnessUsesEachCheckIncludingKnownUnhealthyObservations(t *testing.T) {
	for _, health := range []domain.Health{domain.HealthOnline, domain.HealthOffline, domain.HealthDegraded, domain.HealthUnknown, ""} {
		t.Run(string(health), func(t *testing.T) {
			composer, reader, fake, _ := houseFixture(t)
			checked := fake.Now().Add(-59 * time.Second)
			older := fake.Now().Add(-2 * time.Minute)
			success := fake.Now().Add(-time.Hour)
			reader.rows = []domain.Source{{ID: "family", Name: "Family photos", Status: domain.SourceActive, Health: health, LastCheckAt: &checked, LastSuccessAt: &success}, {ID: "second", Name: "Second", Status: domain.SourceActive, Health: domain.HealthOffline, LastCheckAt: &older}}
			house := composer.Compose(t.Context())
			require.Len(t, house.NAS, 2)
			require.EqualValues(t, "available", house.NAS[0].Availability)
			require.Nil(t, house.NAS[0].Reason)
			require.Equal(t, checked.In(time.FixedZone("SGT", 8*3600)).Format(time.RFC3339), *house.NAS[0].ObservedAt)
			require.Equal(t, checked.Add(time.Minute).In(time.FixedZone("SGT", 8*3600)).Format(time.RFC3339), *house.NAS[0].ExpiresAt)
			expected := health
			if expected == "" {
				expected = domain.HealthUnknown
			}
			require.Equal(t, expected, house.NAS[0].Items[0].Health)
			require.Equal(t, success.In(time.FixedZone("SGT", 8*3600)).Format(time.RFC3339), *house.NAS[0].Items[0].LastSuccessAt)
			require.Nil(t, house.NAS[0].Items[0].UpdatedAt)
			require.Nil(t, house.NAS[0].Items[0].ValidFrom)
			require.Nil(t, house.NAS[0].Items[0].ValidUntil)
			require.EqualValues(t, "stale", house.NAS[1].Availability)
			require.EqualValues(t, "expired", *house.NAS[1].Reason)
			fake.Advance(time.Second)
			expired := composer.Compose(t.Context())
			require.EqualValues(t, "stale", expired.NAS[0].Availability, "expiry is inclusive")
			require.Equal(t, house.NAS[0].ObservedAt, expired.NAS[0].ObservedAt, "serialization never renews observation")
			require.Equal(t, house.NAS[0].Items, expired.NAS[0].Items)
			require.NotEqual(t, house.Core.ObservedAt, expired.Core.ObservedAt)
		})
	}
}

func TestHouseUnobservedAndAuthorizationSet(t *testing.T) {
	composer, reader, fake, cfg := houseFixture(t)
	empty := composer.Compose(t.Context())
	require.NotNil(t, empty.NAS)
	require.Empty(t, empty.NAS)
	now := fake.Now()
	reader.rows = []domain.Source{{ID: "family", Name: "Family photos", Status: domain.SourceActive, Health: domain.HealthOnline}, {ID: "second", Status: domain.SourceRevoked, LastCheckAt: &now}, {ID: "removed", Status: domain.SourceActive, LastCheckAt: &now}}
	house := composer.Compose(t.Context())
	require.Len(t, house.NAS, 1)
	require.EqualValues(t, "loading", house.NAS[0].Availability)
	require.EqualValues(t, "not_observed", *house.NAS[0].Reason)
	require.Nil(t, house.NAS[0].ObservedAt)
	require.Nil(t, house.NAS[0].ExpiresAt)
	require.NotNil(t, house.NAS[0].Items)
	require.Empty(t, house.NAS[0].Items)
	reader.rows[0].LastCheckAt = &now
	require.Len(t, composer.Compose(t.Context()).NAS[0].Items, 1)
	reader.rows[0].Status = domain.SourceRevoked
	require.Empty(t, composer.Compose(t.Context()).NAS)
	reader.rows[0].Status = domain.SourceActive
	cfg.Sources = nil
	require.Empty(t, composer.Compose(t.Context()).NAS, "configuration removal cannot retain an old authorized row")
}

func TestHouseSourceReadFailureClearsPriorItemsAndPartialRows(t *testing.T) {
	composer, reader, fake, _ := houseFixture(t)
	now := fake.Now()
	reader.rows = []domain.Source{{ID: "family", Name: "Family photos", Status: domain.SourceActive, LastCheckAt: &now, Health: domain.HealthOnline}}
	require.Len(t, composer.Compose(t.Context()).NAS[0].Items, 1)
	reader.err = errors.New("private database failure")
	failed := composer.Compose(t.Context())
	require.Len(t, failed.NAS, 2, "configured labels survive unavailable authorization read")
	require.Equal(t, "Family photos", failed.NAS[0].SourceLabel)
	for _, snapshot := range failed.NAS {
		require.EqualValues(t, "failed", snapshot.Availability)
		require.EqualValues(t, "read_failed", *snapshot.Reason)
		require.Nil(t, snapshot.ObservedAt)
		require.Nil(t, snapshot.ExpiresAt)
		require.NotNil(t, snapshot.Items)
		require.Empty(t, snapshot.Items)
	}
	reader.err = nil
	reader.rows = nil
	require.Empty(t, composer.Compose(t.Context()).NAS, "a successful empty read replaces failure")
}
