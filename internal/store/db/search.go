package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// SearchTables are the search index's plain tables (docs/04 "search (FTS)"),
// in the order migration 0022 creates them. Like the projections they are
// derived from the thread logs and can be rebuilt from them, but they are not
// projections: `ketju reindex --projections` leaves them alone, and `ketju
// reindex --search` rebuilds them (TKT-01M29CTY9D).
//
// TestSearchTablesAreMarked holds this list equal to the tables whose schema
// carries the SEARCH INDEX marker. The FTS5 table `search` is not listed: it
// holds no data of its own, only an index over search_docs, and its shadow
// tables are FTS5's.
var SearchTables = []string{"search_docs", "search_state", "search_meta"}

// SearchDoc is one row of the search index: a visible post at its current
// revision, or a thread's title at ordinal 0.
type SearchDoc struct {
	Thread    int64
	Ordinal   int64
	Revision  int64
	AuthorID  int64
	CreatedAt int64
	// Title is set on the title row, and SourcePlain on a post's row.
	Title       string
	SourcePlain string
}

// This file and queries/search.sql are the only writers of search_docs and
// of the FTS5 table over it, which TestOnlySearchGoWritesTheIndex holds. With
// no triggers between them, every change to a search_docs row is paired here
// with the FTS5 commands that keep `search` in step: 'delete' with the old
// values before a row changes or goes, and an insert of the new values after.

const (
	ftsInsert    = `INSERT INTO search (rowid, title, source_plain) VALUES (?, ?, ?)`
	ftsDelete    = `INSERT INTO search (search, rowid, title, source_plain) VALUES ('delete', ?, ?, ?)`
	ftsDeleteAll = `INSERT INTO search (search) VALUES ('delete-all')`
	ftsRebuild   = `INSERT INTO search (search) VALUES ('rebuild')`
	ftsIntegrity = `INSERT INTO search (search, rank) VALUES ('integrity-check', 1)`
)

// PutSearchDoc writes doc as the row for its (thread, ordinal), replacing
// any row already there, and keeps the FTS5 index in step. A row whose text
// is unchanged keeps its index entries and has only its other columns
// rewritten, so indexing a thread again from its first frame costs no FTS5
// write for a post that did not change.
func PutSearchDoc(ctx context.Context, tx *sql.Tx, doc SearchDoc) error {
	q := dbq.New(tx)
	old, err := q.GetSearchDoc(ctx, dbq.GetSearchDocParams{ThreadID: doc.Thread, Ordinal: doc.Ordinal})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		id, err := q.InsertSearchDoc(ctx, dbq.InsertSearchDocParams{
			ThreadID: doc.Thread, Ordinal: doc.Ordinal, Revision: doc.Revision,
			AuthorID: doc.AuthorID, CreatedAt: doc.CreatedAt, Title: doc.Title, SourcePlain: doc.SourcePlain,
		})
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, ftsInsert, id, doc.Title, doc.SourcePlain)
		return err
	case err != nil:
		return err
	}
	sameText := old.Title == doc.Title && old.SourcePlain == doc.SourcePlain
	if sameText && old.Revision == doc.Revision && old.AuthorID == doc.AuthorID && old.CreatedAt == doc.CreatedAt {
		return nil
	}
	if !sameText {
		if _, err := tx.ExecContext(ctx, ftsDelete, old.ID, old.Title, old.SourcePlain); err != nil {
			return err
		}
	}
	if err := q.UpdateSearchDoc(ctx, dbq.UpdateSearchDocParams{
		Revision: doc.Revision, AuthorID: doc.AuthorID, CreatedAt: doc.CreatedAt,
		Title: doc.Title, SourcePlain: doc.SourcePlain, ID: old.ID,
	}); err != nil {
		return err
	}
	if sameText {
		return nil
	}
	_, err = tx.ExecContext(ctx, ftsInsert, old.ID, doc.Title, doc.SourcePlain)
	return err
}

// DropSearchDoc removes the row for (thread, ordinal), if there is one, and
// its FTS5 index entries.
func DropSearchDoc(ctx context.Context, tx *sql.Tx, thread, ordinal int64) error {
	q := dbq.New(tx)
	old, err := q.GetSearchDoc(ctx, dbq.GetSearchDocParams{ThreadID: thread, Ordinal: ordinal})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return err
	}
	if _, err := tx.ExecContext(ctx, ftsDelete, old.ID, old.Title, old.SourcePlain); err != nil {
		return err
	}
	return q.DeleteSearchDoc(ctx, old.ID)
}

// DropSearchThread removes every row thread holds, through DropSearchDoc,
// and its search_state row, so the thread can be indexed again from its
// first frame with nothing left over: an ordinal the log no longer holds
// keeps no row (Terva review 1731).
func DropSearchThread(ctx context.Context, tx *sql.Tx, thread int64) error {
	q := dbq.New(tx)
	ordinals, err := q.ListSearchDocOrdinals(ctx, thread)
	if err != nil {
		return err
	}
	for _, o := range ordinals {
		if err := DropSearchDoc(ctx, tx, thread, o); err != nil {
			return err
		}
	}
	return q.DeleteSearchState(ctx, thread)
}

// ClearSearch empties the whole index: FTS5's 'delete-all', then every table
// in SearchTables. 'delete-all' works on an index whose segments are
// damaged, which a row-by-row delete does not. Run it inside a Write, so the
// index is never seen half-empty; the search_meta row goes with the rest, so
// whoever clears records the version it will build.
func ClearSearch(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, ftsDeleteAll); err != nil {
		return err
	}
	q := dbq.New(tx)
	for _, clear := range []func(context.Context) error{q.ClearSearchDocs, q.ClearSearchState, q.ClearSearchMeta} {
		if err := clear(ctx); err != nil {
			return err
		}
	}
	return nil
}

// RebuildSearchIndex rebuilds the FTS5 index from search_docs in one pass,
// leaving search_docs as it is. It is `ketju reindex --search`'s fast path,
// for an index whose segments are damaged while its rows are not
// (TKT-01M29CTY9D).
func RebuildSearchIndex(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, ftsRebuild)
	return err
}

// CheckSearchIndex runs FTS5's integrity-check against search_docs, the
// content it indexes. It returns an error when the index and its rows
// disagree. FTS5 runs the check as an INSERT, so it needs a Write.
func CheckSearchIndex(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, ftsIntegrity)
	return err
}
