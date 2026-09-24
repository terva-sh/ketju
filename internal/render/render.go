// Package render turns post source into HTML. It is the only thing in the
// system that does.
//
// Everything that shows a post goes through [Render]: the thread page, the
// composer preview at POST /render, the search indexer's excerpt, the
// notification body. One function, because the promise the composer makes —
// that the preview is exactly what will be posted — is only true if the
// preview and the post are produced by the same code, and it stops being true
// the moment a second path exists. It is also why there is no client-side
// markdown parser: every JavaScript parser drifts from goldmark, and a preview
// that drifts is worse than no preview.
//
// # The renderer version
//
// [Version] is part of every cache key and part of every ETag (docs/03). That
// is what makes a pipeline upgrade free: bumping the constant invalidates
// every piece of cached HTML in the system at once, without rewriting a log,
// touching the database, or knowing which posts were affected.
//
// It only works if the constant is honest, so the rule is:
//
//	Any change that can change the output of Render must bump Version.
//
// "Can change" is the operative phrase. It covers upgrading goldmark or
// bluemonday, adding or removing an extension, changing a sanitizer rule, and
// changing a renderer option — whether or not any post in the corpus happens
// to render differently today. A change that leaves stale HTML in the caches
// is not a rendering bug that shows up in a test; it is a rendering bug that
// shows up for one reader, on one post, weeks later.
//
// The rule is enforced rather than merely written down. TestGoldenOutput keeps
// a per-case hash of the output in testdata/pipeline.sum, and updating a
// golden whose content changed is refused unless Version was bumped first.
//
// # Sanitization
//
// Raw HTML never reaches the output. goldmark drops it during rendering, and
// what goldmark produces is then run through a bluemonday allowlist built from
// nothing — not from UGCPolicy — so that an element goldmark starts emitting
// after an upgrade is stripped until somebody adds it deliberately. Both
// halves are load-bearing: goldmark's raw-HTML handling is a rendering option
// that an extension could change, and the sanitizer is the backstop that does
// not care how the HTML was produced.
package render

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

// Version identifies this pipeline's output. Read the rule above before
// changing it, and read it again before deciding not to.
const Version = 3

// MaxSource is the largest source Render will accept.
//
// 256 KiB is far more than a post and far less than MaxFrameSize (docs/03), so
// a source that cannot be stored is refused by the renderer before it reaches
// the thread actor, where the failure would arrive after the composer had
// already accepted it.
const MaxSource = 256 << 10

// Result is one rendered post.
//
// It is a struct rather than a string because quotes, mentions and reply_to all
// come out of the same parse, and adding one must not mean changing every call
// site. reply_to is the remaining piece (TKT-01M29C2WWG).
type Result struct {
	// HTML is sanitized and safe to write into a page without escaping.
	HTML string

	// Quotes is every post this one quotes, in document order, for the
	// projection writer to record as backlinks so a quoted post can show who
	// quoted it. A quote of an ordinal the resolver does not know is not
	// listed: there is nothing to link back to.
	Quotes []Quote

	// Mentions is every user this post mentions, in document order, for the
	// notifier and the mentions projection. A handle nobody has is not listed,
	// and neither is a mention inside a quotation: see resolveMentions.
	Mentions []Mention

	// ReplyTo is the post this one answers, validated against
	// Context.Posts: Context.ReplyTo when that post exists in the thread,
	// nil for a post that answers none. It never reaches the HTML, which is
	// why validating it here did not move Version.
	ReplyTo *uint32
}

// ErrTooLarge means the source exceeded MaxSource.
var ErrTooLarge = fmt.Errorf("render: source is larger than %d bytes", MaxSource)

// ErrInvalidReplyTo means Context.ReplyTo names no post in the thread: an
// ordinal that does not exist, or one the resolver does not know because it
// is in another thread. It is refused at render rather than stored, because
// a reply link to nowhere is a fact the thread log would keep forever.
var ErrInvalidReplyTo = errors.New("render: reply_to names no post in this thread")

// Context is what a render needs to know that the source does not say.
//
// The zero value is valid and means "nothing to resolve against", which is what
// a composer preview has before the post exists.
type Context struct {
	// Posts resolves quoted ordinals. Nil means quotes are rendered
	// optimistically as links and reported as backlinks, because a preview that
	// declined to link would disagree with the posted result, and the promise
	// that they match is the reason this package is the only renderer.
	Posts PostResolver

	// Handles resolves mentioned handles. Nil means mentions render as plain
	// text and none are reported.
	//
	// The asymmetry with Posts is deliberate. An unresolved quote still links
	// to an ordinal in the thread the reader is already on, so an optimistic
	// link is right far more often than not. An unresolved mention would link
	// to a profile page for a person who may not exist, and a preview that
	// promises a reader they have addressed somebody is worse than one that
	// shows them plain text.
	Handles HandleResolver

	// ReplyTo is the post the author says this one answers, nil for none. It
	// is checked against Posts, and a render with a ReplyTo and no Posts to
	// check it against fails: a reply can only be validated in its thread.
	// A deleted post still exists and is accepted: only a post that is not
	// there is refused. Whether a reply to a deleted post should be refused
	// is open in the docs (TKT-01M29C2WWG's notes), and refusing it is a
	// narrower rule that can be added here without changing a stored fact.
	ReplyTo *uint32
}

// Render converts markdown source to sanitized HTML, with nothing to resolve
// against. It is Render with an empty [Context].
func Render(source string) (Result, error) {
	return RenderWith(source, Context{})
}

// RenderWith converts markdown source to sanitized HTML.
//
// It is safe for concurrent use. The parser and renderer are built once and
// hold no per-document state; the footnote extension keeps its state in a
// context created per parse, and quote resolution walks the document this call
// produced.
func RenderWith(source string, ctx Context) (Result, error) {
	if len(source) > MaxSource {
		return Result{}, ErrTooLarge
	}

	var replyTo *uint32
	if ctx.ReplyTo != nil {
		if ctx.Posts == nil {
			return Result{}, ErrInvalidReplyTo
		}
		if exists, _ := ctx.Posts.ResolvePost(*ctx.ReplyTo); !exists {
			return Result{}, ErrInvalidReplyTo
		}
		r := *ctx.ReplyTo
		replyTo = &r
	}

	src := []byte(source)
	var buf bytes.Buffer
	doc := mdParser.Parse(src)
	quotes := resolveQuotes(doc, ctx.Posts)
	mentions := resolveMentions(doc, ctx.Handles)
	if err := mdRenderer.Render(&buf, src, doc); err != nil {
		return Result{}, fmt.Errorf("render: %w", err)
	}
	return Result{HTML: sanitize(buf.String()), Quotes: quotes, Mentions: mentions, ReplyTo: replyTo}, nil
}

// mdParser and mdRenderer are the pipeline. They are package-level because
// building them costs more than rendering a short post and because there must
// be exactly one configuration in the process — two would be two renderer
// versions sharing one constant.
var (
	mdParser = parser.New(parser.WithExtensions(
		// GFM: tables, strikethrough, task lists, autolinks. The set a person
		// who has used any other forum expects to work.
		extension.GFMParser,
		extension.FootnoteParser,
		// Typographer turns quotes and dashes into their typographic forms.
		// It is a rendering choice, not a content change: the source keeps
		// the straight quotes, so quoting a post still quotes what was typed.
		extension.TypographerParser,
	),
		parser.WithParagraphTransformers(
			util.Prioritized[parser.ParagraphTransformer](quoteAttribution{}, 100),
		),
		parser.WithInlineParsers(
			// Priority below goldmark's own inline parsers: an @ inside a link
			// destination or a code span is not a mention, and letting those
			// parsers claim their text first is what keeps it that way.
			util.Prioritized[parser.InlineParser](mentionParser{}, 999),
		),
		parser.WithASTTransformers(
			util.Prioritized[parser.ASTTransformer](quoteBuilder{}, 100),
		),
	)

	mdRenderer = html.New(html.WithExtensions(
		extension.GFMHTMLRenderer,
		extension.FootnoteHTMLRenderer,
		// Table alignment as an align attribute rather than an inline style.
		// The two render identically, and the attribute means the sanitizer
		// never has to parse CSS — a whole parser, and a whole class of
		// sanitizer bypass, that this pipeline simply does not contain.
		extension.NewTableHTMLRenderer(
			extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute),
		),
		// Attributed quotations. A plain blockquote keeps goldmark's own
		// renderer, untouched.
		NewQuoteRenderer(),
		NewMentionRenderer(),
	))
)
