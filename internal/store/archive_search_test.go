package store_test

import (
	"context"
	"database/sql"
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

func TestArchiveSearchSchema(t *testing.T) {
	d := testutil.NewDB(t)
	var n int
	err := d.SQL().QueryRowContext(context.Background(), `SELECT count(*) FROM sqlite_master WHERE name IN ('archive_search_rows','archive_search_fts','archive_search_state')`).Scan(&n)
	require.NoError(t, err)
	require.Equal(t, 3, n, "safe search projection and generation migration must exist")
}

func TestArchiveSearchCurrentEvidenceAndPending(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	rec, f, x := publicationFixture(t, d, r, "search", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, publicationBundle(t, "70"))
	_, err := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, err)
	p, err := r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Query: "70"}, []byte("test-key"))
	require.NoError(t, err)
	require.False(t, p.Coverage.Complete)
	_, err = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, err)
	p, err = r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Query: "70"}, []byte("test-key"))
	require.NoError(t, err)
	require.True(t, p.Coverage.Complete)
	require.Len(t, p.Items, 1)
	require.Equal(t, rec.DocumentID, p.Items[0].DocumentID)
	require.Equal(t, x.ID, p.Items[0].Evidence[0].SourceExtractionID)
	require.Equal(t, "70", p.Items[0].Evidence[0].Excerpt)
	require.Equal(t, "volume", p.Items[0].Evidence[0].FieldName)
	_, err = r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, 2, store.ArchiveRevision{Kind: "withdraw", Reason: "test"})
	require.NoError(t, err)
	p, err = r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Query: "70"}, []byte("test-key"))
	require.NoError(t, err)
	require.Empty(t, p.Items)
}

func TestArchiveSearchScreenRevocationAndCursorBinding(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	for _, msg := range []string{"first", "second", "third"} {
		_, f, x := publicationFixture(t, d, r, msg, domain.ArchiveInputText)
		f = saveBundle(t, r, f, publicationBundle(t, "70"))
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
	}
	_, e := indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	key := []byte("key")
	p, e := r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Limit: 1}, key)
	require.NoError(t, e)
	require.NotEmpty(t, p.NextCursor)
	next, e := r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Limit: 1, Cursor: p.NextCursor}, key)
	require.NoError(t, e)
	require.NotEqual(t, p.Items[0].DocumentID, next.Items[0].DocumentID)
	_, e = r.SearchArchives(ctx, a, "home-1", true, archive.SearchFilter{Limit: 1, Cursor: p.NextCursor}, key)
	require.Error(t, e)
	_, e = d.SQL().Exec(`UPDATE local_household SET household_id='home-1'`)
	require.NoError(t, e)
	require.NoError(t, d.Screens().Create(ctx, &domain.Screen{ID: "tv-search", Name: "TV", TokenHash: "hash"}))
	_, e = r.SearchArchivesForScreen(ctx, "tv-search", "hash", "home-1", archive.SearchFilter{}, key)
	require.NoError(t, e)
	require.NoError(t, d.Screens().Revoke(ctx, "tv-search", archiveNow))
	_, e = r.SearchArchivesForScreen(ctx, "tv-search", "hash", "home-1", archive.SearchFilter{}, key)
	require.Error(t, e)
}

func TestArchiveReadSnapshotDoesNotBlockWriter(t *testing.T) {
	d := testutil.NewDB(t)
	_ = archiveActor(t, d)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, d.InReadTx(ctx, func(tx *sql.Tx) error {
		var n int
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT count(*) FROM households`).Scan(&n))
		done := make(chan error, 1)
		go func() { _, e := d.SQL().ExecContext(ctx, `UPDATE households SET id=id`); done <- e }()
		select {
		case e := <-done:
			require.NoError(t, e)
		case <-ctx.Done():
			t.Fatal("reader held writer lock")
		}
		return nil
	}))
}

func TestArchiveSearchDirectEvidenceWithoutIndex(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	rec, f, x := publicationFixture(t, d, r, "pending-person", domain.ArchiveInputText)
	f = saveBundle(t, r, f, publicationBundle(t, "70"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	page, e := r.ArchiveEvidence(ctx, a, "home-1", false, rec.DocumentID, archive.SearchFilter{}, []byte("key"))
	require.NoError(t, e)
	require.Len(t, page.Items, 1)
	require.NotEmpty(t, page.Items[0].Evidence)
}

func TestArchiveSearchWaitingIncludesUnconfirmedEvidence(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	_, f, x := publicationFixture(t, d, r, "waiting", domain.ArchiveInputText)
	f = saveBundle(t, r, f, namedPublicationBundle(t, "妈妈"))
	task, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	require.Equal(t, domain.ArchiveTaskWaitingInput, task.Status)
	_, e = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	p, e := r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Query: "70", Ownership: "waiting"}, []byte("key"))
	require.NoError(t, e)
	require.Len(t, p.Items, 1)
	require.False(t, p.Items[0].Evidence[0].Confirmed)
	require.Empty(t, p.Items[0].EntityIDs)
}

func TestArchiveSearchEntitiesAndProtectedIdentity(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	_, f, x := publicationFixture(t, d, r, "person", domain.ArchiveInputText)
	f = saveBundle(t, r, f, namedPublicationBundle(t, "林明"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	page, e := r.SearchArchiveEntities(ctx, a, "home-1", false, archive.SearchFilter{Query: "林明"}, []byte("key"))
	require.NoError(t, e)
	require.Len(t, page.Items, 1)
	require.Contains(t, page.Items[0].Names, "林明")
	_, f, x = publicationFixture(t, d, r, "identity", domain.ArchiveInputText)
	f = saveBundle(t, r, f, literalBundle(t, "方圆", []publicationLiteral{{Name: "national_id", Value: "001234567890"}, {Name: "identity_issuer", Value: "CN"}, {Name: "amount", Value: "88"}}, "CNY"))
	_, e = r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	_, e = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	p, e := r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Query: "001234567890"}, []byte("key"))
	require.NoError(t, e)
	require.Empty(t, p.Items)
	var n int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_search_rows WHERE instr(text,'001234567890')>0 OR instr(body,'001234567890')>0`).Scan(&n))
	require.Zero(t, n)
}

func TestArchiveSearchFourDomainQuestions(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	key := []byte("four-domain-key")
	type specimen struct{ domain, kind, name, term, value, date, id string }
	fixtures := []specimen{
		{"health", "checkup", "林明", "血红蛋白", "145.6", "2026-06-15", ""},
		{"health", "feeding", "小安", "喂奶", "70", "2026-07-01", ""},
		{"education", "report_card", "李妍", "数学", "88", "2026-06-20", ""},
		{"education", "notice", "周青", "开学", "九月报到", "2026-09-01", ""},
		{"finance", "receipt", "王林", "水费", "128.10", "2026-07-02", ""},
		{"finance", "bill", "方圆", "电费", "71.90", "2026-07-03", ""},
		{"property", "warranty", "陈芳", "空调", "DEMO-AC", "2028-05-31", ""},
		{"property", "repair", "吴桐", "维修", "更换滤芯", "2026-08-04", ""},
	}
	for i := range fixtures {
		s := &fixtures[i]
		rec, f, x := publicationFixture(t, d, r, fmt.Sprint("four", i), domain.ArchiveInputText)
		b := literalBundle(t, s.name, []publicationLiteral{{Name: s.term, Value: s.value}, {Name: "report_date", Value: s.date}}, "")
		result := b.Understanding.Chunks[0].Result
		result.Domain = s.domain
		result.DocumentType = s.kind
		result.Fields[1].Normalizations = []archive.Normalization{{Kind: "date", Value: s.date}}
		b = revalidatePublication(t, b, result)
		f = saveBundle(t, r, f, b)
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
		s.id = rec.DocumentID
	}
	_, e := indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	// 32 independently asserted structured questions across the four domains.
	for i, s := range fixtures {
		cases := []archive.SearchFilter{
			{Query: s.name, Domain: s.domain}, {Query: s.term, DocumentType: s.kind},
			{Query: s.value, DateFrom: s.date, DateTo: s.date}, {DocumentType: s.kind, Ownership: "confirmed"},
		}
		for j, q := range cases {
			t.Run(fmt.Sprintf("Q%02d_%s", i*4+j+1, s.domain), func(t *testing.T) {
				p, e := r.SearchArchives(ctx, a, "home-1", false, q, key)
				require.NoError(t, e)
				require.Len(t, p.Items, 1)
				require.Equal(t, s.id, p.Items[0].DocumentID)
				require.NotEmpty(t, p.Items[0].Evidence)
				require.NotEmpty(t, p.Items[0].Evidence[0].SegmentID)
				require.NotEmpty(t, p.Items[0].Evidence[0].ComponentKind)
				hasFact := false
				for _, ev := range p.Items[0].Evidence {
					hasFact = hasFact || ev.Confirmed
				}
				require.True(t, hasFact, "source excerpts must remain distinguishable from confirmed facts")
			})
		}
	}
	for _, literal := range []string{"NOT x", "*", "\" OR 1=1 --", "' UNION SELECT", "未知档案"} {
		p, e := r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Query: literal}, key)
		require.NoError(t, e)
		require.Empty(t, p.Items)
		require.True(t, p.Coverage.Complete)
	}
}

func TestArchiveSearchLegacyCurrentChain(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	key := []byte("key")
	_, e := d.SQL().Exec(`UPDATE local_household SET household_id='home-1'`)
	require.NoError(t, e)
	require.NoError(t, d.FamilyRecords().PutMember(ctx, domain.FamilyMember{ID: "child", Name: "小安"}, archiveNow))
	input := domain.FamilyRecordInput{MemberID: "child", Category: "meal", Title: "喂奶", OccurredAt: archiveNow, DisplayOnTV: true, Measurements: []domain.FamilyMeasurement{{Metric: "volume", Value: 70, Unit: "ml"}}}
	first, _, e := d.FamilyRecords().Record(ctx, "p", "legacy-search", input, archiveNow)
	require.NoError(t, e)
	require.NoError(t, d.ProjectLegacyArchive(ctx, first.ID))
	p, e := r.SearchArchives(ctx, a, "home-1", true, archive.SearchFilter{Query: "喂奶"}, key)
	require.NoError(t, e)
	require.Len(t, p.Items, 1)
	p, e = r.SearchArchives(ctx, a, "home-1", true, archive.SearchFilter{Query: "小安"}, key)
	require.NoError(t, e)
	require.Len(t, p.Items, 1, "legacy name remains searchable")
	input.SupersedesID = first.ID
	input.RevisionReason = "private correction"
	input.DisplayOnTV = false
	input.Measurements[0].Value = 80
	second, _, e := d.FamilyRecords().Record(ctx, "p", "legacy-private", input, archiveNow)
	require.NoError(t, e)
	p, e = r.SearchArchives(ctx, a, "home-1", true, archive.SearchFilter{Query: "喂奶"}, key)
	require.NoError(t, e)
	require.Empty(t, p.Items, "stale projection cannot show old public data")
	require.NoError(t, d.ProjectLegacyArchive(ctx, second.ID))
	p, e = r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Query: "80"}, key)
	require.NoError(t, e)
	require.Len(t, p.Items, 1)
}

func TestArchiveSearchRebuildAndReplay(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	key := []byte("key")
	for i := 0; i < 3; i++ {
		_, f, x := publicationFixture(t, d, r, fmt.Sprint("rebuild", i), domain.ArchiveInputText)
		f = saveBundle(t, r, f, publicationBundle(t, "70"))
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
	}
	p, e := r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Limit: 1}, key)
	require.NoError(t, e)
	require.NotEmpty(t, p.NextCursor)
	_, e = r.IndexArchives(ctx, "home-1", 1)
	require.NoError(t, e)
	_, e = r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Limit: 1, Cursor: p.NextCursor}, key)
	require.Error(t, e, "index progress changes pagination snapshot")
	_, e = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	require.NoError(t, r.RebuildArchiveSearch(ctx, "home-1"))
	p, e = r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Query: "70"}, key)
	require.NoError(t, e)
	require.Empty(t, p.Items)
	require.False(t, p.Coverage.Complete)
	_, e = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	_, e = d.SQL().Exec(`UPDATE archive_outbox SET status='pending'`)
	require.NoError(t, e)
	_, e = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	var n int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_outbox WHERE status='pending'`).Scan(&n))
	require.Zero(t, n)
}

func TestArchiveSearchCurrentOlderExtractionPrivateAndAlias(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	key := []byte("key")
	rec, f, old := publicationFixture(t, d, r, "old", domain.ArchiveInputFile)
	b := literalBundle(t, "林明", []publicationLiteral{{Name: "amount", Value: "70"}, {Name: "note", Value: "保留说明"}}, "CNY")
	f = saveBundle(t, r, f, b)
	_, e := r.PublishExtraction(ctx, f, old.ID)
	require.NoError(t, e)
	_, e = d.SQL().Exec(`INSERT INTO archive_entity_attributes(household_id,id,entity_id,kind,value,precision,document_id,legacy_member_id,evidence_json,tv_visible,conflict,link_id) SELECT household_id,'search-alias',entity_id,'display_alias','小林','',document_id,NULL,evidence_json,1,0,link_id FROM archive_entity_attributes WHERE kind='name' AND document_id=? LIMIT 1`, rec.DocumentID)
	require.NoError(t, e)
	_, f, next := publicationFixture(t, d, r, "new", domain.ArchiveInputFile)
	result := b.Understanding.Chunks[0].Result
	result.Fields = result.Fields[1:]
	result.Events[0].FieldRefs = result.Events[0].FieldRefs[1:]
	partial := revalidatePublication(t, b, result)
	f = saveBundle(t, r, f, partial)
	_, e = r.PublishExtraction(ctx, f, next.ID)
	require.NoError(t, e)
	_, e = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	p, e := r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Query: "小林"}, key)
	require.NoError(t, e)
	require.Len(t, p.Items, 1)
	all, e := r.ArchiveEvidence(ctx, a, "home-1", false, rec.DocumentID, archive.SearchFilter{}, key)
	require.NoError(t, e)
	found := false
	obs := ""
	for _, ev := range all.Items[0].Evidence {
		if ev.Excerpt == "70" {
			found = true
			obs = ev.ObservationID
			require.Equal(t, old.ID, ev.SourceExtractionID)
			require.NotEqual(t, next.ID, ev.SourceExtractionID)
			require.NotEmpty(t, ev.PublicationID)
		}
	}
	require.True(t, found)
	_, e = r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, 3, store.ArchiveRevision{Kind: "private", ObservationID: obs, Reason: "private amount"})
	require.NoError(t, e)
	p, e = r.SearchArchives(ctx, a, "home-1", true, archive.SearchFilter{Query: "70"}, key)
	require.NoError(t, e)
	require.Empty(t, p.Items)
	_, e = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	p, e = r.SearchArchives(ctx, a, "home-1", true, archive.SearchFilter{Query: "70"}, key)
	require.NoError(t, e)
	require.Empty(t, p.Items)
	p, e = r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Query: "70"}, key)
	require.NoError(t, e)
	require.Len(t, p.Items, 1)
}

func TestArchiveSearchMultiSubjectAndDatePrecision(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	key := []byte("key")
	_, f, x := publicationFixture(t, d, r, "multi", domain.ArchiveInputText)
	b := literalBundle(t, "Alice Bob", []publicationLiteral{{Name: "address", Value: "测试苑1号"}, {Name: "date", Value: "2026-10"}}, "")
	result := b.Understanding.Chunks[0].Result
	first := result.Subjects[0]
	first.Name = "Alice"
	first.Evidence[0].Quote = "Alice"
	first.Evidence[0].End = 5
	second := first
	second.ID = "b"
	second.Name = "Bob"
	second.Evidence = append([]archive.EvidenceReference{}, first.Evidence...)
	second.Evidence[0].Start = 6
	second.Evidence[0].End = 9
	second.Evidence[0].Quote = "Bob"
	result.Subjects = []archive.LocalSubject{first, second}
	for i := range result.Fields {
		result.Fields[i].SubjectRefs = []string{"p", "b"}
	}
	result.Events[0].SubjectRefs = []string{"p", "b"}
	result.Domain = "property"
	result.DocumentType = "ownership"
	result.Fields[1].Normalizations = []archive.Normalization{{Kind: "date_partial", Value: "2026-10"}}
	b = revalidatePublication(t, b, result)
	f = saveBundle(t, r, f, b)
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	_, e = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	for _, name := range []string{"Alice", "Bob"} {
		p, e := r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Query: name}, key)
		require.NoError(t, e)
		require.Len(t, p.Items, 1)
		require.Len(t, p.Items[0].EntityIDs, 2)
	}
	p, e := r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{DateFrom: "2026-10-01", DateTo: "2026-10-31"}, key)
	require.NoError(t, e)
	require.Len(t, p.Items, 1, "the complete month is inside the requested interval")
	require.Equal(t, []string{"2026-10"}, p.Items[0].Dates, "retain source precision")
	p, e = r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{DateFrom: "2026-10-04", DateTo: "2026-10-04"}, key)
	require.NoError(t, e)
	require.Empty(t, p.Items, "overlapping month must not fabricate an exact day")
}

func TestArchiveSearchSerializedPageBudgetAndCredentialRotation(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	key := []byte("key")
	for i := 0; i < 18; i++ {
		_, f, x := publicationFixture(t, d, r, fmt.Sprintf("large-%d", i), domain.ArchiveInputText)
		b := literalBundle(t, "", []publicationLiteral{{Name: "long1", Value: strings.Repeat("长", 250)}, {Name: "long2", Value: strings.Repeat("字", 250)}, {Name: "long3", Value: strings.Repeat("段", 250)}}, "")
		f = saveBundle(t, r, f, b)
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
	}
	_, e := indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		p, e := r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Limit: 25, Cursor: cursor}, key)
		require.NoError(t, e)
		wire, e := json.Marshal(p)
		require.NoError(t, e)
		require.LessOrEqual(t, len(wire), 32<<10)
		for _, item := range p.Items {
			require.False(t, seen[item.DocumentID])
			seen[item.DocumentID] = true
		}
		pages++
		cursor = p.NextCursor
		if cursor == "" {
			break
		}
		require.Less(t, pages, 10)
	}
	require.Len(t, seen, 18)
	require.Greater(t, pages, 1)
	_, e = d.SQL().Exec(`UPDATE integration_credentials SET revoked_at=? WHERE id=?`, store.FormatTime(archiveNow), a.CredentialID)
	require.NoError(t, e)
	_, e = r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{}, key)
	require.Error(t, e)
}

func TestArchiveSearchProtectedRangeDoesNotEraseIndependentSameParagraphValue(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	_, f, x := publicationFixture(t, d, r, "range", domain.ArchiveInputText)
	f = saveBundle(t, r, f, literalBundle(t, "林明", []publicationLiteral{{Name: "passport", Value: "00123456"}, {Name: "identity_issuer", Value: "CN"}, {Name: "amount", Value: "145.6"}}, "CNY"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	_, e = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	p, e := r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Query: "145.6"}, []byte("key"))
	require.NoError(t, e)
	require.Len(t, p.Items, 1, "known ID range must not discard unrelated evidence from the paragraph")
}

func TestArchiveSearchSafeOriginalTextBeyondExtractedFacts(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	_, f, x := publicationFixture(t, d, r, "source-text", domain.ArchiveInputText)
	b := literalBundle(t, "", []publicationLiteral{{Name: "amount", Value: "70"}, {Name: "note", Value: "免费上门维修"}}, "CNY")
	result := b.Understanding.Chunks[0].Result
	result.Fields = result.Fields[:1]
	result.Events[0].FieldRefs = result.Events[0].FieldRefs[:1]
	b = revalidatePublication(t, b, result)
	f = saveBundle(t, r, f, b)
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	_, e = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	p, e := r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Query: "上门维修"}, []byte("key"))
	require.NoError(t, e)
	require.Len(t, p.Items, 1)
	found := false
	for _, ev := range p.Items[0].Evidence {
		if strings.Contains(ev.Excerpt, "上门维修") {
			found = true
			require.Equal(t, "original", ev.SourceKind)
			require.False(t, ev.Confirmed)
		}
	}
	require.True(t, found)
}

func TestArchiveSearchSameNameDoesNotInventConfirmedOwnership(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	key := []byte("key")
	for i := 0; i < 2; i++ {
		_, f, x := publicationFixture(t, d, r, fmt.Sprint("same-name", i), domain.ArchiveInputText)
		f = saveBundle(t, r, f, namedPublicationBundle(t, "张明"))
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
	}
	_, e := indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	p, e := r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Query: "张明"}, key)
	require.NoError(t, e)
	require.Len(t, p.Items, 2)
	entity := ""
	waiting := 0
	for _, item := range p.Items {
		if item.Ownership == "waiting" {
			waiting++
			require.Empty(t, item.EntityIDs)
		} else {
			require.Len(t, item.EntityIDs, 1)
			entity = item.EntityIDs[0]
		}
	}
	require.Equal(t, 1, waiting)
	p, e = r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{EntityID: entity}, key)
	require.NoError(t, e)
	require.Len(t, p.Items, 1)
}

func TestArchiveSearchSameFileSourcesRemainOneDocument(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	key := []byte("key")
	ids := []string{}
	for i := 0; i < 2; i++ {
		rec, f, x := publicationFixture(t, d, r, fmt.Sprint("source", i), domain.ArchiveInputFile)
		ids = append(ids, rec.DocumentID)
		f = saveBundle(t, r, f, publicationBundle(t, "70"))
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
	}
	require.Equal(t, ids[0], ids[1])
	_, e := indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	p, e := r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Query: "70"}, key)
	require.NoError(t, e)
	require.Len(t, p.Items, 1)
	var count int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_sources WHERE document_id=?`, ids[0]).Scan(&count))
	require.Equal(t, 2, count)
}

func TestArchiveSearchPartiallyBuiltFTSDoesNotFailOnMissingRows(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		_, f, x := publicationFixture(t, d, r, fmt.Sprint("partial-index", i), domain.ArchiveInputText)
		f = saveBundle(t, r, f, publicationBundle(t, "70"))
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
	}
	_, e := r.IndexArchives(ctx, "home-1", 1)
	require.NoError(t, e)
	p, e := r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{Query: "volume"}, []byte("key"))
	require.NoError(t, e)
	require.Len(t, p.Items, 1)
	require.False(t, p.Coverage.Complete)
}

func indexAllArchiveSearch(ctx context.Context, r *store.ArchiveTasks, h string) (int, error) {
	total := 0
	for pass := 0; pass < 100; pass++ {
		n, e := r.IndexArchives(ctx, h, 32)
		if e != nil {
			return total, e
		}
		total += n
		if n == 0 {
			return total, nil
		}
	}
	return total, fmt.Errorf("index fixture did not finish")
}

func TestArchiveSearchHouseholdNoticeDiffersFromUnassignedReport(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	key := []byte("key")
	for i, kind := range []string{"checkup", "school_notice"} {
		_, f, x := publicationFixture(t, d, r, fmt.Sprint("ownership", i), domain.ArchiveInputText)
		b := publicationBundle(t, "70")
		result := b.Understanding.Chunks[0].Result
		result.DocumentType = kind
		b = revalidatePublication(t, b, result)
		f = saveBundle(t, r, f, b)
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
	}
	_, e := indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	for _, q := range []archive.SearchFilter{{DocumentType: "checkup", Ownership: "unassigned"}, {DocumentType: "school_notice", Ownership: "household"}} {
		p, e := r.SearchArchives(ctx, a, "home-1", false, q, key)
		require.NoError(t, e)
		require.Len(t, p.Items, 1)
	}
}

func TestArchiveSearchCorrectedDatePrecision(t *testing.T) {
	for _, value := range []string{"2026-07-20", "2026-07", "2026", "2026-02-30", "2026-13", "26-07-20", "07-20", "日期未知", "", "2026-7-2"} {
		t.Run(value, func(t *testing.T) {
			ctx := context.Background()
			d := testutil.NewDB(t)
			r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
			a := archiveActor(t, d)
			rec, f, x := publicationFixture(t, d, r, "correct-date", domain.ArchiveInputText)
			b := literalBundle(t, "林明", []publicationLiteral{{Name: "report_date", Value: "2026-06-15"}}, "")
			result := b.Understanding.Chunks[0].Result
			result.Fields[0].Normalizations = []archive.Normalization{{Kind: "date", Value: "2026-06-15"}}
			b = revalidatePublication(t, b, result)
			f = saveBundle(t, r, f, b)
			_, err := r.PublishExtraction(ctx, f, x.ID)
			require.NoError(t, err)
			obs, err := r.CurrentArchiveObservations(ctx, a, "home-1", rec.DocumentID, false)
			require.NoError(t, err)
			require.Len(t, obs, 1)
			rev, err := r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, 2, store.ArchiveRevision{Kind: "value", ObservationID: obs[0].ID, Value: value, Reason: "日期更正"})
			require.NoError(t, err)
			_, err = indexAllArchiveSearch(ctx, r, "home-1")
			require.NoError(t, err)
			valid := value == "2026-07-20" || value == "2026-07" || value == "2026"
			for _, public := range []bool{false, true} {
				page, err := r.SearchArchives(ctx, a, "home-1", public, archive.SearchFilter{}, []byte("key"))
				require.NoError(t, err)
				require.Len(t, page.Items, 1)
				if valid {
					require.Equal(t, []string{value}, page.Items[0].Dates)
				} else {
					require.Empty(t, page.Items[0].Dates)
					require.True(t, page.Items[0].ReviewRequired)
					require.False(t, page.Coverage.Complete)
				}
				old, err := r.SearchArchives(ctx, a, "home-1", public, archive.SearchFilter{DateFrom: "2026-06-15", DateTo: "2026-06-15"}, []byte("key"))
				require.NoError(t, err)
				require.Empty(t, old.Items)
				current, err := r.SearchArchives(ctx, a, "home-1", public, archive.SearchFilter{DateFrom: "2026-07-20", DateTo: "2026-07-20"}, []byte("key"))
				require.NoError(t, err)
				if value == "2026-07-20" {
					require.Len(t, current.Items, 1)
				} else {
					require.Empty(t, current.Items, "partial dates must not fabricate a day")
				}
				ev, err := r.ArchiveEvidence(ctx, a, "home-1", public, rec.DocumentID, archive.SearchFilter{}, []byte("key"))
				require.NoError(t, err)
				found := false
				for _, e := range ev.Items[0].Evidence {
					if e.ObservationID == obs[0].ID {
						found = true
						require.Equal(t, "2026-06-15", e.Excerpt)
						require.Equal(t, value, e.AssertedValue)
						require.Equal(t, []archive.Normalization{{Kind: "date", Value: "2026-06-15"}}, e.SourceNormalizations)
						require.NotEmpty(t, e.AssertionID)
					}
				}
				require.True(t, found)
			}
			_, err = r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, rev, store.ArchiveRevision{Kind: "private", ObservationID: obs[0].ID, Reason: "日期仅家人可见"})
			require.NoError(t, err)
			_, err = indexAllArchiveSearch(ctx, r, "home-1")
			require.NoError(t, err)
			public, err := r.SearchArchives(ctx, a, "home-1", true, archive.SearchFilter{}, []byte("key"))
			require.NoError(t, err)
			require.Len(t, public.Items, 1)
			require.Empty(t, public.Items[0].Dates)
			for _, e := range public.Items[0].Evidence {
				require.Empty(t, e.AssertionID)
			}
			personal, err := r.SearchArchives(ctx, a, "home-1", false, archive.SearchFilter{}, []byte("key"))
			require.NoError(t, err)
			require.Len(t, personal.Items, 1)
			if valid {
				require.Equal(t, []string{value}, personal.Items[0].Dates)
			} else {
				require.Empty(t, personal.Items[0].Dates)
			}
		})
	}
}
