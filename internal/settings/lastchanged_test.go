package settings

import (
	"database/sql"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// TestValueLastChanged is the settings half of TestSettingLastChanged
// (TKT-01M29CXK2H): UpdatedAt and UpdatedBy come from the row, for each way
// a key can stand. Set from a request, both are set. Set from the command
// line, as settingSet in cmd/ketju sends it, UpdatedBy is 0 and UpdatedAt
// the time. Never set, both are 0.
func TestValueLastChanged(t *testing.T) {
	d := openDB(t)
	if err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO users (id, handle, display_name, status, role, kind,
			prefs_json, created_at, updated_at) VALUES (4, 'dana', 'Dana', 'active', 'admin', 'human', '{}', 1, 1)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	screen, cli := now.Add(time.Minute), now.Add(2*time.Minute)
	if err := SetBy(t.Context(), d, "site_name", json.RawMessage(`"Club"`),
		Change{Actor: 4, IP: "192.0.2.7", At: screen}); err != nil {
		t.Fatal(err)
	}
	if err := SetBy(t.Context(), d, "registration.mode", json.RawMessage(`"open"`),
		Change{Details: map[string]string{"via": "cli"}, At: cli}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, key string
		at, by    int64
	}{
		{"set from the screen", "site_name", screen.UnixMilli(), 4},
		{"set from the command line", "registration.mode", cli.UnixMilli(), 0},
		{"never set", "public_edit_history", 0, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			v, err := Lookup(t.Context(), d, c.key)
			if err != nil {
				t.Fatal(err)
			}
			if v.UpdatedAt != c.at || v.UpdatedBy != c.by {
				t.Errorf("Lookup's %s: updated at %d by %d, want at %d by %d", c.key, v.UpdatedAt, v.UpdatedBy, c.at, c.by)
			}
			vs, err := Current(t.Context(), d)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, w := range vs {
				if w.Name != c.key {
					continue
				}
				found = true
				if w.UpdatedAt != c.at || w.UpdatedBy != c.by {
					t.Errorf("Current's %s: updated at %d by %d, want at %d by %d", c.key, w.UpdatedAt, w.UpdatedBy, c.at, c.by)
				}
			}
			if !found {
				t.Errorf("Current has no %s", c.key)
			}
		})
	}
}

// TestChoices: a key with a closed list carries it, in the key's order, so
// a screen can draw a select; every other key has none.
func TestChoices(t *testing.T) {
	for _, s := range All() {
		var want []string
		if s.Name == "registration.mode" {
			want = []string{"open", "approval"}
		}
		if !slices.Equal(s.Choices, want) {
			t.Errorf("%s: choices %q, want %q", s.Name, s.Choices, want)
		}
	}
	// The listing's copy is its own: changing it changes no later listing.
	s, err := Describe("registration.mode")
	if err != nil {
		t.Fatal(err)
	}
	s.Choices[0] = "invite"
	if s, _ := Describe("registration.mode"); s.Choices[0] != "open" {
		t.Errorf("a caller changed the registry's choices: %q", s.Choices)
	}
}
