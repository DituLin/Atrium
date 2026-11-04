package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/iot"
)

// IoTRepository owns one process-local adapter session. Creating a new repository
// retires its predecessor, so generations can safely restart at one.
// Config is frozen per session; changes require NewIoT and startup reconciliation.
type IoTRepository struct {
	db           *DB
	cfg          config.IoT
	allowed      map[string]config.IoTCapability
	fingerprints map[string]string
	generation   uint64
	status       iot.ConnectionStatus
	cacheHidden  bool
}

func iotObservationKey(device, capability string) string { return device + "\x00" + capability }

// NewIoT starts a configuration session and removes revoked persisted bindings.
func (d *DB) NewIoT(ctx context.Context, cfg config.IoT) (*IoTRepository, error) {
	d.iotMu.Lock()
	defer d.iotMu.Unlock()
	// Own the configuration slices/maps independently of the caller.
	b, _ := json.Marshal(cfg)
	var owned config.IoT
	_ = json.Unmarshal(b, &owned)
	r := &IoTRepository{db: d, cfg: owned, allowed: map[string]config.IoTCapability{}, fingerprints: map[string]string{}, status: iot.Disconnected}
	if cfg.Enabled {
		for _, dev := range owned.HomeAssistant.Devices {
			for _, cap := range dev.Capabilities {
				r.allowed[iotObservationKey(dev.ID, cap.ID)] = cap
				r.fingerprints[iotObservationKey(dev.ID, cap.ID)] = iotBindingFingerprint(owned.HomeAssistant.URL, cap)
			}
		}
	}
	d.iotSession = r // Retire old writers even if reconciliation fails.
	err := d.InTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT instance_id,device_id,capability_id,binding_hash FROM iot_observations")
		if err != nil {
			return err
		}
		type key struct{ i, d, c, fingerprint string }
		var revoked []key
		for rows.Next() {
			var k key
			if err = rows.Scan(&k.i, &k.d, &k.c, &k.fingerprint); err != nil {
				_ = rows.Close()
				return err
			}
			if _, ok := r.allowed[iotObservationKey(k.d, k.c)]; !ok || k.i != owned.HomeAssistant.ID || k.fingerprint != r.fingerprints[iotObservationKey(k.d, k.c)] {
				revoked = append(revoked, k)
			}
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		for _, k := range revoked {
			if _, err = tx.ExecContext(ctx, "DELETE FROM iot_observations WHERE instance_id=? AND device_id=? AND capability_id=?", k.i, k.d, k.c); err != nil {
				return err
			}
		}
		return nil
	})
	return r, err
}

type iotQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (r *IoTRepository) load(ctx context.Context, q iotQuerier) (map[string]iot.Observation, *time.Time, error) {
	out := map[string]iot.Observation{}
	rows, err := q.QueryContext(ctx, "SELECT device_id,capability_id,observation_json,sync_at,binding_hash FROM iot_observations WHERE instance_id=?", r.cfg.HomeAssistant.ID)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	var last *time.Time
	for rows.Next() {
		var d, c, raw, syncAt, fingerprint string
		if err = rows.Scan(&d, &c, &raw, &syncAt, &fingerprint); err != nil {
			return nil, nil, err
		}
		key := iotObservationKey(d, c)
		if _, ok := r.allowed[key]; !ok || fingerprint != r.fingerprints[key] {
			continue
		}
		var o iot.Observation
		if err = json.Unmarshal([]byte(raw), &o); err != nil {
			return nil, nil, err
		}
		o.InstanceID = r.cfg.HomeAssistant.ID
		o.DeviceID = d
		o.CapabilityID = c
		out[key] = o
		if at, e := time.Parse(time.RFC3339Nano, syncAt); e == nil && (last == nil || at.After(*last)) {
			last = &at
		}
	}
	return out, last, rows.Err()
}

func (r *IoTRepository) Read(ctx context.Context) (iot.Snapshot, error) {
	r.db.iotMu.Lock()
	defer r.db.iotMu.Unlock()
	if !r.cfg.Enabled || r.db.iotSession != r {
		return iot.Snapshot{Instances: []iot.Instance{}}, nil
	}
	obs := map[string]iot.Observation{}
	var last *time.Time
	if !r.cacheHidden {
		var err error
		obs, last, err = r.load(ctx, r.db.sql)
		if err != nil {
			return iot.Snapshot{}, err
		}
	}
	inst := iot.Instance{ID: r.cfg.HomeAssistant.ID, Provider: "home_assistant", Generation: r.generation, Status: r.status, LastSuccessAt: last, Devices: []iot.Device{}}
	for _, d := range r.cfg.HomeAssistant.Devices {
		dev := iot.Device{ID: d.ID, Name: d.Name, RoomID: d.RoomID, RoomName: d.RoomName, Capabilities: []iot.Capability{}}
		for _, c := range d.Capabilities {
			o, ok := obs[iotObservationKey(d.ID, c.ID)]
			if !ok {
				o = iot.Observation{InstanceID: inst.ID, DeviceID: d.ID, CapabilityID: c.ID, Generation: r.generation, Availability: iot.Unknown, Reason: iot.ReasonMissing}
			}
			dev.Capabilities = append(dev.Capabilities, iot.Capability{ID: c.ID, Name: c.Name, Kind: c.Kind, Observation: o})
		}
		inst.Devices = append(inst.Devices, dev)
	}
	return iot.Snapshot{Instances: []iot.Instance{inst}}, nil
}

// Disconnect makes reader health honest even when storage is unavailable.
func (r *IoTRepository) Disconnect() {
	r.db.iotMu.Lock()
	defer r.db.iotMu.Unlock()
	r.status = iot.Disconnected
}

// Apply commits the entire replacement/delta atomically. The returned boolean
// describes visible changes, excluding receipt/confirmation bookkeeping.
func (r *IoTRepository) Apply(ctx context.Context, u iot.Update) (changed bool, err error) {
	r.db.iotMu.Lock()
	defer r.db.iotMu.Unlock()
	if r.db.iotSession != r {
		return false, errors.New("iot session retired")
	}
	if !r.cfg.Enabled || u.InstanceID != r.cfg.HomeAssistant.ID || u.Generation < r.generation {
		return false, nil
	}
	if u.Status != iot.Connected && u.Status != iot.Connecting && u.Status != iot.Disconnected {
		return false, errors.New("invalid iot status")
	}
	defer func() {
		if err != nil {
			r.status = iot.Disconnected
		}
	}()
	if u.Status == iot.Connected && !u.Replace && (r.status != iot.Connected || u.Generation != r.generation) {
		return false, errors.New("iot full synchronization required")
	}
	// Authorization revocation must hide prior readings even if SQL cannot
	// delete them. Only a committed full synchronization restores visibility.
	if u.Replace && u.Status == iot.Disconnected {
		r.cacheHidden = true
	}
	err = r.db.InTx(ctx, func(tx *sql.Tx) error {
		old, last, e := r.load(ctx, tx)
		if e != nil {
			return e
		}
		next := map[string]iot.Observation{}
		if !u.Replace {
			for k, o := range old {
				next[k] = o
			}
		}
		if u.Replace {
			last = nil
		}
		if u.Status == iot.Connected {
			for _, o := range u.Observations {
				key := iotObservationKey(o.DeviceID, o.CapabilityID)
				cap, ok := r.allowed[key]
				if !ok || o.InstanceID != u.InstanceID || o.Generation != u.Generation {
					continue
				}
				// Revalidate at the persistence boundary: never store unbounded provider data.
				n := iot.Normalize(cap.Kind, o.Value, o.Unit, map[string]string{"off": "off", "idle": "idle", "active": "active", "paused": "paused", "error": "error"})
				if o.Availability == iot.Available {
					o.Value = n.Value
					o.Unit = n.Unit
					o.Availability = n.Availability
					o.Reason = n.Reason
				} else {
					o.Value = nil
					o.Unit = ""
					switch o.Reason {
					case iot.ReasonUnknown, iot.ReasonUnavailable, iot.ReasonMissing, iot.ReasonInvalid:
					default:
						o.Reason = iot.ReasonInvalid
					}
					if o.Availability != iot.Unavailable {
						o.Availability = iot.Unknown
					}
				}
				next[key] = o
				if u.Replace && !o.ConfirmedAt.IsZero() && (last == nil || o.ConfirmedAt.After(*last)) {
					at := o.ConfirmedAt
					last = &at
				}
			}
		}
		changed = r.status != u.Status || !reflect.DeepEqual(visibleObservations(old), visibleObservations(next))
		if _, e = tx.ExecContext(ctx, "DELETE FROM iot_observations WHERE instance_id=?", u.InstanceID); e != nil {
			return e
		}
		syncAt := ""
		if last != nil {
			syncAt = last.Format(time.RFC3339Nano)
		}
		for _, o := range next {
			raw, e := json.Marshal(o)
			if e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, "INSERT INTO iot_observations VALUES (?,?,?,?,?,?)", o.InstanceID, o.DeviceID, o.CapabilityID, string(raw), syncAt, r.fingerprints[iotObservationKey(o.DeviceID, o.CapabilityID)]); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	if u.Replace && u.Status == iot.Connected {
		r.cacheHidden = false
	}
	r.generation = u.Generation
	r.status = u.Status
	return changed, nil
}
func visibleObservations(in map[string]iot.Observation) map[string]iot.Observation {
	out := map[string]iot.Observation{}
	for k, o := range in {
		o.ReceivedAt = time.Time{}
		o.ConfirmedAt = time.Time{}
		o.Generation = 0
		out[k] = o
	}
	return out
}

// JSON uses stable map-key ordering. Persist only the digest; neither provider
// identifiers nor origins enter the database. Presentation-only edits retain data.
func iotBindingFingerprint(origin string, cap config.IoTCapability) string {
	binding := struct {
		Origin, EntityID, Attribute, Unit string
		Kind                              iot.Kind
		Values                            map[string]string
	}{origin, cap.EntityID, cap.Attribute, cap.Unit, cap.Kind, cap.Values}
	raw, _ := json.Marshal(binding)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
