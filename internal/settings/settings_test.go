package settings

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

func openDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return d
}

var now = time.UnixMilli(1_757_500_000_000)

func rows(t *testing.T, d *db.DB) (settings, audits int) {
	t.Helper()
	if err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		if err := tx.QueryRowContext(t.Context(), `SELECT count(*) FROM settings`).Scan(&settings); err != nil {
			return err
		}
		return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_log`).Scan(&audits)
	}); err != nil {
		t.Fatal(err)
	}
	return settings, audits
}

// TestSettingsUnknownKeyRejected is AC4: an unknown key and an ill-typed or
// out-of-range value are errors, and neither writes a settings row or an
// audit row.
func TestSettingsUnknownKeyRejected(t *testing.T) {
	d := openDB(t)
	for _, c := range []struct {
		key, value string
		want       error
	}{
		{"site_title", `"Ketju"`, ErrUnknownKey},
		{"slow_mode", `0`, ErrUnknownKey},
		{"", `1`, ErrUnknownKey},
		{"site_name", `42`, ErrInvalidValue},
		{"site_name", `""`, ErrInvalidValue},
		{"site_name", `null`, ErrInvalidValue},
		{"site_name", ` null `, ErrInvalidValue},
		// null for keys whose zero value is valid, so only the null check
		// refuses it.
		{"posts.edit_window_seconds", `null`, ErrInvalidValue},
		{"public_edit_history", `null`, ErrInvalidValue},
		{"site_name", `"a" "b"`, ErrInvalidValue},
		{"site_name", `"Ketju"]`, ErrInvalidValue},
		{"flags.auto_hide_threshold", `5}`, ErrInvalidValue},
		{"public_edit_history", `true,`, ErrInvalidValue},
		{"site_name", `"` + strings.Repeat("x", 101) + `"`, ErrInvalidValue},
		{"registration.mode", `"invite"`, ErrInvalidValue},
		{"registration.mode", `"Open"`, ErrInvalidValue},
		{"registration.allow_local_passwords", `"true"`, ErrInvalidValue},
		{"registration.allow_local_passwords", `1`, ErrInvalidValue},
		{"posts.edit_window_seconds", `1.5`, ErrInvalidValue},
		{"posts.edit_window_seconds", `-1`, ErrInvalidValue},
		{"flags.auto_hide_threshold", `0`, ErrInvalidValue},
		{"public_edit_history", `{}`, ErrInvalidValue},
		{"public_edit_history", `not json`, ErrInvalidValue},
	} {
		err := Set(t.Context(), d, c.key, json.RawMessage(c.value), 0, now)
		if !errors.Is(err, c.want) {
			t.Errorf("Set(%q, %s) = %v, want %v", c.key, c.value, err, c.want)
		}
	}
	if s, a := rows(t, d); s != 0 || a != 0 {
		t.Fatalf("refused writes left %d settings rows and %d audit rows", s, a)
	}
}

// TestSettingsRoundTrip: a key with no row reads as its default; a value set
// reads back as its type, is stored canonically, and leaves an audit row
// saying who changed what from what.
func TestSettingsRoundTrip(t *testing.T) {
	d := openDB(t)
	var actor int64
	if err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		actor, err = q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "admin", DisplayName: "Admin",
			Status: "active", Role: "admin", Kind: "human", CreatedAt: 1, UpdatedAt: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if v, err := FlagAutoHideThreshold.Get(t.Context(), d); err != nil || v != 3 {
		t.Fatalf("the default: %d, %v", v, err)
	}
	if v, err := PublicEditHistory.Get(t.Context(), d); err != nil || !v {
		t.Fatalf("the default: %v, %v", v, err)
	}
	if err := Set(t.Context(), d, "flags.auto_hide_threshold", json.RawMessage(" 5 "), actor, now); err != nil {
		t.Fatal(err)
	}
	if err := Set(t.Context(), d, "public_edit_history", json.RawMessage("false"), 0, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := Set(t.Context(), d, "flags.auto_hide_threshold", json.RawMessage(" 7\n"), actor, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if v, err := FlagAutoHideThreshold.Get(t.Context(), d); err != nil || v != 7 {
		t.Fatalf("after setting 7: %d, %v", v, err)
	}
	if v, err := PublicEditHistory.Get(t.Context(), d); err != nil || v {
		t.Fatalf("after setting false: %v, %v", v, err)
	}

	var (
		stored  string
		by      sql.NullInt64
		details []string
		actors  []sql.NullInt64
	)
	if err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		s, err := q.GetSetting(t.Context(), "flags.auto_hide_threshold")
		stored, by = s.ValueJson, s.UpdatedBy
		if err != nil {
			return err
		}
		r, err := tx.QueryContext(t.Context(),
			`SELECT actor_id, details_json FROM audit_log WHERE action = 'setting.changed' ORDER BY id`)
		if err != nil {
			return err
		}
		defer r.Close()
		for r.Next() {
			var a sql.NullInt64
			var dj string
			if err := r.Scan(&a, &dj); err != nil {
				return err
			}
			actors, details = append(actors, a), append(details, dj)
		}
		return r.Err()
	}); err != nil {
		t.Fatal(err)
	}
	if stored != "7" || by.Int64 != actor {
		t.Errorf("stored %q by %v", stored, by)
	}
	want := []string{`{"from":3,"to":5}`, `{"from":true,"to":false}`, `{"from":5,"to":7}`}
	if strings.Join(details, " ") != strings.Join(want, " ") {
		t.Errorf("audit details %v, want %v", details, want)
	}
	if !actors[0].Valid || actors[0].Int64 != actor || actors[1].Valid {
		t.Errorf("audit actors %v: want the admin, then the system (null)", actors)
	}
}

// TestStoredValueThatNoLongerDecodes: a row written around Set is an error
// on read, not silently the default.
func TestStoredValueThatNoLongerDecodes(t *testing.T) {
	d := openDB(t)
	if err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		return q.PutSetting(t.Context(), dbq.PutSettingParams{Key: "flags.auto_hide_threshold", ValueJson: `"three"`, UpdatedAt: 1})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := FlagAutoHideThreshold.Get(t.Context(), d); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("an ill-typed stored value read as %v", err)
	}
}

// TestKeysDocumented: every key is in docs/04's list, with its default, and
// the list names no key that does not exist.
func TestKeysDocumented(t *testing.T) {
	b, err := os.ReadFile("../../docs/04-data-model.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	start := strings.Index(doc, "<!-- settings:begin -->")
	end := strings.Index(doc, "<!-- settings:end -->")
	if start < 0 || end < start {
		t.Fatal("docs/04 has no settings:begin/settings:end block")
	}
	block := doc[start:end]
	listed := 0
	for _, line := range strings.Split(block, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(line, "|")
		name := strings.Trim(strings.TrimSpace(cells[1]), "`")
		def := strings.Trim(strings.TrimSpace(cells[2]), "`")
		s, ok := registry[name]
		switch {
		case !ok:
			t.Errorf("docs/04 lists %q, which is not a key", name)
		case s.encoded != def:
			t.Errorf("docs/04 gives %s the default %s; the code has %s", name, def, s.encoded)
		}
		listed++
	}
	if listed != len(registry) {
		t.Errorf("docs/04 lists %d keys; there are %d", listed, len(registry))
	}
}

// TestAllIsSorted: the listing is in name order and complete.
func TestAllIsSorted(t *testing.T) {
	all := All()
	if len(all) != len(registry) {
		t.Fatalf("All has %d keys; the registry %d", len(all), len(registry))
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].Name >= all[i].Name {
			t.Fatalf("%s before %s", all[i-1].Name, all[i].Name)
		}
	}
}

// TestGetFailureIsNotTheDefault: a database that cannot answer is an error,
// not the default; only a missing row means the default.
func TestGetFailureIsNotTheDefault(t *testing.T) {
	d := openDB(t)
	d.Close()
	if _, err := PublicEditHistory.Get(t.Context(), d); err == nil {
		t.Fatal("a closed database read as the default")
	}
}

// TestFromText: a text key takes the words as typed; an integer or boolean
// key takes JSON. What FromText lets through, Set still validates.
func TestFromText(t *testing.T) {
	for _, c := range []struct {
		key, text string
		want      string // the JSON stored, or "" when Set refuses it
		err       error  // FromText's own error
	}{
		{"registration.mode", "open", `"open"`, nil},
		{"registration.mode", `"open"`, "", nil}, // the quotes are part of the text
		{"registration.mode", "invite", "", nil},
		{"site_name", "My forum", `"My forum"`, nil},
		{"site_name", `a "quoted" name`, `"a \"quoted\" name"`, nil},
		{"site_name", "", "", nil},
		{"flags.auto_hide_threshold", "5", `5`, nil},
		{"flags.auto_hide_threshold", " 5 ", `5`, nil},
		{"flags.auto_hide_threshold", "five", "", nil},
		{"flags.auto_hide_threshold", "", "", nil},
		{"public_edit_history", "false", `false`, nil},
		{"public_edit_history", "no", "", nil},
		{"public_edit_history", `"true"`, "", nil},
		{"registration.modes", "open", "", ErrUnknownKey},
	} {
		raw, err := FromText(c.key, c.text)
		if !errors.Is(err, c.err) {
			t.Errorf("FromText(%q, %q) = %v, want %v", c.key, c.text, err, c.err)
			continue
		}
		if err != nil {
			continue
		}
		d := openDB(t)
		err = Set(t.Context(), d, c.key, raw, 0, now)
		if c.want == "" {
			if !errors.Is(err, ErrInvalidValue) {
				t.Errorf("%s = %q: Set = %v, want an invalid value", c.key, c.text, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s = %q: %v", c.key, c.text, err)
			continue
		}
		if v, err := Lookup(t.Context(), d, c.key); err != nil || v.JSON != c.want {
			t.Errorf("%s = %q stored %s (%v), want %s", c.key, c.text, v.JSON, err, c.want)
		}
	}
}

// TestRefusalsSayWhatIsAllowed: an operator refused at the command line is
// told the keys there are, or what the key takes.
func TestRefusalsSayWhatIsAllowed(t *testing.T) {
	d := openDB(t)
	for _, c := range []struct {
		key, value, want string
	}{
		{"registration.mode", `"invite"`, `it takes one of "open", "approval"`},
		{"flags.auto_hide_threshold", `0`, "it takes a whole number, 1 or more"},
		{"slow_mode.default_seconds", `"x"`, "it takes a whole number, 0 or more"},
		{"public_edit_history", `yes`, "it takes true or false"},
		{"site_name", `""`, "it takes text of 1 to 100 characters"},
		{"site_nam", `"x"`, "the keys are flags.auto_hide_threshold, posts.delete_window_seconds"},
	} {
		err := Set(t.Context(), d, c.key, json.RawMessage(c.value), 0, now)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Set(%s, %s) = %v, want it to say %q", c.key, c.value, err, c.want)
		}
	}
}

// TestSetByDetails: the details go into the audit row beside from and to,
// and cannot replace them.
func TestSetByDetails(t *testing.T) {
	d := openDB(t)
	if err := SetBy(t.Context(), d, "registration.mode", json.RawMessage(`"open"`),
		Change{Details: map[string]string{"via": "cli", "to": "forged"}, At: now}); err != nil {
		t.Fatal(err)
	}
	var actor sql.NullInt64
	var details string
	var at int64
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT actor_id, details_json, at FROM audit_log
			WHERE action = 'setting.changed' AND target_id = 'registration.mode'`).Scan(&actor, &details, &at)
	}); err != nil {
		t.Fatal(err)
	}
	if actor.Valid || details != `{"from":"approval","to":"open","via":"cli"}` || at != now.UnixMilli() {
		t.Errorf("audit row: actor %v, details %s, at %d", actor, details, at)
	}
}

// TestCurrent: every key, in name order, with its value and whether it is
// the default; a row that sets a key back to its default counts as the
// default.
func TestCurrent(t *testing.T) {
	d := openDB(t)
	for _, kv := range [][2]string{
		{"registration.mode", `"open"`},
		{"flags.auto_hide_threshold", `5`},
		{"flags.auto_hide_threshold", `3`},
	} {
		if err := Set(t.Context(), d, kv[0], json.RawMessage(kv[1]), 0, now); err != nil {
			t.Fatal(err)
		}
	}
	vs, err := Current(t.Context(), d)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != len(registry) {
		t.Fatalf("%d values for %d keys", len(vs), len(registry))
	}
	got := map[string]Value{}
	for i, v := range vs {
		if i > 0 && vs[i-1].Name >= v.Name {
			t.Errorf("%s before %s", vs[i-1].Name, v.Name)
		}
		got[v.Name] = v
	}
	if v := got["registration.mode"]; v.JSON != `"open"` || v.IsDefault || v.Text() != "open" || v.Default != `"approval"` {
		t.Errorf("registration.mode: %+v, text %q", v, v.Text())
	}
	if v := got["flags.auto_hide_threshold"]; v.JSON != `3` || !v.IsDefault || v.Text() != "3" {
		t.Errorf("flags.auto_hide_threshold: %+v", v)
	}
	if v := got["site_name"]; v.JSON != `"Ketju"` || !v.IsDefault || v.Text() != "Ketju" || v.Kind != KindText {
		t.Errorf("site_name: %+v", v)
	}
	if v := got["public_edit_history"]; v.Kind != KindBoolean || v.Allowed != "true or false" {
		t.Errorf("public_edit_history: %+v", v)
	}

	if _, err := Lookup(t.Context(), d, "nope"); !errors.Is(err, ErrUnknownKey) {
		t.Errorf("Lookup(nope) = %v", err)
	}

	// A row written around Set is an error naming the key, not the default.
	if err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		return q.PutSetting(t.Context(), dbq.PutSettingParams{Key: "site_name", ValueJson: `7`, UpdatedAt: 1})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Current(t.Context(), d); !errors.Is(err, ErrInvalidValue) || !strings.Contains(err.Error(), "site_name") {
		t.Errorf("Current over a bad row = %v", err)
	}
	if _, err := Lookup(t.Context(), d, "site_name"); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("Lookup over a bad row = %v", err)
	}
}
