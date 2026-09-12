// Package briefing builds deterministic public family overviews without model or media dependencies.
package briefing

import "github.com/DituLin/Atrium/internal/family"

// Notice retains only configured content and its genuine content times.
type Notice struct {
	family.ItemMeta
	Text string `json:"text"`
}

// Sources contains the independent public observations referenced by entries.
type Sources struct {
	Core        family.SourceSnapshot[family.CoreResponse] `json:"core"`
	NAS         []family.SourceSnapshot[family.NASHealth]  `json:"nas"`
	Profile     family.SourceSnapshot[struct{}]            `json:"profile"`
	Environment family.SourceSnapshot[struct{}]            `json:"environment"`
	Notice      family.SourceSnapshot[Notice]              `json:"notice"`
	Calendar    family.SourceSnapshot[struct{}]            `json:"calendar"`
}

// Entry is a stable reference, never a copy or renewed observation of source content.
type Entry struct {
	ID       string  `json:"id"`
	Kind     string  `json:"kind"`
	Module   string  `json:"module"`
	SourceID string  `json:"source_id"`
	ItemID   *string `json:"item_id"`
}

// OverviewResponse uses one generation instant for all module projections.
type OverviewResponse struct {
	SchemaVersion int              `json:"schema_version"`
	GeneratedAt   string           `json:"generated_at"`
	Home          family.HouseHome `json:"home"`
	Sources       Sources          `json:"sources"`
	Entries       []Entry          `json:"entries"`
}
