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
