package api

import (
	"testing"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestMissingAuthorIsADeletedMember is TKT-01M3KB7FXC's rendering half: a
// record whose author id the database lacks, as after a restore from a
// backup older than the logs, draws as a deleted member on the thread page,
// in the thread list, and in the forum index. It is drawn as an anonymized
// account is, deleted-user-<id> and nothing else, and never as anyone the
// database has.
func TestMissingAuthorIsADeletedMember(t *testing.T) {
	w := newForumWorld(t)
	const lostPoster, lostStarter = 777, 778
	for _, id := range w.users {
		if id == lostPoster || id == lostStarter {
			t.Fatalf("the world already has member %d", id)
		}
	}
	gone := func(id int64) dto.Author {
		return dto.Author{Handle: "deleted-user-" + itoa(id), Role: "member", Kind: "human"}
	}

	// A thread alice started, last posted in by a member the database lacks.
	older := w.threadIn("general", 1, "Older thread")
	w.post(older, "alice", "Before the backup.", 0)
	w.append(older, threadlog.Append{Type: threadlog.TypePost, Author: lostPoster,
		Body: &threadlog.PostBody{Source: "After the backup."}})

	// A thread whose starter the database lacks, active last, so the forum
	// index names its last poster.
	newer := threadid.ID(uint64(1_757_500_000_000)<<16 | 2)
	th, err := w.cache.Create(newer, uint64(w.ids["general"]), w.clock)
	if err != nil {
		t.Fatal(err)
	}
	w.cache.Release(th)
	w.append(newer, threadlog.Append{Type: threadlog.TypeThreadCreated, Author: lostStarter,
		Body: &threadlog.ThreadCreatedBody{Title: "Newer thread", Slug: "newer-thread", ForumID: uint64(w.ids["general"])}})
	w.append(newer, threadlog.Append{Type: threadlog.TypePost, Author: lostStarter,
		Body: &threadlog.PostBody{Source: "Also after the backup."}})

	p := w.pageOf(older)
	if len(p.Posts) != 2 {
		t.Fatalf("thread page has %d posts, want 2", len(p.Posts))
	}
	if got := p.Posts[0].Author; got.Handle != "alice" {
		t.Errorf("a known author = %+v, want alice", got)
	}
	if got := p.Posts[1].Author; got != gone(lostPoster) {
		t.Errorf("a missing author on the thread page = %+v, want %+v", got, gone(lostPoster))
	}

	rows := map[string]dto.ThreadRow{}
	for _, r := range w.threadList("alice", "general", "", "").Threads {
		rows[r.ID] = r
	}
	if got := rows[older.String()].LastPostAuthor; got != gone(lostPoster) {
		t.Errorf("a missing last poster in the list = %+v, want %+v", got, gone(lostPoster))
	}
	if got := rows[newer.String()].Author; got != gone(lostStarter) {
		t.Errorf("a missing starter in the list = %+v, want %+v", got, gone(lostStarter))
	}

	tree, _ := w.tree("alice")
	var found bool
	for _, f := range tree.Forums {
		if f.Slug != "general" {
			continue
		}
		found = true
		if f.LastPost == nil || f.LastPost.Author != gone(lostStarter) {
			t.Errorf("a missing last poster in the forum index = %+v, want %+v", f.LastPost, gone(lostStarter))
		}
	}
	if !found {
		t.Fatal("the forum index has no general forum")
	}
}
