package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// TestNewApplicationCountsInMe is AC3 over HTTP (TKT-01M29CP2N0): each
// submission through POST /api/v1/apply adds one to a moderator's GET /me
// notifications, the number the Alerts badge shows in both layouts, and a
// denial takes it away again while the row stays unread. A member's count
// never moves.
func TestNewApplicationCountsInMe(t *testing.T) {
	w := newApplyWorld(t)
	var mod int64
	w.write(func(q *dbq.Queries) error {
		var err error
		mod, err = q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "mod", DisplayName: "Moderator",
			Status: "active", Role: domain.RoleModerator, Kind: "human", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	modToken := sessiontest.SignIn(t, w.db, mod, w.now)
	notifications := func(token string) int {
		t.Helper()
		rec := w.do(http.MethodGet, "/api/v1/me", "", token)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /me: %d %s", rec.Code, rec.Body)
		}
		return decodeMe(t, rec.Body.Bytes()).Notifications
	}
	if n := notifications(modToken); n != 0 {
		t.Fatalf("the moderator starts with %d", n)
	}

	var apps []int
	for i, subject := range []string{"carol", "dave"} {
		id, token := w.applicant(subject)
		if rec := w.do(http.MethodPost, "/api/v1/apply", validAnswers, token); rec.Code >= 300 {
			t.Fatalf("POST /apply as %s: %d %s", subject, rec.Code, rec.Body)
		}
		if n := notifications(modToken); n != i+1 {
			t.Errorf("after %d submissions the moderator counts %d", i+1, n)
		}
		apps = append(apps, w.count(`SELECT id FROM applications WHERE identity_id = ?`, id))
	}
	if n := notifications(w.member); n != 0 {
		t.Errorf("a member counts %d", n)
	}

	rec := w.do(http.MethodPost, fmt.Sprintf("/api/v1/admin/applications/%d/deny", apps[0]), `{"note":"no"}`, modToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("deny: %d %s", rec.Code, rec.Body)
	}
	if n := notifications(modToken); n != 1 {
		t.Errorf("after a denial the moderator counts %d, want 1", n)
	}
	if n := w.count(`SELECT count(*) FROM notifications WHERE user_id = ? AND kind = 'new_application'
		AND read_at IS NULL`, mod); n != 2 {
		t.Errorf("%d unread new_application rows after the denial, want both still unread", n)
	}
}
