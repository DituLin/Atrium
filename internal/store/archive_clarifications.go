package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"strings"
)

type ArchiveQuestion struct {
	SubjectName      string            `json:"subject_name,omitempty"`
	SubjectRole      string            `json:"subject_role,omitempty"`
	TargetLabels     map[string]string `json:"target_labels,omitempty"`
	TargetsTruncated bool              `json:"targets_truncated,omitempty"`

	EntityKind         string           `json:"entity_kind,omitempty"`
	ID                 string           `json:"question_id"`
	Version            int64            `json:"question_version"`
	WaitingTaskVersion int64            `json:"waiting_task_version"`
	ExtractionID       string           `json:"extraction_id"`
	DocumentID         string           `json:"document_id"`
	DocumentRevision   int64            `json:"document_revision"`
	ResolutionRevision int64            `json:"resolution_revision"`
	MentionRef         string           `json:"mention_ref,omitempty"`
	Actions            []string         `json:"actions"`
	Targets            map[string]int64 `json:"targets"`
	Reason             string           `json:"reason"`
}
type ArchiveAnswer struct {
	Name               string `json:"name,omitempty"`
	QuestionID         string `json:"question_id"`
	QuestionVersion    int64  `json:"question_version"`
	WaitingTaskVersion int64  `json:"waiting_task_version"`
	ExtractionID       string `json:"extraction_id"`
	DocumentRevision   int64  `json:"document_revision"`
	ResolutionKey      string `json:"resolution_key"`
	Action             string `json:"action"`
	Target             string `json:"target,omitempty"`
}

func archiveQuestionIn(ctx context.Context, tx *sql.Tx, h, task string) (ArchiveQuestion, error) {
	var q ArchiveQuestion
	var raw string
	e := tx.QueryRowContext(ctx, `SELECT body_json FROM archive_questions WHERE household_id=? AND task_id=? AND answer_json IS NULL ORDER BY version DESC LIMIT 1`, h, task).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return q, domain.ErrNotFound
	}
	if e != nil {
		return q, e
	}
	e = json.Unmarshal([]byte(raw), &q)
	return q, e
}
func archiveCreateQuestionIn(ctx context.Context, tx *sql.Tx, f ArchiveWorkFence, x ArchiveExtraction, p archive.ResolutionPlan, s archive.CandidateSnapshot, in archive.ResolutionInput) (ArchiveQuestion, error) {
	q := ArchiveQuestion{ID: domain.NewID(), Version: 1, WaitingTaskVersion: f.StateVersion + 1, ExtractionID: x.ID, DocumentID: x.DocumentID, DocumentRevision: x.DocumentRevision, ResolutionRevision: s.Revision, Actions: []string{"keep_current"}, Targets: map[string]int64{}, TargetLabels: map[string]string{}, Reason: "publication_requires_review"}
	if e := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM archive_questions WHERE household_id=? AND task_id=?`, f.HouseholdID, f.TaskID).Scan(&q.Version); e != nil {
		return q, e
	}
	for _, d := range p.Decisions {
		if d.Action == "needs_resolution" {
			q.MentionRef = d.MentionRef
			q.Reason = d.Reason
			q.Actions = append(q.Actions, "select_subject", "create_subject")
			for _, m := range in.Mentions {
				if m.Ref == d.MentionRef {
					q.EntityKind = m.Kind
					q.SubjectName = archiveQuestionText(m.Name)
					q.SubjectRole = archiveQuestionText(m.Role)
				}
			}
			for _, c := range s.Candidates {
				if c.Kind == q.EntityKind {
					if len(q.Targets) >= 64 {
						q.TargetsTruncated = true
						continue
					}
					q.Targets[c.ID] = c.Version
					if len(c.Names) > 0 {
						q.TargetLabels[c.ID] = archiveQuestionText(c.Names[0].Value)
					}
				}
			}
			break
		}
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO archive_questions(household_id,id,task_id,version,body_json) VALUES(?,?,?,?,?)`, f.HouseholdID, q.ID, f.TaskID, q.Version, entityJSON(q))
	return q, e
}
func (r *ArchiveTasks) ArchiveQuestion(ctx context.Context, a domain.IntegrationActor, h, task string) (ArchiveQuestion, error) {
	var q ArchiveQuestion
	e := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, e := r.currentTime()
		if e != nil {
			return e
		}
		if _, e = r.authorizeArchiveClient(ctx, tx, a, h, "archive.read", now); e != nil {
			return e
		}
		q, e = archiveQuestionIn(ctx, tx, h, task)
		return e
	})
	return q, e
}
func (r *ArchiveTasks) AnswerArchiveQuestion(ctx context.Context, a domain.IntegrationActor, h, task string, answer ArchiveAnswer) (*domain.ArchiveTask, error) {
	var out *domain.ArchiveTask
	if archiveIdentifiers(answer.ResolutionKey) != nil {
		return nil, domain.ErrConflict
	}
	e := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, e := r.currentTime()
		if e != nil {
			return e
		}
		p, e := r.authorizeArchiveClient(ctx, tx, a, h, "archive.read", now)
		if e != nil {
			return e
		}
		if !p.Policy.AllowsArchive("archive.organize", h) {
			return domain.Errorf(domain.CodeForbidden, "archive.organize required")
		}
		t, e := archiveTaskIn(ctx, tx, h, task)
		if e != nil {
			return e
		}
		if e = r.authorizeArchiveDelegate(ctx, tx, t, now); e != nil {
			return e
		}
		var previous string
		replayErr := tx.QueryRowContext(ctx, `SELECT answer_json FROM archive_questions WHERE household_id=? AND task_id=? AND resolution_key=?`, h, task, answer.ResolutionKey).Scan(&previous)
		if replayErr == nil {
			if previous != entityJSON(answer) {
				return domain.ErrConflict
			}
			out = t
			return nil
		}
		if !errors.Is(replayErr, sql.ErrNoRows) {
			return replayErr
		}
		if t.Status != domain.ArchiveTaskWaitingInput {
			return domain.ErrConflict
		}
		q, e := archiveQuestionIn(ctx, tx, h, task)
		if e != nil {
			return e
		}
		if q.ID != answer.QuestionID || q.Version != answer.QuestionVersion || q.WaitingTaskVersion != answer.WaitingTaskVersion || q.WaitingTaskVersion != t.StateVersion || q.ExtractionID != answer.ExtractionID || q.DocumentRevision != answer.DocumentRevision {
			return domain.ErrConflict
		}
		var rev int64
		if e = tx.QueryRowContext(ctx, `SELECT document_revision FROM archive_documents WHERE household_id=? AND id=?`, h, q.DocumentID).Scan(&rev); e != nil {
			return e
		}
		rr, e := archiveResolutionRevisionIn(ctx, tx, h)
		if e != nil {
			return e
		}
		if rev != q.DocumentRevision || rr != q.ResolutionRevision {
			return domain.ErrConflict
		}
		allowed := false
		for _, s := range q.Actions {
			allowed = allowed || s == answer.Action
		}
		if !allowed {
			return domain.ErrConflict
		}
		if answer.Action == "select_subject" || answer.Action == "create_subject" {
			target := answer.Target
			if answer.Action == "create_subject" {
				if target != "" || strings.TrimSpace(answer.Name) == "" || len(answer.Name) > 256 {
					return domain.ErrConflict
				}
				target, e = archiveEntityCreateIn(ctx, tx, h, q.EntityKind)
				if e != nil {
					return e
				}
			} else {
				if answer.Name != "" {
					return domain.ErrConflict
				}
				version, ok := q.Targets[target]
				if !ok {
					return domain.ErrConflict
				}
				_, current, e := archiveEntityVersionIn(ctx, tx, h, target)
				if e != nil {
					return e
				}
				if current != version {
					return domain.ErrConflict
				}
			}

			var bv int64
			e = tx.QueryRowContext(ctx, `SELECT version FROM archive_entity_bindings WHERE household_id=? AND scope='document_mention' AND scope_key=?`, h, q.DocumentID+"#"+q.MentionRef).Scan(&bv)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			if e = archiveBindingPutIn(ctx, tx, h, "document_mention", q.DocumentID+"#"+q.MentionRef, target, a.PrincipalID, []string{"user_assertion:" + q.ID}, bv); e != nil {
				return e
			}
			// An answer is an explicit scoped human assertion, audited separately
			// from source extraction. It does not pretend the supplied name was quoted.
			var link string
			e = tx.QueryRowContext(ctx, `SELECT id FROM archive_entity_links WHERE household_id=? AND document_id=? AND mention_ref=?`, h, q.DocumentID, q.MentionRef).Scan(&link)
			if e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE archive_entity_links SET matched_entity_id=?,status='linked',version=version+1 WHERE household_id=? AND id=?`, target, h, link); e != nil {
				return e
			}
			if answer.Action == "create_subject" {
				var visible bool
				if e = tx.QueryRowContext(ctx, `SELECT tv_visible AND NOT original_restricted FROM archive_documents WHERE household_id=? AND id=?`, h, q.DocumentID).Scan(&visible); e != nil {
					return e
				}
				if e = archiveAttributePutIn(ctx, tx, h, target, "name", answer.Name, "", q.DocumentID, "", []string{"user_assertion:" + q.ID}, visible, false, link); e != nil {
					return e
				}
			}
			if e = archiveEntityHistoryIn(ctx, tx, h, link, "clarification", a.PrincipalID, "user assertion", nil, answer); e != nil {
				return e
			}
			_, e = tx.ExecContext(ctx, `UPDATE archive_documents SET document_revision=document_revision+1 WHERE household_id=? AND id=?`, h, q.DocumentID)
			if e != nil {
				return e
			}
			if e = archiveDocumentChangedIn(ctx, tx, h, q.DocumentID, q.DocumentRevision+1); e != nil {
				return e
			}
			t.Status = domain.ArchiveTaskQueued
			t.AvailableAt = now
		} else {
			if answer.Target != "" || answer.Name != "" {
				return domain.ErrConflict
			}
			t.Status = domain.ArchiveTaskSucceeded
			t.Stage = "kept_current"
		}
		_, e = tx.ExecContext(ctx, `UPDATE archive_questions SET answer_json=?,resolution_key=? WHERE household_id=? AND id=? AND answer_json IS NULL`, entityJSON(answer), answer.ResolutionKey, h, q.ID)
		if e != nil {
			return e
		}
		t.StateVersion++
		t.ClarificationJSON = json.RawMessage(`{}`)
		t.LeaseToken = ""
		t.LeaseExpiresAt = nil
		out = t
		return archiveSave(ctx, tx, t, "question_answered", now, a.PrincipalID)
	})
	if out != nil {
		out.LeaseToken = ""
		out.LeaseExpiresAt = nil
	}
	return out, e
}

func archiveQuestionText(s string) string {
	r := []rune(s)
	if len(r) > 128 {
		return string(r[:128])
	}
	return s
}

// RefreshStaleArchiveQuestions is worker maintenance, never a delegation refresh.
// It requeues only outdated typed questions so a fresh question can be derived
// from committed products under current document/entity versions.
func (r *ArchiveTasks) RefreshStaleArchiveQuestions(ctx context.Context, h string, limit int) (int, error) {
	if limit < 1 || limit > 32 {
		return 0, domain.ErrConflict
	}
	count := 0
	err := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, e := r.currentTime()
		if e != nil {
			return e
		}
		rows, e := tx.QueryContext(ctx, `SELECT t.id FROM archive_tasks t JOIN archive_questions q ON q.household_id=t.household_id AND q.task_id=t.id AND q.answer_json IS NULL JOIN archive_documents d ON d.household_id=t.household_id AND d.id=json_extract(q.body_json,'$.document_id') JOIN archive_resolution_revisions rr ON rr.household_id=t.household_id WHERE t.household_id=? AND t.kind='document.extract' AND t.status='waiting_input' AND d.withdrawn=0 AND q.version=(SELECT MAX(latest.version) FROM archive_questions latest WHERE latest.household_id=q.household_id AND latest.task_id=q.task_id) AND (d.document_revision<>json_extract(q.body_json,'$.document_revision') OR rr.revision<>json_extract(q.body_json,'$.resolution_revision')) AND EXISTS(SELECT 1 FROM integration_principals principal WHERE principal.id=t.principal_id AND principal.policy_version=t.policy_version AND principal.enabled=1 AND principal.revoked_at IS NULL AND principal.expires_at>? AND EXISTS(SELECT 1 FROM json_each(principal.policy_json,'$.permissions') WHERE value='archive.ingest') AND EXISTS(SELECT 1 FROM json_each(principal.policy_json,'$.household_ids') WHERE value=t.household_id)) ORDER BY t.id LIMIT ?`, h, FormatTime(now), limit)
		if e != nil {
			return e
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, id := range ids {
			t, e := archiveTaskIn(ctx, tx, h, id)
			if e != nil {
				return e
			}
			if e = r.authorizeArchiveDelegate(ctx, tx, t, now); e != nil {
				continue
			}
			t.Status = domain.ArchiveTaskQueued
			t.Stage = "question_refresh"
			t.StateVersion++
			t.ClarificationJSON = json.RawMessage(`{}`)
			t.AvailableAt = now
			t.LeaseToken = ""
			t.LeaseExpiresAt = nil
			if e = archiveSave(ctx, tx, t, "question_stale", now, ""); e != nil {
				return e
			}
			count++
		}
		return nil
	})
	return count, err
}
