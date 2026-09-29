package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for a moderator's edit, delete and restore of any post
// (TKT-01M29CRJ5J). In the seeded thread bob moderates, and #4 is carol's.

// frames is every record in id's log about ordinal, in log order, with its
// header and decoded body.
func (w *world) frames(id threadid.ID, ordinal uint32) []struct {
	h    threadlog.FrameHeader
	body any
} {
	w.t.Helper()
	f, err := os.ReadFile(id.LogPath(w.root))
	if err != nil {
		w.t.Fatal(err)
	}
	var out []struct {
		h    threadlog.FrameHeader
		body any
	}
	if err := threadlog.ScanFrames(bytes.NewReader(f), int64(len(f)), func(r threadlog.ScanRecord) bool {
		if r.Err != nil {
			w.t.Fatal(r.Err)
		}
		if r.Header.Ordinal == ordinal {
			v, err := threadlog.DecodeBody(r.Header.Type, r.Header.Encoding, r.Body)
			if err != nil {
				w.t.Fatal(err)
			}
			out = append(out, struct {
				h    threadlog.FrameHeader
				body any
			}{r.Header, v})
		}
		return true
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// auditSince is the audit rows written after the first n, each as its
// action, its actor (null for none), its target and its reason.
func (w *world) auditSince(n int) []string {
	w.t.Helper()
	var out []string
	for _, a := range w.auditLog()[n:] {
		by := "null"
		if a.actor.Valid {
			by = fmt.Sprint(a.actor.Int64)
		}
		out = append(out, fmt.Sprintf("%s by %s on %s/%s: %v", a.action, by, a.typ, a.id, a.details["reason"]))
	}
	return out
}

// audited is the auditSince line for a row by who against post ordinal.
func (w *world) audited(action, who string, id threadid.ID, ordinal int, reason string) string {
	return fmt.Sprintf("%s by %d on post/%s/%d: %s", action, w.users[who], id, ordinal, reason)
}

// TestModeratorEditsAnyPost is AC1 and the server half of AC2: a
// moderator's edit of a member's post appends an Edit whose frame actor is
// the moderator, is never a grace edit, leaves the post its author, and is
// served with edited_by_moderator to every class while history names the
// moderator. It writes one post.edited row. The author's later edit makes the
// marker go, since it describes the current revision, and writes no row.
func TestModeratorEditsAnyPost(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	h := w.chain()
	rows := len(w.auditLog())

	// Carol's post is inside her grace window here; the edit is still a
	// visible one, because it is not hers.
	res := changed(t, send(h, "bob", http.MethodPatch, postPathN(id, 4),
		`{"source":"Agreed with @alice on this. [link removed]","reason":"spam link"}`))
	if !res.Post.EditedByModerator || res.Post.EditedAt == nil || res.Revision != 2 || res.Post.Author.Handle != "carol" {
		t.Fatalf("the moderator's edit answered %+v", res)
	}
	e := w.entry(id, 4)
	if e.Author != uint64(w.users["carol"]) || e.Revision != 2 || !e.Flags.Has(threadlog.EntryEdited) {
		t.Errorf("entry after the moderator's edit %+v: want carol's, revision 2, edited", e)
	}
	fr := w.frames(id, 4)
	last := fr[len(fr)-1]
	if b, ok := last.body.(*threadlog.EditBody); !ok || last.h.Actor != uint64(w.users["bob"]) || b.Grace || b.Reason != "spam link" {
		t.Errorf("the last record for #4 is %s by %d with %+v: want bob's visible Edit", last.h.Type, last.h.Actor, last.body)
	}

	for _, who := range []string{"", "carol", "alice", "bob"} {
		p := decodePost(t, newHandlerFor(w).get(w.viewer(who), postPathN(id, 4)))
		if !p.EditedByModerator || p.Author.Handle != "carol" {
			t.Errorf("%q reads #4 as %+v: want carol's, edited by a moderator", who, p)
		}
	}
	var hist dto.PostHistory
	if err := json.Unmarshal(w.get(w.viewer("alice"), postPathN(id, 4)+"/history").Body.Bytes(), &hist); err != nil {
		t.Fatal(err)
	}
	if len(hist.Revisions) != 2 || hist.Revisions[0].Actor.Handle != "carol" || hist.Revisions[1].Actor.Handle != "bob" ||
		hist.Revisions[1].Reason != "spam link" {
		t.Errorf("history %+v: want carol then bob", hist)
	}
	if got, want := w.auditSince(rows), []string{w.audited(projection.ActionPostEdited, "bob", id, 4, "spam link")}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("audit rows %v, want %v", got, want)
	}

	// The author's own edit: the marker describes the current revision.
	rows = len(w.auditLog())
	res = changed(t, send(h, "carol", http.MethodPatch, postPathN(id, 4), `{"source":"Agreed with @alice."}`))
	if res.Post.EditedByModerator || res.Post.EditedAt == nil {
		t.Errorf("after the author's edit the post is %+v: want edited, not by a moderator", res.Post)
	}
	if _, err := w.proj.ProjectThread(t.Context(), id, res.LogLength); err != nil {
		t.Fatal(err)
	}
	if got := w.auditSince(rows); len(got) != 0 {
		t.Errorf("the author's own edit wrote audit rows %v", got)
	}
}

// TestModeratorDeleteHidesAndOnlyAModeratorRestores is AC5 and the hide and
// restore half of AC4: a moderator's delete of a member's post writes
// by_moderator and sets hidden-by-mod, not deleted; the author's restore,
// edit and delete are then forbidden and append nothing; the moderator's
// restore clears it. Each writes one row, post.hidden then post.restored. A
// moderator's delete of their own post is an ordinary delete with no row.
func TestModeratorDeleteHidesAndOnlyAModeratorRestores(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	h := w.chain()
	p4 := postPathN(id, 4)
	rows := len(w.auditLog())

	del := changed(t, send(h, "bob", http.MethodDelete, p4, `{"reason":"off topic"}`))
	if !del.Post.Deleted || !strings.Contains(del.Post.HTML, "Agreed with") {
		t.Errorf("the moderator's delete answered %+v: want a tombstone with its content, for a moderator", del.Post)
	}
	e := w.entry(id, 4)
	if !e.Flags.Has(threadlog.EntryHiddenByMod) || e.Flags.Has(threadlog.EntryDeleted) || e.Author != uint64(w.users["carol"]) {
		t.Errorf("entry after the moderator's delete %+v: want hidden-by-mod and not deleted, still carol's", e)
	}
	fr := w.frames(id, 4)
	if b, ok := fr[len(fr)-1].body.(*threadlog.DeleteBody); !ok || !b.ByModerator || fr[len(fr)-1].h.Actor != uint64(w.users["bob"]) {
		t.Errorf("the last record for #4 is %+v: want bob's Delete with by_moderator", fr[len(fr)-1])
	}
	for _, who := range []string{"", "alice", "carol"} {
		if p := decodePost(t, newHandlerFor(w).get(w.viewer(who), p4)); !p.Deleted || p.HTML != "" {
			t.Errorf("%q reads the hidden post as %+v: want a tombstone without html", who, p)
		}
	}

	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, p4 + "/restore", ""},
		{http.MethodPatch, p4, `{"source":"putting it back"}`},
		{http.MethodDelete, p4, ""},
	} {
		before := w.logState(id)
		if rec := send(h, "carol", c.method, c.path, c.body); problemCode(t, rec) != "forbidden" {
			t.Errorf("the author's %s %s on her hidden post: %d %s, want forbidden", c.method, c.path, rec.Code, rec.Body)
		}
		if after := w.logState(id); after != before {
			t.Errorf("the author's refused %s changed the log: %+v then %+v", c.method, before, after)
		}
	}
	// Hiding it again, or editing it, is conflict, even for a moderator.
	for _, c := range []struct{ method, body string }{{http.MethodDelete, ""}, {http.MethodPatch, `{"source":"x"}`}} {
		before := w.logState(id)
		if rec := send(h, "bob", c.method, p4, c.body); problemCode(t, rec) != "conflict" {
			t.Errorf("the moderator's %s of the hidden post: %d %s, want conflict", c.method, rec.Code, rec.Body)
		}
		if after := w.logState(id); after != before {
			t.Errorf("the moderator's refused %s changed the log", c.method)
		}
	}

	res := changed(t, send(h, "bob", http.MethodPost, p4+"/restore", `{"reason":"on reflection"}`))
	if res.Post.Deleted || res.Post.HTML == "" {
		t.Errorf("the moderator's restore answered %+v", res.Post)
	}
	if e := w.entry(id, 4); !e.Flags.Visible() {
		t.Errorf("entry after the moderator's restore %+v", e)
	}
	if p := decodePost(t, newHandlerFor(w).get(w.viewer("alice"), p4)); p.Deleted || p.HTML == "" {
		t.Errorf("a member reads the restored post as %+v", p)
	}
	want := []string{
		w.audited(projection.ActionPostHidden, "bob", id, 4, "off topic"),
		w.audited(projection.ActionPostRestored, "bob", id, 4, "on reflection"),
	}
	if got := w.auditSince(rows); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("audit rows %v, want %v", got, want)
	}

	// The author's own delete stays hers to restore, and a moderator may
	// restore it too. Neither of hers writes a row; the moderator's does.
	rows = len(w.auditLog())
	changed(t, send(h, "carol", http.MethodDelete, p4, ""))
	if e := w.entry(id, 4); !e.Flags.Has(threadlog.EntryDeleted) || e.Flags.Has(threadlog.EntryHiddenByMod) {
		t.Errorf("entry after the author's delete %+v: want deleted, not hidden", e)
	}
	changed(t, send(h, "carol", http.MethodPost, p4+"/restore", ""))
	changed(t, send(h, "carol", http.MethodDelete, p4, ""))
	changed(t, send(h, "bob", http.MethodPost, p4+"/restore", `{"reason":"undeleted"}`))
	if got, want := w.auditSince(rows), []string{w.audited(projection.ActionPostRestored, "bob", id, 4, "undeleted")}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("audit rows %v, want %v", got, want)
	}

	// A moderator's delete of their own post is an ordinary delete.
	rows = len(w.auditLog())
	n, _ := w.fresh(id, "bob")
	res = changed(t, send(h, "bob", http.MethodDelete, postPathN(id, n), ""))
	if e := w.entry(id, uint32(n)); !e.Flags.Has(threadlog.EntryDeleted) || e.Flags.Has(threadlog.EntryHiddenByMod) {
		t.Errorf("entry after a moderator deleted their own post %+v: want deleted, not hidden", e)
	}
	if _, err := w.proj.ProjectThread(t.Context(), id, res.LogLength); err != nil {
		t.Fatal(err)
	}
	if got := w.auditSince(rows); len(got) != 0 {
		t.Errorf("a moderator's delete of their own post wrote %v", got)
	}
}

// TestModeratorBypassesWindows is AC3: past both author windows, the author
// is refused and a moderator's edit and delete of the post succeed.
func TestModeratorBypassesWindows(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.setting("posts.edit_window_seconds", 120)
	w.setting("posts.delete_window_seconds", 60)
	n, _ := w.fresh(id, "carol")
	p := postPathN(id, n)
	w.clock += 10 * 60_000
	h := w.chain()
	for _, c := range []struct{ method, body, want string }{
		{http.MethodPatch, `{"source":"late"}`, "edit-window-closed"},
		{http.MethodDelete, "", "delete-window-closed"},
	} {
		if rec := send(h, "carol", c.method, p, c.body); problemCode(t, rec) != c.want {
			t.Errorf("the author's late %s: %d %s, want %s", c.method, rec.Code, rec.Body, c.want)
		}
	}
	if res := changed(t, send(h, "bob", http.MethodPatch, p, `{"source":"tidied by a moderator"}`)); !res.Post.EditedByModerator {
		t.Errorf("the moderator's late edit answered %+v", res.Post)
	}
	if res := changed(t, send(h, "bob", http.MethodDelete, p, "")); !res.Post.Deleted {
		t.Errorf("the moderator's late delete answered %+v", res.Post)
	}
	if e := w.entry(id, uint32(n)); !e.Flags.Has(threadlog.EntryHiddenByMod) {
		t.Errorf("entry %+v: want hidden-by-mod", e)
	}
	if res := changed(t, send(h, "bob", http.MethodPost, p+"/restore", "")); res.Post.Deleted {
		t.Errorf("the moderator's late restore answered %+v", res.Post)
	}
}

// TestModeratorHideMovesTheResponseCacheKey is AC6, the response cache
// invariant (docs/13 "Invariants"): a page cached before a moderator's hide
// is not served after it. The Delete is an append, so it moves the log
// length in the key; members and anonymous viewers then get the post as a
// tombstone without html, and a moderator still gets its content.
func TestModeratorHideMovesTheResponseCacheKey(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	path := "/api/v1/threads/" + id.String()
	classes := []string{"", "carol", "bob"}
	for _, who := range classes {
		w.same(w.viewer(who), path)
	}
	before := w.svc.Responses.Stats()
	for _, who := range classes {
		if p := page(t, w.get(w.viewer(who), path)); p.Posts[3].Ordinal != 4 || p.Posts[3].Deleted {
			t.Fatalf("before the hide %q got #4 as %+v", who, p.Posts[3])
		}
	}
	if s := w.svc.Responses.Stats(); s.Hits != before.Hits+3 || s.Inserts != before.Inserts {
		t.Fatalf("before the hide the second round was not served from the cache: %+v then %+v", before, s)
	}

	del := changed(t, w.sendAs(w.viewer("bob"), http.MethodDelete, postPathN(id, 4), ""))
	before = w.svc.Responses.Stats()
	for _, who := range classes {
		rec := w.get(w.viewer(who), path)
		p := page(t, rec)
		if tag := rec.Header().Get("ETag"); !strings.HasPrefix(tag, fmt.Sprintf(`"%s-%d-`, id, del.LogLength)) {
			t.Errorf("after the hide %q got ETag %s, not one under the new log length %d", who, tag, del.LogLength)
		}
		post := p.Posts[3]
		switch {
		case post.Ordinal != 4 || !post.Deleted:
			t.Errorf("after the hide %q got #4 as %+v: want a tombstone", who, post)
		case who == "bob" && !strings.Contains(post.HTML, "Agreed with"):
			t.Errorf("after the hide the moderator got #4 without its content: %+v", post)
		case who != "bob" && post.HTML != "":
			t.Errorf("after the hide %q got #4 with html %q", who, post.HTML)
		}
	}
	if s := w.svc.Responses.Stats(); s.Misses != before.Misses+3 || s.Inserts != before.Inserts+3 || s.Hits != before.Hits {
		t.Errorf("after the hide the three classes' pages were not fresh draws: %+v then %+v", before, s)
	}
	for _, who := range classes {
		w.same(w.viewer(who), path)
	}
}
