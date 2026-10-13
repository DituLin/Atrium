package store

import (
	"context"
	"strings"
	"testing"
)

func TestSchemaRejectsNonPrefix(t *testing.T) {
	for _, q := range []string{`INSERT INTO schema_migrations VALUES(9999,'future','')`, `UPDATE schema_migrations SET name='wrong' WHERE version=2`, `DELETE FROM schema_migrations WHERE version=2`} {
		t.Run(q, func(t *testing.T) {
			ctx := context.Background()
			d, e := OpenMemory(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer d.Close()
			if _, e = d.Migrate(ctx); e != nil {
				t.Fatal(e)
			}
			if _, e = d.sql.ExecContext(ctx, q); e != nil {
				t.Fatal(e)
			}
			if _, e = d.Migrate(ctx); e == nil {
				t.Fatal("accepted incompatible schema")
			} else if !strings.Contains(e.Error(), "incompatible schema") {
				t.Fatalf("not rejected by schema guard: %v", e)
			}
		})
	}
}
