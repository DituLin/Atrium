package iot

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestNormalize(t *testing.T) {
	for _, tc := range []struct {
		name         string
		kind         Kind
		raw          any
		unit         string
		values       map[string]string
		value        any
		availability Availability
		reason       Reason
	}{
		{"false", Power, "off", "", nil, false, Available, ""},
		{"zero", Humidity, "0", "%", nil, float64(0), Available, ""},
		{"fahrenheit", Temperature, "77", "°F", nil, float64(25), Available, ""},
		{"celsius", Temperature, 21.5, "°C", nil, 21.5, Available, ""},
		{"battery", Battery, "50", "%", nil, float64(50), Available, ""},
		{"filter", FilterLife, "100", "%", nil, float64(100), Available, ""},
		{"state", OperatingState, "humidifying", "", map[string]string{"humidifying": "active"}, "active", Available, ""},
		{"unknown", Power, "unknown", "", nil, nil, Unknown, ReasonUnknown},
		{"unavailable", Humidity, "unavailable", "%", nil, nil, Unavailable, ReasonUnavailable},
		{"missing", Humidity, nil, "%", nil, nil, Unknown, ReasonMissing},
		{"nan", Humidity, math.NaN(), "%", nil, nil, Unknown, ReasonInvalid},
		{"conversion overflow", Temperature, math.MaxFloat64, "°F", nil, nil, Unknown, ReasonInvalid},
		{"inf", Temperature, "+Inf", "°C", nil, nil, Unknown, ReasonInvalid},
		{"range", Humidity, "101", "%", nil, nil, Unknown, ReasonInvalid},
		{"absolute zero", Temperature, "-274", "°C", nil, nil, Unknown, ReasonInvalid},
		{"wrong unit", Humidity, "30", "°C", nil, nil, Unknown, ReasonInvalid},
		{"missing unit", Temperature, "30", "", nil, nil, Unknown, ReasonInvalid},
		{"bad power", Power, "maybe", "", nil, nil, Unknown, ReasonInvalid},
		{"unmapped", OperatingState, "secret error", "", map[string]string{"on": "active"}, nil, Unknown, ReasonInvalid},
		{"bad enum", OperatingState, "on", "", map[string]string{"on": "secret"}, nil, Unknown, ReasonInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Normalize(tc.kind, tc.raw, tc.unit, tc.values)
			if got.Value != tc.value || got.Availability != tc.availability || got.Reason != tc.reason {
				t.Fatalf("got %#v", got)
			}
		})
	}
}
func TestObservationJSONKeepsNullFalseAndClocks(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	o := Observation{InstanceID: "home", DeviceID: "humidifier", CapabilityID: "power", Generation: 2, Value: false, Availability: Available, SourceUpdatedAt: &now, ReceivedAt: now.Add(time.Second), ConfirmedAt: now.Add(2 * time.Second)}
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`"value":false`, `"source_updated_at"`, `"received_at"`, `"confirmed_at"`} {
		if !strings.Contains(string(b), s) {
			t.Fatalf("missing %s: %s", s, b)
		}
	}
	o.Value = nil
	b, _ = json.Marshal(o)
	if !strings.Contains(string(b), `"value":null`) {
		t.Fatal(string(b))
	}
}

func TestInstancePublicProviderAndLastSuccess(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b, err := json.Marshal(Instance{ID: "home", Provider: "home_assistant", LastSuccessAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`"provider":"home_assistant"`, `"last_success_at":"2026-01-01T00:00:00Z"`} {
		if !strings.Contains(string(b), s) {
			t.Fatalf("missing %s: %s", s, b)
		}
	}
}
