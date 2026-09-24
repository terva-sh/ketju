package users

import (
	"context"
	"database/sql"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// The suggestion list's size: DefaultSuggestions when the client names none,
// and never more than MaxSuggestions, which is more than a listbox under a
// caret should show and bounds what one keystroke can ask for.
const (
	DefaultSuggestions = 8
	MaxSuggestions     = 20
)

// MaySuggest reports whether v may ask for suggestions: a member, as for
// POST /render, which it serves (member.Require). Handles are not secret,
// since every post shows its author's, but a list of accounts by prefix is a
// directory, and a directory is for members. The handler asks this before
// reading the query, so a stranger is refused as one whatever they sent.
func (s *Service) MaySuggest(v domain.Viewer) error {
	return member.Require(v, "list members")
}

// Suggest is GET /users?prefix=&limit= (docs/07): up to limit active members
// whose handle starts with prefix, in handle order, for the composer's
// mention autocomplete (TKT-01M38XJJKM).
//
// A member is required (MaySuggest).
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
	if err := s.MaySuggest(v); err != nil {
		return out, err
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
