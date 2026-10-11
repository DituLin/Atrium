package archive

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestRetainedModelPublicationSurvivesPromptUpgrade(t *testing.T) {
	raw, e := os.ReadFile("testdata/t15-h02-retained.json")
	require.NoError(t, e)
	var b struct {
		Parsed        ParseResult
		Plan          UnderstandingPlan
		Understanding Understanding
	}
	require.NoError(t, json.Unmarshal(raw, &b))
	out, e := CompilePublication("synthetic", "doc", b.Parsed, b.Plan, b.Understanding)
	require.NoError(t, e)
	require.NotEmpty(t, out.Events)
	r := b.Understanding.Chunks[0]
	r.Receipt.ConfigurationFingerprint = "unknown-policy"
	r.Digest = r.digest()
	_, e = MergeChunks(b.Plan, []ChunkUnderstanding{r})
	require.Error(t, e)
}

func TestRealReportNotificationRetainsUnresolvedEventSubject(t *testing.T) {
	raw, e := os.ReadFile("testdata/t15-a01-retained.json")
	require.NoError(t, e)
	var b struct {
		Parsed        ParseResult
		Plan          UnderstandingPlan
		Understanding Understanding
	}
	require.NoError(t, json.Unmarshal(raw, &b))
	c, e := CompilePublication("synthetic", "doc", b.Parsed, b.Plan, b.Understanding)
	require.NoError(t, e)
	require.Len(t, c.Resolution.Mentions, 1)
	require.True(t, c.Resolution.Mentions[0].Subject, "explicit report patient remains an archive subject without invented fields")
	require.Empty(t, c.Resolution.Fields)
	snapshot := CandidateSnapshot{HouseholdID: "synthetic", Complete: true, Candidates: []EntityCandidate{{ID: "first", Version: 1, Kind: "person", Names: []CandidateName{{Value: "张明", Evidence: []string{"first-source"}}}, Birth: IdentityDate{Value: "1980-03-12", Precision: "date", Evidence: []string{"first-source"}}}, {ID: "second", Version: 1, Kind: "person", Names: []CandidateName{{Value: "张明", Evidence: []string{"second-source"}}}, Birth: IdentityDate{Value: "1992-08-20", Precision: "date", Evidence: []string{"second-source"}}}}}
	plan, e := ResolveEntities(c.Resolution, snapshot)
	require.NoError(t, e)
	require.Len(t, plan.Decisions, 1)
	require.Equal(t, "needs_resolution", plan.Decisions[0].Action)
}

func TestRealScoreChunksShareOnlyExactOriginalNameLocation(t *testing.T) {
	raw, e := os.ReadFile("testdata/t15-e02-retained.json")
	require.NoError(t, e)
	var b struct {
		Parsed        ParseResult
		Plan          UnderstandingPlan
		Understanding Understanding
	}
	require.NoError(t, json.Unmarshal(raw, &b))
	c, e := CompilePublication("synthetic", "doc", b.Parsed, b.Plan, b.Understanding)
	require.NoError(t, e)
	students := []ResolutionMention{}
	for _, m := range c.Resolution.Mentions {
		if m.Role == "student" {
			students = append(students, m)
		}
	}
	require.Len(t, students, 1, "same literal name position must not split when a model also cites its role label")
	require.Equal(t, "2012-11-06", students[0].Birth.Value)
	for _, event := range c.Events {
		for _, f := range event.Fields {
			if f.Name == "数学" || f.Name == "full_score" {
				require.Equal(t, []string{students[0].Ref}, f.Subjects)
			}
		}
	}
}

func TestRealReportHeadingAlternativesSharePhysicalSection(t *testing.T) {
	raw, e := os.ReadFile("testdata/t15-h01-retained.json")
	require.NoError(t, e)
	var b struct {
		Parsed        ParseResult
		Plan          UnderstandingPlan
		Understanding Understanding
	}
	require.NoError(t, json.Unmarshal(raw, &b))
	c, e := CompilePublication("synthetic", "doc", b.Parsed, b.Plan, b.Understanding)
	require.NoError(t, e)
	require.False(t, c.RequiresReview, "equivalent confirmed whole headings must not block unrelated safe facts")
	require.Len(t, c.Events, 2)
	uncertain := 0
	for _, ev := range c.Events {
		for _, f := range ev.Fields {
			if f.RequiresReview {
				uncertain++
			}
		}
	}
	require.Positive(t, uncertain, "real OCR field conflicts must remain uncertain")
}

func TestReportSectionEquivalenceRejectsUncertainAndPartialAnchors(t *testing.T) {
	raw, e := os.ReadFile("testdata/t15-h01-retained.json")
	require.NoError(t, e)
	var b struct {
		Parsed        ParseResult
		Understanding Understanding
	}
	require.NoError(t, json.Unmarshal(raw, &b))
	require.Len(t, publicationReportSections(b.Parsed, b.Understanding.Events), 2)
	for _, kind := range []string{"conflict", "review", "clipped", "different_text", "partial", "different_parent"} {
		t.Run(kind, func(t *testing.T) {
			var x struct {
				Parsed        ParseResult
				Understanding Understanding
			}
			require.NoError(t, json.Unmarshal(raw, &x))
			parent := x.Understanding.Events[2].CanonicalEvidence[0].ParentSegmentID
			for i := range x.Parsed.Segments {
				s := &x.Parsed.Segments[i]
				if s.ID != parent {
					continue
				}
				switch kind {
				case "conflict":
					s.Visual.State = "conflict"
				case "review":
					s.Visual.RequiresReview = true
				case "clipped":
					s.Visual.Alternatives[0].Clipped = true
				case "different_text":
					s.Visual.Alternatives[0].Text += "!"
				}
			}
			switch kind {
			case "partial":
				x.Understanding.Events[2].CanonicalEvidence[0].Start = 1
			case "different_parent":
				x.Understanding.Events[2].CanonicalEvidence[0].ParentSegmentID = "other"
			}
			require.Len(t, publicationReportSections(x.Parsed, x.Understanding.Events), 3)
		})
	}
}
