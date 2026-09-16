package render

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// resolver builds a PostResolver from a set of existing and deleted ordinals.
func resolver(exists []uint32, deleted ...uint32) PostResolver {
	e := map[uint32]bool{}
	for _, o := range exists {
		e[o] = true
	}
	d := map[uint32]bool{}
	for _, o := range deleted {
		e[o] = true
		d[o] = true
	}
	return ResolverFunc(func(ordinal uint32) (bool, bool) {
		return e[ordinal], d[ordinal]
	})
}

// --- Acceptance criterion 1: a linked, collapsible quote ---------------------

func TestAQuoteLinksToTheOrdinalAndCollapses(t *testing.T) {
	src := "> [quote=#37]\n> the *quoted* text\n"
	got, err := RenderWith(src, Context{Posts: resolver([]uint32{37})})
	if err != nil {
		t.Fatalf("RenderWith: %v", err)
	}

	for _, want := range []string{
		`<details class="quote"`,
		`open`,
		`<summary>`,
		`href="#post-37"`,
		`<blockquote>`,
		`<em>quoted</em>`,
	} {
		if !strings.Contains(got.HTML, want) {
			t.Errorf("output is missing %q:\n%s", want, got.HTML)
		}
	}
	// <details> collapses with no script, which is what lets the non-JS client
	// collapse a quote too. A div with a class would need JavaScript, and the
	// second client would silently lose the behaviour.
	if !strings.Contains(got.HTML, "<details") {
		t.Error("the quote is not collapsible without script")
	}
	assertSafe(t, src, got.HTML)
}

func TestTheAttributionLineDoesNotSurviveIntoTheText(t *testing.T) {
	got, err := RenderWith("> [quote=#37]\n> body\n", Context{Posts: resolver([]uint32{37})})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.HTML, "[quote=") {
		t.Errorf("the attribution line was rendered as text:\n%s", got.HTML)
	}
}

func TestAPlainBlockquoteIsUntouched(t *testing.T) {
	// The extension replaces goldmark's blockquote handling for attributed
	// quotes only. Every ordinary blockquote in every existing post has to keep
	// rendering exactly as it did, or the version bump is hiding a change
	// nobody asked for.
	got, err := Render("> just a quotation\n>\n> with two paragraphs\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.HTML, "details") {
		t.Errorf("a plain blockquote became a quote block:\n%s", got.HTML)
	}
	if !strings.HasPrefix(got.HTML, "<blockquote>") {
		t.Errorf("a plain blockquote no longer renders as one:\n%s", got.HTML)
	}
	if len(got.Quotes) != 0 {
		t.Errorf("a plain blockquote reported %d quotes", len(got.Quotes))
	}
}

// --- Acceptance criterion 2: a deleted post shows a tombstone ----------------

// TestQuotingADeletedPostShowsTheTombstone is the criterion that matters most,
// and the reason it matters is not obvious from the wording.
//
// The quoted text is not fetched from the quoted post at render time. The
// composer copied it into the quoting post's own source when somebody clicked
// "quote", so deleting post 37 does nothing whatsoever to the copy sitting in
// post 50. Dropping the children here is the only thing that makes a delete
// take effect everywhere, and without it a moderator removing a post would
// leave the text visible in every post that had quoted it.
func TestQuotingADeletedPostShowsTheTombstone(t *testing.T) {
	const secret = "the-text-that-was-removed"
	src := "> [quote=#37]\n> " + secret + "\n>\n> and a second paragraph of it\n\nmy reply\n"

	got, err := RenderWith(src, Context{Posts: resolver(nil, 37)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.HTML, secret) {
		t.Fatalf("a deleted post's text is still in the output:\n%s", got.HTML)
	}
	if strings.Contains(got.HTML, "second paragraph") {
		t.Fatalf("only the first paragraph of the quote was dropped:\n%s", got.HTML)
	}
	if !strings.Contains(got.HTML, "quote-deleted") {
		t.Errorf("no tombstone was rendered:\n%s", got.HTML)
	}
	// The quote is still a quote: the reader should see that something was
	// quoted and be able to follow the link, not see the quotation vanish.
	if !strings.Contains(got.HTML, `href="#post-37"`) {
		t.Errorf("the tombstone lost the link to the quoted post:\n%s", got.HTML)
	}
	if !strings.Contains(got.HTML, "my reply") {
		t.Errorf("the rest of the post was dropped along with the quote:\n%s", got.HTML)
	}
}

func TestADeletedQuoteIsStillABacklink(t *testing.T) {
	got, err := RenderWith("> [quote=#37]\n> gone\n", Context{Posts: resolver(nil, 37)})
	if err != nil {
		t.Fatal(err)
	}
	// The deleted post still needs to know who quoted it: a tombstone that can
	// show "quoted by 3 posts" is how a moderator sees the reach of what they
	// removed.
	if len(got.Quotes) != 1 {
		t.Fatalf("Quotes = %+v, want one", got.Quotes)
	}
	if !got.Quotes[0].Deleted {
		t.Error("the backlink does not record that the quoted post was deleted")
	}
}

// --- Acceptance criterion 3: backlinks come out of the render call -----------

func TestBacklinksAreEmittedInDocumentOrder(t *testing.T) {
	src := strings.Join([]string{
		"> [quote=#3]", "> first", "", "some text", "",
		"> [quote=#11]", "> second", "", "> [quote=#7]", "> third", "",
	}, "\n")

	got, err := RenderWith(src, Context{Posts: resolver([]uint32{3, 7, 11})})
	if err != nil {
		t.Fatal(err)
	}
	want := []uint32{3, 11, 7}
	if len(got.Quotes) != len(want) {
		t.Fatalf("Quotes = %+v, want %v", got.Quotes, want)
	}
	for i, w := range want {
		if got.Quotes[i].Ordinal != w {
			// Order is the document's, not a map's. The projection writer
			// records these as rows and a stable order makes the rows stable.
			t.Errorf("Quotes[%d].Ordinal = %d, want %d", i, got.Quotes[i].Ordinal, w)
		}
	}
}

func TestTheSameOrdinalQuotedTwiceIsReportedTwice(t *testing.T) {
	src := "> [quote=#5]\n> once\n\n> [quote=#5]\n> again\n"
	got, err := RenderWith(src, Context{Posts: resolver([]uint32{5})})
	if err != nil {
		t.Fatal(err)
	}
	// Deduplicating here would be a decision about what a backlink means, and
	// it belongs to whoever writes the rows, not to the renderer.
	if len(got.Quotes) != 2 {
		t.Errorf("Quotes = %+v, want the same ordinal reported twice", got.Quotes)
	}
}

// TestWithoutAResolverQuotesAreOptimistic states the preview contract.
//
// A composer preview is rendered before the post exists and often without the
// thread in hand. Declining to link would make the preview disagree with the
// posted result, and that agreement is the whole reason this package is the
// only renderer in the system.
func TestWithoutAResolverQuotesAreOptimistic(t *testing.T) {
	got, err := Render("> [quote=#37]\n> text\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.HTML, `href="#post-37"`) {
		t.Errorf("a quote rendered without a resolver was not linked:\n%s", got.HTML)
	}
	if len(got.Quotes) != 1 || got.Quotes[0].Ordinal != 37 {
		t.Errorf("Quotes = %+v, want one for 37", got.Quotes)
	}
}

// --- Acceptance criterion 4: an unresolvable ordinal degrades ----------------

// TestQuotingAPostThatDoesNotExistIsAPlainBlockquote is the fourth criterion.
//
// Someone hand-types an ordinal, or quotes a post that was later hard-deleted,
// or pastes a quote block from another thread. None of those should cost them
// their post, and none should produce a link to an anchor that is not on the
// page.
func TestQuotingAPostThatDoesNotExistIsAPlainBlockquote(t *testing.T) {
	src := "> [quote=#999]\n> text that was quoted\n"
	got, err := RenderWith(src, Context{Posts: resolver([]uint32{1, 2, 3})})
	if err != nil {
		t.Fatalf("an unresolvable ordinal failed the post: %v", err)
	}
	if strings.Contains(got.HTML, "details") || strings.Contains(got.HTML, "#post-999") {
		t.Errorf("a quote of a post that does not exist was linked:\n%s", got.HTML)
	}
	if !strings.Contains(got.HTML, "<blockquote>") {
		t.Errorf("what remained is not a blockquote:\n%s", got.HTML)
	}
	if !strings.Contains(got.HTML, "text that was quoted") {
		t.Errorf("the quoted text was dropped along with the attribution:\n%s", got.HTML)
	}
	if len(got.Quotes) != 0 {
		t.Errorf("Quotes = %+v; there is nothing to link back to", got.Quotes)
	}
}

// --- The syntax boundary -----------------------------------------------------

func TestWhatIsNotAnAttribution(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"ordinal zero", "> [quote=#0]\n> x\n"},
		{"not a number", "> [quote=#abc]\n> x\n"},
		{"negative", "> [quote=#-1]\n> x\n"},
		{"no hash", "> [quote=37]\n> x\n"},
		{"trailing text", "> [quote=#37] said this\n> x\n"},
		{"leading text", "said [quote=#37]\n> x\n"},
		{"not first line", "> first\n> [quote=#37]\n> x\n"},
		{"outside a blockquote", "[quote=#37]\n\ntext\n"},
		{"past the ceiling", "> [quote=#1000000000]\n> x\n"},
		{"empty ordinal", "> [quote=#]\n> x\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := RenderWith(c.src, Context{Posts: resolver([]uint32{37})})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(got.HTML, "quote-source") {
				t.Errorf("%s was treated as an attribution:\n%s", c.name, got.HTML)
			}
			if len(got.Quotes) != 0 {
				t.Errorf("%s produced backlinks %+v", c.name, got.Quotes)
			}
		})
	}
}

// TestAnAttributionInsideCodeIsText is the one that would be a real bug.
//
// A post explaining the quote syntax has to be able to show it. If a fenced
// block were transformed, documenting the feature would be impossible and the
// code sample would turn into a live quote of whatever ordinal it named.
func TestAnAttributionInsideCodeIsText(t *testing.T) {
	cases := []string{
		"```\n> [quote=#37]\n> x\n```\n",
		"    > [quote=#37]\n    > x\n",
		"`[quote=#37]`\n",
		"> ```\n> [quote=#37]\n> ```\n",
	}
	for _, src := range cases {
		got, err := RenderWith(src, Context{Posts: resolver([]uint32{37})})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got.HTML, "quote-source") {
			t.Errorf("an attribution inside code became a quote:\n%s\n%s", src, got.HTML)
		}
		if !strings.Contains(got.HTML, "quote=#37") {
			t.Errorf("the code sample lost its text:\n%s\n%s", src, got.HTML)
		}
	}
}

func TestANestedQuoteIsAQuote(t *testing.T) {
	// Quoting a post that itself quoted something is ordinary on a forum.
	src := "> [quote=#10]\n> outer\n>\n> > [quote=#5]\n> > inner\n"
	got, err := RenderWith(src, Context{Posts: resolver([]uint32{5, 10})})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(got.HTML, "quote-source"); n != 2 {
		t.Errorf("%d attributions rendered, want 2:\n%s", n, got.HTML)
	}
	if len(got.Quotes) != 2 {
		t.Errorf("Quotes = %+v, want two", got.Quotes)
	}
	assertSafe(t, src, got.HTML)
}

func TestDeletingTheOuterQuoteTakesTheNestedOneWithIt(t *testing.T) {
	src := "> [quote=#10]\n> outer\n>\n> > [quote=#5]\n> > inner text\n"
	got, err := RenderWith(src, Context{Posts: resolver([]uint32{5}, 10)})
	if err != nil {
		t.Fatal(err)
	}
	// The inner quotation is part of the deleted post's content, so it goes too.
	if strings.Contains(got.HTML, "inner text") {
		t.Errorf("a nested quote survived the deletion of its container:\n%s", got.HTML)
	}
	if len(got.Quotes) != 1 || got.Quotes[0].Ordinal != 10 {
		t.Errorf("Quotes = %+v, want only the outer quote", got.Quotes)
	}
}

// --- Safety -------------------------------------------------------------------

// TestTheOrdinalCannotEscapeTheHref. The ordinal is the only user-controlled
// value that reaches an attribute, so this is the whole injection surface.
func TestTheOrdinalCannotEscapeTheHref(t *testing.T) {
	attempts := []string{
		`> [quote=#37" onmouseover="alert(1)]` + "\n> x\n",
		"> [quote=#37\"><script>alert(1)</script>]\n> x\n",
		"> [quote=#javascript:alert(1)]\n> x\n",
		"> [quote=#37 37]\n> x\n",
		"> [quote=#3\u00377]\n> x\n",
	}
	for _, src := range attempts {
		got, err := RenderWith(src, Context{Posts: resolver([]uint32{37})})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got.HTML, "quote-source") {
			t.Errorf("a malformed ordinal produced an attribution link:\n%s\n%s", src, got.HTML)
		}
		assertSafe(t, src, got.HTML)
	}
}

func TestQuotedContentIsStillSanitized(t *testing.T) {
	src := "> [quote=#37]\n> <script>alert(1)</script>\n> [x](javascript:alert(1))\n"
	got, err := RenderWith(src, Context{Posts: resolver([]uint32{37})})
	if err != nil {
		t.Fatal(err)
	}
	assertSafe(t, src, got.HTML)
	if strings.Contains(got.HTML, "<script") {
		t.Errorf("a script survived inside a quote:\n%s", got.HTML)
	}
}

func TestQuoteRenderingIsSafeForConcurrentUse(t *testing.T) {
	// The paragraph transformer stores what it found in the parse context
	// rather than on itself, because the parser is a package-level singleton
	// shared by every render in the process. A field on the transformer would
	// be one parse seeing another's blockquotes.
	src := "> [quote=#37]\n> text\n\n> [quote=#38]\n> more\n"
	posts := resolver([]uint32{37, 38})

	want, err := RenderWith(src, Context{Posts: posts})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				got, err := RenderWith(src, Context{Posts: posts})
				if err != nil {
					t.Errorf("RenderWith: %v", err)
					return
				}
				if got.HTML != want.HTML {
					t.Errorf("concurrent renders disagree:\n%s\n---\n%s", want.HTML, got.HTML)
					return
				}
				if len(got.Quotes) != 2 {
					t.Errorf("Quotes = %+v", got.Quotes)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestManyQuotesInOnePost(t *testing.T) {
	var b strings.Builder
	const n = 200
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "> [quote=#%d]\n> body %d\n\n", i, i)
	}
	exists := make([]uint32, n)
	for i := range exists {
		exists[i] = uint32(i + 1)
	}

	got, err := RenderWith(b.String(), Context{Posts: resolver(exists)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Quotes) != n {
		t.Fatalf("Quotes = %d, want %d", len(got.Quotes), n)
	}
	for i, q := range got.Quotes {
		if q.Ordinal != uint32(i+1) {
			t.Fatalf("Quotes[%d] = %d, want %d", i, q.Ordinal, i+1)
		}
	}
}
