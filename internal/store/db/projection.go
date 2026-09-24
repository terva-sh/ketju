package db

import (
	"context"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// ProjectionTables are the tables derived from thread logs (docs/04
// "Projection tables"), in the order migration 0004 creates them. Every row in
// them can be rebuilt by `ketju reindex`, and nothing else in the database
// can: the rest is authoritative and must be backed up.
//
// TestProjectionTablesAreMarked holds this list equal to the tables whose
// schema carries the PROJECTION marker, so a new projection (the search
// index, TKT-01M29CRJ3B) must add itself here or fail the tests.
var ProjectionTables = []string{"threads", "thread_tags", "post_meta", "mentions", "projection_state"}

// ClearProjections empties every projection table, for reindex to replay the
// logs into. Run it inside a Write so the tables are never seen half-empty.
//
// It empties rather than drops. The tables' DDL lives in migration 0004 and
// nowhere else; dropping them would need a second copy of it to recreate
// them, because the migration runner does not re-run a recorded migration.
func ClearProjections(ctx context.Context, q *dbq.Queries) error {
	for _, clear := range []func(context.Context) error{
		q.ClearThreads, q.ClearThreadTags, q.ClearPostMeta, q.ClearMentions, q.ClearProjectionState,
	} {
		if err := clear(ctx); err != nil {
			return err
		}
	}
	return nil
}
