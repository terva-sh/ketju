package notifier

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// start is the fixture's clock at its first record. Every record is dated
// after it, and Now stays an hour past the last one, inside the horizon.
const start = 1_790_000_000_000

// fixture is a migrated database with five members and a public forum, a
// thread cache writing real logs, the projection, and a Generator over them
// whose install cutoff is before every record.
type fixture struct {
	t     testing.TB
	db    *db.DB
	root  string
	cache *threadcache.Cache
	proj  *projection.Writer
	perm  *perm.Evaluator
	g     *Generator
	users map[string]int64
	forum int64
	clock atomic.Int64
	seq   atomic.Uint64
}

func newFixture(t testing.TB) *fixture {
	t.Helper()
	d := dbtest.Open(t, db.Options{Path: filepath.Join(t.TempDir(), "db", "ketju.db")})
	root := filepath.Join(t.TempDir(), "threads")
	c, err := threadcache.New(root, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	f := &fixture{t: t, db: d, root: root, cache: c, perm: perm.New(d), users: map[string]int64{}}
	f.clock.Store(start)
	f.proj, err = projection.New(projection.Options{DB: d, Root: root, Sweep: func(ctx context.Context) error {
		_, err := f.proj.CatchUp(ctx, f.reopen)
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	f.g = f.generator(1)
	for _, u := range []struct{ handle, role string }{
		{"alice", domain.RoleMember}, {"bob", domain.RoleMember}, {"carol", domain.RoleMember},
		{"dave", domain.RoleMember}, {"mod", domain.RoleModerator},
	} {
		f.write(func(q *dbq.Queries) error {
			id, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: u.handle, DisplayName: u.handle,
				Status: domain.StatusActive, Role: u.role, Kind: "human", CreatedAt: 1, UpdatedAt: 1})
			f.users[u.handle] = id
			return err
		})
	}
	f.forum = f.newForum("general", domain.VisibilityPublic)
	return f
}

// generator is a Generator over the fixture with the install cutoff since,
// zero for the migration's own.
func (f *fixture) generator(since int64) *Generator {
	f.t.Helper()
	g, err := New(Options{DB: f.db, Root: f.root, Perm: f.perm, Since: since,
		Project: func(ctx context.Context, id threadid.ID, upTo int64) error {
			_, err := f.proj.ProjectThread(ctx, id, upTo)
			return err
		},
		Sweep:       func(ctx context.Context) error { _, err := f.proj.CatchUp(ctx, f.reopen); return err },
		Quarantined: f.proj.Quarantined,
		Now:         func() time.Time { return time.UnixMilli(f.clock.Load() + int64(time.Hour/time.Millisecond)) },
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return g
}

func (f *fixture) reopen(_ context.Context, id threadid.ID) (int64, error) {
	var n int64
	err := f.cache.With(id, func(th *threadcache.Thread) error { n = th.Actor.LogLength(); return nil })
	return n, err
}

func (f *fixture) newForum(slug, visibility string) int64 {
	f.t.Helper()
	var id int64
	f.write(func(q *dbq.Queries) error {
		var err error
		id, err = q.CreateForum(f.t.Context(), dbq.CreateForumParams{Slug: slug, Name: slug, Visibility: visibility,
			CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	return id
}

func (f *fixture) write(fn func(q *dbq.Queries) error) {
	f.t.Helper()
	if err := f.db.Write(f.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error { return fn(q) }); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) exec(query string, args ...any) {
	f.t.Helper()
	if err := f.db.Write(f.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(f.t.Context(), query, args...)
		return err
	}); err != nil {
		f.t.Fatalf("%s: %v", query, err)
	}
	f.perm.Invalidate()
}

// thread creates a thread in forum, started by alice, and returns it with
// the event its creation published.
func (f *fixture) thread(forum int64) (threadid.ID, events.Event) {
	f.t.Helper()
	id := threadid.ID(uint64(start)<<16 | f.seq.Add(1))
	th, err := f.cache.Create(id, uint64(forum), f.clock.Load())
	if err != nil {
		f.t.Fatal(err)
	}
	f.cache.Release(th)
	return id, f.append(id, threadlog.Append{Type: threadlog.TypeThreadCreated, Author: uint64(f.users["alice"]),
		Body: &threadlog.ThreadCreatedBody{Title: "T", Slug: "t", ForumID: uint64(forum)}})
}

// append writes a record a second after the last and returns its event. It
// does not project: delivering the event is the generator's to do.
func (f *fixture) append(id threadid.ID, a threadlog.Append) events.Event {
	f.t.Helper()
	if a.TimeMS == 0 {
		a.TimeMS = f.clock.Add(1000)
	}
	var res threadlog.Result
	if err := f.cache.With(id, func(th *threadcache.Thread) error {
		var err error
		res, err = th.Actor.Append(a)
		return err
	}); err != nil {
		f.t.Fatalf("appending %s: %v", a.Type, err)
	}
	return events.Event{Thread: id, Ordinal: res.Ordinal, LogLength: res.LogLength}
}

// post appends author's post with source, answering replyTo and naming
// mentions.
func (f *fixture) post(id threadid.ID, author, source string, replyTo uint32, mentions ...string) events.Event {
	f.t.Helper()
	var ids []uint64
	for _, m := range mentions {
		ids = append(ids, uint64(f.users[m]))
	}
	return f.append(id, threadlog.Append{Type: threadlog.TypePost, Author: uint64(f.users[author]),
		Body: &threadlog.PostBody{Source: source, ReplyTo: replyTo, Mentions: ids}})
}

// edit appends editor's edit of ordinal, with mentions as its mentions key:
// nil leaves the key out.
func (f *fixture) edit(id threadid.ID, editor string, ordinal uint32, mentions []string) events.Event {
	f.t.Helper()
	b := &threadlog.EditBody{Source: "edited"}
	if mentions != nil {
		ids := []uint64{}
		for _, m := range mentions {
			ids = append(ids, uint64(f.users[m]))
		}
		b.Mentions = &ids
	}
	return f.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: uint64(f.users[editor]), Ordinal: ordinal,
		Body: b})
}

func (f *fixture) deliver(evs ...events.Event) {
	f.t.Helper()
	if err := f.g.Deliver(f.t.Context(), events.Batch{Events: evs}); err != nil {
		f.t.Fatal(err)
	}
}

// watch sets handle's level on a thread (forum 0) or a forum (thread 0),
// chosen before every record.
func (f *fixture) watch(handle string, thread threadid.ID, forum int64, level string) {
	f.t.Helper()
	f.watchAt(handle, thread, forum, level, 1)
}

// watchAt is watch with the level chosen at at, in Unix milliseconds.
func (f *fixture) watchAt(handle string, thread threadid.ID, forum int64, level string, at int64) {
	f.t.Helper()
	if thread != 0 {
		f.exec(`INSERT INTO watches (user_id, thread_id, level, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			f.users[handle], int64(thread), level, at, at)
	} else {
		f.exec(`INSERT INTO watches (user_id, forum_id, level, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			f.users[handle], forum, level, at, at)
	}
}

// rows is every notification, one line each in id order: who, what, which
// post, by whom, and whether it is read.
func (f *fixture) rows() string {
	f.t.Helper()
	handle := map[int64]string{}
	for h, id := range f.users {
		handle[id] = h
	}
	var b strings.Builder
	err := f.db.Read(f.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rs, err := tx.QueryContext(f.t.Context(), `SELECT user_id, kind, coalesce(ordinal, 0), coalesce(actor_id, 0),
			read_at IS NOT NULL FROM notifications ORDER BY id`)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var user, ordinal, actor int64
			var kind string
			var read bool
			if err := rs.Scan(&user, &kind, &ordinal, &actor, &read); err != nil {
				return err
			}
			fmt.Fprintf(&b, "%s %s #%d by %s", handle[user], kind, ordinal, handle[actor])
			if read {
				b.WriteString(" read")
			}
			b.WriteString("\n")
		}
		return rs.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return b.String()
}

// want fails unless the notifications are exactly lines, in id order.
func (f *fixture) want(lines ...string) {
	f.t.Helper()
	want := ""
	for _, l := range lines {
		want += l + "\n"
	}
	if got := f.rows(); got != want {
		f.t.Fatalf("notifications:\n%s\nwant:\n%s", got, want)
	}
}

func (f *fixture) count(query string, args ...any) int64 {
	f.t.Helper()
	var n int64
	if err := f.db.Read(f.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(f.t.Context(), query, args...).Scan(&n)
	}); err != nil {
		f.t.Fatal(err)
	}
	return n
}
