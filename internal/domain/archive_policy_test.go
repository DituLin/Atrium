package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArchivePolicyVocabularyAndHouseholdValidation(t *testing.T) {
	for _, permission := range []string{"archive.read", "archive.ingest", "archive.organize", "archive.analyze"} {
		var p IntegrationPolicy
		require.NoError(t, json.Unmarshal([]byte(`{"permissions":["`+permission+`"],"household_ids":["home"]}`), &p))
		require.NoError(t, p.Validate())
		encoded, err := json.Marshal(p)
		require.NoError(t, err)
		require.Contains(t, string(encoded), `"household_ids":["home"]`)
	}
	for _, households := range []string{`["*"]`, `["home","home"]`, `[""]`} {
		var p IntegrationPolicy
		require.NoError(t, json.Unmarshal([]byte(`{"household_ids":`+households+`}`), &p))
		require.Error(t, p.Validate())
	}
}

func TestArchivePermissionsStayIndependent(t *testing.T) {
	p := IntegrationPolicy{Permissions: []string{"archive.ingest", "archive.analyze", "family.write"}, HouseholdIDs: []string{"home"}, MemberIDs: []string{"child"}}
	require.True(t, p.AllowsArchive("archive.ingest", "home"))
	require.False(t, p.AllowsArchive("archive.organize", "home"))
	require.False(t, p.AllowsArchive("archive.read", "home"))
	require.False(t, p.AllowsArchive("archive.analyze", "home"))
	require.False(t, p.AllowsArchive("family.write", "home"))
	require.False(t, p.AllowsArchive("archive.ingest", "child"))
	p.Permissions = append(p.Permissions, "archive.read")
	require.True(t, p.AllowsArchive("archive.analyze", "home"))
}
