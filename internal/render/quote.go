package render

import (
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

// The quote syntax is a normal markdown blockquote whose first line is an
// attribution:
//
//	> [quote=#37]
//	> the text that was quoted
//
// Keeping the attribution inside the blockquote rather than above it is the
// decision worth recording. It means any renderer that has never heard of this
// extension -- a plain markdown viewer, a mail client showing a notification,
// a future export -- still shows the quoted text as a quotation, with one
// unfamiliar line at the top, instead of showing an orphan line followed by an
// unattributed quote. It also makes the whole construct one block, so nothing
// can separate the attribution from what it attributes.
//
// The ordinal is digits only and bounded, but the pattern is not what makes
// that safe, and it is worth being exact about which layer does what. Loosening
// this expression to accept any text changes nothing: strconv.ParseUint refuses
// what is not a number, the range check refuses what is out of range, and the
// value that reaches the href is written with %d from a uint32, so no string
// taken from the source is ever formatted into an attribute. The pattern is
// here to keep the syntax narrow for the reader, not to be the last line of
// defence -- it isn't one.
var quoteLine = regexp.MustCompile(`^\[quote=#([0-9]{1,9})\][ \t]*$`)

// MaxQuoteOrdinal is the largest ordinal the syntax accepts. Ordinals number
// the posts within one thread, and a thread with a billion posts is not a
// thread.
const MaxQuoteOrdinal = 999_999_999

// Quote is one quotation found in a post. The projection writer records them
// as backlinks, through QuotedOrdinals, so the quoted post can show who
// quoted it.
type Quote struct {
	// Ordinal is the post being quoted.
	Ordinal uint32

	// Deleted reports that the quoted post exists but has been deleted, so the
	// rendered output carries a tombstone instead of the quoted text.
	Deleted bool
}

// QuotedOrdinals is every post a source quotes, other than self, each once
// and in ascending order: the quote backlinks the projection writer records
// (docs/04 "post_quotes", TKT-01M3YME9AZ).
//
// It renders with no resolver, so every quotation is reported whether or not
// its ordinal exists, and the answer depends on the source alone. That is
// what lets a replay or a reindex reproduce the rows exactly. Whether a
// quoted post exists, and whether a quoting one is visible, is the reader's
// to decide when it reads them, as a quote on the thread page resolves
// against the thread as it is now. A quotation nested inside another counts,
// because Result.Quotes lists it.
//
// A source with no attribution line is not rendered at all. A source the
// renderer refuses quotes nothing: it was never posted.
func QuotedOrdinals(source string, self uint32) []uint32 {
	if !strings.Contains(source, "[quote=#") {
		return nil
	}
	r, err := RenderWith(source, Context{})
	if err != nil {
		return nil
	}
	var out []uint32
	for _, q := range r.Quotes {
		if q.Ordinal != self {
			out = append(out, q.Ordinal)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// PostResolver answers what the renderer needs to know about a quoted post.
//
// It is an interface rather than a map because the caller is the thread page,
// which already holds the index and can answer from memory. Requiring a map
// would mean building one on every render.
type PostResolver interface {
	// ResolvePost reports whether a post with this ordinal exists in the thread
	// being rendered, and whether it has been deleted.
	ResolvePost(ordinal uint32) (exists, deleted bool)
}

// ResolverFunc adapts a function to PostResolver.
type ResolverFunc func(ordinal uint32) (exists, deleted bool)

// ResolvePost implements PostResolver.
func (f ResolverFunc) ResolvePost(ordinal uint32) (bool, bool) { return f(ordinal) }

// quoteState is what the renderer does with one attributed blockquote.
type quoteState int

const (
	// quoteLinked is the ordinary case: the post exists, so the quotation is
	// shown with a link to it.
	quoteLinked quoteState = iota

	// quoteTombstone is a quote of a deleted post. The quoted text is dropped.
	quoteTombstone
)

// KindQuote is the node kind for an attributed quotation.
var KindQuote = ast.NewNodeKind("KetjuQuote")

// quoteNode is a blockquote that carried an attribution line.
//
// It is a distinct node rather than a flag on ast.Blockquote because goldmark
// v2's node attributes hold text values taken from the source, not arbitrary
// Go values, and because a separate kind means the renderer for a plain
// blockquote stays exactly the renderer goldmark ships.
type quoteNode struct {
	ast.BaseBlock

	Ordinal uint32
	State   quoteState
}

// newQuoteNode builds a quotation node.
//
// The Init call is not optional and not decoration. goldmark v2's BaseNode
// keeps a reference to the concrete node it is embedded in, and AppendChild
// uses it to set each child's parent. Without Init that reference is nil, every
// child moved into this node is silently orphaned, and the symptom is a nested
// quotation that parses correctly and then renders as a plain blockquote.
func newQuoteNode(ordinal uint32) *quoteNode {
	q := &quoteNode{Ordinal: ordinal}
	q.Init(q)
	return q
}

func (*quoteNode) Kind() ast.NodeKind { return KindQuote }

func (n *quoteNode) Dump(source []byte) *ast.NodeDump {
	return ast.NewNodeDump(n, map[string]any{
		"Ordinal": n.Ordinal,
		"State":   int(n.State),
	})
}

// pendingQuotes is the set of blockquotes the paragraph transformer found, read
// by the AST transformer once inline parsing is done.
//
// It travels through the parse context rather than through a field on the
// transformer, because the parser is a package-level singleton shared by every
// concurrent render, and a field on it would be shared mutable state across
// them.
var pendingQuotes = parser.NewContextKey()

// quoteAttribution strips the attribution line and records the blockquote that
// carried it.
//
// It is a ParagraphTransformer rather than an ASTTransformer because of when
// each one runs. AST transformers run after inline parsing, by which point
// "[quote=#37]" has become a run of inline nodes whose shape depends on how the
// link parser gave up on an unclosed bracket. A paragraph transformer runs
// while the paragraph is still a list of source lines, where the attribution is
// a line and removing it is removing a line.
type quoteAttribution struct{}

func (quoteAttribution) Transform(node *ast.Paragraph, reader text.Reader, pc parser.Context) {
	bq, ok := node.Parent().(*ast.Blockquote)
	if !ok || bq.FirstChild() != ast.Node(node) {
		return
	}
	lines := node.Source()
	if len(lines) == 0 {
		return
	}

	m := quoteLine.FindSubmatch(trimLineEnd(lines[0].Bytes(reader.Source())))
	if m == nil {
		return
	}
	n, err := strconv.ParseUint(string(m[1]), 10, 32)
	if err != nil || n == 0 || n > MaxQuoteOrdinal {
		// Ordinal zero is not a post and neither is anything past the ceiling.
		// Both stay as the literal text somebody typed rather than becoming a
		// link to nothing.
		return
	}

	found, _ := pc.Get(pendingQuotes).(map[ast.Node]uint32)
	if found == nil {
		found = map[ast.Node]uint32{}
		pc.Set(pendingQuotes, found)
	}
	found[bq] = uint32(n)

	if len(lines) == 1 {
		// The blockquote was the attribution and nothing else. Drop the empty
		// paragraph rather than render a blank line.
		bq.RemoveChild(node)
		return
	}
	node.SetSource(lines[1:])
}

// trimLineEnd removes the line terminator, which a segment includes.
func trimLineEnd(line []byte) []byte {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	return line
}

// quoteBuilder replaces each marked blockquote with a quoteNode.
type quoteBuilder struct{}

func (quoteBuilder) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	found, _ := pc.Get(pendingQuotes).(map[ast.Node]uint32)
	if len(found) == 0 {
		return
	}
	for node, ordinal := range found {
		parent := node.Parent()
		if parent == nil {
			continue
		}
		q := newQuoteNode(ordinal)
		for c := node.FirstChild(); c != nil; c = node.FirstChild() {
			node.RemoveChild(c)
			q.AppendChild(c)
		}
		parent.ReplaceChild(node, q)
	}
	// The map is per-parse, but the context can be supplied by the caller, so
	// leaving entries behind would let one parse see another's nodes.
	pc.Set(pendingQuotes, map[ast.Node]uint32{})
}

// resolveQuotes decides what each quotation becomes and returns the backlinks.
//
// Resolution happens here, between parsing and rendering, rather than inside a
// goldmark extension, because the resolver belongs to the caller. Threading a
// per-call value through a package-level parser would mean either shared
// mutable state or a parser built per render, and both are worse than one walk.
func resolveQuotes(doc ast.Node, posts PostResolver) []Quote {
	var quotes []Quote
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		q, ok := n.(*quoteNode)
		if !ok {
			return ast.WalkContinue, nil
		}

		if posts == nil {
			// No resolver: a composer preview, rendered before the post exists.
			// The link is written optimistically and the backlink reported,
			// because declining to link would make the preview disagree with
			// the posted result, and that agreement is the one promise this
			// package exists to keep.
			q.State = quoteLinked
			quotes = append(quotes, Quote{Ordinal: q.Ordinal})
			return ast.WalkContinue, nil
		}

		exists, deleted := posts.ResolvePost(q.Ordinal)
		switch {
		case !exists:
			// The fourth acceptance criterion: an ordinal nobody can resolve
			// must not fail the post and must not become a link to nothing.
			// Demote it to an ordinary blockquote, which is what a reader would
			// have seen if the attribution had never been typed.
			demoteToBlockquote(q)
		case deleted:
			q.State = quoteTombstone
			// The quoted text lives in the quoting post's own source, copied
			// there by the composer. Deleting a post therefore does not remove
			// it from the posts that quoted it -- only this does. Dropping the
			// children at render time is what makes a delete effective
			// everywhere without rewriting anybody else's post.
			q.RemoveChildren()
			quotes = append(quotes, Quote{Ordinal: q.Ordinal, Deleted: true})
		default:
			q.State = quoteLinked
			quotes = append(quotes, Quote{Ordinal: q.Ordinal})
		}
		return ast.WalkContinue, nil
	})
	return quotes
}

func demoteToBlockquote(q *quoteNode) {
	parent := q.Parent()
	if parent == nil {
		return
	}
	bq := ast.NewBlockquote()
	for c := q.FirstChild(); c != nil; c = q.FirstChild() {
		q.RemoveChild(c)
		bq.AppendChild(c)
	}
	parent.ReplaceChild(q, bq)
}

// quoteRenderer writes quotation blocks.
type quoteRenderer struct{}

// NewQuoteRenderer returns the html.Extension that renders quote blocks.
func NewQuoteRenderer() html.Extension { return &quoteRenderer{} }

func (r *quoteRenderer) RendererOptions(_ *html.Config) []html.Option {
	return []html.Option{
		html.WithNodeRenderers(map[ast.NodeKind]html.NodeRenderer{
			KindQuote: html.NodeRendererFunc(r.renderQuote),
		}),
	}
}

func (r *quoteRenderer) renderQuote(
	writer io.Writer, source []byte, n ast.Node, entering bool, rc renderer.Context,
) (ast.WalkStatus, error) {
	w := writer.(util.BufWriter)
	q := n.(*quoteNode)

	if !entering {
		_, _ = w.WriteString("</blockquote>\n</details>\n")
		return ast.WalkContinue, nil
	}

	// <details> collapses with no script at all, which matters because the
	// non-JS client (docs/07) has to be able to collapse a quote too. It is open
	// by default: a quote the reader must click to see is a quote that may as
	// well not be there.
	//
	// The summary is plain text and the source link is the blockquote's first
	// line (TKT-01M3BFSBPJ). A <summary> is itself a control, the toggle, and a
	// link inside it is a control inside a control: assistive technology
	// announces the summary as a button and may never reach the link as one
	// (axe nested-interactive, WCAG 4.1.2).
	fmt.Fprintf(w, "<details class=\"quote\" open>\n<summary>Quote from #%d</summary>\n<blockquote>\n", q.Ordinal)
	fmt.Fprintf(w, "<p class=\"quote-attribution\"><a href=\"#post-%d\" class=\"quote-source\">Post #%d</a></p>\n", q.Ordinal, q.Ordinal)

	if q.State == quoteTombstone {
		_, _ = w.WriteString("<p class=\"quote-deleted\">This post was deleted.</p>\n")
	}
	return ast.WalkContinue, nil
}
