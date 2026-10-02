package notifier

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// quote is source quoting post #ordinal with a line of its text, as the
// composer writes one.
func quote(ordinal string, rest string) string {
	return "> [quote=#" + ordinal + "]\n> the quoted line\n\n" + rest
}

// TestOnePostOneNotificationPerRecipient is AC1: a post that replies to,
// quotes and mentions bob in a thread he watches gives him one row, a
// mention. carol, who only watches, already has an unread watch row from
// the first post, so the second adds nothing for her.
func TestOnePostOneNotificationPerRecipient(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread(f.forum)
	f.watch("bob", id, 0, domain.WatchWatch)
	f.watch("carol", id, 0, domain.WatchWatch)
	p1 := f.post(id, "bob", "first", 0)
	p2 := f.post(id, "alice", quote("1", "Agreed, @bob."), 1, "bob")
	f.deliver(created, p1, p2)
	f.want(
		"carol thread_you_watch #1 by bob",
		"bob mention #2 by alice",
	)
}

// TestStrongestKindWins is AC1's ranking: mention, then reply_to_you, then
// quoted, then thread_you_watch, as the Go table and the SQL upsert both
// state it. Every ordered pair of kinds written for one post leaves the
// stronger, in either order, and a generated post that replies to one
// member and quotes another who watches gives each the stronger of theirs.
func TestStrongestKindWins(t *testing.T) {
	order := []string{domain.NotifyMention, domain.NotifyReplyToYou, domain.NotifyQuoted, domain.NotifyThreadYouWatch}
	for i := 1; i < len(order); i++ {
		if rank[order[i-1]] <= rank[order[i]] {
			t.Fatalf("rank puts %s at or below %s", order[i-1], order[i])
		}
	}
	if len(rank) != len(order) {
		t.Fatalf("rank has %d kinds, want %d", len(rank), len(order))
	}

	f := newFixture(t)
	bob := f.users["bob"]
	ordinal := int64(0)
	write := func(q *dbq.Queries, kind string, n int64) error {
		p := dbq.UpsertPostNotificationParams{UserID: bob, Kind: kind, ThreadID: sql.NullInt64{Int64: 1, Valid: true},
			Ordinal: sql.NullInt64{Int64: n, Valid: true}, At: 1}
		if kind == domain.NotifyThreadYouWatch {
			return q.InsertWatchNotification(t.Context(), dbq.InsertWatchNotificationParams{UserID: p.UserID,
				ThreadID: p.ThreadID, Ordinal: p.Ordinal, At: p.At})
		}
		return q.UpsertPostNotification(t.Context(), p)
	}
	for _, a := range order {
		for _, b := range order {
			ordinal++
			f.write(func(q *dbq.Queries) error {
				if err := write(q, a, ordinal); err != nil {
					return err
				}
				return write(q, b, ordinal)
			})
			want := a
			if rank[b] > rank[a] {
				want = b
			}
			var got string
			if err := f.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
				return tx.QueryRowContext(t.Context(), `SELECT group_concat(kind) FROM notifications
					WHERE user_id = ? AND ordinal = ?`, bob, ordinal).Scan(&got)
			}); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("%s then %s left %q, want %q", a, b, got, want)
			}
		}
	}

	// carol is replied to; dave is quoted in a thread he watches.
	f2 := newFixture(t)
	id, created := f2.thread(f2.forum)
	p1 := f2.post(id, "carol", "carol's", 0)
	p2 := f2.post(id, "dave", "dave's", 0)
	f2.deliver(created, p1, p2)
	f2.watch("dave", id, 0, domain.WatchWatch)
	p3 := f2.post(id, "alice", quote("2", "and also"), 1)
	f2.deliver(p3)
	f2.want(
		"carol reply_to_you #3 by alice",
		"dave quoted #3 by alice",
	)
}

// TestQuotedReadsTheRecordedBacklinks (TKT-01M3YME9AZ): quoted comes from
// the post_quotes rows the projection writer recorded, not from rendering
// the source. Rows deleted before generation give nothing, a row the source
// does not back gives a quoted, a row naming the post itself or a later one
// gives nothing, and an Edit in the same range is read as it left the post.
func TestQuotedReadsTheRecordedBacklinks(t *testing.T) {
	// project brings the projection to evs' end without generating.
	project := func(f *fixture, id threadid.ID, evs ...events.Event) {
		t.Helper()
		if _, err := f.proj.ProjectThread(t.Context(), id, evs[len(evs)-1].LogLength); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("from the rows", func(t *testing.T) {
		f := newFixture(t)
		id, created := f.thread(f.forum)
		evs := []events.Event{created,
			f.post(id, "bob", "bob's", 0),
			f.post(id, "carol", "carol's", 0),
			f.post(id, "alice", quote("1", "")+quote("2", "quoting both"), 0),
		}
		f.deliver(evs...)
		f.want("bob quoted #3 by alice", "carol quoted #3 by alice")
	})

	t.Run("no rows, no quoted", func(t *testing.T) {
		f := newFixture(t)
		id, created := f.thread(f.forum)
		evs := []events.Event{created,
			f.post(id, "bob", "bob's", 0),
			f.post(id, "alice", quote("1", "quoting bob"), 0),
		}
		project(f, id, evs...)
		if n := f.count(`SELECT count(*) FROM post_quotes`); n != 1 {
			t.Fatalf("the projection recorded %d backlinks, want 1", n)
		}
		f.exec(`DELETE FROM post_quotes`)
		f.deliver(evs...)
		f.want()
	})

	t.Run("a row the source does not back", func(t *testing.T) {
		f := newFixture(t)
		id, created := f.thread(f.forum)
		evs := []events.Event{created,
			f.post(id, "bob", "bob's", 0),
			f.post(id, "carol", "carol's", 0),
			f.post(id, "alice", "quotes nobody", 0),
		}
		project(f, id, evs...)
		// #3 quoting #1 is read; #3 quoting itself and #3 quoting the later
		// #4 are not, as quoted() kept only earlier posts.
		f.exec(`INSERT INTO post_quotes (thread_id, ordinal, quoted_ordinal) VALUES (?, 3, 1), (?, 2, 4)`,
			int64(id), int64(id))
		evs = append(evs, f.post(id, "dave", "dave's", 0))
		project(f, id, evs...)
		f.deliver(evs...)
		f.want("bob quoted #3 by alice")
	})

	// A post projected before migration 0025, and not yet generated, has
	// no rows; the migration rewinds the projection, and the generator waits
	// for the replay that writes them (Terva review 1998).
	t.Run("projected before the table existed", func(t *testing.T) {
		f := newFixture(t)
		id, created := f.thread(f.forum)
		evs := []events.Event{created,
			f.post(id, "bob", "bob's", 0),
			f.post(id, "alice", quote("1", "quoting bob"), 0),
		}
		project(f, id, evs...)
		f.exec(`DELETE FROM post_quotes`)
		f.exec(`UPDATE projection_state SET log_length = 64 WHERE log_length > 64`) // as 0025 does
		f.deliver(evs...)
		f.want("bob quoted #2 by alice")
	})

	t.Run("an edit in the same range", func(t *testing.T) {
		f := newFixture(t)
		id, created := f.thread(f.forum)
		evs := []events.Event{created,
			f.post(id, "bob", "bob's", 0),
			f.post(id, "carol", "carol's", 0),
			f.post(id, "alice", quote("1", "quoting bob"), 0),
		}
		// alice's edit swaps the quotation before the generator has seen the
		// post: carol is quoted by the post as readers see it, bob is not.
		evs = append(evs, f.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: uint64(f.users["alice"]),
			Ordinal: 3, Body: &threadlog.EditBody{Source: quote("2", "quoting carol instead")}}))
		f.deliver(evs...)
		f.want("carol quoted #3 by alice")
	})
}

// TestNoSelfNotification is AC2: an author who watches their own thread
// and replies to, quotes and mentions themselves gets nothing, and neither
// does an edit naming them.
func TestNoSelfNotification(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread(f.forum)
	f.watch("alice", id, 0, domain.WatchWatch)
	p1 := f.post(id, "alice", "mine", 0)
	p2 := f.post(id, "alice", quote("1", "@alice, as I said"), 1, "alice")
	e := f.edit(id, "alice", 2, []string{"alice"})
	f.deliver(created, p1, p2, e)
	f.want()
}

// TestMuteSuppressesEveryKind is AC3: a thread mute and a forum mute each
// stop a reply, a quote, a mention and a watch, and a thread watch inside a
// muted forum still alerts.
func TestMuteSuppressesEveryKind(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread(f.forum)
	// bob watches the forum but muted this thread; carol muted the forum;
	// dave muted the forum but watches this thread.
	f.watch("bob", 0, f.forum, domain.WatchWatch)
	f.watch("bob", id, 0, domain.WatchMute)
	f.watch("carol", 0, f.forum, domain.WatchMute)
	f.watch("dave", 0, f.forum, domain.WatchMute)
	f.watch("dave", id, 0, domain.WatchWatch)
	evs := []events.Event{created,
		f.post(id, "bob", "bob's", 0),
		f.post(id, "carol", "carol's", 0),
		f.post(id, "alice", "a reply to each", 1),
		f.post(id, "alice", "a reply to carol", 2),
		f.post(id, "alice", quote("1", "")+quote("2", "quoting both"), 0),
		f.post(id, "alice", "naming them", 0, "bob", "carol", "dave"),
	}
	evs = append(evs, f.edit(id, "alice", 6, []string{"bob", "carol"}))
	f.deliver(evs...)
	f.want(
		"dave thread_you_watch #1 by bob",
		"dave mention #6 by alice",
	)
}

// TestReplayIsIdempotent is AC4: delivering the same events again, then
// again with the progress reset to the file header, then as a batch
// reporting drops, writes nothing new and leaves a read row read.
func TestReplayIsIdempotent(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread(f.forum)
	f.watch("carol", id, 0, domain.WatchWatch)
	evs := []events.Event{created,
		f.post(id, "bob", "first", 0),
		f.post(id, "alice", quote("1", "@dave"), 1, "dave"),
		f.post(id, "dave", "third", 2),
	}
	evs = append(evs, f.edit(id, "alice", 2, []string{"dave", "mod"}))
	f.deliver(evs...)
	f.exec(`UPDATE notifications SET read_at = 5 WHERE kind = 'mention' AND user_id = ?`, f.users["dave"])
	want := f.rows()
	if !strings.Contains(want, " read\n") {
		t.Fatalf("the first delivery wrote:\n%s", want)
	}

	f.deliver(evs...)
	if got := f.rows(); got != want {
		t.Fatalf("a second delivery:\n%s\nwant:\n%s", got, want)
	}
	f.exec(`UPDATE notification_progress SET log_length = ?`, threadlog.FileHeaderSize)
	f.deliver(evs[len(evs)-1])
	if got := f.rows(); got != want {
		t.Fatalf("a delivery from the file header:\n%s\nwant:\n%s", got, want)
	}
	f.exec(`DELETE FROM notification_progress`)
	if err := f.g.Deliver(t.Context(), events.Batch{Events: evs[:1], Dropped: 3}); err != nil {
		t.Fatal(err)
	}
	if got := f.rows(); got != want {
		t.Fatalf("a batch reporting drops:\n%s\nwant:\n%s", got, want)
	}
}

// TestCatchUpAfterCrash is AC5: whatever point a process stops at, the next
// start generates each missing notification once.
func TestCatchUpAfterCrash(t *testing.T) {
	t.Run("projected, not generated", func(t *testing.T) {
		// The process stopped after the projection applied the posts and
		// before the generator saw them.
		f := newFixture(t)
		id, _ := f.thread(f.forum)
		f.post(id, "bob", "first", 0)
		end := f.post(id, "alice", "@carol", 1, "carol")
		if _, err := f.proj.ProjectThread(t.Context(), id, end.LogLength); err != nil {
			t.Fatal(err)
		}
		g := f.generator(1) // a fresh start
		for range 2 {
			if err := g.CatchUp(t.Context()); err != nil {
				t.Fatal(err)
			}
			f.want("bob reply_to_you #2 by alice", "carol mention #2 by alice")
		}
	})
	t.Run("rows written, not committed", func(t *testing.T) {
		f := newFixture(t)
		id, created := f.thread(f.forum)
		p := f.post(id, "alice", "@carol", 0, "carol")
		fail := true
		f.g.afterWrites = failOnce(&fail)
		f.deliver(created, p)
		f.want()
		if l := f.g.Lag(); l.Threads != 1 {
			t.Fatalf("lag %+v after a failed transaction, want the thread held", l)
		}
		if n := f.count(`SELECT count(*) FROM notification_progress WHERE thread_id = ? AND log_length >= ?`,
			int64(id), p.LogLength); n != 0 {
			t.Fatal("the progress moved with the rows rolled back")
		}
		if err := f.g.Retry(t.Context()); err != nil {
			t.Fatal(err)
		}
		f.want("carol mention #1 by alice")
		if err := f.g.Retry(t.Context()); err != nil {
			t.Fatal(err)
		}
		f.want("carol mention #1 by alice")
		if l := f.g.Lag(); l.Threads != 0 {
			t.Fatalf("lag %+v after the retry", l)
		}
	})
	t.Run("held and retried by Run", func(t *testing.T) {
		retryFloor = 5 * time.Millisecond
		t.Cleanup(func() { retryFloor = time.Second })
		f := newFixture(t)
		id, created := f.thread(f.forum)
		p := f.post(id, "alice", "@carol", 0, "carol")
		fail := true
		f.g.afterWrites = failOnce(&fail)
		f.g.sweepOwed.Store(false)
		f.deliver(created, p)
		f.want()
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() { defer close(done); f.g.Run(ctx) }()
		waitFor(t, func() bool { return f.rows() != "" })
		cancel()
		<-done
		f.want("carol mention #1 by alice")
	})
}

// failOnce is an afterWrites hook that fails the first transaction it sees
// while *fail, as a process stopping between the rows and the commit would.
func failOnce(fail *bool) func(threadid.ID) error {
	return func(threadid.ID) error {
		if *fail {
			*fail = false
			return errors.New("the process stopped here")
		}
		return nil
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestDroppedEventOnIdleThreadIsGenerated is review 1743's requirement: an
// event both subscribers dropped, for a thread nothing appends to again,
// still generates. The projection had not applied the post either, so the
// generator's sweep must run the projection's first and find the thread
// from its log, not from projection_state.
func TestDroppedEventOnIdleThreadIsGenerated(t *testing.T) {
	f := newFixture(t)
	idle, _ := f.thread(f.forum)
	end := f.post(idle, "alice", "@carol", 0, "carol")
	_, created := f.thread(f.forum)
	if err := f.g.Deliver(t.Context(), events.Batch{Events: []events.Event{created}, Dropped: 2}); err != nil {
		t.Fatal(err)
	}
	f.want("carol mention #1 by alice")
	if n := f.count(`SELECT log_length FROM projection_state WHERE thread_id = ?`, int64(idle)); n != end.LogLength {
		t.Fatalf("the projection reached %d of %d", n, end.LogLength)
	}

	// The same through Run's sweep, as at a start, with no batch at all.
	retryFloor = 5 * time.Millisecond
	t.Cleanup(func() { retryFloor = time.Second })
	idle2, _ := f.thread(f.forum)
	f.post(idle2, "bob", "@dave", 0, "dave")
	f.g.sweepOwed.Store(true)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); f.g.Run(ctx) }()
	waitFor(t, func() bool { return f.count(`SELECT count(*) FROM notifications`) == 2 })
	cancel()
	<-done
	f.want("carol mention #1 by alice", "dave mention #1 by bob")

	// A process that stopped mid-append left a torn frame after a post
	// neither subscriber saw. Only the projection's sweep cuts it, by
	// opening the thread through the store; without it the log on disk is
	// longer than any frame boundary, and nothing past the projection could
	// be read. With it, the post is alerted and nothing stays behind.
	torn, _ := f.thread(f.forum)
	end = f.post(torn, "carol", "@bob", 0, "bob")
	if err := f.cache.Close(); err != nil {
		t.Fatal(err)
	}
	lf, err := os.OpenFile(torn.LogPath(f.root), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lf.Write([]byte{0x40, 0, 0, 0, 1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	lf.Close()
	if f.cache, err = threadcache.New(f.root, threadcache.Options{Sync: threadlog.SyncNever}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.cache.Close() })
	if err := f.g.Deliver(t.Context(), events.Batch{Events: []events.Event{created}, Dropped: 1}); err != nil {
		t.Fatal(err)
	}
	f.want("carol mention #1 by alice", "dave mention #1 by bob", "bob mention #1 by carol")
	if l := f.g.Lag(); l.Threads != 0 {
		t.Fatalf("lag %+v after the sweep cut the torn frame", l)
	}
}

// TestFirstStartAlertsNoHistory is AC5's first start: nothing written at or
// before the generator's migration is alerted, whether the projection had
// reached it when the migration ran or reached it only at the next
// catch-up. Its range is consumed, and a post after it is alerted.
func TestFirstStartAlertsNoHistory(t *testing.T) {
	f := newFixture(t)
	f.g = f.generator(0)
	since, err := f.g.cutoff(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if since <= 0 {
		t.Fatalf("the cutoff is %d", since)
	}
	f.clock.Store(since - 60_000)
	projected, _ := f.thread(f.forum)
	f.watch("carol", projected, 0, domain.WatchWatch)
	f.post(projected, "bob", "history", 0)
	old := f.post(projected, "alice", "@carol, long ago", 1, "carol")
	if _, err := f.proj.ProjectThread(t.Context(), projected, old.LogLength); err != nil {
		t.Fatal(err)
	}
	unprojected, _ := f.thread(f.forum)
	f.post(unprojected, "alice", "@dave, also long ago", 0, "dave")

	if err := f.g.CatchUp(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.want()
	if n := f.count(`SELECT count(*) FROM notification_progress WHERE thread_id IN (?, ?)`,
		int64(projected), int64(unprojected)); n != 2 {
		t.Fatalf("%d threads' history consumed, want 2", n)
	}

	f.clock.Store(since + 1)
	f.deliver(f.post(projected, "dave", "@bob, now", 2, "bob"))
	f.want("alice reply_to_you #3 by dave", "bob mention #3 by dave", "carol thread_you_watch #3 by dave")
}

// TestHorizonSkipsOldRecords: a record more than seven days older than the
// moment it is processed generates nothing, as after a database restored
// without the generator's progress; one inside the horizon does.
func TestHorizonSkipsOldRecords(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread(f.forum)
	now := f.clock.Load() + int64(30*24*time.Hour/time.Millisecond)
	f.g.now = func() time.Time { return time.UnixMilli(now) }
	old := f.append(id, threadlog.Append{Type: threadlog.TypePost, Author: uint64(f.users["alice"]),
		TimeMS: now - int64(Horizon/time.Millisecond) - 1, Body: &threadlog.PostBody{Source: "@bob",
			Mentions: []uint64{uint64(f.users["bob"])}}})
	recent := f.append(id, threadlog.Append{Type: threadlog.TypePost, Author: uint64(f.users["alice"]),
		TimeMS: now - int64(Horizon/time.Millisecond) + 1, Body: &threadlog.PostBody{Source: "@carol",
			Mentions: []uint64{uint64(f.users["carol"])}}})
	f.deliver(created, old, recent)
	f.want("carol mention #2 by alice")
}

// TestWatchCollapsesToFirstUnread: five posts give a watcher one unread row
// naming the first; once it is read, the next post gives a new one, and the
// one after that nothing.
func TestWatchCollapsesToFirstUnread(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread(f.forum)
	f.watch("carol", 0, f.forum, domain.WatchWatch)
	evs := []events.Event{created}
	for range 5 {
		evs = append(evs, f.post(id, "alice", "more", 0))
	}
	f.deliver(evs...)
	f.want("carol thread_you_watch #1 by alice")
	f.exec(`UPDATE notifications SET read_at = 9`)
	f.deliver(f.post(id, "bob", "after reading", 0))
	f.deliver(f.post(id, "alice", "and again", 0))
	f.want("carol thread_you_watch #1 by alice read", "carol thread_you_watch #6 by bob")
}

// TestEditNotifiesNewMentionsOnly: an edit whose mentions key names
// members alerts the ones not yet alerted about the post, moves an unread
// row to a stronger kind, and alerts nobody twice. An edit without the key,
// and one naming only the post's author, alert nobody.
func TestEditNotifiesNewMentionsOnly(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread(f.forum)
	f.watch("dave", id, 0, domain.WatchWatch)
	p := f.post(id, "alice", "@bob", 0, "bob")
	f.deliver(created, p)
	f.want("bob mention #1 by alice", "dave thread_you_watch #1 by alice")

	f.deliver(
		f.edit(id, "alice", 1, []string{"bob", "carol", "dave"}),
		f.edit(id, "alice", 1, nil),
		f.edit(id, "mod", 1, []string{"alice"}),
		f.edit(id, "alice", 1, []string{"bob", "carol", "dave"}),
	)
	f.want("bob mention #1 by alice", "dave mention #1 by alice", "carol mention #1 by alice")
}

// TestEditLeavesReadNotificationAlone (Terva review 1781): an edit that
// mentions a member moves their unread row for the post to mention, but a
// row they have read stays the kind and actor they read, still read. Only
// an unread row may move to a stronger kind (docs/04, run decision
// 2026-10-01).
func TestEditLeavesReadNotificationAlone(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread(f.forum)
	f.watch("dave", id, 0, domain.WatchWatch)
	f.watch("carol", id, 0, domain.WatchWatch)
	f.deliver(created, f.post(id, "alice", "hello", 0))
	f.want("carol thread_you_watch #1 by alice", "dave thread_you_watch #1 by alice")
	f.exec(`UPDATE notifications SET read_at = 5 WHERE user_id = ?`, f.users["dave"])

	f.deliver(f.edit(id, "mod", 1, []string{"carol", "dave"}))
	f.want("carol mention #1 by mod", "dave thread_you_watch #1 by alice read")
}

// TestGeneratorSkipsInactiveAndUnreadable: a suspended or deactivated
// member, and one without read on a restricted forum, get nothing; a
// moderator by role does not read a restricted forum by that role alone,
// so gets nothing there either.
func TestGeneratorSkipsInactiveAndUnreadable(t *testing.T) {
	f := newFixture(t)
	f.exec(`UPDATE users SET status = ? WHERE id = ?`, domain.StatusSuspended, f.users["bob"])
	f.exec(`UPDATE users SET status = ? WHERE id = ?`, domain.StatusDeactivated, f.users["carol"])
	open, created := f.thread(f.forum)
	f.deliver(created, f.post(open, "alice", "everyone", 0, "bob", "carol", "dave"))
	f.want("dave mention #1 by alice")

	staff := f.newForum("staff", domain.VisibilityRestricted)
	f.exec(`INSERT INTO groups (id, name, slug, created_at, updated_at) VALUES (100, 'Insiders', 'insiders', 1, 1)`)
	f.exec(`INSERT INTO group_members (group_id, user_id, source, added_at) VALUES (100, ?, 'admin', 1), (100, ?, 'admin', 1)`,
		f.users["alice"], f.users["dave"])
	f.exec(`INSERT INTO permissions (forum_id, group_id, permission, created_at) VALUES (?, 100, 'read', 1)`, staff)
	closed, created := f.thread(staff)
	f.deliver(created, f.post(closed, "alice", "insiders only", 0, "dave", "mod"))
	f.want("dave mention #1 by alice", "dave mention #1 by alice")
}

// TestRecipientViewerMatchesSession is AC9's second half: the viewer the
// generator decides a recipient's access as gets, from perm, exactly what
// that member's own session gets, for a member, a moderator by role, an
// admin, a moderator by group grant, and a suspended moderator. That keeps
// the generator inside "role changes go through perm".
func TestRecipientViewerMatchesSession(t *testing.T) {
	f := newFixture(t)
	staff := f.newForum("staff", domain.VisibilityRestricted)
	members := f.newForum("members", domain.VisibilityMembers)
	f.exec(`INSERT INTO users (id, handle, display_name, status, role, kind, created_at, updated_at) VALUES
		(50, 'root', 'root', 'active', 'admin', 'human', 1, 1),
		(51, 'gmod', 'gmod', 'active', 'member', 'human', 1, 1),
		(52, 'gone', 'gone', 'suspended', 'moderator', 'human', 1, 1)`)
	f.users["root"], f.users["gmod"], f.users["gone"] = 50, 51, 52
	f.exec(`INSERT INTO groups (id, name, slug, created_at, updated_at) VALUES (100, 'Mods', 'mods', 1, 1)`)
	f.exec(`INSERT INTO group_members (group_id, user_id, source, added_at) VALUES (100, 51, 'admin', 1)`)
	f.exec(`INSERT INTO permissions (forum_id, group_id, permission, created_at) VALUES
		(?, 100, 'read', 1), (?, 100, 'moderate', 1)`, staff, staff)

	store := &session.Store{DB: f.db}
	ids := []int64{f.users["alice"], f.users["mod"], 50, 51, 52}
	for _, uid := range ids {
		token := sessiontest.SignIn(t, f.db, uid, time.Now())
		sess, err := store.Lookup(t.Context(), token)
		if err != nil {
			t.Fatal(err)
		}
		var u dbq.User
		f.write(func(q *dbq.Queries) error {
			var err error
			u, err = q.GetUser(t.Context(), uid)
			return err
		})
		sv, rv := sess.Viewer(), RecipientViewer(u)
		if sv.UserID != rv.UserID || sv.Role != rv.Role || sv.Active != rv.Active {
			t.Errorf("user %d: the session's viewer is %+v, the generator's %+v", uid, sv, rv)
		}
		for _, forum := range []int64{f.forum, staff, members} {
			for _, p := range []domain.Permission{domain.PermRead, domain.PermModerate} {
				sd, err := f.perm.Check(t.Context(), sv, forum, p)
				if err != nil {
					t.Fatal(err)
				}
				rd, err := f.perm.Check(t.Context(), rv, forum, p)
				if err != nil {
					t.Fatal(err)
				}
				if sd != rd {
					t.Errorf("user %d, forum %d, %s: the session gets %s, the generator %s", uid, forum, p, sd, rd)
				}
			}
		}
	}
	// The moderator by group grant reads the restricted forum, and so is
	// alerted there; the suspended moderator is not.
	closed, created := f.thread(staff)
	f.deliver(created, f.post(closed, "alice", "to the mods", 0, "gmod", "gone"))
	if n := f.count(`SELECT count(*) FROM notifications WHERE user_id = 51`); n != 1 {
		t.Errorf("the moderator by group grant has %d rows, want 1", n)
	}
	if n := f.count(`SELECT count(*) FROM notifications WHERE user_id = 52`); n != 0 {
		t.Errorf("the suspended moderator has %d rows", n)
	}
}

// TestGeneratorOpensLogsReadOnly: the generator's opener cannot write a
// log, which is what keeps it out of the frozen format (docs/03).
func TestGeneratorOpensLogsReadOnly(t *testing.T) {
	f := newFixture(t)
	id, _ := f.thread(f.forum)
	lf, err := f.g.open(id.LogPath(f.root))
	if err != nil {
		t.Fatal(err)
	}
	defer lf.Close()
	file, ok := lf.(*os.File)
	if !ok {
		t.Fatalf("the default opener returned %T", lf)
	}
	if _, err := file.WriteAt([]byte("x"), 0); err == nil {
		t.Fatal("a log opened by the generator accepted a write")
	}
}

// TestNewRequiresOptions: a generator missing what it needs to decide
// access, or to recover a drop, is refused at construction.
func TestNewRequiresOptions(t *testing.T) {
	f := newFixture(t)
	full := func() Options {
		return Options{DB: f.db, Root: f.root, Perm: f.perm,
			Project: func(context.Context, threadid.ID, int64) error { return nil },
			Sweep:   func(context.Context) error { return nil }}
	}
	if _, err := New(full()); err != nil {
		t.Fatal(err)
	}
	for name, drop := range map[string]func(*Options){
		"DB":      func(o *Options) { o.DB = nil },
		"Root":    func(o *Options) { o.Root = "" },
		"Perm":    func(o *Options) { o.Perm = nil },
		"Project": func(o *Options) { o.Project = nil },
		"Sweep":   func(o *Options) { o.Sweep = nil },
	} {
		o := full()
		drop(&o)
		if _, err := New(o); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("without %s: %v", name, err)
		}
	}
}

// TestLookupsSpanChunks: with the lookup bound at two ids, a batch that
// needs several posts' authors and several members' rows reads them over
// more than one statement and generates exactly what one statement would.
func TestLookupsSpanChunks(t *testing.T) {
	defer func(n int) { lookupChunk = n }(lookupChunk)
	lookupChunk = 2
	f := newFixture(t)
	id, created := f.thread(f.forum)
	f.deliver(created,
		f.post(id, "alice", "one", 0),
		f.post(id, "bob", "two", 0),
		f.post(id, "carol", "three", 0),
		f.post(id, "dave", "to alice", 1),
		f.post(id, "alice", "to bob", 2, "carol", "dave", "mod"),
		f.post(id, "mod", "to carol", 3))
	f.want("alice reply_to_you #4 by dave",
		"bob reply_to_you #5 by alice", "carol mention #5 by alice", "dave mention #5 by alice", "mod mention #5 by alice",
		"carol reply_to_you #6 by mod")
}
