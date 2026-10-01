package threadcache

import (
	"bytes"
	"os"
	"testing"

	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// The tests here are TKT-01M3VHSYFR's. A crash between the log fsync and the
// index entry write leaves a log holding a record the .idx does not show,
// which is what an Extended open exists for; a missing or damaged .idx is a
// Rebuilt one. Either way the replayed state was held only in memory, and the
// actor's first header then made the next open trust the stale file: a
// deleted, hidden or redacted post came back visible.

// reopen is a cache over root, closed when the test ends unless the test
// closes it first.
func reopen(t *testing.T, root string, policy threadlog.SyncPolicy) *Cache {
	t.Helper()
	c, err := New(root, Options{Sync: policy})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// appendTo appends one record to an open thread.
func appendTo(t *testing.T, c *Cache, id threadid.ID, a threadlog.Append) {
	t.Helper()
	if err := c.With(id, func(th *Thread) error {
		_, err := th.Actor.Append(a)
		return err
	}); err != nil {
		t.Fatalf("appending a %s: %v", a.Type, err)
	}
}

// fullRebuild is the index the log alone gives, which every open must agree
// with.
func fullRebuild(t *testing.T, root string, id threadid.ID) *threadlog.Index {
	t.Helper()
	raw, err := os.ReadFile(id.LogPath(root))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := threadlog.Open(bytes.NewReader(raw), int64(len(raw)), nil)
	if err != nil {
		t.Fatal(err)
	}
	return rec.Index
}

// sameEntries fails unless got holds exactly want's entries.
func sameEntries(t *testing.T, got, want *threadlog.Index, what string) {
	t.Helper()
	if got.Len() != want.Len() {
		t.Fatalf("%s: %d entries, want %d", what, got.Len(), want.Len())
	}
	g, w := got.Entries(1, uint32(got.Len())), want.Entries(1, uint32(want.Len()))
	for i := range w {
		if g[i] != w[i] {
			t.Errorf("%s: entry %d is %+v, a full rebuild gives %+v", what, i+1, g[i], w[i])
		}
	}
}

func del(body threadlog.DeleteBody) threadlog.Append {
	return threadlog.Append{Type: threadlog.TypeDelete, Ordinal: 1, Author: 9, Body: &body}
}

// The records whose effect on post 1 a crash must not lose. prep runs before
// the index is copied, so it is in the copy; the record runs after.
var replayCases = []struct {
	name string
	prep []threadlog.Append
	// record appends the record, or for a redaction rewrites the log the
	// way ketju redact does, with no actor running.
	record func(t *testing.T, c *Cache, root string, id threadid.ID)
	// hidden is whether post 1 must end as a tombstone.
	hidden bool
	source string
}{
	{name: "author delete", hidden: true, record: func(t *testing.T, c *Cache, _ string, id threadid.ID) {
		appendTo(t, c, id, threadlog.Append{Type: threadlog.TypeDelete, Ordinal: 1, Author: 7, Body: &threadlog.DeleteBody{}})
	}},
	{name: "moderator hide", hidden: true, record: func(t *testing.T, c *Cache, _ string, id threadid.ID) {
		appendTo(t, c, id, del(threadlog.DeleteBody{ByModerator: true, Reason: "off topic"}))
	}},
	{name: "suspension hide", hidden: true, record: func(t *testing.T, c *Cache, _ string, id threadid.ID) {
		appendTo(t, c, id, del(threadlog.DeleteBody{ByModerator: true, Suspension: 41}))
	}},
	{name: "auto-hide", hidden: true, record: func(t *testing.T, c *Cache, _ string, id threadid.ID) {
		appendTo(t, c, id, threadlog.Append{Type: threadlog.TypeDelete, Ordinal: 1, Body: &threadlog.DeleteBody{Auto: true}})
	}},
	{name: "redacted", hidden: true, record: func(t *testing.T, c *Cache, root string, id threadid.ID) {
		// ketju redact swaps the rewritten log in and then writes the index;
		// the crash this case stands for lands between the two.
		raw, err := os.ReadFile(id.LogPath(root))
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if _, err := threadlog.RedactPosts(bytes.NewReader(raw), int64(len(raw)), &out, []uint32{1},
			threadlog.RedactOptions{TimeMS: 1_800_000_000_000, Operator: 9, Reason: "court order", Journal: "01K6DHZ3Q8N6T2V4X9B7M5C1RE"}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(id.LogPath(root), out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}},
	{name: "edit", source: "the corrected text", record: func(t *testing.T, c *Cache, _ string, id threadid.ID) {
		appendTo(t, c, id, threadlog.Append{Type: threadlog.TypeEdit, Ordinal: 1, Author: 7,
			Body: &threadlog.EditBody{Source: "the corrected text"}})
	}},
	{name: "restore", prep: []threadlog.Append{del(threadlog.DeleteBody{Auto: true})},
		record: func(t *testing.T, c *Cache, _ string, id threadid.ID) {
			appendTo(t, c, id, threadlog.Append{Type: threadlog.TypeRestore, Ordinal: 1, Author: 9, Body: &threadlog.RestoreBody{}})
		}},
}

// The crashes: what the .idx holds when the thread is next opened.
var crashes = []struct {
	name string
	want threadlog.Outcome
	// make leaves the index as the crash would, given a copy taken before
	// the record.
	make func(t *testing.T, path string, before []byte)
}{
	{name: "extended", want: threadlog.Extended, make: func(t *testing.T, path string, before []byte) {
		if err := os.WriteFile(path, before, 0o644); err != nil {
			t.Fatal(err)
		}
	}},
	{name: "rebuilt from no index", want: threadlog.Rebuilt, make: func(t *testing.T, path string, _ []byte) {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}},
	{name: "rebuilt from a damaged header", want: threadlog.Rebuilt, make: func(t *testing.T, path string, before []byte) {
		b := bytes.Clone(before)
		b[28] ^= 0xff // the header CRC; the old entries stay as they were
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}},
}

// Criterion: an index replayed onto or rebuilt is written back before
// anything can mark it trusted, so after the description's steps the next
// open is trusted and agrees with the log.
//
// The steps: a thread with two posts; the index copied; the record; the
// crash; reopen and append one post; reopen.
func TestAReplayedRecordIsWrittenBackBeforeTheIndexIsTrusted(t *testing.T) {
	for _, rc := range replayCases {
		for _, cr := range crashes {
			t.Run(rc.name+"/"+cr.name, func(t *testing.T) {
				root := t.TempDir()
				id := ids(1)[0]
				idx := id.IndexPath(root)

				c := reopen(t, root, threadlog.SyncGroup)
				create(t, c, id, 2)
				for _, a := range rc.prep {
					appendTo(t, c, id, a)
				}
				c.Close()
				before, err := os.ReadFile(idx)
				if err != nil {
					t.Fatal(err)
				}

				c = reopen(t, root, threadlog.SyncGroup)
				rc.record(t, c, root, id)
				c.Close()
				cr.make(t, idx, before)

				c = reopen(t, root, threadlog.SyncGroup)
				if err := c.With(id, func(th *Thread) error {
					if th.Outcome != cr.want {
						t.Errorf("the open after the crash is %v, want %v", th.Outcome, cr.want)
					}
					post(t, th, "after the crash")
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				c.Close()

				c = reopen(t, root, threadlog.SyncGroup)
				th, err := c.Acquire(id)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Release(th)
				if th.Outcome != threadlog.Trusted {
					t.Errorf("the last open is %v, want trusted", th.Outcome)
				}
				sameEntries(t, th.Index, fullRebuild(t, root, id), "the last open")

				e, err := th.Index.Entry(1)
				if err != nil {
					t.Fatal(err)
				}
				if e.Flags.Visible() == rc.hidden {
					t.Errorf("post 1 has flags %#x: visible %v, want %v", e.Flags, e.Flags.Visible(), !rc.hidden)
				}
				posts, err := th.Reader.Range(1, 1)
				if err != nil || len(posts) != 1 {
					t.Fatalf("reading post 1: %v, %v", posts, err)
				}
				p := posts[0]
				switch {
				case rc.hidden && (!p.Tombstone || p.Source != ""):
					t.Errorf("post 1 is served as %+v, want a tombstone with no source", p)
				case !rc.hidden && p.Tombstone:
					t.Errorf("post 1 is served as a tombstone, want it visible")
				case rc.source != "" && p.Source != rc.source:
					t.Errorf("post 1 is served with %q, want the current revision's %q", p.Source, rc.source)
				}
			})
		}
	}
}
