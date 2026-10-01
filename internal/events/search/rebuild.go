package search

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// logFault is an indexing failure the log explains: its bytes or its
// records. Any other failure is the database's or the filesystem's.
type logFault struct{ err error }

func (e logFault) Error() string { return e.err.Error() }
func (e logFault) Unwrap() error { return e.err }

// IsLogFault reports whether an indexing failure is the log's own. Rebuild
// reports such a thread and goes on; it stops on any other failure.
func IsLogFault(err error) bool {
	var f logFault
	return errors.As(err, &f)
}

// RebuildProgress is how far a Rebuild has got. Done and Bytes count the
// threads looked at so far, indexed or not, out of Total and TotalBytes,
// which the walk found before anything was indexed.
type RebuildProgress struct {
	Done, Total       int
	Bytes, TotalBytes int64
	Elapsed           time.Duration
}

// RebuildFailure is a thread Rebuild could not index because of its log.
type RebuildFailure struct {
	Thread threadid.ID
	Err    error
}

// RebuildReport is what one Rebuild did. It describes the index the run
// committed; a run that returned an error committed nothing.
type RebuildReport struct {
	// Threads is how many logs the walk found, Indexed how many of them were
	// indexed, and Posts how many post rows the index holds afterwards, the
	// title rows left out.
	Threads int
	Indexed int
	Posts   int

	// Torn are threads whose log ends in a torn final frame, an interrupted
	// write. Each was indexed to the end of its last whole frame and is
	// counted in Indexed; serve cuts the torn bytes off when it next opens
	// the thread, and its catch-up indexes what follows.
	Torn []threadid.ID

	// Failed are threads whose log could not be indexed: damage before its
	// last frame, or a header that is damaged or names another thread. Each
	// has no rows and no search_state.
	Failed []RebuildFailure

	// Strays are files under threads/ that are not a thread's log or index
	// at the path its id names.
	Strays []string

	// WasVersion is the search_meta version the index had before the run,
	// when HadVersion.
	WasVersion int64
	HadVersion bool

	Elapsed time.Duration
}

// Rebuild replaces the whole index with one built from the thread logs, for
// `ketju reindex --search` (docs/03 "Durability and recovery",
// TKT-01M29CTY9D). It is for a data directory no server is running on: the
// caller holds the directory lock.
//
// It walks threads/ first, so a walk that fails changes nothing. Then, in one
// write transaction, it reads the version the index had, empties the index
// (db.ClearSearch, which works on damaged segments), and indexes every
// thread from its first frame through the same per-thread path as Deliver
// and CatchUp, reading each log alone and never its .idx. Each thread runs
// inside its own savepoint: one whose log is at fault is rolled back to it,
// keeps no rows, and is reported, and the run goes on. Last it records
// Version in search_meta, builds the FTS5 index from the rows in one pass,
// and merges it into one segment.
//
// Any other failure, a cancelled context included, rolls the whole
// transaction back: the index, and the version it carries, stay exactly as
// they were (Terva review 1731). So Rebuild never calls Prepare, which would
// commit an emptied index on its own.
//
// progress, if not nil, is called once before the first thread and once
// after each, inside the transaction.
func (x *Indexer) Rebuild(ctx context.Context, progress func(RebuildProgress)) (RebuildReport, error) {
	start := time.Now()
	var rep RebuildReport
	logs, strays, err := x.walk(x.root)
	if err != nil {
		return rep, fmt.Errorf("search: rebuild: %w", err)
	}
	rep.Threads, rep.Strays = len(logs), strays
	ids := make([]threadid.ID, 0, len(logs))
	var total int64
	for id, size := range logs {
		ids = append(ids, id)
		total += size
	}
	slices.Sort(ids)

	x.write.Lock()
	defer x.write.Unlock()
	// What the transaction found, kept only if it commits.
	var got RebuildReport
	err = x.db.Write(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
		got = RebuildReport{Threads: rep.Threads, Strays: rep.Strays}
		v, err := q.GetSearchVersion(ctx)
		switch {
		case err == nil:
			got.WasVersion, got.HadVersion = v, true
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("reading the index version: %w", err)
		}
		if err := db.ClearSearch(ctx, tx); err != nil {
			return fmt.Errorf("emptying the index: %w", err)
		}

		p := RebuildProgress{Total: len(ids), TotalBytes: total}
		report := func() {
			if progress != nil {
				p.Elapsed = time.Since(start)
				progress(p)
			}
		}
		report()
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return err
			}
			out, threadErr, txErr := x.inSavepoint(ctx, tx, target{id: id, rebuild: true})
			switch {
			case txErr != nil:
				return txErr
			case threadErr != nil && (ctx.Err() != nil || !IsLogFault(threadErr)):
				return threadErr
			case threadErr != nil:
				got.Failed = append(got.Failed, RebuildFailure{Thread: id, Err: threadErr})
			default:
				got.Indexed++
				got.Posts += out.docs
				if out.end < logs[id] {
					got.Torn = append(got.Torn, id)
				}
			}
			p.Done++
			p.Bytes += logs[id]
			report()
		}

		if err := q.SetSearchVersion(ctx, Version); err != nil {
			return fmt.Errorf("recording the index version: %w", err)
		}
		if err := db.RebuildSearchIndex(ctx, tx); err != nil {
			return fmt.Errorf("building the full-text index: %w", err)
		}
		if err := db.OptimizeSearchIndex(ctx, tx); err != nil {
			return fmt.Errorf("merging the full-text index: %w", err)
		}
		return ctx.Err()
	})
	got.Elapsed = time.Since(start)
	if err != nil {
		rep.Elapsed = got.Elapsed
		return rep, fmt.Errorf("search: rebuild: %w", err)
	}

	// Nothing this indexer held is true of the rebuilt index.
	x.mu.Lock()
	clear(x.behind)
	x.mu.Unlock()
	x.sweepOwed.Store(false)
	x.indexing.Store(false)
	return got, nil
}
