package threads

import (
	"database/sql"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/service/respcache"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// starterThread writes a thread alice starts and bob replies in last, so its
// starter and its last poster differ, and projects it. It returns bob's id.
func starterThread(t *testing.T, w *repliesWorld, c *threadcache.Cache, id threadid.ID) int64 {
	t.Helper()
	var bob int64
	if err := w.db.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		bob, err = q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "bob", DisplayName: "Bob",
			Status: "active", Role: "member", Kind: "human", CreatedAt: 1, UpdatedAt: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	th, err := c.Create(id, uint64(w.forum), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Release(th)
	var length int64
	for i, a := range []threadlog.Append{
		{Type: threadlog.TypeThreadCreated, Author: uint64(w.author),
			Body: &threadlog.ThreadCreatedBody{Title: "Started", Slug: "started", ForumID: uint64(w.forum)}},
		{Type: threadlog.TypePost, Author: uint64(w.author), Body: &threadlog.PostBody{Source: "first"}},
		{Type: threadlog.TypePost, Author: uint64(bob), Body: &threadlog.PostBody{Source: "second"}},
	} {
		a.TimeMS = 1_757_500_000_000 + int64(i)
		res, err := th.Actor.Append(a)
		if err != nil {
			t.Fatal(err)
		}
		length = res.LogLength
	}
	if _, err := w.proj.ProjectThread(t.Context(), id, length); err != nil {
		t.Fatal(err)
	}
	return bob
}

// TestThreadHeaderNamesItsStarter: the page's thread.author is the thread's
// author, not its last poster, and the header a moderation answer carries
// names the same member (TKT-01M3S3ZF6J).
func TestThreadHeaderNamesItsStarter(t *testing.T) {
	w := newRepliesWorld(t)
	c := w.cache()
	id := threadid.ID(uint64(1_757_500_000_000)<<16 | 7)
	starterThread(t, w, c, id)
	s := w.service(c, nil)

	res, err := s.View(t.Context(), domain.Anonymous(), id, Query{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Body.Thread.Author; got.Handle != "alice" || got.DisplayName != "Alice" {
		t.Errorf("thread.author is %+v, want alice, who started the thread", got)
	}
	if n := len(res.Body.Posts); n != 2 || res.Body.Posts[1].Author.Handle != "bob" {
		t.Fatalf("the page's posts are not alice's then bob's: %+v", res.Body.Posts)
	}
	h, err := s.threadHeader(t.Context(), domain.Anonymous(), id)
	if err != nil {
		t.Fatal(err)
	}
	if h.Author.Handle != "alice" {
		t.Errorf("the moderation answer's header names %q, want alice", h.Author.Handle)
	}
}

// TestThreadHeaderAuthorFollowsRename: after the starter's handle changes,
// or the account is anonymized, the next view names them as they now are,
// with the response cache on and already holding the page, and with it off.
func TestThreadHeaderAuthorFollowsRename(t *testing.T) {
	for _, cached := range []bool{false, true} {
		name := "uncached"
		if cached {
			name = "cached"
		}
		t.Run(name, func(t *testing.T) {
			w := newRepliesWorld(t)
			c := w.cache()
			id := threadid.ID(uint64(1_757_500_000_000)<<16 | 8)
			starterThread(t, w, c, id)
			s := w.service(c, nil)
			if cached {
				s.Responses = respcache.New(1 << 20)
			}
			starter := func() string {
				t.Helper()
				res, err := s.View(t.Context(), domain.Anonymous(), id, Query{}, "")
				if err != nil {
					t.Fatal(err)
				}
				return res.Body.Thread.Author.Handle
			}
			if got := starter(); got != "alice" {
				t.Fatalf("before the rename the head names %q", got)
			}

			if err := w.db.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
				n, err := q.RenameUser(t.Context(), dbq.RenameUserParams{Handle: "alicia", UpdatedAt: 2, ID: w.author, OldHandle: "alice"})
				if err == nil && n != 1 {
					t.Fatalf("the rename changed %d rows", n)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if got := starter(); got != "alicia" {
				t.Errorf("after the rename the head names %q, want alicia", got)
			}

			if err := w.db.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
				return q.AnonymizeUser(t.Context(), dbq.AnonymizeUserParams{UpdatedAt: 3, ID: w.author})
			}); err != nil {
				t.Fatal(err)
			}
			res, err := s.View(t.Context(), domain.Anonymous(), id, Query{}, "")
			if err != nil {
				t.Fatal(err)
			}
			want := domain.DeletedMemberHandle(w.author)
			if got := res.Body.Thread.Author; got.Handle != want || got.DisplayName != "" {
				t.Errorf("after anonymizing, the head names %+v, want %s with no display name", got, want)
			}
		})
	}
}
