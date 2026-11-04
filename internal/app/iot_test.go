package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/app/events"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/iot"
	"github.com/DituLin/Atrium/internal/iot/homeassistant"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
)

type iotAdapterFunc func(context.Context, iot.Sink) error

func (f iotAdapterFunc) Run(ctx context.Context, s iot.Sink) error { return f(ctx, s) }
func serviceFixture(t *testing.T) (*iotService, *bytes.Buffer, *store.DB) {
	t.Helper()
	db := testutil.NewDB(t)
	cfg := config.IoT{Enabled: true, HomeAssistant: config.HomeAssistant{ID: "home", Devices: []config.IoTDevice{{ID: "lamp", Capabilities: []config.IoTCapability{{ID: "power", Kind: iot.Power}}}}}}
	repo, err := db.NewIoT(context.Background(), cfg)
	require.NoError(t, err)
	buf := &bytes.Buffer{}
	return &iotService{repo: repo, cfg: cfg, bus: events.NewBus(), log: slog.New(slog.NewTextHandler(buf, nil))}, buf, db
}
func appIoTUpdate() iot.Update {
	return iot.Update{InstanceID: "home", Generation: 1, Status: iot.Connected, Replace: true, Observations: []iot.Observation{{InstanceID: "home", DeviceID: "lamp", CapabilityID: "power", Generation: 1, Value: true, Availability: iot.Available, ConfirmedAt: time.Now()}}}
}
func TestIoTServicePersistsBeforeNotifyAndSuppressesReceiptChanges(t *testing.T) {
	s, _, _ := serviceFixture(t)
	sub := s.bus.Subscribe("test", 4)
	defer sub.Close()
	s.factory = func() (iot.Adapter, error) {
		return iotAdapterFunc(func(ctx context.Context, sink iot.Sink) error {
			u := appIoTUpdate()
			require.NoError(t, sink.Publish(ctx, u))
			ev := <-sub.C()
			require.Equal(t, "iot", string(ev.Topics[0]))
			snapshot, err := s.repo.Read(ctx)
			require.NoError(t, err)
			require.Equal(t, true, snapshot.Instances[0].Devices[0].Capabilities[0].Observation.Value)
			u.Observations[0].ConfirmedAt = time.Now()
			u.Observations[0].ReceivedAt = time.Now()
			require.NoError(t, sink.Publish(ctx, u))
			require.Equal(t, int64(1), s.bus.Version())
			return nil
		}), nil
	}
	s.Run(context.Background())
	snapshot, err := s.repo.Read(context.Background())
	require.NoError(t, err)
	require.Equal(t, iot.Disconnected, snapshot.Instances[0].Status)
}
func TestIoTServiceInitializationFailureClearsCache(t *testing.T) {
	s, logs, _ := serviceFixture(t)
	_, err := s.repo.Apply(context.Background(), appIoTUpdate())
	require.NoError(t, err)
	s.factory = func() (iot.Adapter, error) { return nil, errors.New("SECRET /private/token/path") }
	s.Run(context.Background())
	snapshot, err := s.repo.Read(context.Background())
	require.NoError(t, err)
	require.Nil(t, snapshot.Instances[0].Devices[0].Capabilities[0].Observation.Value)
	require.Nil(t, snapshot.Instances[0].LastSuccessAt)
	require.NotContains(t, logs.String(), "SECRET")
	require.NotContains(t, logs.String(), "/private")
}
func TestIoTServiceCancellation(t *testing.T) {
	s, _, _ := serviceFixture(t)
	entered := make(chan struct{})
	s.factory = func() (iot.Adapter, error) {
		return iotAdapterFunc(func(ctx context.Context, _ iot.Sink) error { close(entered); <-ctx.Done(); return ctx.Err() }), nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	<-entered
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}
func TestRuntimeIoTTokenFailureLeavesCoreAvailable(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	cfg.IoT = config.IoT{Enabled: true, HomeAssistant: config.HomeAssistant{ID: "home", URL: "https://ha.example.test", TokenFile: "/nonexistent/synthetic-token"}}
	r, err := New(context.Background(), Options{Config: cfg})
	require.NoError(t, err)
	defer r.Shutdown()
	require.NotNil(t, r.IoTReader())
	r.iot.Run(context.Background())
	response := httptest.NewRecorder()
	r.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/health/ready", nil))
	require.Equal(t, 200, response.Code)
}

func TestIoTSinkFailureStopsWorkerWithoutNotification(t *testing.T) {
	s, logs, db := serviceFixture(t)
	s.factory = func() (iot.Adapter, error) {
		return iotAdapterFunc(func(ctx context.Context, sink iot.Sink) error {
			require.NoError(t, sink.Publish(ctx, appIoTUpdate()))
			_, err := db.SQL().Exec(`CREATE TRIGGER fail_iot BEFORE INSERT ON iot_observations BEGIN SELECT RAISE(ABORT,'SECRET'); END`)
			require.NoError(t, err)
			u := appIoTUpdate()
			u.Observations[0].Value = false
			return sink.Publish(ctx, u)
		}), nil
	}
	s.Run(context.Background())
	require.Equal(t, int64(1), s.bus.Version())
	require.NotContains(t, logs.String(), "SECRET")
	snapshot, err := s.repo.Read(context.Background())
	require.NoError(t, err)
	require.Equal(t, iot.Disconnected, snapshot.Instances[0].Status)
	require.Equal(t, true, snapshot.Instances[0].Devices[0].Capabilities[0].Observation.Value)
}
func TestIoTUnauthorizedClearsPreviousSuccess(t *testing.T) {
	s, _, _ := serviceFixture(t)
	sub := s.bus.Subscribe("clear", 4)
	defer sub.Close()
	_, err := s.repo.Apply(context.Background(), appIoTUpdate())
	require.NoError(t, err)
	s.factory = func() (iot.Adapter, error) {
		return iotAdapterFunc(func(context.Context, iot.Sink) error { return homeassistant.ErrUnauthorized }), nil
	}
	s.Run(context.Background())
	require.Contains(t, (<-sub.C()).Topics, domain.Topic("iot.clear"))
	snapshot, err := s.repo.Read(context.Background())
	require.NoError(t, err)
	require.Nil(t, snapshot.Instances[0].LastSuccessAt)
	require.Nil(t, snapshot.Instances[0].Devices[0].Capabilities[0].Observation.Value)
}

func TestIoTUnauthorizedSinkClearFailureCannotExposeCachedValues(t *testing.T) {
	s, _, db := serviceFixture(t)
	sub := s.bus.Subscribe("clear-failure", 4)
	defer sub.Close()
	s.factory = func() (iot.Adapter, error) {
		return iotAdapterFunc(func(ctx context.Context, sink iot.Sink) error {
			require.NoError(t, sink.Publish(ctx, appIoTUpdate()))
			_, err := db.SQL().Exec(`CREATE TRIGGER fail_iot_clear BEFORE DELETE ON iot_observations BEGIN SELECT RAISE(ABORT,'synthetic failure'); END`)
			require.NoError(t, err)
			return sink.Publish(ctx, iot.Update{InstanceID: "home", Generation: 1, Status: iot.Disconnected, Replace: true})
		}), nil
	}
	s.Run(context.Background())
	require.Equal(t, int64(2), s.bus.Version(), "authorization cache hiding must notify even when SQL fails")
	<-sub.C() // Initial synchronization.
	require.Contains(t, (<-sub.C()).Topics, domain.TopicIoTClear)
	snapshot, err := s.repo.Read(context.Background())
	require.NoError(t, err)
	require.Equal(t, iot.Disconnected, snapshot.Instances[0].Status)
	require.Nil(t, snapshot.Instances[0].LastSuccessAt)
	require.Nil(t, snapshot.Instances[0].Devices[0].Capabilities[0].Observation.Value)
}
