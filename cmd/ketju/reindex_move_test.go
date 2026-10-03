package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestMoveProjectionsRebuildFromLogs is criterion 3 of TKT-01M29CXJZZ at
// the command: a merge into an existing thread and a split into a new one,
// projected live record by record with each target ahead of its source,
// and ketju reindex of the logs alone gives the same rows, post_moves,
// counts and last_post_* included.
func TestMoveProjectionsRebuildFromLogs(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(filepath.Join(data, threadid.ThreadsDir), 0o750); err != nil {
		t.Fatal(err)
	}
	d := openDB(t, data)
	cache, err := threadcache.New(data, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	w, err := projection.New(projection.Options{DB: d, Root: data, Sweep: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	clock := int64(1_700_000_000_000)
	lengths := map[threadid.ID]int64{}
	add := func(id threadid.ID, a threadlog.Append) {
		t.Helper()
		if a.TimeMS == 0 {
			clock += 1000
			a.TimeMS = clock
		}
		if err := cache.With(id, func(th *threadcache.Thread) error {
			res, err := th.Actor.Append(a)
			lengths[id] = res.LogLength
			return err
		}); err != nil {
			t.Fatalf("appending %s: %v", a.Type, err)
		}
	}
	project := func(ids ...threadid.ID) {
		t.Helper()
		for _, id := range ids {
			if _, err := w.ProjectThread(t.Context(), id, lengths[id]); err != nil {
				t.Fatal(err)
			}
		}
	}
	thread := func(seq uint64, title string) threadid.ID {
		t.Helper()
		id := threadid.ID(uint64(clock)<<16 | seq)
		th, err := cache.Create(id, 1, clock)
		if err != nil {
			t.Fatal(err)
		}
		cache.Release(th)
		add(id, threadlog.Append{Type: threadlog.TypeThreadCreated, Author: 3,
			Body: &threadlog.ThreadCreatedBody{Title: title, Slug: strings.ToLower(title), ForumID: 1}})
		project(id)
		return id
	}
	post := func(id threadid.ID, author uint64) int64 {
		t.Helper()
		add(id, threadlog.Append{Type: threadlog.TypePost, Author: author, Body: &threadlog.PostBody{Source: "words"}})
		project(id)
		return clock
	}
	edit := func(id threadid.ID, ordinal uint32, ms int64) {
		t.Helper()
		add(id, threadlog.Append{Type: threadlog.TypeEdit, Author: 5, Ordinal: ordinal, TimeMS: ms,
			Body: &threadlog.EditBody{Source: "edited"}})
		project(id)
	}
	// move writes a move the way the service does, target first in the
	// projection: the target's run, projected a record at a time, then the
	// source's split.
	move := func(src, dst threadid.ID, times map[uint32]int64, edits map[uint32]int64, ords ...uint32) {
		t.Helper()
		add(src, threadlog.Append{Type: threadlog.TypePostsSplitOut, Author: 5,
			Body: &threadlog.PostsSplitOutBody{Ordinals: ords, TargetThreadID: uint64(dst)}})
		add(dst, threadlog.Append{Type: threadlog.TypePostsMergedIn, Author: 5,
			Body: &threadlog.PostsMergedInBody{SourceThreadID: uint64(src), Ordinals: ords}})
		project(dst)
		for _, o := range ords {
			add(dst, threadlog.Append{Type: threadlog.TypePost, Author: 4, TimeMS: times[o],
				Body: &threadlog.PostBody{Source: "words", Origin: &threadlog.PostOrigin{ThreadID: uint64(src), Ordinal: o}}})
			project(dst)
			if ms, ok := edits[o]; ok {
				var copyOrdinal uint32
				if err := cache.With(dst, func(th *threadcache.Thread) error {
					copyOrdinal = uint32(th.Index.Len())
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				edit(dst, copyOrdinal, ms)
			}
		}
		project(src)
	}

	src := thread(1, "Source")
	dst := thread(2, "Target")
	times := map[uint32]int64{}
	for o := uint32(1); o <= 4; o++ {
		times[o] = post(src, 4)
	}
	edited := clock + 1
	edit(src, 2, edited)
	post(dst, 6)
	move(src, dst, times, map[uint32]int64{2: edited}, 2, 3)
	split := thread(3, "Split")
	move(src, split, times, nil, 4)
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	d.Close()

	live, _ := dumpAt(t, data)
	for _, want := range []string{"post_moves: 3 rows", "opens_thread=1", "moved=1"} {
		if !strings.Contains(live, want) {
			t.Fatalf("the live projection lacks %q:\n%s", want, live)
		}
	}
	d = openDB(t, data)
	if err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		return db.ClearProjections(t.Context(), q)
	}); err != nil {
		t.Fatal(err)
	}
	d.Close()
	reindexJSON(t, data)
	if rebuilt, _ := dumpAt(t, data); rebuilt != live {
		t.Fatalf("reindexed rows differ from the live ones:\n%s\n---\n%s", live, rebuilt)
	}
}
