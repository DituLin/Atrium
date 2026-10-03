package store_test

import (
	"context"
	"testing"

	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
)

func TestLocalHouseholdStableAcrossReopenAndDisplayNameChange(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDB(t)
	var first string
	require.NoError(t, db.SQL().QueryRowContext(ctx, `SELECT household_id FROM local_household WHERE singleton=1`).Scan(&first))
	require.NotEmpty(t, first)
	require.NoError(t, db.Settings().Set(ctx, "home.name", "Changed Home"))
	require.NoError(t, db.Close())
	reopened, err := store.Open(ctx, db.Path())
	require.NoError(t, err)
	defer reopened.Close()
	n, err := reopened.Migrate(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	var second string
	require.NoError(t, reopened.SQL().QueryRowContext(ctx, `SELECT household_id FROM local_household WHERE singleton=1`).Scan(&second))
	require.Equal(t, first, second)
	other := testutil.NewDB(t)
	var independent string
	require.NoError(t, other.SQL().QueryRowContext(ctx, `SELECT household_id FROM local_household WHERE singleton=1`).Scan(&independent))
	require.NotEqual(t, first, independent)
}
