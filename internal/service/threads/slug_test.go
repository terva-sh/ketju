package threads

import (
	"strings"
	"testing"
)

func TestSlug(t *testing.T) {
	for _, c := range []struct{ title, want string }{
		{"Why we left Slack", "why-we-left-slack"},
		{"  Café au lait: ÆON — 2026!  ", "cafe-au-lait-aeon-2026"},
		{"Straße, Œuvre, Łódź, İstanbul", "strasse-oeuvre-lodz-istanbul"},
		{"--a--b--", "a-b"},
		{"日本語のスレッド", "thread"},
		{"", "thread"},
		{"!!!", "thread"},
		{"Ω and 日本 mixed", "and-mixed"},
		// Cut at the last word boundary inside the limit.
		{strings.Repeat("word ", 20), strings.TrimSuffix(strings.Repeat("word-", 12), "-")},
		// One word longer than the limit is cut inside it.
		{strings.Repeat("x", 70), strings.Repeat("x", MaxSlug)},
		// A boundary in the first half is not worth cutting at.
		{"ab " + strings.Repeat("y", 70), "ab-" + strings.Repeat("y", MaxSlug-3)},
	} {
		got := Slug(c.title)
		if got != c.want {
			t.Errorf("Slug(%q) = %q, want %q", c.title, got, c.want)
		}
		if len(got) > MaxSlug || strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") || strings.Contains(got, "--") {
			t.Errorf("Slug(%q) = %q breaks the shape", c.title, got)
		}
	}
}
