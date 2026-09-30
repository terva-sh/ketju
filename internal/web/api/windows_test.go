package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for the author windows and grace edits (TKT-01M29CKRHS). Each posts
// a fresh post of carol's, so the seed's times do not decide a window, and
// moves the world's clock, which is the service's.

// setting sets a site setting as bob.
func (w *world) setting(name string, value int) {
	w.t.Helper()
	raw, _ := json.Marshal(value)
	if err := settings.Set(w.t.Context(), w.db, name, raw, w.users["bob"], time.UnixMilli(w.clock)); err != nil {
		w.t.Fatal(err)
	}
}

// fresh posts a new post by who and returns its ordinal and its creation
// time, which is the world's clock afterwards.
func (w *world) fresh(id threadid.ID, who string) (int, int64) {
	w.t.Helper()
	w.post(id, who, "a fresh post by "+who, 0)
	return w.entryCount(id), w.clock
}

func (w *world) entryCount(id threadid.ID) int {
	w.t.Helper()
	return w.logState(id).posts
}

// editBodies is every Edit body in id's log for ordinal, in log order.
func (w *world) editBodies(id threadid.ID, ordinal uint32) []threadlog.EditBody {
	w.t.Helper()
	f, err := os.ReadFile(id.LogPath(w.root))
	if err != nil {
		w.t.Fatal(err)
	}
	var out []threadlog.EditBody
	if err := threadlog.ScanFrames(bytes.NewReader(f), int64(len(f)), func(r threadlog.ScanRecord) bool {
		if r.Err != nil {
			w.t.Fatal(r.Err)
		}
		if r.Header.Type == threadlog.TypeEdit && r.Header.Ordinal == ordinal {
			v, err := threadlog.DecodeBody(r.Header.Type, r.Header.Encoding, r.Body)
			if err != nil {
				w.t.Fatal(err)
			}
			out = append(out, *v.(*threadlog.EditBody))
		}
		return true
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// projectedEditedAt is post_meta.edited_at for one post, after projecting
// the whole log.
func (w *world) projectedEditedAt(id threadid.ID, ordinal int) sql.NullInt64 {
	w.t.Helper()
	fi, err := os.Stat(id.LogPath(w.root))
	if err != nil {
		w.t.Fatal(err)
	}
	if _, err := w.proj.ProjectThread(w.t.Context(), id, fi.Size()); err != nil {
		w.t.Fatal(err)
	}
	var v sql.NullInt64
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), `SELECT edited_at FROM post_meta WHERE thread_id = ? AND ordinal = ?`,
			int64(id), ordinal).Scan(&v)
	}); err != nil {
		w.t.Fatal(err)
	}
	return v
}

// servedEditedAt is edited_at as the single-post endpoint serves it to
// carol, nil for none, checked equal to the thread page's and to
// post_meta's.
func (w *world) servedEditedAt(id threadid.ID, ordinal int) *dto.Millis {
	w.t.Helper()
	p := decodePost(w.t, newHandlerFor(w).get(w.viewer("carol"), postPathN(id, ordinal)))
	onPage := page(w.t, newHandlerFor(w).get(w.viewer("carol"), "/api/v1/threads/"+id.String()+"?page=1"))
	found := false
	for _, q := range onPage.Posts {
		if q.Ordinal == ordinal {
			found = true
			if !equalMillis(q.EditedAt, p.EditedAt) {
				w.t.Errorf("post %d: the page serves edited_at %v, the post endpoint %v", ordinal, show(q.EditedAt), show(p.EditedAt))
			}
		}
	}
	if !found {
		w.t.Fatalf("post %d is not on page 1", ordinal)
	}
	projected := w.projectedEditedAt(id, ordinal)
	if projected.Valid != (p.EditedAt != nil) || (p.EditedAt != nil && projected.Int64 != int64(*p.EditedAt)) {
		w.t.Errorf("post %d: served edited_at %v, post_meta.edited_at %+v", ordinal, show(p.EditedAt), projected)
	}
	return p.EditedAt
}

func equalMillis(a, b *dto.Millis) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

func show(m *dto.Millis) any {
	if m == nil {
		return "null"
	}
	return int64(*m)
}

// TestAuthorWindows is AC1 and AC4: with the windows set to two minutes for
// edits and one for deletes, the author edits and deletes and restores
// inside them and is refused from the moment each closes, measured from the
// post's creation, with the log unchanged. A moderator's own post is bound
// by neither window.
func TestAuthorWindows(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.setting("posts.edit_window_seconds", 120)
	w.setting("posts.delete_window_seconds", 60)

	type attempt struct {
		after          time.Duration // since the post was created
		method, suffix string
		body, want     string
	}
	run := func(t *testing.T, who string, ordinal int, created int64, cases []attempt) {
		t.Helper()
		for _, c := range cases {
			w.clock = created + c.after.Milliseconds()
			before := w.logState(id)
			// A chain per request, so the write limiter's burst is never
			// what answers.
			rec := send(w.chain(), who, c.method, postPathN(id, ordinal)+c.suffix, c.body)
			if c.want == "ok" {
				if rec.Code != http.StatusOK {
					t.Errorf("%s %s%s at +%s: %d %s, want 200", who, c.method, c.suffix, c.after, rec.Code, rec.Body)
				}
				continue
			}
			if got := problemCode(t, rec); got != c.want {
				t.Errorf("%s %s%s at +%s: %s (%d), want %s", who, c.method, c.suffix, c.after, got, rec.Code, c.want)
			}
			if c.want != "conflict" && rec.Code != http.StatusForbidden {
				t.Errorf("%s %s%s at +%s: status %d, want 403", who, c.method, c.suffix, c.after, rec.Code)
			}
			if after := w.logState(id); after != before {
				t.Errorf("%s %s%s at +%s: a refusal changed the log: %+v then %+v", who, c.method, c.suffix, c.after, before, after)
			}
		}
	}

	t.Run("author", func(t *testing.T) {
		n, created := w.fresh(id, "carol")
		run(t, "carol", n, created, []attempt{
			{59 * time.Second, http.MethodDelete, "", "", "ok"},
			{59*time.Second + 999*time.Millisecond, http.MethodPost, "/restore", "", "ok"},
			{60 * time.Second, http.MethodDelete, "", "", "delete-window-closed"},
			// An edit is measured from the post's creation, not from the
			// last edit.
			{100 * time.Second, http.MethodPatch, "", `{"source":"inside the edit window"}`, "ok"},
			{119 * time.Second, http.MethodPatch, "", `{"source":"still inside"}`, "ok"},
			{120 * time.Second, http.MethodPatch, "", `{"source":"too late"}`, "edit-window-closed"},
			{24 * time.Hour, http.MethodPatch, "", `{"source":"far too late"}`, "edit-window-closed"},
		})
	})

	t.Run("restore of one's own delete", func(t *testing.T) {
		n, created := w.fresh(id, "carol")
		run(t, "carol", n, created, []attempt{
			{30 * time.Second, http.MethodDelete, "", "", "ok"},
			{61 * time.Second, http.MethodPost, "/restore", "", "delete-window-closed"},
			// The edit window is still open; the post is deleted, so the
			// edit is conflict, not a window refusal.
			{62 * time.Second, http.MethodPatch, "", `{"source":"x"}`, "conflict"},
		})
	})

	t.Run("a moderator's own post", func(t *testing.T) {
		n, created := w.fresh(id, "bob")
		run(t, "bob", n, created, []attempt{
			{48 * time.Hour, http.MethodPatch, "", `{"source":"a moderator's late edit"}`, "ok"},
			{48 * time.Hour, http.MethodDelete, "", "", "ok"},
			{49 * time.Hour, http.MethodPost, "/restore", "", "ok"},
		})
	})

	t.Run("a window of zero never opens", func(t *testing.T) {
		w.setting("posts.edit_window_seconds", 0)
		w.setting("posts.delete_window_seconds", 0)
		n, created := w.fresh(id, "carol")
		run(t, "carol", n, created, []attempt{
			{0, http.MethodPatch, "", `{"source":"x"}`, "edit-window-closed"},
			{0, http.MethodDelete, "", "", "delete-window-closed"},
			// Nor to a clock that stepped back behind the post (review 1378
			// on PR #146).
			{-time.Minute, http.MethodPatch, "", `{"source":"x"}`, "edit-window-closed"},
			{-time.Minute, http.MethodDelete, "", "", "delete-window-closed"},
		})
	})
}

// TestAuthorWindowsTooLargeForADuration: each window's setting at a count of
// seconds past what a Duration holds is the longest window there is, still
// open 30 days after posting, rather than one that overflowed to negative
// and closed at once (TKT-01M3R33SR5, following Terva review 1535 finding 2
// on the rename window). The grace period only matters inside the edit
// window, so its subtest opens that too.
func TestAuthorWindowsTooLargeForADuration(t *testing.T) {
	const tooLarge = 10_000_000_000
	const month = 30 * 24 * time.Hour

	t.Run("posts.edit_window_seconds", func(t *testing.T) {
		w := newWorld(t)
		id, _ := w.seed()
		w.setting("posts.edit_window_seconds", tooLarge)
		n, created := w.fresh(id, "carol")
		w.clock = created + month.Milliseconds()
		res := changed(t, send(w.chain(), "carol", http.MethodPatch, postPathN(id, n), `{"source":"a month on"}`))
		// Grace is still its default of seconds, so the edit is visible.
		if res.Post.EditedAt == nil || int64(*res.Post.EditedAt) != w.clock {
			t.Errorf("the edit a month on answered edited_at %v, want %d", show(res.Post.EditedAt), w.clock)
		}
	})

	t.Run("posts.delete_window_seconds", func(t *testing.T) {
		w := newWorld(t)
		id, _ := w.seed()
		w.setting("posts.delete_window_seconds", tooLarge)
		n, created := w.fresh(id, "carol")
		w.clock = created + month.Milliseconds()
		if rec := send(w.chain(), "carol", http.MethodDelete, postPathN(id, n), ""); rec.Code != http.StatusOK {
			t.Fatalf("the delete a month on: %d %s, want 200", rec.Code, rec.Body)
		}
		if e := w.entry(id, uint32(n)); !e.Flags.Has(threadlog.EntryDeleted) {
			t.Errorf("entry %+v: want deleted", e)
		}
	})

	t.Run("posts.grace_edit_seconds", func(t *testing.T) {
		w := newWorld(t)
		id, _ := w.seed()
		w.setting("posts.edit_window_seconds", tooLarge)
		w.setting("posts.grace_edit_seconds", tooLarge)
		n, created := w.fresh(id, "carol")
		w.clock = created + month.Milliseconds()
		res := changed(t, send(w.chain(), "carol", http.MethodPatch, postPathN(id, n), `{"source":"still a grace edit"}`))
		if res.Post.EditedAt != nil {
			t.Errorf("the grace edit a month on answered edited_at %d, want null", *res.Post.EditedAt)
		}
		if b := w.editBodies(id, uint32(n)); len(b) != 1 || !b[0].Grace {
			t.Fatalf("the log holds %+v, want one grace Edit", b)
		}
		if at := w.servedEditedAt(id, n); at != nil {
			t.Errorf("a grace-edited post is served with edited_at %d", *at)
		}
	})
}

// TestGraceEdit is AC2 and AC3: an author's edit ten seconds after posting
// is stored as revision 2 with grace, served with edited_at null, and listed
// in history. An edit past the grace period sets edited_at, and a later edit
// inside the grace period, once the setting has widened, does not clear it.
func TestGraceEdit(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	h := w.chain()
	n, created := w.fresh(id, "carol")

	w.clock = created + 10_000
	res := changed(t, send(h, "carol", http.MethodPatch, postPathN(id, n), `{"source":"a typo, fixed"}`))
	if res.Revision != 2 || res.Post.EditedAt != nil {
		t.Fatalf("the grace edit answered revision %d, edited_at %v", res.Revision, show(res.Post.EditedAt))
	}
	if b := w.editBodies(id, uint32(n)); len(b) != 1 || !b[0].Grace {
		t.Fatalf("the log holds %+v, want one grace Edit", b)
	}
	if e := w.entry(id, uint32(n)); e.Revision != 2 || e.Flags.Has(threadlog.EntryEdited) {
		t.Fatalf("entry %+v: want revision 2, not edited", e)
	}
	if at := w.servedEditedAt(id, n); at != nil {
		t.Fatalf("a grace-edited post is served with edited_at %d", *at)
	}
	var hist dto.PostHistory
	if err := json.Unmarshal(newHandlerFor(w).get(w.viewer("alice"), postPathN(id, n)+"/history").Body.Bytes(), &hist); err != nil {
		t.Fatal(err)
	}
	if len(hist.Revisions) != 2 || hist.Revisions[1].Source != "a typo, fixed" {
		t.Fatalf("history %+v, want both revisions", hist)
	}

	w.clock = created + 6*60_000
	res = changed(t, send(h, "carol", http.MethodPatch, postPathN(id, n), `{"source":"a real change"}`))
	if res.Post.EditedAt == nil || int64(*res.Post.EditedAt) != w.clock {
		t.Fatalf("the edit past grace answered edited_at %v, want %d", show(res.Post.EditedAt), w.clock)
	}
	visible := w.clock

	// Widen grace past the post's age. The post already has a visible
	// edit, so the next edit is visible too: no grace edit follows one.
	w.setting("posts.grace_edit_seconds", 3600)
	w.clock = created + 7*60_000
	res = changed(t, send(h, "carol", http.MethodPatch, postPathN(id, n), `{"source":"and once more"}`))
	if b := w.editBodies(id, uint32(n)); len(b) != 3 || b[1].Grace || b[2].Grace {
		t.Fatalf("the log holds %+v, want a grace Edit and two visible ones", b)
	}
	if at := w.servedEditedAt(id, n); at == nil || int64(*at) != w.clock || int64(*at) == visible {
		t.Fatalf("after a later edit inside the widened grace, edited_at is %v, want %d", show(at), w.clock)
	}
}

// TestGraceOfZeroIsOff: with posts.grace_edit_seconds at 0 no edit is a
// grace edit, not at the moment of posting and not from a clock that stepped
// back behind the post (review 1378 on PR #146). The edit window, still
// open, admits the edit that stepped-back clock makes.
func TestGraceOfZeroIsOff(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.setting("posts.grace_edit_seconds", 0)
	for _, after := range []int64{0, -60_000} {
		n, created := w.fresh(id, "carol")
		w.clock = created + after
		res := changed(t, send(w.chain(), "carol", http.MethodPatch, postPathN(id, n), `{"source":"no grace"}`))
		if b := w.editBodies(id, uint32(n)); len(b) != 1 || b[0].Grace {
			t.Errorf("at %+d ms: the log holds %+v, want one Edit without grace", after, b)
		}
		if res.Post.EditedAt == nil {
			t.Errorf("at %+d ms: the edit is served with edited_at null", after)
		}
	}
}

// TestEditedAtIsTheLastVisibleEdit is AC6: a moderator's visible edit of a
// member's post a minute after posting, then the author's edit at two
// minutes, still inside grace. The author's edit follows a visible one, so
// it is written without grace, and edited_at is its time on every read path.
// A grace Edit after a visible one, which only a hand-made log holds, moves
// edited_at to its own time on the index path and the projection alike.
func TestEditedAtIsTheLastVisibleEdit(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	n, created := w.fresh(id, "carol")

	// The moderator's edit is appended directly: reaching over another
	// member's post through the API is TKT-01M29CRJ5J.
	w.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: uint64(w.users["bob"]), Ordinal: uint32(n),
		TimeMS: created + 60_000, Body: &threadlog.EditBody{Source: "moderated"}})
	if at := w.servedEditedAt(id, n); at == nil || int64(*at) != created+60_000 {
		t.Fatalf("after the moderator's edit, edited_at %v", show(at))
	}

	w.clock = created + 120_000
	res := changed(t, send(w.chain(), "carol", http.MethodPatch, postPathN(id, n), `{"source":"the author's own words"}`))
	if b := w.editBodies(id, uint32(n)); len(b) != 2 || b[1].Grace {
		t.Fatalf("the log holds %+v, want the author's edit without grace", b)
	}
	if res.Post.EditedAt == nil || int64(*res.Post.EditedAt) != created+120_000 {
		t.Fatalf("the author's edit answered edited_at %v, want %d", show(res.Post.EditedAt), created+120_000)
	}
	if at := w.servedEditedAt(id, n); at == nil || int64(*at) != created+120_000 {
		t.Fatalf("edited_at %v, want %d", show(at), created+120_000)
	}

	w.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: uint64(w.users["carol"]), Ordinal: uint32(n),
		TimeMS: created + 180_000, Body: &threadlog.EditBody{Source: "hand-made grace", Grace: true}})
	if at := w.servedEditedAt(id, n); at == nil || int64(*at) != created+180_000 {
		t.Fatalf("after a hand-made grace edit, edited_at %v, want %d", show(at), created+180_000)
	}
}

// TestGraceComesFromTheLog is AC5 and the response cache invariant for this
// ticket: after a grace edit, setting posts.grace_edit_seconds to 0 changes
// neither the page, fresh or from the response cache, nor the edited flag a
// full rebuild of the index gives. Nothing in the cached posts depends on a
// setting.
func TestGraceComesFromTheLog(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	n, created := w.fresh(id, "carol")
	w.clock = created + 10_000
	changed(t, send(w.chain(), "carol", http.MethodPatch, postPathN(id, n), `{"source":"a typo, fixed"}`))

	path := "/api/v1/threads/" + id.String() + "?page=1"
	editedAt := func(body string) *dto.Millis {
		t.Helper()
		var p dto.ThreadPage
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatal(err)
		}
		for _, q := range p.Posts {
			if q.Ordinal == n {
				return q.EditedAt
			}
		}
		t.Fatalf("post %d is not on the page", n)
		return nil
	}
	anon := w.viewer("")
	before := w.same(anon, path)
	w.same(anon, path) // a hit
	if at := editedAt(before); at != nil {
		t.Fatalf("the grace-edited post is served edited at %d", *at)
	}
	hits := w.svc.Responses.Stats().Hits

	w.setting("posts.grace_edit_seconds", 0)
	cached := w.same(anon, path)
	if w.svc.Responses.Stats().Hits == hits {
		t.Fatal("the page after the setting changed was not served from the response cache")
	}
	if cached != before {
		t.Fatalf("the cached page changed with the setting:\n%s\n---\n%s", before, cached)
	}
	if fresh := uncached(w).get(anon, path).Body.String(); fresh != before {
		t.Fatalf("a fresh page changed with the setting:\n%s\n---\n%s", before, fresh)
	}

	f, err := os.Open(id.LogPath(w.root))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := threadlog.Open(f, fi.Size(), nil)
	if err != nil {
		t.Fatal(err)
	}
	e, err := rec.Index.Entry(uint32(n))
	if err != nil {
		t.Fatal(err)
	}
	if live := w.entry(id, uint32(n)); e != live || e.Flags.Has(threadlog.EntryEdited) || e.Revision != 2 {
		t.Fatalf("the rebuilt entry %+v, the live one %+v: want the same, revision 2 and not edited", e, live)
	}
}

// TestNoGraceEditFollowsAVisibleOne is the other half of AC6, under
// concurrency: on each of many fresh posts, the author's edit inside grace
// races a visible Edit appended straight to the thread. Whichever lands
// first, no grace Edit follows a visible one in the log. The author's edit
// lands first as grace, lands after as visible, or is refused as conflict
// when the visible edit slips in between its check and its append.
func TestNoGraceEditFollowsAVisibleOne(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	carol := w.viewer("carol")
	const rounds = 60
	var graceFirst, conflicts int
	for range rounds {
		n, created := w.fresh(id, "carol")
		w.clock = created + 10_000
		var wg sync.WaitGroup
		var code int
		wg.Add(2)
		go func() {
			defer wg.Done()
			// Straight to the handler, past the chain's write limiter.
			r := httptest.NewRequest(http.MethodPatch, postPathN(id, n), strings.NewReader(`{"source":"author"}`))
			r = r.WithContext(domain.WithViewer(r.Context(), carol))
			rec := httptest.NewRecorder()
			w.handler.ServeHTTP(rec, r)
			code = rec.Code
		}()
		go func() {
			defer wg.Done()
			// Spread the visible edit across the author's request, so that
			// some rounds land it before, some after, and some in between
			// the check and the append. The test cannot fail for timing;
			// only how many rounds reach the race varies.
			time.Sleep(time.Duration(rand.IntN(3000)) * time.Microsecond)
			if err := w.cache.With(id, func(th *threadcache.Thread) error {
				_, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypeEdit, Author: uint64(w.users["bob"]),
					Ordinal: uint32(n), TimeMS: created + 5_000, Body: &threadlog.EditBody{Source: "visible"}})
				return err
			}); err != nil {
				t.Error(err)
			}
		}()
		wg.Wait()
		switch code {
		case http.StatusOK:
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("the author's edit answered %d", code)
		}
		b := w.editBodies(id, uint32(n))
		visible := false
		for i, e := range b {
			if e.Grace && visible {
				t.Fatalf("post %d: Edit %d is grace and follows a visible one: %+v", n, i+1, b)
			}
			visible = visible || !e.Grace
		}
		if len(b) > 0 && b[0].Grace {
			graceFirst++
		}
	}
	t.Logf("%d rounds: the grace edit landed first %d times and was refused %d times", rounds, graceFirst, conflicts)
}
