package db

import (
	"context"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// ProjectionTables are the tables derived from thread logs (docs/04
// "Projection tables"), in the order migrations 0004, 0007, 0020, 0025, 0026
// and 0027 create them. Every row in them can be rebuilt by `ketju reindex`,
// and nothing else in the database can: the rest is authoritative and must
// be backed up.
//
// TestProjectionTablesAreMarked holds this list equal to the tables whose
// schema carries the PROJECTION marker, so a new projection must add itself
// here or fail the tests. The search index is not one: it is SearchTables,
// which ClearProjections leaves alone (search.go, TKT-01M29CRJ3B).
var ProjectionTables = []string{"threads", "thread_tags", "post_meta", "mentions", "projection_state",
	"forum_thread_counts", "post_attachments", "post_quotes", "post_moves", "thread_continuations"}

// ClearProjections empties every projection table, for reindex to replay the
// logs into. Run it inside a Write so the tables are never seen half-empty.
//
// It empties rather than drops. The tables' DDL lives in migrations 0004,
// 0007, 0020, 0025, 0026 and 0027 and nowhere else; dropping them would need a
// second copy of it to recreate them, because the migration runner does not
// re-run a recorded migration.
func ClearProjections(ctx context.Context, q *dbq.Queries) error {
	for _, clear := range []func(context.Context) error{
		q.ClearThreads, q.ClearThreadTags, q.ClearPostMeta, q.ClearMentions, q.ClearProjectionState,
		// Emptied by ClearThreads' triggers already; cleared by name as well,
		// so a count that had drifted cannot survive a reindex.
		q.ClearForumThreadCounts,
		q.ClearPostAttachments,
		q.ClearPostQuotes,
		q.ClearPostMoves,
		q.ClearThreadContinuations,
	} {
		if err := clear(ctx); err != nil {
			return err
		}
	}
	return nil
}
