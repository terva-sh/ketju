package render

import (
	"regexp"
	"sync"

	"github.com/microcosm-cc/bluemonday"
)

// sanitize applies the output allowlist.
func sanitize(h string) string { return policy().Sanitize(h) }

// policy builds the allowlist once.
//
// It starts from bluemonday.NewPolicy rather than from UGCPolicy, and that is
// the important decision. UGCPolicy permits a generous set of elements for
// arbitrary user HTML; this pipeline does not have arbitrary user HTML, it has
// goldmark's output, which is a short and knowable list. Starting from nothing
// means the allowlist and the list of things goldmark emits are the same list,
// and an element goldmark starts emitting after an upgrade is stripped until
// somebody adds it here on purpose. Starting from UGCPolicy would mean new
// output silently passing through because it happened to be on somebody
// else's list.
var policy = sync.OnceValue(func() *bluemonday.Policy {
	p := bluemonday.NewPolicy()

	// http, https and mailto only, URLs must parse, relative URLs allowed
	// (in-site links), and rel="nofollow" added to every link.
	p.AllowStandardURLs()

	// Text and structure goldmark emits.
	p.AllowElements(
		"p", "br", "hr", "blockquote",
		"h1", "h2", "h3", "h4", "h5", "h6",
		"em", "strong", "del",
		"pre", "code",
	)

	// Links. class and role appear only on footnote links; both are matched
	// against the exact values goldmark writes rather than a pattern, so a
	// future extension cannot smuggle a third value through.
	p.AllowAttrs("href").OnElements("a")
	// A link or image title is the tooltip markdown's [x](url "title") syntax
	// exists to set. It is plain text, escaped on the way out, and dropping it
	// would silently lose something the author wrote.
	p.AllowAttrs("title").OnElements("a", "img")
	p.AllowAttrs("class").Matching(footnoteLinkClass).OnElements("a")
	p.AllowAttrs("role").Matching(footnoteLinkRole).OnElements("a")

	// Lists, including the start attribute for a list that does not begin at
	// one, and the task-list checkbox.
	p.AllowLists()
	p.AllowAttrs("start").Matching(bluemonday.Integer).OnElements("ol")
	p.AllowAttrs("type").Matching(checkboxOnly).OnElements("input")
	p.AllowAttrs("checked", "disabled").Matching(emptyValue).OnElements("input")
	p.AllowElements("input")

	// Tables. Alignment arrives as an attribute rather than a style, so this
	// policy never enables bluemonday's CSS parsing.
	p.AllowTables()
	p.AllowAttrs("align").Matching(tableAlign).OnElements("th", "td")

	// Images. An img keeps its src only when the src is an attachment
	// (attachmentSrc, image.go). The renderer already turns every other
	// markdown image into a link or text (docs/07, decided 2026-09-28), so
	// this is the second layer: an img that reaches the policy by any other
	// path, such as a future extension, loses its src and never loads, and
	// loading it would tell a remote host who read the thread. The other
	// rules are the ones AllowImages sets, which this replaced.
	p.AllowAttrs("align").Matching(bluemonday.ImageAlign).OnElements("img")
	p.AllowAttrs("alt").Matching(bluemonday.Paragraph).OnElements("img")
	p.AllowAttrs("height", "width").Matching(bluemonday.NumberOrPercent).OnElements("img")
	p.AllowAttrs("src").Matching(attachmentSrc).OnElements("img")

	// Code blocks keep their language class so that highlighting, whether in
	// the client or later in this pipeline, has something to key on.
	p.AllowAttrs("class").Matching(languageClass).OnElements("code")

	// Quotations. <details>/<summary> is what makes a quote collapsible with no
	// script, which the non-JS client needs as much as the Preact one. The
	// classes are matched against the exact values this package writes, so the
	// markup a stylesheet keys on cannot be minted by a post.
	p.AllowElements("details", "summary")
	p.AllowAttrs("class").Matching(quoteClass).OnElements("details")
	p.AllowAttrs("class").Matching(quoteParagraphClass).OnElements("p")
	p.AllowAttrs("open").Matching(emptyValue).OnElements("details")

	// Footnotes: the section wrapper, the reference anchors, and the ids the
	// two ends use to find each other. The id patterns are goldmark's exact
	// shapes; anything else is dropped, so a footnote label cannot mint an id
	// that collides with the page's own anchors.
	p.AllowElements("sup", "div", "section")
	p.AllowAttrs("class").Matching(footnoteSection).OnElements("div", "section")
	p.AllowAttrs("role").Matching(footnoteSectionRole).OnElements("div", "section")
	p.AllowAttrs("id").Matching(footnoteID).OnElements("li", "sup")

	return p
})

var (
	// The classes this pipeline writes on anchors: goldmark's two footnote
	// classes and the quote attribution link.
	footnoteLinkClass = regexp.MustCompile(`^(footnote-ref|footnote-backref|quote-source|mention)$`)
	footnoteLinkRole  = regexp.MustCompile(`^(doc-noteref|doc-backlink)$`)

	footnoteSection     = regexp.MustCompile(`^footnotes$`)
	footnoteSectionRole = regexp.MustCompile(`^doc-endnotes$`)

	// fn:1 and fnref:1 for numbered notes, fn:name for named ones.
	footnoteID = regexp.MustCompile(`^fn(ref)?:[0-9A-Za-z_-]{1,64}$`)

	// language-go, language-c++, language-objective-c. Bounded, because the
	// class comes from the info string, which is whatever the author typed.
	languageClass = regexp.MustCompile(`^language-[0-9A-Za-z_+#.-]{1,32}$`)

	// A task list checkbox and nothing else: no name, no value, no form
	// attributes, so there is no input here that could submit anything.
	checkboxOnly = regexp.MustCompile(`^checkbox$`)
	emptyValue   = regexp.MustCompile(`^$`)

	tableAlign = regexp.MustCompile(`^(left|center|right)$`)

	quoteClass = regexp.MustCompile(`^quote$`)
	// The two paragraphs a quote writes inside its blockquote: the attribution
	// line holding the source link, and a deleted post's tombstone.
	quoteParagraphClass = regexp.MustCompile(`^(quote-attribution|quote-deleted)$`)
)
