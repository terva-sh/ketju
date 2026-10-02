package domain

import (
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestNormalizeTag holds NormalizeTag to docs/04 `thread_tags`: trimmed,
// ASCII folded, 1 to 24 characters of [a-z0-9]+(-[a-z0-9]+)*.
func TestNormalizeTag(t *testing.T) {
	for _, c := range []struct {
		in, want string
		err      error
	}{
		{"garden", "garden", nil},
		{"Garden", "garden", nil},
		{" long-read ", "long-read", nil},
		{"\tLONG-Read\n", "long-read", nil},
		{"a", "a", nil},
		{"2026", "2026", nil},
		{"a-b-c", "a-b-c", nil},
		{strings.Repeat("x", MaxTagLen), strings.Repeat("x", MaxTagLen), nil},
		{strings.Repeat("X", MaxTagLen), strings.Repeat("x", MaxTagLen), nil},
		{"  " + strings.Repeat("x", MaxTagLen) + "  ", strings.Repeat("x", MaxTagLen), nil},
		{strings.Repeat("x", MaxTagLen+1), "", ErrTagTooLong},
		{strings.Repeat("ä", MaxTagLen+1), "", ErrTagTooLong},
		// Under the limit in characters though over it in bytes: refused for
		// what it is, not for its length.
		{strings.Repeat("ä", 13), "", ErrInvalidTag},
		{"", "", ErrInvalidTag},
		{"   ", "", ErrInvalidTag},
		{"-a", "", ErrInvalidTag},
		{"a-", "", ErrInvalidTag},
		{"-", "", ErrInvalidTag},
		{"a--b", "", ErrInvalidTag},
		{"a_b", "", ErrInvalidTag},
		{"a b", "", ErrInvalidTag},
		{"a.b", "", ErrInvalidTag},
		{"ä", "", ErrInvalidTag},
		{"café", "", ErrInvalidTag},
		// The Kelvin sign, which strings.ToLower folds to "k".
		{"K", "", ErrInvalidTag},
		{"Kelvin", "", ErrInvalidTag},
		// The Turkish dotted capital I.
		{"İ", "", ErrInvalidTag},
		{"a\x00b", "", ErrInvalidTag},
		{"\xff", "", ErrInvalidTag},
	} {
		got, err := NormalizeTag(c.in)
		if got != c.want || !errors.Is(err, c.err) || (c.err == nil) != (err == nil) {
			t.Errorf("NormalizeTag(%q) = %q, %v; want %q, %v", c.in, got, err, c.want, c.err)
		}
	}
}

// TestNormalizeTagRefusesALongInputUnfolded: a 10 KB tag is refused for its
// length, and nothing is allocated to say so.
func TestNormalizeTagRefusesALongInputUnfolded(t *testing.T) {
	long := strings.Repeat("A", 10<<10)
	if _, err := NormalizeTag(long); !errors.Is(err, ErrTagTooLong) {
		t.Fatalf("a 10 KB tag: %v", err)
	}
	if n := testing.AllocsPerRun(10, func() { _, _ = NormalizeTag(long) }); n != 0 {
		t.Errorf("refusing a 10 KB tag allocated %v times", n)
	}
	if n := testing.AllocsPerRun(10, func() { _, _ = NormalizeTag("garden") }); n != 0 {
		t.Errorf("a lowercase tag allocated %v times", n)
	}
}

// TestReservedTagsDocumented: docs/08 "Thread tags" lists exactly
// ReservedTags between its reserved-tags markers, so the rule a member reads
// is the rule the server keeps.
func TestReservedTagsDocumented(t *testing.T) {
	b, err := os.ReadFile("../../docs/08-moderation.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	i := strings.Index(doc, "<!-- reserved-tags:begin -->")
	j := strings.Index(doc, "<!-- reserved-tags:end -->")
	if i < 0 || j < i {
		t.Fatal("docs/08 has no reserved-tags markers")
	}
	var listed []string
	for _, m := range regexp.MustCompile("(?m)^\\s*- `([^`]*)`\\s*$").FindAllStringSubmatch(doc[i:j], -1) {
		listed = append(listed, m[1])
	}
	want := slices.Sorted(slices.Values(ReservedTags))
	slices.Sort(listed)
	if !slices.Equal(listed, want) {
		t.Errorf("docs/08 lists the reserved tags %q, domain.ReservedTags is %q", listed, want)
	}
	for _, r := range ReservedTags {
		if n, err := NormalizeTag(r); err != nil || n != r {
			t.Errorf("the reserved tag %q is not a tag as stored: %q, %v", r, n, err)
		}
		if !IsReservedTag(r) {
			t.Errorf("IsReservedTag(%q) is false", r)
		}
	}
	if IsReservedTag("agents") || IsReservedTag("Agent") {
		t.Error("IsReservedTag matches something other than a listed tag as stored")
	}
}
