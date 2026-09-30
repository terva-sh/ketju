package render

import (
	"bytes"
	"io"
	"regexp"
	"strings"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/renderer"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

// attachmentSrc is the one image source a post may load: an attachment,
// served from this origin at GET /blobs/{hash} (docs/07).
//
// It is matched exactly, and that is the point. Deciding by "has no scheme"
// lost, because //host/x.png and /\host/x.png have no scheme and a browser
// loads both from another host. An attachment URL is a fixed shape, so the
// test is the shape and nothing looser. If TKT-01M29CXK0 (attachments) writes
// attachments into posts at another URL, this pattern changes with it, and so
// does Version.
var attachmentSrc = regexp.MustCompile(`^/blobs/[0-9a-f]{64}$`)

// imageRenderer writes markdown images.
//
// The CSP says img-src 'self' data:, so a remote image never loads; left to
// goldmark it renders as a broken image. Decided 2026-09-28 (TKT-01M2K1JKS7,
// docs/07): keep the CSP, and render a remote image as a link to its URL, so
// nothing renders broken and merely viewing a post sends nothing to the
// image's host. The rules, in the order they are applied:
//
//   - An attachment source stays an <img>, written as goldmark writes one,
//     inside a link or not: an <img> in an <a> nests no link.
//   - A source the link policy would refuse (javascript:, data:, anything
//     else goldmark or the sanitizer drops) becomes its alt text as plain
//     text, and nothing when there is none. There is no URL worth showing.
//   - Inside a link, an image becomes its alt text, or its URL when there is
//     none, as plain text. HTML cannot nest links; a parser splits a nested
//     <a> apart, and a second link after the first puts a link where the
//     author wrote none.
//   - Anything else becomes <a href="URL">alt or URL</a>, keeping the title.
type imageRenderer struct{}

// NewImageRenderer returns the html.Extension that renders images.
func NewImageRenderer() html.Extension { return &imageRenderer{} }

func (r *imageRenderer) RendererOptions(_ *html.Config) []html.Option {
	return []html.Option{
		html.WithNodeRenderers(map[ast.NodeKind]html.NodeRenderer{
			ast.KindImage: html.NodeRendererFunc(r.renderImage),
		}),
	}
}

func (r *imageRenderer) renderImage(
	writer io.Writer, source []byte, node ast.Node, entering bool, rc renderer.Context,
) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	w := writer.(util.BufWriter)
	tw := html.ContextTextWriter(rc)
	n := node.(*ast.Image)
	dest := n.Destination.Value(source)
	alt := altText(n, source)

	if attachmentSrc.MatchString(dest) {
		// goldmark's renderImage, less its Unsafe and XHTML branches, which
		// this pipeline never configures.
		_, _ = w.WriteString(`<img src="`)
		_, _ = html.ContextLinkURLWriter(rc).WriteString(dest)
		_, _ = w.WriteString(`" alt="`)
		_, _ = tw.WriteString(alt)
		_ = w.WriteByte('"')
		writeTitle(w, tw, n, source)
		if n.Attributes() != nil {
			html.RenderAttributes(w, source, n, html.ImageAttributeFilter, rc)
		}
		_ = w.WriteByte('>')
		return ast.WalkSkipChildren, nil
	}

	href := hrefBytes(dest)
	if !linkable(href) {
		_, _ = tw.WriteString(alt)
		return ast.WalkSkipChildren, nil
	}

	text := alt
	if strings.TrimSpace(text) == "" {
		text = dest
	}
	if insideLink(n) {
		_, _ = tw.WriteString(text)
		return ast.WalkSkipChildren, nil
	}

	_, _ = w.WriteString(`<a href="`)
	_, _ = w.Write(href)
	_ = w.WriteByte('"')
	writeTitle(w, tw, n, source)
	_ = w.WriteByte('>')
	_, _ = tw.WriteString(text)
	_, _ = w.WriteString(`</a>`)
	return ast.WalkSkipChildren, nil
}

func writeTitle(w, tw util.BufWriter, n *ast.Image, source []byte) {
	if n.Title.IsEmpty() {
		return
	}
	_, _ = w.WriteString(` title="`)
	_, _ = n.Title.WriteTo(tw, source)
	_ = w.WriteByte('"')
}

// hrefBytes is the href goldmark's link renderer would write for dest: empty
// for a URL goldmark calls dangerous, and otherwise the URL escaped as
// renderLink's URL writer escapes it (that writer is util.URLEscape and nothing
// else). Building it once means linkable judges exactly the bytes the renderer
// then writes.
func hrefBytes(dest string) []byte {
	if html.IsDangerousURL(dest) {
		return nil
	}
	return util.URLEscape([]byte(dest))
}

// linkable reports whether the sanitizer keeps href on a link to this
// (already escaped) URL.
//
// It asks the policy rather than restating it. A second copy of the scheme
// allowlist and bluemonday's parse rules would drift the first time either
// changed, and the renderer would start writing links the sanitizer strips,
// which are anchors with no destination. TestLinkableMatchesThePolicy holds
// it against a markdown link through the whole pipeline.
func linkable(href []byte) bool {
	if len(href) == 0 {
		return false
	}
	var b bytes.Buffer
	b.WriteString(`<a href="`)
	b.Write(href)
	b.WriteString(`">x</a>`)
	return strings.Contains(sanitize(b.String()), ` href="`)
}

// insideLink reports whether n has a link ancestor.
func insideLink(n ast.Node) bool {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == ast.KindLink {
			return true
		}
	}
	return false
}

// altText is an image's description as plain text: what goldmark writes into
// alt, plus the inline pieces its alt writer skips (code spans, autolinks and
// mentions), so a link made from an image shows what the author typed.
func altText(n ast.Node, source []byte) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c := c.(type) {
			case *ast.Text:
				b.WriteString(c.Value.Value(source))
				if c.SoftLineBreak() || c.HardLineBreak() {
					b.WriteByte('\n')
				}
			case *ast.CodeSpan:
				b.WriteString(c.Value.Value(source))
			case *ast.AutoLink:
				b.WriteString(c.Label.Value(source))
			case *mentionNode:
				b.WriteByte('@')
				b.WriteString(c.Typed)
			case *ast.RawHTML:
				// Raw HTML is never rendered, and it is not text either.
			default:
				walk(c)
			}
		}
	}
	walk(n)
	return b.String()
}
