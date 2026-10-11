package archive

// ScreenDocument is a public, current read model. Processing is deliberately a
// presentation state rather than a worker state; it discloses no account or job.
type ScreenDocument struct {
	TaskSummary   ScreenTaskSummary `json:"task_summary"`
	Item          SearchItem        `json:"item"`
	OriginalSaved bool              `json:"original_saved"`
	NASStatus     string            `json:"nas_status"`
	Processing    string            `json:"processing"`
	Preview       string            `json:"preview"`
	Generation    string            `json:"generation"`
	Coverage      SearchCoverage    `json:"coverage"`
}

func ScreenProcessing(i SearchItem) string {
	if i.ReviewRequired || i.Ownership == "waiting" {
		return "needs_review"
	}
	if i.MetadataOnly {
		return "processing"
	}
	if i.ParseStatus == "parsed" {
		return "ready"
	}
	return "saved_only"
}

// ScreenTaskSummary contains user-facing stages, never internal worker state,
// job IDs, provider identities or private object counts.
type ScreenTaskSummary struct {
	Parse         string `json:"parse"`
	Understanding string `json:"understanding"`
	Attribution   string `json:"attribution"`
}

func SummarizeScreenTask(i SearchItem) ScreenTaskSummary {
	s := ScreenTaskSummary{Parse: i.ParseStatus, Understanding: "complete", Attribution: i.Ownership}
	if i.MetadataOnly {
		s.Understanding = "pending"
	} else if i.ReviewRequired {
		s.Understanding = "needs_review"
	} else if i.ParseStatus != "parsed" {
		s.Understanding = "unavailable"
	}
	return s
}
