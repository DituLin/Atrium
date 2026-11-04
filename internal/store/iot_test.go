package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/iot"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
)

func iotConfig() config.IoT {
	return config.IoT{Enabled: true, HomeAssistant: config.HomeAssistant{ID: "home", Devices: []config.IoTDevice{{ID: "lamp", Name: "Lamp", RoomID: "room", RoomName: "Room", Capabilities: []config.IoTCapability{{ID: "power", Kind: iot.Power}, {ID: "battery", Kind: iot.Battery}}}}}}
}
func iotUpdate(g uint64) iot.Update {
	return iot.Update{InstanceID: "home", Generation: g, Status: iot.Connected, Replace: true, Observations: []iot.Observation{{InstanceID: "home", DeviceID: "lamp", CapabilityID: "power", Generation: g, Value: true, Availability: iot.Available, ConfirmedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}}}
}
func TestIoTReplacementGenerationAndRestart(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDB(t)
	cfg := iotConfig()
	repo, err := db.NewIoT(ctx, cfg)
	require.NoError(t, err)
	changed, err := repo.Apply(ctx, iotUpdate(8))
	require.NoError(t, err)
	require.True(t, changed)
	snapshot, err := repo.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, true, snapshot.Instances[0].Devices[0].Capabilities[0].Observation.Value)
	require.NotNil(t, snapshot.Instances[0].LastSuccessAt)
	u := iotUpdate(8)
	u.Observations[0].ConfirmedAt = u.Observations[0].ConfirmedAt.Add(time.Minute)
	u.Observations[0].ReceivedAt = time.Now()
	changed, err = repo.Apply(ctx, u)
	require.NoError(t, err)
	require.False(t, changed, "receipt-only updates must not notify")
	u = iotUpdate(7)
	u.Observations[0].Value = false
	changed, err = repo.Apply(ctx, u)
	require.NoError(t, err)
	require.False(t, changed)
	restarted, err := db.NewIoT(ctx, cfg)
	require.NoError(t, err)
	snapshot, err = restarted.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, iot.Disconnected, snapshot.Instances[0].Status)
	require.Equal(t, true, snapshot.Instances[0].Devices[0].Capabilities[0].Observation.Value)
	_, err = repo.Apply(ctx, iotUpdate(99))
	require.Error(t, err, "previous session must be retired")
	changed, err = restarted.Apply(ctx, iotUpdate(1))
	require.NoError(t, err)
	require.True(t, changed)
	_, err = restarted.Apply(ctx, iot.Update{InstanceID: "home", Generation: 1, Status: iot.Disconnected})
	require.NoError(t, err)
	snapshot, err = restarted.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, true, snapshot.Instances[0].Devices[0].Capabilities[0].Observation.Value)
	_, err = restarted.Apply(ctx, iot.Update{InstanceID: "home", Generation: 2, Status: iot.Connected, Replace: true})
	require.NoError(t, err)
	snapshot, err = restarted.Read(ctx)
	require.NoError(t, err)
	require.Nil(t, snapshot.Instances[0].Devices[0].Capabilities[0].Observation.Value)
}
func TestIoTWhitelistWithdrawalAndDisabled(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDB(t)
	cfg := iotConfig()
	repo, err := db.NewIoT(ctx, cfg)
	require.NoError(t, err)
	u := iotUpdate(1)
	extra := u.Observations[0]
	extra.DeviceID = "private"
	u.Observations = append(u.Observations, extra)
	_, err = repo.Apply(ctx, u)
	require.NoError(t, err)
	var n int
	require.NoError(t, db.SQL().QueryRow("SELECT count(*) FROM iot_observations").Scan(&n))
	require.Equal(t, 1, n)
	cfg.HomeAssistant.Devices[0].Capabilities = cfg.HomeAssistant.Devices[0].Capabilities[1:]
	repo, err = db.NewIoT(ctx, cfg)
	require.NoError(t, err)
	require.NoError(t, db.SQL().QueryRow("SELECT count(*) FROM iot_observations").Scan(&n))
	require.Zero(t, n)
	// Even rows inserted outside this repository cannot bypass the read whitelist.
	_, err = db.SQL().Exec(`INSERT INTO iot_observations VALUES ('home','private','power','{}','','')`)
	require.NoError(t, err)
	s, err := repo.Read(ctx)
	require.NoError(t, err)
	require.Len(t, s.Instances[0].Devices, 1)
	require.Len(t, s.Instances[0].Devices[0].Capabilities, 1)
	cfg.Enabled = false
	repo, err = db.NewIoT(ctx, cfg)
	require.NoError(t, err)
	s, err = repo.Read(ctx)
	require.NoError(t, err)
	require.Empty(t, s.Instances)
	require.NoError(t, db.SQL().QueryRow("SELECT count(*) FROM iot_observations").Scan(&n))
	require.Zero(t, n)
}
func TestIoTAtomicFailureAndAuthorizationClear(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDB(t)
	repo, err := db.NewIoT(ctx, iotConfig())
	require.NoError(t, err)
	_, err = repo.Apply(ctx, iotUpdate(1))
	require.NoError(t, err)
	_, err = db.SQL().Exec(`CREATE TRIGGER fail_iot BEFORE INSERT ON iot_observations BEGIN SELECT RAISE(ABORT,'synthetic failure'); END`)
	require.NoError(t, err)
	u := iotUpdate(2)
	u.Observations[0].Value = false
	changed, err := repo.Apply(ctx, u)
	require.Error(t, err)
	require.False(t, changed)
	s, err := repo.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, true, s.Instances[0].Devices[0].Capabilities[0].Observation.Value)
	require.Equal(t, iot.Disconnected, s.Instances[0].Status)
	_, err = db.SQL().Exec(`DROP TRIGGER fail_iot`)
	require.NoError(t, err)
	_, err = repo.Apply(ctx, iot.Update{InstanceID: "home", Generation: 3, Status: iot.Disconnected, Replace: true})
	require.NoError(t, err)
	s, err = repo.Read(ctx)
	require.NoError(t, err)
	require.Nil(t, s.Instances[0].LastSuccessAt)
	require.Nil(t, s.Instances[0].Devices[0].Capabilities[0].Observation.Value)
}

func TestIoTConcurrentReadersNeverSeePartialReplacement(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDB(t)
	cfg := iotConfig()
	cfg.HomeAssistant.Devices[0].Capabilities[1].Kind = iot.Power
	repo, err := db.NewIoT(ctx, cfg)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 40; i++ {
			u := iotUpdate(uint64(i + 1))
			u.Observations[0].Value = i%2 == 0
			o := u.Observations[0]
			o.CapabilityID = "battery"
			u.Observations = append(u.Observations, o)
			if _, err := repo.Apply(ctx, u); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 40; i++ {
		s, err := repo.Read(ctx)
		require.NoError(t, err)
		caps := s.Instances[0].Devices[0].Capabilities
		require.Equal(t, caps[0].Observation.Value, caps[1].Observation.Value)
	}
	require.NoError(t, <-done)
}
func TestIoTPersistenceRejectsRawProviderContent(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDB(t)
	repo, err := db.NewIoT(ctx, iotConfig())
	require.NoError(t, err)
	u := iotUpdate(1)
	u.Observations[0].Value = "SECRET RAW STATE"
	u.Observations[0].Reason = "SECRET ERROR"
	u.Observations[0].Unit = "SECRET UNIT"
	_, err = repo.Apply(ctx, u)
	require.NoError(t, err)
	var raw string
	require.NoError(t, db.SQL().QueryRow("SELECT observation_json FROM iot_observations").Scan(&raw))
	require.NotContains(t, raw, "SECRET")
	s, err := repo.Read(ctx)
	require.NoError(t, err)
	require.Nil(t, s.Instances[0].Devices[0].Capabilities[0].Observation.SampledAt)
}

func TestIoTRequiresFullSyncBeforeConnected(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDB(t)
	repo, err := db.NewIoT(ctx, iotConfig())
	require.NoError(t, err)
	u := iotUpdate(1)
	u.Replace = false
	_, err = repo.Apply(ctx, u)
	require.Error(t, err)
	s, err := repo.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, iot.Disconnected, s.Instances[0].Status)
	require.Nil(t, s.Instances[0].LastSuccessAt)
	u.Replace = true
	_, err = repo.Apply(ctx, u)
	require.NoError(t, err)
	u.Generation = 2
	u.Observations[0].Generation = 2
	u.Replace = false
	_, err = repo.Apply(ctx, u)
	require.Error(t, err)
}

func TestIoTRebindingRevokesCacheButRenamingPreservesIt(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDB(t)
	cfg := iotConfig()
	cfg.HomeAssistant.Devices[0].Capabilities[0].EntityID = "switch.original"
	repo, err := db.NewIoT(ctx, cfg)
	require.NoError(t, err)
	_, err = repo.Apply(ctx, iotUpdate(1))
	require.NoError(t, err)
	cfg.HomeAssistant.Devices[0].Name = "Renamed lamp"
	cfg.HomeAssistant.Devices[0].RoomName = "Renamed room"
	repo, err = db.NewIoT(ctx, cfg)
	require.NoError(t, err)
	s, err := repo.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, true, s.Instances[0].Devices[0].Capabilities[0].Observation.Value)
	cfg.HomeAssistant.Devices[0].Capabilities[0].EntityID = "switch.replacement"
	repo, err = db.NewIoT(ctx, cfg)
	require.NoError(t, err)
	s, err = repo.Read(ctx)
	require.NoError(t, err)
	require.Nil(t, s.Instances[0].Devices[0].Capabilities[0].Observation.Value)
	var n int
	require.NoError(t, db.SQL().QueryRow("SELECT count(*) FROM iot_observations").Scan(&n))
	require.Zero(t, n)
}

func TestIoTBindingFingerprintCoversInterpretationAndFiltersEveryRead(t *testing.T) {
	changes := map[string]func(*config.IoT){
		"origin":    func(c *config.IoT) { c.HomeAssistant.URL = "https://replacement.example.test" },
		"entity":    func(c *config.IoT) { c.HomeAssistant.Devices[0].Capabilities[0].EntityID = "sensor.replacement" },
		"attribute": func(c *config.IoT) { c.HomeAssistant.Devices[0].Capabilities[0].Attribute = "mode" },
		"kind":      func(c *config.IoT) { c.HomeAssistant.Devices[0].Capabilities[0].Kind = iot.Power },
		"unit":      func(c *config.IoT) { c.HomeAssistant.Devices[0].Capabilities[0].Unit = "%" },
		"values":    func(c *config.IoT) { c.HomeAssistant.Devices[0].Capabilities[0].Values["running"] = "idle" },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db := testutil.NewDB(t)
			cfg := iotConfig()
			cfg.HomeAssistant.URL = "https://original.example.test"
			cap := &cfg.HomeAssistant.Devices[0].Capabilities[0]
			cap.Kind = iot.OperatingState
			cap.EntityID = "sensor.original"
			cap.Values = map[string]string{"idle": "idle", "running": "active"}
			repo, err := db.NewIoT(ctx, cfg)
			require.NoError(t, err)
			u := iotUpdate(1)
			u.Observations[0].Value = "active"
			_, err = repo.Apply(ctx, u)
			require.NoError(t, err)
			var raw, hash string
			require.NoError(t, db.SQL().QueryRow("SELECT observation_json,binding_hash FROM iot_observations").Scan(&raw, &hash))
			require.Len(t, hash, 64)
			require.NotContains(t, hash, "sensor.original")
			change(&cfg)
			repo, err = db.NewIoT(ctx, cfg)
			require.NoError(t, err)
			// A stale row reappearing after reconciliation still cannot bypass Read.
			_, err = db.SQL().Exec("INSERT INTO iot_observations VALUES ('home','lamp','power',?,'',?)", raw, hash)
			require.NoError(t, err)
			snapshot, err := repo.Read(ctx)
			require.NoError(t, err)
			require.Nil(t, snapshot.Instances[0].Devices[0].Capabilities[0].Observation.Value)
		})
	}
}
