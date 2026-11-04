package family_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/iot"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type iotReader struct {
	snapshot iot.Snapshot
	err      error
}

func (r *iotReader) Read(context.Context) (iot.Snapshot, error) { return r.snapshot, r.err }
func TestHouseIoTIndependentSafeProjection(t *testing.T) {
	c, nas, clock, cfg := houseFixture(t)
	r := &iotReader{}
	c.WithIoT(r)
	require.Nil(t, c.Compose(t.Context()).IoT)
	cfg.IoT = config.IoT{Enabled: true, HomeAssistant: config.HomeAssistant{ID: "ha", Devices: []config.IoTDevice{{ID: "fan", Name: "Fan", RoomID: "living", RoomName: "Living room", Capabilities: []config.IoTCapability{{ID: "power", Kind: iot.Power}}}}}}
	require.EqualValues(t, "loading", c.Compose(t.Context()).IoT.Availability)
	now := clock.Now()
	r.snapshot.Instances = []iot.Instance{{ID: "ha", Provider: "private URL", Status: iot.Connected, Generation: 2, LastSuccessAt: &now, Devices: []iot.Device{{ID: "fan", Name: "old private name", Capabilities: []iot.Capability{{ID: "power", Kind: iot.Power, Observation: iot.Observation{Value: false, Availability: iot.Available, ConfirmedAt: now}}}}, {ID: "unauthorized"}}}}
	nas.err = errors.New("private NAS error")
	h := c.Compose(t.Context())
	require.EqualValues(t, "available", h.IoT.Availability)
	require.Len(t, h.IoT.Instances, 1)
	d := h.IoT.Instances[0].Devices[0]
	require.Equal(t, "Fan", d.Name)
	require.Len(t, h.IoT.Instances[0].Devices, 1)
	require.Equal(t, false, d.Capabilities[0].Observation.Value)
	require.Nil(t, d.Capabilities[0].Observation.ReceivedAt)
	raw, err := json.Marshal(h)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private")
	require.NotContains(t, string(raw), "0001-")
	r.snapshot.Instances[0].Status = iot.Disconnected
	require.EqualValues(t, "stale", c.Compose(t.Context()).IoT.Availability)
	r.snapshot.Instances[0].LastSuccessAt = nil
	r.snapshot.Instances[0].Devices = nil
	h = c.Compose(t.Context())
	require.EqualValues(t, "loading", h.IoT.Availability)
	require.Nil(t, h.IoT.Instances[0].Devices[0].Capabilities[0].Observation.Value)
	r.err = errors.New("private reader error")
	h = c.Compose(t.Context())
	require.EqualValues(t, "failed", h.IoT.Availability)
	require.EqualValues(t, "read_failed", *h.IoT.Reason)
	require.Empty(t, h.IoT.Instances)
	r.err = nil
	cfg.IoT.HomeAssistant.Devices = nil
	require.Empty(t, c.Compose(t.Context()).IoT.Instances[0].Devices)
}
func TestHouseIoTRejectsUnsafeObservation(t *testing.T) {
	for _, v := range []any{"secret raw state", map[string]any{"token": "secret"}, 42} {
		c, _, _, cfg := houseFixture(t)
		cfg.IoT = config.IoT{Enabled: true, HomeAssistant: config.HomeAssistant{ID: "ha", Devices: []config.IoTDevice{{ID: "fan", Capabilities: []config.IoTCapability{{ID: "power", Kind: iot.Power}}}}}}
		c.WithIoT(&iotReader{snapshot: iot.Snapshot{Instances: []iot.Instance{{ID: "ha", Status: iot.Connected, Devices: []iot.Device{{ID: "fan", Capabilities: []iot.Capability{{ID: "power", Observation: iot.Observation{Value: v, Availability: iot.Available, Reason: "private"}}}}}}}}})
		o := c.Compose(t.Context()).IoT.Instances[0].Devices[0].Capabilities[0].Observation
		require.Nil(t, o.Value)
		require.EqualValues(t, "invalid_value", o.Reason)
	}
}

func TestHouseIoTPartialUnavailableAndIndependentTimes(t *testing.T) {
	c, _, clock, cfg := houseFixture(t)
	cfg.IoT = config.IoT{Enabled: true, HomeAssistant: config.HomeAssistant{ID: "ha", Devices: []config.IoTDevice{{ID: "fan", Capabilities: []config.IoTCapability{{ID: "power", Kind: iot.Power}, {ID: "humidity", Kind: iot.Humidity}}}}}}
	now := clock.Now()
	sampled := now.Add(-time.Hour)
	source := now.Add(-time.Minute)
	received := now.Add(-time.Second)
	c.WithIoT(&iotReader{snapshot: iot.Snapshot{Instances: []iot.Instance{{ID: "ha", Status: iot.Connected, LastSuccessAt: &now, Devices: []iot.Device{{ID: "fan", Capabilities: []iot.Capability{
		{ID: "power", Observation: iot.Observation{Value: false, Availability: iot.Available, SourceUpdatedAt: &source, SampledAt: &sampled, ReceivedAt: received, ConfirmedAt: now}},
		{ID: "humidity", Observation: iot.Observation{Value: 50.0, Unit: "%", Availability: iot.Unavailable}},
	}}}}}}})
	h := c.Compose(t.Context())
	require.EqualValues(t, "available", h.IoT.Availability)
	caps := h.IoT.Instances[0].Devices[0].Capabilities
	require.Equal(t, false, caps[0].Observation.Value)
	require.Nil(t, caps[1].Observation.Value)
	require.EqualValues(t, "unavailable", caps[1].Observation.Availability)
	for got, want := range map[string]time.Time{*caps[0].Observation.SourceUpdatedAt: source, *caps[0].Observation.SampledAt: sampled, *caps[0].Observation.ReceivedAt: received, *caps[0].Observation.ConfirmedAt: now} {
		parsed, err := time.Parse(time.RFC3339Nano, got)
		require.NoError(t, err)
		require.True(t, parsed.Equal(want))
		require.Contains(t, got, "+08:00")
	}
}
