package db

import (
	"database/sql"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

func exec(t *testing.T, d *DB, query string, args ...any) error {
	t.Helper()
	return try(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), query, args...)
		return err
	})
}

func mustExec(t *testing.T, d *DB, query string, args ...any) {
	t.Helper()
	if err := exec(t, d, query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// TestReadPositionOnePerPair is AC1: the primary key refuses a second row for
// a (user, thread), and UpsertReadPosition moves the one row forward only.
func TestReadPositionOnePerPair(t *testing.T) {
	d, _ := openTest(t)
	u := newUser(t, d, "reader")
	mustExec(t, d, `INSERT INTO read_positions VALUES (?, 42, 3, 1)`, u)
	if err := exec(t, d, `INSERT INTO read_positions VALUES (?, 42, 5, 2)`, u); err == nil {
		t.Fatal("a second read_positions row for one (user, thread) was accepted")
	}
	mustExec(t, d, `INSERT INTO read_positions VALUES (?, 43, 1, 1)`, u)

	upsert := func(ordinal, at int64) dbq.ReadPosition {
		t.Helper()
		write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
			return q.UpsertReadPosition(t.Context(), dbq.UpsertReadPositionParams{
				UserID: u, ThreadID: 42, LastReadOrdinal: ordinal, LastReadAt: at})
		})
		var rp dbq.ReadPosition
		if err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
			var err error
			rp, err = q.GetReadPosition(t.Context(), dbq.GetReadPositionParams{UserID: u, ThreadID: 42})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return rp
	}
	if rp := upsert(9, 10); rp.LastReadOrdinal != 9 || rp.LastReadAt != 10 {
		t.Fatalf("after reading to 9: %+v", rp)
	}
	if rp := upsert(4, 11); rp.LastReadOrdinal != 9 || rp.LastReadAt != 11 {
		t.Fatalf("revisiting an older page moved the position back: %+v", rp)
	}
	if n := countRows(t, d, "read_positions"); n != 2 {
		t.Fatalf("%d read_positions rows, want 2: the upsert added a row", n)
	}
}

// TestDraftUniqueness is AC2: one draft per (user, thread), one new-thread
// draft per (user, forum), and a draft is for a thread or a forum, not both
// and not neither.
func TestDraftUniqueness(t *testing.T) {
	d, _ := openTest(t)
	u, other := newUser(t, d, "writer"), newUser(t, d, "other")
	f1, err := newForum(t, d, "one", 0)
	if err != nil {
		t.Fatal(err)
	}
	f2, err := newForum(t, d, "two", 0)
	if err != nil {
		t.Fatal(err)
	}
	f3, err := newForum(t, d, "three", 0) // no draft yet, so only the CHECK can refuse
	if err != nil {
		t.Fatal(err)
	}
	ins := `INSERT INTO drafts (user_id, thread_id, forum_id, source, reply_to, created_at, updated_at)
	        VALUES (?, ?, ?, 'text', ?, 1, 1)`
	null := sql.NullInt64{}
	n := func(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }
	for _, c := range []struct {
		name          string
		user          int64
		thread, forum sql.NullInt64
		replyTo       sql.NullInt64
		ok            bool
	}{
		{"a reply draft", u, n(42), null, n(3), true},
		{"a second draft for the same thread", u, n(42), null, null, false},
		{"another user's draft for that thread", other, n(42), null, null, true},
		{"the same user, another thread", u, n(43), null, null, true},
		{"a new-thread draft in forum one", u, null, n(f1), null, true},
		{"a second new-thread draft in forum one", u, null, n(f1), null, false},
		{"a new-thread draft in forum two", u, null, n(f2), null, true},
		{"neither thread nor forum", u, null, null, null, false},
		{"both thread and forum", u, n(44), n(f1), null, false},
		{"a reply target without a thread", u, null, n(f3), n(1), false},
	} {
		err := exec(t, d, ins, c.user, c.thread, c.forum, c.replyTo)
		if (err == nil) != c.ok {
			t.Errorf("%s: got %v, want accepted=%v", c.name, err, c.ok)
		}
	}
}

// TestNotificationKindsClosed is AC3: the six kinds are accepted, anything
// else is refused, and the CHECK's list is domain.NotificationKinds.
func TestNotificationKindsClosed(t *testing.T) {
	d, _ := openTest(t)
	u := newUser(t, d, "notified")
	// The application kinds name their application (0011), and only they do.
	app := newApplication(t, d, sql.NullInt64{})
	ins := `INSERT INTO notifications (user_id, kind, at, application_id) VALUES (?, ?, 1, ?)`
	for _, k := range domain.NotificationKinds {
		appID := sql.NullInt64{Int64: app, Valid: k == domain.NotifyNewApplication || k == domain.NotifyApplicationDecided}
		if err := exec(t, d, ins, u, k, appID); err != nil {
			t.Errorf("kind %s was refused: %v", k, err)
		}
	}
	for _, k := range []string{"like", "", "Mention"} {
		if err := exec(t, d, ins, u, k, nil); err == nil {
			t.Errorf("kind %q was accepted", k)
		}
	}

	var ddl string
	if err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), "SELECT sql FROM sqlite_schema WHERE name = 'notifications'").Scan(&ddl)
	}); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`kind IN \(([^)]*)\)`).FindStringSubmatch(ddl)
	if m == nil {
		t.Fatalf("no kind CHECK in %s", ddl)
	}
	var inSchema []string
	for _, s := range strings.Split(m[1], ",") {
		inSchema = append(inSchema, strings.Trim(strings.TrimSpace(s), "'"))
	}
	if !slices.Equal(inSchema, domain.NotificationKinds) {
		t.Fatalf("schema kinds %v, domain.NotificationKinds %v", inSchema, domain.NotificationKinds)
	}
}

// TestWatchIsOnOneTarget: a watch names a thread or a forum, once per user.
func TestWatchIsOnOneTarget(t *testing.T) {
	d, _ := openTest(t)
	u := newUser(t, d, "watcher")
	f, err := newForum(t, d, "watched", 0)
	if err != nil {
		t.Fatal(err)
	}
	ins := `INSERT INTO watches (user_id, thread_id, forum_id, level, created_at, updated_at) VALUES (?, ?, ?, ?, 1, 1)`
	null := sql.NullInt64{}
	n := func(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }
	for _, c := range []struct {
		name          string
		thread, forum sql.NullInt64
		level         string
		ok            bool
	}{
		{"a thread watch", n(42), null, domain.WatchWatch, true},
		{"the same thread again", n(42), null, domain.WatchMute, false},
		{"a forum mute", null, n(f), domain.WatchMute, true},
		{"the same forum again", null, n(f), domain.WatchWatch, false},
		{"neither", null, null, domain.WatchWatch, false},
		{"both", n(43), n(f), domain.WatchWatch, false},
		{"an unknown level", n(44), null, "follow", false},
	} {
		if err := exec(t, d, ins, u, c.thread, c.forum, c.level); (err == nil) != c.ok {
			t.Errorf("%s: got %v, want accepted=%v", c.name, err, c.ok)
		}
	}
	var level string
	if err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		level, err = q.ThreadWatchLevel(t.Context(), dbq.ThreadWatchLevelParams{UserID: u, ThreadID: sql.NullInt64{Int64: 42, Valid: true}})
		return err
	}); err != nil || level != domain.WatchWatch {
		t.Fatalf("ThreadWatchLevel: %q, %v", level, err)
	}
}

// TestUnreadFromMarkAllReadAlone is AC4. A user with no read_positions rows
// has every post unread, until mark_all_read_at marks everything written
// before it read; a thread partly posted after it counts only the later
// posts. With a read position the two combine: a post is unread only past
// the position and after the mark. Expected counts are worked out by hand
// from the fixture below.
//
// Thread 1: posts at 100, 200, 300. Thread 2: posts at 100, 500, 600, 700.
func TestUnreadFromMarkAllReadAlone(t *testing.T) {
	d, _ := openTest(t)
	u := newUser(t, d, "unread")
	// Another user has read everything; that must not count for u.
	someoneElse := newUser(t, d, "someone")
	for thread, times := range map[int64][]int64{1: {100, 200, 300}, 2: {100, 500, 600, 700}} {
		for i, at := range times {
			mustExec(t, d, `INSERT INTO post_meta (thread_id, ordinal, author_id, created_at, log_offset)
				VALUES (?, ?, 1, ?, ?)`, thread, i+1, at, 64*(i+1))
		}
	}
	cases := []struct {
		name         string
		mark         sql.NullInt64
		position     map[int64]int64 // thread -> last_read_ordinal
		want1, want2 int64
	}{
		{"no mark, no rows: everything unread", sql.NullInt64{}, nil, 3, 4},
		{"mark after every post: nothing unread", sql.NullInt64{Int64: 1000, Valid: true}, nil, 0, 0},
		{"mark mid-way: only later posts", sql.NullInt64{Int64: 400, Valid: true}, nil, 0, 3},
		{"mark on a post's exact time: that post is read", sql.NullInt64{Int64: 500, Valid: true}, nil, 0, 2},
		{"mark before every post", sql.NullInt64{Int64: 50, Valid: true}, nil, 3, 4},
		{"a position without a mark", sql.NullInt64{}, map[int64]int64{2: 2}, 3, 2},
		{"a position and a later mark: the mark wins", sql.NullInt64{Int64: 600, Valid: true}, map[int64]int64{2: 1}, 0, 1},
		{"a position past the mark: the position wins", sql.NullInt64{Int64: 150, Valid: true}, map[int64]int64{2: 3}, 2, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustExec(t, d, `DELETE FROM read_positions`)
			mustExec(t, d, `INSERT INTO read_positions VALUES (?, 1, 3, 1), (?, 2, 4, 1)`, someoneElse, someoneElse)
			mustExec(t, d, `UPDATE users SET mark_all_read_at = ? WHERE id = ?`, c.mark, u)
			for thread, ord := range c.position {
				mustExec(t, d, `INSERT INTO read_positions VALUES (?, ?, ?, 1)`, u, thread, ord)
			}
			for thread, want := range map[int64]int64{1: c.want1, 2: c.want2} {
				var got int64
				if err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
					var err error
					got, err = q.UnreadCount(t.Context(), dbq.UnreadCountParams{UserID: u, ThreadID: thread})
					return err
				}); err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Errorf("thread %d: %d unread, want %d", thread, got, want)
				}
			}
		})
	}
}
