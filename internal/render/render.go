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
	"fmt"

	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
)

// Version identifies this pipeline's output. Read the rule above before
// changing it, and read it again before deciding not to.
const Version = 1

// MaxSource is the largest source Render will accept.
//
// 256 KiB is far more than a post and far less than MaxFrameSize (docs/03), so
// a source that cannot be stored is refused by the renderer before it reaches
// the thread actor, where the failure would arrive after the composer had
// already accepted it.
const MaxSource = 256 << 10

// Result is one rendered post.
//
// It is a struct with one field on purpose. Mentions, quotes and reply_to come
// out of the same parse (TKT-01M29C2WWG), and adding them here later must not
// mean changing every call site.
type Result struct {
	// HTML is sanitized and safe to write into a page without escaping.
	HTML string
}

// ErrTooLarge means the source exceeded MaxSource.
var ErrTooLarge = fmt.Errorf("render: source is larger than %d bytes", MaxSource)

// Render converts markdown source to sanitized HTML.
//
// It is safe for concurrent use. The parser and renderer are built once and
// hold no per-document state; the footnote extension keeps its state in a
// context created per parse.
func Render(source string) (Result, error) {
	if len(source) > MaxSource {
		return Result{}, ErrTooLarge
	}

	src := []byte(source)
	var buf bytes.Buffer
	doc := mdParser.Parse(src)
	if err := mdRenderer.Render(&buf, src, doc); err != nil {
		return Result{}, fmt.Errorf("render: %w", err)
	}
	return Result{HTML: sanitize(buf.String())}, nil
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
	))

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
	))
)
