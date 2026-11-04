package config

import (
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const iotYAML = `
home: {timezone: Asia/Singapore}
iot:
 enabled: true
 home_assistant:
  id: home
  url: https://ha.example.invalid
  token_file: ./secrets/ha-token
  ca_file: ./secrets/ca.pem
  devices:
   - id: humidifier
     name: Humidifier
     room_id: living_room
     room_name: Living room
     capabilities:
      - {id: power, kind: power, entity_id: humidifier.example}
      - {id: humidity, kind: humidity, entity_id: humidifier.example, attribute: current_humidity}
`

func TestIoTConfig(t *testing.T) {
	if Defaults().IoT.Enabled {
		t.Fatal("enabled by default")
	}
	c, e := Parse([]byte(iotYAML))
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Validate(); e != nil {
		t.Fatal(e)
	}
	if !filepath.IsAbs(c.IoT.HomeAssistant.TokenFile) || !filepath.IsAbs(c.IoT.HomeAssistant.CAFile) {
		t.Fatal("paths not expanded")
	}
	b, e := yaml.Marshal(c.Redacted())
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{"ha.example.invalid", "ha-token", "ca.pem", "humidifier.example", "current_humidity"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("redaction leaked %s", secret)
		}
	}
	if c.IoT.HomeAssistant.Devices[0].Capabilities[0].EntityID != "humidifier.example" {
		t.Fatal("redaction mutated original")
	}
}
func TestIoTRejectsInvalidConfig(t *testing.T) {
	for _, tc := range []struct{ name, from, to string }{
		{"http", "https://ha.example.invalid", "http://ha.example.invalid"},
		{"credentials", "https://ha.example.invalid", "https://user:secret@ha.example.invalid"},
		{"query", "https://ha.example.invalid", "https://ha.example.invalid?token=secret"},
		{"token missing", "token_file: ./secrets/ha-token", "token_file: ''"},
		{"inline token", "token_file: ./secrets/ha-token", "token: secret"},
		{"kind", "kind: power", "kind: unlock"},
		{"duplicate capability", "id: humidity, kind", "id: power, kind"},
		{"duplicate binding", ", attribute: current_humidity", ""},
		{"bad ID", "id: humidifier", "id: bad/id"},
		{"missing room", "room_id: living_room", "room_id: ''"},
		{"enum missing", "kind: power", "kind: operating_state"},
		{"enum invalid", "kind: power", "kind: operating_state, values: {on: secret}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, e := Parse([]byte(strings.Replace(iotYAML, tc.from, tc.to, 1)))
			if e == nil {
				e = c.Validate()
			}
			if e == nil {
				t.Fatal("accepted invalid config")
			}
			if strings.Contains(e.Error(), "user:secret") {
				t.Fatal("error leaked URL credentials")
			}
		})
	}
}
func TestIoTRejectsCrossDeviceEntityAndDuplicateDevice(t *testing.T) {
	c, _ := Parse([]byte(iotYAML))
	d := c.IoT.HomeAssistant.Devices[0]
	d.ID = "other"
	c.IoT.HomeAssistant.Devices = append(c.IoT.HomeAssistant.Devices, d)
	if c.Validate() == nil {
		t.Fatal("cross-device entity accepted")
	}
	c, _ = Parse([]byte(iotYAML))
	c.IoT.HomeAssistant.Devices = append(c.IoT.HomeAssistant.Devices, c.IoT.HomeAssistant.Devices[0])
	if c.Validate() == nil {
		t.Fatal("duplicate device accepted")
	}
}

func TestIoTCapabilityLabelAndUnit(t *testing.T) {
	raw := strings.Replace(iotYAML, "attribute: current_humidity", "attribute: current_humidity, name: Current humidity, unit: '%'", 1)
	c, e := Parse([]byte(raw))
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Validate(); e != nil {
		t.Fatal(e)
	}
	cap := c.IoT.HomeAssistant.Devices[0].Capabilities[1]
	if cap.Name != "Current humidity" || cap.Unit != "%" {
		t.Fatal("label or unit lost")
	}
	c.IoT.HomeAssistant.Devices[0].Capabilities[1].Unit = "°C"
	if c.Validate() == nil {
		t.Fatal("incompatible configured unit accepted")
	}
}

func TestIoTRejectsWhitespaceSecretPathsBeforeExpansion(t *testing.T) {
	for _, field := range []string{"token_file", "ca_file"} {
		t.Run(field, func(t *testing.T) {
			from := "token_file: ./secrets/ha-token"
			if field == "ca_file" {
				from = "ca_file: ./secrets/ca.pem"
			}
			raw := []byte(strings.Replace(iotYAML, from, field+": '   '", 1))
			if _, err := Parse(raw); err == nil {
				t.Fatal("Parse expanded whitespace path")
			}
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("Load accepted whitespace path")
			}
		})
	}
}
