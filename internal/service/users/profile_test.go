package users

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
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
)

// A member's own display name and bio (TKT-01M3V6XVVG, docs/05 "Display
// name and bio").

// profileWorld is a database with one member, alice, and an admin, ada.
func profileWorld(t *testing.T) (*Service, domain.Viewer, domain.Viewer) {
	t.Helper()
	d := dbtest.Open(t, db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	var alice, ada int64
	if err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		if alice, err = q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "alice", DisplayName: "Alice",
			Status: domain.StatusActive, Role: domain.RoleMember, Kind: domain.KindHuman, CreatedAt: 1, UpdatedAt: 1}); err != nil {
			return err
		}
		ada, err = q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "ada", DisplayName: "Ada",
			Status: domain.StatusActive, Role: domain.RoleAdmin, Kind: domain.KindHuman, CreatedAt: 1, UpdatedAt: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	s := &Service{DB: d, Perm: perm.New(d), Now: func() time.Time { return time.UnixMilli(1_760_000_000_000) }}
	return s, domain.Viewer{UserID: alice, Role: domain.RoleMember, Active: true},
		domain.Viewer{UserID: ada, Role: domain.RoleAdmin, Active: true}
}

// raw is v as JSON, for a ProfilePatch field.
func raw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fieldCodes is the field codes of a validation-failed refusal.
func fieldCodes(t *testing.T, err error) map[string]string {
	t.Helper()
	var inv *forms.Invalid
	if !errors.As(err, &inv) {
		t.Fatalf("not a field refusal: %v", err)
	}
	out := map[string]string{}
	for _, f := range inv.Fields {
		out[f.Field] = f.Code
	}
	return out
}

// TestUpdateProfileValidates is criterion 1's service half: each code for
// each field, at the boundary, counted in characters rather than bytes, and
// a refusal writes nothing, the other field's valid value included.
func TestUpdateProfileValidates(t *testing.T) {
	s, alice, _ := profileWorld(t)
	// ä is two bytes in UTF-8, so a byte count would refuse these.
	name100 := strings.Repeat("ä", 100)
	bio2000 := strings.Repeat("ä", 1999) + "\n"
	got, err := s.UpdateProfile(t.Context(), alice, ProfilePatch{DisplayName: raw(t, "  "+name100+" "), Bio: raw(t, bio2000)})
	if err != nil {
		t.Fatalf("100 and 2000 characters: %v", err)
	}
	if got.DisplayName != name100 || got.Bio != bio2000 {
		t.Fatalf("the name is trimmed and the bio stored as sent: %+v", got)
	}

	cases := []struct {
		name string
		p    ProfilePatch
		want map[string]string
	}{
		{"101 characters", ProfilePatch{DisplayName: raw(t, name100+"ä")}, map[string]string{"display_name": forms.CodeTooLong}},
		{"2001 characters", ProfilePatch{Bio: raw(t, bio2000+"ä")}, map[string]string{"bio": forms.CodeTooLong}},
		{"a null name", ProfilePatch{DisplayName: json.RawMessage(`null`)}, map[string]string{"display_name": forms.CodeWrongType}},
		{"a number for a bio", ProfilePatch{Bio: json.RawMessage(`3`)}, map[string]string{"bio": forms.CodeWrongType}},
		{"a control character in a name", ProfilePatch{DisplayName: raw(t, "Al\x07ice")},
			map[string]string{"display_name": forms.CodeInvalidText}},
		{"a newline in a name", ProfilePatch{DisplayName: raw(t, "Al\nice")}, map[string]string{"display_name": forms.CodeInvalidText}},
		{"a control character in a bio", ProfilePatch{Bio: raw(t, "line\x00")}, map[string]string{"bio": forms.CodeInvalidText}},
		{"one bad, one good", ProfilePatch{DisplayName: raw(t, "Fine"), Bio: json.RawMessage(`false`)},
			map[string]string{"bio": forms.CodeWrongType}},
		{"both bad", ProfilePatch{DisplayName: json.RawMessage(`[]`), Bio: raw(t, bio2000+"x")},
			map[string]string{"display_name": forms.CodeWrongType, "bio": forms.CodeTooLong}},
	}
	for _, c := range cases {
		_, err := s.UpdateProfile(t.Context(), alice, c.p)
		if got := fieldCodes(t, err); len(got) != len(c.want) || got["display_name"] != c.want["display_name"] || got["bio"] != c.want["bio"] {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
		if p, err := s.Profile(t.Context(), alice); err != nil || p.DisplayName != name100 || p.Bio != bio2000 {
			t.Errorf("%s wrote: %+v %v", c.name, p, err)
		}
	}

	// Tab, line feed and carriage return are Markdown's, and pass in a bio.
	if _, err := s.UpdateProfile(t.Context(), alice, ProfilePatch{Bio: raw(t, "a\tb\r\nc")}); err != nil {
		t.Errorf("tab and CRLF in a bio: %v", err)
	}
	// An empty name is stored empty: author blocks then show the handle.
	if got, err := s.UpdateProfile(t.Context(), alice, ProfilePatch{DisplayName: raw(t, "   ")}); err != nil || got.DisplayName != "" {
		t.Errorf("an empty name: %+v %v", got, err)
	}
}

// TestUpdateProfileKeepsTheOtherField: a write of one field keeps the other,
// and an empty patch writes nothing and answers the profile.
func TestUpdateProfileKeepsTheOtherField(t *testing.T) {
	s, alice, _ := profileWorld(t)
	if _, err := s.UpdateProfile(t.Context(), alice, ProfilePatch{Bio: raw(t, "My *bio*.")}); err != nil {
		t.Fatal(err)
	}
	got, err := s.UpdateProfile(t.Context(), alice, ProfilePatch{DisplayName: raw(t, "Alice L.")})
	if err != nil || got.DisplayName != "Alice L." || got.Bio != "My *bio*." {
		t.Fatalf("a name write: %+v %v", got, err)
	}
	got, err = s.UpdateProfile(t.Context(), alice, ProfilePatch{Bio: raw(t, "")})
	if err != nil || got.DisplayName != "Alice L." || got.Bio != "" {
		t.Fatalf("a bio write: %+v %v", got, err)
	}
	if got, err = s.UpdateProfile(t.Context(), alice, ProfilePatch{}); err != nil || got.DisplayName != "Alice L." {
		t.Fatalf("an empty patch: %+v %v", got, err)
	}
}

// TestProfileRefusesStrangers: anonymous is unauthenticated and a pre-user
// forbidden, on both methods.
func TestProfileRefusesStrangers(t *testing.T) {
	s, _, _ := profileWorld(t)
	for _, c := range []struct {
		v    domain.Viewer
		code problem.Code
	}{{domain.Viewer{}, problem.CodeUnauthenticated}, {domain.Viewer{PreUser: true, IdentityID: 1}, problem.CodeForbidden}} {
		if _, err := s.Profile(t.Context(), c.v); problem.FromError(err) != c.code {
			t.Errorf("GET as %+v: %v", c.v, err)
		}
		if _, err := s.UpdateProfile(t.Context(), c.v, ProfilePatch{Bio: raw(t, "x")}); problem.FromError(err) != c.code {
			t.Errorf("PATCH as %+v: %v", c.v, err)
		}
	}
}

// TestDisplayNameRuleIsShared is criterion 2: an agent's creation and a
// member's update accept and refuse the same names.
func TestDisplayNameRuleIsShared(t *testing.T) {
	s, alice, ada := profileWorld(t)
	for i, name := range []string{
		"Plain", "  Trimmed  ", "", strings.Repeat("ä", 100), strings.Repeat("ä", 101), "Bell\x07", "Two\nlines",
		"Tab\there", "​zero-width", "Del\x7f",
	} {
		_, memberErr := s.UpdateProfile(t.Context(), alice, ProfilePatch{DisplayName: raw(t, name)})
		_, agentErr := s.CreateAgent(t.Context(), ada, "192.0.2.1", NewAgent{Handle: "agent-" + string(rune('a'+i)), DisplayName: name})
		if (memberErr == nil) != (agentErr == nil) {
			t.Errorf("%q: the member's update %v, the agent's creation %v", name, memberErr, agentErr)
		}
	}
}
