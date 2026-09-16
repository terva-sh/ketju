package render

import (
	"io"
	"strconv"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

// MaxHandle bounds a mention. It is generous on purpose: this is a ceiling on
// what the syntax will read, not the rule for what a handle may be.
const MaxHandle = 64

// Mention is one resolved mention, for the notifier and the mentions
// projection.
//
// It carries the user id rather than the handle because that is what makes an
// old post keep working after a handle change: the source keeps the text the
// author typed, and the identity the mention refers to is a number that does
// not move. Handle is the canonical spelling at the time of the render, for
// whoever wants to display it.
type Mention struct {
	UserID int64
	Handle string
}

// HandleResolver turns a handle into the user it names.
type HandleResolver interface {
	// ResolveHandle reports the user id for a handle and the canonical
	// spelling of it. Handles are case-insensitive (docs/04), so the canonical
	// spelling is what should be displayed and linked, not what was typed.
	ResolveHandle(handle string) (id int64, canonical string, ok bool)
}

// HandleFunc adapts a function to HandleResolver.
type HandleFunc func(handle string) (int64, string, bool)

// ResolveHandle implements HandleResolver.
func (f HandleFunc) ResolveHandle(h string) (int64, string, bool) { return f(h) }

// KindMention is the node kind for an @handle.
var KindMention = ast.NewNodeKind("KetjuMention")

type mentionNode struct {
	ast.BaseInline

	// Typed is the handle exactly as it appeared in the source.
	Typed string

	// Resolved is set once a resolver has answered.
	Resolved bool
	UserID   int64
	Handle   string

	// Quoted means this mention is inside a quotation or a link, so it is shown
	// as text rather than resolved. See resolveMentions.
	Quoted bool
}

func newMentionNode(typed string) *mentionNode {
	n := &mentionNode{Typed: typed}
	// goldmark v2's BaseNode holds a reference to the concrete node so that
	// AppendChild can parent its children. A custom node that skips this
	// orphans anything moved into it.
	n.Init(n)
	return n
}

func (*mentionNode) Kind() ast.NodeKind { return KindMention }

func (n *mentionNode) Dump(source []byte) *ast.NodeDump {
	return ast.NewNodeDump(n, map[string]any{
		"Typed":    n.Typed,
		"Resolved": n.Resolved,
		"UserID":   strconv.FormatInt(n.UserID, 10),
	})
}

// mentionParser reads @handle.
//
// The handle character set here is deliberately wider than any handle is likely
// to be allowed to be, and that direction is the safe one. This pattern decides
// only what text becomes a *candidate*; whether a candidate is a user is the
// resolver's answer. A pattern narrower than the real rule would mean mentions
// of some accounts silently never resolving, which nobody would report as a bug
// because it looks like the person simply was not mentioned.
//
// The authoritative handle rule is not written down anywhere yet -- docs/04
// says only "unique, case-insensitive" -- and belongs with the users schema
// (TKT-01M29BYYH7) and the handle change policy (TKT-01M29BSPEN).
type mentionParser struct{}

func (mentionParser) Trigger() []byte { return []byte{'@'} }

func (mentionParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, seg := block.PeekLine()
	if len(line) < 2 || line[0] != '@' {
		return nil
	}

	// An @ in the middle of a word is not a mention. Without this, the local
	// part of an email address would mention a user named after the domain, and
	// every address written in a post would notify somebody.
	if before := block.PrecedingCharacter(); isHandleByte(byte(before)) {
		return nil
	}

	n := 1
	for n < len(line) && n <= MaxHandle && isHandleByte(line[n]) {
		n++
	}
	handle := string(line[1:n])
	// Trailing punctuation belongs to the sentence, not the handle: "@alice."
	// at the end of a sentence mentions alice.
	for len(handle) > 0 && (handle[len(handle)-1] == '.' || handle[len(handle)-1] == '-') {
		handle = handle[:len(handle)-1]
		n--
	}
	if handle == "" || len(handle) > MaxHandle {
		return nil
	}

	block.Advance(n)
	_ = seg
	return newMentionNode(handle)
}

func isHandleByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '_', c == '-', c == '.':
		return true
	}
	return false
}

// resolveMentions walks the document, resolves each mention, and returns the
// ones that should be reported.
//
// A mention inside a quotation is rendered as plain text and not reported. A
// post that quotes somebody else's words is repeating them, not addressing the
// people they addressed, and notifying on a quote would mean one popular post
// could be requoted into an unbounded number of notifications for whoever it
// mentioned. Rendering it as a link would carry the same implication visually.
//
// A mention inside a link is also left as text, because a mention is itself a
// link and an anchor inside an anchor is not valid HTML. A browser handed one
// closes the outer element early, so the author's link and the mention both
// come out wrong.
//
// Code needs no handling here at all: goldmark does not run inline parsers
// inside code spans or code blocks, so @handle in a code sample never becomes
// a mention node in the first place.
//
// Repeated mentions of the same person are reported once. The consumers are the
// notifier and the mentions projection, and both want "this post mentions this
// person" rather than a count; a naive notifier handed two entries sends two
// notifications for one post. Quotes deliberately do not do this, because each
// quotation is a separate piece of content in the post rather than a second
// reference to the same thing.
func resolveMentions(doc ast.Node, handles HandleResolver) []Mention {
	var mentions []Mention
	seen := map[int64]bool{}
	var quoteDepth, linkDepth int

	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch n.(type) {
		case *quoteNode, *ast.Blockquote:
			if entering {
				quoteDepth++
			} else {
				quoteDepth--
			}
			return ast.WalkContinue, nil
		case *ast.Link, *ast.AutoLink:
			if entering {
				linkDepth++
			} else {
				linkDepth--
			}
			return ast.WalkContinue, nil
		}
		if !entering {
			return ast.WalkContinue, nil
		}
		m, ok := n.(*mentionNode)
		if !ok {
			return ast.WalkContinue, nil
		}

		if quoteDepth > 0 || linkDepth > 0 {
			m.Quoted = true
			return ast.WalkContinue, nil
		}
		if handles == nil {
			// No resolver: a composer preview with nothing to resolve against.
			// Unlike a quote, an unresolvable mention is not rendered as a link,
			// because a link to /u/whatever-was-typed is a link to a profile
			// that may not exist, and the preview would promise a person who is
			// not there.
			return ast.WalkContinue, nil
		}

		id, canonical, found := handles.ResolveHandle(m.Typed)
		if !found {
			// The second acceptance criterion. A handle nobody has is text
			// somebody typed, not a broken link and not a notification.
			return ast.WalkContinue, nil
		}
		m.Resolved, m.UserID, m.Handle = true, id, canonical
		if !seen[id] {
			seen[id] = true
			mentions = append(mentions, Mention{UserID: id, Handle: canonical})
		}
		return ast.WalkContinue, nil
	})
	return mentions
}

// mentionRenderer writes @handle.
type mentionRenderer struct{}

// NewMentionRenderer returns the html.Extension that renders mentions.
func NewMentionRenderer() html.Extension { return &mentionRenderer{} }

func (r *mentionRenderer) RendererOptions(_ *html.Config) []html.Option {
	return []html.Option{
		html.WithNodeRenderers(map[ast.NodeKind]html.NodeRenderer{
			KindMention: html.NodeRendererFunc(r.renderMention),
		}),
	}
}

func (r *mentionRenderer) renderMention(
	writer io.Writer, source []byte, n ast.Node, entering bool, rc renderer.Context,
) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	w := writer.(util.BufWriter)
	m := n.(*mentionNode)

	if !m.Resolved {
		// Plain text, escaped. What was typed is what is shown.
		_ = w.WriteByte('@')
		_, _ = w.Write(util.EscapeHTML([]byte(m.Typed)))
		return ast.WalkSkipChildren, nil
	}

	// The link uses the canonical handle from the resolver rather than the text
	// in the source, so a post written before a rename links to where the person
	// is now. Cached HTML rendered before the rename still carries the old link
	// until it is evicted; whether a rename invalidates those renderings belongs
	// with TKT-01M29BSPEN, which owns the handle change policy.
	_, _ = w.WriteString(`<a href="/u/`)
	_, _ = w.Write(util.EscapeHTML([]byte(m.Handle)))
	_, _ = w.WriteString(`" class="mention">@`)
	_, _ = w.Write(util.EscapeHTML([]byte(m.Handle)))
	_, _ = w.WriteString(`</a>`)
	return ast.WalkSkipChildren, nil
}
