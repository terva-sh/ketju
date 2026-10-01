package threads

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/threads/lens"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// A lens is served in three steps (TKT-01M29CRJ1P, docs/07 "What the thread
// endpoint does"):
//
//  1. lens.Parse reads ?lens=. A value that is not a decided form is the
//     canonical page, with lens: null.
//  2. After access has admitted the viewer, and outside the thread's actor,
//     the kind's resolver reads what it needs from the database and decides
//     what the lens is for this viewer. A kind with no resolver is the
//     canonical page, so the framework landed before any lens did.
//  3. Inside threadlog.Actor.Stable the resolved lens builds its filter from
//     the same index snapshot the page is drawn from, lens.Apply selects,
//     and the page is cut from the selection.
//
// To add a lens, write its Filter constructor in package lens and add a row
// to lensResolvers below. Nothing else changes: the page, the cache key, the
// ETag and what each post shows all follow from the ordinals the lens
// selects. Add the kind's rows to TestLensPostsMatchCanonical in
// internal/web/api.

// lensResolver decides what a well-formed spec of its kind is for the
// viewer v. It runs after access and outside the actor, so it may read the
// database: a handle's user, the mentions projection. It refuses with a
// problem code where docs/07 says so, such as mod to a viewer who may not
// moderate the forum.
type lensResolver func(ctx context.Context, s *Service, v lensViewer, spec lens.Spec) (resolvedLens, error)

// lensViewer is what a resolver knows of the viewer: what access admitted,
// and what View has already read for the viewer block, so a resolver that
// needs it costs no statement.
type lensViewer struct {
	access
	// lastRead is the viewer's last_read_ordinal in this thread: zero for a
	// member with no position, and for anyone who is not a member.
	lastRead int64
}

// lensResolvers is every served lens kind. Each lens ticket adds its row:
// unread TKT-01M29CTY4J, author TKT-01M29CTY58, chain TKT-01M29CTY5T, and
// since, mentions and mod TKT-01M29CTY6E.
var lensResolvers = map[lens.Kind]lensResolver{
	lens.Unread: resolveUnread,
}

// resolveUnread is the unread lens (TKT-01M29CTY4J, docs/06 "the unread lens
// holds its boundary"). unread:B is the posts after B for whoever asks: a
// shared link is a range and depends on no viewer. A bare unread is
// unread:B with B the viewer's last_read_ordinal, the integer the landing
// page, the divider and "first unread" use, so the lens's first post is the
// one first unread lands on, and post 1 when there is no position. The
// answer names B, so the client can hold it in the address while reading
// moves the position. Anyone who is not a member has no position, and their
// bare unread is empty: 200 with nothing shown, never an error.
//
// The key is unread:B. Two members at different positions therefore never
// share a cached page; two at the same position see the same posts, which
// depend on B and the viewer class alone, and the class is keyed beside it.
func resolveUnread(_ context.Context, _ *Service, v lensViewer, spec lens.Spec) (resolvedLens, error) {
	after := spec.Ordinal
	if spec.Bare {
		if !v.viewer.Member() {
			return resolvedLens{kind: lens.Unread, key: "unread",
				filter: func(*threadlog.Index) (lens.Filter, error) { return lens.Ordinals(), nil }}, nil
		}
		after = uint32(min(max(v.lastRead, 0), math.MaxUint32))
	}
	value := strconv.FormatUint(uint64(after), 10)
	return resolvedLens{kind: lens.Unread, value: value, key: "unread:" + value,
		filter: func(*threadlog.Index) (lens.Filter, error) { return lens.After(after), nil }}, nil
}

// resolvedLens is a lens as it applies to one request.
type resolvedLens struct {
	kind lens.Kind
	// value is lens.value in the answer: kind alone when it is empty, and
	// kind:value otherwise, opens the same lens again (docs/07).
	value string
	// key is the lens in the response cache key, in a form that names
	// everything the selection depends on beyond the thread and its length:
	// author:<user id>, mentions:<viewer id>, unread:<boundary>. The page's
	// ordinal digest is keyed beside it, so a key that missed something could
	// still never serve one selection as another.
	key string
	// filter builds the lens's filter from the index the page is drawn from.
	// It runs inside Stable, may run more than once, and must only read. It
	// may read ix before it returns, as a reply chain's ancestor walk does,
	// but the filter it returns runs under the index's read lock and must not
	// touch ix.
	filter func(ix *threadlog.Index) (lens.Filter, error)
}

// resolveLens is the lens raw names for the viewer v, or nil when it names
// none this build serves: the canonical page.
func (s *Service) resolveLens(ctx context.Context, v lensViewer, raw string) (*resolvedLens, error) {
	spec := lens.Parse(raw)
	if spec.Kind == lens.Canonical {
		return nil, nil
	}
	if f, ok := s.TestLenses[spec.Kind]; ok {
		return &resolvedLens{kind: spec.Kind, value: spec.Value(), key: "test:" + spec.String(),
			filter: func(*threadlog.Index) (lens.Filter, error) { return f, nil }}, nil
	}
	resolve, ok := lensResolvers[spec.Kind]
	if !ok {
		return nil, nil
	}
	rl, err := resolve(ctx, s, v, spec)
	if err != nil {
		return nil, err
	}
	return &rl, nil
}

// lensPage cuts the page q asks for from the lens's selection of ix, and
// takes the snapshot of its posts. Call it inside Stable.
//
// A permalink serves the page holding its post, or else the page holding the
// next post the lens keeps; the landing rule does not apply. An ordinal past
// the thread's last post is not-found, as on the canonical page.
func lensPage(ix *threadlog.Index, logLength int64, rl *resolvedLens, q Query, size int) (snapshot, lens.Selection, error) {
	f, err := rl.filter(ix)
	if err != nil {
		return snapshot{}, lens.Selection{}, err
	}
	sel := lens.Apply(ix, f)
	page := 1
	switch {
	case q.Ordinal > 0:
		if int(q.Ordinal) > sel.Posts() {
			return snapshot{}, sel, problem.Coded(problem.CodeNotFound,
				fmt.Errorf("threads: ordinal %d past the last post %d", q.Ordinal, sel.Posts()))
		}
		page = sel.PageOf(q.Ordinal, size)
	case q.Page > 0:
		page = q.Page
	}
	ordinals, err := sel.Page(page, size)
	if err != nil {
		return snapshot{}, sel, err
	}
	var snap snapshot
	if err := snap.take(ix, logLength, ordinals); err != nil {
		return snapshot{}, sel, err
	}
	snap.page = page
	return snap, sel, nil
}

// lensBody is the answer's lens block.
func lensBody(rl *resolvedLens, sel lens.Selection, page, size int) *dto.Lens {
	return &dto.Lens{Kind: string(rl.kind), Value: rl.value, Shown: sel.Len(), Hidden: sel.Hidden(),
		Gaps: sel.Gaps(page, size)}
}
