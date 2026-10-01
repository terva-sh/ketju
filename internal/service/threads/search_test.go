package threads

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/store/db/views"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestParseTerms is q's rules (docs/07 "Search"): trimmed, 1 to 200
// characters, at most 16 words, words and quoted phrases, and nothing else
// is syntax. The API tests run the same through FTS5.
func TestParseTerms(t *testing.T) {
	for _, c := range []struct {
		q    string
		want []string
	}{
		{"bill", []string{"bill"}},
		{"  the  bill\t", []string{"the", "bill"}},
		{`"smaller half" bill`, []string{"smaller half", "bill"}},
		{`bill "smaller   half"`, []string{"bill", "smaller half"}},
		{`a "b c`, []string{"a", "b c"}}, // an open quote ends at the end
		{`"" bill`, []string{"bill"}},
		{`OR NOT NEAR(a b) * ^x title:x`, []string{"OR", "NOT", "NEAR(a", "b)", "*", "^x", "title:x"}},
		{strings.Repeat("w ", 16), strings.Fields(strings.Repeat("w ", 16))},
		{strings.Repeat("é", 200), []string{strings.Repeat("é", 200)}}, // characters, not bytes
	} {
		got, err := parseTerms(c.q)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseTerms(%q) = %q, %v; want %q", c.q, got, err, c.want)
		}
	}
	for _, c := range []struct{ q, code string }{
		{"", "required"},
		{" \t\n", "required"},
		{`""`, "required"},
		{strings.Repeat("a", 201), "too_long"},
		{strings.Repeat("w ", 17), "too_many"},
		{`"` + strings.Repeat("w ", 17) + `"`, "too_many"}, // a phrase's words count
	} {
		_, err := parseTerms(c.q)
		fe := problem.FieldErrorsOf(err)
		if problem.FromError(err) != problem.CodeValidationFailed || len(fe) != 1 ||
			fe[0] != (problem.FieldError{Field: "q", Code: c.code}) {
			t.Errorf("parseTerms(%.20q): %v %+v, want validation-failed on q, %s", c.q, err, fe, c.code)
		}
	}
}

// TestSplitSnippet: the sentinels become segments, neighbours alike merge,
// empty runs vanish, and a sentinel out of place is dropped, never shown.
func TestSplitSnippet(t *testing.T) {
	o, c := string(render.SnippetOpen), string(render.SnippetClose)
	part := func(text string, match bool) dto.SnippetPart { return dto.SnippetPart{Text: text, Match: match} }
	for _, tc := range []struct {
		in   string
		want []dto.SnippetPart
	}{
		{"", []dto.SnippetPart{}},
		{"plain", []dto.SnippetPart{part("plain", false)}},
		{"the " + o + "bill" + c + " was", []dto.SnippetPart{part("the ", false), part("bill", true), part(" was", false)}},
		{o + "a" + c + o + "b" + c, []dto.SnippetPart{part("ab", true)}},
		{o + c + "x", []dto.SnippetPart{part("x", false)}},
		{"a" + c + "b", []dto.SnippetPart{part("ab", false)}},                                 // a close outside a match
		{o + "a" + o + "b" + c, []dto.SnippetPart{part("ab", true)}},                          // an open inside one
		{"x " + o + "<b>&amp;", []dto.SnippetPart{part("x ", false), part("<b>&amp;", true)}}, // text stays text
	} {
		if got := splitSnippet(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitSnippet(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

// TestSearchCursor: what the server writes, it reads back, and nothing
// else: not offset 0, which is the first page's empty cursor, not the
// reach or past it, and no other spelling of an offset.
func TestSearchCursor(t *testing.T) {
	for _, n := range []int{1, 25, 999} {
		if got, err := decodeSearchCursor(encodeSearchCursor(n)); err != nil || got != n {
			t.Errorf("%d came back %d, %v", n, got, err)
		}
	}
	if got, err := decodeSearchCursor(""); got != 0 || err != nil {
		t.Errorf("empty: %d, %v", got, err)
	}
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for _, bad := range []string{"!", "MQ==", enc("0"), enc("05"), enc("+5"), enc("-1"), enc("1000"),
		enc("1001"), enc("abc"), enc("1|2|3")} {
		if _, err := decodeSearchCursor(bad); problem.FromError(err) != problem.CodeBadRequest {
			t.Errorf("cursor %q: %v, want bad-request", bad, err)
		}
	}
}

// TestServesPost is the per-post half of the freshness check for every
// entry state. A moved post is here because nothing in this build can move
// one (split and merge records are reserved), so the API tests cannot reach
// it; an edit flag alone is still the post.
func TestServesPost(t *testing.T) {
	row := views.SearchRow{ThreadID: 1, Ordinal: 2, Revision: 3, AuthorID: 7, CreatedAt: 1000}
	entry := threadlog.IndexEntry{Revision: 3, Author: 7, CreatedMS: 1000}
	if !servesPost(entry, row) {
		t.Fatal("a visible post at the indexed revision is not served")
	}
	for _, c := range []struct {
		name  string
		flags threadlog.EntryFlags
		want  bool
	}{
		{"edited", threadlog.EntryEdited, true},
		{"deleted", threadlog.EntryDeleted, false},
		{"hidden by a moderator", threadlog.EntryHiddenByMod, false},
		{"hidden pending review", threadlog.EntryHiddenByMod | threadlog.EntryAutoHidden, false},
		{"moved", threadlog.EntryMoved, false},
		{"redacted", threadlog.EntryRedacted, false},
	} {
		e := entry
		e.Flags = c.flags
		if got := servesPost(e, row); got != c.want {
			t.Errorf("%s: %t, want %t", c.name, got, c.want)
		}
	}
	for name, e := range map[string]threadlog.IndexEntry{
		"a later revision": {Revision: 4, Author: 7, CreatedMS: 1000},
		"another author":   {Revision: 3, Author: 8, CreatedMS: 1000},
		"another time":     {Revision: 3, Author: 7, CreatedMS: 1001},
	} {
		if servesPost(e, row) {
			t.Errorf("%s is served", name)
		}
	}
}

// TestSearchMillis: absent, or a non-negative integer of Unix ms.
func TestSearchMillis(t *testing.T) {
	if got, err := searchMillis("after", ""); got != nil || err != nil {
		t.Errorf("empty: %v, %v", got, err)
	}
	for _, ok := range []string{"0", "1757500000000"} {
		if got, err := searchMillis("after", ok); err != nil || got == nil {
			t.Errorf("%q: %v, %v", ok, got, err)
		}
	}
	for _, bad := range []string{"-1", "1.5", "x", " 1", "1e3", "9223372036854775808"} {
		if _, err := searchMillis("before", bad); problem.FromError(err) != problem.CodeBadRequest {
			t.Errorf("%q: %v, want bad-request", bad, err)
		}
	}
}
