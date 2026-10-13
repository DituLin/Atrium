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

// ScreenProcessing derives the screen-facing processing state from a current item.
func ScreenProcessing(i SearchItem) string {
	// A failed reprocessing attempt does not invalidate an existing publication.
	if !i.MetadataOnly {
		if i.ReviewRequired || i.Ownership == "waiting" {
			return "needs_review"
		}
		if i.ParseStatus == "parsed" {
			return "ready"
		}
		return "saved_only"
	}
	switch i.TaskStatus {
	case "queued", "running":
		return "processing"
	case "retry_wait":
		return "waiting"
	case "waiting_input":
		return "needs_review"
	default:
		return "saved_only"
	}
}

// ScreenTaskSummary contains user-facing stages, never internal worker state,
// job IDs, provider identities or private object counts.
type ScreenTaskSummary struct {
	Parse         string `json:"parse"`
	Understanding string `json:"understanding"`
	Attribution   string `json:"attribution"`
}

// SummarizeScreenTask summarizes parse, understanding and attribution status for display.
func SummarizeScreenTask(i SearchItem) ScreenTaskSummary {
	s := ScreenTaskSummary{Parse: i.ParseStatus, Understanding: "complete", Attribution: i.Ownership}
	if i.MetadataOnly {
		switch ScreenProcessing(i) {
		case "processing":
			s.Parse = "pending"
			s.Understanding = "pending"
		case "waiting":
			s.Parse = "waiting"
			s.Understanding = "pending"
		case "needs_review":
			s.Parse = "pending"
			s.Understanding = "needs_review"
		default:
			s.Parse = "unavailable"
			s.Understanding = "unavailable"
		}
	} else if i.ReviewRequired {
		s.Understanding = "needs_review"
	} else if i.ParseStatus != "parsed" {
		s.Understanding = "unavailable"
	}
	return s
}

// PresentScreenItem adds processing and task-summary fields to a screen item.
func PresentScreenItem(i *SearchItem) {
	i.Processing = ScreenProcessing(*i)
	summary := SummarizeScreenTask(*i)
	i.TaskSummary = &summary
	i.OriginalSaved = true
	i.TaskStatus = ""
}
