package applications

// Notifications about applications (TKT-01M29CP2N0): a submission tells
// staff, an approval tells the new member, and closing an application never
// marks anything read.

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/oidc"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/identity"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// meCount is user's GET /me notifications count, from the statement GET /me
// runs.
func (w *world) meCount(user int64) int64 {
	w.t.Helper()
	var n int64
	if err := w.db.Read(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		rows, err := q.MeCounts(w.t.Context(), user)
		if err == nil {
			n = rows[0].Notifications
		}
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// AC1 through Submit: the rows are written with the application, one per
// active staff member, and a refused notification refuses the submission.
func TestSubmitNotifiesStaff(t *testing.T) {
	w := newReviewWorld(t)
	w.exec(`INSERT INTO users (id, handle, display_name, status, role, kind, prefs_json, created_at, updated_at)
		VALUES (4, 'suspmod', 'Suspended moderator', 'suspended', 'moderator', 'human', '{}', 1, 1)`)

	// First the failure: a trigger refuses the notification.
	w.exec(`CREATE TRIGGER fail_notify BEFORE INSERT ON notifications BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)
	v := w.applicant("carol")
	if err := w.submit(v); err == nil {
		t.Fatal("the submission succeeded with its notifications refused")
	}
	if n := w.applications(v); n != 0 {
		t.Errorf("%d applications stored with the notifications refused", n)
	}
	w.exec(`DROP TRIGGER fail_notify`)

	// Then the success.
	if err := w.submit(v); err != nil {
		t.Fatal(err)
	}
	var app int64
	if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT id FROM applications WHERE identity_id = ?`, v.IdentityID).Scan(&app)
	}); err != nil {
		t.Fatal(err)
	}
	for user, want := range map[int64]int{admin.UserID: 1, moderator.UserID: 1, aMember.UserID: 0, 4: 0} {
		if n := w.count(`SELECT count(*) FROM notifications WHERE user_id = ? AND kind = 'new_application'
			AND application_id = ? AND read_at IS NULL AND at = ?`, user, app, w.now.UnixMilli()); n != want {
			t.Errorf("user %d has %d new_application rows, want %d", user, n, want)
		}
	}
	if n := w.count(`SELECT count(*) FROM notifications`); n != 2 {
		t.Errorf("%d notifications in all, want 2", n)
	}
}

// AC5: approving, denying or withdrawing an application writes no read_at,
// and each drops the application out of every recipient's GET /me count.
// Another pending application still counts, a notification of another kind
// still counts, and one the moderator read stays read and uncounted.
func TestClosedApplicationLeavesTheCount(t *testing.T) {
	for _, c := range []struct {
		name string
		do   func(w *world, v domain.Viewer, id int64) error
	}{
		{"approve", func(w *world, _ domain.Viewer, id int64) error {
			_, err := w.svc.Approve(w.t.Context(), moderator, id, "closed", "", "")
			return err
		}},
		{"deny", func(w *world, _ domain.Viewer, id int64) error {
			_, err := w.svc.Deny(w.t.Context(), moderator, id, "not this time", false, "")
			return err
		}},
		{"deny and block", func(w *world, _ domain.Viewer, id int64) error {
			_, err := w.svc.Deny(w.t.Context(), moderator, id, "spam", true, "")
			return err
		}},
		{"withdraw", func(w *world, v domain.Viewer, _ int64) error {
			_, err := w.svc.Withdraw(w.t.Context(), v)
			return err
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newReviewWorld(t)
			// A reply notification with no application, and one the
			// moderator already read.
			w.exec(`INSERT INTO notifications (user_id, kind, thread_id, ordinal, at, read_at) VALUES
				(2, 'reply_to_you', 1, 2, 1, NULL), (2, 'mention', 1, 3, 1, 5)`)
			v, id := w.pendingApp("closing")
			_, other := w.pendingApp("waiting")
			for _, u := range []int64{admin.UserID, moderator.UserID} {
				want := int64(2)
				if u == moderator.UserID {
					want = 3 // and the reply
				}
				if n := w.meCount(u); n != want {
					t.Fatalf("user %d counts %d before closing, want %d", u, n, want)
				}
			}

			if err := c.do(w, v, id); err != nil {
				t.Fatal(err)
			}

			if n := w.count(`SELECT count(*) FROM notifications WHERE kind = 'new_application' AND read_at IS NOT NULL`); n != 0 {
				t.Errorf("closing marked %d new_application rows read", n)
			}
			if n := w.count(`SELECT count(*) FROM notifications WHERE kind = 'new_application' AND application_id = ?`,
				id); n != 2 {
				t.Errorf("%d rows name the closed application, want both recipients' still there", n)
			}
			if n := w.count(`SELECT count(*) FROM notifications WHERE user_id = 2 AND kind = 'mention' AND read_at = 5`); n != 1 {
				t.Error("the moderator's read mention changed")
			}
			if n := w.meCount(admin.UserID); n != 1 {
				t.Errorf("the admin counts %d after closing, want 1 (application %d)", n, other)
			}
			if n := w.meCount(moderator.UserID); n != 2 {
				t.Errorf("the moderator counts %d after closing, want 2 (application %d and the reply)", n, other)
			}
		})
	}
}

// AC2's approved half, as the maintainer decided on TKT-01M3Q2V25E: the
// approval writes one application_decided row for the new member, naming
// the application and the moderator, in its transaction; the applicant
// learns the decision by signing in, where GET /me counts it.
func TestApprovedApplicantIsNotified(t *testing.T) {
	w := newReviewWorld(t)
	_, id := w.pendingApp("carol")

	// A refused notification refuses the approval.
	w.exec(`CREATE TRIGGER fail_decided BEFORE INSERT ON notifications WHEN NEW.kind = 'application_decided'
		BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)
	if _, err := w.svc.Approve(t.Context(), moderator, id, "carol", "", ""); err == nil {
		t.Fatal("the approval succeeded with its notification refused")
	}
	if s := w.status(id); s != StatusPending {
		t.Errorf("the application is %s", s)
	}
	if n := w.count(`SELECT count(*) FROM users WHERE handle = 'carol'`); n != 0 {
		t.Error("the member was created")
	}
	w.exec(`DROP TRIGGER fail_decided`)

	out, err := w.svc.Approve(t.Context(), moderator, id, "carol", "", "")
	if err != nil {
		t.Fatal(err)
	}
	var user int64
	if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT id FROM users WHERE handle = 'carol'`).Scan(&user)
	}); err != nil {
		t.Fatal(err)
	}
	if out.Decision == nil || out.Decision.User == nil || out.Decision.User.Handle != "carol" {
		t.Fatalf("the answer: %+v", out)
	}
	if n := w.count(`SELECT count(*) FROM notifications WHERE kind = 'application_decided'`); n != 1 {
		t.Errorf("%d application_decided rows, want 1", n)
	}
	if n := w.count(`SELECT count(*) FROM notifications WHERE kind = 'application_decided' AND user_id = ?
		AND application_id = ? AND actor_id = ? AND at = ? AND read_at IS NULL`,
		user, id, moderator.UserID, w.now.UnixMilli()); n != 1 {
		t.Error("the application_decided row does not name the member, the application and the moderator")
	}

	// Sign in as the member, and read GET /me through the session.
	store := &session.Store{DB: w.db, Now: func() time.Time { return w.now }}
	pm := perm.New(w.db)
	ids := &identity.Service{DB: w.db, Sessions: store, Perm: pm, Now: func() time.Time { return w.now }}
	res, err := ids.Login(t.Context(), &oidc.Identity{Provider: "github", Issuer: "https://github.com", Subject: "carol"},
		domain.Viewer{}, session.Meta{Provider: "github"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != identity.SignedIn || res.Token == "" {
		t.Fatalf("the sign-in after approval: %+v", res)
	}
	us := &users.Service{DB: w.db, Perm: pm}
	var me dto.Me
	var meErr error
	h := session.Middleware(store, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		me, meErr = us.Me(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: res.Token})
	h.ServeHTTP(httptest.NewRecorder(), req)
	if meErr != nil {
		t.Fatal(meErr)
	}
	if me.User == nil || me.User.Handle != "carol" || me.Notifications != 1 {
		t.Errorf("GET /me after signing in: %+v, want carol with 1 notification", me)
	}
}

// AC2's denied half: the applicant, who has no user, sees the decision on
// the status page, and no notification is written for anyone.
func TestDeniedApplicantSeesTheStatus(t *testing.T) {
	w := newReviewWorld(t)
	v, id := w.pendingApp("dave")
	before := w.count(`SELECT count(*) FROM notifications`)
	w.now = w.now.Add(time.Minute)
	if _, err := w.svc.Deny(t.Context(), moderator, id, "not known to anyone", false, ""); err != nil {
		t.Fatal(err)
	}
	st, err := w.svc.Status(t.Context(), v)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusDenied || st.DecidedAt == nil || int64(*st.DecidedAt) != w.now.UnixMilli() {
		t.Errorf("status %+v, want denied at %d", st, w.now.UnixMilli())
	}
	if n := w.count(`SELECT count(*) FROM notifications`); n != before {
		t.Errorf("the denial wrote %d notifications", n-before)
	}
	if n := w.count(`SELECT count(*) FROM notifications WHERE kind = 'application_decided'`); n != 0 {
		t.Errorf("%d application_decided rows after a denial", n)
	}
}
