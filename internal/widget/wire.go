package widget

import (
	"encoding/json"
	"fmt"
)

// Wire shape of GET /api/v1/home (design §6.7, PRD WidgetSnapshot): a closed
// list of typed widgets so a client can hide types it does not know, plus the
// home identity and the change-bus version the WebSocket `data.changed`
// messages refer to. The Go struct keeps named fields for internal callers;
// the JSON form is produced here.
type snapshotWire struct {
	SchemaVersion int          `json:"schema_version"`
	ServerTime    string       `json:"server_time"`
	Version       int64        `json:"version"`
	Home          homeWire     `json:"home"`
	Widgets       []widgetWire `json:"widgets"`
}

type homeWire struct {
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
}

type widgetWire struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// MarshalJSON renders the envelope form.
func (s Snapshot) MarshalJSON() ([]byte, error) {
	widgets := make([]widgetWire, 0, 5)
	add := func(kind string, payload any) error {
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("widget %s: %w", kind, err)
		}
		widgets = append(widgets, widgetWire{Type: kind, Payload: raw})
		return nil
	}
	if err := add("clock", s.Clock); err != nil {
		return nil, err
	}
	if err := add("photo", s.Photo); err != nil {
		return nil, err
	}
	if err := add("nas", s.NAS); err != nil {
		return nil, err
	}
	if s.Weather != nil {
		if err := add("weather", s.Weather); err != nil {
			return nil, err
		}
	}
	if s.Notice != nil {
		if err := add("notice", s.Notice); err != nil {
			return nil, err
		}
	}
	return json.Marshal(snapshotWire{
		SchemaVersion: s.SchemaVersion,
		ServerTime:    s.ServerTime,
		Version:       s.Version,
		Home:          homeWire{Name: s.HomeName, Timezone: s.Clock.Timezone},
		Widgets:       widgets,
	})
}

// UnmarshalJSON accepts the envelope form (used by the CLI and tests).
func (s *Snapshot) UnmarshalJSON(data []byte) error {
	var w snapshotWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	s.SchemaVersion = w.SchemaVersion
	s.ServerTime = w.ServerTime
	s.Version = w.Version
	s.HomeName = w.Home.Name
	s.Weather, s.Notice = nil, nil
	for _, item := range w.Widgets {
		var err error
		switch item.Type {
		case "clock":
			err = json.Unmarshal(item.Payload, &s.Clock)
		case "photo":
			err = json.Unmarshal(item.Payload, &s.Photo)
		case "nas":
			err = json.Unmarshal(item.Payload, &s.NAS)
		case "weather":
			s.Weather = &Weather{}
			err = json.Unmarshal(item.Payload, s.Weather)
		case "notice":
			s.Notice = &Notice{}
			err = json.Unmarshal(item.Payload, s.Notice)
		default:
			// Unknown widget types are ignored, mirroring the client rule.
		}
		if err != nil {
			return fmt.Errorf("widget %s: %w", item.Type, err)
		}
	}
	return nil
}
