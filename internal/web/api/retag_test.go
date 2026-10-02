package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for retagging and the tag filter (TKT-01M29CYSCP): docs/07
// "Retagging threads", docs/08 "Thread tags", docs/06 "Thread list views".

func retag(tags ...string) string {
	if tags == nil {
		tags = []string{}
	}
	return fmt.Sprintf(`{"tags":%s}`, renderJSONTags(tags))
}

func renderJSONTags(tags []string) string {
	q := make([]string, len(tags))
	for i, t := range tags {
		q[i] = fmt.Sprintf("%q", t)
	}
	return "[" + strings.Join(q, ",") + "]"
}

// taggedRecords is the body of every ThreadTagged in id's log, in log order.
func (w *world) taggedRecords(id threadid.ID) [][]string {
	w.t.Helper()
	f, err := os.ReadFile(id.LogPath(w.root))
	if err != nil {
		w.t.Fatal(err)
	}
	var out [][]string
	if err := threadlog.ScanFrames(bytes.NewReader(f), int64(len(f)), func(r threadlog.ScanRecord) bool {
		if r.Err != nil {
			w.t.Fatal(r.Err)
		}
		if r.Header.Type == threadlog.TypeThreadTagged {
			v, err := threadlog.DecodeBody(r.Header.Type, r.Header.Encoding, r.Body)
			if err != nil {
				w.t.Fatal(err)
			}
			out = append(out, v.(*threadlog.ThreadTaggedBody).Tags)
		}
		return true
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// tagsEverywhere checks that the thread page, the forum's thread list and
// the thread_tags rows all hold want for id.
func (w *world) tagsEverywhere(t *testing.T, id threadid.ID, want []string) {
	t.Helper()
	if got := page(t, newHandlerFor(w).get(w.viewer("carol"), threadPath(id))).Thread.Tags; !slices.Equal(got, want) {
		t.Errorf("the thread page's tags are %q, want %q", got, want)
	}
	if got := w.listedTags("carol", id.String()); !slices.Equal(got, want) {
		t.Errorf("the thread list's tags are %q, want %q", got, want)
	}
}

// TestRetag: a moderator's retag writes one ThreadTagged holding the whole
// set, folded and sorted, and the answer, the page, the list and a reindex
// agree. An empty set clears the tags.
func TestRetag(t *testing.T) {
	w := newWorld(t)
	id := w.threadIn(10, w.forum, "Raised beds", "alice")
	h := header(t, send(w.chain(), "bob", http.MethodPut, modPath(id, "tags"), retag("Garden", "agent", "garden")))
	want := []string{"agent", "garden"}
	if !slices.Equal(h.Tags, want) {
		t.Errorf("the answer's tags are %q, want %q", h.Tags, want)
	}
	if recs := w.taggedRecords(id); len(recs) != 1 || !slices.Equal(recs[0], want) {
		t.Errorf("the log's ThreadTagged records are %q, want one of %q", recs, want)
	}
	if hs := w.logHeaders(id); hs[len(hs)-1].Type != threadlog.TypeThreadTagged || hs[len(hs)-1].Actor != uint64(w.users["bob"]) {
		t.Errorf("the last record is %v by %d, want bob's ThreadTagged", hs[len(hs)-1].Type, hs[len(hs)-1].Actor)
	}
	w.tagsEverywhere(t, id, want)

	h = header(t, send(w.chain(), "bob", http.MethodPut, modPath(id, "tags"), retag()))
	if len(h.Tags) != 0 || h.Tags == nil {
		t.Errorf("after clearing, the answer's tags are %#v", h.Tags)
	}
	w.tagsEverywhere(t, id, []string{})
	w.reindex()
	w.tagsEverywhere(t, id, []string{})
	if recs := w.taggedRecords(id); len(recs) != 2 || len(recs[1]) != 0 {
		t.Errorf("the log's ThreadTagged records are %q, want the set then none", recs)
	}
}

// TestRetagWritesFullReplacement: ThreadTagged replaces the whole set, so
// [a b] then [b c] leaves exactly [b c], and the second record holds the
// set, not a change to it.
func TestRetagWritesFullReplacement(t *testing.T) {
	w := newWorld(t)
	id := w.threadIn(11, w.forum, "Replaced", "alice")
	header(t, send(w.chain(), "bob", http.MethodPut, modPath(id, "tags"), retag("a", "b")))
	header(t, send(w.chain(), "bob", http.MethodPut, modPath(id, "tags"), retag("c", "b")))
	if recs := w.taggedRecords(id); len(recs) != 2 || !slices.Equal(recs[1], []string{"b", "c"}) {
		t.Errorf("the ThreadTagged records are %q, want [a b] then [b c]", recs)
	}
	w.tagsEverywhere(t, id, []string{"b", "c"})
	w.reindex()
	w.tagsEverywhere(t, id, []string{"b", "c"})
}

// TestRetagRefusals: in docs/07's order, each refusal writes nothing to the
// log or to audit_log.
func TestRetagRefusals(t *testing.T) {
	w, _, lounge, _, vault := forumModWorld(t)
	admin := w.newAdmin()
	id := w.threadIn(12, w.forum, "Refusals", "carol")
	header(t, send(w.chain(), "bob", http.MethodPut, modPath(id, "tags"), retag("meta")))
	elsewhere := w.threadIn(13, lounge, "In the lounge", "alice")
	hidden := w.threadIn(14, vault, "In the vault", "alice")
	h := w.chain()
	for _, c := range []struct {
		name, who string
		id        threadid.ID
		body      string
		want      string
	}{
		{"anonymous", "", id, retag("x"), "unauthenticated"},
		{"a member", "alice", id, retag("x"), "forbidden"},
		{"the thread's author", "carol", id, retag("x"), "forbidden"},
		{"no tags", "bob", id, `{}`, "bad-request"},
		{"no body", "bob", id, ``, "bad-request"},
		{"tags null", "bob", id, `{"tags":null}`, "bad-request"},
		{"an unknown field", "bob", id, `{"tags":[],"why":1}`, "bad-request"},
		{"a thread hidden from the viewer", "gina", hidden, retag("x"), "not-found"},
		{"a moderator of another forum", "gina", elsewhere, retag("x"), "forbidden"},
		{"a tag that is not a string", "bob", id, `{"tags":[1]}`, "validation-failed: tags.0 wrong_type"},
		{"tags that are not a list", "bob", id, `{"tags":"x"}`, "validation-failed: tags wrong_type"},
		{"an invalid tag", "bob", id, retag("a b"), "validation-failed: tags.0 invalid_tag"},
		{"a long tag", "bob", id, retag(strings.Repeat("x", 25)), "validation-failed: tags.0 too_long"},
		{"nine tags", "bob", id, retag("a", "b", "c", "d", "e", "f", "g", "h", "i"), "validation-failed: tags too_many"},
		{"the set it has", "bob", id, retag("meta"), "conflict"},
		{"the set it has, in another case", "gina", id, retag("META", "meta"), "conflict"},
	} {
		w.unchanged(t, c.id, c.name, c.want, func() string {
			rec := send(h, c.who, http.MethodPut, modPath(c.id, "tags"), c.body)
			if code := problemCode(t, rec); code != "validation-failed" {
				return code
			}
			return fieldErrors(t, rec)
		})
	}
	// The service asks for moderate itself, whatever the gate admitted.
	w.unchanged(t, id, "the author through the service", "forbidden", func() string {
		_, err := w.svc.Retag(t.Context(), w.viewer("carol"), id, []string{"x"})
		return string(problem.FromError(err))
	})
	// An archived thread is read-only to moderators and admins alike.
	w.append(id, threadlog.Append{Type: threadlog.TypeThreadArchived, Author: uint64(admin.UserID),
		Body: &threadlog.ThreadArchivedBody{}})
	w.cache.MarkArchived(id)
	for _, who := range []string{"bob", "gina"} {
		w.unchanged(t, id, who+" on an archived thread", "conflict", func() string {
			return problemCode(t, send(h, who, http.MethodPut, modPath(id, "tags"), retag("x")))
		})
	}
	w.unchanged(t, id, "an admin on an archived thread", "conflict", func() string {
		return problemCode(t, w.sendAs(admin, http.MethodPut, modPath(id, "tags"), retag("x")))
	})
}

// TestRetagByGroupModerator: gina, who moderates general through a group,
// retags a thread there, and may set or remove a reserved tag. Once her
// grant is revoked her next retag is forbidden: the decision is perm's on
// each request, not the session's.
func TestRetagByGroupModerator(t *testing.T) {
	w, _, _, den, _ := forumModWorld(t)
	id := w.threadIn(15, w.forum, "Group moderated", "alice")
	h := w.chain()
	if got := header(t, send(h, "gina", http.MethodPut, modPath(id, "tags"), retag("agent", "digest"))).Tags; !slices.Equal(got, []string{"agent", "digest"}) {
		t.Errorf("gina's retag answered tags %q", got)
	}
	if got := header(t, send(h, "gina", http.MethodPut, modPath(id, "tags"), retag("digest"))).Tags; !slices.Equal(got, []string{"digest"}) {
		t.Errorf("gina's second retag answered tags %q", got)
	}
	mods := w.groupID(domain.GroupModerators)
	if err := perm.New(w.db).Revoke(t.Context(), audit.By{}, w.forum, mods, domain.PermModerate); err != nil {
		t.Fatal(err)
	}
	_ = den // gina still moderates den, so the gate admits her and the service refuses.
	w.unchanged(t, id, "gina after the revoke", "forbidden", func() string {
		return problemCode(t, send(h, "gina", http.MethodPut, modPath(id, "tags"), retag("x")))
	})
}

// TestRetagInArchivedForum: an archived forum is read-only to members, and
// its moderators may still retag in it, as they may lock.
func TestRetagInArchivedForum(t *testing.T) {
	w := newWorld(t)
	id := w.threadIn(16, w.forum, "Kept", "alice")
	w.archive()
	if got := header(t, send(w.chain(), "bob", http.MethodPut, modPath(id, "tags"), retag("history"))).Tags; !slices.Equal(got, []string{"history"}) {
		t.Errorf("bob's retag in an archived forum answered tags %q", got)
	}
}

// TestRetagDecidesAfterAMove: a move into lounge, which gina does not
// moderate, lands between her retag's decision and its append. The append
// fails on the log's length, the retag is decided again against lounge, and
// it is forbidden and writes nothing of hers; so is a retag to the set the
// thread already has. TestAMoveLandingMidDecisionIsJudged runs the same
// case for every thread action.
func TestRetagDecidesAfterAMove(t *testing.T) {
	for _, a := range threadActions {
		if a.action != "tags" {
			continue
		}
		for _, noop := range []bool{false, true} {
			t.Run(fmt.Sprint("noop ", noop), func(t *testing.T) { moveMidDecision(t, a, noop) })
		}
	}
}

// TestRetagAudited: every retag writes one thread.retagged row naming the
// moderator, with no tag text. Tags set on create write none.
func TestRetagAudited(t *testing.T) {
	w := newWorld(t)
	res := created[dto.CreatedThread](t, send(w.chain(), "carol", http.MethodPost, threadsPath("general"),
		`{"title":"Tagged by its author","source":"s","tags":["secret-word"]}`))
	if n := len(w.auditLog()); n != 0 {
		t.Errorf("creating a tagged thread wrote %d audit rows", n)
	}
	id, err := threadid.Parse(res.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	header(t, send(w.chain(), "bob", http.MethodPut, modPath(id, "tags"), retag("other-word")))
	header(t, send(w.chain(), "bob", http.MethodPut, modPath(id, "tags"), retag()))
	log := w.auditLog()
	if len(log) != 2 {
		t.Fatalf("%d audit rows, want one per retag", len(log))
	}
	for _, r := range log {
		if r.action != projection.ActionThreadRetagged || r.actor.Int64 != w.users["bob"] || r.typ != "thread" || r.id != id.String() {
			t.Errorf("audit row %+v, want thread.retagged by bob against the thread", r)
		}
		for k, v := range r.details {
			if k != "thread" && k != "log_offset" {
				t.Errorf("the row carries %s = %v", k, v)
			}
			if s, ok := v.(string); ok && strings.Contains(s, "word") {
				t.Errorf("the row carries tag text: %s = %q", k, s)
			}
		}
	}
	// A reindex replays the records and adds no row.
	w.reindex()
	if n := len(w.auditLog()); n != 2 {
		t.Errorf("after a reindex, %d audit rows", n)
	}
}

// TestRetagMovesTheResponseCacheKey: the thread page cached before a retag
// is not served after it, for any viewer class, because the log length is
// in the key.
func TestRetagMovesTheResponseCacheKey(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	path := threadPath(id)
	classes := []string{"", "carol", "bob"}
	for _, who := range classes {
		w.same(w.viewer(who), path)
	}
	h := header(t, w.sendAs(w.viewer("bob"), http.MethodPut, modPath(id, "tags"), retag("meta", "history")))
	before := w.svc.Responses.Stats()
	for _, who := range classes {
		rec := w.get(w.viewer(who), path)
		p := page(t, rec)
		if p.Thread.LogLength != h.LogLength || !slices.Equal(p.Thread.Tags, []string{"history", "meta"}) {
			t.Errorf("after the retag %q got the header %+v", who, p.Thread)
		}
		if tag := rec.Header().Get("ETag"); !strings.HasPrefix(tag, fmt.Sprintf(`"%s-%d-`, id, h.LogLength)) {
			t.Errorf("after the retag %q got ETag %s, not one under the new log length %d", who, tag, h.LogLength)
		}
	}
	if s := w.svc.Responses.Stats(); s.Misses != before.Misses+3 || s.Hits != before.Hits {
		t.Errorf("after the retag the three classes' pages were not fresh draws: %+v then %+v", before, s)
	}
}

// tagThread makes a thread in forum with one post and tags, by alice.
func (w *world) tagThread(seq uint64, forum int64, title string, tags ...string) threadid.ID {
	w.t.Helper()
	id := w.threadIn(seq, forum, title, "alice")
	if len(tags) > 0 {
		w.append(id, threadlog.Append{Type: threadlog.TypeThreadTagged, Author: uint64(w.users["bob"]),
			Body: &threadlog.ThreadTaggedBody{Tags: tags}})
	}
	return id
}

// listPage is one page of the general forum's list as who reads it, with
// query, or the problem code it answered.
func (w *world) listPage(who, query string) (dto.ThreadList, string) {
	w.t.Helper()
	rec := w.get(w.viewer(who), threadsPath("general")+query)
	if rec.Code != http.StatusOK {
		return dto.ThreadList{}, problemCode(w.t, rec)
	}
	return decode[dto.ThreadList](w.t, rec), ""
}

// TestThreadListTagFilter: ?tag= lists only the forum's threads carrying
// the folded tag, pins included, pages through them once each with the tag
// bound into the cursor, and refuses a tag that could not be one.
func TestThreadListTagFilter(t *testing.T) {
	w := newWorld(t)
	var garden []string
	for i := range 30 {
		garden = append(garden, w.tagThread(uint64(100+i), w.forum, fmt.Sprintf("Garden %d", i), "garden", "misc").String())
		w.tagThread(uint64(200+i), w.forum, fmt.Sprintf("Other %d", i), "misc")
	}
	untaggedPin := w.tagThread(300, w.forum, "Rules", "meta")
	taggedPin := w.tagThread(301, w.forum, "Garden rules", "garden")
	garden = append(garden, taggedPin.String())
	for _, id := range []threadid.ID{untaggedPin, taggedPin} {
		header(t, send(w.chain(), "bob", http.MethodPost, modPath(id, "pin"), `{"scope":"forum"}`))
	}

	for _, q := range []string{"?tag=garden", "?tag=Garden", "?tag=%20GARDEN%20"} {
		var got []string
		cursor := ""
		for page := 0; ; page++ {
			l, code := w.listPage("carol", q+"&cursor="+url.QueryEscape(cursor))
			if code != "" {
				t.Fatalf("%s page %d: %s", q, page, code)
			}
			for i, r := range l.Threads {
				if !slices.Contains(r.Tags, "garden") {
					t.Errorf("%s: %s %q does not carry garden", q, r.ID, r.Title)
				}
				if page == 0 && i == 0 && (r.ID != taggedPin.String() || r.Pinned == nil) {
					t.Errorf("%s: the first row is %s %q, want the tagged pin", q, r.ID, r.Title)
				}
				if r.ID == untaggedPin.String() {
					t.Errorf("%s: the untagged pin is listed", q)
				}
				got = append(got, r.ID)
			}
			if l.NextCursor == "" {
				break
			}
			cursor = l.NextCursor
			if page == 0 {
				// The cursor is bound to its tag: another tag, or none, refuses it.
				for _, other := range []string{"?tag=misc&", "?"} {
					if _, code := w.listPage("carol", other+"cursor="+url.QueryEscape(cursor)); code != "bad-request" {
						t.Errorf("a %s cursor with %q answered %q, want bad-request", q, other, code)
					}
				}
			}
		}
		slices.Sort(got)
		want := slices.Clone(garden)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s listed %d threads, want the %d carrying garden, each once", q, len(got), len(want))
		}
	}

	// The unfiltered list still has everything, both pins first.
	l, _ := w.listPage("carol", "")
	if len(l.Threads) < 2 || l.Threads[0].Pinned == nil || l.Threads[1].Pinned == nil {
		t.Errorf("the unfiltered list does not open with both pins")
	}
	// A tag nothing carries is an empty list, not an error.
	if l, code := w.listPage("carol", "?tag=nothing-here"); code != "" || len(l.Threads) != 0 || l.NextCursor != "" {
		t.Errorf("an unused tag: %q, %d threads", code, len(l.Threads))
	}
	for _, bad := range []string{"a_b", "-a", strings.Repeat("x", 25), "caf%C3%A9", "%E2%84%AA"} {
		if _, code := w.listPage("carol", "?tag="+bad); code != "bad-request" {
			t.Errorf("?tag=%s answered %q, want bad-request", bad, code)
		}
	}
}

// TestTagFilterNeverWidensAccess: a filtered list answers a hidden or
// unreadable forum exactly as the unfiltered list does, and never lists a
// global pin from a forum the viewer cannot read, whatever it is tagged.
func TestTagFilterNeverWidensAccess(t *testing.T) {
	w := newWorld(t)
	vault := w.forumAt("vault", domain.VisibilityRestricted, false)
	members := w.forumAt("members", domain.VisibilityMembers, false)
	secret := w.tagThread(400, vault, "Vault notes", "garden")
	w.append(secret, threadlog.Append{Type: threadlog.TypeThreadPinned, Author: uint64(w.users["bob"]),
		Body: &threadlog.ThreadPinnedBody{Scope: "global"}})
	w.tagThread(401, w.forum, "Visible", "garden")
	w.tagThread(402, members, "Members only", "garden")

	l, code := w.listPage("carol", "?tag=garden")
	if code != "" {
		t.Fatal(code)
	}
	for _, r := range l.Threads {
		if r.ID == secret.String() {
			t.Errorf("the vault's global pin is listed to carol under ?tag=garden")
		}
	}
	if len(l.Threads) != 1 {
		t.Errorf("carol's filtered list has %d threads, want the one in general", len(l.Threads))
	}
	for _, c := range []struct{ who, slug string }{
		{"carol", "vault"}, {"", "vault"}, {"", "members"}, {"carol", "nowhere"},
	} {
		path := "/api/v1/forums/" + c.slug + "/threads"
		plain := problemCode(t, w.get(w.viewer(c.who), path))
		filtered := problemCode(t, w.get(w.viewer(c.who), path+"?tag=garden"))
		if plain != filtered {
			t.Errorf("%q in %s: the unfiltered list answers %s and the filtered %s", c.who, c.slug, plain, filtered)
		}
	}
	if got := problemCode(t, w.get(domain.Anonymous(), "/api/v1/forums/members/threads?tag=garden")); got != "unauthenticated" {
		t.Errorf("an anonymous viewer's filtered members list: %s", got)
	}
	if got := problemCode(t, w.get(w.viewer("carol"), "/api/v1/forums/vault/threads?tag=garden")); got != "not-found" {
		t.Errorf("carol's filtered vault list: %s", got)
	}
}
