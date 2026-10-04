package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func publicationFixture(t *testing.T, d *store.DB, r *store.ArchiveTasks, message string, kind domain.ArchiveInputKind) (*domain.ArchiveIntakeReceipt, store.ArchiveWorkFence, store.ArchiveExtraction) {
	t.Helper()
	ctx := context.Background()
	a := archiveActor(t, d)
	m := domain.ArchiveIntakeMetadata{ArchiveSourceIdentity: domain.ArchiveSourceIdentity{Kind: kind, Channel: "test", Account: "a", Conversation: "c", Sender: "s", ProviderMessageID: message}, OriginalName: "note.txt"}
	rec, e := r.AcceptIntake(ctx, a, "home-1", 1, m, domain.ArchiveBlobRef{HouseholdID: "home-1", SHA256: strings.Repeat("a", 64), SizeBytes: 1, MIME: "text/plain", StorageVersion: 1})
	require.NoError(t, e)
	task, e := r.ClaimKinds(ctx, "home-1", time.Minute, []string{"document.extract"})
	require.NoError(t, e)
	require.Equal(t, rec.TaskID, task.ID)
	f := store.ArchiveWorkFence{HouseholdID: "home-1", TaskID: task.ID, Token: task.LeaseToken, StateVersion: task.StateVersion}
	x, next, e := r.BeginArchiveExtraction(ctx, f, "fp1")
	require.NoError(t, e)
	f.StateVersion = next.StateVersion
	return rec, f, x
}
func publicationBundle(t *testing.T, value string) store.ArchivePublicationBundle {
	t.Helper()
	text := "体积" + value + " ml"
	parsed, e := archive.Parse(context.Background(), archive.ParseSource{Reader: strings.NewReader(text), Size: int64(len(text)), SHA256: strings.Repeat("a", 64), FormatHint: "text"}, archive.DefaultParseLimits())
	require.NoError(t, e)
	p, e := archive.PlanChunks(parsed)
	require.NoError(t, e)
	c := p.Chunks[0]
	v := c.Owned[0]
	x := archive.Extraction{Labels: []string{}, Domain: "health", DocumentType: "record", Subjects: []archive.LocalSubject{}, Fields: []archive.ExtractedField{{ID: "f", Name: "volume", Raw: value, SubjectRefs: []string{}, Dimensions: []archive.LiteralDimension{}, Normalizations: []archive.Normalization{{Kind: "decimal", Value: value}}, Evidence: []archive.EvidenceReference{{ViewID: v.ViewID, Start: 2, End: 2 + len(value), Quote: value}}}}, Events: []archive.EventProposal{{ID: "e", Type: "measurement", FieldRefs: []string{"f"}, SubjectRefs: []string{}, SectionEvidence: []archive.EvidenceReference{{ViewID: v.ViewID, Start: 0, End: len([]rune(text)), Quote: text}}}}, Coverage: []archive.UnitCoverage{{ViewID: v.ViewID, Disposition: "extracted"}}, Issues: []archive.ExtractionIssue{}}
	b, e := json.Marshal(x)
	require.NoError(t, e)
	ch, e := archive.ValidateChunk(p, c, b)
	require.NoError(t, e)
	u, e := archive.MergeChunks(p, []archive.ChunkUnderstanding{ch})
	require.NoError(t, e)
	return store.ArchivePublicationBundle{Parsed: parsed, Plan: p, Understanding: u}
}
func saveBundle(t *testing.T, r *store.ArchiveTasks, f store.ArchiveWorkFence, b store.ArchivePublicationBundle) store.ArchiveWorkFence {
	t.Helper()
	body, e := json.Marshal(b)
	require.NoError(t, e)
	next, e := r.SaveProductAndCheckpoint(context.Background(), f, store.ArchiveProduct{Key: "publication", Fingerprint: "fp1", Body: body})
	require.NoError(t, e)
	f.StateVersion = next.StateVersion
	return f
}
func TestArchivePublishAtomicAndSameFileSecondSource(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	rec, f, x := publicationFixture(t, d, r, "one", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, publicationBundle(t, "70"))
	_, e := d.SQL().Exec(`CREATE TRIGGER fail_outbox BEFORE INSERT ON archive_outbox BEGIN SELECT RAISE(ABORT,'crash at publication'); END`)
	require.NoError(t, e)
	_, e = r.PublishExtraction(ctx, f, x.ID)
	require.Error(t, e)
	var n int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_observations`).Scan(&n))
	require.Zero(t, n)
	_, e = d.SQL().Exec(`DROP TRIGGER fail_outbox`)
	require.NoError(t, e)
	done, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	require.Equal(t, domain.ArchiveTaskSucceeded, done.Status)
	second, f, x := publicationFixture(t, d, r, "two", domain.ArchiveInputFile)
	require.Equal(t, rec.DocumentID, second.DocumentID)
	f = saveBundle(t, r, f, publicationBundle(t, "70"))
	_, e = r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_subject_events`).Scan(&n))
	require.Equal(t, 1, n)
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_observations`).Scan(&n))
	require.Equal(t, 1, n)
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_outbox WHERE status='pending'`).Scan(&n))
	require.Equal(t, 2, n)
}
func TestArchivePrivateAndHumanValueSurviveReextract(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	rec, f, x := publicationFixture(t, d, r, "one", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, publicationBundle(t, "70"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	var obs string
	require.NoError(t, d.SQL().QueryRow(`SELECT id FROM archive_observations`).Scan(&obs))
	revision, e := r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, 2, store.ArchiveRevision{Kind: "value", ObservationID: obs, Value: "75", Reason: "measured correction"})
	require.NoError(t, e)
	require.EqualValues(t, 3, revision)
	revision, e = r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, 3, store.ArchiveRevision{Kind: "private", ObservationID: obs, Reason: "personal"})
	require.NoError(t, e)
	require.EqualValues(t, 4, revision)
	_, f, x = publicationFixture(t, d, r, "two", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, publicationBundle(t, "80"))
	_, e = r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	var restricted bool
	require.NoError(t, d.SQL().QueryRow(`SELECT original_restricted FROM archive_documents WHERE id=?`, rec.DocumentID).Scan(&restricted))
	require.True(t, restricted)
	var body, kind string
	require.NoError(t, d.SQL().QueryRow(`SELECT r.body_json,r.assertion_kind FROM archive_documents d JOIN archive_publication_observations p ON p.publication_id=d.active_publication_id JOIN archive_observation_revisions r ON r.observation_id=p.observation_id AND r.revision=p.revision WHERE d.id=?`, rec.DocumentID).Scan(&body, &kind))
	require.Equal(t, "user", kind)
	require.Contains(t, body, "75")
}
func TestArchiveTypedClarificationRejectsStaleAndIngestOnly(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	_, f, x := publicationFixture(t, d, r, "one", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, publicationBundle(t, "70"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	_, f, x = publicationFixture(t, d, r, "two", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, publicationBundle(t, "100"))
	waiting, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	require.Equal(t, domain.ArchiveTaskWaitingInput, waiting.Status)
	q, e := r.ArchiveQuestion(ctx, a, "home-1", f.TaskID)
	require.NoError(t, e)
	require.NotEmpty(t, q.ID)
	_, _, e = r.ResolveWaiting(ctx, a, "home-1", f.TaskID, waiting.Version, "generic", json.RawMessage(`{}`))
	require.ErrorIs(t, e, domain.ErrConflict)
	only := newArchiveActor(t, d, "ingest-only", []string{"archive.ingest"}, []string{"home-1"})
	_, e = r.ArchiveQuestion(ctx, only, "home-1", f.TaskID)
	require.Error(t, e)
	answer := store.ArchiveAnswer{QuestionID: q.ID, QuestionVersion: q.Version, WaitingTaskVersion: q.WaitingTaskVersion, ExtractionID: q.ExtractionID, DocumentRevision: q.DocumentRevision, ResolutionKey: "answer1", Action: "keep_current"}
	bad := answer
	bad.QuestionVersion++
	_, e = r.AnswerArchiveQuestion(ctx, a, "home-1", f.TaskID, bad)
	require.ErrorIs(t, e, domain.ErrConflict)
	_, e = r.AnswerArchiveQuestion(ctx, only, "home-1", f.TaskID, answer)
	require.Error(t, e)
	done, e := r.AnswerArchiveQuestion(ctx, a, "home-1", f.TaskID, answer)
	require.NoError(t, e)
	require.Equal(t, domain.ArchiveTaskSucceeded, done.Status)
	require.Empty(t, done.LeaseToken)
	replay, e := r.AnswerArchiveQuestion(ctx, a, "home-1", f.TaskID, answer)
	require.NoError(t, e)
	require.Equal(t, done.ID, replay.ID)
	changed := answer
	changed.Action = "select_subject"
	_, e = r.AnswerArchiveQuestion(ctx, a, "home-1", f.TaskID, changed)
	require.ErrorIs(t, e, domain.ErrConflict)
}
func TestArchiveConcurrentSourceCanRebaseWithoutRepeatingInference(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	_, f1, x1 := publicationFixture(t, d, r, "one", domain.ArchiveInputFile)
	f1 = saveBundle(t, r, f1, publicationBundle(t, "70"))
	_, f2, x2 := publicationFixture(t, d, r, "two", domain.ArchiveInputFile)
	f2 = saveBundle(t, r, f2, publicationBundle(t, "70"))
	_, e := r.PublishExtraction(ctx, f1, x1.ID)
	require.NoError(t, e)
	_, e = r.PublishExtraction(ctx, f2, x2.ID)
	require.ErrorIs(t, e, domain.ErrConflict)
	x3, next, e := r.RebaseArchiveExtraction(ctx, f2, "fp1")
	require.NoError(t, e)
	require.NotEqual(t, x2.ID, x3.ID)
	f2.StateVersion = next.StateVersion
	_, e = r.PublishExtraction(ctx, f2, x3.ID)
	require.NoError(t, e)
}
func TestArchiveSubregionPrivacyKeepsDocumentProjectionAvailable(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	rec, f, x := publicationFixture(t, d, r, "one", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, publicationBundle(t, "70"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	var obs string
	require.NoError(t, d.SQL().QueryRow(`SELECT id FROM archive_observations`).Scan(&obs))
	_, e = r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, 2, store.ArchiveRevision{Kind: "private", ObservationID: obs, Reason: "private region"})
	require.NoError(t, e)
	var visible, restricted bool
	require.NoError(t, d.SQL().QueryRow(`SELECT tv_visible,original_restricted FROM archive_documents WHERE id=?`, rec.DocumentID).Scan(&visible, &restricted))
	require.True(t, visible)
	require.True(t, restricted)
}
func TestArchivePartialPublishesCompleteIndependentEvidence(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	_, f, x := publicationFixture(t, d, r, "one", domain.ArchiveInputFile)
	b := publicationBundle(t, "70")
	b.Parsed.Status = archive.ParsePartial
	b.Parsed.Coverage.Complete = false
	p, e := archive.PlanChunks(b.Parsed)
	require.NoError(t, e)
	raw, e := json.Marshal(b.Understanding.Chunks[0].Result)
	require.NoError(t, e)
	// Plan view IDs carry the parse digest; rebuild the explicitly selected refs.
	var extracted archive.Extraction
	require.NoError(t, json.Unmarshal(raw, &extracted))
	v := p.Chunks[0].Owned[0].ViewID
	extracted.Fields[0].Evidence[0].ViewID = v
	extracted.Events[0].SectionEvidence[0].ViewID = v
	extracted.Coverage[0].ViewID = v
	raw, _ = json.Marshal(extracted)
	chunk, e := archive.ValidateChunk(p, p.Chunks[0], raw)
	require.NoError(t, e)
	u, e := archive.MergeChunks(p, []archive.ChunkUnderstanding{chunk})
	require.NoError(t, e)
	b.Plan = p
	b.Understanding = u
	f = saveBundle(t, r, f, b)
	done, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	require.Equal(t, domain.ArchiveTaskSucceeded, done.Status)
	var n int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_observations`).Scan(&n))
	require.Equal(t, 1, n)
	require.Contains(t, string(done.ResultJSON), "partial")
}
func TestArchiveSecondSourceReusesPublishedProductsWithoutTaskTransfer(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	first, f, x := publicationFixture(t, d, r, "one", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, publicationBundle(t, "70"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	second, f, x := publicationFixture(t, d, r, "two", domain.ArchiveInputFile)
	require.NotEqual(t, first.TaskID, second.TaskID)
	products, e := r.ArchiveProducts(ctx, f, "fp1")
	require.NoError(t, e)
	require.Len(t, products, 1)
	done, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	require.Equal(t, second.TaskID, done.ID)
}
func namedPublicationBundle(t *testing.T, name string) store.ArchivePublicationBundle {
	t.Helper()
	text := name + " 70 ml"
	parsed, e := archive.Parse(context.Background(), archive.ParseSource{Reader: strings.NewReader(text), Size: int64(len(text)), SHA256: strings.Repeat("a", 64), FormatHint: "text"}, archive.DefaultParseLimits())
	require.NoError(t, e)
	p, e := archive.PlanChunks(parsed)
	require.NoError(t, e)
	c := p.Chunks[0]
	v := c.Owned[0]
	n := len([]rune(name))
	x := publicationBundle(t, "70").Understanding.Chunks[0].Result
	x.Subjects = []archive.LocalSubject{{ID: "p", Name: name, Role: "patient", Ambiguous: name == "妈妈", Evidence: []archive.EvidenceReference{{ViewID: v.ViewID, Start: 0, End: n, Quote: name}}}}
	x.Fields[0].SubjectRefs = []string{"p"}
	x.Fields[0].Evidence = []archive.EvidenceReference{{ViewID: v.ViewID, Start: n + 1, End: n + 3, Quote: "70"}}
	x.Events[0].SubjectRefs = []string{"p"}
	x.Events[0].SectionEvidence = []archive.EvidenceReference{{ViewID: v.ViewID, Start: 0, End: n + 6, Quote: text}}
	x.Coverage[0].ViewID = v.ViewID
	raw, _ := json.Marshal(x)
	ch, e := archive.ValidateChunk(p, c, raw)
	require.NoError(t, e)
	u, e := archive.MergeChunks(p, []archive.ChunkUnderstanding{ch})
	require.NoError(t, e)
	return store.ArchivePublicationBundle{Parsed: parsed, Plan: p, Understanding: u}
}
func TestArchiveClarificationCanCreateAssertedSubject(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	_, f, x := publicationFixture(t, d, r, "one", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, namedPublicationBundle(t, "妈妈"))
	waiting, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	require.Equal(t, domain.ArchiveTaskWaitingInput, waiting.Status)
	q, e := r.ArchiveQuestion(ctx, a, "home-1", f.TaskID)
	require.NoError(t, e)
	answer := store.ArchiveAnswer{QuestionID: q.ID, QuestionVersion: q.Version, WaitingTaskVersion: q.WaitingTaskVersion, ExtractionID: q.ExtractionID, DocumentRevision: q.DocumentRevision, ResolutionKey: "newperson", Action: "create_subject", Name: "林清"}
	next, e := r.AnswerArchiveQuestion(ctx, a, "home-1", f.TaskID, answer)
	require.NoError(t, e)
	require.Equal(t, domain.ArchiveTaskQueued, next.Status)
	worker, e := r.ClaimKinds(ctx, "home-1", time.Minute, []string{"document.extract"})
	require.NoError(t, e)
	f.Token = worker.LeaseToken
	f.StateVersion = worker.StateVersion
	x, next, e = r.RebaseArchiveExtraction(ctx, f, "fp1")
	require.NoError(t, e)
	f.StateVersion = next.StateVersion
	done, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	require.Equal(t, domain.ArchiveTaskSucceeded, done.Status)
}
func TestArchiveSamePhysicalNamedSubjectWithoutDOBDoesNotAskAgain(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	_, f, x := publicationFixture(t, d, r, "one", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, namedPublicationBundle(t, "Alice"))
	done, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	require.Equal(t, domain.ArchiveTaskSucceeded, done.Status)
	_, f, x = publicationFixture(t, d, r, "two", domain.ArchiveInputFile)
	_, e = r.ArchiveProducts(ctx, f, "fp1")
	require.NoError(t, e)
	done, e = r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	require.Equal(t, domain.ArchiveTaskSucceeded, done.Status)
}
func TestArchiveCurrentViewExcludesPrivateAndHistoricalObservations(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	rec, f, x := publicationFixture(t, d, r, "one", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, publicationBundle(t, "70"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	all, e := r.CurrentArchiveObservations(ctx, a, "home-1", rec.DocumentID, true)
	require.NoError(t, e)
	require.Len(t, all, 1)
	_, e = r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, 2, store.ArchiveRevision{Kind: "private", ObservationID: all[0].ID, Reason: "private"})
	require.NoError(t, e)
	all, e = r.CurrentArchiveObservations(ctx, a, "home-1", rec.DocumentID, true)
	require.NoError(t, e)
	require.Empty(t, all)
	all, e = r.CurrentArchiveObservations(ctx, a, "home-1", rec.DocumentID, false)
	require.NoError(t, e)
	require.Len(t, all, 1)
}
func TestArchivePrivateFieldKeepsIndependentFieldButBlocksOriginal(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	rec, f, x := publicationFixture(t, d, r, "one", domain.ArchiveInputFile)
	b := publicationBundle(t, "70")
	text := "体积70 ml 体积80 ml"
	parsed, e := archive.Parse(ctx, archive.ParseSource{Reader: strings.NewReader(text), Size: int64(len(text)), SHA256: strings.Repeat("a", 64), FormatHint: "text"}, archive.DefaultParseLimits())
	require.NoError(t, e)
	p, e := archive.PlanChunks(parsed)
	require.NoError(t, e)
	v := p.Chunks[0].Owned[0].ViewID
	out := b.Understanding.Chunks[0].Result
	out.Fields[0].Evidence[0].ViewID = v
	second := out.Fields[0]
	second.ID = "f2"
	second.Raw = "80"
	second.Evidence = []archive.EvidenceReference{{ViewID: v, Start: 10, End: 12, Quote: "80"}}
	second.Normalizations = []archive.Normalization{{Kind: "decimal", Value: "80"}}
	out.Fields = append(out.Fields, second)
	out.Events[0].FieldRefs = append(out.Events[0].FieldRefs, "f2")
	out.Events[0].SectionEvidence = []archive.EvidenceReference{{ViewID: v, Start: 0, End: 15, Quote: text}}
	out.Coverage[0].ViewID = v
	raw, _ := json.Marshal(out)
	ch, e := archive.ValidateChunk(p, p.Chunks[0], raw)
	require.NoError(t, e)
	u, e := archive.MergeChunks(p, []archive.ChunkUnderstanding{ch})
	require.NoError(t, e)
	b = store.ArchivePublicationBundle{Parsed: parsed, Plan: p, Understanding: u}
	f = saveBundle(t, r, f, b)
	_, e = r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	items, e := r.CurrentArchiveObservations(ctx, a, "home-1", rec.DocumentID, true)
	require.NoError(t, e)
	require.Len(t, items, 2)
	private := ""
	for _, item := range items {
		var body struct{ Field archive.PublicationField }
		require.NoError(t, json.Unmarshal(item.Body, &body))
		if body.Field.Raw == "70" {
			private = item.ID
		}
	}
	_, e = r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, 2, store.ArchiveRevision{Kind: "private", ObservationID: private, Reason: "one field"})
	require.NoError(t, e)
	items, e = r.CurrentArchiveObservations(ctx, a, "home-1", rec.DocumentID, true)
	require.NoError(t, e)
	require.Len(t, items, 1)
	require.Contains(t, string(items[0].Body), "80")
	_, e = r.ArchiveOriginalReference(ctx, a, "home-1", rec.DocumentID, true)
	require.ErrorIs(t, e, domain.ErrNotFound)
}

func TestArchivePrivateDimensionDependencyCannotLeakThroughPublicValue(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	rec, f, x := publicationFixture(t, d, r, "one", domain.ArchiveInputFile)
	b := publicationBundle(t, "70")
	text := "体积70 ml 体积80 ml"
	parsed, e := archive.Parse(ctx, archive.ParseSource{Reader: strings.NewReader(text), Size: int64(len(text)), SHA256: strings.Repeat("a", 64), FormatHint: "text"}, archive.DefaultParseLimits())
	require.NoError(t, e)
	p, e := archive.PlanChunks(parsed)
	require.NoError(t, e)
	v := p.Chunks[0].Owned[0].ViewID
	out := b.Understanding.Chunks[0].Result
	out.Fields[0].Evidence[0].ViewID = v
	second := out.Fields[0]
	second.ID = "f2"
	second.Dimensions = []archive.LiteralDimension{{Kind: "unit", Raw: "70", Evidence: []archive.EvidenceReference{{ViewID: v, Start: 2, End: 4, Quote: "70"}}}}
	second.Raw = "80"
	second.Evidence = []archive.EvidenceReference{{ViewID: v, Start: 10, End: 12, Quote: "80"}}
	second.Normalizations = []archive.Normalization{{Kind: "decimal", Value: "80"}}
	out.Fields = append(out.Fields, second)
	out.Events[0].FieldRefs = append(out.Events[0].FieldRefs, "f2")
	out.Events[0].SectionEvidence = []archive.EvidenceReference{{ViewID: v, Start: 0, End: 15, Quote: text}}
	out.Coverage[0].ViewID = v
	raw, _ := json.Marshal(out)
	ch, e := archive.ValidateChunk(p, p.Chunks[0], raw)
	require.NoError(t, e)
	u, e := archive.MergeChunks(p, []archive.ChunkUnderstanding{ch})
	require.NoError(t, e)
	b = store.ArchivePublicationBundle{Parsed: parsed, Plan: p, Understanding: u}
	f = saveBundle(t, r, f, b)
	_, e = r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	items, e := r.CurrentArchiveObservations(ctx, a, "home-1", rec.DocumentID, true)
	require.NoError(t, e)
	require.Len(t, items, 2)
	private := ""
	for _, item := range items {
		var body struct{ Field archive.PublicationField }
		require.NoError(t, json.Unmarshal(item.Body, &body))
		if body.Field.Raw == "70" {
			private = item.ID
		}
	}
	_, e = r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, 2, store.ArchiveRevision{Kind: "private", ObservationID: private, Reason: "one field"})
	require.NoError(t, e)
	items, e = r.CurrentArchiveObservations(ctx, a, "home-1", rec.DocumentID, true)
	require.NoError(t, e)
	require.Empty(t, items)
	_, e = r.ArchiveOriginalReference(ctx, a, "home-1", rec.DocumentID, true)
	require.ErrorIs(t, e, domain.ErrNotFound)
}

type publicationLiteral struct{ Name, Value string }

func literalBundle(t *testing.T, subject string, fields []publicationLiteral, currency string) store.ArchivePublicationBundle {
	t.Helper()
	parts := []string{}
	if subject != "" {
		parts = append(parts, subject)
	}
	for _, f := range fields {
		parts = append(parts, f.Value)
	}
	if currency != "" {
		parts = append(parts, currency)
	}
	text := strings.Join(parts, " | ")
	parsed, e := archive.Parse(context.Background(), archive.ParseSource{Reader: strings.NewReader(text), Size: int64(len(text)), SHA256: strings.Repeat("a", 64), FormatHint: "text"}, archive.DefaultParseLimits())
	require.NoError(t, e)
	p, e := archive.PlanChunks(parsed)
	require.NoError(t, e)
	c := p.Chunks[0]
	v := c.Owned[0].ViewID
	refs := func(raw string) []archive.EvidenceReference {
		i := strings.Index(text, raw)
		start := len([]rune(text[:i]))
		return []archive.EvidenceReference{{ViewID: v, Start: start, End: start + len([]rune(raw)), Quote: raw}}
	}
	x := archive.Extraction{Labels: []string{}, Domain: "finance", DocumentType: "receipt", Subjects: []archive.LocalSubject{}, Fields: []archive.ExtractedField{}, Events: []archive.EventProposal{}, Coverage: []archive.UnitCoverage{{ViewID: v, Disposition: "extracted"}}, Issues: []archive.ExtractionIssue{}}
	owners := []string{}
	if subject != "" {
		owners = []string{"p"}
		x.Subjects = []archive.LocalSubject{{ID: "p", Name: subject, Role: "patient", Evidence: refs(subject)}}
	}
	ids := []string{}
	for i, f := range fields {
		id := fmt.Sprintf("f%d", i)
		ids = append(ids, id)
		field := archive.ExtractedField{ID: id, Name: f.Name, Raw: f.Value, Evidence: refs(f.Value), SubjectRefs: owners, Normalizations: []archive.Normalization{}, Dimensions: []archive.LiteralDimension{}}
		if f.Name == "amount" {
			field.Normalizations = []archive.Normalization{{Kind: "decimal", Value: f.Value}}
			if currency != "" {
				field.Dimensions = []archive.LiteralDimension{{Kind: "currency", Raw: currency, Evidence: refs(currency)}}
			}
		}
		x.Fields = append(x.Fields, field)
	}
	x.Events = []archive.EventProposal{{ID: "event", Type: "payment", FieldRefs: ids, SubjectRefs: owners, SectionEvidence: []archive.EvidenceReference{{ViewID: v, Start: 0, End: len([]rune(text)), Quote: text}}}}
	raw, _ := json.Marshal(x)
	ch, e := archive.ValidateChunk(p, c, raw)
	require.NoError(t, e)
	u, e := archive.MergeChunks(p, []archive.ChunkUnderstanding{ch})
	require.NoError(t, e)
	return store.ArchivePublicationBundle{Parsed: parsed, Plan: p, Understanding: u}
}
func TestArchiveProtectedIdentityFullPublicationKeepsLeadingZeros(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	for _, name := range []string{"Alice", "Alias"} {
		_, f, x := publicationFixture(t, d, r, name, domain.ArchiveInputText)
		f = saveBundle(t, r, f, literalBundle(t, name, []publicationLiteral{{"national_id", "00012"}, {"identity_issuer", "SG"}, {"diagnosis", "well"}}, ""))
		done, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
		require.Equal(t, domain.ArchiveTaskSucceeded, done.Status)
	}
	var n int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_entities`).Scan(&n))
	require.Equal(t, 1, n)
	var value string
	require.NoError(t, d.SQL().QueryRow(`SELECT value FROM archive_protected_identity_facts LIMIT 1`).Scan(&value))
	require.Equal(t, "00012", value)
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_observation_revisions WHERE body_json LIKE '%00012%'`).Scan(&n))
	require.Zero(t, n)
}
func TestArchiveTransactionPublicationDeduplicatesAmountAndKeepsConflict(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	var docs []string
	for i, value := range []string{"70", "70", "80"} {
		rec, f, x := publicationFixture(t, d, r, fmt.Sprint(i), domain.ArchiveInputText)
		docs = append(docs, rec.DocumentID)
		f = saveBundle(t, r, f, literalBundle(t, "", []publicationLiteral{{"transaction_id", "0012"}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", value}}, "SGD"))
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
	}
	var n int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_observations WHERE slot_key='supported-transaction-amount/1'`).Scan(&n))
	require.Equal(t, 1, n)
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_observation_alternatives`).Scan(&n))
	require.Equal(t, 1, n)
	for _, doc := range docs {
		items, e := r.CurrentArchiveObservations(ctx, a, "home-1", doc, false)
		require.NoError(t, e)
		for _, item := range items {
			var body struct{ Field archive.PublicationField }
			require.NoError(t, json.Unmarshal(item.Body, &body))
			require.NotEqual(t, "amount", body.Field.Name)
		}
	}
}
func TestArchivePrivateConflictingProofDoesNotSuppressPublicAmount(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	var docs []string
	for i, value := range []string{"70", "70", "80"} {
		rec, f, x := publicationFixture(t, d, r, fmt.Sprint(i), domain.ArchiveInputText)
		docs = append(docs, rec.DocumentID)
		f = saveBundle(t, r, f, literalBundle(t, "", []publicationLiteral{{"transaction_id", "0012"}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", value}}, "SGD"))
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
	}
	_, e := r.ReviseArchive(ctx, a, "home-1", docs[2], 2, store.ArchiveRevision{Kind: "private", Reason: "private conflicting proof"})
	require.NoError(t, e)
	for _, doc := range docs[:2] {
		items, e := r.CurrentArchiveObservations(ctx, a, "home-1", doc, true)
		require.NoError(t, e)
		found := false
		for _, item := range items {
			var body struct{ Field archive.PublicationField }
			require.NoError(t, json.Unmarshal(item.Body, &body))
			if body.Field.Name == "amount" {
				found = true
				require.Equal(t, "70", body.Field.Raw)
			}
		}
		require.True(t, found)
	}
}
func TestArchiveFieldReassignmentDoesNotSilentlyReconfirmOldSubject(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	rec, f, x := publicationFixture(t, d, r, "alice", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, namedPublicationBundle(t, "Alice"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	_, fb, xb := publicationFixture(t, d, r, "bob", domain.ArchiveInputText)
	fb = saveBundle(t, r, fb, namedPublicationBundle(t, "Bob"))
	_, e = r.PublishExtraction(ctx, fb, xb.ID)
	require.NoError(t, e)
	var target, obs string
	require.NoError(t, d.SQL().QueryRow(`SELECT entity_id FROM archive_entity_attributes WHERE value='Bob' LIMIT 1`).Scan(&target))
	require.NoError(t, d.SQL().QueryRow(`SELECT o.id FROM archive_observations o JOIN archive_subject_events e ON e.id=o.event_id WHERE e.document_id=?`, rec.DocumentID).Scan(&obs))
	_, e = r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, 2, store.ArchiveRevision{Kind: "reassign", ObservationID: obs, EntityID: target, Reason: "only this observation"})
	require.NoError(t, e)
	_, f, x = publicationFixture(t, d, r, "again", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, namedPublicationBundle(t, "Alice"))
	done, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	require.Equal(t, domain.ArchiveTaskWaitingInput, done.Status)
	items, e := r.CurrentArchiveObservations(ctx, a, "home-1", rec.DocumentID, false)
	require.NoError(t, e)
	require.Len(t, items, 1)
	require.Contains(t, string(items[0].Body), target)
}
func TestArchiveUnrelatedPrivateRegionDoesNotHidePublicTransactionConflict(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	var docs []string
	for i, value := range []string{"70", "70", "80"} {
		rec, f, x := publicationFixture(t, d, r, fmt.Sprint(i), domain.ArchiveInputText)
		docs = append(docs, rec.DocumentID)
		f = saveBundle(t, r, f, literalBundle(t, "", []publicationLiteral{{"transaction_id", "0012"}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", value}, {"note", "private note"}}, "SGD"))
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
	}
	items, e := r.CurrentArchiveObservations(ctx, a, "home-1", docs[2], false)
	require.NoError(t, e)
	note := ""
	for _, i := range items {
		var b struct{ Field archive.PublicationField }
		require.NoError(t, json.Unmarshal(i.Body, &b))
		if b.Field.Name == "note" {
			note = i.ID
		}
	}
	require.NotEmpty(t, note)
	_, e = r.ReviseArchive(ctx, a, "home-1", docs[2], 2, store.ArchiveRevision{Kind: "private", ObservationID: note, Reason: "unrelated note"})
	require.NoError(t, e)
	items, e = r.CurrentArchiveObservations(ctx, a, "home-1", docs[0], true)
	require.NoError(t, e)
	for _, i := range items {
		var b struct{ Field archive.PublicationField }
		require.NoError(t, json.Unmarshal(i.Body, &b))
		require.NotEqual(t, "amount", b.Field.Name)
	}
}

// Candidate references, like observation references, survive a partial extraction.
func TestArchiveConflictReferencesSurvivePartialAndExpireOnReplacement(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	first, f, x := publicationFixture(t, d, r, "first", domain.ArchiveInputText)
	f = saveBundle(t, r, f, literalBundle(t, "", []publicationLiteral{{"transaction_id", "0012"}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", "70"}}, "SGD"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	second, f, x := publicationFixture(t, d, r, "second", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, literalBundle(t, "", []publicationLiteral{{"transaction_id", "0012"}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", "80"}}, "SGD"))
	_, e = r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	assertAmount := func(want bool) {
		t.Helper()
		items, e := r.CurrentArchiveObservations(ctx, a, "home-1", first.DocumentID, true)
		require.NoError(t, e)
		found := false
		for _, i := range items {
			var b struct{ Field archive.PublicationField }
			require.NoError(t, json.Unmarshal(i.Body, &b))
			found = found || b.Field.Name == "amount"
		}
		require.Equal(t, want, found)
	}
	assertAmount(false)

	// The same physical source is re-extracted with uncertain coverage. No new
	// amount is confirmed, so the old candidate remains current under a NEW x.
	_, f, partialX := publicationFixture(t, d, r, "partial", domain.ArchiveInputFile)
	b := literalBundle(t, "", []publicationLiteral{{"transaction_id", "0012"}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", "80"}}, "SGD")
	result := b.Understanding.Chunks[0].Result
	result.Issues = []archive.ExtractionIssue{{Code: "uncertain", ViewRefs: []string{b.Plan.Chunks[0].Owned[0].ViewID}}}
	raw, e := json.Marshal(result)
	require.NoError(t, e)
	ch, e := archive.ValidateChunk(b.Plan, b.Plan.Chunks[0], raw)
	require.NoError(t, e)
	b.Understanding, e = archive.MergeChunks(b.Plan, []archive.ChunkUnderstanding{ch})
	require.NoError(t, e)
	f = saveBundle(t, r, f, b)
	_, e = r.PublishExtraction(ctx, f, partialX.ID)
	require.NoError(t, e)
	require.NotEqual(t, x.ID, partialX.ID)
	var activeExtraction string
	require.NoError(t, d.SQL().QueryRow(`SELECT p.extraction_id FROM archive_documents d JOIN archive_publications p ON p.id=d.active_publication_id WHERE d.id=?`, second.DocumentID).Scan(&activeExtraction))
	require.Equal(t, partialX.ID, activeExtraction)
	assertAmount(false)
	// A subsequent complete corrected extraction removes only its current
	// conflict reference, retaining immutable old alternative evidence.
	_, f, fixedX := publicationFixture(t, d, r, "corrected", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, literalBundle(t, "", []publicationLiteral{{"transaction_id", "0012"}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", "70"}}, "SGD"))
	_, e = r.PublishExtraction(ctx, f, fixedX.ID)
	require.NoError(t, e)
	assertAmount(true)
	var n int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_observation_alternatives`).Scan(&n))
	require.Equal(t, 1, n)
}

func TestArchiveCurrentViewDoesNotTreatUnresolvedLinkAsConfirmed(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	rec, f, x := publicationFixture(t, d, r, "named", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, namedPublicationBundle(t, "Alice"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	items, e := r.CurrentArchiveObservations(ctx, a, "home-1", rec.DocumentID, false)
	require.NoError(t, e)
	require.Len(t, items, 1)
	_, e = d.SQL().Exec(`UPDATE archive_entity_links SET status='needs_resolution',version=version+1 WHERE document_id=?`, rec.DocumentID)
	require.NoError(t, e)
	items, e = r.CurrentArchiveObservations(ctx, a, "home-1", rec.DocumentID, false)
	require.NoError(t, e)
	require.Empty(t, items)
	var n int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_observation_revisions`).Scan(&n))
	require.Equal(t, 1, n)
}

func TestArchivePrivateSourceBumpsPublicationFenceAndOutbox(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	rec, f, x := publicationFixture(t, d, r, "public", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, publicationBundle(t, "70"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	m := domain.ArchiveIntakeMetadata{ArchiveSourceIdentity: domain.ArchiveSourceIdentity{Kind: domain.ArchiveInputFile, Channel: "test", Account: "a", Conversation: "c", Sender: "s", ProviderMessageID: "private-source"}, OriginalName: "note.txt", VisibilityIntent: "private"}
	_, e = r.AcceptIntake(ctx, a, "home-1", 1, m, domain.ArchiveBlobRef{HouseholdID: "home-1", SHA256: strings.Repeat("a", 64), SizeBytes: 1, MIME: "text/plain", StorageVersion: 1})
	require.NoError(t, e)
	var revision int64
	require.NoError(t, d.SQL().QueryRow(`SELECT document_revision FROM archive_documents WHERE id=?`, rec.DocumentID).Scan(&revision))
	require.EqualValues(t, 3, revision)
	var n int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_outbox WHERE document_id=? AND revision=3 AND status='pending'`, rec.DocumentID).Scan(&n))
	require.Equal(t, 1, n)
}

func TestArchiveManualRevisionOfSharedObservationUsesFreshRevision(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	var first, second string
	for i := 0; i < 2; i++ {
		rec, f, x := publicationFixture(t, d, r, fmt.Sprint(i), domain.ArchiveInputText)
		if i == 0 {
			first = rec.DocumentID
		} else {
			second = rec.DocumentID
		}
		f = saveBundle(t, r, f, literalBundle(t, "", []publicationLiteral{{"transaction_id", "0012"}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", "70"}}, "SGD"))
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
	}
	items, e := r.CurrentArchiveObservations(ctx, a, "home-1", first, false)
	require.NoError(t, e)
	var obs string
	for _, i := range items {
		var b struct{ Field archive.PublicationField }
		require.NoError(t, json.Unmarshal(i.Body, &b))
		if b.Field.Name == "amount" {
			obs = i.ID
		}
	}
	require.NotEmpty(t, obs)
	_, e = r.ReviseArchive(ctx, a, "home-1", first, 2, store.ArchiveRevision{Kind: "value", ObservationID: obs, Value: "71", Reason: "corrected assertion"})
	require.NoError(t, e)

	readAmount := func(doc string, public bool, want string, asserted bool) {
		t.Helper()
		items, e := r.CurrentArchiveObservations(ctx, a, "home-1", doc, public)
		require.NoError(t, e)
		found := false
		for _, i := range items {
			var b struct {
				Field         archive.PublicationField
				AssertedValue string
			}
			require.NoError(t, json.Unmarshal(i.Body, &b))
			if b.Field.Name == "amount" {
				found = true
				if asserted {
					require.Equal(t, want, b.AssertedValue)
					require.Equal(t, first, i.SourceDocumentID)
					require.Equal(t, "70", b.Field.Raw)
				} else {
					require.Equal(t, want, b.Field.Raw)
					require.Empty(t, b.AssertedValue)
				}
			}
		}
		require.True(t, found)
	}
	readAmount(first, true, "71", true)
	readAmount(second, true, "71", true)
	_, e = r.ReviseArchive(ctx, a, "home-1", first, 3, store.ArchiveRevision{Kind: "private", ObservationID: obs, Reason: "private correction"})
	require.NoError(t, e)
	readAmount(second, true, "70", false)
	readAmount(second, false, "71", true)
	third, f, x := publicationFixture(t, d, r, "third-proof", domain.ArchiveInputText)
	f = saveBundle(t, r, f, literalBundle(t, "", []publicationLiteral{{"transaction_id", "0012"}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", "70"}}, "SGD"))
	_, e = r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	readAmount(third.DocumentID, true, "70", false)

	_, e = r.ReviseArchive(ctx, a, "home-1", first, 4, store.ArchiveRevision{Kind: "withdraw", Reason: "withdraw correction source"})
	require.NoError(t, e)
	readAmount(second, false, "70", false)
}

func revalidatePublication(t *testing.T, b store.ArchivePublicationBundle, result archive.Extraction) store.ArchivePublicationBundle {
	t.Helper()
	raw, e := json.Marshal(result)
	require.NoError(t, e)
	ch, e := archive.ValidateChunk(b.Plan, b.Plan.Chunks[0], raw)
	require.NoError(t, e)
	b.Understanding, e = archive.MergeChunks(b.Plan, []archive.ChunkUnderstanding{ch})
	require.NoError(t, e)
	return b
}
func TestArchiveNewFieldKeepsEventAndSplitKeepsOldActive(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(fmt.Sprint(split), func(t *testing.T) {
			d := testutil.NewDB(t)
			r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
			ctx := context.Background()
			b := literalBundle(t, "", []publicationLiteral{{"volume", "70"}, {"temperature", "36"}}, "")
			initial := b
			if !split {
				var result archive.Extraction
				raw, _ := json.Marshal(b.Understanding.Chunks[0].Result)
				require.NoError(t, json.Unmarshal(raw, &result))
				result.Fields = result.Fields[:1]
				result.Events[0].FieldRefs = result.Events[0].FieldRefs[:1]
				initial = revalidatePublication(t, b, result)
			}
			rec, f, x := publicationFixture(t, d, r, "initial", domain.ArchiveInputFile)
			f = saveBundle(t, r, f, initial)
			_, e := r.PublishExtraction(ctx, f, x.ID)
			require.NoError(t, e)
			var publication, event string
			require.NoError(t, d.SQL().QueryRow(`SELECT active_publication_id FROM archive_documents WHERE id=?`, rec.DocumentID).Scan(&publication))
			require.NoError(t, d.SQL().QueryRow(`SELECT id FROM archive_subject_events WHERE document_id=?`, rec.DocumentID).Scan(&event))
			if split {
				var result archive.Extraction
				raw, _ := json.Marshal(b.Understanding.Chunks[0].Result)
				require.NoError(t, json.Unmarshal(raw, &result))
				result.Events = nil
				for i, field := range result.Fields {
					result.Events = append(result.Events, archive.EventProposal{ID: fmt.Sprint("event", i), Type: "measurement", FieldRefs: []string{field.ID}, SubjectRefs: []string{}, SectionEvidence: field.Evidence})
				}
				b = revalidatePublication(t, b, result)
			}
			_, f, x = publicationFixture(t, d, r, "again", domain.ArchiveInputFile)
			f = saveBundle(t, r, f, b)
			done, e := r.PublishExtraction(ctx, f, x.ID)
			require.NoError(t, e)
			if split {
				require.Equal(t, domain.ArchiveTaskWaitingInput, done.Status)
				var active string
				require.NoError(t, d.SQL().QueryRow(`SELECT active_publication_id FROM archive_documents WHERE id=?`, rec.DocumentID).Scan(&active))
				require.Equal(t, publication, active)
			} else {
				require.Equal(t, domain.ArchiveTaskSucceeded, done.Status)
			}
			var n int
			require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_subject_events WHERE document_id=?`, rec.DocumentID).Scan(&n))
			require.Equal(t, 1, n)
			require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_observations WHERE event_id=?`, event).Scan(&n))
			require.Equal(t, 2, n)
		})
	}
}
func TestArchiveIdenticalTextMessagesRemainTwoEvents(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	for _, message := range []string{"first", "second"} {
		_, f, x := publicationFixture(t, d, r, message, domain.ArchiveInputText)
		f = saveBundle(t, r, f, publicationBundle(t, "70"))
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
	}
	var n int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_subject_events`).Scan(&n))
	require.Equal(t, 2, n)
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_observations`).Scan(&n))
	require.Equal(t, 2, n)
}

func TestArchiveMultipleCompatibleIdentityEvidencePublishesTogether(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	_, f, x := publicationFixture(t, d, r, "ids", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, literalBundle(t, "Alice", []publicationLiteral{{"national_id", "0012"}, {"passport", "0034"}, {"identity_issuer", "Authority"}, {"diagnosis", "normal"}}, ""))
	done, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	require.Equal(t, domain.ArchiveTaskSucceeded, done.Status)
	var n int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_protected_identity_facts`).Scan(&n))
	require.Equal(t, 2, n)
}

func TestArchiveConcurrentTypedAnswersHaveOneWinner(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	_, f, x := publicationFixture(t, d, r, "question-race", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, namedPublicationBundle(t, "妈妈"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	q, e := r.ArchiveQuestion(ctx, a, "home-1", f.TaskID)
	require.NoError(t, e)
	require.Equal(t, "妈妈", q.SubjectName)
	require.Equal(t, "patient", q.SubjectRole)
	errors := make(chan error, 2)
	start := make(chan struct{})
	for _, name := range []string{"Alice", "Bob"} {
		go func(name string) {
			<-start
			_, err := r.AnswerArchiveQuestion(ctx, a, "home-1", f.TaskID, store.ArchiveAnswer{QuestionID: q.ID, QuestionVersion: q.Version, WaitingTaskVersion: q.WaitingTaskVersion, ExtractionID: q.ExtractionID, DocumentRevision: q.DocumentRevision, ResolutionKey: "same-answer-key", Action: "create_subject", Name: name})
			errors <- err
		}(name)
	}
	close(start)
	wins := 0
	for i := 0; i < 2; i++ {
		err := <-errors
		if err == nil {
			wins++
		} else {
			require.ErrorIs(t, err, domain.ErrConflict)
		}
	}
	require.Equal(t, 1, wins)
	var n int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_entities`).Scan(&n))
	require.Equal(t, 1, n)
}

func TestArchiveMultipleIdentitiesPointingAtDifferentPeopleRemainUnresolved(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	cases := []struct {
		name   string
		fields []publicationLiteral
	}{{"Alice", []publicationLiteral{{"national_id", "0012"}, {"identity_issuer", "Authority"}, {"diagnosis", "normal"}}}, {"Bob", []publicationLiteral{{"passport", "0034"}, {"identity_issuer", "Authority"}, {"diagnosis", "normal"}}}, {"Alice", []publicationLiteral{{"national_id", "0012"}, {"passport", "0034"}, {"identity_issuer", "Authority"}, {"diagnosis", "normal"}}}}
	for i, c := range cases {
		_, f, x := publicationFixture(t, d, r, fmt.Sprint("identity", i), domain.ArchiveInputText)
		f = saveBundle(t, r, f, literalBundle(t, c.name, c.fields, ""))
		done, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
		if i < 2 {
			require.Equal(t, domain.ArchiveTaskSucceeded, done.Status)
		} else {
			require.Equal(t, domain.ArchiveTaskWaitingInput, done.Status)
		}
	}
	var n int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_protected_identity_facts`).Scan(&n))
	require.Equal(t, 2, n)
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_entities`).Scan(&n))
	require.Equal(t, 2, n)
}

func TestArchiveStaleQuestionCanRefreshUsingCommittedProducts(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	rec, f, x := publicationFixture(t, d, r, "stale-question", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, namedPublicationBundle(t, "妈妈"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	old, e := r.ArchiveQuestion(ctx, a, "home-1", f.TaskID)
	require.NoError(t, e)
	_, e = r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, old.DocumentRevision, store.ArchiveRevision{Kind: "private", Reason: "private before answer"})
	require.NoError(t, e)
	n, e := r.RefreshStaleArchiveQuestions(ctx, "home-1", 32)
	require.NoError(t, e)
	require.Equal(t, 1, n)
	task, e := r.ClaimKinds(ctx, "home-1", time.Minute, []string{"document.extract"})
	require.NoError(t, e)
	require.Equal(t, f.TaskID, task.ID)
	f.Token = task.LeaseToken
	f.StateVersion = task.StateVersion
	x, task, e = r.RebaseArchiveExtraction(ctx, f, "fp1")
	require.NoError(t, e)
	f.StateVersion = task.StateVersion
	_, e = r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	q, e := r.ArchiveQuestion(ctx, a, "home-1", f.TaskID)
	require.NoError(t, e)
	require.NotEqual(t, old.ID, q.ID)
	require.Greater(t, q.Version, old.Version)
	_, e = r.AnswerArchiveQuestion(ctx, a, "home-1", f.TaskID, store.ArchiveAnswer{QuestionID: q.ID, QuestionVersion: q.Version, WaitingTaskVersion: q.WaitingTaskVersion, ExtractionID: q.ExtractionID, DocumentRevision: q.DocumentRevision, ResolutionKey: "fresh", Action: "keep_current"})
	require.NoError(t, e)
}
