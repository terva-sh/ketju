package render

import (
	"strings"
	"sync"
	"testing"

	"golang.org/x/net/html"
)

// handles builds a HandleResolver over a set of canonical handles, matched
// case-insensitively as docs/04 requires. The id is the index plus one, so a
// test can assert on a specific number.
func handles(canonical ...string) HandleResolver {
	byLower := map[string]struct {
		id int64
		h  string
	}{}
	for i, h := range canonical {
		byLower[strings.ToLower(h)] = struct {
			id int64
			h  string
		}{int64(i + 1), h}
	}
	return HandleFunc(func(typed string) (int64, string, bool) {
		v, ok := byLower[strings.ToLower(typed)]
		return v.id, v.h, ok
	})
}

func renderMentions(t *testing.T, src string, r HandleResolver) Result {
	t.Helper()
	got, err := RenderWith(src, Context{Handles: r})
	if err != nil {
		t.Fatalf("RenderWith(%q): %v", src, err)
	}
	return got
}

// --- Acceptance criterion 1: a known handle links and reports ---------------

func TestAMentionLinksToTheProfileAndReportsTheID(t *testing.T) {
	got := renderMentions(t, "thanks @alice for the fix", handles("alice"))

	if !strings.Contains(got.HTML, `href="/u/alice"`) {
		t.Errorf("no profile link:\n%s", got.HTML)
	}
	if !strings.Contains(got.HTML, `class="mention"`) {
		t.Errorf("the mention is not marked for styling:\n%s", got.HTML)
	}
	if !strings.Contains(got.HTML, ">@alice</a>") {
		t.Errorf("the link text lost its @:\n%s", got.HTML)
	}
	if len(got.Mentions) != 1 || got.Mentions[0].UserID != 1 {
		t.Fatalf("Mentions = %+v, want one with UserID 1", got.Mentions)
	}
	assertSafe(t, "thanks @alice for the fix", got.HTML)
}

// TestTheCanonicalHandleIsUsedNotTheTypedOne is what makes a rename survivable.
//
// Handles are case-insensitive, so "@Alice" and "@ALICE" are the same person.
// Rendering the typed spelling would mean the same account appearing under
// three different names in three posts, and a link that only works because the
// route happens to be case-insensitive too.
func TestTheCanonicalHandleIsUsedNotTheTypedOne(t *testing.T) {
	for _, typed := range []string{"@alice", "@Alice", "@ALICE"} {
		got := renderMentions(t, "hi "+typed, handles("alice"))
		if !strings.Contains(got.HTML, `href="/u/alice"`) {
			t.Errorf("%s did not link to the canonical handle:\n%s", typed, got.HTML)
		}
		if strings.Contains(got.HTML, "ALICE") || strings.Contains(got.HTML, "/u/Alice") {
			t.Errorf("%s rendered the typed spelling:\n%s", typed, got.HTML)
		}
		if len(got.Mentions) != 1 || got.Mentions[0].Handle != "alice" {
			t.Errorf("%s: Mentions = %+v", typed, got.Mentions)
		}
	}
}

// TestTheIDIsWhatIsReported states why Mention carries a number.
//
// The source keeps the text the author typed, forever. If the projection stored
// handles, every row would break the day somebody renamed, and repairing them
// would mean rewriting posts. The id does not move.
func TestTheIDIsWhatIsReported(t *testing.T) {
	got := renderMentions(t, "@bob and @carol", handles("alice", "bob", "carol"))
	if len(got.Mentions) != 2 {
		t.Fatalf("Mentions = %+v, want two", got.Mentions)
	}
	if got.Mentions[0].UserID != 2 || got.Mentions[1].UserID != 3 {
		t.Errorf("ids = %d, %d; want 2, 3 in document order",
			got.Mentions[0].UserID, got.Mentions[1].UserID)
	}
}

func TestTheSamePersonMentionedTwiceIsReportedOnce(t *testing.T) {
	got := renderMentions(t, "@alice, and again @alice, and @Alice", handles("alice"))

	// Both mentions are still linked for the reader.
	if n := strings.Count(got.HTML, `href="/u/alice"`); n != 3 {
		t.Errorf("%d links rendered, want 3", n)
	}
	// But the notifier is told once. Handed three, a naive one sends three
	// notifications for a single post.
	if len(got.Mentions) != 1 {
		t.Errorf("Mentions = %+v, want one", got.Mentions)
	}
}

// --- Acceptance criterion 2: an unknown handle is text ----------------------

func TestAMentionOfNobodyIsPlainText(t *testing.T) {
	src := "hello @nobody and @alice"
	got := renderMentions(t, src, handles("alice"))

	if strings.Contains(got.HTML, "/u/nobody") {
		t.Errorf("a handle nobody has was linked:\n%s", got.HTML)
	}
	if !strings.Contains(got.HTML, "@nobody") {
		t.Errorf("the text the author typed was dropped:\n%s", got.HTML)
	}
	if len(got.Mentions) != 1 || got.Mentions[0].Handle != "alice" {
		t.Errorf("Mentions = %+v, want only alice", got.Mentions)
	}
}

func TestWithoutAResolverMentionsAreText(t *testing.T) {
	// Deliberately the opposite of a quote without a resolver. A quote links to
	// an ordinal in the thread the reader is already on, so optimism is usually
	// right. A mention would link to a profile for someone who may not exist,
	// and a preview that tells an author they have addressed a person who is not
	// there is worse than one showing plain text.
	got, err := Render("hi @alice")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.HTML, "/u/alice") {
		t.Errorf("a mention was linked with no resolver:\n%s", got.HTML)
	}
	if !strings.Contains(got.HTML, "@alice") {
		t.Errorf("the mention text was lost:\n%s", got.HTML)
	}
	if len(got.Mentions) != 0 {
		t.Errorf("Mentions = %+v, want none", got.Mentions)
	}
}

// --- Acceptance criterion 3: code and quotes do not mention -----------------

func TestMentionsInCodeAreNotResolved(t *testing.T) {
	cases := []string{
		"`@alice` in a code span",
		"```\n@alice\n```\n",
		"    @alice\n",
		"~~~js\nconst x = \"@alice\"\n~~~\n",
	}
	for _, src := range cases {
		got := renderMentions(t, src, handles("alice"))
		if strings.Contains(got.HTML, "/u/alice") {
			t.Errorf("a mention in code was linked:\n%s\n%s", src, got.HTML)
		}
		if len(got.Mentions) != 0 {
			t.Errorf("a mention in code notified: %s -> %+v", src, got.Mentions)
		}
		if !strings.Contains(got.HTML, "@alice") {
			t.Errorf("the code sample lost its text:\n%s\n%s", src, got.HTML)
		}
	}
}

// TestMentionsInsideAQuoteDoNotNotify is the half of the criterion with a
// reason behind it rather than a mechanism.
//
// Quoting somebody is repeating their words, not addressing the people they
// addressed. Without this, one post that mentions three people becomes three
// notifications every time anybody quotes it, and the more the post is worth
// quoting the more its subjects are punished for being in it.
func TestMentionsInsideAQuoteDoNotNotify(t *testing.T) {
	src := "> [quote=#3]\n> as @alice said earlier\n\nI agree, and so does @bob\n"
	got, err := RenderWith(src, Context{
		Handles: handles("alice", "bob"),
		Posts:   resolver([]uint32{3}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.HTML, "/u/alice") {
		t.Errorf("a mention inside a quote was linked:\n%s", got.HTML)
	}
	if !strings.Contains(got.HTML, "@alice") {
		t.Errorf("the quoted text lost the mention:\n%s", got.HTML)
	}
	if !strings.Contains(got.HTML, "/u/bob") {
		t.Errorf("the mention outside the quote was not linked:\n%s", got.HTML)
	}
	if len(got.Mentions) != 1 || got.Mentions[0].Handle != "bob" {
		t.Errorf("Mentions = %+v, want only bob", got.Mentions)
	}
}

func TestMentionsInAPlainBlockquoteDoNotNotifyEither(t *testing.T) {
	// A blockquote without an attribution is still somebody else's words most
	// of the time, and the cost of being wrong in this direction is a missing
	// notification rather than an unwanted one.
	got := renderMentions(t, "> @alice wrote this\n", handles("alice"))
	if len(got.Mentions) != 0 {
		t.Errorf("Mentions = %+v, want none", got.Mentions)
	}
	if strings.Contains(got.HTML, "/u/alice") {
		t.Errorf("a mention in a blockquote was linked:\n%s", got.HTML)
	}
}

// --- The parse boundary ------------------------------------------------------

// TestAnEmailAddressIsNotAMention is the one that would be noticed immediately.
//
// Without the preceding-character check, every address written in a post
// mentions whoever is named after the domain, which on a forum discussing
// anything technical is most posts.
func TestAnEmailAddressIsNotAMention(t *testing.T) {
	for _, src := range []string{
		"write to bob@example.com",
		"bob@alice",
		"see admin@alice for help",
	} {
		got := renderMentions(t, src, handles("alice", "example.com"))
		if strings.Contains(got.HTML, `class="mention"`) {
			t.Errorf("an address became a mention:\n%s\n%s", src, got.HTML)
		}
		if len(got.Mentions) != 0 {
			t.Errorf("%s notified %+v", src, got.Mentions)
		}
	}
}

// TestAMentionInsideALinkStaysText is a correctness bug, not a policy.
//
// A mention is itself an anchor, and an anchor inside an anchor is not valid
// HTML: a browser handed one closes the outer element early, so the author's
// link and the mention both come out wrong.
func TestAMentionInsideALinkStaysText(t *testing.T) {
	src := "[@alice](https://example.org/somewhere)"
	got := renderMentions(t, src, handles("alice"))

	if strings.Contains(got.HTML, "/u/alice") {
		t.Errorf("a mention nested inside a link:\n%s", got.HTML)
	}
	if !strings.Contains(got.HTML, `href="https://example.org/somewhere"`) {
		t.Errorf("the author's link was lost:\n%s", got.HTML)
	}
	if n := strings.Count(got.HTML, "<a "); n != 1 {
		t.Errorf("%d anchors in the output, want 1:\n%s", n, got.HTML)
	}
	assertSafe(t, src, got.HTML)
}

func TestWhereAHandleEnds(t *testing.T) {
	cases := []struct {
		src  string
		want string // the canonical handle expected, or "" for no mention
	}{
		{"@alice", "alice"},
		{"@alice.", "alice"},
		{"@alice, hello", "alice"},
		{"(@alice)", "alice"},
		{"@alice's post", "alice"},
		{"@alice\n", "alice"},
		{"say @alice!", "alice"},
		{"@alice-bot", "alice-bot"},
		{"@alice_2", "alice_2"},
		{"@", ""},
		{"@ alice", ""},
		{"@.", ""},
		{"@-", ""},
		{"email@alice", ""},
	}
	r := handles("alice", "alice-bot", "alice_2")
	for _, c := range cases {
		got := renderMentions(t, c.src, r)
		if c.want == "" {
			if len(got.Mentions) != 0 {
				t.Errorf("%q mentioned %+v, want none", c.src, got.Mentions)
			}
			continue
		}
		if len(got.Mentions) != 1 || got.Mentions[0].Handle != c.want {
			t.Errorf("%q -> %+v, want %s", c.src, got.Mentions, c.want)
		}
	}
}

func TestAnOverlongHandleIsNotAMention(t *testing.T) {
	long := strings.Repeat("a", MaxHandle+10)
	got := renderMentions(t, "@"+long, handles(long))
	if len(got.Mentions) != 0 {
		t.Errorf("a handle past the ceiling was resolved: %+v", got.Mentions)
	}
}

// --- Safety -------------------------------------------------------------------

// TestAHandleCannotEscapeTheHref. The handle reaches both an href and the link
// text, so it is the injection surface. Two defences apply and both are
// exercised: the parser only reads handle characters, and the resolver decides
// what is canonical.
func TestAHandleCannotEscapeTheHref(t *testing.T) {
	// A resolver that hands back a hostile canonical spelling is the worst case:
	// it models a compromised or buggy user store, and the renderer must still
	// not produce markup from it.
	hostile := HandleFunc(func(typed string) (int64, string, bool) {
		return 9, `x" onmouseover="alert(1)` + "\"><script>alert(1)</script>", true
	})
	src := "hi @alice"
	got := renderMentions(t, src, hostile)

	// assertSafe parses the output and checks elements, attribute names and URL
	// schemes. Grepping for "onmouseover=" here would fail against a correct
	// result, because the hostile string is present -- escaped, as link text, in
	// a position no browser acts on. That distinction is the whole point.
	assertSafe(t, src, got.HTML)

	// Structurally: whatever survived must not be a usable link to anywhere. In
	// practice the sanitizer drops the href entirely, because "/u/" followed by
	// that string is not a URL it will keep.
	for _, href := range hrefsIn(t, got.HTML) {
		if strings.ContainsAny(href, `"'<>`) {
			t.Errorf("an href carries markup characters: %q", href)
		}
		if !strings.HasPrefix(href, "/u/") {
			t.Errorf("a mention linked somewhere other than a profile: %q", href)
		}
	}
}

// hrefsIn returns every href in the output, parsed rather than matched.
func hrefsIn(t *testing.T, out string) []string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatalf("parsing the output: %v", err)
	}
	var found []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, a := range n.Attr {
				if a.Key == "href" {
					found = append(found, a.Val)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return found
}

func TestMentionSyntaxCannotInjectThroughTheTypedText(t *testing.T) {
	for _, src := range []string{
		`@alice"><script>alert(1)</script>`,
		"@alice'onmouseover='alert(1)",
		"@<script>alert(1)</script>",
		"@javascript:alert(1)",
	} {
		got := renderMentions(t, src, handles("alice", "javascript:alert(1)"))
		assertSafe(t, src, got.HTML)
	}
}

func TestMentionRenderingIsSafeForConcurrentUse(t *testing.T) {
	src := "@alice and @bob talking, plus `@carol` in code"
	r := handles("alice", "bob", "carol")

	want, err := RenderWith(src, Context{Handles: r})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				got, err := RenderWith(src, Context{Handles: r})
				if err != nil {
					t.Errorf("RenderWith: %v", err)
					return
				}
				if got.HTML != want.HTML || len(got.Mentions) != 2 {
					t.Errorf("concurrent renders disagree: %q / %+v", got.HTML, got.Mentions)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestQuotesAndMentionsComeOutOfOneParse(t *testing.T) {
	src := "> [quote=#3]\n> @alice said so\n\n@bob what do you think\n"
	got, err := RenderWith(src, Context{
		Handles: handles("alice", "bob"),
		Posts:   resolver([]uint32{3}),
	})
	if err != nil {
		t.Fatal(err)
	}
	// One call, both answers. The alternative is two parses that can disagree
	// about what the source says.
	if len(got.Quotes) != 1 || got.Quotes[0].Ordinal != 3 {
		t.Errorf("Quotes = %+v", got.Quotes)
	}
	if len(got.Mentions) != 1 || got.Mentions[0].Handle != "bob" {
		t.Errorf("Mentions = %+v", got.Mentions)
	}
}
