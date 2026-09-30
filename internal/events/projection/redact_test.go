package projection

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

var fixtureRedact = threadlog.RedactOptions{TimeMS: 1_790_000_000_000, Operator: 1, Reason: "takedown", Journal: "01K6DHZ3Q8N6T2V4X9B7M5C1RE"}

// rewriteLog does to thread id's log what ketju redact does, with every
// thread closed as the command requires, and reopens the cache. It returns
// the new log length.
func (f *fixture) rewriteLog(id threadid.ID, rw func(src *bytes.Reader, size int64, dst *bytes.Buffer) error) int64 {
	f.t.Helper()
	if err := f.cache.Close(); err != nil {
		f.t.Fatal(err)
	}
	path := id.LogPath(f.root)
	old, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	var out bytes.Buffer
	if err := rw(bytes.NewReader(old), int64(len(old)), &out); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, out.Bytes(), 0o640); err != nil {
		f.t.Fatal(err)
	}
	c, err := threadcache.New(f.root, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { c.Close() })
	f.cache = c
	return int64(out.Len())
}

func (f *fixture) redact(id threadid.ID, ordinals ...uint32) int64 {
	return f.rewriteLog(id, func(src *bytes.Reader, size int64, dst *bytes.Buffer) error {
		_, err := threadlog.RedactPosts(src, size, dst, ordinals, fixtureRedact)
		return err
	})
}

// TestRedactedCatchUpEqualsReindex: a projection that catches up by the
// marker alone ends where a reindex of the rewritten log does: the post
// deleted, its mentions gone, its neighbours untouched.
func TestRedactedCatchUpEqualsReindex(t *testing.T) {
	f := newFixture(t)
	id, _ := f.thread("Hello", "meta")
	f.post(id, 5, 0, 9)
	f.post(id, 6, 1, 8, 9)
	f.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: 6, Ordinal: 2,
		Body: &threadlog.EditBody{Source: "now naming 7", ReplyTo: 1, Mentions: &[]uint64{7}}})
	end := f.post(id, 5, 2, 4)
	f.project(id, end)
	if before := f.dump(); !strings.Contains(before, "mentioned_user_id=7") {
		t.Fatalf("the setup has no mention of 7:\n%s", before)
	}

	n := f.redact(id, 2)
	f.project(id, n)
	caught := f.dump()
	f.clear()
	f.project(id, n)
	if rebuilt := f.dump(); rebuilt != caught {
		t.Fatalf("catching up by the marker differs from a reindex:\n%s\n---\n%s", caught, rebuilt)
	}
	for _, want := range []string{"mentioned_user_id=9", "mentioned_user_id=4", "mentions: 2 rows"} {
		if !strings.Contains(caught, want) {
			t.Errorf("the projection lacks %q:\n%s", want, caught)
		}
	}
	if strings.Contains(caught, "mentioned_user_id=7") || strings.Contains(caught, "mentioned_user_id=8") {
		t.Errorf("the redacted post keeps its mentions:\n%s", caught)
	}
	f.agreesWithIndex(id)
}

// TestRedactedTitleCatchUpEqualsReindex: a title redaction reads the
// replacement after catch-up and after a reindex alike.
func TestRedactedTitleCatchUpEqualsReindex(t *testing.T) {
	f := newFixture(t)
	id, _ := f.thread("Secret one", "meta")
	f.post(id, 5, 0)
	end := f.append(id, threadlog.Append{Type: threadlog.TypeThreadRenamed, Author: 1,
		Body: &threadlog.ThreadRenamedBody{Title: "Secret two", Slug: "secret-two"}})
	f.project(id, end)

	n := f.rewriteLog(id, func(src *bytes.Reader, size int64, dst *bytes.Buffer) error {
		_, err := threadlog.RedactTitle(src, size, dst, threadlog.ThreadRenamedBody{Title: "Title removed", Slug: "title-removed"}, fixtureRedact)
		return err
	})
	f.project(id, n)
	caught := f.dump()
	f.clear()
	f.project(id, n)
	if rebuilt := f.dump(); rebuilt != caught {
		t.Fatalf("catching up by the marker differs from a reindex:\n%s\n---\n%s", caught, rebuilt)
	}
	if !strings.Contains(caught, `slug="title-removed" title="Title removed"`) || strings.Contains(caught, "Secret") {
		t.Errorf("the thread does not read the replacement:\n%s", caught)
	}
	f.agreesWithIndex(id)
}
