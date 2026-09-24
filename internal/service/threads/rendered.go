package threads

import (
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/render"
)

// RenderDTO is the one conversion from a render to what a client is told
// about it. The composer preview (TKT-01M29CA9D4) and the create path
// (TKT-01M29CA9B9) both answer through it, so for the same source and
// thread they cannot disagree about what a post mentions, quotes or answers
// (TKT-01M29C2WWG). Nothing here parses anything: the facts are the ones the
// render found.
func RenderDTO(r render.Result) dto.RenderResult {
	out := dto.RenderResult{
		HTML: r.HTML,
		// Empty, not null: dto's rule is that a key is always present, and
		// a list with nothing in it is an empty list.
		Mentions: make([]dto.MentionRef, 0, len(r.Mentions)),
		Quotes:   make([]dto.QuoteRef, 0, len(r.Quotes)),
	}
	for _, m := range r.Mentions {
		out.Mentions = append(out.Mentions, dto.MentionRef{Handle: m.Handle})
	}
	for _, q := range r.Quotes {
		out.Quotes = append(out.Quotes, dto.QuoteRef{Ordinal: q.Ordinal, Deleted: q.Deleted})
	}
	if r.ReplyTo != nil {
		v := *r.ReplyTo
		out.ReplyTo = &v
	}
	return out
}
