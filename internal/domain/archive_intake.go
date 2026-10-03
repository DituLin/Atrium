package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ArchiveSourceIdentity is host-attested delivery metadata, never model-chosen
// authority. T13 binds these fields to the actual IM provider delivery.
type ArchiveSourceIdentity struct {
	Kind              ArchiveInputKind `json:"kind"`
	Channel           string           `json:"channel"`
	Account           string           `json:"account"`
	Conversation      string           `json:"conversation"`
	Sender            string           `json:"sender"`
	ProviderMessageID string           `json:"provider_message_id"`
	SlotIndex         int              `json:"slot_index"`
}

func (s ArchiveSourceIdentity) Validate() error {
	if s.Kind != ArchiveInputText && s.Kind != ArchiveInputFile {
		return Errorf(CodeInvalidRequest, "invalid intake kind")
	}
	if s.SlotIndex < 0 || s.SlotIndex > 1023 {
		return Errorf(CodeInvalidRequest, "invalid intake slot")
	}
	for _, v := range []string{s.Channel, s.Account, s.Conversation, s.Sender, s.ProviderMessageID} {
		if len(v) == 0 || len(v) > 512 || !utf8.ValidString(v) || strings.IndexFunc(v, unicode.IsControl) >= 0 {
			return Errorf(CodeInvalidRequest, "invalid source identity")
		}
	}
	return nil
}
func (s ArchiveSourceIdentity) Key(household, principal string) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	for _, id := range []string{household, principal} {
		if err := ValidateArchiveIdentifier(id); err != nil {
			return "", err
		}
	}
	b, _ := json.Marshal([]any{"archive-source-v1", household, principal, s.Channel, s.Account, s.Conversation, s.Sender, s.ProviderMessageID, s.Kind, s.SlotIndex})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

type ArchiveIntakeMetadata struct {
	ArchiveSourceIdentity
	OriginalName     string `json:"original_name"`
	DeclaredMIME     string `json:"declared_mime,omitempty"`
	ExpectedSize     *int64 `json:"expected_size,omitempty"`
	ExpectedSHA256   string `json:"expected_sha256,omitempty"`
	VisibilityIntent string `json:"visibility_intent,omitempty"`
}

func (m ArchiveIntakeMetadata) Intent() string {
	if m.VisibilityIntent == "" {
		return "inherit"
	}
	return m.VisibilityIntent
}
func (m ArchiveIntakeMetadata) Validate() error {
	if err := m.ArchiveSourceIdentity.Validate(); err != nil {
		return err
	}
	if len(m.OriginalName) == 0 || len(m.OriginalName) > 512 || !utf8.ValidString(m.OriginalName) || strings.IndexFunc(m.OriginalName, unicode.IsControl) >= 0 || strings.ContainsAny(m.OriginalName, "/\\") {
		return Errorf(CodeInvalidRequest, "original_name must be a filename")
	}
	if m.Intent() != "inherit" && m.Intent() != "private" {
		return Errorf(CodeInvalidRequest, "invalid visibility intent")
	}
	if m.DeclaredMIME != "" {
		if len(m.DeclaredMIME) > 255 {
			return Errorf(CodeInvalidRequest, "invalid declared MIME")
		}
		if _, _, err := mime.ParseMediaType(m.DeclaredMIME); err != nil {
			return Errorf(CodeInvalidRequest, "invalid declared MIME")
		}
	}
	if m.ExpectedSize != nil && *m.ExpectedSize < 1 {
		return Errorf(CodeInvalidRequest, "invalid expected size")
	}
	if m.ExpectedSHA256 != "" {
		return ValidateArchiveSHA256(m.ExpectedSHA256)
	}
	return nil
}

// BusinessJSON excludes optional transport assertions: bytes actually received
// determine equality. Omitted inherit and explicit inherit are identical.
func (m ArchiveIntakeMetadata) BusinessJSON() []byte {
	m.ExpectedSize = nil
	m.ExpectedSHA256 = ""
	m.VisibilityIntent = m.Intent()
	b, _ := json.Marshal(m)
	return b
}

type ArchiveIntakeReceipt struct {
	ReceiptID      string                 `json:"receipt_id"`
	TaskID         string                 `json:"task_id"`
	OriginalStatus string                 `json:"original_status"`
	SizeBytes      int64                  `json:"size_bytes"`
	SHA256         string                 `json:"sha256"`
	ReceivedAt     time.Time              `json:"received_at"`
	DocumentID     string                 `json:"document_id,omitempty"`
	Task           *ArchiveTaskProjection `json:"task,omitempty"`
}

// ArchiveTaskProjection is a public schema, not a serialization of worker
// checkpoints. T09 can produce these enums/counts; free text, refs, file paths,
// source metadata and arbitrary nested JSON are intentionally not projected.
type ArchiveTaskProjection struct {
	ID                  string                    `json:"id"`
	Status              ArchiveTaskStatus         `json:"status"`
	Stage               string                    `json:"stage"`
	OriginalStatus      string                    `json:"original_status"`
	NASStatus           string                    `json:"nas_status"`
	Result              *ArchiveTaskResult        `json:"result,omitempty"`
	Clarification       *ArchiveTaskClarification `json:"clarification,omitempty"`
	DelegationErrorCode string                    `json:"delegation_error_code,omitempty"`
	CreatedAt           time.Time                 `json:"created_at"`
	UpdatedAt           time.Time                 `json:"updated_at"`
}
type ArchiveTaskResult struct {
	ParseStatus string `json:"parse_status,omitempty"`
	FactCount   *int64 `json:"fact_count,omitempty"`
}
type ArchiveTaskClarification struct {
	Code string `json:"code"`
}

func ProjectArchiveTask(t *ArchiveTask) *ArchiveTaskProjection {
	p := &ArchiveTaskProjection{ID: t.ID, Status: t.Status, Stage: "processing", OriginalStatus: "saved", NASStatus: "pending", CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
	switch t.Stage {
	case "intake", "saved":
		p.Stage = "saved"
	case "parsing", "understanding", "resolving", "committing":
		p.Stage = t.Stage
	}
	switch t.DelegationErrorCode {
	case "unbound", "principal_inactive", "policy_changed", "permission_denied":
		p.DelegationErrorCode = t.DelegationErrorCode
	}
	var r struct {
		ParseStatus string `json:"parse_status"`
		FactCount   *int64 `json:"fact_count"`
	}
	if json.Unmarshal(t.ResultJSON, &r) == nil {
		out := &ArchiveTaskResult{}
		switch r.ParseStatus {
		case "pending", "parsed", "unsupported", "failed":
			out.ParseStatus = r.ParseStatus
		}
		if r.FactCount != nil && *r.FactCount >= 0 && *r.FactCount <= 1000000 {
			out.FactCount = r.FactCount
		}
		if out.ParseStatus != "" || out.FactCount != nil {
			p.Result = out
		}
	}
	var c ArchiveTaskClarification
	if json.Unmarshal(t.ClarificationJSON, &c) == nil {
		switch c.Code {
		case "subject_ambiguous", "document_unreadable", "unsupported_format":
			p.Clarification = &c
		}
	}
	return p
}
