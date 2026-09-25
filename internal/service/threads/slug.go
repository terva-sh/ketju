package threads

import (
	"strings"
)

// MaxSlug is the longest slug Slug returns, in bytes, which are ASCII.
const MaxSlug = 60

// fold spells a Latin letter with a diacritic, or a ligature, in ASCII.
// It covers Latin-1 and Latin Extended-A, which is where the titles of a
// Latin-script forum spend nearly all of their non-ASCII letters. A letter
// outside it separates words like punctuation does: a slug is a readable
// hint in a URL whose thread id is what resolves it, so a title in another
// script getting the "thread" slug costs nothing but looks.
var fold = map[rune]string{}

func init() {
	for _, g := range []struct{ to, from string }{
		{"a", "àáâãäåāăą"}, {"c", "çćĉċč"}, {"d", "ďđð"}, {"e", "èéêëēĕėęě"},
		{"g", "ĝğġģ"}, {"h", "ĥħ"}, {"i", "ìíîïĩīĭįı"}, {"j", "ĵ"}, {"k", "ķĸ"},
		{"l", "ĺļľŀł"}, {"n", "ñńņňŉŋ"}, {"o", "òóôõöøōŏő"}, {"r", "ŕŗř"},
		{"s", "śŝşšſ"}, {"t", "ţťŧ"}, {"u", "ùúûüũūŭůűų"}, {"w", "ŵ"},
		{"y", "ýÿŷ"}, {"z", "źżž"}, {"ae", "æ"}, {"oe", "œ"}, {"ss", "ß"},
		{"th", "þ"}, {"ij", "ĳ"},
	} {
		for _, r := range g.from {
			fold[r] = g.to
		}
	}
}

// Slug is a thread's URL slug: its title folded to lower-case ASCII letters
// and digits, with every run of anything else one '-', no '-' at either end,
// at most MaxSlug bytes, cut at a word boundary when one falls in the second
// half of that. A title with nothing to keep is "thread" (settled in
// grooming for TKT-01M29CA9B9: an ASCII fold of at most 60 characters).
func Slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		var s string
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			s = string(r)
		default:
			s = fold[r]
		}
		if s == "" {
			dash = b.Len() > 0
			continue
		}
		if dash {
			b.WriteByte('-')
			dash = false
		}
		b.WriteString(s)
	}
	out := b.String()
	if len(out) > MaxSlug {
		out = out[:MaxSlug]
		if i := strings.LastIndexByte(out, '-'); i >= MaxSlug/2 {
			out = out[:i]
		}
		out = strings.TrimRight(out, "-")
	}
	if out == "" {
		return "thread"
	}
	return out
}
