package render

import (
	"html"
	"strings"

	"github.com/yuin/goldmark/v2/ast"
	east "github.com/yuin/goldmark/v2/extension/ast"
)

// PlainVersion identifies Plain's output, as Version identifies Render's.
// The search index records it, through events/search's Version, and an
// index built by another one is emptied and rebuilt (docs/04 "search
// (FTS)"), so the rule is the same:
//
//	Any change that can change the output of Plain must bump PlainVersion.
//
// That includes a change to the parser Plain shares with Render: an
// extension added or removed, or a goldmark upgrade. TestPlainVersionTracksOutput
// keeps a per-case hash of the output in testdata/plain.sum and refuses an
// update whose output changed unless PlainVersion was bumped first.
const PlainVersion = 1

// Snippet sentinels. The search query asks FTS5 to mark each match with
// these two private-use characters, which the API turns into markup after
// escaping the text around them (TKT-01M29CTY8T). Plain removes them from
// every source, so no post can forge a match marker.
const (
	SnippetOpen  = ''
	SnippetClose = ''
)

// Plain turns post source into the plain text the search index holds
// (TKT-01M29CRJ3B). It parses with the same parser as Render, so a word is
// text in the index exactly when it is text on the page, and it walks the
// tree:
//
//   - text, code spans and code blocks are kept;
//   - a link keeps its text and loses its destination, an image keeps its alt
//     text, and an autolink, whose text is its destination, is dropped;
//   - a mention is "@" and the handle as typed;
//   - a plain blockquote keeps its text, and an attributed quotation
//     ("[quote=#N]") is dropped whole: it is a copy of another post in the
//     thread, which has its own row while it is visible, and the page drops
//     the copy once that post is deleted, so indexing it would let search
//     find text the page no longer shows;
//   - raw HTML and markdown punctuation are dropped;
//   - each block ends with a newline, and a table cell with a space.
//
// Plain never fails. A source longer than MaxSource cannot be in a log, and
// is cut there rather than refused, since the indexer has no one to refuse
// it to.
func Plain(source string) string {
	if len(source) > MaxSource {
		source = source[:MaxSource]
	}
	src := []byte(source)
	doc := mdParser.Parse(src)
	var b strings.Builder
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			switch {
			case n.Kind() == east.KindTableCell:
				b.WriteByte(' ')
			default:
				if _, block := n.(ast.BlockNode); block && n.Kind() != ast.KindDocument {
					endLine(&b)
				}
			}
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Text:
			v := n.Value.Value(src)
			if n.Value.IsOwned() {
				v = html.UnescapeString(v)
			}
			b.WriteString(v)
			switch {
			case n.HardLineBreak():
				b.WriteByte('\n')
			case n.SoftLineBreak():
				b.WriteByte(' ')
			}
		case *ast.CodeSpan:
			b.WriteString(n.Value.Value(src))
			return ast.WalkSkipChildren, nil
		case *ast.CodeBlock:
			b.WriteString(n.Value.Str(src))
			return ast.WalkSkipChildren, nil
		case *ast.AutoLink, *ast.RawHTML, *ast.HTMLBlock, *quoteNode:
			return ast.WalkSkipChildren, nil
		case *mentionNode:
			b.WriteByte('@')
			b.WriteString(n.Typed)
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return DropSentinels(strings.TrimSpace(b.String()))
}

// DropSentinels removes SnippetOpen and SnippetClose from s. Plain calls it
// on every post, and the indexer on every title, which is not markdown and
// does not go through Plain.
func DropSentinels(s string) string {
	return strings.Map(func(r rune) rune {
		if r == SnippetOpen || r == SnippetClose {
			return -1
		}
		return r
	}, s)
}

// endLine ends the text so far with one newline, if it has any text.
func endLine(b *strings.Builder) {
	s := b.String()
	if s == "" || s[len(s)-1] == '\n' {
		return
	}
	if s[len(s)-1] == ' ' {
		// A table row's last cell left a space; a newline replaces it.
		trimmed := strings.TrimRight(s, " ")
		b.Reset()
		b.WriteString(trimmed)
	}
	b.WriteByte('\n')
}
