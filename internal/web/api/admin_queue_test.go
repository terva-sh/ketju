package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// Tests for the moderator queue's routes (TKT-01M29CRJ4E). In the seeded
// thread #4 is carol's; dave reports it. gus is a member granted moderate on
// the forum through a group, so only the moderates gate admits him.

func (w *world) queueWorld() (threadid.ID, http.Handler) {
	w.t.Helper()
	id, _ := w.seed()
	w.addUsers("active", "dave", "gus")
	if err := w.db.Write(w.t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		g, err := q.CreateGroup(w.t.Context(), dbq.CreateGroupParams{Name: "Forum mods", Slug: "forum-mods", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(w.t.Context(), `INSERT INTO group_members (group_id, user_id, source, added_at)
			VALUES (?, ?, 'admin', 1)`, g, w.users["gus"]); err != nil {
			return err
		}
		return q.GrantPermission(w.t.Context(), dbq.GrantPermissionParams{ForumID: sql.NullInt64{Int64: w.forum, Valid: true},
			GroupID: g, Permission: "moderate", CreatedAt: 1})
	}); err != nil {
		w.t.Fatal(err)
	}
	return id, w.chain()
}

func queuePostPath(id threadid.ID, ordinal int) string {
	return "/api/v1/admin/queue/posts/" + id.String() + "/" + strconv.Itoa(ordinal)
}

// TestForumScopedModeratorUsesQueue is AC6 through the routes: a member
// granted moderate on one forum through a group passes the moderates gate,
// lists the forum's item, and resolves it; a plain member, a pre-user and an
// anonymous viewer are refused before anything is read. Both answers are
// no-store, and a second action on the item is conflict.
func TestForumScopedModeratorUsesQueue(t *testing.T) {
	w := newWorld(t)
	id, h := w.queueWorld()
	if rec := send(h, "dave", http.MethodPost, flagsPath(id), `{"ordinal":4,"reason":"spam"}`); rec.Code != http.StatusOK {
		t.Fatalf("dave's flag: %d %s", rec.Code, rec.Body)
	}
	for _, c := range []struct {
		who  string
		want int
	}{{"", http.StatusUnauthorized}, {"pre-user", http.StatusForbidden}, {"carol", http.StatusForbidden}} {
		if rec := send(h, c.who, http.MethodGet, "/api/v1/admin/queue", ""); rec.Code != c.want {
			t.Errorf("%q's queue: %d, want %d", c.who, rec.Code, c.want)
		}
		if rec := send(h, c.who, http.MethodPost, queuePostPath(id, 4), `{"action":"dismiss"}`); rec.Code != c.want {
			t.Errorf("%q's dismissal: %d, want %d", c.who, rec.Code, c.want)
		}
	}

	rec := send(h, "gus", http.MethodGet, "/api/v1/admin/queue", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("gus's queue: %d, Cache-Control %q: %s", rec.Code, rec.Header().Get("Cache-Control"), rec.Body)
	}
	var q dto.Queue
	if err := json.Unmarshal(rec.Body.Bytes(), &q); err != nil {
		t.Fatal(err)
	}
	if len(q.Items) != 1 || q.Items[0].Flag == nil || q.Items[0].Flag.Ordinal != 4 ||
		len(q.Items[0].Flag.Flags) != 1 || q.Items[0].Flag.Flags[0].Reporter.Handle != "dave" {
		t.Fatalf("gus's queue: %s", rec.Body)
	}

	for _, c := range []struct{ body string }{{`{}`}, {`{"action":"dismiss","extra":1}`}} {
		if rec := send(h, "gus", http.MethodPost, queuePostPath(id, 4), c.body); rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: %d, want 400", c.body, rec.Code)
		}
	}
	if rec := send(h, "gus", http.MethodPost, queuePostPath(id, 4), `{"action":"ignore"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("an unknown action: %d, want 422", rec.Code)
	}
	if rec := send(h, "gus", http.MethodPost, queuePostPath(id, 0), `{"action":"dismiss"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("ordinal 0: %d, want 400", rec.Code)
	}

	rec = send(h, "gus", http.MethodPost, queuePostPath(id, 4), `{"action":"dismiss","note":"not spam"}`)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("gus's dismissal: %d, Cache-Control %q: %s", rec.Code, rec.Header().Get("Cache-Control"), rec.Body)
	}
	var res dto.QueueResolution
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Action != "dismiss" || res.Post != dto.QueuePostUnchanged || len(res.Flags) != 1 {
		t.Errorf("the dismissal answered %s", rec.Body)
	}
	if rec := send(h, "bob", http.MethodPost, queuePostPath(id, 4), `{"action":"remove"}`); rec.Code != http.StatusConflict {
		t.Errorf("bob's remove after gus's dismissal: %d, want 409", rec.Code)
	}
	if rec := send(h, "gus", http.MethodGet, "/api/v1/admin/queue", ""); rec.Code != http.StatusOK ||
		json.Unmarshal(rec.Body.Bytes(), &q) != nil || len(q.Items) != 0 {
		t.Errorf("gus's queue after the dismissal: %s", rec.Body)
	}
	rows := w.auditLog()
	last := rows[len(rows)-1]
	if last.action != "flag.dismissed" || last.actor.Int64 != w.users["gus"] {
		t.Errorf("the last audit row %+v: want flag.dismissed by gus", last)
	}
}
