package family

import (
	"context"

	"github.com/DituLin/Atrium/internal/iot"
)

// WithIoT injects a persisted snapshot reader; requests never contact providers.
func (c *HouseComposer) WithIoT(reader iot.SnapshotReader) *HouseComposer { c.iot = reader; return c }

func (c *HouseComposer) composeIoT(ctx context.Context) *HouseIoT {
	if !c.cfg.IoT.Enabled {
		return nil
	}
	reason := NotObserved
	out := &HouseIoT{Availability: Loading, Reason: &reason, Instances: []HouseIoTInstance{}}
	var snap iot.Snapshot
	if c.iot != nil {
		var err error
		snap, err = c.iot.Read(ctx)
		if err != nil {
			reason = ReadFailed
			out.Availability = Failed
			return out
		}
	}
	cfg := c.cfg.IoT.HomeAssistant
	instance := iot.Instance{ID: cfg.ID, Status: iot.Disconnected}
	for _, row := range snap.Instances {
		if row.ID == cfg.ID {
			instance = row
			break
		}
	}
	status := instance.Status
	if status != iot.Connected && status != iot.Connecting {
		status = iot.Disconnected
	}
	public := HouseIoTInstance{ID: cfg.ID, Provider: "home_assistant", Status: status, Generation: instance.Generation, LastSuccessAt: c.formatTime(instance.LastSuccessAt), Devices: []HouseIoTDevice{}}
	if status == iot.Connected {
		out.Availability = Available
		out.Reason = nil
	} else if public.LastSuccessAt != nil {
		out.Availability = Stale
		reason = Expired
	}
	for _, d := range cfg.Devices {
		device := HouseIoTDevice{ID: d.ID, Name: d.Name, RoomID: d.RoomID, RoomName: d.RoomName, Capabilities: []HouseIoTCapability{}}
		for _, cap := range d.Capabilities {
			if !cap.Kind.Valid() {
				continue
			}
			observation := iot.Observation{}
			for _, sd := range instance.Devices {
				if sd.ID == d.ID {
					for _, sc := range sd.Capabilities {
						if sc.ID == cap.ID {
							observation = sc.Observation
							break
						}
					}
					break
				}
			}
			normalized := iot.Normalize(cap.Kind, observation.Value, observation.Unit, map[string]string{"off": "off", "idle": "idle", "active": "active", "paused": "paused", "error": "error"})
			// Missing/unavailable observations carry no value, even if a faulty reader supplied one.
			if observation.Availability != iot.Available {
				normalized = iot.Normalized{Availability: iot.Unknown, Reason: iot.ReasonMissing}
				if observation.Availability == iot.Unavailable {
					normalized.Availability = iot.Unavailable
					normalized.Reason = iot.ReasonUnavailable
				} else {
					switch observation.Reason {
					case iot.ReasonUnknown, iot.ReasonInvalid, iot.ReasonDisconnected:
						normalized.Reason = observation.Reason
					}
				}
			}
			device.Capabilities = append(device.Capabilities, HouseIoTCapability{ID: cap.ID, Name: cap.Name, Kind: cap.Kind, Observation: HouseIoTObservation{Value: normalized.Value, Unit: normalized.Unit, Availability: normalized.Availability, Reason: normalized.Reason, SourceUpdatedAt: c.formatTime(observation.SourceUpdatedAt), SampledAt: c.formatTime(observation.SampledAt), ReceivedAt: c.formatTime(&observation.ReceivedAt), ConfirmedAt: c.formatTime(&observation.ConfirmedAt)}})
		}
		public.Devices = append(public.Devices, device)
	}
	out.Instances = append(out.Instances, public)
	return out
}
