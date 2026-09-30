package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Tests for GET /api/v1/admin/audit (TKT-01M29CP2R4), in the admin world:
// member, scoped (moderate on a forum through the moderators group),
// moderator, admin, and a suspended admin.

func (w *adminWorld) audit(viewer, query string) *httptest.ResponseRecorder {
	w.t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit"+query, nil)
	if tok := w.tokens[viewer]; tok != "" {
		req.AddCookie(sessiontest.Cookie(tok))
	}
	rec := httptest.NewRecorder()
	w.handler.ServeHTTP(rec, req)
	return rec
}

// appendAudit writes one row through audit.Append at ms and returns its id.
func (w *adminWorld) appendAudit(ms int64, by audit.By, action string, target audit.Target) int64 {
	w.t.Helper()
	var id int64
	if err := w.db.Write(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		id, err = audit.Append(w.t.Context(), q, time.UnixMilli(ms), by, action, target, map[string]any{"note": "<b>x</b>"})
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return id
}

// TestAuditIsAdminOnly is the fourth criterion's endpoint half: 401 to
// anonymous and to a suspended admin, whose session carries no role; 403 to
// a pre-user, a member, a moderator by role, and a member who moderates a
// forum through a group; 200 and no-store to an admin. The refusals carry
// no row.
func TestAuditIsAdminOnly(t *testing.T) {
	w := newAdminWorld(t)
	w.appendAudit(1_757_000_000_000, audit.By{Actor: 4, IP: "203.0.113.7"}, "user.promoted", audit.User(3))
	for viewer, want := range map[string]int{
		"anonymous": http.StatusUnauthorized,
		"suspended": http.StatusUnauthorized,
		"pre-user":  http.StatusForbidden,
		"member":    http.StatusForbidden,
		"scoped":    http.StatusForbidden,
		"moderator": http.StatusForbidden,
		"admin":     http.StatusOK,
	} {
		rec := w.audit(viewer, "")
		if rec.Code != want {
			t.Errorf("%s: %d, want %d: %s", viewer, rec.Code, want, rec.Body)
			continue
		}
		if want != http.StatusOK {
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["entries"] != nil || body["actions"] != nil {
				t.Errorf("%s's refusal carries %s", viewer, rec.Body)
			}
			continue
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("the admin's answer has Cache-Control %q", cc)
		}
		var page dto.AuditPage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		// The newest row is the one above; the world's own setup wrote the
		// others, through perm.
		var details map[string]string
		if len(page.Entries) == 0 || page.Entries[0].Action != "user.promoted" || page.Entries[0].Actor == nil ||
			page.Entries[0].Actor.Handle != "admin" || page.Entries[0].IP != "203.0.113.7" ||
			json.Unmarshal(page.Entries[0].Details, &details) != nil || details["note"] != "<b>x</b>" {
			t.Errorf("the admin's page: %s", rec.Body)
		}
		if !slices.Equal(page.Actions, audit.Actions) {
			t.Errorf("the admin's page offers %v, want audit.Actions", page.Actions)
		}
	}
}

// TestAuditQueryParameters: each query string parameter reaches its filter
// under the name docs/07 gives it, a malformed one is 400, and next_cursor
// fetches the next page.
func TestAuditQueryParameters(t *testing.T) {
	w := newAdminWorld(t)
	// The world's setup wrote rows of its own, through perm; they are left
	// out of what each answer is compared with.
	setup := map[int64]bool{}
	var first dto.AuditPage
	if err := json.Unmarshal(w.audit("admin", "").Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	for _, e := range first.Entries {
		setup[e.ID] = true
	}
	promoted := w.appendAudit(1000, audit.By{Actor: 4}, "user.promoted", audit.User(3))
	expired := w.appendAudit(2000, audit.System("expiry"), "user.unsuspended", audit.User(5))
	changed := w.appendAudit(3000, audit.By{Actor: 3}, "setting.changed", audit.Setting("registration.mode"))
	for _, c := range []struct {
		query string
		want  []int64
	}{
		{"", []int64{changed, expired, promoted}},
		{"?actor=moderator", []int64{changed}},
		{"?system=1", []int64{expired}},
		{"?action=user.promoted", []int64{promoted}},
		{"?action=user.", []int64{expired, promoted}},
		{"?target_type=user", []int64{expired, promoted}},
		{"?target_type=user&target_id=5", []int64{expired}},
		{"?from=2000", []int64{changed, expired}},
		{"?to=2000", []int64{promoted}},
		// 0 is a bound like any other: nothing was written before the epoch.
		{"?to=0", []int64{}},
		{"?from=0", []int64{changed, expired, promoted}},
		{"?actor=nobody", []int64{}},
	} {
		rec := w.audit("admin", c.query)
		var page dto.AuditPage
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &page) != nil {
			t.Errorf("%q: %d %s", c.query, rec.Code, rec.Body)
			continue
		}
		got := []int64{}
		for _, e := range page.Entries {
			if !setup[e.ID] {
				got = append(got, e.ID)
			}
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%q: %v, want %v", c.query, got, c.want)
		}
	}
	for _, q := range []string{"?cursor=zz!", "?target_id=5", "?target_type=users", "?from=today", "?system=yes",
		"?system=1&actor=admin"} {
		if rec := w.audit("admin", q); rec.Code != http.StatusBadRequest {
			t.Errorf("%q: %d, want 400", q, rec.Code)
		}
	}

	for i := 0; i < 60; i++ {
		w.appendAudit(4000+int64(i), audit.By{Actor: 4}, "user.demoted", audit.User(3))
	}
	seen := 0
	cursor := ""
	for pages := 1; ; pages++ {
		rec := w.audit("admin", "?action=user.demoted&cursor="+cursor)
		var page dto.AuditPage
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &page) != nil {
			t.Fatalf("page %d: %d %s", pages, rec.Code, rec.Body)
		}
		seen += len(page.Entries)
		if page.NextCursor == "" {
			if pages != 2 || seen != 60 {
				t.Errorf("%d rows in %d pages, want 60 in 2", seen, pages)
			}
			break
		}
		cursor = page.NextCursor
		if pages > 3 {
			t.Fatal("paging does not end")
		}
	}
}
