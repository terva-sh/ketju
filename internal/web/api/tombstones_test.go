package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for tombstones: who is served a removed post's content, and what
// every other viewer gets in its place (TKT-01M29CKRJ9, docs/08
// "Tombstones").

// selfSecret is the body of the post its author deletes in removedThread.
const selfSecret = "a post its author took down"

// removedThread is seedHidden, then:
//
//	#6 alice's, which she deleted herself
//	#7 carol's reply to #5, the post bob hid
//	#8 carol's reply to #6
//	#9 carol's quote of #5
//
// So alice is the author of one post a moderator removed and one she removed
// herself, and carol is a member who wrote neither.
func (w *world) removedThread() threadid.ID {
	w.t.Helper()
	id, _ := w.seedHidden()
	w.post(id, "alice", selfSecret, 0)
	w.del(id, 6, "alice")
	w.post(id, "carol", "an answer to the hidden post", 5)
	w.post(id, "carol", "an answer to the deleted post", 6)
	w.post(id, "carol", "> [quote=#5]\n> "+hiddenSecret+"\n\nquoting it\n", 0)
	return id
}

// removedIs checks p is ordinal n, removed by who, with or without its body.
func removedIs(t *testing.T, where string, p dto.Post, n int, who string, body string) {
	t.Helper()
	if p.Ordinal != n || !p.Deleted || p.Removal == nil || *p.Removal != who {
		t.Errorf("%s: #%d is %+v: want deleted, removal %q", where, n, p, who)
		return
	}
	switch {
	case body == "" && p.HTML != "":
		t.Errorf("%s: #%d carries html %q, want none", where, n, p.HTML)
	case body != "" && !strings.Contains(p.HTML, body):
		t.Errorf("%s: #%d has html %q, want its body", where, n, p.HTML)
	}
}

// historyOf reads a post's history as who, returning the status and, on a
// 200, the body.
func (w *world) historyOf(who string, id threadid.ID, n int) (int, dto.PostHistory) {
	w.t.Helper()
	rec := w.get(w.viewer(who), postPathN(id, n)+"/history")
	if rec.Code != http.StatusOK {
		return rec.Code, dto.PostHistory{}
	}
	return rec.Code, decode[dto.PostHistory](w.t, rec)
}

func (w *world) publicEditHistory(on bool) {
	w.t.Helper()
	if err := settings.Set(w.t.Context(), w.db, "public_edit_history", json.RawMessage(fmt.Sprint(on)), 0, time.UnixMilli(1)); err != nil {
		w.t.Fatal(err)
	}
}

// userPost is who's view of ordinal n of id in alice's history, and whether
// it is there.
func (w *world) userPost(who string, id threadid.ID, n int) (dto.Post, bool) {
	w.t.Helper()
	h := decode[dto.UserPosts](w.t, w.get(w.viewer(who), "/api/v1/users/alice/posts"))
	for _, up := range h.Posts {
		if up.Thread.ID == id.String() && up.Post.Ordinal == n {
			return up.Post, true
		}
	}
	return dto.Post{}, false
}

// TestTombstoneKeepsOrdinalAndPermalink is AC6 and AC1: a removed post keeps
// its ordinal, so ?ordinal=N serves the page holding it with the post as a
// tombstone, and the reply to it keeps its reply_to and is listed by the
// removed post's replies.
func TestTombstoneKeepsOrdinalAndPermalink(t *testing.T) {
	w := newWorld(t)
	id := w.longThread(1, 30)
	w.del(id, 27, "alice")
	w.post(id, "carol", "an answer to 27", 27)

	p := page(t, w.get(w.viewer("carol"), "/api/v1/threads/"+id.String()+"?ordinal=27"))
	if p.Page.Number != 2 || p.Thread.PostCount != 31 {
		t.Fatalf("?ordinal=27 served page %d of a thread of %d", p.Page.Number, p.Thread.PostCount)
	}
	for i, post := range p.Posts {
		if post.Ordinal != 26+i {
			t.Fatalf("page 2 holds %d at %d: the numbering moved", post.Ordinal, i)
		}
	}
	removedIs(t, "the permalink's page", p.Posts[1], 27, dto.RemovalAuthor, "")
	if r := p.Posts[1]; r.ReplyCount != 1 {
		t.Errorf("#27 has reply_count %d, want 1", r.ReplyCount)
	}
	if reply := p.Posts[5]; reply.Ordinal != 31 || reply.ReplyTo == nil || *reply.ReplyTo != 27 {
		t.Errorf("the reply to 27 is %+v: want reply_to 27", reply)
	}

	replies := decode[dto.PostReplies](t, w.get(w.viewer("carol"), postPathN(id, 27)+"/replies"))
	if len(replies.Posts) != 1 || replies.Posts[0].Ordinal != 31 || replies.Posts[0].HTML == "" {
		t.Errorf("the replies of the removed #27: %+v", replies)
	}
}

// TestMembersNeverReceiveRemovedContent is AC2: for a post its author
// deleted and one a moderator hid, an anonymous viewer and a member who wrote
// neither get no html and no revisions from the page, the single post, the
// author's post history and the post's history. A moderator gets the html
// from each. A quote of the hidden post is a tombstone to all three.
func TestMembersNeverReceiveRemovedContent(t *testing.T) {
	w := newWorld(t)
	id := w.removedThread()
	base := "/api/v1/threads/" + id.String()
	removed := []struct {
		n       int
		removal string
		body    string
	}{{5, dto.RemovalModerator, hiddenSecret}, {6, dto.RemovalAuthor, selfSecret}}

	for _, who := range []string{"", "carol", "bob"} {
		moderator := who == "bob"
		p := page(t, w.get(w.viewer(who), base))
		for _, r := range removed {
			want := ""
			if moderator {
				want = r.body
			}
			removedIs(t, fmt.Sprintf("%q's page", who), p.Posts[r.n-1], r.n, r.removal, want)
			removedIs(t, fmt.Sprintf("%q's single post", who), decode[dto.Post](t, w.get(w.viewer(who), postPathN(id, r.n))),
				r.n, r.removal, want)
			code, h := w.historyOf(who, id, r.n)
			switch {
			case who == "":
				if code != http.StatusUnauthorized {
					t.Errorf("anonymous history of #%d: %d", r.n, code)
				}
			case !h.Deleted || (len(h.Revisions) > 0) != moderator:
				t.Errorf("%q's history of #%d: %+v", who, r.n, h)
			}
			if moderator && (len(h.Revisions) != 1 || h.Revisions[0].Source != r.body) {
				t.Errorf("the moderator's history of #%d: %+v", r.n, h.Revisions)
			}
			// Nothing in the page as written names the body, quotes included.
			if !moderator && strings.Contains(fmt.Sprint(p.Posts), r.body) {
				t.Errorf("%q's page carries the body of #%d somewhere", who, r.n)
			}
		}
		if q := p.Posts[8]; q.Ordinal != 9 || strings.Contains(q.HTML, hiddenSecret) || !strings.Contains(q.HTML, "quote-deleted") {
			t.Errorf("%q: the quote of the hidden post rendered as %q", who, q.HTML)
		}

		// The replies endpoints are TestRemovedRepliesFromTheExpander's.
		if who == "" {
			if rec := w.get(w.viewer(who), "/api/v1/users/alice/posts"); rec.Code != http.StatusUnauthorized {
				t.Errorf("anonymous user posts: %d", rec.Code)
			}
			continue
		}
		for _, r := range removed {
			up, ok := w.userPost(who, id, r.n)
			switch {
			case moderator && !ok:
				t.Errorf("the moderator's view of alice's posts leaves out #%d", r.n)
			case moderator:
				removedIs(t, "the moderator's user posts", up, r.n, r.removal, r.body)
			case ok:
				t.Errorf("%q sees #%d in alice's posts: %+v", who, r.n, up)
			}
		}
	}
}

// TestRemovedRepliesFromTheExpander is AC1 and AC2 for replies: a removed
// post listed among its parent's replies is a tombstone to a member and
// carries its body to a moderator, and the replies under a removed post are
// listed with their reply_to.
func TestRemovedRepliesFromTheExpander(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.post(id, "alice", hiddenSecret, 1)
	w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["bob"]), Ordinal: 5,
		Body: &threadlog.DeleteBody{ByModerator: true}})
	w.post(id, "carol", "an answer to the hidden post", 5)

	for who, want := range map[string]string{"": "", "carol": "", "alice": hiddenSecret, "bob": hiddenSecret} {
		replies := decode[dto.PostReplies](t, w.get(w.viewer(who), postPathN(id, 1)+"/replies"))
		last := replies.Posts[len(replies.Posts)-1]
		removedIs(t, fmt.Sprintf("%q's replies of #1", who), last, 5, dto.RemovalModerator, want)

		under := decode[dto.PostReplies](t, w.get(w.viewer(who), postPathN(id, 5)+"/replies"))
		if len(under.Posts) != 1 || under.Posts[0].Ordinal != 6 || under.Posts[0].ReplyTo == nil || *under.Posts[0].ReplyTo != 5 {
			t.Errorf("%q: the replies under the hidden #5: %+v", who, under)
		}
	}
}

// TestAuthorSeesOwnHiddenPost is AC3: the author of a post a moderator hid
// gets its html, marked removed by a moderator, on the page, the single
// post, their user posts and its history. Their own deleted post stays a
// tombstone without html everywhere, its history included.
func TestAuthorSeesOwnHiddenPost(t *testing.T) {
	w := newWorld(t)
	id := w.removedThread()

	p := page(t, w.get(w.viewer("alice"), "/api/v1/threads/"+id.String()))
	removedIs(t, "alice's page", p.Posts[4], 5, dto.RemovalModerator, hiddenSecret)
	removedIs(t, "alice's page", p.Posts[5], 6, dto.RemovalAuthor, "")
	if strings.Contains(fmt.Sprint(p.Posts), selfSecret) {
		t.Error("alice's page carries the body of the post she deleted")
	}
	// Bob's own deleted #3 is nobody's but bob's.
	removedIs(t, "alice's page", p.Posts[2], 3, dto.RemovalAuthor, "")

	removedIs(t, "alice's single post", decode[dto.Post](t, w.get(w.viewer("alice"), postPathN(id, 5))), 5, dto.RemovalModerator, hiddenSecret)
	removedIs(t, "alice's single post", decode[dto.Post](t, w.get(w.viewer("alice"), postPathN(id, 6))), 6, dto.RemovalAuthor, "")

	if up, ok := w.userPost("alice", id, 5); !ok {
		t.Error("alice's posts leave out the one bob hid")
	} else {
		removedIs(t, "alice's user posts", up, 5, dto.RemovalModerator, hiddenSecret)
	}
	if up, ok := w.userPost("alice", id, 6); !ok {
		t.Error("alice's posts leave out the one she deleted")
	} else {
		removedIs(t, "alice's user posts", up, 6, dto.RemovalAuthor, "")
	}

	if code, h := w.historyOf("alice", id, 5); code != http.StatusOK || !h.Deleted || len(h.Revisions) != 1 || h.Revisions[0].Source != hiddenSecret {
		t.Errorf("alice's history of the post bob hid: %d %+v", code, h)
	}
	if code, h := w.historyOf("alice", id, 6); code != http.StatusOK || !h.Deleted || len(h.Revisions) != 0 {
		t.Errorf("alice's history of the post she deleted: %d %+v", code, h)
	}
}

// TestHiddenPostsAuthorReadsHistoryWithSettingOff is AC8: with
// public_edit_history off, the author of a post a moderator hid reads its
// history, and nothing else is admitted by that: their own visible post and
// their own deleted one are forbidden, another member is forbidden on the
// hidden post, and once a moderator restores it the author is forbidden on
// it too.
func TestHiddenPostsAuthorReadsHistoryWithSettingOff(t *testing.T) {
	w := newWorld(t)
	id := w.removedThread()
	w.publicEditHistory(false)

	if code, h := w.historyOf("alice", id, 5); code != http.StatusOK || len(h.Revisions) != 1 || h.Revisions[0].Source != hiddenSecret {
		t.Errorf("alice's history of the post bob hid, with the setting off: %d %+v", code, h)
	}
	for _, c := range []struct {
		who string
		n   int
	}{{"alice", 1}, {"alice", 6}, {"carol", 5}, {"carol", 4}, {"alice", 99}} {
		if code, _ := w.historyOf(c.who, id, c.n); code != http.StatusForbidden {
			t.Errorf("%s's history of #%d with the setting off: %d, want 403", c.who, c.n, code)
		}
	}
	if code, _ := w.historyOf("bob", id, 5); code != http.StatusOK {
		t.Errorf("the moderator's history with the setting off: %d", code)
	}

	changed(t, w.sendAs(w.viewer("bob"), http.MethodPost, postPathN(id, 5)+"/restore", ""))
	if code, _ := w.historyOf("alice", id, 5); code != http.StatusForbidden {
		t.Errorf("alice's history of her restored post, with the setting off: %d, want 403", code)
	}
}

// TestVisiblePostsUnchanged is AC7: with removed posts on the page, members
// and anonymous viewers still get every visible post's html, and a visible
// post's history follows public_edit_history as it did before.
func TestVisiblePostsUnchanged(t *testing.T) {
	w := newWorld(t)
	id := w.removedThread()
	for _, who := range []string{"", "carol", "alice"} {
		p := page(t, w.get(w.viewer(who), "/api/v1/threads/"+id.String()))
		for _, post := range p.Posts {
			if post.Deleted {
				continue
			}
			if post.HTML == "" || post.Removal != nil {
				t.Errorf("%q got visible #%d as %+v", who, post.Ordinal, post)
			}
		}
	}
	for on, want := range map[bool]int{true: http.StatusOK, false: http.StatusForbidden} {
		w.publicEditHistory(on)
		for _, who := range []string{"carol", "alice"} {
			code, h := w.historyOf(who, id, 1)
			if code != want {
				t.Errorf("%s's history of visible #1 with the setting %v: %d, want %d", who, on, code, want)
			}
			if code == http.StatusOK && (h.Deleted || len(h.Revisions) != 2) {
				t.Errorf("%s's history of visible #1: %+v", who, h)
			}
		}
		if code, _ := w.historyOf("", id, 1); code != http.StatusUnauthorized {
			t.Errorf("anonymous history with the setting %v: %d", on, code)
		}
		if code, _ := w.historyOf("bob", id, 1); code != http.StatusOK {
			t.Errorf("the moderator's history with the setting %v: %d", on, code)
		}
	}
}
