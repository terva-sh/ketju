package threads

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestRestoreRecordPreconditions holds the Restore each viewer appends to
// the actor's check, on a real thread: a moderator's takes a post that is
// deleted or hidden by a moderator, and an author's takes only their own
// delete (TKT-01M29CRJ5J). A post both deleted and hidden, which only records
// appended without the service's preconditions can make, is the moderator's:
// a Restore clears both bits, so the author's would undo the hide.
func TestRestoreRecordPreconditions(t *testing.T) {
	const author, moderator = 3, 5
	for _, c := range []struct {
		name      string
		state     []threadlog.Append // after the post, before the restore
		moderator bool
		ok        bool
	}{
		{"author, own delete", []threadlog.Append{del(author, false)}, false, true},
		{"author, a moderator's hide", []threadlog.Append{del(moderator, true)}, false, false},
		{"author, a hide after a restore of her delete", []threadlog.Append{del(author, false), restore(moderator), del(moderator, true)}, false, false},
		{"author, own delete under a moderator's hide", []threadlog.Append{del(author, false), del(moderator, true)}, false, false},
		{"author, visible", nil, false, false},
		{"moderator, a delete under a hide", []threadlog.Append{del(author, false), del(moderator, true)}, true, true},
		{"moderator, a hide", []threadlog.Append{del(moderator, true)}, true, true},
		{"moderator, an author's delete", []threadlog.Append{del(author, false)}, true, true},
		{"moderator, visible", nil, true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			cache, err := threadcache.New(filepath.Join(t.TempDir(), "threads"), threadcache.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer cache.Close()
			id := threadid.ID(uint64(1_757_500_000_000)<<16 | 1)
			th, err := cache.Create(id, 1, 1_757_500_000_000)
			if err != nil {
				t.Fatal(err)
			}
			cache.Release(th)
			recs := append([]threadlog.Append{
				{Type: threadlog.TypeThreadCreated, Author: author, Body: &threadlog.ThreadCreatedBody{Title: "t", Slug: "t", ForumID: 1}},
				{Type: threadlog.TypePost, Author: author, Body: &threadlog.PostBody{Source: "x"}},
			}, c.state...)
			who := uint64(author)
			if c.moderator {
				who = moderator
			}
			r := restoreRecord(c.moderator, "")
			r.Author, r.Ordinal = who, 1
			if err := cache.With(id, func(th *threadcache.Thread) error {
				for _, rec := range recs {
					if _, err := th.Actor.Append(rec); err != nil {
						t.Fatalf("appending %s: %v", rec.Type, err)
					}
				}
				before := th.Actor.LogLength()
				_, err := th.Actor.Append(r)
				switch {
				case c.ok && err != nil:
					t.Errorf("restore refused: %v", err)
				case !c.ok && !errors.Is(err, threadlog.ErrPostState):
					t.Errorf("restore: err %v, want ErrPostState", err)
				case !c.ok && th.Actor.LogLength() != before:
					t.Errorf("a refused restore grew the log")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func del(actor uint64, byModerator bool) threadlog.Append {
	return threadlog.Append{Type: threadlog.TypeDelete, Author: actor, Ordinal: 1, Body: &threadlog.DeleteBody{ByModerator: byModerator}}
}

func restore(actor uint64) threadlog.Append {
	return threadlog.Append{Type: threadlog.TypeRestore, Author: actor, Ordinal: 1, Body: &threadlog.RestoreBody{}}
}
