package db

import (
	"database/sql"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// newApplication stores a pending application against the seeded
// registration form, version 1, and returns its id.
func newApplication(t *testing.T, d *DB, identity sql.NullInt64) int64 {
	t.Helper()
	var id int64
	write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		res, err := tx.ExecContext(t.Context(), `INSERT INTO applications
			(identity_id, form_id, form_version, answers_json, created_at) VALUES (?, 1, 1, '{"who":"a friend"}', 1)`,
			identity)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	return id
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// oneValue reads a single value, failing the test if the query does.
func oneValue[T any](t *testing.T, d *DB, query string, args ...any) T {
	t.Helper()
	var v T
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), query, args...).Scan(&v)
	}); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return v
}

// TestApplicationRecordsItsFormVersion is AC1: an application names the
// (form, version) it answered, and a pair that does not exist is refused.
func TestApplicationRecordsItsFormVersion(t *testing.T) {
	d, _ := openTest(t)
	ins := `INSERT INTO applications (form_id, form_version, answers_json, created_at) VALUES (?, ?, '{}', 1)`
	for _, c := range []struct{ form, version int64 }{{1, 2}, {1, 0}, {99, 1}} {
		if err := exec(t, d, ins, c.form, c.version); err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
			t.Errorf("an application naming form %d version %d: %v, want a foreign key failure", c.form, c.version, err)
		}
	}
	mustExec(t, d, ins, 1, 1)
	if n := oneValue[int64](t, d, `SELECT count(*) FROM applications WHERE form_id = 1 AND form_version = 1`); n != 1 {
		t.Errorf("%d applications name registration version 1, want 1", n)
	}
	if err := exec(t, d, `INSERT INTO applications (form_id, form_version, answers_json, created_at)
		VALUES (1, 1, 'not json', 1)`); err == nil {
		t.Error("answers that are not JSON were accepted")
	}
}

// TestFormVersionsAreImmutable is AC2: no statement changes, removes or
// replaces a written version, and saving version 2 leaves version 1
// byte-identical.
func TestFormVersionsAreImmutable(t *testing.T) {
	d, _ := openTest(t)
	const v1 = `SELECT form_id, version, CAST(schema_json AS BLOB), created_by, created_at
		FROM form_versions WHERE form_id = 1 AND version = 1`
	snapshot := func() []string {
		var out []string
		if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			out = rows(t, tx, v1)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := snapshot()
	if len(before) != 1 {
		t.Fatalf("the seeded version 1 reads as %v", before)
	}

	for _, stmt := range []string{
		`UPDATE form_versions SET schema_json = '{"fields":[]}' WHERE form_id = 1 AND version = 1`,
		`UPDATE form_versions SET created_at = created_at`,
		`DELETE FROM form_versions WHERE form_id = 1 AND version = 1`,
		`DELETE FROM form_versions`,
		// REPLACE's implicit delete fires no DELETE trigger unless
		// recursive_triggers is on, so it is refused at the insert.
		`INSERT OR REPLACE INTO form_versions (form_id, version, schema_json, created_at) VALUES (1, 1, '{"fields":[]}', 9)`,
		`REPLACE INTO form_versions (form_id, version, schema_json, created_at) VALUES (1, 1, '{"fields":[]}', 9)`,
		`INSERT INTO form_versions (form_id, version, schema_json, created_at) VALUES (1, 1, '{"fields":[]}', 9)
			ON CONFLICT DO UPDATE SET schema_json = excluded.schema_json`,
	} {
		if err := exec(t, d, stmt); err == nil {
			t.Errorf("%s changed a form version", stmt)
		}
	}

	// Saving version 2 is a new row and a move of the form's pointer.
	execAll(t, d,
		`INSERT INTO form_versions (form_id, version, schema_json, created_at) VALUES (1, 2, '{"fields":[]}', 9)`,
		`UPDATE forms SET active_version = 2, updated_at = 9 WHERE id = 1`,
	)
	// Version 1 is no longer active and no application names it, so only
	// the trigger stands between it and a DELETE.
	if err := exec(t, d, `DELETE FROM form_versions WHERE form_id = 1 AND version = 1`); err == nil {
		t.Error("an inactive, unreferenced version was deleted")
	}
	if after := snapshot(); !slices.Equal(after, before) {
		t.Errorf("saving version 2 changed version 1:\n%v\n---\n%v", before, after)
	}
	if v := oneValue[int64](t, d, `SELECT active_version FROM forms WHERE id = 1`); v != 2 {
		t.Errorf("active_version = %d after saving version 2, want 2", v)
	}
	if err := exec(t, d, `INSERT INTO form_versions (form_id, version, schema_json, created_at)
		VALUES (1, 3, 'not json', 9)`); err == nil {
		t.Error("a schema that is not JSON was accepted")
	}
	if err := exec(t, d, `INSERT INTO form_versions (form_id, version, schema_json, created_at)
		VALUES (1, 0, '{"fields":[]}', 9)`); err == nil {
		t.Error("version 0 was accepted")
	}
	if err := exec(t, d, `INSERT INTO form_versions (form_id, version, schema_json, created_at)
		VALUES (99, 1, '{"fields":[]}', 9)`); err == nil {
		t.Error("a version of a form that does not exist was accepted")
	}
}

// TestActiveVersionAlwaysExists is AC8: forms.active_version is NOT NULL
// under a foreign key deferred to commit. Every statement of each refused
// transaction runs; the commit is what refuses it, and nothing of it remains.
func TestActiveVersionAlwaysExists(t *testing.T) {
	d, _ := openTest(t)
	for _, c := range []struct {
		name  string
		stmts []string
	}{
		{"a form with no version", []string{
			`INSERT INTO forms (id, key, title, active_version, created_at, updated_at) VALUES (2, 'lonely', 'L', 1, 1, 1)`,
		}},
		{"a form naming a version it does not have", []string{
			`INSERT INTO forms (id, key, title, active_version, created_at, updated_at) VALUES (2, 'skewed', 'S', 2, 1, 1)`,
			`INSERT INTO form_versions (form_id, version, schema_json, created_at) VALUES (2, 1, '{"fields":[]}', 1)`,
		}},
		{"an update to a missing version", []string{
			`UPDATE forms SET active_version = 7 WHERE key = 'registration'`,
		}},
	} {
		err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			for _, s := range c.stmts {
				if _, err := tx.ExecContext(t.Context(), s); err != nil {
					t.Errorf("%s: %s was refused before commit: %v", c.name, s, err)
					return err
				}
			}
			return nil
		})
		if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
			t.Errorf("%s: commit gave %v, want a foreign key failure", c.name, err)
		}
		if n := oneValue[int64](t, d, `SELECT count(*) FROM forms WHERE id <> 1`); n != 0 {
			t.Errorf("%s: %d forms remain after the refused commit", c.name, n)
		}
		if n := oneValue[int64](t, d, `SELECT count(*) FROM form_versions WHERE form_id <> 1`); n != 0 {
			t.Errorf("%s: %d form versions remain after the refused commit", c.name, n)
		}
		if v := oneValue[int64](t, d, `SELECT active_version FROM forms WHERE key = 'registration'`); v != 1 {
			t.Errorf("%s: registration's active_version is %d after the refused commit", c.name, v)
		}
	}

	if err := exec(t, d, `INSERT INTO forms (id, key, title, created_at, updated_at) VALUES (2, 'nulled', 'N', 1, 1)`); err == nil {
		t.Error("a form with a null active_version was accepted")
	}

	// A form and its first version, together, commit.
	execAll(t, d,
		`INSERT INTO forms (id, key, title, active_version, created_at, updated_at) VALUES (2, 'report', 'Report', 1, 1, 1)`,
		`INSERT INTO form_versions (form_id, version, schema_json, created_at) VALUES (2, 1, '{"fields":[]}', 1)`,
	)
	if v := oneValue[int64](t, d, `SELECT active_version FROM forms WHERE key = 'report'`); v != 1 {
		t.Errorf("the report form's active_version is %d, want 1", v)
	}

	// A key is lowercase ASCII, digits and _, starting with a letter. Each
	// form comes with its version, so only the key can refuse it.
	for i, key := range []string{"Upper", "1st", "has-dash", "", "a b", "_x", "ok_key2"} {
		id := 10 + i
		err := try(t, d, func(_ *dbq.Queries, tx *sql.Tx) error {
			if _, err := tx.ExecContext(t.Context(), `INSERT INTO forms (id, key, title, active_version, created_at, updated_at)
				VALUES (?, ?, 'x', 1, 1, 1)`, id, key); err != nil {
				return err
			}
			_, err := tx.ExecContext(t.Context(), `INSERT INTO form_versions (form_id, version, schema_json, created_at)
				VALUES (?, 1, '{"fields":[]}', 1)`, id)
			return err
		})
		if ok := key == "ok_key2"; ok != (err == nil) {
			t.Errorf("form key %q: %v", key, err)
		}
	}
}

// TestApplicationStatusIsConstrained is AC3: the four statuses and no
// other, each with decision fields that agree with it.
func TestApplicationStatusIsConstrained(t *testing.T) {
	d, _ := openTest(t)
	member, mod := newUser(t, d, "member"), newUser(t, d, "mod")
	type row struct {
		status       string
		user, by, at any
		note         any
		ok           bool
		name         string
	}
	for _, c := range []row{
		{"pending", nil, nil, nil, nil, true, "pending, undecided"},
		{"approved", member, mod, 5, "welcome", true, "approved with its member"},
		{"approved", member, mod, 5, nil, true, "approved without a note"},
		{"denied", nil, mod, 5, "not now", true, "denied"},
		{"withdrawn", nil, nil, 5, nil, true, "withdrawn"},

		// Decision fields that no other CHECK objects to, so only the
		// status list can refuse these.
		{"rejected", nil, nil, 5, nil, false, "an undocumented status"},
		{"Pending", nil, nil, 5, nil, false, "a status in the wrong case"},
		{"pending", nil, nil, 5, nil, false, "pending with decided_at"},
		{"pending", nil, nil, nil, "hm", false, "pending with a note"},
		{"pending", nil, mod, nil, nil, false, "pending with decided_by"},
		{"pending", member, nil, nil, nil, false, "pending with a user"},
		{"approved", nil, mod, 5, nil, false, "approved without user_id"},
		{"approved", member, mod, nil, nil, false, "approved without decided_at"},
		{"denied", member, mod, 5, nil, false, "denied with a user"},
		{"denied", nil, mod, nil, nil, false, "denied without decided_at"},
		{"withdrawn", nil, mod, 5, nil, false, "withdrawn with decided_by"},
		{"withdrawn", nil, nil, 5, "bye", false, "withdrawn with a note"},
		{"withdrawn", nil, nil, nil, nil, false, "withdrawn without decided_at"},
		{"withdrawn", member, nil, 5, nil, false, "withdrawn with a user"},
	} {
		err := exec(t, d, `INSERT INTO applications (form_id, form_version, answers_json, created_at,
			status, user_id, decided_by, decided_at, decision_note) VALUES (1, 1, '{}', 1, ?, ?, ?, ?, ?)`,
			c.status, c.user, c.by, c.at, c.note)
		if c.ok && err != nil {
			t.Errorf("%s was refused: %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s was accepted", c.name)
		}
	}

	// A CHECK runs per statement, so an approval sets status and user_id in
	// one UPDATE, after the user exists; approving without the user fails.
	app := newApplication(t, d, sql.NullInt64{})
	if err := exec(t, d, `UPDATE applications SET status = 'approved', decided_by = ?, decided_at = 5 WHERE id = ?`,
		mod, app); err == nil {
		t.Error("an UPDATE to approved without a user_id was accepted")
	}
	mustExec(t, d, `UPDATE applications SET status = 'approved', user_id = ?, decided_by = ?, decided_at = 5 WHERE id = ?`,
		member, mod, app)
}

// TestOnePendingApplicationPerIdentity is AC5: the partial unique index
// allows one pending application per identity, beside any number of
// decided ones.
func TestOnePendingApplicationPerIdentity(t *testing.T) {
	d, _ := openTest(t)
	id := sql.NullInt64{Int64: newIdentity(t, d, sql.NullInt64{}, "applicant"), Valid: true}
	other := sql.NullInt64{Int64: newIdentity(t, d, sql.NullInt64{}, "other"), Valid: true}
	first := newApplication(t, d, id)
	ins := `INSERT INTO applications (identity_id, form_id, form_version, answers_json, created_at) VALUES (?, 1, 1, '{}', 2)`
	if err := exec(t, d, ins, id); err == nil {
		t.Fatal("a second pending application for one identity was accepted")
	}
	mustExec(t, d, ins, other)
	mustExec(t, d, `UPDATE applications SET status = 'withdrawn', decided_at = 3 WHERE id = ?`, first)
	mustExec(t, d, ins, id)
	if err := exec(t, d, ins, id); err == nil {
		t.Error("a pending application beside a withdrawn and a pending one was accepted")
	}
	// Reopening the withdrawn one would make two pending.
	if err := exec(t, d, `UPDATE applications SET status = 'pending', decided_at = NULL WHERE id = ?`, first); err == nil {
		t.Error("reopening a withdrawn application beside a pending one was accepted")
	}
}

// TestBlockedIdentityHasNoUser is AC4: a block keeps user_id NULL, however
// the row is written.
func TestBlockedIdentityHasNoUser(t *testing.T) {
	d, _ := openTest(t)
	member, mod := newUser(t, d, "member"), newUser(t, d, "mod")
	applicant := newIdentity(t, d, sql.NullInt64{}, "applicant")
	linked := newIdentity(t, d, sql.NullInt64{Int64: member, Valid: true}, "linked")

	mustExec(t, d, `UPDATE identities SET blocked_at = 5, blocked_by = ? WHERE id = ?`, mod, applicant)
	if err := exec(t, d, `UPDATE identities SET user_id = ? WHERE id = ?`, member, applicant); err == nil {
		t.Error("a blocked identity was given a user")
	}
	if err := exec(t, d, `UPDATE identities SET blocked_at = 5, blocked_by = ? WHERE id = ?`, mod, linked); err == nil {
		t.Error("an identity with a user was blocked")
	}
	fresh := newIdentity(t, d, sql.NullInt64{}, "fresh")
	if err := exec(t, d, `UPDATE identities SET user_id = ?, blocked_at = 5 WHERE id = ?`, member, fresh); err == nil {
		t.Error("an identity was given a user and blocked in one statement")
	}
	if err := exec(t, d, `INSERT INTO identities (user_id, provider_key, subject, issuer, created_at, updated_at, blocked_at)
		VALUES (?, 'main', 'born-blocked', 'https://idp.example', 1, 1, 5)`, member); err == nil {
		t.Error("an identity was inserted with a user and a block")
	}
	mustExec(t, d, `INSERT INTO identities (provider_key, subject, issuer, created_at, updated_at, blocked_at, blocked_by)
		VALUES ('main', 'blocked-at-once', 'https://idp.example', 1, 1, 5, ?)`, mod)

	// Unblocking clears both columns and passes the triggers; the identity
	// can then be linked.
	mustExec(t, d, `UPDATE identities SET blocked_at = NULL, blocked_by = NULL WHERE id = ?`, applicant)
	mustExec(t, d, `UPDATE identities SET user_id = ? WHERE id = ?`, member, applicant)
}

// TestBlockedIdentityCannotApply: a block refuses re-applications in the
// schema (review 1373). Denying and blocking pass in either order, and
// unblocking lets the identity apply again.
func TestBlockedIdentityCannotApply(t *testing.T) {
	d, _ := openTest(t)
	mod := newUser(t, d, "mod")
	ident := sql.NullInt64{Int64: newIdentity(t, d, sql.NullInt64{}, "applicant"), Valid: true}
	other := sql.NullInt64{Int64: newIdentity(t, d, sql.NullInt64{}, "other"), Valid: true}
	first := newApplication(t, d, ident)
	second := newApplication(t, d, other)

	// Block, then deny, in one transaction; the other order is below.
	execAll(t, d,
		`UPDATE identities SET blocked_at = 5, blocked_by = `+itoa(mod)+` WHERE id = `+itoa(ident.Int64),
		`UPDATE applications SET status = 'denied', decided_by = `+itoa(mod)+`, decided_at = 5 WHERE id = `+itoa(first),
	)
	ins := `INSERT INTO applications (identity_id, form_id, form_version, answers_json, created_at) VALUES (?, 1, 1, '{}', 6)`
	if err := exec(t, d, ins, ident); err == nil || !strings.Contains(err.Error(), "blocked identity cannot apply") {
		t.Errorf("a blocked identity applied again: %v", err)
	}
	if err := exec(t, d, `UPDATE applications SET status = 'pending', decided_by = NULL, decided_at = NULL WHERE id = ?`,
		first); err == nil || !strings.Contains(err.Error(), "blocked identity cannot apply") {
		t.Errorf("a blocked identity's denied application was reopened: %v", err)
	}
	// Moving another identity's pending application onto the blocked one.
	if err := exec(t, d, `UPDATE applications SET identity_id = ? WHERE id = ?`, ident, second); err == nil ||
		!strings.Contains(err.Error(), "blocked identity cannot apply") {
		t.Errorf("a pending application was moved onto a blocked identity: %v", err)
	}

	// Deny, then block.
	execAll(t, d,
		`UPDATE applications SET status = 'denied', decided_by = `+itoa(mod)+`, decided_at = 7 WHERE id = `+itoa(second),
		`UPDATE identities SET blocked_at = 7, blocked_by = `+itoa(mod)+` WHERE id = `+itoa(other.Int64),
	)

	mustExec(t, d, `UPDATE identities SET blocked_at = NULL, blocked_by = NULL WHERE id = ?`, ident)
	mustExec(t, d, ins, ident)
}

// TestApplicationOutlivesItsIdentity: identities are removed (account
// linking, anonymization), and the application stays with a null identity.
func TestApplicationOutlivesItsIdentity(t *testing.T) {
	d, _ := openTest(t)
	mod := newUser(t, d, "mod")
	identity := newIdentity(t, d, sql.NullInt64{}, "leaving")
	app := newApplication(t, d, sql.NullInt64{Int64: identity, Valid: true})
	mustExec(t, d, `INSERT INTO application_notes (application_id, author_id, body, created_at) VALUES (?, ?, 'seen before', 2)`,
		app, mod)
	mustExec(t, d, `DELETE FROM identities WHERE id = ?`, identity)

	got := oneValue[sql.NullInt64](t, d, `SELECT identity_id FROM applications WHERE id = ?`, app)
	if got.Valid {
		t.Errorf("the application still names identity %d after it was removed", got.Int64)
	}
	if a := oneValue[string](t, d, `SELECT answers_json FROM applications WHERE id = ?`, app); a != `{"who":"a friend"}` {
		t.Errorf("the application's answers read %q after its identity was removed", a)
	}
	if n := oneValue[int64](t, d, `SELECT count(*) FROM application_notes WHERE application_id = ?`, app); n != 1 {
		t.Errorf("%d notes remain on the application, want 1", n)
	}
	// Two applications whose identities are gone do not collide on the
	// one-pending index.
	newApplication(t, d, sql.NullInt64{})
}

// TestApplicationNotificationsNameTheApplication is AC6's second half: the
// two application kinds carry an application_id, and no other kind does.
func TestApplicationNotificationsNameTheApplication(t *testing.T) {
	d, _ := openTest(t)
	u := newUser(t, d, "notified")
	app := newApplication(t, d, sql.NullInt64{})
	ins := `INSERT INTO notifications (user_id, kind, at, application_id) VALUES (?, ?, 1, ?)`
	for _, c := range []struct {
		kind string
		app  any
		ok   bool
	}{
		{"new_application", app, true},
		{"application_decided", app, true},
		{"new_application", nil, false},
		{"application_decided", nil, false},
		{"reply_to_you", app, false},
		{"mention", app, false},
		{"reply_to_you", nil, true},
		{"new_application", app + 1000, false}, // no such application
	} {
		err := exec(t, d, ins, u, c.kind, c.app)
		if c.ok && err != nil {
			t.Errorf("%s with application %v was refused: %v", c.kind, c.app, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s with application %v was accepted", c.kind, c.app)
		}
	}
	for _, stmt := range []string{
		`UPDATE notifications SET application_id = NULL WHERE kind = 'new_application'`,
		`UPDATE notifications SET kind = 'mention' WHERE kind = 'application_decided'`,
		`UPDATE notifications SET kind = 'new_application' WHERE kind = 'reply_to_you'`,
		`UPDATE notifications SET application_id = (SELECT max(id) FROM applications) WHERE kind = 'reply_to_you'`,
	} {
		if err := exec(t, d, stmt); err == nil {
			t.Errorf("%s was accepted", stmt)
		}
	}
	mustExec(t, d, `UPDATE notifications SET read_at = 2`)
}

// TestApplicationNotesAreBounded: a note is non-empty text of at most 2000
// characters, on an application that exists, by a user who exists.
func TestApplicationNotesAreBounded(t *testing.T) {
	d, _ := openTest(t)
	mod := newUser(t, d, "mod")
	app := newApplication(t, d, sql.NullInt64{})
	ins := `INSERT INTO application_notes (application_id, author_id, body, created_at) VALUES (?, ?, ?, 1)`
	mustExec(t, d, ins, app, mod, "x")
	mustExec(t, d, ins, app, mod, strings.Repeat("é", 2000))
	for _, c := range []struct {
		name      string
		app, user int64
		body      string
	}{
		{"an empty note", app, mod, ""},
		{"a note of 2001 characters", app, mod, strings.Repeat("x", 2001)},
		{"a note on no application", app + 1000, mod, "x"},
		{"a note by nobody", app, mod + 1000, "x"},
	} {
		if err := exec(t, d, ins, c.app, c.user, c.body); err == nil {
			t.Errorf("%s was accepted", c.name)
		}
	}
}

// TestRegistrationFormIsSeeded is AC6's first half: a fresh install has the
// registration form at version 1, holding docs/05's example fields without
// the illustrative select. TKT-01M29CP2KE holds it to the schema's rules once
// the parser exists.
func TestRegistrationFormIsSeeded(t *testing.T) {
	d, _ := openTest(t)
	var active int64
	var schema string
	var createdBy sql.NullInt64
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT f.active_version, v.schema_json, v.created_by
			FROM forms f JOIN form_versions v ON v.form_id = f.id AND v.version = f.active_version
			WHERE f.key = 'registration'`).Scan(&active, &schema, &createdBy)
	}); err != nil {
		t.Fatal(err)
	}
	if active != 1 || createdBy.Valid {
		t.Errorf("registration is at version %d created by %v, want 1 created by nobody", active, createdBy)
	}
	var form struct {
		Title  string `json:"title"`
		Fields []struct {
			Key      string `json:"key"`
			Type     string `json:"type"`
			Required bool   `json:"required"`
		} `json:"fields"`
	}
	if err := json.Unmarshal([]byte(schema), &form); err != nil {
		t.Fatalf("the seeded schema is not JSON: %v\n%s", err, schema)
	}
	var got []string
	for _, f := range form.Fields {
		got = append(got, f.Key+":"+f.Type+":"+map[bool]string{true: "required", false: "optional"}[f.Required])
	}
	want := []string{"name:text:required", "email:email:optional", "who:textarea:required", "rules:checkbox:required"}
	if form.Title == "" || !slices.Equal(got, want) {
		t.Errorf("the seeded form is %q with fields %v, want a title and %v", form.Title, got, want)
	}
}

// TestMigrationRemovesUnnamedApplicationNotifications: the application kinds
// existed before 0011 without a column to name their application. 0011
// removes any such row, so the rule holds for old rows as well as new ones
// (review 1373), and it leaves every other notification alone.
func TestMigrationRemovesUnnamedApplicationNotifications(t *testing.T) {
	d, _ := openUnmigrated(t)
	if err := d.migrate(t.Context(), migrationsBefore(t, 11), "m"); err != nil {
		t.Fatal(err)
	}
	u := itoa(createUser(t, d, "notified"))
	execAll(t, d,
		`INSERT INTO notifications (user_id, kind, at) VALUES (`+u+`, 'new_application', 1)`,
		`INSERT INTO notifications (user_id, kind, at) VALUES (`+u+`, 'application_decided', 2)`,
		`INSERT INTO notifications (user_id, kind, thread_id, ordinal, at) VALUES (`+u+`, 'mention', 42, 3, 3)`,
	)
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	var left []string
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		left = rows(t, tx, `SELECT kind, thread_id, ordinal, application_id FROM notifications ORDER BY id`)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"[mention 42 3 <nil>]"}; !slices.Equal(left, want) {
		t.Errorf("after 0011 the notifications are %v, want %v", left, want)
	}
}
