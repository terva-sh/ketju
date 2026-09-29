package forms

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// TestSchemaRules is AC6's rules half: each broken rule is refused with its
// path and code.
func TestSchemaRules(t *testing.T) {
	field := func(f string) string { return `{"title":"T","fields":[` + f + `]}` }
	long := func(n int) string { return strings.Repeat("x", n) }
	cases := []struct {
		name, schema string
		want         []string
	}{
		{"not JSON", `{"title":`, []string{" malformed"}},
		{"not an object", `[1]`, []string{" wrong_type"}},
		{"null", `null`, []string{" wrong_type"}},
		{"trailing JSON", `{"title":"T","fields":[{"key":"a","type":"text","label":"L"}]} {}`, []string{" malformed"}},
		{"an unknown property", `{"title":"T","extra":1,"fields":[{"key":"a","type":"text","label":"L"}]}`, []string{" unknown_property"}},
		{"no title", `{"fields":[{"key":"a","type":"text","label":"L"}]}`, []string{"title required"}},
		{"a blank title", `{"title":"  ","fields":[{"key":"a","type":"text","label":"L"}]}`, []string{"title required"}},
		{"a long title", `{"title":"` + long(201) + `","fields":[{"key":"a","type":"text","label":"L"}]}`, []string{"title too_long"}},
		{"a title with a newline", `{"title":"a\nb","fields":[{"key":"a","type":"text","label":"L"}]}`, []string{"title invalid_text"}},
		{"a long intro", `{"title":"T","intro_markdown":"` + long(10001) + `","fields":[{"key":"a","type":"text","label":"L"}]}`, []string{"intro_markdown too_long"}},
		{"a title of the wrong type", `{"title":5,"fields":[{"key":"a","type":"text","label":"L"}]}`, []string{"title wrong_type"}},
		{"no fields", `{"title":"T","fields":[]}`, []string{"fields required"}},
		{"fields missing", `{"title":"T"}`, []string{"fields required"}},
		{"fields not a list", `{"title":"T","fields":{}}`, []string{"fields wrong_type"}},
		{"too many fields", `{"title":"T","fields":[` + strings.Repeat(`{"key":"a","type":"checkbox","label":"L"},`, 50) + `{"key":"b","type":"checkbox","label":"L"}]}`, []string{"fields too_many"}},
		{"a field that is not an object", field(`"text"`), []string{"fields.0 wrong_type"}},
		{"a duplicate key", `{"title":"T","fields":[{"key":"a","type":"text","label":"L"},{"key":"a","type":"checkbox","label":"L"}]}`, []string{"fields.1.key duplicate_key"}},
		{"a duplicate key on a field with another error", `{"title":"T","fields":[{"key":"a","type":"text","label":"L"},{"key":"a","type":"date","label":"L"}]}`,
			[]string{"fields.1.type unknown_type", "fields.1.key duplicate_key"}},
		{"a duplicate of a field with another error", `{"title":"T","fields":[{"key":"a","type":"text"},{"key":"a","type":"text","label":"L"}]}`,
			[]string{"fields.0.label required", "fields.1.key duplicate_key"}},
		{"a bad key", field(`{"key":"Name","type":"text","label":"L"}`), []string{"fields.0.key invalid_key"}},
		{"a key too long", field(`{"key":"` + long(41) + `","type":"text","label":"L"}`), []string{"fields.0.key invalid_key"}},
		{"no key", field(`{"type":"text","label":"L"}`), []string{"fields.0.key required"}},
		{"no type", field(`{"key":"a","label":"L"}`), []string{"fields.0.type required"}},
		{"an unknown type", field(`{"key":"a","type":"date","label":"L"}`), []string{"fields.0.type unknown_type"}},
		{"an unknown field property", field(`{"key":"a","type":"text","label":"L","placeholder":"x"}`), []string{"fields.0 unknown_property"}},
		{"no label", field(`{"key":"a","type":"text"}`), []string{"fields.0.label required"}},
		{"a long label", field(`{"key":"a","type":"text","label":"` + long(501) + `"}`), []string{"fields.0.label too_long"}},
		{"options on text", field(`{"key":"a","type":"text","label":"L","options":["x"]}`), []string{"fields.0.options not_allowed"}},
		{"multiple on a checkbox", field(`{"key":"a","type":"checkbox","label":"L","multiple":true}`), []string{"fields.0.multiple not_allowed"}},
		{"max on a checkbox", field(`{"key":"a","type":"checkbox","label":"L","max":3}`), []string{"fields.0.max not_allowed"}},
		{"max over the cap", field(`{"key":"a","type":"text","label":"L","max":201}`), []string{"fields.0.max out_of_range"}},
		{"max of zero", field(`{"key":"a","type":"textarea","label":"L","max":0}`), []string{"fields.0.max out_of_range"}},
		{"max not a number", field(`{"key":"a","type":"text","label":"L","max":"10"}`), []string{"fields.0.max wrong_type"}},
		{"from_claim on a checkbox", field(`{"key":"a","type":"checkbox","label":"L","from_claim":"name"}`), []string{"fields.0.from_claim not_allowed"}},
		{"an unknown claim", field(`{"key":"a","type":"text","label":"L","from_claim":"sub"}`), []string{"fields.0.from_claim unknown_claim"}},
		{"locked without from_claim", field(`{"key":"a","type":"text","label":"L","locked":true}`), []string{"fields.0.from_claim required"}},
		{"locked on a textarea", field(`{"key":"a","type":"textarea","label":"L","locked":true}`), []string{"fields.0.locked not_allowed"}},
		{"a select without options", field(`{"key":"a","type":"select","label":"L"}`), []string{"fields.0.options required"}},
		{"a select with no options", field(`{"key":"a","type":"select","label":"L","options":[]}`), []string{"fields.0.options required"}},
		{"too many options", field(`{"key":"a","type":"select","label":"L","options":[` + strings.Repeat(`"x",`, 100) + `"y"]}`), []string{"fields.0.options too_many"}},
		{"a duplicate option", field(`{"key":"a","type":"select","label":"L","options":["x","y","x"]}`), []string{"fields.0.options.2 duplicate_option"}},
		{"an empty option", field(`{"key":"a","type":"select","label":"L","options":["x",""]}`), []string{"fields.0.options.1 required"}},
		{"a long option", field(`{"key":"a","type":"select","label":"L","options":["` + long(201) + `"]}`), []string{"fields.0.options.0 too_long"}},
		{"options not strings", field(`{"key":"a","type":"select","label":"L","options":[1]}`), []string{"fields.0.options wrong_type"}},
		{"required on a notice", field(`{"key":"a","type":"markdown_notice","markdown":"m","required":true}`), []string{"fields.0.required not_allowed"}},
		{"a notice without markdown", field(`{"key":"a","type":"markdown_notice"}`), []string{"fields.0.markdown required"}},
		{"a long notice", field(`{"key":"a","type":"markdown_notice","markdown":"` + long(10001) + `"}`), []string{"fields.0.markdown too_long"}},
		{"markdown on text", field(`{"key":"a","type":"text","label":"L","markdown":"m"}`), []string{"fields.0.markdown not_allowed"}},
		{"a schema over 64 KiB", `{"title":"` + long(MaxSchemaBytes) + `"}`, []string{" too_long"}},
		// 36 KB as sent, 72 KB once encoding/json escapes each U+2028 to
		//  : the stored form would be one no load accepts.
		{"a schema whose canonical form is over 64 KiB", `{"title":"T","fields":[` +
			`{"key":"a","type":"markdown_notice","markdown":"x` + strings.Repeat(" ", 6000) + `"},` +
			`{"key":"b","type":"markdown_notice","markdown":"x` + strings.Repeat(" ", 6000) + `"}]}`, []string{" too_long"}},
		{"a title with a line separator", `{"title":"a` + " " + `b","fields":[{"key":"a","type":"text","label":"L"}]}`, []string{"title invalid_text"}},
		{"a label with a paragraph separator", field(`{"key":"a","type":"text","label":"a` + " " + `"}`), []string{"fields.0.label invalid_text"}},
		{"an option with a line separator", field(`{"key":"a","type":"select","label":"L","options":["x` + " " + `"]}`), []string{"fields.0.options.0 invalid_text"}},
		{"several at once", `{"title":"","fields":[{"key":"A","type":"text"},{"key":"b","type":"select","label":"L","options":["x","x"]}]}`,
			[]string{"title required", "fields.0.key invalid_key", "fields.0.label required", "fields.1.options.1 duplicate_option"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := Parse([]byte(c.schema))
			if err == nil {
				t.Fatalf("accepted: %+v", s)
			}
			if !errors.Is(err, ErrInvalidSchema) {
				t.Errorf("%v is not ErrInvalidSchema", err)
			}
			if got := codes(t, err); !reflect.DeepEqual(got, c.want) {
				t.Errorf("errors %q, want %q", got, c.want)
			}
		})
	}
}

// TestAValidSchemaParsesAndRoundTrips: every type with every property it
// allows parses, and its canonical form parses back to the same schema.
func TestAValidSchemaParsesAndRoundTrips(t *testing.T) {
	in := `{"fields":[
		{"key":"name","type":"text","label":"Name","from_claim":"name","required":true,"max":80},
		{"key":"mail","type":"email","label":"Email","from_claim":"email","locked":true},
		{"key":"who","type":"textarea","label":"Who?","required":true,"max":1000},
		{"key":"rules","type":"checkbox","label":"Rules","required":true},
		{"key":"likes","type":"select","label":"Likes","options":["Games","Books"],"multiple":true},
		{"key":"note","type":"markdown_notice","label":"","markdown":"Read **this** & <that>."}],
		"intro_markdown":"Hello", "title":"Apply"}`
	s, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Canonical(s)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"title":"Apply","intro_markdown":"Hello","fields":[` +
		`{"key":"name","type":"text","label":"Name","required":true,"max":80,"from_claim":"name"},` +
		`{"key":"mail","type":"email","label":"Email","from_claim":"email","locked":true},` +
		`{"key":"who","type":"textarea","label":"Who?","required":true,"max":1000},` +
		`{"key":"rules","type":"checkbox","label":"Rules","required":true},` +
		`{"key":"likes","type":"select","label":"Likes","options":["Games","Books"],"multiple":true},` +
		`{"key":"note","type":"markdown_notice","markdown":"Read **this** & <that>."}]}`
	if string(b) != want {
		t.Errorf("canonical form\n got %s\nwant %s", b, want)
	}
	back, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, s) {
		t.Errorf("the canonical form parses to %+v, want %+v", back, s)
	}
}

// TestSeededRegistrationFormParses is AC7's first half: version 1 of the
// registration form, as migration 0011 seeds it, parses under the rules.
func TestSeededRegistrationFormParses(t *testing.T) {
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT schema_json FROM form_versions fv JOIN forms f ON f.id = fv.form_id
			WHERE f.key = 'registration' AND fv.version = 1`).Scan(&raw)
	}); err != nil {
		t.Fatal(err)
	}
	s, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("the seeded registration form does not parse: %v", err)
	}
	if len(s.Fields) != 4 {
		t.Errorf("the seed has %d fields, want 4", len(s.Fields))
	}
}

// TestCodesAndTypesAreDocumented is AC7's second half: docs/05 names every
// field type, and docs/07 every error code, and neither names one that does
// not exist.
func TestCodesAndTypesAreDocumented(t *testing.T) {
	read := func(path, begin, end string) []string {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		doc := string(b)
		i, j := strings.Index(doc, begin), strings.Index(doc, end)
		if i < 0 || j < i {
			t.Fatalf("%s has no %s ... %s block", path, begin, end)
		}
		var out []string
		for _, m := range regexp.MustCompile("(?m)^\\| `([a-z_]+)` \\|").FindAllStringSubmatch(doc[i:j], -1) {
			out = append(out, m[1])
		}
		return out
	}
	same := func(what string, doc, code []string) {
		t.Helper()
		for _, c := range code {
			if !strings.Contains(" "+strings.Join(doc, " ")+" ", " "+c+" ") {
				t.Errorf("the documentation does not list the %s %q", what, c)
			}
		}
		for _, d := range doc {
			if !strings.Contains(" "+strings.Join(code, " ")+" ", " "+d+" ") {
				t.Errorf("the documentation lists the %s %q, which does not exist", what, d)
			}
		}
	}
	same("field type", read("../../docs/05-identity-and-accounts.md", "<!-- form-types:begin -->", "<!-- form-types:end -->"), Types())
	same("error code", read("../../docs/07-api-and-clients.md", "<!-- form-codes:begin -->", "<!-- form-codes:end -->"), Codes)
}
