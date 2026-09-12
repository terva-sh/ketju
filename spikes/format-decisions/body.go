// Package format holds throwaway benchmarks for the three Phase 0 thread log
// format decisions in TKT-01M29BSPD. Nothing here is production code and
// nothing here should be imported by it. See README.md in this directory.
package format

import (
	"fmt"
	"math/rand"
	"strings"
)

// Attachment mirrors the attachments[] entries described in docs/03.
type Attachment struct {
	Hash string `json:"hash" cbor:"hash"`
	Name string `json:"name" cbor:"name"`
	Size int64  `json:"size" cbor:"size"`
	MIME string `json:"mime" cbor:"mime"`
}

// PostBody is the type 2 record body from the docs/03 record table. String
// keys on both encodings, because docs/03 calls for a self-describing map so
// fields can be added without a format bump. Integer keys would make CBOR far
// smaller but would give up that property, so comparing them would be
// comparing two different designs rather than two encodings.
type PostBody struct {
	Source      string       `json:"source" cbor:"source"`
	ReplyTo     uint32       `json:"reply_to,omitempty" cbor:"reply_to,omitempty"`
	Mentions    []uint64     `json:"mentions,omitempty" cbor:"mentions,omitempty"`
	Quotes      []uint32     `json:"quotes,omitempty" cbor:"quotes,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty" cbor:"attachments,omitempty"`
}

var vocab = strings.Fields(`the a of to and in that it is was for on with as by at from but not
they we you this have has been would could should there their which when what about into over after
slack forum thread server backup config index offset ordinal revision moderator member community
because however although really actually probably certainly obviously frankly honestly
migration rollback deployment latency throughput cache invalidation permission projection`)

// markdown builds a post body of roughly target bytes with the shapes real
// posts have: paragraphs, an occasional fenced code block, a quote, a list.
func markdown(r *rand.Rand, target int) string {
	var b strings.Builder
	for b.Len() < target {
		switch n := r.Intn(10); {
		case n < 6: // paragraph
			words := 30 + r.Intn(90)
			for i := 0; i < words; i++ {
				if i > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(vocab[r.Intn(len(vocab))])
			}
			b.WriteString(".\n\n")
		case n < 7: // fenced code
			b.WriteString("```go\n")
			for i := 0; i < 3+r.Intn(8); i++ {
				fmt.Fprintf(&b, "\tif err := store.Append(ctx, %d); err != nil {\n\t\treturn err\n\t}\n", r.Intn(1000))
			}
			b.WriteString("```\n\n")
		case n < 8: // quote
			fmt.Fprintf(&b, "[quote=#%d]\n> ", 1+r.Intn(200))
			for i := 0; i < 15+r.Intn(25); i++ {
				b.WriteString(vocab[r.Intn(len(vocab))])
				b.WriteByte(' ')
			}
			b.WriteString("\n\n")
		default: // list
			for i := 0; i < 3+r.Intn(5); i++ {
				b.WriteString("- ")
				for j := 0; j < 5+r.Intn(12); j++ {
					b.WriteString(vocab[r.Intn(len(vocab))])
					b.WriteByte(' ')
				}
				b.WriteByte('\n')
			}
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// GenBody produces a post body whose source is roughly target bytes.
func GenBody(r *rand.Rand, target int) PostBody {
	p := PostBody{Source: markdown(r, target)}
	if r.Intn(2) == 0 {
		p.ReplyTo = uint32(1 + r.Intn(200))
	}
	for i := 0; i < r.Intn(3); i++ {
		p.Mentions = append(p.Mentions, uint64(r.Int63()))
	}
	for i := 0; i < r.Intn(2); i++ {
		p.Quotes = append(p.Quotes, uint32(1+r.Intn(200)))
	}
	if r.Intn(5) == 0 {
		p.Attachments = append(p.Attachments, Attachment{
			Hash: fmt.Sprintf("%064x", r.Int63()),
			Name: "screenshot-2026-09-11.png",
			Size: int64(r.Intn(4 << 20)),
			MIME: "image/png",
		})
	}
	return p
}
