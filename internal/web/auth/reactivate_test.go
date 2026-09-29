package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/identity"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// The account lifecycle at sign-in (TKT-01M29CKRFS): the "account
// unavailable" page, the Reactivate button, and no way back in otherwise.

// status is userID's status.
func (w *world) status(userID int64) string {
	w.t.Helper()
	var s string
	if err := w.db.Read(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUser(w.t.Context(), userID)
		s = u.Status
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return s
}

// change makes a lifecycle change to userID as the forum's clock sees it,
// through perm, as the account service does.
func (w *world) change(userID int64, action, reason string, until int64) {
	w.t.Helper()
	ev := perm.New(w.db)
	var err error
	if action == perm.ActionAnonymized {
		err = ev.Anonymize(w.t.Context(), userID, audit.System("test"), nil)
	} else {
		err = ev.ChangeStatus(w.t.Context(), userID, perm.StatusChange{By: audit.System("test"), Action: action,
			Reason: reason, Until: until})
	}
	if err != nil {
		w.t.Fatal(err)
	}
}

// post sends a same-origin POST to target, as a form on this site does.
func (w *world) post(target string, cookies ...*http.Cookie) *http.Response {
	w.t.Helper()
	return w.do(http.MethodPost, target, cookies...)
}

// TestTheSuspendedPageShowsReasonAndEnd is AC3: a suspended account that
// signs in is told the reason, escaped, and the end or that none is set,
// and gets no session and no Reactivate button.
func TestTheSuspendedPageShowsReasonAndEnd(t *testing.T) {
	const reason = `<script>alert("x")</script> & spam`
	end := time.Date(2026, 10, 7, 18, 30, 0, 0, time.UTC)
	for _, c := range []struct {
		name  string
		until int64
		want  string
	}{
		{"with an end", end.UnixMilli(), "Until: 7 October 2026, 18:30 UTC"},
		{"with no end", 0, "No end date is set."},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			id := w.user("sam", "active", "user-1")
			w.change(id, perm.ActionSuspended, reason, c.until)
			resp := w.signIn("/t/abc/x")
			b := body(resp)
			if resp.StatusCode != http.StatusForbidden || !strings.Contains(b, "<h1>Account unavailable</h1>") {
				t.Fatalf("status %d:\n%s", resp.StatusCode, b)
			}
			if strings.Contains(b, "<script>") || !strings.Contains(b,
				"Reason: &lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt; &amp; spam") {
				t.Errorf("the reason is not escaped as it should be:\n%s", b)
			}
			if !strings.Contains(b, c.want) || !strings.Contains(b, "This account is suspended.") {
				t.Errorf("the page does not say %q:\n%s", c.want, b)
			}
			if strings.Contains(b, "Reactivate") || cookie(resp, ReactivateCookie) != nil {
				t.Error("a suspended account was offered reactivation")
			}
			if c := cookie(resp, session.CookieName); c != nil && c.Value != "" {
				t.Error("a session cookie was set")
			}
			if n := w.sessions(); n != 0 {
				t.Errorf("%d sessions", n)
			}
		})
	}
}

// reactivating signs in as a deactivated account and returns the page's
// reactivation cookie.
func (w *world) reactivating(returnTo string) *http.Cookie {
	w.t.Helper()
	resp := w.signIn(returnTo)
	b := body(resp)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(b, "This account is deactivated.") ||
		!strings.Contains(b, `<form method="post" action="/auth/reactivate">`) ||
		!strings.Contains(b, "Reactivate my account") {
		w.t.Fatalf("the deactivated page: %d\n%s", resp.StatusCode, b)
	}
	c := cookie(resp, ReactivateCookie)
	if c == nil || c.Value == "" {
		w.t.Fatal("no reactivation cookie")
	}
	if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" ||
		c.MaxAge != int(ReactivateTTL/time.Second) {
		w.t.Errorf("reactivation cookie attributes %+v", c)
	}
	if s := cookie(resp, session.CookieName); s != nil && s.Value != "" {
		w.t.Error("signing in alone set a session cookie")
	}
	if n := w.sessions(); n != 0 {
		w.t.Errorf("signing in alone wrote %d sessions", n)
	}
	return c
}

// forged is a reactivation cookie this server sealed for subject, bound to
// the account row as it stands, so that only the status can refuse it: a
// tampered browser could not make one, but a page left open could present
// it.
func (w *world) forged(subject string) *http.Cookie {
	w.t.Helper()
	var stamp int64
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		err := tx.QueryRowContext(w.t.Context(), `SELECT u.updated_at FROM users u JOIN identities i ON i.user_id = u.id
			WHERE i.subject = ?`, subject).Scan(&stamp)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	v, err := w.handler.sealPending(reactivatePurpose, pending{
		Claims: identity.Claims{Provider: "fake", Issuer: w.idp.URL, Subject: subject}, ReturnTo: "/", Stamp: stamp})
	if err != nil {
		w.t.Fatal(err)
	}
	return &http.Cookie{Name: ReactivateCookie, Value: v}
}

// TestReactivation is AC8: a deactivated member who signs in is offered the
// button, and pressing it makes the account active through
// session.IssueUserWith, writes user.reactivated, and signs them in where
// they were going. The cookie serves one press. A suspended, pending or
// anonymized account posting the same form is refused and nothing is
// written, and so is a missing, expired or cross-site one.
func TestReactivation(t *testing.T) {
	t.Run("a deactivated member comes back", func(t *testing.T) {
		w := newWorld(t)
		id := w.user("dee", "active", "user-1")
		w.change(id, perm.ActionDeactivated, "", 0)
		c := w.reactivating("/t/abc/x?page=2")
		resp := w.post(ReactivatePath, c)
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/t/abc/x?page=2" {
			t.Fatalf("answered %d to %q:\n%s", resp.StatusCode, resp.Header.Get("Location"), body(resp))
		}
		if !cleared(resp, ReactivateCookie) {
			t.Error("the reactivation cookie was not cleared")
		}
		if h := signedInAs(t, w, resp); h != "dee" {
			t.Errorf("signed in as %s", h)
		}
		if s := w.status(id); s != domain.StatusActive {
			t.Errorf("dee is %s", s)
		}
		if n := w.count(`SELECT count(*) FROM audit_log WHERE action = ? AND actor_id = ? AND target_id = ?`,
			perm.ActionReactivated, id, fmt.Sprint(id)); n != 1 {
			t.Errorf("%d user.reactivated rows by dee about dee", n)
		}

		// The same cookie again: the account is active now, so there is
		// nothing to reactivate and no second session.
		again := w.post(ReactivatePath, c)
		if again.StatusCode != http.StatusForbidden || w.sessions() != 1 {
			t.Errorf("a replay answered %d with %d sessions", again.StatusCode, w.sessions())
		}
	})

	t.Run("a copy of the cookie cannot undo a later deactivation", func(t *testing.T) {
		w := newWorld(t)
		id := w.user("dee", "active", "user-1")
		w.change(id, perm.ActionDeactivated, "", 0)
		c := w.reactivating("")
		if resp := w.post(ReactivatePath, c); resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("reactivating answered %d", resp.StatusCode)
		}
		w.now = w.now.Add(time.Minute) // still inside the cookie's five minutes
		w.change(id, perm.ActionDeactivated, "", 0)
		// As if all three changes had landed in the same millisecond: the
		// row's timestamp is back where the sign-in found it, and only the
		// status version tells the two deactivations apart (review 1441).
		var first int64
		w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT min(at) FROM audit_log WHERE action = ?`,
				perm.ActionDeactivated).Scan(&first)
		})
		if err := w.db.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			_, err := tx.ExecContext(t.Context(), `UPDATE users SET updated_at = ? WHERE id = ?`, first, id)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		sessions := w.sessions()
		w.refused(w.post(ReactivatePath, c), http.StatusForbidden, "Account unavailable")
		if s := w.status(id); s != domain.StatusDeactivated || w.sessions() != sessions {
			t.Errorf("the replay left dee %s with %d sessions", s, w.sessions())
		}
	})

	t.Run("a reactivation whose sign-in cannot finish", func(t *testing.T) {
		// The account is reactivated and stays so; the session just issued
		// is ended; the page says to sign in again; and signing in works
		// (review 1441).
		w := newWorld(t)
		id := w.user("dee", "active", "user-1")
		w.user("bob", "active", "bob-sub")
		w.change(id, perm.ActionDeactivated, "", 0)
		c := w.reactivating("")
		w.idp.Subject = "bob-sub"
		bob := cookie(w.signIn(""), session.CookieName)
		w.handler.Login.(*identity.Service).Sessions = failingRevoke{w.store, bob.Value}

		resp := w.post(ReactivatePath, c, bob)
		b := body(resp)
		if resp.StatusCode != http.StatusInternalServerError || !strings.Contains(b, "<h1>Account reactivated</h1>") ||
			!strings.Contains(b, `href="/auth/signin"`) {
			t.Fatalf("answered %d:\n%s", resp.StatusCode, b)
		}
		if s := cookie(resp, session.CookieName); s != nil && s.Value != "" {
			t.Error("a session cookie was set")
		}
		if s := w.status(id); s != domain.StatusActive {
			t.Errorf("dee is %s", s)
		}
		if n := w.count(`SELECT count(*) FROM sessions WHERE user_id = ?`, id); n != 0 {
			t.Errorf("%d of dee's sessions were left", n)
		}
		w.idp.Subject = "user-1"
		if h := signedInAs(t, w, w.signIn("")); h != "dee" {
			t.Errorf("signing in again signed in %s", h)
		}
	})

	for _, status := range []string{domain.StatusSuspended, domain.StatusPending, domain.StatusActive} {
		t.Run("a "+status+" account cannot", func(t *testing.T) {
			w := newWorld(t)
			id := w.user("sam", status, "user-1")
			before := w.count(`SELECT count(*) FROM audit_log`)
			resp := w.post(ReactivatePath, w.forged("user-1"))
			w.refused(resp, http.StatusForbidden, "Account unavailable")
			if s := w.status(id); s != status {
				t.Errorf("the account is %s, was %s", s, status)
			}
			if n := w.count(`SELECT count(*) FROM audit_log`); n != before {
				t.Errorf("a refused reactivation wrote %d audit rows", n-before)
			}
		})
	}

	t.Run("an anonymized account cannot", func(t *testing.T) {
		w := newWorld(t)
		id := w.user("gone", "active", "user-1")
		w.change(id, perm.ActionAnonymized, "", 0)
		w.refused(w.post(ReactivatePath, w.forged("user-1")), http.StatusForbidden, "Account unavailable")
		if s := w.status(id); s != domain.StatusDeactivated {
			t.Errorf("the anonymized account is %s", s)
		}
	})

	t.Run("an expired cookie", func(t *testing.T) {
		w := newWorld(t)
		id := w.user("dee", "active", "user-1")
		w.change(id, perm.ActionDeactivated, "", 0)
		c := w.reactivating("")
		w.now = w.now.Add(ReactivateTTL + time.Second)
		w.refused(w.post(ReactivatePath, c), http.StatusBadRequest, "Reactivation expired")
		if s := w.status(id); s != domain.StatusDeactivated {
			t.Errorf("dee is %s", s)
		}
	})

	t.Run("no cookie, or one sealed for something else", func(t *testing.T) {
		w := newWorld(t)
		id := w.user("dee", "active", "user-1")
		w.change(id, perm.ActionDeactivated, "", 0)
		w.refused(w.post(ReactivatePath), http.StatusBadRequest, "Reactivation expired")
		reg, err := w.handler.sealRegistration(pending{Claims: identity.Claims{Provider: "fake", Subject: "user-1"}})
		if err != nil {
			t.Fatal(err)
		}
		w.refused(w.post(ReactivatePath, &http.Cookie{Name: ReactivateCookie, Value: reg}), http.StatusBadRequest,
			"Reactivation expired")
		if s := w.status(id); s != domain.StatusDeactivated {
			t.Errorf("dee is %s", s)
		}
	})

	t.Run("a cross-site post", func(t *testing.T) {
		w := newWorld(t)
		id := w.user("dee", "active", "user-1")
		w.change(id, perm.ActionDeactivated, "", 0)
		c := w.reactivating("")
		r := httptest.NewRequest("POST", site+ReactivatePath, nil)
		r.Header.Set("Origin", "https://evil.example")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
		rec := httptest.NewRecorder()
		w.srv.ServeHTTP(rec, r)
		if rec.Code != http.StatusForbidden || w.status(id) != domain.StatusDeactivated || w.sessions() != 0 {
			t.Errorf("a cross-site post answered %d; dee is %s", rec.Code, w.status(id))
		}
	})
}

// TestNoTransitionLetsTheUserBackIn is AC10, the chokepoint for this
// ticket: after a suspension and after a deactivation, signing in through
// each linked identity writes no session row. After an anonymization the
// identities are gone, IssueUser for the old identity id is ErrNoIdentity,
// and the same subject is someone new: it takes the registration path and
// gets a new user id, never the anonymized one.
func TestNoTransitionLetsTheUserBackIn(t *testing.T) {
	for _, action := range []string{perm.ActionSuspended, perm.ActionDeactivated} {
		t.Run(action, func(t *testing.T) {
			w := newWorld(t)
			id := w.user("alice", "active", "user-1")
			w.identityAt(id, "fake", "user-1b")
			w.change(id, action, "spam", 0)
			for _, sub := range []string{"user-1", "user-1b"} {
				w.idp.Subject = sub
				resp := w.signIn("")
				if resp.StatusCode != http.StatusForbidden || !strings.Contains(body(resp), "Account unavailable") {
					t.Errorf("%s: answered %d", sub, resp.StatusCode)
				}
				if n := w.sessions(); n != 0 {
					t.Errorf("%s: %d sessions", sub, n)
				}
			}
		})
	}

	t.Run(perm.ActionAnonymized, func(t *testing.T) {
		w := newWorld(t)
		w.open()
		id := w.user("alice", "active", "user-1")
		var old int64
		if err := w.db.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
			i, err := q.GetIdentityBySubject(t.Context(), dbq.GetIdentityBySubjectParams{ProviderKey: "fake", Subject: "user-1"})
			old = i.ID
			return err
		}); err != nil {
			t.Fatal(err)
		}
		w.change(id, perm.ActionAnonymized, "", 0)
		if _, _, err := w.store.IssueUser(t.Context(), old, session.Meta{}); !errors.Is(err, session.ErrNoIdentity) {
			t.Errorf("IssueUser for the old identity: %v", err)
		}
		reg := w.registering("")
		resp := w.submit("alice2", reg)
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("registering again answered %d: %s", resp.StatusCode, body(resp))
		}
		c := cookie(resp, session.CookieName)
		s, err := w.store.Lookup(t.Context(), c.Value)
		if err != nil || s.User == nil || s.User.ID == id {
			t.Errorf("the same subject signed in as %+v (%v), the anonymized account is %d", s.User, err, id)
		}
	})
}
