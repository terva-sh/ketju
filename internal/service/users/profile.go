package users

import (
	"context"
	"database/sql"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/views"
)

// Profile is GET /users/{handle} (docs/07): the account's author block, its
// bio, when it joined, and its post count as v may see them.
//
// Who is served which account is member.Account's rule. The post count is
// the account's posts that are neither deleted nor moved, in the forums v
// may read, so it varies by viewer and says nothing about a forum v cannot
// see; it is what a member's view of the history lists, and a deleted post
// never counts, even for a viewer whose history shows it. The status is
// shown to a moderator or admin and to nobody else.
//
// An anonymized account shows its placeholder handle and no prior identity:
// no display name, avatar or bio (member.Author). Its joined date and post
// count stay, since neither names anyone.
func (s *Service) Profile(ctx context.Context, v domain.Viewer, handle string) (dto.Profile, error) {
	u, err := member.Account(ctx, s.DB, v, handle)
	if err != nil {
		return dto.Profile{}, err
	}
	forums, err := s.Perm.ReadableForums(ctx, v)
	if err != nil {
		return dto.Profile{}, err
	}
	var n int64
	if err := s.DB.Read(ctx, func(_ *dbq.Queries, tx *sql.Tx) error {
		var err error
		n, err = views.UserPostCount(ctx, tx, u.ID, forums)
		return err
	}); err != nil {
		return dto.Profile{}, err
	}

	out := dto.Profile{User: member.Author(u), JoinedAt: dto.Millis(u.CreatedAt), PostCount: int(n)}
	if u.BioSource != "" && !domain.Anonymized(u.Handle) {
		// A bio is not in a thread, so a quote in one has no post to point
		// at: every quoted post is reported missing, and the renderer draws
		// the quote as a plain blockquote rather than a link to an ordinal
		// on whatever page the reader is on.
		r, err := render.RenderWith(u.BioSource, render.Context{Posts: noPosts})
		if err != nil {
			return dto.Profile{}, err
		}
		out.BioHTML = r.HTML
	}
	if v.Staff() {
		status := u.Status
		out.Status = &status
	}
	return out, nil
}

var noPosts = render.ResolverFunc(func(uint32) (bool, bool) { return false, false })
