package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for PATCH and DELETE /threads/{id}/posts/{ordinal} and POST
// …/restore (TKT-01M29CA9BP). The seeded thread is #1 alice (edited), #2
// scribe replying to #1, #3 bob (deleted), #4 carol replying to #1 and
// mentioning alice.

func postPathN(id threadid.ID, n int) string { return fmt.Sprintf("%s/%d", postsPath(id), n) }

// changed decodes a 200 write answer strictly.
func changed(t *testing.T, rec *httptest.ResponseRecorder) dto.CreatedPost {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control %q, want no-store", cc)
	}
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	var v dto.CreatedPost
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decoding dto.CreatedPost: %v\n%s", err, rec.Body)
	}
	return v
}

// entry is the index entry for one post of id.
func (w *world) entry(id threadid.ID, ordinal uint32) threadlog.IndexEntry {
	w.t.Helper()
	var e threadlog.IndexEntry
	if err := w.cache.With(id, func(th *threadcache.Thread) error {
		var err error
		e, err = th.Index.Entry(ordinal)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return e
}

// TestEditPost is AC1 through the API: the edit is appended, the revision
// moves to 2, the entry points at it, the reply link and the new mentions
// come from the render, the reason reaches history, and the answer is the
// post a later read serves. The answer's shape is fixed by a golden.
func TestEditPost(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	before := w.logState(id)
	res := changed(t, send(w.chain(), "carol", http.MethodPatch, postPathN(id, 4),
		`{"source":"Agreed with @bob, on reflection.","reason":"wrong person"}`))

	if res.Ordinal != 4 || res.Revision != 2 || res.Post.Revision != 2 || res.Post.EditedAt == nil ||
		res.Post.ReplyTo == nil || *res.Post.ReplyTo != 1 || res.Post.Deleted {
		t.Fatalf("edit answered %+v", res)
	}
	after := w.logState(id)
	if after.size <= before.size || after.posts != before.posts || after.events != before.events+1 {
		t.Errorf("log %+v then %+v: want one appended record, one event, no new post", before, after)
	}
	e := w.entry(id, 4)
	if e.Revision != 2 || int64(e.Offset) != before.size || e.ReplyTo != 1 || !e.Flags.Has(threadlog.EntryEdited) {
		t.Errorf("entry %+v: want revision 2 at offset %d replying to 1", e, before.size)
	}

	cold := newHandlerFor(w)
	if got, want := cold.get(w.viewer("carol"), postPathN(id, 4)).Body.String(), renderJSON(t, res.Post); got != want {
		t.Errorf("GET the post\n got %s\nwant %s", got, want)
	}
	var h dto.PostHistory
	if err := json.Unmarshal(cold.get(w.viewer("carol"), postPathN(id, 4)+"/history").Body.Bytes(), &h); err != nil {
		t.Fatal(err)
	}
	if len(h.Revisions) != 2 || h.Revisions[1].Reason != "wrong person" || h.Revisions[1].Source != "Agreed with @bob, on reflection." {
		t.Errorf("history %+v", h)
	}

	// The projection takes the edit: reply_to kept, mentions replaced.
	if _, err := w.proj.ProjectThread(t.Context(), id, res.LogLength); err != nil {
		t.Fatal(err)
	}
	var replyTo sql.NullInt64
	var mentioned []int64
	if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		if err := tx.QueryRowContext(t.Context(), `SELECT reply_to FROM post_meta WHERE thread_id = ? AND ordinal = 4`,
			int64(id)).Scan(&replyTo); err != nil {
			return err
		}
		rows, err := tx.QueryContext(t.Context(), `SELECT mentioned_user_id FROM mentions WHERE thread_id = ? AND ordinal = 4`, int64(id))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var u int64
			if err := rows.Scan(&u); err != nil {
				return err
			}
			mentioned = append(mentioned, u)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	if replyTo != (sql.NullInt64{Int64: 1, Valid: true}) {
		t.Errorf("post_meta.reply_to %+v after the edit, want 1", replyTo)
	}
	if len(mentioned) != 1 || mentioned[0] != w.users["bob"] {
		t.Errorf("mentions %v after the edit, want only bob (%d)", mentioned, w.users["bob"])
	}

	got, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "golden", "edit.member.json")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("the edit answer differs from %s:\n%s", path, got)
	}
}

// TestDeleteAndRestorePost is AC2, AC3 and AC4 through the API: a delete
// makes the post a tombstone with its bytes left alone, an edit or a second
// delete of it is conflict and writes nothing, and a restore brings it back
// at its current revision. A restore of a visible post is conflict too.
func TestDeleteAndRestorePost(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	h := w.chain()
	changed(t, send(h, "carol", http.MethodPatch, postPathN(id, 4), `{"source":"revision two"}`))
	orig := w.entry(id, 4)

	del := changed(t, send(h, "carol", http.MethodDelete, postPathN(id, 4), ""))
	if !del.Post.Deleted || del.Post.HTML != "" || del.Revision != 2 || del.Post.Revision != 2 {
		t.Errorf("delete answered %+v, want a tombstone at revision 2", del)
	}
	if e := w.entry(id, 4); !e.Flags.Has(threadlog.EntryDeleted) || e.Offset != orig.Offset || e.Revision != orig.Revision {
		t.Errorf("entry after delete %+v, before %+v", e, orig)
	}
	if p := decodePost(t, w.get(w.viewer("alice"), postPathN(id, 4))); !p.Deleted || p.HTML != "" {
		t.Errorf("another member reads the deleted post as %+v", p)
	}

	for _, c := range []struct{ name, method, path, body string }{
		{"an edit of the deleted post", http.MethodPatch, postPathN(id, 4), `{"source":"back from the dead"}`},
		{"a second delete", http.MethodDelete, postPathN(id, 4), `{"reason":"again"}`},
	} {
		before := w.logState(id)
		if rec := send(h, "carol", c.method, c.path, c.body); problemCode(t, rec) != "conflict" {
			t.Errorf("%s: %d %s, want conflict", c.name, rec.Code, rec.Body)
		}
		if after := w.logState(id); after != before {
			t.Errorf("%s changed the log: %+v then %+v", c.name, before, after)
		}
	}

	res := changed(t, send(h, "carol", http.MethodPost, postPathN(id, 4)+"/restore", `{"reason":"meant it"}`))
	if res.Post.Deleted || res.Revision != 2 || res.Post.Revision != 2 {
		t.Errorf("restore answered %+v, want revision 2 visible", res)
	}
	if got, want := newHandlerFor(w).get(w.viewer("alice"), postPathN(id, 4)).Body.String(), renderJSON(t, res.Post); got != want {
		t.Errorf("GET the restored post\n got %s\nwant %s", got, want)
	}
	if e := w.entry(id, 4); e.Flags.Has(threadlog.EntryDeleted) {
		t.Errorf("entry after restore %+v", e)
	}

	before := w.logState(id)
	if rec := send(h, "carol", http.MethodPost, postPathN(id, 4)+"/restore", ""); problemCode(t, rec) != "conflict" {
		t.Errorf("a restore of a visible post: %d %s, want conflict", rec.Code, rec.Body)
	}
	if after := w.logState(id); after != before {
		t.Errorf("a refused restore changed the log: %+v then %+v", before, after)
	}
}

// TestChangePostRefusals: who may act, and on what. Only the author, and
// not a moderator yet (TKT-01M29CRJ5J); a member only to edit what they may
// reply to; a locked thread refuses a member's edit but not their delete; an
// archived one refuses everything. Every refusal leaves the log alone.
func TestChangePostRefusals(t *testing.T) {
	type attempt struct{ who, method, path, body, want string }
	check := func(t *testing.T, w *world, id threadid.ID, h http.Handler, cases []attempt) {
		t.Helper()
		for _, c := range cases {
			before := w.logState(id)
			rec := send(h, c.who, c.method, c.path, c.body)
			if c.want == "ok" {
				if rec.Code != http.StatusOK {
					t.Errorf("%s %s %s: %d %s, want 200", c.who, c.method, c.path, rec.Code, rec.Body)
				}
				continue
			}
			if got := problemCode(t, rec); got != c.want {
				t.Errorf("%s %s %s %s: %s (%d), want %s", c.who, c.method, c.path, c.body, got, rec.Code, c.want)
			}
			if after := w.logState(id); after != before {
				t.Errorf("%s %s %s: a refusal changed the log: %+v then %+v", c.who, c.method, c.path, before, after)
			}
		}
	}

	t.Run("who", func(t *testing.T) {
		w := newWorld(t)
		id, _ := w.seed()
		p4 := postPathN(id, 4)
		check(t, w, id, w.chain(), []attempt{
			{"", http.MethodPatch, p4, `{"source":"x"}`, "unauthenticated"},
			{"pre-user", http.MethodPatch, p4, `{"source":"x"}`, "forbidden"},
			{"", http.MethodDelete, p4, "", "unauthenticated"},
			{"pre-user", http.MethodPost, p4 + "/restore", "", "forbidden"},
			{"alice", http.MethodPatch, p4, `{"source":"x"}`, "forbidden"},
			{"alice", http.MethodDelete, p4, "", "forbidden"},
			{"bob", http.MethodPatch, p4, `{"source":"x"}`, "forbidden"},
			{"bob", http.MethodDelete, p4, "", "forbidden"},
			{"alice", http.MethodPost, postPathN(id, 3) + "/restore", "", "forbidden"},
			{"carol", http.MethodPatch, postPathN(id, 99), `{"source":"x"}`, "not-found"},
			{"carol", http.MethodDelete, postPathN(id, 0), "", "bad-request"},
			{"carol", http.MethodPatch, p4, `{"reason":"no source"}`, "bad-request"},
			{"carol", http.MethodPatch, p4, `{"source":"x","reply_to":2}`, "bad-request"},
			{"carol", http.MethodPatch, p4, `{"source":"  \n"}`, "validation-failed"},
			{"carol", http.MethodDelete, p4, `{"reason":1}`, "bad-request"},
			{"carol", http.MethodDelete, p4, `{} {}`, "bad-request"},
		})
	})

	t.Run("reply permission", func(t *testing.T) {
		w := newWorld(t)
		w.write(func(q *dbq.Queries) error {
			return q.GrantPermission(t.Context(), dbq.GrantPermissionParams{
				ForumID: sql.NullInt64{Int64: w.forum, Valid: true}, GroupID: 2, Permission: string(domain.PermRead), CreatedAt: 1})
		})
		id, _ := w.seed()
		p4 := postPathN(id, 4)
		check(t, w, id, w.chain(), []attempt{
			{"carol", http.MethodPatch, p4, `{"source":"x"}`, "forbidden"},
			{"carol", http.MethodDelete, p4, "", "ok"},
			{"carol", http.MethodPost, p4 + "/restore", "", "ok"},
		})
	})

	t.Run("locked", func(t *testing.T) {
		w := newWorld(t)
		id, _ := w.seed()
		w.append(id, threadlog.Append{Type: threadlog.TypeThreadLocked, Author: uint64(w.users["bob"]),
			Body: &threadlog.ThreadLockedBody{}})
		p4 := postPathN(id, 4)
		check(t, w, id, w.chain(), []attempt{
			{"carol", http.MethodPatch, p4, `{"source":"x"}`, "conflict"},
			{"carol", http.MethodDelete, p4, "", "ok"},
			{"carol", http.MethodPost, p4 + "/restore", "", "ok"},
			// A moderator passes the lock for their own post.
			{"bob", http.MethodPost, postPathN(id, 3) + "/restore", "", "ok"},
			{"bob", http.MethodPatch, postPathN(id, 3), `{"source":"moderator's own"}`, "ok"},
		})
	})

	t.Run("archived", func(t *testing.T) {
		w := newWorld(t)
		id, _ := w.seed()
		w.cache.MarkArchived(id)
		check(t, w, id, w.chain(), []attempt{
			{"carol", http.MethodPatch, postPathN(id, 4), `{"source":"x"}`, "conflict"},
			{"carol", http.MethodDelete, postPathN(id, 4), "", "conflict"},
			{"bob", http.MethodPost, postPathN(id, 3) + "/restore", "", "conflict"},
		})
	})
}

// decodePost decodes a 200 single-post answer.
func decodePost(t *testing.T, rec *httptest.ResponseRecorder) dto.Post {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var p dto.Post
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}
