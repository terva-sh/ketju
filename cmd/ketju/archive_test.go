package main

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/terva-sh/ketju/internal/auth/oidc/oidctest"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Thread archival over the real serve and the offline commands
// (TKT-01M29CRJ64): the archived state is the log's, so it survives a
// projection rebuild and a restart, and ketju redact still reaches an
// archived thread (docs/08 "Locking semantics", run decision pending
// maintainer review).

// archivedFlag is the threads row's archived column for id.
func archivedFlag(t *testing.T, data string, id threadid.ID) int {
	t.Helper()
	w := &redactWorld{t: t, data: data}
	return w.count(`SELECT archived FROM threads WHERE id = ?`, int64(id))
}

// sealedOnOpen opens id with a cache that reads the archived flag from the
// projection, as serve does, and says whether an append is refused with
// ErrArchived. The log is unchanged either way it answers.
func sealedOnOpen(t *testing.T, data string, id threadid.ID) bool {
	t.Helper()
	d := openDB(t, data)
	defer d.Close()
	c, err := threadcache.New(data, threadcache.Options{Sync: threadlog.SyncNever, Archived: archived(d)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	}()
	err = c.With(id, func(th *threadcache.Thread) error {
		_, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypePost, Author: 1,
			Body: &threadlog.PostBody{Source: "after the archive"}})
		return err
	})
	if err != nil && !errors.Is(err, threadlog.ErrArchived) {
		t.Fatal(err)
	}
	return errors.Is(err, threadlog.ErrArchived)
}

// TestArchivedStateSurvivesReindex: an admin archives a thread through the
// API of a running serve. After serve stops, ketju reindex empties and
// rebuilds the projections from the logs, and threads.archived is 1 again.
// A serve started on the rebuilt database refuses a reply and an admin's
// rename with conflict, and the log does not grow; the thread still reads.
func TestArchivedStateSurvivesReindex(t *testing.T) {
	idp := oidctest.New(t)
	idp.Users = exitSubjects
	path, data := writeConfig(t, providerBlock(idp.URL))
	seedPeople(t, data, idp.URL, ada, sam)
	r := start(t, path, serveOpts{idpClient: idp.Client(), getenv: siteURL})
	s := &exitServer{r: r, idp: idp}
	admin, member := s.member(t, ada), s.member(t, sam)

	thread := newThread(t, member)
	if res := reply(t, member, thread, "Before the archive."); res.status != http.StatusCreated {
		t.Fatalf("a reply before the archive answered %d:\n%s", res.status, res)
	}
	admin.must(t, http.StatusOK, http.MethodPost, "/api/v1/admin/threads/"+thread+"/archive",
		map[string]any{"reason": "Closed history."})
	if err := r.shutdown(t); err != nil {
		t.Fatalf("stopping serve: %v\n%s", err, r.log)
	}

	id, err := threadid.Parse(thread)
	if err != nil {
		t.Fatal(err)
	}
	if got := archivedFlag(t, data, id); got != 1 {
		t.Fatalf("threads.archived is %d after the archive, want 1", got)
	}
	logLen := func() int {
		t.Helper()
		b, err := os.ReadFile(id.LogPath(data))
		if err != nil {
			t.Fatal(err)
		}
		return len(b)
	}
	before := logLen()

	if sum := reindexJSON(t, data); sum.Threads != 1 {
		t.Fatalf("reindex saw %d threads, want 1: %+v", sum.Threads, sum)
	}
	if got := archivedFlag(t, data, id); got != 1 {
		t.Fatalf("threads.archived is %d after ketju reindex, want 1", got)
	}

	r = start(t, path, serveOpts{idpClient: idp.Client(), getenv: siteURL})
	defer r.shutdown(t)
	admin.r, member.r = r, r
	if res := reply(t, member, thread, "After the rebuild."); res.status != http.StatusConflict {
		t.Errorf("a reply after the rebuild answered %d, want 409:\n%s", res.status, res)
	}
	if res := admin.do(t, http.MethodPatch, "/api/v1/threads/"+thread, map[string]any{"title": "Renamed after"}); res.status != http.StatusConflict {
		t.Errorf("an admin's rename after the rebuild answered %d, want 409:\n%s", res.status, res)
	}
	member.must(t, http.StatusOK, http.MethodGet, "/api/v1/threads/"+thread, nil)
	if got := logLen(); got != before {
		t.Errorf("the log grew from %d to %d bytes after the rebuild", before, got)
	}
}

// TestRedactReachesAnArchivedThread: ketju redact blanks a post in an
// archived thread and marks it, as it does in a live one, and the thread
// stays archived: the projection still says so, and a cache opened as serve
// opens it refuses an append.
func TestRedactReachesAnArchivedThread(t *testing.T) {
	w := newRedactWorld(t)
	id := w.thread("An archived thread")
	w.post(id, "keep me")
	n := w.post(id, "REDACT-ME-1")
	w.append(id, threadlog.Append{Type: threadlog.TypeThreadArchived, Author: uint64(w.users["root"]),
		Body: &threadlog.ThreadArchivedBody{Reason: "closed history"}})
	w.ready()
	if got := archivedFlag(t, w.data, id); got != 1 {
		t.Fatalf("threads.archived is %d before the redaction, want 1", got)
	}

	w.mustRedact(postArgs(id, n)...)
	b := w.log(id)
	if bytes.Contains(b, []byte("REDACT-ME-1")) {
		t.Error("the redacted source survived in the archived thread's log")
	}
	if !bytes.Contains(b, []byte("keep me")) {
		t.Error("the redaction lost another post")
	}
	if m := w.markers(id); len(m) != 1 || m[0].Ordinal != n {
		t.Errorf("markers %+v, want one for post %d", m, n)
	}
	if e := w.entry(id, n); !e.Flags.Has(threadlog.EntryRedacted) {
		t.Errorf("entry flags %#x", e.Flags)
	}
	if got := archivedFlag(t, w.data, id); got != 1 {
		t.Errorf("threads.archived is %d after the redaction, want 1", got)
	}
	after := len(w.log(id))
	if !sealedOnOpen(t, w.data, id) {
		t.Error("after the redaction the archived thread takes an append")
	}
	if got := len(w.log(id)); got != after {
		t.Errorf("the log grew from %d to %d bytes", after, got)
	}
}
