package render

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// thread12 is a thread of posts 1 to 12, post 5 deleted.
var thread12 = ResolverFunc(func(o uint32) (bool, bool) { return o >= 1 && o <= 12, o == 5 })

func ptr(v uint32) *uint32 { return &v }

// AC1: one call answers everything the post carries.
func TestOneCallReturnsEveryFact(t *testing.T) {
	src := "> [quote=#3]\n> the quoted words\n\nThanks @alice, and @nobody."
	r, err := RenderWith(src, Context{
		Posts:   thread12,
		Handles: HandleFunc(func(h string) (int64, string, bool) { return 42, "alice", strings.EqualFold(h, "alice") }),
		ReplyTo: ptr(7),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.HTML, "the quoted words") || !strings.Contains(r.HTML, "Thanks") {
		t.Errorf("html %s", r.HTML)
	}
	if !slices.Equal(r.Mentions, []Mention{{UserID: 42, Handle: "alice"}}) {
		t.Errorf("mentions %v", r.Mentions)
	}
	if !slices.Equal(r.Quotes, []Quote{{Ordinal: 3}}) {
		t.Errorf("quotes %v", r.Quotes)
	}
	if r.ReplyTo == nil || *r.ReplyTo != 7 {
		t.Errorf("reply_to %v", r.ReplyTo)
	}
}

// AC3: a reply to a post that is not in the thread is refused, not stored.
func TestReplyToMustNameAPostInTheThread(t *testing.T) {
	cases := map[string]Context{
		"an ordinal past the end": {Posts: thread12, ReplyTo: ptr(13)},
		"ordinal zero":            {Posts: thread12, ReplyTo: ptr(0)},
		"another thread's post": {ReplyTo: ptr(3),
			Posts: ResolverFunc(func(uint32) (bool, bool) { return false, false })},
		"nothing to check against": {ReplyTo: ptr(3)},
	}
	for name, ctx := range cases {
		if _, err := RenderWith("hello", ctx); !errors.Is(err, ErrInvalidReplyTo) {
			t.Errorf("%s: err = %v, want ErrInvalidReplyTo", name, err)
		}
	}
}

func TestReplyToAnExistingPost(t *testing.T) {
	for _, o := range []uint32{1, 5, 12} {
		r, err := RenderWith("hello", Context{Posts: thread12, ReplyTo: ptr(o)})
		if err != nil || r.ReplyTo == nil || *r.ReplyTo != o {
			t.Errorf("reply to %d: %v, %v", o, r.ReplyTo, err)
		}
	}
	// The result is a copy: the caller's value can change after the render
	// without changing what was validated.
	in := ptr(4)
	r, _ := RenderWith("hello", Context{Posts: thread12, ReplyTo: in})
	*in = 99
	if *r.ReplyTo != 4 {
		t.Errorf("the result follows the caller's pointer: %d", *r.ReplyTo)
	}
	// No reply_to, none returned, and no resolver needed.
	if r, err := RenderWith("hello", Context{}); err != nil || r.ReplyTo != nil {
		t.Errorf("no reply: %v, %v", r.ReplyTo, err)
	}
}

// reply_to never reaches the markup, which is why validating it here left
// Version where it was.
func TestReplyToDoesNotChangeTheHTML(t *testing.T) {
	with, err := RenderWith("> [quote=#3]\n> q\n\nhello", Context{Posts: thread12, ReplyTo: ptr(3)})
	if err != nil {
		t.Fatal(err)
	}
	without, _ := RenderWith("> [quote=#3]\n> q\n\nhello", Context{Posts: thread12})
	if with.HTML != without.HTML {
		t.Errorf("reply_to changed the HTML:\n%s\n%s", with.HTML, without.HTML)
	}
}
