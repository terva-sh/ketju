package sse

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/agentcred"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
	"github.com/terva-sh/ketju/internal/web"
)

// TestEventPayloadIsThreadAndLogLength: every thread event's data is exactly
// {thread_id, log_length}, decoded strictly; the only event names are ready
// and thread; ready's data is empty; and no id field is ever sent, so a
// browser's Last-Event-ID carries nothing back.
func TestEventPayloadIsThreadAndLogLength(t *testing.T) {
	w := newWorld(t, defaultOpts())
	a := w.thread("general", "alice")
	b := w.thread("general", "carol")
	c := w.open(eventsPath(ids(a, b)), w.signIn("dave")...)
	if got := c.resp.Header.Get("Content-Type"); got != "text/event-stream; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := c.resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	snap := c.ready(2)
	if snap[0].ThreadID != a.String() || snap[1].ThreadID != b.String() {
		t.Errorf("the snapshot names %v, want %s then %s", snap, a, b)
	}
	n := w.reply(a, "carol")
	if h := c.hint(); h.ThreadID != a.String() || h.LogLength != n {
		t.Errorf("hint = %+v, want %s at %d", h, a, n)
	}
	n = w.reply(b, "alice")
	if h := c.hint(); h.ThreadID != b.String() || h.LogLength != n {
		t.Errorf("hint = %+v, want %s at %d", h, b, n)
	}
	c.close()

	c.mu.Lock()
	lines := append([]string(nil), c.lines...)
	c.mu.Unlock()
	var name string
	for _, line := range lines {
		field, value, _ := strings.Cut(line, ": ")
		switch field {
		case "", "retry":
		case "event":
			if value != "ready" && value != "thread" {
				t.Errorf("an event named %q", value)
			}
			name = value
		case "data":
			if name == "ready" {
				if value != "{}" {
					t.Errorf("ready carries %q", value)
				}
				continue
			}
			decodeHint(t, value)
		default:
			if !strings.HasPrefix(line, ":") {
				t.Errorf("a line with field %q: %q", field, line)
			}
		}
	}
}

// TestSubscribeAnswersAsThePage: a subscription is refused exactly as the
// thread page or the thread list refuses the viewer, and a malformed one is
// bad-request.
func TestSubscribeAnswersAsThePage(t *testing.T) {
	w := newWorld(t, defaultOpts())
	general := w.thread("general", "alice")
	club := w.thread("club", "alice")
	staff := w.thread("staff", "dave")
	// erin's members group gets a row for club that leaves out read, so the
	// forum stays visible to her but unreadable: forbidden, not not-found.
	if err := w.perm.Grant(t.Context(), w.by(), w.forums["club"], 2, domain.PermReply); err != nil {
		t.Fatal(err)
	}
	unknown := threadid.ID(uint64(1_757_000_000_000)<<16 | 7)

	alice := w.signIn("alice")
	for _, c := range []struct {
		name   string
		path   string
		page   string // the page the answer must match; empty means none
		header []string
		want   int
	}{
		{"an unknown thread", eventsPath(ids(unknown)), "/api/v1/threads/" + unknown.String(), alice, 404},
		{"a thread in a hidden forum", eventsPath(ids(staff)), "/api/v1/threads/" + staff.String(), alice, 404},
		{"members only, anonymous", eventsPath(ids(club)), "/api/v1/threads/" + club.String(), nil, 401},
		{"a member without read", eventsPath(ids(club)), "/api/v1/threads/" + club.String(), alice, 403},
		{"one hidden thread among readable ones", eventsPath(ids(general, staff)), "", alice, 404},
		{"a hidden forum", eventsPath(nil, "staff"), "/api/v1/forums/staff/threads", alice, 404},
		{"a members forum, anonymous", eventsPath(nil, "club"), "/api/v1/forums/club/threads", nil, 401},
		{"no such forum", eventsPath(nil, "nowhere"), "/api/v1/forums/nowhere/threads", alice, 404},
		{"nothing named", "/events", "", nil, 400},
		{"an empty list", "/events?threads=", "", nil, 400},
		{"a malformed id", "/events?threads=not-an-id", "", nil, 400},
		{"an empty element", "/events?threads=" + general.String() + ",", "", nil, 400},
		{"an unknown parameter", eventsPath(ids(general)) + "&lens=all", "", nil, 400},
		{"a parameter twice", eventsPath(ids(general)) + "&threads=" + general.String(), "", nil, 400},
		{"too many threads", "/events?threads=" + strings.Repeat(general.String()+",", MaxThreads) + general.String() + "x", "", nil, 400},
	} {
		t.Run(c.name, func(t *testing.T) {
			resp, body := w.get(c.path, c.header...)
			if resp.StatusCode != c.want {
				t.Errorf("GET %s = %d, want %d: %s", c.path, resp.StatusCode, c.want, body)
			}
			if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/problem+json") {
				t.Errorf("the refusal is %q, not a problem", resp.Header.Get("Content-Type"))
			}
			if c.page != "" {
				if page, _ := w.get(c.page, c.header...); page.StatusCode != resp.StatusCode {
					t.Errorf("the page answers %d and the stream %d", page.StatusCode, resp.StatusCode)
				}
			}
		})
	}
	// The members forum the page admits a member to, the stream does too,
	// and a moderator by group grant reads the restricted forum.
	w.open(eventsPath(ids(general), "general")).ready(1)
	dave := w.open(eventsPath(ids(staff), "staff"), w.signIn("dave")...)
	dave.ready(1)
}

// TestPreUserCannotSubscribe: an approval-mode applicant's session is refused
// at the pre-user gate, before the stream admits anything.
func TestPreUserCannotSubscribe(t *testing.T) {
	w := newWorld(t, defaultOpts())
	id := w.thread("general", "alice")
	var identity int64
	w.write(func(q *dbq.Queries) error {
		var err error
		identity, err = q.CreateIdentity(t.Context(), dbq.CreateIdentityParams{ProviderKey: "github",
			Subject: "applicant-1", Issuer: "https://github.com", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	tok := sessiontest.PreUser(t, w.db, identity, w.clock.now())
	// The gate answers a GET outside the API as it answers any page an
	// applicant asks for: a redirect to the status page, which an
	// EventSource follows to HTML and gives up on.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, w.srv.URL+eventsPath(ids(id)), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", session.CookieName+"="+tok)
	hc := *w.srv.Client()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != web.ApplyStatusPath ||
		strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Errorf("a pre-user's subscription = %d to %q, %q; want the gate's 303 to the status page",
			resp.StatusCode, resp.Header.Get("Location"), resp.Header.Get("Content-Type"))
	}
	if n := w.hub.Stats().Open; n != 0 {
		t.Errorf("%d streams open after the refusal", n)
	}
	// The handler refuses too, should the gate ever let one through.
	rec := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, eventsPath(ids(id)), nil)
	req = req.WithContext(domain.WithViewer(req.Context(), domain.Viewer{IdentityID: identity, PreUser: true}))
	w.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("the handler answered a pre-user %d, want 403", rec.Code)
	}
}

// TestSubscribeRefusesCrossSite: a stream another site starts is refused; a
// same-origin one, and one from a client that says nothing, are admitted.
func TestSubscribeRefusesCrossSite(t *testing.T) {
	w := newWorld(t, defaultOpts())
	id := w.thread("general", "alice")
	resp, _ := w.get(eventsPath(ids(id)), append(w.signIn("alice"), "Sec-Fetch-Site", "cross-site")...)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a cross-site subscription = %d, want 403", resp.StatusCode)
	}
	w.open(eventsPath(ids(id)), "Sec-Fetch-Site", "same-origin").ready(1)
	w.open(eventsPath(ids(id))).ready(1)
}

// TestEventCarriesNoMoreThanThePage: whatever happens to a post, a member's
// stream carries the thread and the log length, and that length is what
// the member's next thread page serves.
func TestEventCarriesNoMoreThanThePage(t *testing.T) {
	cases := []struct {
		name string
		// change makes the change and returns the length it left.
		change func(w *world, id threadid.ID) int64
	}{
		{"an author's delete", func(w *world, id threadid.ID) int64 {
			w.reply(id, "carol")
			w.settleHintless(id)
			res, err := w.svc.Delete(w.t.Context(), w.viewer("carol"), id, 2, "")
			if err != nil {
				w.t.Fatal(err)
			}
			return res.LogLength
		}},
		{"a moderator's hide", func(w *world, id threadid.ID) int64 {
			w.reply(id, "carol")
			w.settleHintless(id)
			res, err := w.svc.Delete(w.t.Context(), w.viewer("bob"), id, 2, "off topic")
			if err != nil {
				w.t.Fatal(err)
			}
			return res.LogLength
		}},
		{"hidden pending review by flags", func(w *world, id threadid.ID) int64 {
			w.reply(id, "carol")
			w.settleHintless(id)
			for _, who := range []string{"dave", "erin", "frank"} {
				if _, err := w.svc.Flag(w.t.Context(), w.viewer(who), id, threads.FlagRequest{Ordinal: 2, Reason: threads.FlagSpam}); err != nil {
					w.t.Fatal(err)
				}
			}
			return w.length(id)
		}},
		{"a hard redaction's marker", func(w *world, id threadid.ID) int64 {
			// ketju redact runs with the server stopped, so no stream sees
			// its records: the reconnect after the restart reads them in
			// its snapshot.
			w.reply(id, "carol")
			w.settleHintless(id)
			w.streamOf.close()
			n := w.redactOffline(id, 2)
			s := w.open(eventsPath(ids(id)), w.streamOf.header...)
			if ev, ok := s.next(5 * time.Second); !ok || ev.name != "ready" {
				w.t.Fatalf("the reconnect did not open with ready: %+v", ev)
			}
			w.streamOf = s
			return n
		}},
		{"a suspended author's visible post", func(w *world, id threadid.ID) int64 {
			w.reply(id, "carol")
			w.settleHintless(id)
			w.suspend("carol")
			// The post is still shown until the bulk hide; another member's
			// reply is the event.
			return w.reply(id, "erin")
		}},
		{"a suspended author's posts after the bulk hide", func(w *world, id threadid.ID) int64 {
			w.reply(id, "carol")
			w.reply(id, "carol")
			w.settleHintless(id)
			audit := w.suspend("carol")
			if _, err := w.svc.HidePostsOf(w.t.Context(), w.viewer("bob"), w.users["carol"], audit); err != nil {
				w.t.Fatal(err)
			}
			return w.length(id)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, defaultOpts())
			id := w.thread("general", "alice")
			alice := w.signIn("alice")
			s := w.open(eventsPath(ids(id)), alice...)
			s.ready(1)
			w.streamOf = s
			want := c.change(w, id)
			// Read hints until one reaches the change's length; each must
			// decode strictly and name the thread.
			var last int64
			for last < want {
				h := w.streamOf.hint()
				if h.ThreadID != id.String() {
					t.Fatalf("a hint for %s on a stream for %s", h.ThreadID, id)
				}
				last = h.LogLength
			}
			if last != want {
				t.Errorf("the last hint is at %d, want %d", last, want)
			}
			if page := w.pageLength(id, alice...); page != last {
				t.Errorf("the hint says %d and alice's next page serves %d", last, page)
			}
		})
	}
}

// settleHintless waits for the projection and drains the hints the setup
// produced from the stream under test, if one is open.
func (w *world) settleHintless(id threadid.ID) {
	w.t.Helper()
	w.settle(id)
	if w.streamOf == nil {
		return
	}
	n := w.length(id)
	for {
		if h := w.streamOf.hint(); h.LogLength >= n {
			return
		}
	}
}

// appendRecord appends a record through the thread's actor and publishes it,
// as posts.Writer does, and returns the length it left.
func (w *world) appendRecord(id threadid.ID, a threadlog.Append) int64 {
	w.t.Helper()
	w.clock.add(time.Second)
	a.TimeMS = w.clock.now().UnixMilli()
	var res threadlog.Result
	if err := w.cache.With(id, func(th *threadcache.Thread) error {
		var err error
		res, err = w.svc.Writer.Append(th, a)
		return err
	}); err != nil {
		w.t.Fatalf("appending %s: %v", a.Type, err)
	}
	return res.LogLength
}

// suspend suspends handle through perm, which ends their sessions and
// credentials, and returns the suspension's audit id.
func (w *world) suspend(handle string) int64 {
	w.t.Helper()
	id, err := w.perm.ChangeStatusID(w.t.Context(), w.users[handle], perm.StatusChange{By: w.by(),
		Action: perm.ActionSuspended, Reason: "a test"})
	if err != nil {
		w.t.Fatal(err)
	}
	return id
}

// TestGroupGrantModeratorKeepsStream: a member who moderates a restricted
// forum by a group grant (docs/08, "Decided 2026-09-30") keeps their stream
// through the hides and restores they and others make there.
func TestGroupGrantModeratorKeepsStream(t *testing.T) {
	w := newWorld(t, defaultOpts())
	id := w.thread("staff", "dave")
	w.reply(id, "root")
	w.settle(id)
	dave := w.signIn("dave")
	s := w.open(eventsPath(ids(id), "staff"), dave...)
	s.ready(1)
	for _, step := range []func() int64{
		func() int64 { // dave hides root's post
			res, err := w.svc.Delete(t.Context(), w.viewer("dave"), id, 2, "off topic")
			if err != nil {
				t.Fatal(err)
			}
			return res.LogLength
		},
		func() int64 { // and restores it
			res, err := w.svc.Restore(t.Context(), w.viewer("dave"), id, 2, "on reflection")
			if err != nil {
				t.Fatal(err)
			}
			return res.LogLength
		},
		func() int64 { // root hides it again
			res, err := w.svc.Delete(t.Context(), w.viewer("root"), id, 2, "again")
			if err != nil {
				t.Fatal(err)
			}
			return res.LogLength
		},
		func() int64 { return w.reply(id, "root") },
	} {
		want := step()
		if h := s.hint(); h.LogLength != want {
			t.Fatalf("hint at %d, want %d", h.LogLength, want)
		}
	}
	if page := w.pageLength(id, dave...); page != w.length(id) {
		t.Errorf("dave's page serves %d, the log is %d", page, w.length(id))
	}
}

// TestForumSubscription: a forum stream hears of every thread in the forum,
// and stops hearing of one once it moves out. A thread the viewer cannot
// read is never hinted, though it is in a forum the stream names.
func TestForumSubscription(t *testing.T) {
	w := newWorld(t, defaultOpts())
	a := w.thread("general", "alice")
	b := w.thread("general", "carol")
	alice := w.signIn("alice")
	s := w.open(eventsPath(nil, "general"), alice...)
	s.ready(0)
	if n := w.reply(a, "carol"); s.hintAt(a, n) != hintOf(a, n) {
		t.Error("no hint for the first thread")
	}
	if n := w.reply(b, "alice"); s.hintAt(b, n) != hintOf(b, n) {
		t.Error("no hint for the second thread")
	}
	if _, err := w.svc.Move(t.Context(), w.viewer("root"), b, "club"); err != nil {
		t.Fatal(err)
	}
	w.settle(b)
	w.reply(b, "alice")
	n := w.reply(a, "carol")
	if h := s.hintAt(a, n); h != hintOf(a, n) {
		t.Errorf("hint = %+v, want %+v; a thread that moved out was still hinted, or the forum's was lost", h, hintOf(a, n))
	}

	// A thread in a forum alice cannot read sends nothing to her stream on
	// general, though a stream on that forum hears of it.
	staff := w.thread("staff", "dave")
	dave := w.open(eventsPath(nil, "staff"), w.signIn("dave")...)
	dave.ready(0)
	if n := w.reply(staff, "dave"); dave.hintAt(staff, n) != hintOf(staff, n) {
		t.Errorf("the staff stream did not hear of its thread at %d", n)
	}
	s.quiet(300 * time.Millisecond)
}

// The TestStreamCloses* tests cover the ways a stream must end without
// writing the next hint. Each appends after the change, and the stream
// must end without the hint for it.

// TestStreamClosesWhenSessionRevoked: signing out this device, or every
// device, ends the stream before the next hint.
func TestStreamClosesWhenSessionRevoked(t *testing.T) {
	for _, everywhere := range []bool{false, true} {
		w := newWorld(t, defaultOpts())
		id := w.thread("general", "alice")
		tok := sessiontest.SignIn(t, w.db, w.users["alice"], w.clock.now())
		s := w.open(eventsPath(ids(id)), "Cookie", session.CookieName+"="+tok)
		s.ready(1)
		if everywhere {
			if err := w.sessions.RevokeUser(t.Context(), w.users["alice"]); err != nil {
				t.Fatal(err)
			}
		} else if err := w.sessions.Revoke(t.Context(), tok); err != nil {
			t.Fatal(err)
		}
		w.reply(id, "carol")
		s.ended(5 * time.Second)
	}
}

// TestStreamClosesWhenSessionExpires: a session gone idle past IdleExpiry
// ends the stream before the next hint.
func TestStreamClosesWhenSessionExpires(t *testing.T) {
	w := newWorld(t, defaultOpts())
	id := w.thread("general", "alice")
	s := w.open(eventsPath(ids(id)), w.signIn("alice")...)
	s.ready(1)
	w.clock.add(session.IdleExpiry + time.Minute)
	w.reply(id, "carol")
	s.ended(5 * time.Second)
}

// TestStreamClosesAtHeartbeatAfterSignOut: with nothing appended at all, a
// signed-out stream ends at its next heartbeat.
func TestStreamClosesAtHeartbeatAfterSignOut(t *testing.T) {
	o := defaultOpts()
	o.heartbeat = 200 * time.Millisecond
	w := newWorld(t, o)
	id := w.thread("general", "alice")
	tok := sessiontest.SignIn(t, w.db, w.users["alice"], w.clock.now())
	s := w.open(eventsPath(ids(id)), "Cookie", session.CookieName+"="+tok)
	s.ready(1)
	if ev, ok := s.next(2 * time.Second); !ok || ev.comment != "hb" {
		t.Fatalf("no heartbeat before the sign-out: %+v, %v", ev, ok)
	}
	if err := w.sessions.Revoke(t.Context(), tok); err != nil {
		t.Fatal(err)
	}
	// Nothing is appended: the heartbeat alone must notice.
	s.ended(2 * time.Second)
}

// TestStreamClosesWhenUserSuspended: a suspension ends the stream, and so
// does a status change that left the session row in place.
func TestStreamClosesWhenUserSuspended(t *testing.T) {
	w := newWorld(t, defaultOpts())
	id := w.thread("general", "carol")
	s := w.open(eventsPath(ids(id)), w.signIn("alice")...)
	s.ready(1)
	w.suspend("alice")
	w.reply(id, "carol")
	s.ended(5 * time.Second)

	// A status change that left the session row in place ends it too:
	// the stream was opened by a member and is not held as anonymous.
	s = w.open(eventsPath(ids(id)), w.signIn("erin")...)
	s.ready(1)
	w.write(func(q *dbq.Queries) error {
		return q.SetUserStatus(t.Context(), dbq.SetUserStatusParams{Status: domain.StatusSuspended,
			StatusReason: "row only", UpdatedAt: 2, ID: w.users["erin"]})
	})
	w.reply(id, "carol")
	s.ended(5 * time.Second)
}

// TestStreamClosesWhenUserDeactivated: a member deactivating their account
// ends their stream.
func TestStreamClosesWhenUserDeactivated(t *testing.T) {
	w := newWorld(t, defaultOpts())
	id := w.thread("general", "carol")
	s := w.open(eventsPath(ids(id)), w.signIn("alice")...)
	s.ready(1)
	by := w.by()
	by.Actor = w.users["alice"]
	if err := w.perm.ChangeStatus(t.Context(), w.users["alice"], perm.StatusChange{By: by,
		Action: perm.ActionDeactivated}); err != nil {
		t.Fatal(err)
	}
	w.reply(id, "carol")
	s.ended(5 * time.Second)
}

// TestStreamClosesWhenReadRevoked: a perm write that takes read away from
// the viewer's group ends the stream before the next hint.
func TestStreamClosesWhenReadRevoked(t *testing.T) {
	w := newWorld(t, defaultOpts())
	id := w.thread("staff", "dave")
	s := w.open(eventsPath(ids(id)), w.signIn("dave")...)
	s.ready(1)
	if err := w.perm.Revoke(t.Context(), w.by(), w.forums["staff"], w.groups["stewards"], domain.PermRead); err != nil {
		t.Fatal(err)
	}
	w.reply(id, "root")
	s.ended(5 * time.Second)
}

// TestMovedPostSSEHintOnly is the live-events half of TKT-01M29CXJZZ's sixth
// criterion: a post split from general into staff gives alice, who reads
// general alone, the source's hint and nothing of the new thread, on her
// stream of the thread and her stream of general, and both stay open; dave,
// who reads staff, hears the new thread. A hint carries an id and a length
// and nothing else (decodeHint).
func TestMovedPostSSEHintOnly(t *testing.T) {
	w := newWorld(t, defaultOpts())
	id := w.thread("general", "alice")
	w.reply(id, "carol")
	w.quiesce()
	alice := w.signIn("alice")
	byThread := w.open(eventsPath(ids(id)), alice...)
	byThread.ready(1)
	byForum := w.open(eventsPath(nil, "general"), alice...)
	byForum.ready(0)
	staff := w.open(eventsPath(nil, "staff"), w.signIn("dave")...)
	staff.ready(0)

	res, err := w.svc.Split(t.Context(), w.viewer("root"), id, []uint32{2}, "staff", "Moved to staff")
	if err != nil {
		t.Fatal(err)
	}
	target, err := threadid.Parse(res.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	n := w.length(id)
	byThread.hintAt(id, n)
	byForum.hintAt(id, n)
	staff.hintAt(target, w.length(target))
	byThread.quiet(300 * time.Millisecond)
	byForum.quiet(300 * time.Millisecond)
}

// TestStreamClosesWhenThreadMovedToUnreadableForum: a thread moved where
// the viewer cannot read ends their stream on it.
func TestStreamClosesWhenThreadMovedToUnreadableForum(t *testing.T) {
	w := newWorld(t, defaultOpts())
	id := w.thread("general", "alice")
	s := w.open(eventsPath(ids(id)), w.signIn("alice")...)
	s.ready(1)
	if _, err := w.svc.Move(t.Context(), w.viewer("root"), id, "staff"); err != nil {
		t.Fatal(err)
	}
	w.reply(id, "root")
	s.ended(5 * time.Second)
}

// TestAgentStreamClosesWhenCredentialRevoked: an agent's stream, opened with
// its bearer credential, ends when the credential is revoked.
func TestAgentStreamClosesWhenCredentialRevoked(t *testing.T) {
	w := newWorld(t, defaultOpts())
	id := w.thread("general", "alice")
	var token string
	w.write(func(q *dbq.Queries) error {
		iss, err := agentcred.Issue(t.Context(), q, w.clock.now(), w.by(), w.users["scribe"])
		token = iss.Token
		return err
	})
	s := w.open(eventsPath(ids(id)), "Authorization", "Bearer "+token)
	s.ready(1)
	n := w.reply(id, "carol")
	if h := s.hint(); h.LogLength != n {
		t.Fatalf("the agent's hint is at %d, want %d", h.LogLength, n)
	}
	w.write(func(q *dbq.Queries) error {
		_, err := perm.RevokeAgentCredentialIn(t.Context(), q, w.clock.now(), w.by(), w.users["scribe"], "admin")
		return err
	})
	w.reply(id, "carol")
	s.ended(5 * time.Second)
}

// TestMoveIsRoutedAfterProjection: the hub receives a move's event while the
// projection is held. Once it is applied, the hint goes to the forum the
// thread joined and not the one it left, and a thread subscriber who cannot
// read the new forum has their stream closed without the move's hint.
func TestMoveIsRoutedAfterProjection(t *testing.T) {
	w := newWorld(t, defaultOpts())
	id := w.thread("general", "alice")
	alice := w.signIn("alice")
	byThread := w.open(eventsPath(ids(id)), alice...)
	byThread.ready(1)
	oldForum := w.open(eventsPath(nil, "general"), alice...)
	oldForum.ready(0)
	newForum := w.open(eventsPath(nil, "staff"), w.signIn("dave")...)
	newForum.ready(0)

	w.gate.hold()
	moved := make(chan error, 1)
	go func() {
		_, err := w.svc.Move(t.Context(), w.viewer("root"), id, "staff")
		moved <- err
	}()
	// The move's event is on the bus and the hub is waiting on the row,
	// which still says general. Without the wait, the hint would be routed
	// there and alice would still pass the access check.
	time.Sleep(300 * time.Millisecond)
	oldForum.quiet(10 * time.Millisecond)
	w.gate.release()
	if err := <-moved; err != nil {
		t.Fatal(err)
	}
	if h := newForum.hint(); h.ThreadID != id.String() || h.LogLength != w.length(id) {
		t.Errorf("the new forum's hint = %+v", h)
	}
	byThread.ended(5 * time.Second)
	oldForum.quiet(500 * time.Millisecond)
}

// TestProjectionLagClosesStreams: a thread whose projection does not reach
// its event within the wait gets no hint, and every stream that names it or
// any forum is closed. A stream about another thread is left alone.
func TestProjectionLagClosesStreams(t *testing.T) {
	o := defaultOpts()
	o.wait = 300 * time.Millisecond
	w := newWorld(t, o)
	held := w.thread("general", "alice")
	other := w.thread("general", "carol")
	alice := w.signIn("alice")
	byThread := w.open(eventsPath(ids(held)), alice...)
	byThread.ready(1)
	byForum := w.open(eventsPath(nil, "club"), alice...)
	byForum.ready(0)
	bystander := w.open(eventsPath(ids(other)), alice...)
	bystander.ready(1)

	w.gate.hold()
	go func() {
		// Reply appends and publishes; its read-back waits on nothing the
		// gate holds, but the projection of the append does.
		_, _ = w.svc.Reply(t.Context(), w.viewer("carol"), held, threads.ReplyRequest{Source: "behind"})
	}()
	byThread.ended(5 * time.Second)
	byForum.ended(5 * time.Second)
	bystander.quiet(200 * time.Millisecond)
	w.gate.release()
	if got := w.hub.Stats().Lagged; got == 0 {
		t.Error("the hub reports no lagging thread")
	}
	if DefaultProjectionWait != 5*time.Second {
		t.Errorf("the default wait is %s; docs/02 says 5 s", DefaultProjectionWait)
	}
}

// TestStreamLimits: each of the three limits refuses the next stream as
// rate-limited with Retry-After, and a closed stream frees its slot.
func TestStreamLimits(t *testing.T) {
	o := defaultOpts()
	o.limits = Limits{Connections: 4, PerUser: 2, Anonymous: 1}
	w := newWorld(t, o)
	id := w.thread("general", "alice")
	path := eventsPath(ids(id))
	refused := func(why string, header ...string) {
		t.Helper()
		resp, body := w.get(path, header...)
		if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") != "30" ||
			!strings.Contains(body, "rate-limited") {
			t.Errorf("%s: %d, Retry-After %q: %s", why, resp.StatusCode, resp.Header.Get("Retry-After"), body)
		}
	}
	alice := w.signIn("alice")
	a1 := w.open(path, alice...)
	a1.ready(1)
	w.open(path, w.signIn("alice")...).ready(1) // another tab, another session
	refused("a third stream for alice", alice...)

	anon := w.open(path)
	anon.ready(1)
	refused("a second anonymous stream")

	w.open(path, w.signIn("carol")...).ready(1)
	refused("a fifth stream in all", w.signIn("dave")...)

	if got := w.hub.Stats(); got.Open != 4 || got.Anonymous != 1 || got.Refused != 3 {
		t.Errorf("stats = %+v", got)
	}
	a1.close()
	anon.close()
	waitFor(t, func() bool { return w.hub.Stats().Open == 2 })
	w.open(path, alice...).ready(1)
	w.open(path).ready(1)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("the condition did not hold in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestResumeMissesNothing: a client that held an old length and reconnects
// after three appends gets the current length in the snapshot.
func TestResumeMissesNothing(t *testing.T) {
	w := newWorld(t, defaultOpts())
	id := w.thread("general", "alice")
	alice := w.signIn("alice")
	s := w.open(eventsPath(ids(id)), alice...)
	old := s.ready(1)[0].LogLength
	s.close()
	for range 3 {
		w.reply(id, "carol")
	}
	s = w.open(eventsPath(ids(id)), alice...)
	got := s.ready(1)[0].LogLength
	if got <= old || got != w.length(id) || got != w.pageLength(id, alice...) {
		t.Errorf("the snapshot is at %d; it was %d, the log is %d", got, old, w.length(id))
	}
	// A thread the cache has evicted is read from its projection.
	w.settle(id)
	if n, err := (threads.Projections{DB: w.db}).Projection(t.Context(), id); err != nil || n.LogLength != got {
		t.Errorf("the projection is at %+v, %v", n, err)
	}
}

// TestDroppedBatchResyncs: a batch reporting dropped events makes every
// thread-only stream resend its snapshot, which carries what it missed, and
// closes every stream that names a forum.
func TestDroppedBatchResyncs(t *testing.T) {
	w := newWorld(t, defaultOpts())
	missed := w.thread("general", "alice")
	other := w.thread("general", "carol")
	alice := w.signIn("alice")
	threadsOnly := w.open(eventsPath(ids(missed)), alice...)
	threadsOnly.ready(1)
	withForum := w.open(eventsPath(ids(missed), "general"), alice...)
	withForum.ready(1)

	// An append the bus never carried, as a dropped event is: written and
	// projected, and never published.
	var n int64
	if err := w.cache.With(missed, func(th *threadcache.Thread) error {
		res, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypePost, Author: uint64(w.users["carol"]),
			TimeMS: w.clock.now().UnixMilli(), Body: &threadlog.PostBody{Source: "unannounced"}})
		n = res.LogLength
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.proj.ProjectThread(t.Context(), missed, n); err != nil {
		t.Fatal(err)
	}
	if err := w.hub.Deliver(t.Context(), events.Batch{Dropped: 1,
		Events: []events.Event{{Thread: other, LogLength: w.length(other)}}}); err != nil {
		t.Fatal(err)
	}
	if h := threadsOnly.hint(); h != hintOf(missed, n) {
		t.Errorf("the resent snapshot = %+v, want %s at %d", h, missed, n)
	}
	withForum.ended(5 * time.Second)
}

// TestHintsFlushThroughReverseProxy: with httputil.ReverseProxy, as Traefik
// is, in front with its default settings, a hint reaches the client within
// a second of the append.
func TestHintsFlushThroughReverseProxy(t *testing.T) {
	w := newWorld(t, defaultOpts())
	id := w.thread("general", "alice")
	backend, err := url.Parse(w.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(httputil.NewSingleHostReverseProxy(backend))
	t.Cleanup(proxy.Close)
	s := openAt(t, proxy.Client(), proxy.URL+eventsPath(ids(id)))
	s.ready(1)
	start := time.Now()
	n := w.reply(id, "carol")
	select {
	case ev, ok := <-s.events:
		if !ok || ev.name != "thread" || decodeHint(t, ev.data).LogLength != n {
			t.Fatalf("got %+v, %v", ev, ok)
		}
	case <-time.After(time.Second):
		t.Fatal("no hint through the proxy within a second")
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("the hint took %s", d)
	}
}

func hintOf(id threadid.ID, n int64) dto.ThreadHint {
	return dto.ThreadHint{ThreadID: id.String(), LogLength: n}
}
