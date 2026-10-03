package render

import (
	"strings"
	"testing"
)

// TestQuoteInCopyIsForeign (TKT-01M29CXJZZ): in a post copied from another
// thread, an attributed quote draws as "Quote from another thread", with
// neither the quoted text nor a link to the ordinal, whatever the resolver
// says, and reports no backlink. The same source without Foreign links as
// before.
func TestQuoteInCopyIsForeign(t *testing.T) {
	src := "> [quote=#3]\n> SECRET-QUOTED-TEXT\n\nmy reply"
	posts := ResolverFunc(func(o uint32) (bool, bool) { return o == 3, false })

	r, err := RenderWith(src, Context{Posts: posts, Foreign: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"SECRET-QUOTED-TEXT", "#post-3", "Quote from #3"} {
		if strings.Contains(r.HTML, bad) {
			t.Errorf("a foreign quote kept %q:\n%s", bad, r.HTML)
		}
	}
	if !strings.Contains(r.HTML, "Quote from another thread") || !strings.Contains(r.HTML, "my reply") {
		t.Errorf("a foreign quote rendered as:\n%s", r.HTML)
	}
	if len(r.Quotes) != 0 {
		t.Errorf("a foreign quote reported backlinks %+v", r.Quotes)
	}

	r, err = RenderWith(src, Context{Posts: posts})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.HTML, "#post-3") || len(r.Quotes) != 1 {
		t.Errorf("without Foreign the quote did not link:\n%s", r.HTML)
	}
}
