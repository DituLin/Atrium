package cli

import (
	"fmt"
	"strings"
)

// configTemplateValues are the answers `atrium init` substitutes.
type configTemplateValues struct {
	Timezone  string
	PublicURL string
	DataDir   string
}

// configTemplate mirrors config.example.yaml. It is kept here so the generated
// file carries the same comments without shipping an extra data file.
const configTemplate = `# Atrium Home Hub configuration.
# Every key below shows its default value. State (screens, pairings, index)
# lives in SQLite, not in this file.

home:
  name: "Home"
  timezone: "%s"          # IANA name; required. Do not infer a location from it.

server:
  listen: "0.0.0.0:8443"
  public_url: "%s"   # origin the TV opens; used for Origin checks and TLS SANs
  extra_sans: []                      # e.g. ["macmini.local"]
  allowed_origins: []                 # defaults to the origin of public_url + localhost variants
  tls:
    mode: "auto"                      # auto | file | off (off = insecure demo only)
    cert_file: ""
    key_file: ""

storage:
  data_dir: "%s"
  cache_budget_bytes: 10737418240     # 10 GiB
  min_free_bytes: 5368709120          # pause preview generation below 5 GiB free

# Add a read-only NAS mount here when you reach V0.2. Nothing outside the root
# is ever read, and Atrium never stores NAS credentials.
sources: []

media:
  workers: 2
  decode_timeout: "30s"
  preview_max_edge: 2560
  preview_max_bytes: 1048576
  thumb_max_edge: 480
  max_pixels: 80000000
  max_source_bytes: 62914560
  heic:
    converter: "sips"                 # sips | off

screens:
  heartbeat_interval: "15s"
  offline_after: "45s"
  command_ttl: "10s"
  slideshow_interval: "30s"

widgets:
  weather:
    enabled: false
    provider: "open-meteo"
    latitude: 0
    longitude: 0
    location_label: ""
    refresh_interval: "30m"
    stale_after: "2h"
  notice:
    enabled: false
    text: ""

logging:
  level: "info"                       # debug | info | warn | error
  retain_days: 7
  max_total_mb: 200

backup:
  enabled: false
  dir: ""                             # must be outside every source root and outside data_dir
  time: "03:00"
  keep: 7
`

// renderConfig substitutes the operator's answers into the template.
func renderConfig(v configTemplateValues) string {
	return fmt.Sprintf(configTemplate,
		yamlEscape(v.Timezone), yamlEscape(v.PublicURL), yamlEscape(v.DataDir))
}

// yamlEscape makes a value safe inside a double-quoted YAML scalar.
func yamlEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}
