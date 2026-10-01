package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Criterion (TKT-01M3VHSYFR): serve discards the thread indexes an older
// build wrote, before it opens a thread, so an index left trusted over a
// stale entry cannot serve a deleted post as visible. The run decision is in
// docs/03 "Durability and recovery".
func TestServeDiscardsIndexesWrittenBeforeWriteBack(t *testing.T) {
	path, data := writeConfig(t, "")
	r := start(t, path, serveOpts{})
	if err := r.shutdown(t); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(data, threadcache.WriteBackMarker)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the first start wrote no marker: %v", err)
	}
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(data, "db", "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	err = d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		_, err := q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "general", Name: "General",
			Visibility: "public", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	d.Close()
	if err != nil {
		t.Fatal(err)
	}

	// A thread whose author deleted post 1, with an index that an older
	// build left trusted over an entry that does not show it, and a data
	// directory that older build left with no marker.
	id := seedThreads(t, data, 1)[0]
	c, err := threadcache.New(data, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	appendTo(t, c, id, threadlog.Append{Type: threadlog.TypeDelete, Ordinal: 1, Author: 1,
		Body: &threadlog.DeleteBody{}})
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	idx, err := os.ReadFile(id.IndexPath(data))
	if err != nil {
		t.Fatal(err)
	}
	const flagsAt = threadlog.IndexHeaderSize + 34 // ordinal 1's flags, docs/03
	idx[flagsAt], idx[flagsAt+1] = 0, 0
	if err := os.WriteFile(id.IndexPath(data), idx, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}

	r = start(t, path, serveOpts{})
	defer r.shutdown(t)
	code, body := get(t, "http://"+r.addr+"/api/v1/threads/"+id.String())
	if code != http.StatusOK {
		t.Fatalf("GET the thread = %d: %s", code, body)
	}
	var page dto.ThreadPage
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 1 {
		t.Fatalf("the page holds %d posts, want 1: %s", len(page.Posts), body)
	}
	if p := page.Posts[0]; !p.Deleted || p.HTML != "" {
		t.Errorf("post 1 is served as %+v, want a tombstone with no HTML", p)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the start wrote no marker: %v", err)
	}
}
