package api

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/applications"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for thread rename and move (TKT-01M29CKRH9).

func threadPath(id threadid.ID) string { return "/api/v1/threads/" + id.String() }

// forumAt makes a forum with slug and visibility, archived or not, and
// returns its id.
func (w *world) forumAt(slug, visibility string, archived bool) int64 {
	w.t.Helper()
	var id int64
	w.write(func(q *dbq.Queries) error {
		var err error
		id, err = q.CreateForum(w.t.Context(), dbq.CreateForumParams{Slug: slug, Name: slug,
			Visibility: visibility, CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	if archived {
		if err := perm.New(w.db).SetForumArchived(w.t.Context(), audit.By{}, id, true); err != nil {
			w.t.Fatal(err)
		}
	}
	return id
}

// groupModerator makes a member, gina, who moderates exactly forums through
// the moderators group rather than by role, and returns her viewer.
func (w *world) groupModerator(forums ...int64) domain.Viewer {
	w.t.Helper()
	w.addUsers("active", "gina")
	var mods int64
	if err := w.db.Read(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		g, err := q.GetGroupBySlug(w.t.Context(), domain.GroupModerators)
		mods = g.ID
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	e := perm.New(w.db)
	if err := e.AddMember(w.t.Context(), audit.By{}, mods, w.users["gina"]); err != nil {
		w.t.Fatal(err)
	}
	for _, f := range forums {
		if err := e.Grant(w.t.Context(), audit.By{}, f, mods, domain.PermModerate); err != nil {
			w.t.Fatal(err)
		}
	}
	return w.viewer("gina")
}

// threadRow is the projection row of id.
func (w *world) threadRow(id threadid.ID) dbq.Thread {
	w.t.Helper()
	var row dbq.Thread
	if err := w.db.Read(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		row, err = q.GetThread(w.t.Context(), int64(id))
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return row
}

// forumCounts is forum_thread_counts by forum id.
func (w *world) forumCounts() map[int64][2]int64 {
	w.t.Helper()
	out := map[int64][2]int64{}
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(w.t.Context(), `SELECT forum_id, threads, live_posts FROM forum_thread_counts`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var f, n, p int64
			if err := rows.Scan(&f, &n, &p); err != nil {
				return err
			}
			out[f] = [2]int64{n, p}
		}
		return rows.Err()
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// reindex rebuilds the projection from the logs, as `ketju reindex` does.
func (w *world) reindex() {
	w.t.Helper()
	if _, err := w.proj.Reindex(w.t.Context(), nil); err != nil {
		w.t.Fatal(err)
	}
}

// unchanged runs do, wants the problem code want from it, and wants the log
// of id and audit_log left as they were.
func (w *world) unchanged(t *testing.T, id threadid.ID, name, want string, do func() string) {
	t.Helper()
	before, rows := w.logState(id), len(w.auditLog())
	if got := do(); got != want {
		t.Errorf("%s: %s, want %s", name, got, want)
	}
	if after := w.logState(id); after != before {
		t.Errorf("%s changed the log: %+v then %+v", name, before, after)
	}
	if got := len(w.auditLog()); got != rows {
		t.Errorf("%s wrote %d audit rows", name, got-rows)
	}
}

func rename(title string) string { return fmt.Sprintf(`{"title":%q}`, title) }
func moveTo(forum string) string { return fmt.Sprintf(`{"forum":%q}`, forum) }

// TestRenameWindow is the third criterion and half the seventh: the author
// renames inside threads.rename_window_seconds, which defaults to 86400; at
// its end and a second past it the author gets rename-window-closed and the
// log is unchanged; a moderator renames past it.
func TestRenameWindow(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	alice, bob := w.viewer("alice"), w.viewer("bob")
	h := header(t, w.sendAs(alice, http.MethodPatch, threadPath(id), rename("Why we left Slack, in short")))
	if h.Title != "Why we left Slack, in short" || h.Slug != "why-we-left-slack-in-short" {
		t.Fatalf("the author's rename answered %+v", h)
	}
	created := int64(h.CreatedAt)
	for _, at := range []int64{created + 86_400_000, created + 86_401_000} {
		w.clock = at
		w.unchanged(t, id, fmt.Sprintf("the author's rename %d ms after the start", at-created), "rename-window-closed",
			func() string {
				return problemCode(t, w.sendAs(alice, http.MethodPatch, threadPath(id), rename("Too late")))
			})
	}
	if h := header(t, w.sendAs(bob, http.MethodPatch, threadPath(id), rename("Renamed by a moderator"))); h.Slug != "renamed-by-a-moderator" {
		t.Fatalf("the moderator's rename past the window answered %+v", h)
	}

	// The window is the setting's: at 0 it is never open to the author.
	w2 := newWorld(t)
	id2, _ := w2.seed()
	w2.setting("threads.rename_window_seconds", 0)
	w2.unchanged(t, id2, "a rename with the window at 0", "rename-window-closed", func() string {
		return problemCode(t, w2.sendAs(w2.viewer("alice"), http.MethodPatch, threadPath(id2), rename("Never")))
	})
}

// TestRenameRefusals: every refusal of a rename, in the order docs/07
// gives, appends nothing and writes no audit row.
func TestRenameRefusals(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	h := w.chain()
	patch := func(who, body string) func() string {
		return func() string { return problemCode(t, send(h, who, http.MethodPatch, threadPath(id), body)) }
	}
	for _, c := range []struct{ name, who, body, want string }{
		{"anonymous", "", rename("x"), "unauthenticated"},
		{"a pre-user", "pre-user", rename("x"), "forbidden"},
		{"no title", "alice", `{}`, "bad-request"},
		{"no body", "alice", ``, "bad-request"},
		{"an unknown field", "alice", `{"title":"x","slug":"y"}`, "bad-request"},
		{"a member who is not the author", "carol", rename("Mine now"), "forbidden"},
		{"an empty title", "alice", rename("   "), "validation-failed"},
		{"a control character", "alice", rename("Topic\n"), "validation-failed"},
		{"a title over 200 characters", "alice", rename(strings.Repeat("é", 201)), "validation-failed"},
		{"the title it has", "alice", rename("Why we left Slack"), "conflict"},
		{"the title it has, padded", "bob", rename("  Why we left Slack "), "conflict"},
	} {
		w.unchanged(t, id, c.name, c.want, patch(c.who, c.body))
	}
	w.unchanged(t, id, "no such thread", "not-found", func() string {
		return problemCode(t, send(h, "alice", http.MethodPatch, threadPath(threadid.ID(12345)), rename("x")))
	})

	// A locked thread refuses its author, not a moderator.
	header(t, w.sendAs(w.viewer("bob"), http.MethodPost, modPath(id, "lock"), ``))
	w.unchanged(t, id, "the author in a locked thread", "conflict", patch("alice", rename("Locked in")))
	header(t, w.sendAs(w.viewer("bob"), http.MethodPatch, threadPath(id), rename("Locked, renamed")))
	header(t, w.sendAs(w.viewer("bob"), http.MethodPost, modPath(id, "unlock"), ``))

	// An archived forum refuses its author, as an edit of their own post
	// is refused, and not a moderator.
	w.archive()
	w.unchanged(t, id, "the author in an archived forum", "conflict", patch("alice", rename("Archived")))
	header(t, w.sendAs(w.viewer("bob"), http.MethodPatch, threadPath(id), rename("Archived, renamed")))

	// An archived thread refuses everyone.
	w.cache.MarkArchived(id)
	w.unchanged(t, id, "a moderator on an archived thread", "conflict", patch("bob", rename("No")))

	// The service asks the same of a caller that is not the API.
	w.unchanged(t, id, "a member through the service", "forbidden", func() string {
		_, err := w.svc.Rename(t.Context(), w.viewer("carol"), id, "x")
		return string(problem.FromError(err))
	})
}

// TestMoveNeedsModerateInBothForums is the second criterion, and completes
// the seventh of TKT-01M3N24RHD: a move is forbidden without moderate in
// the destination, not-found for a destination hidden from the mover, and
// conflict for an archived destination unless the mover is an admin, and a
// refusal appends nothing. A moderator moves a thread out of an archived
// forum. After a move into a restricted forum, a member without read there
// gets not-found at once.
func TestMoveNeedsModerateInBothForums(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	lounge := w.forumAt("lounge", domain.VisibilityPublic, false)
	w.forumAt("staff", domain.VisibilityRestricted, false)
	attic := w.forumAt("attic", domain.VisibilityPublic, true)
	den := w.forumAt("den", domain.VisibilityPublic, false)
	gina := w.groupModerator(w.forum, den)
	admin := w.newAdmin()
	h := w.chain()
	move := func(who, body string) func() string {
		return func() string { return problemCode(t, send(h, who, http.MethodPost, modPath(id, "move"), body)) }
	}

	for _, c := range []struct{ name, who, body, want string }{
		{"anonymous", "", moveTo("lounge"), "unauthenticated"},
		{"a member", "carol", moveTo("lounge"), "forbidden"},
		{"no forum", "bob", `{}`, "bad-request"},
		{"no body", "bob", ``, "bad-request"},
		{"an unknown field", "bob", `{"forum":"lounge","why":"x"}`, "bad-request"},
		{"a forum that does not exist", "bob", moveTo("nowhere"), "not-found"},
		{"a destination hidden from the mover", "bob", moveTo("staff"), "not-found"},
		{"an archived destination", "bob", moveTo("attic"), "conflict"},
		{"the forum it is in", "bob", moveTo("general"), "conflict"},
	} {
		w.unchanged(t, id, c.name, c.want, move(c.who, c.body))
	}
	// gina moderates general and den through a group. The moderates gate
	// admits her (TKT-01M3RQP0TG), and the service holds her to moderate in
	// the destination, so general to lounge is forbidden.
	w.unchanged(t, id, "gina through the gate", "forbidden", func() string {
		return problemCode(t, w.sendAs(gina, http.MethodPost, modPath(id, "move"), moveTo("lounge")))
	})
	// From general to den, both hers, she moves; from lounge, which is not
	// hers, to general, she does not.
	mine := w.threadIn(20, w.forum, "Gina moves this", "alice")
	if got := header(t, w.sendAs(gina, http.MethodPost, modPath(mine, "move"), moveTo("den"))); got.Forum.Slug != "den" {
		t.Fatalf("gina's move from general to den answered %+v", got)
	}
	notMine := w.threadIn(21, lounge, "In the lounge", "alice")
	w.unchanged(t, notMine, "gina from lounge to general", "forbidden", func() string {
		return problemCode(t, w.sendAs(gina, http.MethodPost, modPath(notMine, "move"), moveTo("general")))
	})
	w.unchanged(t, id, "without moderate in the destination", "forbidden", func() string {
		_, err := w.svc.Move(t.Context(), gina, id, "lounge")
		return string(problem.FromError(err))
	})
	w.unchanged(t, id, "a member through the service", "forbidden", func() string {
		_, err := w.svc.Move(t.Context(), w.viewer("carol"), id, "lounge")
		return string(problem.FromError(err))
	})

	// An admin moves it into the archived forum; a moderator moves it out.
	if got := header(t, w.sendAs(admin, http.MethodPost, modPath(id, "move"), moveTo("attic"))); got.Forum.Slug != "attic" {
		t.Fatalf("the admin's move answered %+v", got)
	}
	if row := w.threadRow(id); row.ForumID != attic {
		t.Fatalf("after the admin's move the row is in forum %d, want %d", row.ForumID, attic)
	}
	if got := header(t, w.sendAs(w.viewer("bob"), http.MethodPost, modPath(id, "move"), moveTo("lounge"))); got.Forum.Slug != "lounge" {
		t.Fatalf("the moderator's move out of the archived forum answered %+v", got)
	}
	if row := w.threadRow(id); row.ForumID != lounge {
		t.Fatalf("after the move out the row is in forum %d, want %d", row.ForumID, lounge)
	}
	// gina moderates general, not lounge, so the thread is no longer hers.
	w.unchanged(t, id, "gina once the thread left her forum", "forbidden", func() string {
		_, err := w.svc.Move(t.Context(), gina, id, "general")
		return string(problem.FromError(err))
	})

	// Into a restricted forum: gone from everyone without read, at once.
	header(t, w.sendAs(admin, http.MethodPost, modPath(id, "move"), moveTo("staff")))
	for _, who := range []string{"carol", "bob", "alice"} {
		if got := problemCode(t, w.get(w.viewer(who), threadPath(id))); got != "not-found" {
			t.Errorf("%s reads the thread moved into a restricted forum as %s, want not-found", who, got)
		}
	}
	if got := page(t, w.get(admin, threadPath(id))); got.Thread.Forum.Slug != "staff" {
		t.Errorf("the admin reads it in %q", got.Thread.Forum.Slug)
	}
	// And a move from there is not-found to a moderator who cannot see it.
	w.unchanged(t, id, "a moderator moving a thread they cannot see", "not-found", move("bob", moveTo("general")))
}

// TestConcurrentMovesAreDecidedOnTheLog: gina moderates general and den
// through a group; bob moderates everywhere by role. Each round bob moves a
// thread from general to lounge while gina moves it from general to den, at
// once. Whichever lands first, gina's record is never appended while the
// thread is in lounge, a forum she does not moderate: every thread.moved row
// of hers leaves a forum she moderates. Without the decision at a log
// length, gina's move could pass on general and land after bob's.
func TestConcurrentMovesAreDecidedOnTheLog(t *testing.T) {
	w := newWorld(t)
	w.forumAt("lounge", domain.VisibilityPublic, false)
	den := w.forumAt("den", domain.VisibilityPublic, false)
	gina := w.groupModerator(w.forum, den)
	bob := w.viewer("bob")
	// Without the decision at a log length, gina passing on a forum she
	// lost failed in 4 of 5 runs of 20 rounds; 50 makes a miss rare.
	const rounds = 50
	for round := range rounds {
		id := w.threadIn(uint64(100+round), w.forum, fmt.Sprintf("Round %d", round), "alice")
		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() { defer wg.Done(); _, errs[0] = w.svc.Move(t.Context(), bob, id, "lounge") }()
		go func() { defer wg.Done(); _, errs[1] = w.svc.Move(t.Context(), gina, id, "den") }()
		wg.Wait()
		if errs[0] != nil {
			t.Fatalf("round %d: bob's move: %v", round, errs[0])
		}
		if c := problem.FromError(errs[1]); errs[1] != nil && c != problem.CodeForbidden {
			t.Fatalf("round %d: gina's move: %v", round, errs[1])
		}
	}
	moved := 0
	for _, a := range w.auditLog() {
		if a.action != projection.ActionThreadMoved || a.actor.Int64 != w.users["gina"] {
			continue
		}
		moved++
		if from := int64(a.details["from_forum"].(float64)); from != w.forum {
			t.Errorf("gina's move of %s left forum %d, which she does not moderate", a.id, from)
		}
	}
	t.Logf("gina's move landed first in %d of %d rounds", moved, rounds)
}

// TestRenameJudgesAnUnprojectedMove (Terva review 1535, finding 1): alice's
// thread starts in a restricted forum she cannot read, and a move into
// general is in its log but not yet projected. Her rename is judged against
// the forum the log puts the thread in, so it succeeds, and the answer is in
// general. Judged from the row as it stood, it would be not-found.
func TestRenameJudgesAnUnprojectedMove(t *testing.T) {
	w := newWorld(t)
	staff := w.forumAt("staff", domain.VisibilityRestricted, false)
	id := w.threadIn(5, staff, "Started in staff", "alice")
	if got := problemCode(t, w.get(w.viewer("alice"), threadPath(id))); got != "not-found" {
		t.Fatalf("alice reads the thread in staff as %s; the test needs it hidden from her", got)
	}
	if err := w.cache.With(id, func(th *threadcache.Thread) error {
		_, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypeThreadMoved, Author: uint64(w.users["bob"]),
			TimeMS: w.clock, Body: &threadlog.ThreadMovedBody{ForumID: uint64(w.forum)}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if row := w.threadRow(id); row.ForumID != staff {
		t.Fatalf("the move was projected already: the row is in forum %d", row.ForumID)
	}
	h := header(t, w.sendAs(w.viewer("alice"), http.MethodPatch, threadPath(id), rename("Now in general")))
	if h.Forum.Slug != "general" || h.Title != "Now in general" {
		t.Fatalf("the rename answered %+v", h)
	}
}

// appendUnprojected appends a ThreadMoved to forum by bob to id's log and
// does not project it, and fails unless the row is still in from.
func (w *world) appendUnprojected(id threadid.ID, from, forum int64) {
	w.t.Helper()
	if err := w.cache.With(id, func(th *threadcache.Thread) error {
		_, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypeThreadMoved, Author: uint64(w.users["bob"]),
			TimeMS: w.clock, Body: &threadlog.ThreadMovedBody{ForumID: uint64(forum)}})
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	if row := w.threadRow(id); row.ForumID != from {
		w.t.Fatalf("the move was projected already: the row is in forum %d", row.ForumID)
	}
}

// TestThreadModerationJudgesAnUnprojectedMove is TKT-01M3RQP0TG's seventh
// criterion, first half: a thread in general, which gina moderates, has a
// move to lounge, which she does not, in its log and not projected. Each of
// her six actions is judged against the forum the log puts the thread in,
// so each is forbidden and writes nothing. Judged from the row as it stood,
// in general, each would have passed.
func TestThreadModerationJudgesAnUnprojectedMove(t *testing.T) {
	for _, a := range threadActions {
		t.Run(a.name, func(t *testing.T) {
			w, _, lounge, _, _ := forumModWorld(t)
			id := w.threadIn(60, w.forum, "About to move", "alice")
			bob := w.viewer("bob")
			switch a.action {
			case "unlock":
				if _, err := w.svc.Lock(t.Context(), bob, id, ""); err != nil {
					t.Fatal(err)
				}
			case "unpin":
				if _, err := w.svc.Pin(t.Context(), bob, id, threads.PinForum, nil); err != nil {
					t.Fatal(err)
				}
			}
			w.appendUnprojected(id, w.forum, lounge)
			// Deciding projects bob's move, which writes its own audit row,
			// so what must not appear is a row of gina's.
			before, slow := w.logState(id), w.slowModeRow(id)
			if got := problemCode(t, send(w.chain(), "gina", a.method, modPath(id, a.action), a.body)); got != "forbidden" {
				t.Errorf("gina's %s: %s, want forbidden", a.name, got)
			}
			if after := w.logState(id); after != before {
				t.Errorf("gina's %s changed the log: %+v then %+v", a.name, before, after)
			}
			if got := w.slowModeRow(id); got != slow {
				t.Errorf("gina's %s changed the slow mode row: %s then %s", a.name, slow, got)
			}
			for _, r := range w.auditLog() {
				if r.actor.Int64 == w.users["gina"] {
					t.Errorf("gina's %s wrote the audit row %+v", a.name, r)
				}
			}
			if row := w.threadRow(id); row.ForumID != lounge {
				t.Errorf("after deciding, the row is in forum %d, want lounge %d", row.ForumID, lounge)
			}
		})
	}
}

// logHeaders is the frame header of every record in id's log, in log order.
func (w *world) logHeaders(id threadid.ID) []threadlog.FrameHeader {
	w.t.Helper()
	f, err := os.ReadFile(id.LogPath(w.root))
	if err != nil {
		w.t.Fatal(err)
	}
	var out []threadlog.FrameHeader
	if err := threadlog.ScanFrames(bytes.NewReader(f), int64(len(f)), func(r threadlog.ScanRecord) bool {
		if r.Err != nil {
			w.t.Fatal(r.Err)
		}
		out = append(out, r.Header)
		return true
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// TestAMoveLandingMidDecisionIsJudged: a move to lounge lands after gina's
// request has projected the thread in general and admitted her, and before
// her change is written. The append's length precondition (lock, unlock,
// pin, unpin, move) or the length check in the slow mode transaction refuses
// the stale decision, and the request is decided again against lounge, so
// each is forbidden and writes nothing of hers. The move is landed from the
// service's Project hook, which every decision calls once it has read the
// length, so the timing is exact rather than raced.
//
// Each action is also asked when it would change nothing: locking a locked
// thread, the slow mode the thread has, a move to the forum it is in. That
// refusal is an answer too, and it is not given at a length the log has left,
// so it is forbidden rather than conflict (Terva review 1570, finding 1).
func TestAMoveLandingMidDecisionIsJudged(t *testing.T) {
	for _, a := range threadActions {
		for _, noop := range []bool{false, true} {
			name := a.name
			if noop {
				name += " that changes nothing"
			}
			t.Run(name, func(t *testing.T) { moveMidDecision(t, a, noop) })
		}
	}
}

// moveMidDecision is one case of TestAMoveLandingMidDecisionIsJudged: the
// thread is put in the state a needs, or, when noop, in the state in which a
// would change nothing.
func moveMidDecision(t *testing.T, a threadAction, noop bool) {
	w, _, lounge, _, _ := forumModWorld(t)
	id := w.threadIn(70, w.forum, "Moved mid-decision", "alice")
	bob := w.viewer("bob")
	var err error
	switch {
	case a.action == "unlock" && !noop, a.action == "lock" && noop:
		_, err = w.svc.Lock(t.Context(), bob, id, "")
	case a.action == "unpin" && !noop, a.action == "pin" && noop:
		_, err = w.svc.Pin(t.Context(), bob, id, threads.PinForum, nil)
	case a.action == "slow-mode" && noop:
		_, err = w.svc.SetSlowMode(t.Context(), bob, "", id, 30)
	case a.action == "move" && noop:
		a.body = moveTo("general")
	}
	if err != nil {
		t.Fatal(err)
	}
	if noop {
		// Without the move, bob's answer is conflict: the case is one in
		// which the action would change nothing.
		if got := problemCode(t, send(w.chain(), "bob", a.method, modPath(id, a.action), a.body)); got != "conflict" {
			t.Fatalf("bob's %s answered %s; the case needs conflict", a.name, got)
		}
	}
	project := w.svc.Project
	var once sync.Once
	w.svc.Project = func(ctx context.Context, tid threadid.ID, n int64) error {
		err := project(ctx, tid, n)
		if tid == id {
			once.Do(func() {
				if err := w.cache.With(id, func(th *threadcache.Thread) error {
					_, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypeThreadMoved,
						Author: uint64(w.users["bob"]), TimeMS: w.clock,
						Body: &threadlog.ThreadMovedBody{ForumID: uint64(lounge)}})
					return err
				}); err != nil {
					t.Error(err)
				}
			})
		}
		return err
	}
	before, slow := w.logState(id), w.slowModeRow(id)
	if got := problemCode(t, send(w.chain(), "gina", a.method, modPath(id, a.action), a.body)); got != "forbidden" {
		t.Errorf("gina's %s: %s, want forbidden", a.name, got)
	}
	w.svc.Project = project
	// The only record added is bob's move.
	hs := w.logHeaders(id)
	if last := hs[len(hs)-1]; last.Type != threadlog.TypeThreadMoved {
		t.Errorf("the last record is %v, want bob's move", last.Type)
	}
	if after := w.logState(id); after.posts != before.posts || after.size <= before.size {
		t.Errorf("the log went from %+v to %+v", before, after)
	}
	for _, hd := range hs {
		if hd.Actor == uint64(w.users["gina"]) {
			t.Errorf("gina's %s appended a %v", a.name, hd.Type)
		}
	}
	if got := w.slowModeRow(id); got != slow {
		t.Errorf("gina's %s changed the slow mode row: %s then %s", a.name, slow, got)
	}
	for _, r := range w.auditLog() {
		if r.actor.Int64 == w.users["gina"] {
			t.Errorf("gina's %s wrote the audit row %+v", a.name, r)
		}
	}
}

// TestConcurrentLocksAreDecidedOnTheLog is TKT-01M3RQP0TG's seventh
// criterion, second half: each round bob moves a thread from general to
// lounge while gina, who moderates general and not lounge, locks it, at
// once. Whichever lands first, no ThreadLocked of hers follows the
// ThreadMoved in the log. Before lock decided at a log length, her lock
// could pass on general and land after bob's move.
func TestConcurrentLocksAreDecidedOnTheLog(t *testing.T) {
	w, gina, _, _, _ := forumModWorld(t)
	bob := w.viewer("bob")
	const rounds = 50
	ids := make([]threadid.ID, rounds)
	for round := range rounds {
		id := w.threadIn(uint64(100+round), w.forum, fmt.Sprintf("Round %d", round), "alice")
		ids[round] = id
		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() { defer wg.Done(); _, errs[0] = w.svc.Move(t.Context(), bob, id, "lounge") }()
		go func() { defer wg.Done(); _, errs[1] = w.svc.Lock(t.Context(), gina, id, "") }()
		wg.Wait()
		if errs[0] != nil {
			t.Fatalf("round %d: bob's move: %v", round, errs[0])
		}
		if c := problem.FromError(errs[1]); errs[1] != nil && c != problem.CodeForbidden {
			t.Fatalf("round %d: gina's lock: %v", round, errs[1])
		}
	}
	locked := 0
	for round, id := range ids {
		moved := false
		for _, hd := range w.logHeaders(id) {
			switch {
			case hd.Type == threadlog.TypeThreadMoved:
				moved = true
			case hd.Type == threadlog.TypeThreadLocked && hd.Actor == uint64(w.users["gina"]):
				locked++
				if moved {
					t.Errorf("round %d: gina's lock of %s landed after the move to lounge", round, id)
				}
			}
		}
		if !moved {
			t.Errorf("round %d: %s has no move in its log", round, id)
		}
	}
	t.Logf("gina's lock landed first in %d of %d rounds", locked, rounds)
}

// TestRenameWindowTooLargeForADuration (Terva review 1535, finding 2): a
// count of seconds past what a time.Duration holds is the longest window,
// not a negative one that is closed at once.
func TestRenameWindowTooLargeForADuration(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.setting("threads.rename_window_seconds", 10_000_000_000)
	header(t, w.sendAs(w.viewer("alice"), http.MethodPatch, threadPath(id), rename("Still mine to rename")))
}

// TestRenameAndMoveSurviveReindex is the fourth criterion: after a reindex
// the thread's title, slug and forum are its last rename's and move's, and
// forum_thread_counts has followed the move.
func TestRenameAndMoveSurviveReindex(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	lounge := w.forumAt("lounge", domain.VisibilityPublic, false)
	bob := w.viewer("bob")
	before := w.forumCounts()
	header(t, w.sendAs(bob, http.MethodPatch, threadPath(id), rename("First rename")))
	header(t, w.sendAs(bob, http.MethodPost, modPath(id, "move"), moveTo("lounge")))
	last := header(t, w.sendAs(bob, http.MethodPatch, threadPath(id), rename("Second rename")))
	check := func(when string) {
		t.Helper()
		row := w.threadRow(id)
		if row.Title != "Second rename" || row.Slug != "second-rename" || row.ForumID != lounge {
			t.Fatalf("%s: the row is %q, %q, forum %d", when, row.Title, row.Slug, row.ForumID)
		}
		counts := w.forumCounts()
		if counts[w.forum][0] != before[w.forum][0]-1 || counts[lounge] != before[w.forum] {
			t.Fatalf("%s: forum_thread_counts %v, from %v: want general's one thread and its posts in lounge", when, counts, before)
		}
	}
	check("after the moves")
	w.reindex()
	check("after a reindex")
	if got := page(t, w.get(bob, threadPath(id))).Thread; !reflect.DeepEqual(got, last) {
		t.Fatalf("after a reindex the header is %+v, the last answer %+v", got, last)
	}
}

// TestRenameAuditCarriesNoTitle is the eighth criterion: a moderator's
// rename writes one thread.renamed row whose details are exactly {thread,
// log_offset}, and neither title appears anywhere in it; every move writes
// one thread.moved row with the actor and {from_forum, to_forum}.
func TestRenameAuditCarriesNoTitle(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	lounge := w.forumAt("lounge", domain.VisibilityPublic, false)
	bob := w.viewer("bob")
	const newTitle = "A title nobody should find in audit_log"

	n0 := len(w.auditLog()) // the seed's pin

	offset := w.logState(id).size
	header(t, w.sendAs(bob, http.MethodPatch, threadPath(id), rename(newTitle)))
	log := w.auditLog()
	if len(log) != n0+1 {
		t.Fatalf("the rename wrote %d audit rows, want 1: %+v", len(log)-n0, log)
	}
	got := log[n0]
	want := map[string]any{"thread": id.String(), "log_offset": float64(offset)}
	if got.action != projection.ActionThreadRenamed || got.actor.Int64 != w.users["bob"] || got.typ != "thread" ||
		got.id != id.String() || fmt.Sprint(got.details) != fmt.Sprint(want) {
		t.Errorf("the rename's row is %+v, want thread.renamed by bob with %v", got, want)
	}
	// Every column of the row, as text: no title, no slug, no word of one.
	var raw string
	if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT id || '|' || at || '|' || coalesce(actor_id, '') || '|' ||
			action || '|' || target_type || '|' || target_id || '|' || details_json || '|' || coalesce(ip, '')
			FROM audit_log WHERE action = ?`, projection.ActionThreadRenamed).Scan(&raw)
	}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{newTitle, "a-title-nobody", "Why we left Slack", "why-we-left-slack", "title", "slug", "nobody"} {
		if strings.Contains(raw, s) {
			t.Errorf("the rename's row holds %q: %s", s, raw)
		}
	}

	offset = w.logState(id).size
	header(t, w.sendAs(bob, http.MethodPost, modPath(id, "move"), moveTo("lounge")))
	log = w.auditLog()
	if len(log) != n0+2 {
		t.Fatalf("the move wrote %d audit rows, want 1", len(log)-n0-1)
	}
	got = log[n0+1]
	want = map[string]any{"thread": id.String(), "log_offset": float64(offset),
		"from_forum": float64(w.forum), "to_forum": float64(lounge)}
	if got.action != projection.ActionThreadMoved || got.actor.Int64 != w.users["bob"] || got.typ != "thread" ||
		got.id != id.String() || fmt.Sprint(got.details) != fmt.Sprint(want) {
		t.Errorf("the move's row is %+v, want thread.moved by bob with %v", got, want)
	}

	// A reindex writes neither again.
	w.reindex()
	if n := len(w.auditLog()); n != n0+2 {
		t.Errorf("after a reindex there are %d audit rows, want %d", n, n0+2)
	}
}

// TestAuthorRenameIsNotAudited is the rest of the seventh criterion: an
// author's rename of their own thread writes no audit row, then or on a
// reindex. A moderator renaming their own thread is its author too.
func TestAuthorRenameIsNotAudited(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	own := w.threadIn(2, w.forum, "Bob's thread", "bob")
	before := w.auditLog() // the seed's pin
	header(t, w.sendAs(w.viewer("alice"), http.MethodPatch, threadPath(id), rename("Alice's own words")))
	header(t, w.sendAs(w.viewer("bob"), http.MethodPatch, threadPath(own), rename("Bob's thread, renamed")))
	if log := w.auditLog(); !reflect.DeepEqual(log, before) {
		t.Fatalf("an author's rename wrote %+v", log[len(before):])
	}
	w.reindex()
	if log := w.auditLog(); !reflect.DeepEqual(log, before) {
		t.Fatalf("after a reindex an author's rename wrote %+v", log[len(before):])
	}
}

// TestRenameMovesTheResponseCacheKey is the fifth criterion and the
// response cache invariant: a page cached before a rename or a move is not
// served after it. Each class's next page is a fresh draw that equals one
// with no cache, and carries the new title, slug and forum. The same holds
// for the first criterion's server half: after a rename the thread is
// served under the new slug whatever the request says, which is what the
// client's canonicalThreadUrl rewrites the address from.
func TestRenameMovesTheResponseCacheKey(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.forumAt("lounge", domain.VisibilityPublic, false)
	classes := []string{"", "carol", "bob"}
	warm := func() {
		for _, who := range classes {
			w.same(w.viewer(who), threadPath(id))
		}
	}
	warm()
	warm()
	for _, step := range []struct {
		name  string
		do    func() dto.ThreadHeader
		check func(dto.ThreadHeader) bool
	}{
		{"a rename", func() dto.ThreadHeader {
			return header(t, w.sendAs(w.viewer("bob"), http.MethodPatch, threadPath(id), rename("Why we stayed on Slack")))
		}, func(h dto.ThreadHeader) bool {
			return h.Title == "Why we stayed on Slack" && h.Slug == "why-we-stayed-on-slack"
		}},
		{"a move", func() dto.ThreadHeader {
			return header(t, w.sendAs(w.viewer("bob"), http.MethodPost, modPath(id, "move"), moveTo("lounge")))
		}, func(h dto.ThreadHeader) bool { return h.Forum.Slug == "lounge" }},
	} {
		before := w.svc.Responses.Stats()
		answer := step.do()
		for _, who := range classes {
			for _, path := range []string{threadPath(id), threadPath(id) + "?ordinal=4"} {
				p := page(t, w.get(w.viewer(who), path))
				if !step.check(p.Thread) || !reflect.DeepEqual(p.Thread, answer) {
					t.Errorf("after %s %q reads %s with header %+v; the answer was %+v", step.name, who, path, p.Thread, answer)
				}
			}
		}
		if s := w.svc.Responses.Stats(); s.Inserts-before.Inserts != 3 || s.Hits-before.Hits != 3 {
			t.Errorf("after %s: stats went from %+v to %+v; want a miss for each class, then a hit", step.name, before, s)
		}
		warm()
	}
}

// The ThreadRenamed and ThreadMoved records are the catalogue's, and this
// ticket writes no ThreadTagged and serves no tag route (the sixth
// criterion): TestRenameAndMoveWriteNoTags reads every record type in a
// log that was renamed and moved through the API.
func TestRenameAndMoveWriteNoTags(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.forumAt("lounge", domain.VisibilityPublic, false)
	bob := w.viewer("bob")
	before := w.recordTypes(id)
	header(t, w.sendAs(bob, http.MethodPatch, threadPath(id), rename("Renamed once")))
	header(t, w.sendAs(bob, http.MethodPost, modPath(id, "move"), moveTo("lounge")))
	after := w.recordTypes(id)
	if got := after[len(before):]; fmt.Sprint(got) != fmt.Sprint([]threadlog.RecordType{threadlog.TypeThreadRenamed, threadlog.TypeThreadMoved}) {
		t.Fatalf("rename and move appended %v", got)
	}
	for _, rt := range Routes(Deps{Threads: w.svc, Users: &users.Service{DB: w.db, Perm: perm.New(w.db)},
		Forums: &forums.Service{DB: w.db, Perm: perm.New(w.db)}, Forms: &formsvc.Service{DB: w.db},
		Applications: &applications.Service{DB: w.db}, Perm: perm.New(w.db)}) {
		if strings.Contains(rt.Path, "tag") {
			t.Errorf("a tag route is served: %s %s", rt.Method, rt.Path)
		}
	}
}

// recordTypes is every record type in id's log, in log order.
func (w *world) recordTypes(id threadid.ID) []threadlog.RecordType {
	w.t.Helper()
	f, err := os.ReadFile(id.LogPath(w.root))
	if err != nil {
		w.t.Fatal(err)
	}
	var out []threadlog.RecordType
	if err := threadlog.ScanFrames(bytes.NewReader(f), int64(len(f)), func(r threadlog.ScanRecord) bool {
		if r.Err != nil {
			w.t.Fatal(r.Err)
		}
		out = append(out, r.Header.Type)
		return true
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}
