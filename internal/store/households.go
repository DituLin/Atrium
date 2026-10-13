package store

import (
	"context"

	"github.com/DituLin/Atrium/internal/domain"
)

// LocalHousehold discovers the persisted local scope. No display configuration
// or integration grant changes this identity; discovery grants no archive access.
func (d *DB) LocalHousehold(ctx context.Context) (*domain.Household, error) {
	var h domain.Household
	if err := d.sql.QueryRowContext(ctx, `SELECT household_id FROM local_household WHERE singleton=1`).Scan(&h.ID); err != nil {
		return nil, err
	}
	return &h, nil
}
