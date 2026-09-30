package threads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Mentions and handle changes (docs/05 "Changing a handle", TKT-01M3NRP3W9).
//
// A post's source keeps the handle its author typed, and a handle can move:
// its holder renames, the old handle is reserved for 90 days, and then
// someone else may take it. docs/05 says a mention names the member by user
// id, so history does not change. handle_releases keeps, forever, every
// handle that left an account, when, and which account it left (none for an
// anonymized one), so whom a handle named at any moment can be read back.
//
// At write time (the composer's preview, a new post, an edit) a typed handle
// names its holder, or failing that the account a release still in force
// redirects to, so a new @alice written during the 90 days mentions the
// member who renamed and records their id (resolveNow).
//
// At read time a typed handle names whom it named when the post's current
// revision was written, at T, or whom it first named after, and keeps
// naming them through later changes (resolveAt). The first of these that
// applies decides, and nothing after it is asked:
//  1. A release was in force at T, so nobody held the handle and it
//     redirected: its user, as the write-time rule named them.
//  2. The handle has been released since T. The earliest such release was
//     made by whoever held it at T, or, when nobody did, by whoever first
//     took it after, whom the post linked from then on (docs/07: "a user
//     registering a mentioned handle is seen at once"): its user.
//  3. Nobody has released it since T: its holder now, likewise.
//  4. Nobody holds it: the text typed.
//
// Each named user is shown by their handle now. An anonymized account's
// releases name no user (perm.Anonymize), so its old mentions stop at 1 or
// 2 as text and are never a link to whoever takes the handle next. A
// release names only the member who left the handle, so a later holder is
// reached only through step 3, and only while nobody has left it since T.
//
// The groomed plan also read the mentions projection, the ids each post
// recorded, and asked each answer to be among them. Every use of it lost to
// a review of PR #180. As a set of users rather than of typed handles it
// let one mentioned member take another's mention: "the holder now, when
// recorded" let bob take @alice by renaming into alice once it was free
// (review 1515, twice). Requiring it in step 2 dropped a handle registered
// after the post, whose holder no post recorded (review 1517). Requiring it
// in step 1 drew a post written during a reservation as text until the
// projection, which trails the log, caught up (review 1521). The release
// rows alone answer each case, and the write-time rule reads them the same
// way, so the projection adds nothing to who a mention names; it still
// records the ids for notifications and the mentions lens.

// mentionScope is what a post's mentions are resolved against: when its
// current revision was written.
type mentionScope struct {
	since int64
}

// key is the scope as text, recorded beside each lookup in render.Deps so a
// cached rendering is checked against the same question (parseScope).
func (sc mentionScope) key() string { return strconv.FormatInt(sc.since, 10) }

// parseScope reads what key wrote.
func parseScope(s string) (mentionScope, bool) {
	since, err := strconv.ParseInt(s, 10, 64)
	return mentionScope{since: since}, err == nil
}

// handleFacts is what one typed handle is resolved from: its holder now, if
// any, and every release of it, oldest first.
type handleFacts struct {
	holder   dbq.User
	held     bool
	releases []dbq.HandleReleasesRow
}

// handleMemo resolves mentions for one request: each handle is read once
// however many posts mention it, with the first failure kept, because the
// renderer's resolver cannot return one.
type handleMemo struct {
	s    *Service
	ctx  context.Context
	seen map[string]handleFacts
	err  error
}

func (s *Service) handleMemo(ctx context.Context) *handleMemo {
	return &handleMemo{s: s, ctx: ctx, seen: map[string]handleFacts{}}
}

// facts reads h's holder and releases, once. A failure is kept in m.err and
// not remembered: a failure is not an answer.
func (m *handleMemo) facts(h string) (handleFacts, bool) {
	if f, ok := m.seen[h]; ok {
		return f, true
	}
	var f handleFacts
	err := m.s.DB.Read(m.ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUserByHandle(m.ctx, h)
		switch {
		case err == nil:
			f.holder, f.held = u, true
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		f.releases, err = q.HandleReleases(m.ctx, h)
		return err
	})
	if err != nil {
		if m.err == nil {
			m.err = fmt.Errorf("threads: resolving @%s: %w", h, err)
		}
		return handleFacts{}, false
	}
	m.seen[h] = f
	return f, true
}

// named is the lookup for a user a handle names, shown by their handle now.
func named(typed, scope string, id int64, handle string) render.HandleLookup {
	return render.HandleLookup{Typed: typed, Scope: scope, ID: id, Canonical: handle, Found: true}
}

// released is the lookup a deciding release gives: the account it left, or
// nobody for an anonymized account's.
func released(typed, scope string, r dbq.HandleReleasesRow) render.HandleLookup {
	if r.UserID.Valid && r.UserHandle.Valid {
		return named(typed, scope, r.UserID.Int64, r.UserHandle.String)
	}
	return render.HandleLookup{Typed: typed, Scope: scope}
}

// resolveAt is the read-time rule above, for typed in a post of scope sc.
func (m *handleMemo) resolveAt(typed string, sc mentionScope) render.HandleLookup {
	key := sc.key()
	f, ok := m.facts(typed)
	if !ok {
		return render.HandleLookup{Typed: typed, Scope: key}
	}
	// 1: the newest release in force at T.
	for i := len(f.releases) - 1; i >= 0; i-- {
		if r := f.releases[i]; r.ReleasedAt <= sc.since && sc.since < r.ReservedUntil {
			return released(typed, key, r)
		}
	}
	// 2: the earliest release after T.
	for _, r := range f.releases {
		if r.ReleasedAt > sc.since {
			return released(typed, key, r)
		}
	}
	// 3 and 4.
	if f.held {
		return named(typed, key, f.holder.ID, f.holder.Handle)
	}
	return render.HandleLookup{Typed: typed, Scope: key}
}

// resolveNow is the write-time rule: the holder, else the account the
// newest release in force at now redirects to. A release naming no user
// redirects nowhere.
func (m *handleMemo) resolveNow(typed string, now int64) render.HandleLookup {
	f, ok := m.facts(typed)
	if !ok {
		return render.HandleLookup{Typed: typed}
	}
	if f.held {
		return named(typed, "", f.holder.ID, f.holder.Handle)
	}
	for i := len(f.releases) - 1; i >= 0; i-- {
		if r := f.releases[i]; r.ReservedUntil > now {
			return released(typed, "", r)
		}
	}
	return render.HandleLookup{Typed: typed}
}

// current reports whether every recorded lookup still has the same answer,
// asked with the scope it was recorded with.
func (m *handleMemo) current(lookups []render.HandleLookup) bool {
	for _, l := range lookups {
		sc, ok := parseScope(l.Scope)
		if !ok || m.resolveAt(l.Typed, sc) != l {
			return false
		}
	}
	return true
}
