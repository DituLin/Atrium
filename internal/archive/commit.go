package archive

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

const PublicationAnchorRule = "physical-parent/1"
const PublicationVersion = "archive-publication/9"

// PhysicalAnchor intentionally excludes generated segment IDs, model names,
// dates and chunk boundaries. Offsets are in the original parent component.
type PhysicalAnchor struct {
	Locator    Locator
	Component  string
	Start, End int
	Geometry   *PageGeometry
}
type PublicationDimension struct {
	Kind, Raw string
	Evidence  []CanonicalEvidence
}
type PublicationField struct {
	SourceName              string `json:",omitempty"`
	DerivedDependencies     []DerivedValue
	VisibilityAnchors       []PhysicalAnchor
	RequiresReview          bool
	Ref, SlotKey, Name, Raw string
	Evidence                []CanonicalEvidence
	Subjects                []string
	Normalizations          []Normalization
	Dimensions              []PublicationDimension
	Anchors                 []PhysicalAnchor
}
type PublicationEvent struct {
	AnchorKey, Type string
	Anchors         []PhysicalAnchor
	Fields          []PublicationField
}
type PublicationCandidate struct {
	Protected      []ProtectedIdentityValue
	Resolution     ResolutionInput
	Events         []PublicationEvent
	Labels         []UnderstandingLabel
	Status         ParseStatus
	Coverage       ParseCoverage
	RequiresReview bool
}

func scopedRef(chunk int, id string) string { return fmt.Sprintf("%d:%s", chunk, id) }
func physicalEvidence(parsed ParseResult, refs []CanonicalEvidence) ([]PhysicalAnchor, error) {
	var out []PhysicalAnchor
	for _, ref := range refs {
		var segment *Segment
		for i := range parsed.Segments {
			if parsed.Segments[i].ID == ref.ParentSegmentID {
				segment = &parsed.Segments[i]
				break
			}
		}
		if segment == nil {
			return nil, understandingError("publication_parent")
		}
		a := PhysicalAnchor{Locator: segment.Locator, Component: ref.ComponentKind, Start: ref.Start, End: ref.End}
		if a.Locator.Kind == "" {
			return nil, understandingError("publication_anchor")
		}
		if a.Locator.Page != nil {
			if parsed.Native == nil {
				return nil, understandingError("publication_geometry")
			}
			for _, g := range parsed.Native.Pages {
				if g.Page == a.Locator.Page.Page {
					g.OCRComplete = false
					g.NativeComplete = false
					a.Geometry = &g
					break
				}
			}
			if a.Geometry == nil {
				return nil, understandingError("publication_geometry")
			}
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return digestJSON(out[i]) < digestJSON(out[j]) })
	return out, nil
}

// CompilePublication rechecks the complete T07 digest/evidence closure and maps
// every field independently to parent evidence. It never allocates business IDs.
func CompilePublication(home, document string, parsed ParseResult, p UnderstandingPlan, u Understanding) (PublicationCandidate, error) {
	out := PublicationCandidate{Resolution: ResolutionInput{HouseholdID: home, DocumentID: document}, Labels: u.Labels, Status: u.Status, Coverage: u.Coverage, RequiresReview: len(u.LinkIssues) > 0}
	prompt := understandingPrompt
	policy := ""
	for _, ch := range u.Chunks {
		if ch.Receipt != nil {
			fp := ch.Receipt.ConfigurationFingerprint
			if policy != "" && policy != fp {
				return out, understandingError("publication_policy")
			}
			policy = fp
		}
	}
	if policy == understandingFingerprintForPrompt(previousUnderstandingPrompt) {
		prompt = previousUnderstandingPrompt
	}
	if policy == understandingFingerprintForPrompt(previousHeaderPrompt) {
		prompt = previousHeaderPrompt
	}
	expected, e := planChunksForPrompt(parsed, prompt)
	if e != nil {
		return out, e
	}
	if !reflect.DeepEqual(expected, p) {
		return out, understandingError("publication_parse")
	}
	merged, e := MergeChunks(p, u.Chunks)
	if e != nil {
		return out, e
	}
	if !reflect.DeepEqual(merged, u) {
		return out, understandingError("publication_merge")
	}
	fields := map[string]PublicationField{}
	subjects := map[string]bool{}
	canonical := func(ci int, refs []EvidenceReference) ([]CanonicalEvidence, error) {
		var out []CanonicalEvidence
		for _, ref := range refs {
			found := false
			for _, v := range p.Chunks[ci].Evidence() {
				if v.ViewID == ref.ViewID {
					out = append(out, canonicalEvidence(v, ref))
					found = true
					break
				}
			}
			if !found {
				return nil, understandingError("publication_evidence")
			}
		}
		return out, nil
	}
	mentions := map[string]ResolutionMention{}
	for _, c := range u.Chunks {
		for _, sub := range c.Result.Subjects {
			refs, e := canonical(c.Index, sub.Evidence)
			if e != nil {
				return out, e
			}
			// Identity of a local mention is its literal name location, not
			// optional role labels cited alongside it. Distinct name locations
			// and distinct roles remain separate; no name-string matching.
			nameRefs := []CanonicalEvidence{}
			for _, ref := range refs {
				if strings.Count(ref.Quote, sub.Name) == 1 {
					at := strings.Index(ref.Quote, sub.Name)
					ref.Start += len([]rune(ref.Quote[:at]))
					ref.End = ref.Start + len([]rune(sub.Name))
					ref.Quote = sub.Name
					nameRefs = append(nameRefs, ref)
				}
			}
			anchors, e := physicalEvidence(parsed, nameRefs)
			if e != nil {
				return out, e
			}
			unique := []PhysicalAnchor{}
			seen := map[string]bool{}
			for _, anchor := range anchors {
				key := digestJSON(anchor)
				if !seen[key] {
					seen[key] = true
					unique = append(unique, anchor)
				}
			}
			if len(unique) == 0 {
				return out, understandingError("publication_subject_name")
			}
			m := ResolutionMention{Ref: digestJSON(struct {
				Policy, Role string
				Anchors      []PhysicalAnchor
			}{"literal-name-location/1", sub.Role, unique}), Kind: "person", Name: sub.Name, Role: sub.Role, Ambiguous: sub.Ambiguous, Evidence: []string{}}
			for _, v := range refs {
				m.Evidence = append(m.Evidence, digestJSON(v))
			}
			for _, ref := range sub.Evidence {
				for _, v := range p.Chunks[c.Index].Evidence() {
					if v.ViewID == ref.ViewID && (v.RequiresReview || v.VisualState == "native_unconfirmed") {
						m.OCRConflict = true
					}
				}
			}
			m.BindingScope = "document_mention"
			m.BindingKey = document + "#" + m.Ref
			// An explicit event patient/owner is still a subject when the source
			// is only a notification. This requests resolution; it never supplies
			// an identity, measurement, or a first-name match.
			ownedName := false
			for _, ref := range sub.Evidence {
				if ref.Quote == sub.Name {
					for _, v := range p.Chunks[c.Index].Owned {
						if v.ViewID == ref.ViewID {
							ownedName = true
						}
					}
				}
			}
			if ownedName && archivePrimarySubjectRole(sub.Role) {
				subjects[m.Ref] = true
			}

			mentions[scopedRef(c.Index, sub.ID)] = m
		}
	}
	for _, c := range u.Chunks {
		for _, f := range c.Result.Fields {
			refs, e := canonical(c.Index, f.Evidence)
			if e != nil {
				return out, e
			}
			anchors, e := physicalEvidence(parsed, refs)
			if e != nil {
				return out, e
			}
			pf := PublicationField{Ref: scopedRef(c.Index, f.ID), SlotKey: digestJSON(anchors), Name: CanonicalFieldName(f.Name), SourceName: f.Name, Raw: f.Raw, Evidence: refs, Anchors: anchors, Normalizations: f.Normalizations, VisibilityAnchors: append([]PhysicalAnchor(nil), anchors...)}
			for _, dimension := range f.Dimensions {
				refs, e := canonical(c.Index, dimension.Evidence)
				if e != nil {
					return out, e
				}
				pf.Dimensions = append(pf.Dimensions, PublicationDimension{Kind: dimension.Kind, Raw: dimension.Raw, Evidence: refs})
				dependencyAnchors, e := physicalEvidence(parsed, refs)
				if e != nil {
					return out, e
				}
				pf.VisibilityAnchors = append(pf.VisibilityAnchors, dependencyAnchors...)
			}
			pf.RequiresReview = p.StructuralContextLimited
			allReferences := append([]EvidenceReference(nil), f.Evidence...)
			for _, dimension := range f.Dimensions {
				allReferences = append(allReferences, dimension.Evidence...)
			}
			for _, reference := range allReferences {
				for _, view := range p.Chunks[c.Index].Evidence() {
					if view.ViewID == reference.ViewID && view.Derived != nil {
						pf.DerivedDependencies = append(pf.DerivedDependencies, *view.Derived)
					}
					if view.ViewID == reference.ViewID && (view.RequiresReview || view.VisualState == "native_unconfirmed") {
						pf.RequiresReview = true
					}
				}
				for _, issue := range c.Result.Issues {
					if len(issue.ViewRefs) == 0 {
						pf.RequiresReview = true
					}
					for _, id := range issue.ViewRefs {
						if id == reference.ViewID {
							pf.RequiresReview = true
						}
					}
				}
			}
			for _, sub := range f.SubjectRefs {
				key := scopedRef(c.Index, sub)
				m := mentions[key]
				pf.Subjects = append(pf.Subjects, m.Ref)
				// A related person's name/signature alone does not establish an archive
				// subject. Explicit owned measurements or supported identity semantics do.
				if archiveSubjectField(m.Role, f.Name) {
					subjects[m.Ref] = true
				}
				if CanonicalFieldName(f.Name) == "full_address" && f.Raw == m.Name {
					m.Kind = "property"
					m.FullAddress = true
				}
				if isBirthField(f.Name) {
					for _, n := range f.Normalizations {
						precision := ""
						if n.Kind == "date" {
							precision = "date"
						}
						if n.Kind == "date_partial" {
							if len(n.Value) == 4 {
								precision = "year"
							}
							if len(n.Value) == 7 {
								precision = "month"
							}
						}
						if precision != "" {
							d := IdentityDate{Value: n.Value, Precision: precision, Evidence: []string{}}
							for _, v := range refs {
								d.Evidence = append(d.Evidence, digestJSON(v))
							}
							m.Birth = CombineIdentityDates(m.Birth, d)
						}
					}
				}
				mentions[key] = m
			}
			sourceBillLabels(parsed, &pf)
			fields[pf.Ref] = pf
			out.Resolution.Fields = append(out.Resolution.Fields, ResolutionField{Ref: pf.Ref, SubjectRefs: pf.Subjects})
		}
	}
	allFields := []PublicationField{}
	for _, f := range fields {
		allFields = append(allFields, f)
	}
	sort.Slice(allFields, func(i, j int) bool { return allFields[i].Ref < allFields[j].Ref })
	out.Protected = ProtectedIdentityValues(allFields)
	for key, m := range mentions {
		for _, identity := range out.Protected {
			if m.Ref == identity.MentionRef {
				m.HasStrongIdentity = true
				subjects[m.Ref] = true
			}
		}
		mentions[key] = m
	}
	// All chunks referring to the same proven name location contribute to
	// its identity. Disagreements remain monotonic rather than first-chunk wins.
	combined := map[string]ResolutionMention{}
	for _, c := range u.Chunks {
		for _, sub := range c.Result.Subjects {
			m := mentions[scopedRef(c.Index, sub.ID)]
			if old, ok := combined[m.Ref]; ok {
				m.Birth = CombineIdentityDates(old.Birth, m.Birth)
				m.Ambiguous = m.Ambiguous || old.Ambiguous
				m.OCRConflict = m.OCRConflict || old.OCRConflict
				m.HasStrongIdentity = m.HasStrongIdentity || old.HasStrongIdentity
				m.FullAddress = m.FullAddress || old.FullAddress
				if old.Kind != m.Kind {
					if m.FullAddress {
						m.Kind = "property"
					} else {
						m.OCRConflict = true
					}
				}
				m.Evidence = append(m.Evidence, old.Evidence...)
				sort.Strings(m.Evidence)
				compact := m.Evidence[:0]
				for _, ev := range m.Evidence {
					if len(compact) == 0 || compact[len(compact)-1] != ev {
						compact = append(compact, ev)
					}
				}
				m.Evidence = compact
			}
			combined[m.Ref] = m
		}
	}
	for key, m := range mentions {
		mentions[key] = combined[m.Ref]
	}
	seenMentions := map[string]bool{}
	for _, c := range u.Chunks {
		for _, sub := range c.Result.Subjects {
			m := mentions[scopedRef(c.Index, sub.ID)]
			m.Subject = subjects[m.Ref]
			if !seenMentions[m.Ref] {
				out.Resolution.Mentions = append(out.Resolution.Mentions, m)
				seenMentions[m.Ref] = true
			}
		}
	}
	events := publicationReportSections(parsed, u.Events)
	out.RequiresReview = false
	for _, issue := range u.LinkIssues {
		if issue != "overlapping_report_anchors" || overlappingReportAnchors(events) {
			out.RequiresReview = true
		}
	}
	for _, ev := range events {
		anchors, e := physicalEvidence(parsed, ev.CanonicalEvidence)
		if e != nil {
			return out, e
		}
		event := PublicationEvent{AnchorKey: digestJSON(anchors), Type: ev.Type, Anchors: anchors}
		for _, ref := range ev.FieldRefs {
			f, ok := fields[scopedRef(ref.ChunkIndex, ref.LocalID)]
			if !ok {
				return out, understandingError("publication_field")
			}
			event.Fields = append(event.Fields, f)
		}
		out.Events = append(out.Events, event)
	}
	return out, nil
}

// PhysicalAnchorsMatch accepts only a bijection of physical components. Native
// rectangles need IoU >= .90 in exactly the same page/crop/rotation space.
// The caller additionally enforces uniqueness against every old/new event.
func PhysicalAnchorsMatch(a, b []PhysicalAnchor) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	used := map[int]bool{}
	for _, x := range a {
		match := -1
		for i, y := range b {
			if used[i] || x.Component != y.Component {
				continue
			}
			same := reflect.DeepEqual(x, y)
			if x.Locator.Page != nil && y.Locator.Page != nil && x.Geometry != nil && y.Geometry != nil && x.Locator.Page.Page == y.Locator.Page.Page && reflect.DeepEqual(x.Geometry, y.Geometry) {
				r, s := x.Locator.Page.Rect, y.Locator.Page.Rect
				area := intersectArea(r, s)
				union := r[2]*r[3] + s[2]*s[3] - area
				same = union > 0 && area/union >= .90
			}
			if same {
				if match >= 0 {
					return false
				}
				match = i
			}
		}
		if match < 0 {
			return false
		}
		used[match] = true
	}
	return true
}

// Role labels remain open. Known related-party roles do not create a subject
// merely from their own name, license, signature, fee or contact field.
func archivePrimarySubjectRole(role string) bool {
	switch role {
	case "patient", "患者", "student", "学生", "owner", "业主", "occupant", "居住人", "account_holder", "账户持有人":
		return true
	}
	return false
}

func archiveSubjectField(role, field string) bool {
	role = NameKey(role)
	field = NameKey(field)
	switch role {
	case "doctor", "physician", "医生", "engineer", "工程师", "teacher", "老师", "教师", "signer", "签字人", "payee", "issuer", "收款方", "收款人", "counterparty", "supplier":
		return false
	}
	if field == "" || field == "name" || strings.HasSuffix(field, "_name") || strings.Contains(field, "signature") {
		return false
	}
	return true
}

type TransactionProof struct {
	Identifier, Type, Issuer string
	Date                     string `json:",omitempty"`
	Evidence                 []CanonicalEvidence
}

// TransactionIdentity recognizes only the supported complete proof tuple. Name,
// date, equal amounts or a bare identifier are never cross-file merge evidence.
func TransactionIdentity(fields []PublicationField) *TransactionProof {
	for _, f := range fields {
		if f.Name == "bill_id" {
			return billIdentity(fields)
		}
	}
	p := &TransactionProof{}
	seen := map[string]bool{}
	for _, f := range fields {
		var target *string
		switch f.Name {
		case "transaction_id":
			target = &p.Identifier
		case "transaction_type":
			target = &p.Type
		case "issuer":
			target = &p.Issuer
		default:
			continue
		}
		if seen[f.Name] || f.Raw == "" || len(f.Evidence) == 0 || f.RequiresReview {
			return nil
		}
		seen[f.Name] = true
		*target = f.Raw
		p.Evidence = append(p.Evidence, f.Evidence...)
	}
	if p.Identifier == "" || p.Issuer == "" || (p.Type != "payment" && p.Type != "transfer" && p.Type != "purchase") {
		return nil
	}
	return p
}

type ProtectedIdentityValue struct {
	MentionRef, Kind, Issuer, Value string
	Evidence                        []string
}

func ProtectedIdentityValues(fields []PublicationField) []ProtectedIdentityValue {
	out := []ProtectedIdentityValue{}
	for _, f := range fields {
		if f.Name != "national_id" && f.Name != "passport" && f.Name != "birth_certificate" {
			continue
		}
		if len(f.Subjects) != 1 || len(f.Evidence) == 0 || f.RequiresReview {
			continue
		}
		issuer := ""
		evidence := []string{}
		for _, v := range f.Evidence {
			evidence = append(evidence, digestJSON(v))
		}
		for _, i := range fields {
			if i.Name == "identity_issuer" && len(i.Subjects) == 1 && i.Subjects[0] == f.Subjects[0] && !i.RequiresReview && len(i.Evidence) > 0 {
				if issuer != "" && issuer != i.Raw {
					issuer = ""
					break
				}
				issuer = i.Raw
				for _, v := range i.Evidence {
					evidence = append(evidence, digestJSON(v))
				}
			}
		}
		if issuer != "" {
			out = append(out, ProtectedIdentityValue{f.Subjects[0], f.Name, issuer, f.Raw, evidence})
		}
	}
	return out
}

// isBirthField recognizes exact equivalent semantic labels while preserving the
// original model field name and source quotation in every stored observation.
func isBirthField(name string) bool {
	switch name {
	case "birth_date", "date_of_birth", "出生日期", "出生年月日", "生日":
		return true
	}
	return false
}

// CanonicalFieldName maps only explicit equivalent labels used by deterministic
// identity/metric consumers. Open labels remain open. SourceName and immutable
// model products retain the original spelling; this never changes raw evidence.
func CanonicalFieldName(name string) string {
	switch name {
	case "birth_date", "date_of_birth", "出生日期", "出生年月日", "生日":
		return "birth_date"
	case "amount", "金额", "费用", "付款", "付款金额", "支付金额", "账单金额", "transaction_amount", "payment_amount":
		return "amount"
	case "volume", "奶量", "喂奶量", "feeding_volume":
		return "volume"
	case "bill_id", "bill_number", "bill_no", "账单编号", "账单号", "账单", "付款所引用账单编号", "引用账单编号":
		return "bill_id"
	case "issuer", "收款方", "收款人", "开票方", "payee":
		return "issuer"
	case "bill_date", "账单日期":
		return "bill_date"
	case "full_address", "房屋地址", "完整地址":
		return "full_address"
	default:
		return name
	}
}

// publicationReportSections reconciles only complete identical headings in an
// already-confirmed parser evidence group. Immutable model products and field
// evidence remain untouched; uncertainty in individual fields is not promoted.
func publicationReportSections(parsed ParseResult, events []MergedEvent) []MergedEvent {
	segments := map[string]Segment{}
	for _, s := range parsed.Segments {
		segments[s.ID] = s
	}
	keyFor := func(e MergedEvent) string {
		if e.Type != "report" || len(e.CanonicalEvidence) == 0 {
			return ""
		}
		parent := e.CanonicalEvidence[0].ParentSegmentID
		s, ok := segments[parent]
		if !ok || s.Locator.Page == nil || s.Visual == nil || s.Visual.State != "confirmed" || s.Visual.RequiresReview || len(s.Visual.Alternatives) < 2 {
			return ""
		}
		text := s.Visual.Alternatives[0].Text
		if text == "" || len([]rune(text)) > 128 || !reportTitleMarker.MatchString(text) {
			return ""
		}
		for _, a := range s.Visual.Alternatives {
			if a.Clipped || a.Text != text {
				return ""
			}
		}
		for _, r := range e.CanonicalEvidence {
			if r.ParentSegmentID != parent || r.Start != 0 || r.End != len([]rune(text)) || r.Quote != text || r.DerivedValueRef != "" {
				return ""
			}
		}
		return parent
	}
	out := []MergedEvent{}
	grouped := map[string]int{}
	for _, e := range events {
		key := keyFor(e)
		if i, ok := grouped[key]; key != "" && ok {
			seen := map[string]bool{}
			refs := append([]ScopedLocalReference(nil), out[i].FieldRefs...)
			for _, r := range refs {
				seen[digestJSON(r)] = true
			}
			for _, r := range e.FieldRefs {
				if !seen[digestJSON(r)] {
					refs = append(refs, r)
					seen[digestJSON(r)] = true
				}
			}
			out[i].FieldRefs = refs
		} else {
			if key != "" {
				grouped[key] = len(out)
			}
			out = append(out, e)
		}
	}
	return out
}
