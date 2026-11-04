package config

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/DituLin/Atrium/internal/iot"
)

// IoT configures the optional read-only device integration.
type IoT struct {
	Enabled       bool          `yaml:"enabled"`
	HomeAssistant HomeAssistant `yaml:"home_assistant"`
}

// HomeAssistant identifies one source and its explicitly allowed devices.
type HomeAssistant struct {
	ID        string      `yaml:"id"`
	URL       string      `yaml:"url"`
	TokenFile string      `yaml:"token_file"`
	CAFile    string      `yaml:"ca_file,omitempty"`
	Devices   []IoTDevice `yaml:"devices"`
}

// IoTDevice gives a source device a stable public identity and room.
type IoTDevice struct {
	ID           string          `yaml:"id"`
	Name         string          `yaml:"name"`
	RoomID       string          `yaml:"room_id"`
	RoomName     string          `yaml:"room_name"`
	Capabilities []IoTCapability `yaml:"capabilities"`
}

// IoTCapability binds one public capability to a source entity or attribute.
type IoTCapability struct {
	Name string `yaml:"name,omitempty"`
	// Unit overrides provider units for attributes with no unit metadata.
	Unit      string            `yaml:"unit,omitempty"`
	ID        string            `yaml:"id"`
	Kind      iot.Kind          `yaml:"kind"`
	EntityID  string            `yaml:"entity_id"`
	Attribute string            `yaml:"attribute,omitempty"`
	Values    map[string]string `yaml:"values,omitempty"`
}

var iotID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)
var haEntityID = regexp.MustCompile(`^[a-z0-9_]+\.[a-z0-9_]+$`)
var haAttribute = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// Validate accepts a completely absent disabled configuration. Configured bindings
// are validated even while disabled so enabling cannot activate ambiguous mappings.
func (c IoT) Validate() error {
	h := c.HomeAssistant
	if !c.Enabled && h.ID == "" && h.URL == "" && h.TokenFile == "" && h.CAFile == "" && len(h.Devices) == 0 {
		return nil
	}
	var errs []error
	add := func(s string, args ...any) { errs = append(errs, fmt.Errorf("iot: "+s, args...)) }
	if !iotID.MatchString(h.ID) {
		add("home_assistant.id is invalid")
	}
	u, e := url.Parse(h.URL)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		add("home_assistant.url must be an HTTPS origin without credentials, path, query or fragment")
	}
	if strings.TrimSpace(h.TokenFile) == "" {
		add("home_assistant.token_file is required")
	}
	devices := map[string]bool{}
	entities := map[string]string{}
	rooms := map[string]string{}
	for i, d := range h.Devices {
		p := fmt.Sprintf("home_assistant.devices[%d]", i)
		if !iotID.MatchString(d.ID) || devices[d.ID] {
			add("%s.id is invalid or duplicated", p)
		}
		devices[d.ID] = true
		if strings.TrimSpace(d.Name) == "" || len(d.Name) > 128 {
			add("%s.name is required and bounded", p)
		}
		if !iotID.MatchString(d.RoomID) || strings.TrimSpace(d.RoomName) == "" || len(d.RoomName) > 128 {
			add("%s room ID and name are required and bounded", p)
		}
		if n, ok := rooms[d.RoomID]; ok && n != d.RoomName {
			add("%s room name conflicts with room ID", p)
		}
		rooms[d.RoomID] = d.RoomName
		if len(d.Capabilities) == 0 {
			add("%s.capabilities must not be empty", p)
		}
		ids := map[string]bool{}
		bindings := map[string]bool{}
		for j, c := range d.Capabilities {
			q := fmt.Sprintf("%s.capabilities[%d]", p, j)
			if !iotID.MatchString(c.ID) || ids[c.ID] {
				add("%s.id is invalid or duplicated", q)
			}
			ids[c.ID] = true
			if len(c.Name) > 128 {
				add("%s.name must be bounded", q)
			}
			if c.Unit != "" {
				valid := false
				switch c.Kind {
				case iot.Humidity, iot.Battery, iot.FilterLife:
					valid = c.Unit == "%"
				case iot.Temperature:
					valid = c.Unit == "°C" || c.Unit == "C" || c.Unit == "°F" || c.Unit == "F"
				}
				if !valid {
					add("%s.unit is incompatible with kind", q)
				}
			}
			if !c.Kind.Valid() {
				add("%s.kind is unsupported", q)
			}
			if !haEntityID.MatchString(c.EntityID) {
				add("%s.entity_id is invalid", q)
			}
			if c.Attribute != "" && !haAttribute.MatchString(c.Attribute) {
				add("%s.attribute is invalid", q)
			}
			key := c.EntityID + "\x00" + c.Attribute
			if bindings[key] {
				add("%s binding is duplicated", q)
			}
			bindings[key] = true
			if owner, ok := entities[c.EntityID]; ok && owner != d.ID {
				add("%s entity is already bound to another device", q)
			}
			entities[c.EntityID] = d.ID
			if c.Kind == iot.OperatingState {
				if len(c.Values) == 0 {
					add("%s.values is required for operating_state", q)
				}
				for raw, v := range c.Values {
					if strings.TrimSpace(raw) == "" || !iot.ValidOperatingState(v) {
						add("%s.values contains an invalid state mapping", q)
					}
				}
			} else if len(c.Values) > 0 {
				add("%s.values is only supported for operating_state", q)
			}
		}
	}
	return errors.Join(errs...)
}
