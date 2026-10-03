package threads

import (
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/forms"
	"github.com/terva-sh/ketju/internal/problem"
)

// TestNormalizeTagsDedupesAndSorts: a set is folded, kept once and sorted,
// counted after that, and every refusal is listed in request order with
// too_many last (docs/04 `thread_tags`, docs/07).
func TestNormalizeTagsDedupesAndSorts(t *testing.T) {
	nine := func(extra ...string) []string {
		var out []string
		for i := range domain.MaxTags + 1 {
			out = append(out, "t"+strconv.Itoa(i))
		}
		return append(out, extra...)
	}
	eightRepeated := append(nine()[:domain.MaxTags], "T0", " t7 ")

	for _, c := range []struct {
		name string
		in   []string
		want []string
		bad  []problem.FieldError
	}{
		{"none", nil, []string{}, nil},
		{"empty", []string{}, []string{}, nil},
		{"folded, once, sorted", []string{"b", "A", "a", "B"}, []string{"a", "b"}, nil},
		{"eight with repeats among ten", eightRepeated, nine()[:domain.MaxTags], nil},
		{"nine distinct", nine(), nil, []problem.FieldError{{Field: "tags", Code: forms.CodeTooMany}}},
		{"a mix", append([]string{"ok", "-bad", "x"}, "Ok", "ünï", "twenty-five-characters-xx", "fine"),
			nil, []problem.FieldError{{Field: "tags.1", Code: forms.CodeInvalidTag},
				{Field: "tags.4", Code: forms.CodeInvalidTag}, {Field: "tags.5", Code: forms.CodeTooLong}}},
		{"a mix past the count", nine("", "Nope!"), nil, []problem.FieldError{
			{Field: "tags.9", Code: forms.CodeInvalidTag}, {Field: "tags.10", Code: forms.CodeInvalidTag},
			{Field: "tags", Code: forms.CodeTooMany}}},
	} {
		got, err := NormalizeTags(c.in)
		if c.bad == nil {
			if err != nil || got == nil || !slices.Equal(got, c.want) {
				t.Errorf("%s: %q, %v; want %q", c.name, got, err, c.want)
			}
			continue
		}
		var inv *forms.Invalid
		if got != nil || !errors.As(err, &inv) || problem.FromError(err) != problem.CodeValidationFailed {
			t.Errorf("%s: %q, %v; want validation-failed", c.name, got, err)
			continue
		}
		if fe := problem.FieldErrorsOf(err); !slices.Equal(fe, c.bad) {
			t.Errorf("%s: errors %+v, want %+v", c.name, fe, c.bad)
		}
	}
}
