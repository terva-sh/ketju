package threads

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// repliesWorld is a database, a thread root and the projection over it, with
// one public forum and one author.
type repliesWorld struct {
	t      *testing.T
	db     *db.DB
	root   string
	proj   *projection.Writer
	author int64
	forum  int64
}

func newRepliesWorld(t *testing.T) *repliesWorld {
	t.Helper()
	w := &repliesWorld{t: t, db: openDB(t), root: filepath.Join(t.TempDir(), "threads")}
	var err error
	if w.proj, err = projection.New(projection.Options{DB: w.db, Root: w.root,
		Sweep: func(context.Context) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	if err := w.db.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		if w.author, err = q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "alice", DisplayName: "Alice",
			Status: "active", Role: "member", Kind: "human", CreatedAt: 1, UpdatedAt: 1}); err != nil {
			return err
		}
		w.forum, err = q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "general", Name: "General",
			Visibility: domain.VisibilityPublic, CreatedAt: 1, UpdatedAt: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return w
}

// cache opens a fresh cache over the root: a thread in it is not open until
// something acquires it.
func (w *repliesWorld) cache() *threadcache.Cache {
	w.t.Helper()
	c, err := threadcache.New(w.root, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { c.Close() })
	return c
}

func (w *repliesWorld) service(c *threadcache.Cache, trace func(string)) *Service {
	return &Service{DB: w.db, Perm: perm.New(w.db), Threads: c, Render: render.NewCache(1 << 20), trace: trace}
}

// thread writes a random reply graph: n posts, each replying to an earlier
// post or to nothing, then some edits that move or drop a reply link and some
// deletes, so the graph the two paths must agree on is not only the one the
// posts were written with. It returns the length written, projected.
func (w *repliesWorld) thread(c *threadcache.Cache, id threadid.ID, rng *rand.Rand, n int, project bool) int64 {
	w.t.Helper()
	th, err := c.Create(id, uint64(w.forum), 1)
	if err != nil {
		w.t.Fatal(err)
	}
	defer c.Release(th)
	var length int64
	add := func(a threadlog.Append) {
		a.Author = uint64(w.author)
		a.TimeMS = 1_757_500_000_000 + int64(length)
		res, err := th.Actor.Append(a)
		if err != nil {
			w.t.Fatalf("appending %s: %v", a.Type, err)
		}
		length = res.LogLength
	}
	add(threadlog.Append{Type: threadlog.TypeThreadCreated,
		Body: &threadlog.ThreadCreatedBody{Title: "Replies", Slug: "replies", ForumID: uint64(w.forum)}})
	target := func(below int) uint32 {
		if below <= 1 || rng.IntN(4) == 0 {
			return 0
		}
		return uint32(1 + rng.IntN(below-1))
	}
	for i := 1; i <= n; i++ {
		add(threadlog.Append{Type: threadlog.TypePost, Body: &threadlog.PostBody{Source: fmt.Sprintf("post %d", i), ReplyTo: target(i)}})
	}
	for range n / 4 {
		o := 2 + rng.IntN(n-1)
		add(threadlog.Append{Type: threadlog.TypeEdit, Ordinal: uint32(o),
			Body: &threadlog.EditBody{Source: fmt.Sprintf("post %d, edited", o), ReplyTo: target(o)}})
	}
	for range n / 8 {
		add(threadlog.Append{Type: threadlog.TypeDelete, Ordinal: uint32(1 + rng.IntN(n)), Body: &threadlog.DeleteBody{}})
	}
	if project {
		if _, err := w.proj.ProjectThread(w.t.Context(), id, length); err != nil {
			w.t.Fatal(err)
		}
	}
	return length
}

func ordinalsOf(t *testing.T, s *Service, id threadid.ID, o uint32) []int {
	t.Helper()
	res, err := s.Replies(t.Context(), domain.Anonymous(), id, o, "")
	if err != nil {
		t.Fatalf("replies to %d: %v", o, err)
	}
	var out []int
	for _, p := range res.Body.Posts {
		out = append(out, p.Ordinal)
	}
	return out
}

// TestRepliesPathsAgree is AC2: over random reply graphs, with edits moving
// and dropping links and deletes among them, the index path (thread open) and
// the post_meta path (thread not open) return the same replies in ordinal
// order for every post, and trace shows each path actually ran.
func TestRepliesPathsAgree(t *testing.T) {
	w := newRepliesWorld(t)
	writer := w.cache()
	for seed := range uint64(6) {
		rng := rand.New(rand.NewPCG(seed, 42))
		id := threadid.ID(uint64(1_757_500_000_000)<<16 | (seed + 1))
		n := 20 + rng.IntN(40)
		w.thread(writer, id, rng, n, true)

		// The writer's cache still holds the thread open: the index path.
		var paths []string
		open := w.service(writer, func(p string) { paths = append(paths, p) })
		th, err := writer.Acquire(id)
		if err != nil {
			t.Fatal(err)
		}
		byIndex := map[uint32][]int{}
		for o := uint32(1); o <= uint32(n); o++ {
			byIndex[o] = ordinalsOf(t, open, id, o)
			if !slices.IsSorted(byIndex[o]) {
				t.Fatalf("seed %d: replies to %d not in ordinal order: %v", seed, o, byIndex[o])
			}
		}
		writer.Release(th)

		// For each ordinal, a cache that has never opened the thread: post_meta.
		for o := uint32(1); o <= uint32(n); o++ {
			closed := w.service(w.cache(), func(p string) { paths = append(paths, p) })
			if got := ordinalsOf(t, closed, id, o); !slices.Equal(got, byIndex[o]) {
				t.Fatalf("seed %d, replies to %d: post_meta says %v, the index %v", seed, o, got, byIndex[o])
			}
		}
		if c := countOf(paths, pathIndex); c != n {
			t.Fatalf("seed %d: the index path ran %d times of %d", seed, c, n)
		}
		if c := countOf(paths, pathPostMeta); c != n {
			t.Fatalf("seed %d: the post_meta path ran %d times of %d", seed, c, n)
		}
	}
}

// TestRepliesPostMetaBehindUsesTheIndex: a thread the projection has not
// caught up with is answered from the index even when the thread is not
// open, so a reply the projection has not seen is not missing.
func TestRepliesPostMetaBehindUsesTheIndex(t *testing.T) {
	w := newRepliesWorld(t)
	writer := w.cache()
	id := threadid.ID(uint64(1_757_500_000_000)<<16 | 1)
	w.thread(writer, id, rand.New(rand.NewPCG(1, 1)), 10, true)
	if err := writer.With(id, func(th *threadcache.Thread) error {
		_, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypePost, Author: uint64(w.author), TimeMS: 1_800_000_000_000,
			Body: &threadlog.PostBody{Source: "not projected yet", ReplyTo: 1}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var paths []string
	s := w.service(w.cache(), func(p string) { paths = append(paths, p) })
	got := ordinalsOf(t, s, id, 1)
	if !slices.Contains(got, 11) || !slices.Equal(paths, []string{pathIndex}) {
		t.Fatalf("with the projection behind: replies %v via %v; want 11 among them, via the index", got, paths)
	}
}

func countOf(xs []string, x string) int {
	n := 0
	for _, v := range xs {
		if v == x {
			n++
		}
	}
	return n
}
