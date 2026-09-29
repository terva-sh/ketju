package member

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Account is the account a profile or post history is about, as v may see
// it (docs/07 GET /users/{handle}, decided 2026-09-28 in TKT-01M3KJJNDC).
//
// A member is required (Require), and is asked for before anything is
// looked up, so a stranger learns nothing about which handles exist. A
// handle nobody has is not-found, and so is a pending account's, except to
// a moderator or admin (domain.Viewer.MaySeeAccount): the two answers are
// the same so a pending account's existence does not show. The handle is
// matched as the column's NOCASE collation matches it, so a handle typed
// with capitals finds its account.
func Account(ctx context.Context, d *db.DB, v domain.Viewer, handle string) (dbq.User, error) {
	var u dbq.User
	if err := Require(v, "read a profile"); err != nil {
		return u, err
	}
	err := d.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		u, err = q.GetUserByHandle(ctx, handle)
		return err
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return u, problem.Coded(problem.CodeNotFound, fmt.Errorf("no account has the handle %q", handle))
	case err != nil:
		return u, err
	case !v.MaySeeAccount(u.Status):
		return dbq.User{}, problem.Coded(problem.CodeNotFound, fmt.Errorf("the account %q is %s", handle, u.Status))
	}
	return u, nil
}

// Author is u as a profile and its post history draw it. An anonymized
// account shows its placeholder handle and nothing else that named the
// person: anonymizing clears the display name and avatar (docs/05), and
// they are withheld here too, so a row caught halfway cannot bring either
// back.
func Author(u dbq.User) dto.Author {
	a := dto.Author{Handle: u.Handle, DisplayName: u.DisplayName, Avatar: u.AvatarBlob.String, Role: u.Role, Kind: u.Kind}
	if domain.Anonymized(u.Handle) {
		a.DisplayName, a.Avatar = "", ""
	}
	return a
}

// Missing is how a post, thread or last post draws an author id the database
// does not have: a database restored from a backup older than the logs, or
// recovered without one (docs/04 "Projection tables"). It is a deleted
// member, drawn as an anonymized account is, and never anyone else: ketju
// reindex keeps new members' ids above every id the logs name, so the id is
// nobody's (TKT-01M3KB7FXC).
func Missing(id int64) dto.Author {
	return dto.Author{Handle: domain.DeletedMemberHandle(id), Role: domain.RoleMember, Kind: domain.KindHuman}
}

// FillMissing fills in Missing for every id in ids that authors lacks, so a
// caller that looked the ids up draws each one as somebody.
func FillMissing(authors map[int64]dto.Author, ids ...int64) {
	for _, id := range ids {
		if _, ok := authors[id]; !ok {
			authors[id] = Missing(id)
		}
	}
}

// AdminUser is u as the admin screens list it (TKT-01M29CXK1X): the author
// block, with the status and times only staff see.
func AdminUser(u dbq.User) dto.AdminUser {
	a := dto.AdminUser{ID: u.ID, User: Author(u), Status: u.Status, CreatedAt: dto.Millis(u.CreatedAt)}
	if u.LastSeenAt.Valid {
		a.LastSeenAt = new(dto.Millis(u.LastSeenAt.Int64))
	}
	return a
}

// AuthorsByID is each id's Author, read through q, and Missing for an id
// the database lacks.
func AuthorsByID(ctx context.Context, q *dbq.Queries, ids []int64) (map[int64]dto.Author, error) {
	out := map[int64]dto.Author{}
	if len(ids) == 0 {
		return out, nil
	}
	us, err := q.UsersByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, u := range us {
		out[u.ID] = Author(u)
	}
	FillMissing(out, ids...)
	return out, nil
}
