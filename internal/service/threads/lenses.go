package threads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/threads/lens"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
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
	lens.Unread:   resolveUnread,
	lens.Author:   resolveAuthor,
	lens.Chain:    resolveChain,
	lens.Since:    resolveSince,
	lens.Mentions: resolveMentions,
	lens.Mod:      resolveMod,
}

// resolveSince is the since lens (TKT-01M29CTY6E): the posts first written
// at or after the moment, for every viewer. lens.Parse has already read the
// moment's three forms as Unix ms, which the answer names. It reads nothing
// from the database, and its key is since:<ms>.
func resolveSince(_ context.Context, _ *Service, _ lensViewer, spec lens.Spec) (resolvedLens, error) {
	ms := spec.SinceMS
	return resolvedLens{kind: lens.Since, value: spec.Value(), key: "since:" + spec.Value(),
		filter: func(*threadlog.Index) (lens.Filter, error) { return lens.CreatedSince(ms), nil }}, nil
}

// resolveMentions is the mentions lens (TKT-01M29CTY6E, docs/06 "the
// mentions lens"): the posts that mention the viewer, from the mentions
// projection, and the posts that reply to theirs, leaving out their own and
// every removed post. It costs one statement. The projection trails the log
// by the bus's delay, so a mention written a moment ago appears on the next
// request, the trade /replies makes with post_meta.
//
// Anyone who is not a member is mentioned by nobody here: an empty lens,
// 200, never an error. The key is mentions:<viewer id>, since the selection
// is the viewer's own; the response key's projected length and the page's
// ordinal digest move with the projection.
func resolveMentions(ctx context.Context, s *Service, v lensViewer, _ lens.Spec) (resolvedLens, error) {
	rl := resolvedLens{kind: lens.Mentions, key: "mentions", filter: keepNothing}
	if !v.viewer.Member() {
		return rl, nil
	}
	var mentioned []uint32
	err := s.DB.Read(ctx, func(dq *dbq.Queries, _ *sql.Tx) error {
		rows, err := dq.MentionedOrdinals(ctx, dbq.MentionedOrdinalsParams{ThreadID: v.row.ID, MentionedUserID: v.viewer.UserID})
		for _, o := range rows {
			if o > 0 && o <= math.MaxUint32 {
				mentioned = append(mentioned, uint32(o))
			}
		}
		return err
	})
	if err != nil {
		return rl, err
	}
	viewer := uint64(v.viewer.UserID)
	rl.key = "mentions:" + strconv.FormatInt(v.viewer.UserID, 10)
	rl.filter = func(ix *threadlog.Index) (lens.Filter, error) { return lens.MentionsOf(ix, viewer, mentioned), nil }
	return rl, nil
}

// resolveMod is the moderator lens (TKT-01M29CTY6E): exactly the removed
// posts, each drawn as the canonical page draws it for this viewer, body
// included where revealRemoved serves one. It is the one lens a viewer may
// be refused, and the refusal is here, in the service, not in a client:
// unauthenticated to anyone who is not a member (a suspended moderator is
// served as anonymous), and forbidden to a member without moderate in the
// thread's forum, by role or by group grant (docs/08 "Decided 2026-09-30").
// That is access.canModerate, the rule by which revealRemoved already gives
// the same viewer every removed body on the canonical page, so the lens
// shows a moderator nothing that page does not. Access has run first, so a
// thread hidden from the viewer is still not-found.
//
// Its pages are drawn for the moderator class alone, which the cache key
// carries beside the lens's key, mod.
func resolveMod(_ context.Context, _ *Service, v lensViewer, _ lens.Spec) (resolvedLens, error) {
	switch {
	case !v.viewer.Member():
		return resolvedLens{}, problem.Coded(problem.CodeUnauthenticated,
			fmt.Errorf("threads: the moderator lens needs a signed-in moderator"))
	case !v.canModerate:
		return resolvedLens{}, problem.Coded(problem.CodeForbidden,
			fmt.Errorf("threads: the moderator lens needs moderate in this forum"))
	}
	return resolvedLens{kind: lens.Mod, key: "mod",
		filter: func(*threadlog.Index) (lens.Filter, error) { return lens.Removed, nil }}, nil
}

// resolveChain is the reply chain lens (TKT-01M29CTY5T, docs/06 "Lenses"):
// chain:N is N's ancestors, N and its direct replies, in ordinal order, for
// every viewer. Each post in it is drawn as the canonical page draws it, so
// a removed ancestor keeps its place as the tombstone that viewer is shown.
// It reads nothing from the database: the chain is read from the index the
// page is drawn from, and chain: past the last post is not-found there. The
// key is chain:N, which with the thread's log length names the selection.
func resolveChain(_ context.Context, _ *Service, _ lensViewer, spec lens.Spec) (resolvedLens, error) {
	n := spec.Ordinal
	return resolvedLens{kind: lens.Chain, value: spec.Value(), key: "chain:" + spec.Value(),
		filter: func(ix *threadlog.Index) (lens.Filter, error) { return lens.ReplyChain(ix, n) }}, nil
}

// keepNothing is the filter of a lens that selects no post.
func keepNothing(*threadlog.Index) (lens.Filter, error) { return lens.Ordinals(), nil }

// resolveAuthor is the author lens (TKT-01M29CTY58, docs/07 "the lens
// parameter"): the posts, shown as themselves, of the member who holds the
// handle now, matched in any case. It costs one statement, the handle's
// lookup, and the selection is read from the index's author ids.
//
// A handle nobody holds answers exactly as a handle with no posts here: the
// same kind, the handle echoed back as asked, shown 0 and every post hidden.
// A handle a rename released is held by nobody, so it is not followed to the
// member who gave it up. The lens therefore tells a stranger nothing about
// who exists or who renamed.
//
// The key is author:<user id>, or author:- when the handle names nobody.
// Two spellings of one handle share the posts, which are all the cache
// holds; lens.value, which echoes the spelling, is built on every request.
func resolveAuthor(ctx context.Context, s *Service, _ lensViewer, spec lens.Spec) (resolvedLens, error) {
	rl := resolvedLens{kind: lens.Author, value: spec.Value(), key: "author:-", filter: keepNothing}
	id, ok, err := s.authorByHandle(ctx, spec.Handle)
	if err != nil || !ok {
		return rl, err
	}
	rl.key = "author:" + strconv.FormatInt(id, 10)
	rl.filter = func(*threadlog.Index) (lens.Filter, error) { return lens.ByAuthor(uint64(id)), nil }
	return rl, nil
}

// authorByHandle is the user id an author lens's handle names: the account
// that holds it now, in any case (users.handle is COLLATE NOCASE).
//
// The one exception is deleted-user-<id>, the placeholder member.Missing
// draws for an author id the database lacks (docs/04 "Projection tables").
// When no account holds it and no users row has that id, it names that id,
// so "Only <handle>" works on a thread whose starter the database lost. When
// a row has that id, it names nobody, so the placeholder never reaches a
// live account's posts. That costs one more statement on this path only.
func (s *Service) authorByHandle(ctx context.Context, handle string) (int64, bool, error) {
	var (
		id int64
		ok bool
	)
	err := s.DB.Read(ctx, func(dq *dbq.Queries, _ *sql.Tx) error {
		u, err := dq.GetUserByHandle(ctx, handle)
		switch {
		case err == nil:
			id, ok = u.ID, true
			return nil
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		missing, placeholder := placeholderID(handle)
		if !placeholder {
			return nil
		}
		switch _, err := dq.GetUser(ctx, missing); {
		case errors.Is(err, sql.ErrNoRows):
			id, ok = missing, true
			return nil
		default:
			return err
		}
	})
	return id, ok, err
}

// placeholderID is the id in deleted-user-<id>, the handle
// domain.DeletedMemberHandle draws for it, written exactly as it draws it.
func placeholderID(handle string) (int64, bool) {
	rest, ok := strings.CutPrefix(handle, domain.AnonymizedHandlePrefix+"-")
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id <= 0 || domain.DeletedMemberHandle(id) != handle {
		return 0, false
	}
	return id, true
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
			return resolvedLens{kind: lens.Unread, key: "unread", filter: keepNothing}, nil
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
		if threadlog.PastLast(q.Ordinal, sel.Posts()) {
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
