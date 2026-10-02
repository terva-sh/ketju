package lens

import (
	"fmt"
	"slices"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Filter is a lens's rule: keep or skip one post, given its ordinal and its
// index entry. Its one method is unexported, so only this package implements
// it, and every lens is a constructor here. A filter cannot reorder, because
// it is never handed more than one post and never returns one.
//
// keep runs under the index's read lock (threadlog.Index.Select) and must not
// call back into the Index. A lens that needs more of the index than the
// entry in hand, such as a reply chain's ancestor line, reads it before it
// builds its filter.
type Filter interface {
	keep(ordinal uint32, e threadlog.IndexEntry) bool
}

// Ordinals keeps exactly the listed ordinals, in whatever order they are
// given: the selection is ascending all the same. It is for a lens whose
// posts are decided before the walk, such as a reply chain or the posts
// that mention a viewer.
func Ordinals(ordinals ...uint32) Filter {
	set := make(map[uint32]bool, len(ordinals))
	for _, o := range ordinals {
		set[o] = true
	}
	return ordinalSet(set)
}

type ordinalSet map[uint32]bool

func (s ordinalSet) keep(o uint32, _ threadlog.IndexEntry) bool { return s[o] }

// Apply walks ix once, in ascending order, and keeps what f keeps. Call it
// inside threadlog.Actor.Stable, so the selection and the post count describe
// the same log.
func Apply(ix *threadlog.Index, f Filter) Selection {
	return Selection{ordinals: ix.Select(f.keep), posts: ix.Len()}
}

// Selection is the posts a lens kept from a thread of posts posts, strictly
// ascending. Its fields are unexported and only Apply builds one, so no code
// outside this package can hold a selection in any other order.
type Selection struct {
	ordinals []uint32
	posts    int
}

// Len is the posts the lens kept: lens.shown.
func (s Selection) Len() int { return len(s.ordinals) }

// Hidden is the thread's other posts: lens.hidden.
func (s Selection) Hidden() int { return s.posts - len(s.ordinals) }

// Posts is the thread's post count the selection was made from.
func (s Selection) Posts() int { return s.posts }

// TotalPages is the pages the selection fills at size posts a page, at least
// one: an empty selection has one empty page, as an empty thread does.
func (s Selection) TotalPages(size int) int {
	return max(1, (len(s.ordinals)+size-1)/size)
}

// Page returns page n's ordinals, a copy, at size posts a page. A page past
// the last is not-found, as the canonical page's is.
func (s Selection) Page(n, size int) ([]uint32, error) {
	if n < 1 || n > s.TotalPages(size) {
		return nil, problem.Coded(problem.CodeNotFound,
			fmt.Errorf("lens: page %d past the last page %d", n, s.TotalPages(size)))
	}
	from := (n - 1) * size
	to := min(from+size, len(s.ordinals))
	return slices.Clone(s.ordinals[from:to]), nil
}

// PageOf is the page holding ordinal, or else the page holding the first
// kept ordinal after it, or else the last page: a permalink under a lens
// lands where the post would be (docs/07).
func (s Selection) PageOf(ordinal uint32, size int) int {
	i, _ := slices.BinarySearch(s.ordinals, ordinal)
	if i >= len(s.ordinals) {
		return s.TotalPages(size)
	}
	return i/size + 1
}

// Gaps are the runs of posts the lens skipped that touch page n: the run
// before its first post, those between its posts, and the run after its last,
// the thread's tail included. After is 0 for a run at the start of the
// thread. An empty selection's one page has one gap, the whole thread, when
// the thread has posts. The result is never nil.
func (s Selection) Gaps(n, size int) []dto.LensGap {
	gaps := []dto.LensGap{}
	add := func(after uint32, next int) {
		if count := next - int(after) - 1; count > 0 {
			gaps = append(gaps, dto.LensGap{After: int(after), Count: count})
		}
	}
	if len(s.ordinals) == 0 {
		add(0, s.posts+1)
		return gaps
	}
	from := (n - 1) * size
	to := min(from+size, len(s.ordinals))
	if from < 0 || from >= to {
		return gaps
	}
	prev := uint32(0)
	if from > 0 {
		prev = s.ordinals[from-1]
	}
	for _, o := range s.ordinals[from:to] {
		add(prev, int(o))
		prev = o
	}
	next := s.posts + 1
	if to < len(s.ordinals) {
		next = int(s.ordinals[to])
	}
	add(prev, next)
	return gaps
}

// After keeps the posts after ordinal after: the unread lens's unread:B
// (docs/06 "the unread lens holds its boundary"). A boundary at or past the
// last post keeps nothing.
func After(after uint32) Filter { return unreadAfter(after) }

type unreadAfter uint32

func (b unreadAfter) keep(o uint32, _ threadlog.IndexEntry) bool { return o > uint32(b) }

// ByAuthor keeps the posts user id author wrote that are shown as
// themselves: the author lens (docs/06 "what each lens keeps of removed
// posts"). A deleted, hidden, moved or redacted post is left out, so
// "showing 7 of 212" counts posts a reader can read. It reads the index
// entry's author id alone.
func ByAuthor(author uint64) Filter { return byAuthor(author) }

type byAuthor uint64

func (a byAuthor) keep(_ uint32, e threadlog.IndexEntry) bool {
	return e.Author == uint64(a) && e.Flags.Visible()
}

// ReplyChain is the reply chain lens's filter for post n (docs/06
// "Lenses"): n's ancestors, found by following reply_to upward, n itself,
// and n's direct replies. It reads ix, so call it inside Stable, before
// Apply; the filter it returns reads nothing more. A post past the last is
// not-found, as a permalink to it is.
//
// A well-formed log only ever points a reply backwards, so the walk stops at
// the first link that does not: a reply_to of 0, one at or past the post
// that names it, or one past the last post. That is what ends a cycle, a
// self-reference or a forward reference in a corrupt index. The walk is also
// capped at the thread's length, a second bound that does not depend on the
// first being right. A removed ancestor keeps its place, and its reply_to
// is followed as any other's, since the index keeps it.
//
// Ordinals are compared with the post count through threadlog.PastLast,
// never converted to int: on a 32-bit build int(4294967295) is negative,
// and would pass a bound it should fail (review 1771).
func ReplyChain(ix *threadlog.Index, n uint32) (Filter, error) {
	posts := ix.Len()
	if n == 0 || threadlog.PastLast(n, posts) {
		return nil, problem.Coded(problem.CodeNotFound,
			fmt.Errorf("lens: chain:%d past the last post %d", n, posts))
	}
	keep := []uint32{n}
	for cur, steps := n, 0; steps < posts; steps++ {
		e, err := ix.Entry(cur)
		if err != nil {
			return nil, err
		}
		up := e.ReplyTo
		if up == 0 || up >= cur || threadlog.PastLast(up, posts) {
			break
		}
		keep = append(keep, up)
		cur = up
	}
	keep = append(keep, ix.Replies(n)...)
	return Ordinals(keep...), nil
}

// CreatedSince keeps the posts first written at or after ms, Unix ms: the
// since lens (docs/06 "Lenses"). It reads the index entry's created time,
// revision 1's, so an edit never brings an old post into the lens. A
// removed post keeps its place, as the canonical page draws it.
func CreatedSince(ms int64) Filter { return createdSince(ms) }

type createdSince int64

func (s createdSince) keep(_ uint32, e threadlog.IndexEntry) bool { return e.CreatedMS >= int64(s) }

// Removed keeps exactly the posts not shown as themselves: deleted, hidden,
// hidden pending review, moved and redacted (threadlog.EntryFlags.Visible).
// It is the moderator lens (docs/06 "what each lens keeps of removed
// posts"). What each post shows, its body included, is the canonical page's
// rule for the viewer.
var Removed Filter = removed{}

type removed struct{}

func (removed) keep(_ uint32, e threadlog.IndexEntry) bool { return !e.Flags.Visible() }

// MentionsOf is the mentions lens's filter for viewer, user id (docs/06
// "the mentions lens"): the posts whose current revision mentions them,
// which mentioned lists from the mentions projection, and the posts that
// reply to a post they wrote. It leaves out the viewer's own posts, and
// every post not shown as itself. It reads ix for the viewer's posts, so
// call it inside Stable, before Apply.
func MentionsOf(ix *threadlog.Index, viewer uint64, mentioned []uint32) Filter {
	f := mentionsOf{viewer: viewer, mentioned: map[uint32]bool{}, own: map[uint32]bool{}}
	for _, o := range mentioned {
		f.mentioned[o] = true
	}
	for _, o := range ix.ByAuthor(viewer) {
		f.own[o] = true
	}
	return f
}

type mentionsOf struct {
	viewer    uint64
	mentioned map[uint32]bool
	own       map[uint32]bool
}

func (m mentionsOf) keep(o uint32, e threadlog.IndexEntry) bool {
	return (m.mentioned[o] || m.own[e.ReplyTo]) && e.Author != m.viewer && e.Flags.Visible()
}
