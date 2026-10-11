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

func TestArchiveSpecRegressionTransactionCorrection(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	var doc string
	for _, v := range []string{"70", "80"} {
		rec, f, x := publicationFixture(t, d, r, v, domain.ArchiveInputFile)
		doc = rec.DocumentID
		f = saveBundle(t, r, f, literalBundle(t, "", []publicationLiteral{{"transaction_id", "0012"}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", v}}, "SGD"))
		done, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
		t.Log("publish", v, done.Status)
	}
	items, e := r.CurrentArchiveObservations(ctx, a, "home-1", doc, false)
	require.NoError(t, e)
	value := ""
	for _, i := range items {
		var b struct{ Field archive.PublicationField }
		require.NoError(t, json.Unmarshal(i.Body, &b))
		if b.Field.Name == "amount" {
			value = b.Field.Raw
		}
	}
	require.Equal(t, "80", value, "complete same-source OCR correction must supersede its old amount")
}
func TestArchiveSpecRegressionWithdrawConflictingSource(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	var docs []string
	for _, v := range []string{"70", "80"} {
		rec, f, x := publicationFixture(t, d, r, v, domain.ArchiveInputText)
		docs = append(docs, rec.DocumentID)
		f = saveBundle(t, r, f, literalBundle(t, "", []publicationLiteral{{"transaction_id", "0012"}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", v}}, "SGD"))
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
	}
	_, e := r.ReviseArchive(ctx, a, "home-1", docs[0], 2, store.ArchiveRevision{Kind: "withdraw", Reason: "withdraw invalid source"})
	require.NoError(t, e)
	items, e := r.CurrentArchiveObservations(ctx, a, "home-1", docs[1], true)
	require.NoError(t, e)
	value := ""
	for _, i := range items {
		var b struct{ Field archive.PublicationField }
		require.NoError(t, json.Unmarshal(i.Body, &b))
		if b.Field.Name == "amount" {
			value = b.Field.Raw
		}
	}
	require.Equal(t, "80", value, "sole active source must cease conflicting with withdrawn source")
}

func TestArchiveSpecRegressionIncompletePartition(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	b := literalBundle(t, "", []publicationLiteral{{"volume", "70"}, {"temperature", "36"}}, "")
	result := b.Understanding.Chunks[0].Result
	result.Events = nil
	for _, field := range result.Fields {
		result.Events = append(result.Events, archive.EventProposal{ID: field.ID, Type: "measurement", FieldRefs: []string{field.ID}, SubjectRefs: []string{}, SectionEvidence: field.Evidence})
	}
	b = revalidatePublication(t, b, result)
	rec, f, x := publicationFixture(t, d, r, "initial-partition", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, b)
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	var old string
	require.NoError(t, d.SQL().QueryRow(`SELECT active_publication_id FROM archive_documents WHERE id=?`, rec.DocumentID).Scan(&old))
	result.Fields = result.Fields[:1]
	result.Events = result.Events[:1]
	b = revalidatePublication(t, b, result)
	_, f, x = publicationFixture(t, d, r, "missing-partition", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, b)
	done, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	require.Equal(t, domain.ArchiveTaskWaitingInput, done.Status, "unmatched old physical event must retain old active and ask")
	var active string
	require.NoError(t, d.SQL().QueryRow(`SELECT active_publication_id FROM archive_documents WHERE id=?`, rec.DocumentID).Scan(&active))
	require.Equal(t, old, active)
}
func TestArchiveSpecRegressionSharedReassignPreservesValue(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	var docs []string
	for _, msg := range []string{"proofA", "proofB"} {
		rec, f, x := publicationFixture(t, d, r, msg, domain.ArchiveInputText)
		docs = append(docs, rec.DocumentID)
		f = saveBundle(t, r, f, literalBundle(t, "", []publicationLiteral{{"transaction_id", "0012"}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", "70"}}, "SGD"))
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
	}
	_, f, x := publicationFixture(t, d, r, "person", domain.ArchiveInputText)
	f = saveBundle(t, r, f, namedPublicationBundle(t, "Bob"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	var entity, obs string
	require.NoError(t, d.SQL().QueryRow(`SELECT entity_id FROM archive_entity_attributes WHERE value='Bob' LIMIT 1`).Scan(&entity))
	require.NoError(t, d.SQL().QueryRow(`SELECT id FROM archive_observations WHERE slot_key='supported-transaction-amount/1'`).Scan(&obs))
	_, e = r.ReviseArchive(ctx, a, "home-1", docs[0], 2, store.ArchiveRevision{Kind: "value", ObservationID: obs, Value: "71", Reason: "amount correction"})
	require.NoError(t, e)
	_, e = r.ReviseArchive(ctx, a, "home-1", docs[1], 2, store.ArchiveRevision{Kind: "reassign", ObservationID: obs, EntityID: entity, Reason: "owner correction"})
	require.NoError(t, e)
	items, e := r.CurrentArchiveObservations(ctx, a, "home-1", docs[0], false)
	require.NoError(t, e)
	for _, item := range items {
		if item.ID == obs {
			var b struct{ AssertedValue string }
			require.NoError(t, json.Unmarshal(item.Body, &b))
			require.Equal(t, "71", b.AssertedValue, "independent reassignment must not erase active value assertion")
		}
	}
}

func TestArchiveSpecRegressionSubjectQuestionAfterPartitionReview(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	_, f, x := publicationFixture(t, d, r, "first-anonymous", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, publicationBundle(t, "70"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	_, f, x = publicationFixture(t, d, r, "new-partition-subject", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, namedPublicationBundle(t, "妈妈"))
	done, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	require.Equal(t, domain.ArchiveTaskWaitingInput, done.Status)
	q, e := r.ArchiveQuestion(ctx, a, "home-1", f.TaskID)
	require.NoError(t, e)
	require.Contains(t, q.Actions, "create_subject")
	_, e = r.AnswerArchiveQuestion(ctx, a, "home-1", f.TaskID, store.ArchiveAnswer{QuestionID: q.ID, QuestionVersion: q.Version, WaitingTaskVersion: q.WaitingTaskVersion, ExtractionID: q.ExtractionID, DocumentRevision: q.DocumentRevision, ResolutionKey: "new-owner", Action: "create_subject", Name: "Alice"})
	require.NoError(t, e, "advertised typed answer must not depend on missing unpublished link")
}

func TestArchiveIndependentAssertionsComposeAcrossVisibilityChanges(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, rawB := range []string{"70", "80"} {
			for _, change := range []string{"private_value", "private_owner", "withdraw_value", "withdraw_owner"} {
				t.Run(fmt.Sprintf("reverse=%t/source=%s/%s", reverse, rawB, change), func(t *testing.T) {
					d := testutil.NewDB(t)
					r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
					ctx := context.Background()
					actor := archiveActor(t, d)
					docs := []string{}
					for i, raw := range []string{"70", rawB} {
						rec, f, x := publicationFixture(t, d, r, fmt.Sprint("proof", i), domain.ArchiveInputText)
						docs = append(docs, rec.DocumentID)
						f = saveBundle(t, r, f, literalBundle(t, "", []publicationLiteral{{"transaction_id", "0012"}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", raw}}, "SGD"))
						_, e := r.PublishExtraction(ctx, f, x.ID)
						require.NoError(t, e)
					}
					_, f, x := publicationFixture(t, d, r, "owner", domain.ArchiveInputText)
					f = saveBundle(t, r, f, namedPublicationBundle(t, "Bob"))
					_, e := r.PublishExtraction(ctx, f, x.ID)
					require.NoError(t, e)
					var owner, obs string
					require.NoError(t, d.SQL().QueryRow(`SELECT entity_id FROM archive_entity_attributes WHERE value='Bob' LIMIT 1`).Scan(&owner))
					require.NoError(t, d.SQL().QueryRow(`SELECT id FROM archive_observations WHERE slot_key='supported-transaction-amount/1'`).Scan(&obs))
					actions := []int{0, 1}
					if reverse {
						actions = []int{1, 0}
					}
					for _, i := range actions {
						revision := store.ArchiveRevision{Kind: "value", ObservationID: obs, Value: "71", Reason: "independent value"}
						if i == 1 {
							revision = store.ArchiveRevision{Kind: "reassign", ObservationID: obs, EntityID: owner, Reason: "independent owner"}
						}
						_, e = r.ReviseArchive(ctx, actor, "home-1", docs[i], 2, revision)
						require.NoError(t, e)
					}
					check := func(doc string, public, withValue, withOwner bool, wantRaw string) {
						t.Helper()
						items, e := r.CurrentArchiveObservations(ctx, actor, "home-1", doc, public)
						require.NoError(t, e)
						found := false
						for _, item := range items {
							if item.ID != obs {
								continue
							}
							found = true
							var body struct {
								Field                              archive.PublicationField
								AssertedValue                      string
								Entities                           []string
								ValueAssertion, OwnershipAssertion *store.ArchiveAssertionProvenance
							}
							require.NoError(t, json.Unmarshal(item.Body, &body))
							require.Equal(t, wantRaw, body.Field.Raw)
							if withValue {
								require.Equal(t, "71", body.AssertedValue)
								require.NotNil(t, body.ValueAssertion)
								require.Equal(t, docs[0], body.ValueAssertion.DocumentID)
								require.Equal(t, item.SourceDocumentID, docs[0])
								require.Equal(t, "value", body.ValueAssertion.Kind)
								require.NotEmpty(t, body.ValueAssertion.ID)
							} else {
								require.Empty(t, body.AssertedValue)
								require.Nil(t, body.ValueAssertion)
							}
							if withOwner {
								require.Equal(t, []string{owner}, body.Entities)
								require.NotNil(t, body.OwnershipAssertion)
								require.Equal(t, docs[1], body.OwnershipAssertion.DocumentID)
								require.Equal(t, "reassign", body.OwnershipAssertion.Kind)
								require.NotEmpty(t, body.OwnershipAssertion.ID)
							} else {
								require.Empty(t, body.Entities)
								require.Nil(t, body.OwnershipAssertion)
							}
						}
						require.True(t, found)
					}
					check(docs[0], true, true, true, "70")
					check(docs[1], true, true, true, "70")
					hidden := 0
					if strings.HasSuffix(change, "owner") {
						hidden = 1
					}
					kind := "private"
					if strings.HasPrefix(change, "withdraw") {
						kind = "withdraw"
					}
					_, e = r.ReviseArchive(ctx, actor, "home-1", docs[hidden], 3, store.ArchiveRevision{Kind: kind, Reason: "visibility regression"})
					require.NoError(t, e)
					visible := 1 - hidden
					withValue := hidden != 0
					withOwner := hidden != 1
					wantRaw := "70"
					if !withValue {
						wantRaw = rawB
					}
					check(docs[visible], true, withValue, withOwner, wantRaw)
					if kind == "private" {
						check(docs[visible], false, true, true, "70")
					} else {
						check(docs[visible], false, withValue, withOwner, wantRaw)
					}
					var auditCount int
					require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_manual_revisions WHERE kind IN ('value','reassign')`).Scan(&auditCount))
					require.Equal(t, 2, auditCount)
				})
			}
		}
	}
}

func TestArchivePartitionSubjectAnswerLeavesPartitionUnconfirmed(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	actor := archiveActor(t, d)
	rec, f, x := publicationFixture(t, d, r, "old-event", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, publicationBundle(t, "70"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	var oldPublication string
	require.NoError(t, d.SQL().QueryRow(`SELECT active_publication_id FROM archive_documents WHERE id=?`, rec.DocumentID).Scan(&oldPublication))
	_, f, x = publicationFixture(t, d, r, "new-subject", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, namedPublicationBundle(t, "妈妈"))
	_, e = r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	q, e := r.ArchiveQuestion(ctx, actor, "home-1", f.TaskID)
	require.NoError(t, e)
	_, e = r.AnswerArchiveQuestion(ctx, actor, "home-1", f.TaskID, store.ArchiveAnswer{QuestionID: q.ID, QuestionVersion: q.Version, WaitingTaskVersion: q.WaitingTaskVersion, ExtractionID: q.ExtractionID, DocumentRevision: q.DocumentRevision, ResolutionKey: "new-person", Action: "create_subject", Name: "Alice"})
	require.NoError(t, e)
	claimed, e := r.ClaimKinds(ctx, "home-1", time.Minute, []string{"document.extract"})
	require.NoError(t, e)
	require.Equal(t, f.TaskID, claimed.ID)
	f.Token = claimed.LeaseToken
	f.StateVersion = claimed.StateVersion
	x, claimed, e = r.RebaseArchiveExtraction(ctx, f, "fp1")
	require.NoError(t, e)
	f.StateVersion = claimed.StateVersion
	waiting, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	require.Equal(t, domain.ArchiveTaskWaitingInput, waiting.Status)
	q, e = r.ArchiveQuestion(ctx, actor, "home-1", f.TaskID)
	require.NoError(t, e)
	require.Equal(t, []string{"keep_current"}, q.Actions)
	var current string
	require.NoError(t, d.SQL().QueryRow(`SELECT active_publication_id FROM archive_documents WHERE id=?`, rec.DocumentID).Scan(&current))
	require.Equal(t, oldPublication, current)
	var n int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_observations`).Scan(&n))
	require.Equal(t, 1, n)
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_entities`).Scan(&n))
	require.Equal(t, 1, n)
}

func TestArchiveTransactionCurrentProofSetRecomputesAfterReplacementOrVisibility(t *testing.T) {
	for _, action := range []string{"replace", "private", "withdraw"} {
		t.Run(action, func(t *testing.T) {
			d := testutil.NewDB(t)
			r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
			ctx := context.Background()
			actor := archiveActor(t, d)
			docs := []string{}
			sources := []string{}
			for i, v := range []string{"70", "80", "80"} {
				kind := domain.ArchiveInputText
				if i == 0 {
					kind = domain.ArchiveInputFile
				}
				rec, f, x := publicationFixture(t, d, r, fmt.Sprint("initial", i), kind)
				docs = append(docs, rec.DocumentID)
				sources = append(sources, x.ID)
				f = saveBundle(t, r, f, literalBundle(t, "", []publicationLiteral{{"transaction_id", "0012"}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", v}}, "SGD"))
				_, e := r.PublishExtraction(ctx, f, x.ID)
				require.NoError(t, e)
			}
			check := func(doc string, public, want bool) {
				t.Helper()
				items, e := r.CurrentArchiveObservations(ctx, actor, "home-1", doc, public)
				require.NoError(t, e)
				found := false
				for _, item := range items {
					var b struct{ Field archive.PublicationField }
					require.NoError(t, json.Unmarshal(item.Body, &b))
					if b.Field.Name == "amount" {
						found = true
						require.Equal(t, "80", b.Field.Raw)
						require.Equal(t, doc, item.SourceDocumentID)
						var sourceDoc string
						require.NoError(t, d.SQL().QueryRow(`SELECT document_id FROM archive_extractions WHERE id=?`, item.ExtractionID).Scan(&sourceDoc))
						require.Equal(t, doc, sourceDoc)
					}
				}
				require.Equal(t, want, found)
			}
			check(docs[1], true, false)
			if action == "replace" {
				rec, f, x := publicationFixture(t, d, r, "corrected-first", domain.ArchiveInputFile)
				require.Equal(t, docs[0], rec.DocumentID)
				f = saveBundle(t, r, f, literalBundle(t, "", []publicationLiteral{{"transaction_id", "0012"}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", "80"}}, "SGD"))
				done, e := r.PublishExtraction(ctx, f, x.ID)
				require.NoError(t, e)
				require.Equal(t, domain.ArchiveTaskSucceeded, done.Status)
				check(docs[0], true, true)
			} else {
				_, e := r.ReviseArchive(ctx, actor, "home-1", docs[0], 2, store.ArchiveRevision{Kind: action, Reason: "remove stale source from public proof set"})
				require.NoError(t, e)
			}
			check(docs[1], true, true)
			check(docs[2], true, true)
			check(docs[1], false, action != "private")
			var oldBody string
			require.NoError(t, d.SQL().QueryRow(`SELECT body_json FROM archive_observation_revisions WHERE extraction_id=? AND json_extract(body_json,'$.Field.Name')='amount' LIMIT 1`, sources[0]).Scan(&oldBody))
			require.Contains(t, oldBody, `"Raw":"70"`)
		})
	}
}
