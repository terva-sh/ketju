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
