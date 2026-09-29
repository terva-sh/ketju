package applications

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/forms"
	"github.com/terva-sh/ketju/internal/problem"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// world is a database with the seeded registration form, an admin, and a
// clock the tests move.
type world struct {
	t   *testing.T
	db  *db.DB
	svc *Service
	now time.Time
}

func newWorld(t *testing.T) *world {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, db: d, now: time.UnixMilli(1_757_600_000_000)}
	w.svc = &Service{DB: d, Now: func() time.Time { return w.now }}
	w.exec(`INSERT INTO users (id, handle, display_name, status, role, kind, prefs_json, created_at, updated_at)
		VALUES (1, 'root', 'Site Admin', 'active', 'admin', 'human', '{}', 1, 1)`)
	return w
}

func (w *world) exec(query string, args ...any) {
	w.t.Helper()
	if err := w.db.Write(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(w.t.Context(), query, args...)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) count(query string, args ...any) int {
	w.t.Helper()
	var n int
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), query, args...).Scan(&n)
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// applicant writes an identity with no user, as BeginApplication does, and
// returns the viewer its pre-user session resolves to.
func (w *world) applicant(subject string) domain.Viewer {
	w.t.Helper()
	var id int64
	if err := w.db.Write(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		id, err = q.CreateIdentity(w.t.Context(), dbq.CreateIdentityParams{ProviderKey: "github", Subject: subject,
			Email: sql.NullString{String: subject + "@example.org", Valid: true}, EmailVerified: 1,
			Issuer: "https://github.com", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			return err
		}
		return q.RecordIdentityLogin(w.t.Context(), dbq.RecordIdentityLoginParams{
			RawClaimsJson: sql.NullString{String: `{"sub":"` + subject + `","name":"Carol ` + subject + `","email":"` + subject + `@example.org"}`, Valid: true},
			Email:         sql.NullString{String: subject + "@example.org", Valid: true}, EmailVerified: 1, UpdatedAt: 1, ID: id})
	}); err != nil {
		w.t.Fatal(err)
	}
	return domain.Viewer{PreUser: true, IdentityID: id}
}

// answers are valid answers to the seeded registration form.
func answers() map[string]json.RawMessage {
	return map[string]json.RawMessage{"name": json.RawMessage(`" Carol "`), "who": json.RawMessage(`"A friend"`),
		"rules": json.RawMessage(`true`)}
}

func (w *world) submit(v domain.Viewer) error {
	w.t.Helper()
	_, err := w.svc.Submit(w.t.Context(), v, 1, answers())
	return err
}

// approve is what TKT-01M29CP2MG's approval writes: a user, the identity
// linked to it, and the application approved with the user's id.
func (w *world) approve(v domain.Viewer, handle string) {
	w.t.Helper()
	if err := w.db.Write(w.t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		uid, err := q.CreateUser(w.t.Context(), dbq.CreateUserParams{Handle: handle, DisplayName: handle, Status: "active",
			Role: domain.RoleMember, Kind: "human", CreatedAt: w.now.UnixMilli(), UpdatedAt: w.now.UnixMilli()})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(w.t.Context(), `UPDATE identities SET user_id = ? WHERE id = ?`, uid, v.IdentityID); err != nil {
			return err
		}
		_, err = tx.ExecContext(w.t.Context(), `UPDATE applications SET status = 'approved', user_id = ?, decided_by = 1,
			decided_at = ?, decision_note = 'known to the admins' WHERE identity_id = ? AND status = 'pending'`,
			uid, w.now.UnixMilli(), v.IdentityID)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
}

// deny denies the pending application with a note, and blocks the identity
// when block is set.
func (w *world) deny(v domain.Viewer, block bool) {
	w.t.Helper()
	w.exec(`UPDATE applications SET status = 'denied', decided_by = 1, decided_at = ?,
		decision_note = 'the moderators wrote this for each other' WHERE identity_id = ? AND status = 'pending'`,
		w.now.UnixMilli(), v.IdentityID)
	if block {
		w.exec(`UPDATE identities SET blocked_at = ?, blocked_by = 1 WHERE id = ?`, w.now.UnixMilli(), v.IdentityID)
	}
}

func (w *world) applications(v domain.Viewer) int {
	return w.count(`SELECT count(*) FROM applications WHERE identity_id = ?`, v.IdentityID)
}

// saveVersion2 makes a second version of the registration form active.
func (w *world) saveVersion2() {
	w.t.Helper()
	forms := &formsvc.Service{DB: w.db, Now: func() time.Time { return w.now }}
	schema := `{"title":"Apply","fields":[{"key":"who","type":"textarea","label":"Who?","required":true}]}`
	if _, err := forms.Create(w.t.Context(), domain.Viewer{UserID: 1, Role: domain.RoleAdmin, Active: true},
		formsvc.Registration, 1, []byte(schema), "192.0.2.1"); err != nil {
		w.t.Fatal(err)
	}
}

// wantCode checks err is the refusal want, coded as code.
func wantCode(t *testing.T, what string, err, want error, code problem.Code) {
	t.Helper()
	if !errors.Is(err, want) || problem.FromError(err) != code {
		t.Errorf("%s: err = %v (%s), want %v (%s)", what, err, problem.FromError(err), want, code)
	}
}

// AC1: the row stores the normalized answers against the active version,
// pending; a submission naming another version is 409 and writes nothing.
func TestSubmitStoresAgainstTheActiveVersion(t *testing.T) {
	w := newWorld(t)
	v := w.applicant("carol")
	st, err := w.svc.Submit(t.Context(), v, 1, answers())
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusPending || !st.CanWithdraw || st.CanApply || st.SubmittedAt == nil ||
		int64(*st.SubmittedAt) != w.now.UnixMilli() || st.DecidedAt != nil || st.SignIn != nil {
		t.Errorf("the status after submitting is %+v", st)
	}
	var version int
	var status, stored string
	if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT form_version, status, answers_json FROM applications WHERE identity_id = ?`,
			v.IdentityID).Scan(&version, &status, &stored)
	}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stored), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"name": "Carol", "email": "carol@example.org", "who": "A friend", "rules": true}
	if version != 1 || status != "pending" || len(got) != len(want) {
		t.Fatalf("stored version %d, %s, %v", version, status, got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("answers_json[%s] = %v, want %v (stored %s)", k, got[k], v, stored)
		}
	}

	// A second applicant answers version 1 after version 2 went live.
	w.saveVersion2()
	other := w.applicant("dave")
	_, err = w.svc.Submit(t.Context(), other, 1, answers())
	wantCode(t, "answering version 1 while 2 is active", err, ErrFormChanged, problem.CodeConflict)
	if n := w.applications(other); n != 0 {
		t.Errorf("a stale version wrote %d applications", n)
	}
	if _, err := w.svc.Submit(t.Context(), other, 2, map[string]json.RawMessage{"who": json.RawMessage(`"x"`)}); err != nil {
		t.Fatalf("answering the active version 2: %v", err)
	}
	if n := w.count(`SELECT count(*) FROM applications WHERE identity_id = ? AND form_version = 2`, other.IdentityID); n != 1 {
		t.Errorf("%d applications against version 2, want 1", n)
	}
}

// Answers that do not validate are refused with their field errors, and
// write nothing.
func TestInvalidAnswersWriteNothing(t *testing.T) {
	w := newWorld(t)
	v := w.applicant("carol")
	a := answers()
	delete(a, "who")
	_, err := w.svc.Submit(t.Context(), v, 1, a)
	fe := problem.FieldErrorsOf(err)
	if !errors.Is(err, forms.ErrInvalidAnswers) || len(fe) != 1 || fe[0].Field != "who" || fe[0].Code != forms.CodeRequired {
		t.Fatalf("err = %v, field errors %+v; want who required", err, fe)
	}
	if n := w.applications(v); n != 0 {
		t.Errorf("wrote %d applications", n)
	}
}

// AC4: a blocked identity is refused at the form, with 403, and at the
// submit, and no row is written. The status says nothing of the block
// beyond can_apply.
func TestBlockedIdentityIsRefusedAtTheForm(t *testing.T) {
	w := newWorld(t)
	v := w.applicant("carol")
	if err := w.submit(v); err != nil {
		t.Fatal(err)
	}
	w.deny(v, true)
	w.now = w.now.Add(48 * time.Hour) // past the 24-hour rule: the block alone refuses
	_, err := w.svc.Form(t.Context(), v)
	wantCode(t, "Form", err, ErrBlocked, problem.CodeForbidden)
	wantCode(t, "Submit", w.submit(v), ErrBlocked, problem.CodeForbidden)
	if n := w.applications(v); n != 1 {
		t.Errorf("%d applications, want the one denied", n)
	}
	st, err := w.svc.Status(t.Context(), v)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusDenied || st.CanApply || st.CanApplyAt != nil || st.CanWithdraw || st.SignIn != nil {
		t.Errorf("a blocked applicant's status is %+v", st)
	}
}

// AC9: no status carries the decision note, whatever the decision.
func TestStatusNeverCarriesTheNote(t *testing.T) {
	w := newWorld(t)
	denied, approved := w.applicant("carol"), w.applicant("dave")
	for _, v := range []domain.Viewer{denied, approved} {
		if err := w.submit(v); err != nil {
			t.Fatal(err)
		}
	}
	w.deny(denied, false)
	w.approve(approved, "dave")
	for _, v := range []domain.Viewer{denied, approved} {
		st, err := w.svc.Status(t.Context(), v)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(st)
		if strings.Contains(string(b), "wrote this") || strings.Contains(string(b), "known to the admins") || strings.Contains(string(b), "note") {
			t.Errorf("the status carries the note: %s", b)
		}
	}
}

// AC8: an identity with a user is refused the form and the submit with
// 409, and its status is approved with where to sign in. The pre-user
// viewer is held past the approval by 25 hours, beyond the 24-hour rule, so
// only the user check stands in the way.
func TestLinkedIdentityCannotApply(t *testing.T) {
	w := newWorld(t)
	v := w.applicant("carol")
	if err := w.submit(v); err != nil {
		t.Fatal(err)
	}
	w.approve(v, "carol")
	w.now = w.now.Add(25 * time.Hour)
	_, err := w.svc.Form(t.Context(), v)
	wantCode(t, "Form", err, ErrAlreadyMember, problem.CodeConflict)
	wantCode(t, "Submit", w.submit(v), ErrAlreadyMember, problem.CodeConflict)
	if n := w.applications(v); n != 1 {
		t.Errorf("%d applications, want the approved one alone", n)
	}
	_, err = w.svc.Withdraw(t.Context(), v)
	wantCode(t, "Withdraw", err, ErrNotPending, problem.CodeConflict)
	st, err := w.svc.Status(t.Context(), v)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusApproved || st.SignIn == nil || *st.SignIn != "/auth/github/start?return_to=/" ||
		st.CanApply || st.CanWithdraw || st.CanApplyAt != nil || st.DecidedAt == nil {
		t.Errorf("the approved status is %+v", st)
	}

	// Linked between reading the form and submitting it: Submit's own
	// check, in its transaction, refuses it.
	u := w.applicant("dave")
	if _, err := w.svc.Form(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	w.exec(`INSERT INTO users (id, handle, display_name, status, role, kind, prefs_json, created_at, updated_at)
		VALUES (50, 'dave', 'Dave', 'active', 'member', 'human', '{}', 1, 1)`)
	w.exec(`UPDATE identities SET user_id = 50 WHERE id = ?`, u.IdentityID)
	wantCode(t, "Submit after a link", w.submit(u), ErrAlreadyMember, problem.CodeConflict)
	if n := w.applications(u); n != 0 {
		t.Errorf("a linked identity wrote %d applications", n)
	}
}

// AC2: with a pending application, the form and the submit are 409, and the
// status is pending with a withdraw.
func TestPendingIdentityIsSentToStatus(t *testing.T) {
	w := newWorld(t)
	v := w.applicant("carol")
	if err := w.submit(v); err != nil {
		t.Fatal(err)
	}
	w.now = w.now.Add(25 * time.Hour) // the pending check, not the 24-hour rule
	_, err := w.svc.Form(t.Context(), v)
	wantCode(t, "Form", err, ErrPending, problem.CodeConflict)
	wantCode(t, "Submit", w.submit(v), ErrPending, problem.CodeConflict)
	st, err := w.svc.Status(t.Context(), v)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusPending || !st.CanWithdraw || st.CanApply || st.CanApplyAt != nil {
		t.Errorf("the pending status is %+v", st)
	}
	if n := w.applications(v); n != 1 {
		t.Errorf("%d applications, want 1", n)
	}
}

// AC3: one submission per rolling 24 hours, after a withdrawal or a denial
// alike: at +1 h it is 429 with Retry-After near 23 h, and at +24 h it is
// accepted.
func TestOneApplicationPer24Hours(t *testing.T) {
	for _, end := range []string{"withdrawn", "denied"} {
		t.Run(end, func(t *testing.T) {
			w := newWorld(t)
			v := w.applicant("carol")
			start := w.now
			if err := w.submit(v); err != nil {
				t.Fatal(err)
			}
			w.now = start.Add(30 * time.Minute)
			if end == "withdrawn" {
				if _, err := w.svc.Withdraw(t.Context(), v); err != nil {
					t.Fatal(err)
				}
			} else {
				w.deny(v, false)
			}

			w.now = start.Add(time.Hour)
			_, err := w.svc.Form(t.Context(), v)
			wantCode(t, "Form at +1 h", err, ErrTooSoon, problem.CodeRateLimited)
			err = w.submit(v)
			wantCode(t, "Submit at +1 h", err, ErrTooSoon, problem.CodeRateLimited)
			if after, ok := problem.RetryAfterOf(err); !ok || after != 23*time.Hour {
				t.Errorf("Retry-After at +1 h is %v (%t), want 23h", after, ok)
			}
			st, err := w.svc.Status(t.Context(), v)
			if err != nil {
				t.Fatal(err)
			}
			if st.Status != end || st.CanApply || st.CanApplyAt == nil ||
				int64(*st.CanApplyAt) != start.Add(24*time.Hour).UnixMilli() || st.DecidedAt == nil {
				t.Errorf("the status at +1 h is %+v", st)
			}

			w.now = start.Add(24*time.Hour - time.Millisecond)
			wantCode(t, "Submit a millisecond early", w.submit(v), ErrTooSoon, problem.CodeRateLimited)

			w.now = start.Add(24 * time.Hour)
			if st, err := w.svc.Status(t.Context(), v); err != nil || !st.CanApply || st.CanApplyAt != nil {
				t.Errorf("the status at +24 h is %+v (%v), want can_apply", st, err)
			}
			if _, err := w.svc.Form(t.Context(), v); err != nil {
				t.Errorf("Form at +24 h: %v", err)
			}
			if err := w.submit(v); err != nil {
				t.Errorf("Submit at +24 h: %v", err)
			}
			if n := w.applications(v); n != 2 {
				t.Errorf("%d applications, want 2", n)
			}
		})
	}
}

// Withdrawing needs a pending application, and leaves it withdrawn with a
// decided_at and no audit row.
func TestWithdrawNeedsAPending(t *testing.T) {
	w := newWorld(t)
	v := w.applicant("carol")
	_, err := w.svc.Withdraw(t.Context(), v)
	wantCode(t, "Withdraw with none", err, ErrNotPending, problem.CodeConflict)
	if err := w.submit(v); err != nil {
		t.Fatal(err)
	}
	w.now = w.now.Add(time.Minute)
	st, err := w.svc.Withdraw(t.Context(), v)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusWithdrawn || st.CanWithdraw || st.DecidedAt == nil || int64(*st.DecidedAt) != w.now.UnixMilli() {
		t.Errorf("the status after withdrawing is %+v", st)
	}
	_, err = w.svc.Withdraw(t.Context(), v)
	wantCode(t, "Withdraw twice", err, ErrNotPending, problem.CodeConflict)
	if n := w.count(`SELECT count(*) FROM audit_log`); n != 0 {
		t.Errorf("withdrawing wrote %d audit rows", n)
	}
}

// With registration.mode open, nobody applies.
func TestApplicationsClosedOutsideApprovalMode(t *testing.T) {
	w := newWorld(t)
	v := w.applicant("carol")
	if err := settings.Set(t.Context(), w.db, settings.RegistrationMode.Name, json.RawMessage(`"open"`), 1, w.now); err != nil {
		t.Fatal(err)
	}
	_, err := w.svc.Form(t.Context(), v)
	wantCode(t, "Form", err, ErrNotAccepting, problem.CodeForbidden)
	wantCode(t, "Submit", w.submit(v), ErrNotAccepting, problem.CodeForbidden)
	if st, err := w.svc.Status(t.Context(), v); err != nil || st.Status != StatusNone || st.CanApply {
		t.Errorf("the status with registration open is %+v (%v)", st, err)
	}
	if n := w.applications(v); n != 0 {
		t.Errorf("wrote %d applications", n)
	}
}

// A locked field stores the identity's claim, whatever was submitted, and
// the form prefills it with the same value.
func TestLockedFieldStoresTheIdentityClaim(t *testing.T) {
	w := newWorld(t)
	v := w.applicant("carol")
	f, err := w.svc.Form(t.Context(), v)
	if err != nil {
		t.Fatal(err)
	}
	if f.Form.Key != formsvc.Registration || f.Form.Version != 1 || f.Prefill["email"] != "carol@example.org" ||
		f.Prefill["name"] != "Carol carol" || len(f.Prefill) != 2 {
		t.Errorf("the form is %s v%d with prefill %v", f.Form.Key, f.Form.Version, f.Prefill)
	}
	a := answers()
	a["email"] = json.RawMessage(`"someone-else@example.org"`)
	if _, err := w.svc.Submit(t.Context(), v, 1, a); err != nil {
		t.Fatal(err)
	}
	if n := w.count(`SELECT count(*) FROM applications WHERE identity_id = ? AND answers_json ->> 'email' = 'carol@example.org'`,
		v.IdentityID); n != 1 {
		t.Errorf("the locked email was not stored as the claim")
	}
}

// Only a pre-user applies: anonymous is unauthenticated, and a member, or a
// suspended user, is forbidden, on every route.
func TestOnlyAnApplicantApplies(t *testing.T) {
	w := newWorld(t)
	for _, c := range []struct {
		name string
		v    domain.Viewer
		want error
		code problem.Code
	}{
		{"anonymous", domain.Viewer{}, ErrNoApplicant, problem.CodeUnauthenticated},
		{"member", domain.Viewer{UserID: 1, Role: domain.RoleAdmin, Active: true}, ErrNotApplicant, problem.CodeForbidden},
		{"suspended", domain.Viewer{UserID: 1}, ErrNotApplicant, problem.CodeForbidden},
		{"a pre-user with no identity", domain.Viewer{PreUser: true}, ErrNoApplicant, problem.CodeUnauthenticated},
		{"an identity that is gone", domain.Viewer{PreUser: true, IdentityID: 999}, ErrNoApplicant, problem.CodeUnauthenticated},
	} {
		_, err := w.svc.Form(t.Context(), c.v)
		wantCode(t, c.name+" Form", err, c.want, c.code)
		_, err = w.svc.Submit(t.Context(), c.v, 1, answers())
		wantCode(t, c.name+" Submit", err, c.want, c.code)
		_, err = w.svc.Status(t.Context(), c.v)
		wantCode(t, c.name+" Status", err, c.want, c.code)
		_, err = w.svc.Withdraw(t.Context(), c.v)
		wantCode(t, c.name+" Withdraw", err, c.want, c.code)
	}
	if n := w.count(`SELECT count(*) FROM applications`); n != 0 {
		t.Errorf("wrote %d applications", n)
	}
}

// The status of an identity that never applied offers the form.
func TestStatusWithNoApplication(t *testing.T) {
	w := newWorld(t)
	st, err := w.svc.Status(t.Context(), w.applicant("carol"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusNone || st.Provider != "github" || !st.CanApply || st.SubmittedAt != nil || st.CanWithdraw {
		t.Errorf("the status with no application is %+v", st)
	}
}
