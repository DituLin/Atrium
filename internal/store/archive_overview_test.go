package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
)

func overviewScreen(t *testing.T, d *store.DB) {
	t.Helper()
	_, err := d.SQL().Exec(`UPDATE local_household SET household_id='home-1'`)
	require.NoError(t, err)
	require.NoError(t, d.Screens().Create(context.Background(), &domain.Screen{ID: "overview-tv", Name: "Synthetic TV", TokenHash: "hash"}))
}

func TestArchiveOverviewCurrentPublicDocumentsAndPendingOwnership(t *testing.T) {
	ctx := context.Background()
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	actor := archiveActor(t, d)
	overviewScreen(t, d)
	var hidden, withdrawn string
	for _, tc := range []struct{ message, name, kind string }{
		{"named", "合成甲", "record"}, {"household", "", "school_notice"},
		{"unassigned", "", "record"}, {"waiting", "妈妈", "record"},
		{"private", "秘密人物", "record"}, {"withdrawn", "撤回人物", "record"},
	} {
		rec, fence, extraction := publicationFixture(t, d, r, tc.message, domain.ArchiveInputText)
		bundle := publicationBundle(t, "70")
		if tc.name != "" {
			bundle = namedPublicationBundle(t, tc.name)
		}
		result := bundle.Understanding.Chunks[0].Result
		result.DocumentType = tc.kind
		bundle = revalidatePublication(t, bundle, result)
		fence = saveBundle(t, r, fence, bundle)
		_, err := r.PublishExtraction(ctx, fence, extraction.ID)
		require.NoError(t, err)
		if tc.message == "private" {
			hidden = rec.DocumentID
		}
		if tc.message == "withdrawn" {
			withdrawn = rec.DocumentID
		}
	}
	_, err := d.SQL().Exec(`UPDATE archive_documents SET tv_visible=0 WHERE id=?`, hidden)
	require.NoError(t, err)
	_, err = r.ReviseArchive(ctx, actor, "home-1", withdrawn, 2, store.ArchiveRevision{Kind: "withdraw", Reason: "synthetic withdrawal"})
	require.NoError(t, err)
	before, err := r.ArchiveOverviewForScreen(ctx, "overview-tv", "hash", "home-1")
	require.NoError(t, err)
	require.Equal(t, 4, before.Total)
	require.Zero(t, before.Confirmed)
	require.Equal(t, 4, before.Pending)
	require.False(t, before.Coverage.Complete)
	require.Equal(t, "index_pending", before.Coverage.Reason)
	_, err = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, err)
	got, err := r.ArchiveOverviewForScreen(ctx, "overview-tv", "hash", "home-1")
	require.NoError(t, err)
	require.Equal(t, 4, got.Total)
	require.Equal(t, 2, got.Confirmed)
	require.Equal(t, 2, got.Pending)
	require.True(t, got.Coverage.Complete, "pending documents must not make document counts incomplete")
	require.Equal(t, 1, got.Ownership.Household)
	require.Equal(t, 1, got.Ownership.Waiting)
	require.Equal(t, 1, got.Ownership.Unassigned)
	require.Equal(t, []archive.OverviewDomain{{Domain: "health", Count: 4}}, got.Domains)
	require.Len(t, got.Entities, 1)
	require.Equal(t, 1, got.Entities[0].Count)
	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), hidden)
	require.NotContains(t, string(encoded), withdrawn)
	require.NotContains(t, string(encoded), "秘密")
	var epoch, generation int64
	require.NoError(t, d.SQL().QueryRow(`SELECT epoch,generation FROM archive_search_state WHERE household_id='home-1'`).Scan(&epoch, &generation))
	require.Equal(t, fmt.Sprintf("%d:%d", epoch, generation), got.Generation)
	require.NoError(t, r.RebuildArchiveSearch(ctx, "home-1"))
	rebuilt, err := r.ArchiveOverviewForScreen(ctx, "overview-tv", "hash", "home-1")
	require.NoError(t, err)
	require.Equal(t, got.Total, rebuilt.Total)
	require.Zero(t, rebuilt.Confirmed, "stale projections must never count as confirmed")
	require.NotEqual(t, got.Generation, rebuilt.Generation)
	require.False(t, rebuilt.Coverage.Complete)
	require.NoError(t, d.Screens().Revoke(ctx, "overview-tv", archiveNow))
	_, err = r.ArchiveOverviewForScreen(ctx, "overview-tv", "hash", "home-1")
	require.Error(t, err)
}

func TestArchiveOverviewLegacyLeavesDoNotDoubleCountAndKeepPrivateEntitiesHidden(t *testing.T) {
	ctx := context.Background()
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	_ = archiveActor(t, d)
	overviewScreen(t, d)
	require.NoError(t, d.FamilyRecords().PutMember(ctx, domain.FamilyMember{ID: "synthetic-public", Name: "合成家人"}, archiveNow))
	require.NoError(t, d.FamilyRecords().PutMember(ctx, domain.FamilyMember{ID: "synthetic-private", Name: "秘密家人"}, archiveNow))
	input := domain.FamilyRecordInput{MemberID: "synthetic-public", Category: "growth", Title: "合成记录", OccurredAt: archiveNow, DisplayOnTV: true}
	first, _, err := d.FamilyRecords().Record(ctx, "synthetic", "original", input, archiveNow)
	require.NoError(t, err)
	require.NoError(t, d.ProjectLegacyArchive(ctx, first.ID))
	input.SupersedesID = first.ID
	input.RevisionReason = "synthetic correction"
	second, _, err := d.FamilyRecords().Record(ctx, "synthetic", "correction", input, archiveNow)
	require.NoError(t, err)
	// The newer visible leaf is pending until its current projection is rebuilt.
	pending, err := r.ArchiveOverviewForScreen(ctx, "overview-tv", "hash", "home-1")
	require.NoError(t, err)
	require.Equal(t, 1, pending.Total)
	require.Equal(t, 1, pending.Pending)
	require.False(t, pending.Coverage.Complete)
	require.NoError(t, d.ProjectLegacyArchive(ctx, second.ID))
	input.SupersedesID = ""
	input.RevisionReason = ""
	input.MemberID = "synthetic-private"
	input.DisplayOnTV = false
	secret, _, err := d.FamilyRecords().Record(ctx, "synthetic", "private", input, archiveNow)
	require.NoError(t, err)
	require.NoError(t, d.ProjectLegacyArchive(ctx, secret.ID))
	got, err := r.ArchiveOverviewForScreen(ctx, "overview-tv", "hash", "home-1")
	require.NoError(t, err)
	require.Equal(t, 1, got.Total)
	require.Equal(t, 1, got.Confirmed)
	require.Zero(t, got.Pending)
	require.True(t, got.Coverage.Complete)
	require.Len(t, got.Entities, 1)
	require.Equal(t, 1, got.Entities[0].Count)
	var privateEntity string
	require.NoError(t, d.SQL().QueryRow(`SELECT entity_id FROM archive_legacy_projections WHERE current_record_id=?`, secret.ID).Scan(&privateEntity))
	wire, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(wire), privateEntity)
	input.MemberID = "synthetic-public"
	input.DisplayOnTV = false
	input.SupersedesID = second.ID
	input.RevisionReason = "synthetic retraction"
	input.Retracted = true
	_, _, err = d.FamilyRecords().Record(ctx, "synthetic", "retraction", input, archiveNow)
	require.NoError(t, err)
	got, err = r.ArchiveOverviewForScreen(ctx, "overview-tv", "hash", "home-1")
	require.NoError(t, err)
	require.Zero(t, got.Total)
	require.Empty(t, got.Entities)
}

func TestArchiveOverviewCountsEntityOncePerDocumentAndBoundsWork(t *testing.T) {
	ctx := context.Background()
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	_ = archiveActor(t, d)
	overviewScreen(t, d)
	rec, f, x := publicationFixture(t, d, r, "repeated-entity", domain.ArchiveInputText)
	f = saveBundle(t, r, f, namedPublicationBundle(t, "合成甲"))
	_, err := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, err)
	_, err = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, err)
	_, err = d.SQL().Exec(`UPDATE archive_search_rows SET body=json_set(body,'$.entity_ids',json_array(json_extract(body,'$.entity_ids[0]'),json_extract(body,'$.entity_ids[0]'))) WHERE public=1 AND document_id=?`, rec.DocumentID)
	require.NoError(t, err)
	got, err := r.ArchiveOverviewForScreen(ctx, "overview-tv", "hash", "home-1")
	require.NoError(t, err)
	require.Len(t, got.Entities, 1)
	require.Equal(t, 1, got.Entities[0].Count)
	_, err = d.SQL().Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<1001) INSERT INTO archive_documents(id,household_id,blob_sha256,input_kind,tv_visible,created_at) SELECT 'budget-'||i,household_id,blob_sha256,'text',1,created_at FROM archive_documents,n WHERE id=?`, rec.DocumentID)
	require.NoError(t, err)
	got, err = r.ArchiveOverviewForScreen(ctx, "overview-tv", "hash", "home-1")
	require.NoError(t, err)
	require.Equal(t, 1002, got.Total)
	require.Equal(t, 1002, got.Pending)
	require.False(t, got.Coverage.Complete)
	require.Equal(t, "scan_budget", got.Coverage.Reason)
}

func TestArchiveOverviewDoesNotExposeEntityWhoseOnlyNameIsPrivate(t *testing.T) {
	ctx := context.Background()
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	_ = archiveActor(t, d)
	overviewScreen(t, d)
	var visibleDocument, privateDocument string
	for _, tc := range []struct{ message, name string }{{"visible-reference", "公开人物"}, {"private-label", "秘密人物"}} {
		rec, f, x := publicationFixture(t, d, r, tc.message, domain.ArchiveInputText)
		f = saveBundle(t, r, f, namedPublicationBundle(t, tc.name))
		_, err := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, err)
		if tc.message == "visible-reference" {
			visibleDocument = rec.DocumentID
		} else {
			privateDocument = rec.DocumentID
		}
	}
	_, err := d.SQL().Exec(`UPDATE archive_documents SET tv_visible=0 WHERE id=?`, privateDocument)
	require.NoError(t, err)
	_, err = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, err)
	var privateID string
	require.NoError(t, d.SQL().QueryRow(`SELECT entity_id FROM archive_entity_attributes WHERE document_id=? AND kind='name'`, privateDocument).Scan(&privateID))
	// A public field may point to an entity whose labels only have private
	// provenance. The overview must independently suppress the private ID.
	_, err = d.SQL().Exec(`UPDATE archive_search_rows SET body=json_set(body,'$.entity_ids',json_array(?)) WHERE document_id=? AND public=1`, privateID, visibleDocument)
	require.NoError(t, err)
	got, err := r.ArchiveOverviewForScreen(ctx, "overview-tv", "hash", "home-1")
	require.NoError(t, err)
	require.Equal(t, 1, got.Total)
	require.Zero(t, got.Confirmed)
	require.Equal(t, 1, got.Pending)
	require.Empty(t, got.Entities)
	require.Equal(t, 1, got.Ownership.Unassigned)
	_, err = r.ArchiveOverviewForScreen(ctx, "overview-tv", "wrong-hash", "home-1")
	require.Error(t, err)
	_, err = r.ArchiveOverviewForScreen(ctx, "overview-tv", "hash", "other-household")
	require.Error(t, err)
}

func TestArchiveOverviewOversizeProjectionDiscardsPartialCounts(t *testing.T) {
	ctx := context.Background()
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	_ = archiveActor(t, d)
	overviewScreen(t, d)
	rec, f, x := publicationFixture(t, d, r, "oversize-overview", domain.ArchiveInputText)
	f = saveBundle(t, r, f, namedPublicationBundle(t, "合成甲"))
	_, err := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, err)
	_, err = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, err)
	_, err = d.SQL().Exec(`UPDATE archive_search_rows SET body=json_set(body,'$.padding',replace(hex(zeroblob(65537)),'0','a')) WHERE document_id=? AND public=1`, rec.DocumentID)
	require.NoError(t, err)
	got, err := r.ArchiveOverviewForScreen(ctx, "overview-tv", "hash", "home-1")
	require.NoError(t, err)
	require.Equal(t, 1, got.Total)
	require.Zero(t, got.Confirmed)
	require.Equal(t, 1, got.Pending)
	require.False(t, got.Coverage.Complete)
	require.Equal(t, "scan_budget", got.Coverage.Reason)
	require.Empty(t, got.Domains)
	require.Empty(t, got.Entities)
}

func TestArchiveOverviewTruncatedProjectionDoesNotClaimCompleteEntityCounts(t *testing.T) {
	ctx := context.Background()
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	_ = archiveActor(t, d)
	overviewScreen(t, d)
	rec, f, x := publicationFixture(t, d, r, "truncated-overview", domain.ArchiveInputText)
	f = saveBundle(t, r, f, namedPublicationBundle(t, "合成甲"))
	_, err := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, err)
	_, err = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, err)
	_, err = d.SQL().Exec(`UPDATE archive_search_rows SET body=json_set(body,'$.truncated',json('true')) WHERE document_id=? AND public=1`, rec.DocumentID)
	require.NoError(t, err)
	got, err := r.ArchiveOverviewForScreen(ctx, "overview-tv", "hash", "home-1")
	require.NoError(t, err)
	require.Equal(t, 1, got.Total)
	require.Zero(t, got.Confirmed)
	require.Equal(t, 1, got.Pending)
	require.False(t, got.Coverage.Complete)
	require.Equal(t, "projection_partial", got.Coverage.Reason)
}

// Large alias/name evidence is irrelevant to overview: the projection needs
// only the public name's entity ID, not the label or evidence payload itself.
func TestArchiveOverviewIgnoresLargeAliasPayloads(t *testing.T) {
	ctx := context.Background()
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	_ = archiveActor(t, d)
	overviewScreen(t, d)
	require.NoError(t, d.FamilyRecords().PutMember(ctx, domain.FamilyMember{ID: "synthetic-budget", Name: "Synthetic"}, archiveNow))
	first, _, err := d.FamilyRecords().Record(ctx, "synthetic", "original", domain.FamilyRecordInput{MemberID: "synthetic-budget", Category: "growth", Title: "Synthetic", OccurredAt: archiveNow, DisplayOnTV: true}, archiveNow)
	require.NoError(t, err)
	require.NoError(t, d.ProjectLegacyArchive(ctx, first.ID))
	_, err = d.SQL().Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<600) INSERT INTO archive_entity_attributes(household_id,id,entity_id,kind,value,precision,legacy_member_id,evidence_json,tv_visible,conflict) SELECT household_id,'budget-alias-'||i,entity_id,'display_alias',replace(hex(zeroblob(1024)),'0','a'),' ',legacy_member_id,'["synthetic"]',1,0 FROM archive_legacy_entities,n WHERE legacy_member_id='synthetic-budget'`)
	require.NoError(t, err)
	var attributeBytes int
	require.NoError(t, d.SQL().QueryRow(`SELECT sum(length(value)+length(evidence_json)) FROM archive_entity_attributes`).Scan(&attributeBytes))
	require.Greater(t, attributeBytes, 1<<20)
	got, err := r.ArchiveOverviewForScreen(ctx, "overview-tv", "hash", "home-1")
	require.NoError(t, err)
	require.Equal(t, 1, got.Total)
	require.Equal(t, 1, got.Confirmed)
	require.True(t, got.Coverage.Complete, "unread aliases must not consume the overview budget")
	// Unrelated alias evidence need not be parsed at all. It is valid JSON but
	// intentionally has no entity-attribute evidence schema.
	_, err = d.SQL().Exec(`UPDATE archive_entity_attributes SET evidence_json='{}' WHERE kind='display_alias'`)
	require.NoError(t, err)
	got, err = r.ArchiveOverviewForScreen(ctx, "overview-tv", "hash", "home-1")
	require.NoError(t, err, "overview must never decode unused alias evidence")
	require.True(t, got.Coverage.Complete)
}

func TestArchiveOverviewPublicNameIDsHaveByteAndCountBounds(t *testing.T) {
	for _, tc := range []struct {
		name           string
		count, idBytes int
	}{{"bytes", 1, 129}, {"count", 257, 30}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			d := testutil.NewDB(t)
			r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
			_ = archiveActor(t, d)
			overviewScreen(t, d)
			require.NoError(t, d.FamilyRecords().PutMember(ctx, domain.FamilyMember{ID: "synthetic-budget", Name: "Synthetic"}, archiveNow))
			first, _, err := d.FamilyRecords().Record(ctx, "synthetic", "original", domain.FamilyRecordInput{MemberID: "synthetic-budget", Category: "growth", Title: "Synthetic", OccurredAt: archiveNow, DisplayOnTV: true}, archiveNow)
			require.NoError(t, err)
			require.NoError(t, d.ProjectLegacyArchive(ctx, first.ID))
			_, err = d.SQL().Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<?) INSERT INTO archive_entities(household_id,id,kind,version) SELECT 'home-1',printf('%0*d',?,i),'person',1 FROM n`, tc.count, tc.idBytes)
			require.NoError(t, err)
			_, err = d.SQL().Exec(`INSERT INTO archive_entity_attributes(household_id,id,entity_id,kind,value,precision,legacy_member_id,evidence_json,tv_visible,conflict) SELECT household_id,'name-'||id,id,'name','Synthetic','','synthetic-budget','[]',1,0 FROM archive_entities WHERE id LIKE '0%'`)
			require.NoError(t, err)
			got, err := r.ArchiveOverviewForScreen(ctx, "overview-tv", "hash", "home-1")
			require.NoError(t, err)
			require.Equal(t, 1, got.Total)
			require.Equal(t, 1, got.Pending)
			require.Zero(t, got.Confirmed)
			require.False(t, got.Coverage.Complete)
			require.Equal(t, "entity_budget", got.Coverage.Reason)
			require.Empty(t, got.Entities)
		})
	}
}

func TestArchiveOverviewRedirectReadsStayWithinIdentityBudget(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		groups, depth, idBytes int
		complete               bool
	}{{"canonical", 1, 2, 26, true}, {"depth", 1, 65, 26, false}, {"bytes", 9, 60, 128, false}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			d := testutil.NewDB(t)
			r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
			_ = archiveActor(t, d)
			overviewScreen(t, d)
			require.NoError(t, d.FamilyRecords().PutMember(ctx, domain.FamilyMember{ID: "synthetic-budget", Name: "Synthetic"}, archiveNow))
			first, _, err := d.FamilyRecords().Record(ctx, "synthetic", "original", domain.FamilyRecordInput{MemberID: "synthetic-budget", Category: "growth", Title: "Synthetic", OccurredAt: archiveNow, DisplayOnTV: true}, archiveNow)
			require.NoError(t, err)
			require.NoError(t, d.ProjectLegacyArchive(ctx, first.ID))
			_, err = d.SQL().Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<?) INSERT INTO archive_entities(household_id,id,kind,version) SELECT 'home-1',printf('%0*d',?,i),'person',1 FROM n`, tc.groups*tc.depth, tc.idBytes)
			require.NoError(t, err)
			_, err = d.SQL().Exec(`INSERT INTO archive_entity_merges(household_id,id,source_id,target_id,version,active,actor,reason,created_revision) SELECT household_id,'budget-merge-'||id,id,printf('%0*d',?,CAST(id AS INTEGER)+1),1,1,'synthetic','synthetic',1 FROM archive_entities WHERE id LIKE '0%' AND CAST(id AS INTEGER)%?!=0`, tc.idBytes, tc.depth)
			require.NoError(t, err)
			_, err = d.SQL().Exec(`INSERT INTO archive_entity_redirects(household_id,source_id,target_id,merge_id) SELECT household_id,source_id,target_id,id FROM archive_entity_merges`)
			require.NoError(t, err)
			_, err = d.SQL().Exec(`INSERT INTO archive_entity_attributes(household_id,id,entity_id,kind,value,precision,legacy_member_id,evidence_json,tv_visible,conflict) SELECT household_id,'name-'||id,id,'name','Synthetic','','synthetic-budget','[]',1,0 FROM archive_entities WHERE id LIKE '0%' AND CAST(id AS INTEGER)%?=1`, tc.depth)
			require.NoError(t, err)
			_, err = d.SQL().Exec(`UPDATE archive_legacy_projections SET entity_id=?`, fmt.Sprintf("%0*d", tc.idBytes, 1))
			require.NoError(t, err)
			got, err := r.ArchiveOverviewForScreen(ctx, "overview-tv", "hash", "home-1")
			require.NoError(t, err)
			require.Equal(t, 1, got.Total)
			require.Equal(t, tc.complete, got.Coverage.Complete)
			if tc.complete {
				require.Equal(t, 1, got.Confirmed)
				require.Equal(t, []archive.OverviewEntity{{ID: fmt.Sprintf("%0*d", tc.idBytes, tc.depth), Count: 1}}, got.Entities)
			} else {
				require.Equal(t, 1, got.Pending)
				require.Zero(t, got.Confirmed)
				require.Equal(t, "entity_budget", got.Coverage.Reason)
				require.Empty(t, got.Entities)
			}
		})
	}
}

func TestArchiveOverviewKeepsOtherAndUnknownDomainsInComposition(t *testing.T) {
	ctx := context.Background()
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	_ = archiveActor(t, d)
	overviewScreen(t, d)
	for _, domainName := range []string{"other", "unknown"} {
		rec, f, x := publicationFixture(t, d, r, "overview-domain-"+domainName, domain.ArchiveInputText)
		f = saveBundle(t, r, f, publicationBundle(t, "70"))
		_, err := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, err)
		_, err = indexAllArchiveSearch(ctx, r, "home-1")
		require.NoError(t, err)
		_, err = d.SQL().Exec(`UPDATE archive_search_rows SET body=json_set(body,'$.domain',?) WHERE document_id=? AND public=1`, domainName, rec.DocumentID)
		require.NoError(t, err)
	}
	// The second publication invalidated the first index epoch. Bring both
	// rows current, then assign the two synthetic public domain classifications.
	_, err := indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, err)
	_, err = d.SQL().Exec(`UPDATE archive_search_rows SET body=json_set(body,'$.domain',CASE WHEN document_id=(SELECT min(document_id) FROM archive_search_rows) THEN 'other' ELSE 'unknown' END) WHERE public=1`)
	require.NoError(t, err)
	got, err := r.ArchiveOverviewForScreen(ctx, "overview-tv", "hash", "home-1")
	require.NoError(t, err)
	require.Equal(t, 2, got.Total)
	require.Equal(t, []archive.OverviewDomain{{Domain: "other", Count: 1}, {Domain: "unknown", Count: 1}}, got.Domains)
	require.True(t, got.Coverage.Complete)
}
