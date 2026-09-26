package store_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
)

func TestOperationFailureRollsBackSequenceAndCommand(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := t.Context()
	now := time.Now()
	require.NoError(t, db.Screens().Create(ctx, &domain.Screen{ID: "tv", Name: "TV", TokenHash: "hash", Status: domain.ScreenActive, CreatedAt: now, ApprovedAt: now}))
	cmd := &domain.Command{ScreenID: "tv", Kind: domain.CommandRefresh, IssuedBy: "integration:missing", IssuedAt: now, ExpiresAt: now.Add(time.Minute), Status: domain.CommandAccepted}
	err := db.InWriteTx(ctx, func(tx *sql.Tx) error {
		if err := db.Commands().IssueInTx(ctx, tx, cmd); err != nil {
			return err
		}
		return db.Operations().WithTx(tx).Insert(ctx, "missing", domain.NewIDAt(now), "hash", cmd.ID, now)
	})
	require.Error(t, err)
	_, err = db.Commands().Get(ctx, cmd.ID)
	require.True(t, errors.Is(err, domain.ErrNotFound))
	require.NoError(t, db.Commands().Issue(ctx, cmd))
	require.EqualValues(t, 1, cmd.Sequence)
}
