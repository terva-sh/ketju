package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestSlowModeSurvivesRestartAndReindex is part of TKT-01M29CRJ50's third
// criterion: thread_slow_mode is authoritative, so a thread's slow mode is
// still there after the database is reopened and after ketju reindex has
// emptied and rebuilt every projection, the threads row included.
func TestSlowModeSurvivesRestartAndReindex(t *testing.T) {
	const alice = 3
	data := filepath.Join(t.TempDir(), "data")
	id := logThread(t, data, 1,
		threadlog.Append{Type: threadlog.TypeThreadCreated, Author: alice,
			Body: &threadlog.ThreadCreatedBody{Title: "T", Slug: "t", ForumID: 1}},
		threadlog.Append{Type: threadlog.TypePost, Author: alice, Body: &threadlog.PostBody{Source: "first"}})

	// Projected first, as a live instance's thread is, so the table sits
	// beside a threads row that reindex will empty and rebuild.
	if sum := reindexJSON(t, data); sum.Threads != 1 {
		t.Fatalf("reindex saw %d threads, want 1: %+v", sum.Threads, sum)
	}
	d := openDB(t, data)
	execDB(t, d, `INSERT INTO users (id, handle, display_name, created_at, updated_at) VALUES (?, 'alice', 'Alice', 1, 1)`, alice)
	if err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		return q.PutThreadSlowMode(t.Context(), dbq.PutThreadSlowModeParams{
			ThreadID: int64(id), Seconds: 45, SetBy: sql.NullInt64{Int64: alice, Valid: true}, SetAt: 1_700_000_500_000})
	}); err != nil {
		t.Fatal(err)
	}
	d.Close()

	want := dbq.ThreadSlowMode{ThreadID: int64(id), Seconds: 45, SetBy: sql.NullInt64{Int64: alice, Valid: true}, SetAt: 1_700_000_500_000}
	read := func(when string) {
		t.Helper()
		d := openDB(t, data)
		defer d.Close()
		if got := slowModeRow(t, d, id); got != want {
			t.Errorf("%s the row is %+v, want %+v", when, got, want)
		}
	}
	read("after a restart")

	sum := reindexJSON(t, data)
	if sum.Threads != 1 {
		t.Fatalf("reindex saw %d threads, want 1: %+v", sum.Threads, sum)
	}
	read("after ketju reindex")
}

func slowModeRow(t *testing.T, d *db.DB, id threadid.ID) dbq.ThreadSlowMode {
	t.Helper()
	var row dbq.ThreadSlowMode
	if err := d.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		row, err = q.GetThreadSlowMode(t.Context(), int64(id))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return row
}
