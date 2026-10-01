package projection

import (
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for the post_attachments projection (TKT-01M29CXK0K).

var (
	hashA = strings.Repeat("a", 64)
	hashB = strings.Repeat("b", 64)
	hashC = strings.Repeat("c", 64)
)

// attach appends a post by author listing hashes as its attachments.
func (f *fixture) attach(id threadid.ID, author uint64, hashes ...string) int64 {
	f.t.Helper()
	return f.append(id, threadlog.Append{Type: threadlog.TypePost, Author: author,
		Body: &threadlog.PostBody{Source: "with files", Attachments: hashes}})
}

// TestPostAttachmentsProjected: a Post's attachments become one row each,
// in the order the record lists them, and an Edit, which has no
// attachments field, leaves them as they were.
func TestPostAttachmentsProjected(t *testing.T) {
	f := newFixture(t)
	id, _ := f.thread("Files")
	f.post(id, 5, 0)
	f.attach(id, 6, hashB, hashA)
	end := f.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: 6, Ordinal: 2,
		Body: &threadlog.EditBody{Source: "edited, files kept"}})
	f.project(id, end)
	got := f.dump()
	for _, want := range []string{
		"ordinal=2 hash=\"" + hashA + "\" position=1",
		"ordinal=2 hash=\"" + hashB + "\" position=0",
		"post_attachments: 2 rows",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the projection lacks %q:\n%s", want, got)
		}
	}
	// Projected again over itself, the rows are the same.
	f.exec(`DELETE FROM projection_state`)
	f.project(id, end)
	if again := f.dump(); again != got {
		t.Fatalf("projecting again changed the rows:\n%s\n---\n%s", got, again)
	}
}

// TestRedactedDropsPostAttachments: a Redacted marker takes the post's
// rows away, catching up by the marker and reindexing the rewritten log
// agree, and the other posts keep theirs.
func TestRedactedDropsPostAttachments(t *testing.T) {
	f := newFixture(t)
	id, _ := f.thread("Files")
	f.attach(id, 5, hashA)
	f.attach(id, 6, hashB, hashC)
	end := f.attach(id, 5, hashA)
	f.project(id, end)

	n := f.redact(id, 2)
	f.project(id, n)
	caught := f.dump()
	f.clear()
	f.project(id, n)
	if rebuilt := f.dump(); rebuilt != caught {
		t.Fatalf("catching up by the marker differs from a reindex:\n%s\n---\n%s", caught, rebuilt)
	}
	if strings.Contains(caught, hashB) || strings.Contains(caught, hashC) {
		t.Errorf("the redacted post keeps its attachments:\n%s", caught)
	}
	if !strings.Contains(caught, "post_attachments: 2 rows") {
		t.Errorf("the other posts lost theirs:\n%s", caught)
	}
}

// TestReindexRebuildsPostAttachments: reindex over emptied tables and over
// scribbled ones comes back to the rows the incremental projection made.
func TestReindexRebuildsPostAttachments(t *testing.T) {
	f := newFixture(t)
	id, end := f.thread("Files")
	f.project(id, end)
	f.post(id, 5, 0)
	f.project(id, f.attach(id, 6, hashA, hashB))
	f.project(id, f.attach(id, 7, hashC))
	f.project(id, f.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: 7, Ordinal: 3,
		Body: &threadlog.DeleteBody{}}))
	incremental := f.dump()
	if !strings.Contains(incremental, "post_attachments: 3 rows") {
		t.Fatalf("the setup has the wrong rows:\n%s", incremental)
	}
	for _, setup := range []struct {
		name string
		run  func()
	}{
		{"stale", func() {
			f.exec(`INSERT INTO post_attachments (thread_id, ordinal, hash, position) VALUES (1, 1, ?, 0)`, hashA)
			f.exec(`UPDATE post_attachments SET position = position + 5`)
		}},
		{"empty", f.clear},
	} {
		t.Run(setup.name, func(t *testing.T) {
			setup.run()
			if _, err := f.w.Reindex(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			if got := f.dump(); got != incremental {
				t.Fatalf("reindexed rows differ from the incremental ones:\n%s\n---\n%s", incremental, got)
			}
		})
	}
}
