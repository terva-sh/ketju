package users

import (
	"context"
	"database/sql"
	"errors"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// The suggestion list's size: DefaultSuggestions when the client names none,
// and never more than MaxSuggestions, which is more than a listbox under a
// caret should show and bounds what one keystroke can ask for.
const (
	DefaultSuggestions = 8
	MaxSuggestions     = 20
)

// Suggest is GET /users?prefix=&limit= (docs/07): up to limit active members
// whose handle starts with prefix, in handle order, for the composer's
// mention autocomplete (TKT-01M38XJJKM).
//
// A member is required, as for POST /render, which it serves: a pre-user
// session is forbidden, and anyone else who is not a member, an inactive
// account included, is unauthenticated. Handles are not secret, since every
// post shows its author's, but a list of accounts by prefix is a directory,
// and a directory is for members.
//
// Matching is case-insensitive, as handles are (docs/04): the query
// compares under the handle column's NOCASE collation, which folds ASCII and
// nothing else, so the prefix is passed as typed. strings.ToLower here would
// be wrong as well as redundant, since it folds the Kelvin sign to k.
//
// A prefix no handle can start with is an empty list rather than an error,
// because the composer asks while the author is still typing: an @ followed
// by a character a handle cannot hold is not a mistake to report, just no
// one to suggest. The query needs no help to get that right. Every handle
// character sorts below '{', so the range from prefix to prefix+"{" holds
// exactly the handles that start with prefix, whatever prefix holds.
//
// Every suggestion is a handle the renderer resolves as a mention of that
// user, so inserting it mentions them. The query leaves out a handle ending
// in '-' for that reason; see UsersByHandlePrefix.
func (s *Service) Suggest(ctx context.Context, v domain.Viewer, prefix string, limit int) (dto.UserSuggestions, error) {
	out := dto.UserSuggestions{Users: []dto.Author{}}
	switch {
	case v.PreUser && !v.Member():
		return out, problem.Coded(problem.CodeForbidden, errors.New("users: a pre-user session cannot list members"))
	case !v.Member():
		return out, problem.Coded(problem.CodeUnauthenticated, errors.New("users: listing members needs a member session"))
	}
	if limit <= 0 {
		limit = DefaultSuggestions
	}
	limit = min(limit, MaxSuggestions)

	var rows []dbq.UsersByHandlePrefixRow
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		rows, err = q.UsersByHandlePrefix(ctx, dbq.UsersByHandlePrefixParams{Lo: prefix, Hi: prefix + "{", Lim: int64(limit)})
		return err
	})
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		out.Users = append(out.Users, dto.Author{Handle: r.Handle, DisplayName: r.DisplayName,
			Avatar: r.AvatarBlob.String, Role: r.Role, Kind: r.Kind})
	}
	return out, nil
}
