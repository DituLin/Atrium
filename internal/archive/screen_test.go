package archive

import "testing"

func TestScreenStageMappingsPreservePublishedFacts(t *testing.T) {
	for _, tc := range []struct{ task, processing, parse, understanding string }{
		{"queued", "processing", "pending", "pending"}, {"running", "processing", "pending", "pending"},
		{"retry_wait", "waiting", "waiting", "pending"}, {"waiting_input", "needs_review", "pending", "needs_review"},
		{"failed", "saved_only", "unavailable", "unavailable"}, {"cancelled", "saved_only", "unavailable", "unavailable"},
		{"succeeded", "saved_only", "unavailable", "unavailable"}, {"", "saved_only", "unavailable", "unavailable"},
	} {
		t.Run(tc.task, func(t *testing.T) {
			item := SearchItem{MetadataOnly: true, ParseStatus: "pending", Ownership: "unassigned", TaskStatus: tc.task}
			PresentScreenItem(&item)
			if item.Processing != tc.processing || item.TaskSummary.Parse != tc.parse || item.TaskSummary.Understanding != tc.understanding || !item.OriginalSaved || item.TaskStatus != "" {
				t.Fatalf("unexpected public presentation: %#v %#v", item, item.TaskSummary)
			}
			item = SearchItem{ParseStatus: "parsed", Ownership: "household", TaskStatus: tc.task}
			PresentScreenItem(&item)
			if item.Processing != "ready" || item.TaskSummary.Parse != "parsed" || item.TaskSummary.Understanding != "complete" {
				t.Fatal("reprocess task hid valid publication", item)
			}
		})
	}
	for _, status := range []string{"encrypted", "partial", "invalid"} {
		item := SearchItem{ParseStatus: status, TaskStatus: "succeeded"}
		PresentScreenItem(&item)
		if item.Processing == "processing" || item.TaskSummary.Understanding == "pending" {
			t.Fatal("completed unavailable content is not running", item)
		}
	}
}
