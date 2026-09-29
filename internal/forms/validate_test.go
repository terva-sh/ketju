package forms

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
)

// one is a schema of a single field, parsed, so a case tests the field and
// nothing else.
func one(t *testing.T, field string) Schema {
	t.Helper()
	s, err := Parse([]byte(`{"title":"T","fields":[` + field + `]}`))
	if err != nil {
		t.Fatalf("the fixture schema does not parse: %v", err)
	}
	return s
}

// codes is the field errors of err, as "field code" strings.
func codes(t *testing.T, err error) []string {
	t.Helper()
	var inv *Invalid
	if !errors.As(err, &inv) {
		t.Fatalf("want a *Invalid, got %v", err)
	}
	out := make([]string, 0, len(inv.Fields))
	for _, f := range inv.Fields {
		out = append(out, f.Field+" "+f.Code)
	}
	return out
}

// TestEveryTypeValidatesFromTheTable is AC1: each of the six types, its
// accepted values with what is stored, and its refused values with the exact
// code, all through Validate and so through the one table.
func TestEveryTypeValidatesFromTheTable(t *testing.T) {
	cases := []struct {
		name  string
		field string
		value string // JSON; "" is no answer at all
		want  any    // the stored value, when accepted
		code  string // the refusal's code, when refused
	}{
		{"text", `{"key":"a","type":"text","label":"L"}`, `"  hello  "`, "hello", ""},
		{"text empty", `{"key":"a","type":"text","label":"L"}`, `""`, "", ""},
		{"text absent", `{"key":"a","type":"text","label":"L"}`, ``, "", ""},
		{"text null", `{"key":"a","type":"text","label":"L"}`, `null`, "", ""},
		{"text at its max", `{"key":"a","type":"text","label":"L","max":3}`, `"äöü"`, "äöü", ""},
		{"text over its max", `{"key":"a","type":"text","label":"L","max":3}`, `"abcd"`, nil, CodeTooLong},
		{"text over the cap", `{"key":"a","type":"text","label":"L"}`, `"` + strings.Repeat("x", 201) + `"`, nil, CodeTooLong},
		{"text with a newline", `{"key":"a","type":"text","label":"L"}`, `"a\nb"`, nil, CodeInvalidText},
		{"text with a control character", `{"key":"a","type":"text","label":"L"}`, `"a\u0007b"`, nil, CodeInvalidText},
		{"text as a number", `{"key":"a","type":"text","label":"L"}`, `5`, nil, CodeWrongType},
		{"text required and blank", `{"key":"a","type":"text","label":"L","required":true}`, `"   "`, nil, CodeRequired},

		{"email", `{"key":"a","type":"email","label":"L"}`, `" ada@example.org "`, "ada@example.org", ""},
		{"email with a name", `{"key":"a","type":"email","label":"L"}`, `"Ada <ada@example.org>"`, nil, CodeInvalidEmail},
		{"email with no domain", `{"key":"a","type":"email","label":"L"}`, `"ada"`, nil, CodeInvalidEmail},
		{"email over its max", `{"key":"a","type":"email","label":"L","max":5}`, `"ada@example.org"`, nil, CodeTooLong},
		{"email as a bool", `{"key":"a","type":"email","label":"L"}`, `true`, nil, CodeWrongType},
		{"email required and absent", `{"key":"a","type":"email","label":"L","required":true}`, ``, nil, CodeRequired},

		{"textarea keeps its lines", `{"key":"a","type":"textarea","label":"L"}`, `"one\r\ntwo\n\tthree\n"`, "one\ntwo\n\tthree", ""},
		{"textarea over its max", `{"key":"a","type":"textarea","label":"L","max":4}`, `"abcde"`, nil, CodeTooLong},
		{"textarea over the cap", `{"key":"a","type":"textarea","label":"L"}`, `"` + strings.Repeat("x", 5001) + `"`, nil, CodeTooLong},
		{"textarea with a control character", `{"key":"a","type":"textarea","label":"L"}`, `"a\u001bb"`, nil, CodeInvalidText},
		{"textarea as an array", `{"key":"a","type":"textarea","label":"L"}`, `["a"]`, nil, CodeWrongType},

		{"checkbox ticked", `{"key":"a","type":"checkbox","label":"L","required":true}`, `true`, true, ""},
		{"checkbox unticked", `{"key":"a","type":"checkbox","label":"L"}`, `false`, false, ""},
		{"checkbox absent", `{"key":"a","type":"checkbox","label":"L"}`, ``, false, ""},
		{"checkbox required and unticked", `{"key":"a","type":"checkbox","label":"L","required":true}`, `false`, nil, CodeRequired},
		{"checkbox as a string", `{"key":"a","type":"checkbox","label":"L"}`, `"true"`, nil, CodeWrongType},

		{"select", `{"key":"a","type":"select","label":"L","options":["x","y"]}`, `"y"`, "y", ""},
		{"select nothing chosen", `{"key":"a","type":"select","label":"L","options":["x","y"]}`, `""`, "", ""},
		{"select not an option", `{"key":"a","type":"select","label":"L","options":["x","y"]}`, `"z"`, nil, CodeNotAnOption},
		{"select as an array", `{"key":"a","type":"select","label":"L","options":["x","y"]}`, `["x"]`, nil, CodeWrongType},
		{"select required and absent", `{"key":"a","type":"select","label":"L","options":["x"],"required":true}`, ``, nil, CodeRequired},
		{"multiple select", `{"key":"a","type":"select","label":"L","options":["x","y","z"],"multiple":true}`, `["z","x"]`, []string{"z", "x"}, ""},
		{"multiple select none", `{"key":"a","type":"select","label":"L","options":["x"],"multiple":true}`, ``, []string{}, ""},
		{"multiple select duplicate", `{"key":"a","type":"select","label":"L","options":["x","y"],"multiple":true}`, `["x","x"]`, nil, CodeDuplicateOption},
		{"multiple select not an option", `{"key":"a","type":"select","label":"L","options":["x"],"multiple":true}`, `["x","q"]`, nil, CodeNotAnOption},
		{"multiple select as a string", `{"key":"a","type":"select","label":"L","options":["x"],"multiple":true}`, `"x"`, nil, CodeWrongType},
		{"multiple select required and empty", `{"key":"a","type":"select","label":"L","options":["x"],"multiple":true,"required":true}`, `[]`, nil, CodeRequired},

		{"notice takes no answer", `{"key":"a","type":"markdown_notice","markdown":"**hi**"}`, ``, nil, ""},
		{"notice refuses a value", `{"key":"a","type":"markdown_notice","markdown":"**hi**"}`, `"x"`, nil, CodeNotAllowed},
	}
	seen := map[string]bool{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := one(t, c.field)
			seen[s.Fields[0].Type] = true
			answers := map[string]json.RawMessage{}
			if c.value != "" {
				answers["a"] = json.RawMessage(c.value)
			}
			got, err := Validate(s, answers, nil)
			if c.code != "" {
				if want := []string{"a " + c.code}; !reflect.DeepEqual(codes(t, err), want) {
					t.Errorf("errors %v, want %v", codes(t, err), want)
				}
				if !errors.Is(err, ErrInvalidAnswers) {
					t.Errorf("%v is not ErrInvalidAnswers", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			v, ok := got["a"]
			if s.Fields[0].Type == "markdown_notice" {
				if ok {
					t.Errorf("a notice stored %v", v)
				}
				return
			}
			if !reflect.DeepEqual(v, c.want) {
				t.Errorf("stored %#v, want %#v", v, c.want)
			}
		})
	}
	for _, typ := range Types() {
		if !seen[typ] {
			t.Errorf("no case exercises the type %s", typ)
		}
	}
}

// TestANewSchemaNeedsNoCode is AC1's other half: schemas with keys nothing
// in this package has seen, over all six types, validate through Validate
// unchanged. A validator that named a field key could not pass this.
func TestANewSchemaNeedsNoCode(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	letters := "abcdefghijklmnopqrstuvwxyz0123456789_"
	for n := range 200 {
		var fields []string
		answers := map[string]json.RawMessage{}
		want := Answers{}
		used := map[string]bool{}
		for i := range 1 + rng.IntN(8) {
			key := string(rune('a' + rng.IntN(26)))
			for range rng.IntN(12) {
				key += string(letters[rng.IntN(len(letters))])
			}
			if used[key] {
				continue
			}
			used[key] = true
			typ := Types()[rng.IntN(len(types))]
			value := fmt.Sprintf("v%d", i)
			switch typ {
			case "text", "textarea":
				fields = append(fields, fmt.Sprintf(`{"key":%q,"type":%q,"label":"L","required":true}`, key, typ))
				answers[key], want[key] = json.RawMessage(`"`+value+`"`), value
			case "email":
				fields = append(fields, fmt.Sprintf(`{"key":%q,"type":"email","label":"L","required":true}`, key))
				answers[key], want[key] = json.RawMessage(`"`+value+`@example.org"`), value+"@example.org"
			case "checkbox":
				fields = append(fields, fmt.Sprintf(`{"key":%q,"type":"checkbox","label":"L","required":true}`, key))
				answers[key], want[key] = json.RawMessage(`true`), true
			case "select":
				fields = append(fields, fmt.Sprintf(`{"key":%q,"type":"select","label":"L","options":["%s","o"],"required":true}`, key, value))
				answers[key], want[key] = json.RawMessage(`"`+value+`"`), value
			case "markdown_notice":
				fields = append(fields, fmt.Sprintf(`{"key":%q,"type":"markdown_notice","markdown":"note"}`, key))
			}
		}
		s, err := Parse([]byte(`{"title":"T","fields":[` + strings.Join(fields, ",") + `]}`))
		if err != nil {
			t.Fatalf("schema %d does not parse: %v", n, err)
		}
		got, err := Validate(s, answers, nil)
		if err != nil {
			t.Fatalf("schema %d: %v", n, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("schema %d stored %v, want %v", n, got, want)
		}
		// And every required field left out is named by its own key.
		if _, err := Validate(s, nil, nil); err != nil {
			for _, fe := range err.(*Invalid).Fields {
				if !used[fe.Field] || fe.Code != CodeRequired {
					t.Fatalf("schema %d: an empty submission reported %v", n, fe)
				}
			}
		}
	}
}

// TestLockedFieldTakesTheClaim is AC2: a locked field ignores what was
// submitted and stores the claim; with no claim it stores "" and is not
// required-checked.
func TestLockedFieldTakesTheClaim(t *testing.T) {
	s := one(t, `{"key":"mail","type":"email","label":"Email","from_claim":"email","locked":true,"required":true}`)
	claims := map[string]string{"email": "ada@example.org"}
	for _, c := range []struct {
		name    string
		answers map[string]json.RawMessage
		claims  map[string]string
		want    string
	}{
		{"a differing value", map[string]json.RawMessage{"mail": json.RawMessage(`"mallory@example.org"`)}, claims, "ada@example.org"},
		{"a value of the wrong type", map[string]json.RawMessage{"mail": json.RawMessage(`42`)}, claims, "ada@example.org"},
		{"no value", nil, claims, "ada@example.org"},
		{"no claim", map[string]json.RawMessage{"mail": json.RawMessage(`"mallory@example.org"`)}, nil, ""},
		{"no claim and no value", nil, map[string]string{"name": "Ada"}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := Validate(s, c.answers, c.claims)
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			if got["mail"] != c.want {
				t.Errorf("stored %q, want %q", got["mail"], c.want)
			}
		})
	}
}

// TestAnUnlockedClaimFieldTakesTheAnswer: from_claim without locked is a
// prefill, and what the applicant submits is what is stored.
func TestAnUnlockedClaimFieldTakesTheAnswer(t *testing.T) {
	s := one(t, `{"key":"name","type":"text","label":"Name","from_claim":"name","required":true}`)
	got, err := Validate(s, map[string]json.RawMessage{"name": json.RawMessage(`"Ada L"`)}, map[string]string{"name": "Ada"})
	if err != nil || got["name"] != "Ada L" {
		t.Fatalf("stored %v (%v), want the answer", got, err)
	}
	if _, err := Validate(s, nil, map[string]string{"name": "Ada"}); !errors.Is(err, ErrInvalidAnswers) {
		t.Errorf("an unlocked required field took the claim when left out: %v", err)
	}
}

// TestRequiredMissingIsFieldScoped: the error names the field and the code,
// and every failing field is listed, in schema order.
func TestRequiredMissingIsFieldScoped(t *testing.T) {
	s, err := Parse([]byte(`{"title":"T","fields":[
		{"key":"who","type":"textarea","label":"Who?","required":true},
		{"key":"ok","type":"text","label":"Fine"},
		{"key":"rules","type":"checkbox","label":"Rules","required":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Validate(s, map[string]json.RawMessage{"rules": json.RawMessage(`false`)}, nil)
	if want := []string{"who required", "rules required"}; !reflect.DeepEqual(codes(t, err), want) {
		t.Errorf("errors %v, want %v", codes(t, err), want)
	}
}

// TestUnknownAnswerIsABadRequest: an answer the schema has no field for is
// a client bug, not a field error, and is refused before anything else.
func TestUnknownAnswerIsABadRequest(t *testing.T) {
	s := one(t, `{"key":"a","type":"text","label":"L","required":true}`)
	_, err := Validate(s, map[string]json.RawMessage{"zzz": json.RawMessage(`"x"`)}, nil)
	if !errors.Is(err, ErrUnknownAnswer) {
		t.Fatalf("got %v, want ErrUnknownAnswer", err)
	}
	if errors.Is(err, ErrInvalidAnswers) {
		t.Errorf("an unknown answer is also ErrInvalidAnswers, which maps first")
	}
	if strings.Contains(err.Error(), "zzz") {
		t.Errorf("the error names the submitted key: %v", err)
	}
}
