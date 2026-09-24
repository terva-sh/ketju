// Package users answers questions about the viewer's own account.
package users

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Service is the users service.
type Service struct {
	DB   *db.DB
	Perm *perm.Evaluator
}

// The settings a viewer has not chosen (docs/06 "Paged, not infinite" for the
// page size and its range; the anonymous golden for the rest).
const (
	defaultTimezone = "UTC"
	defaultLocale   = "en"
	defaultPageSize = 25
	minPageSize     = 10
	maxPageSize     = 50
)

func anonymousMe() dto.Me {
	return dto.Me{Settings: dto.Settings{Timezone: defaultTimezone, Locale: defaultLocale, PageSize: defaultPageSize}}
}

// Me is GET /me for the request's viewer (docs/07).
//
// The shell inlines it on every page load, so it has a budget: the session
// lookup the middleware already ran, which returned every user field shown
// here, and one statement for the counts. Nothing opens a thread log; the
// counts are projections. Which forums count is perm's decision, served from
// its cache, so a warm request costs no more than those two statements.
//
// A viewer who is not a member gets the anonymous shape. That covers a
// pre-user session, which has no user to show (dto.Me: a nil user is the one
// signed-out signal; whether approval mode needs a marker is TKT-01M29CKRG9's
// question), and a suspended user, whose account is not theirs to read once
// suspended.
func (s *Service) Me(ctx context.Context) (dto.Me, error) {
	v := domain.ViewerFrom(ctx)
	sess, ok := session.From(ctx)
	if !v.Member() || !ok || sess.User == nil {
		return anonymousMe(), nil
	}
	u := sess.User
	me := dto.Me{
		User: &dto.Author{Handle: u.Handle, DisplayName: u.DisplayName, Avatar: u.AvatarBlob, Role: u.Role, Kind: u.Kind},
		Settings: dto.Settings{Timezone: orDefault(u.Timezone, defaultTimezone), Locale: orDefault(u.Locale, defaultLocale),
			PageSize: pageSize(u.PrefsJSON)},
	}

	var rows []dbq.MeCountsRow
	if err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		rows, err = q.MeCounts(ctx, v.UserID)
		return err
	}); err != nil {
		return dto.Me{}, err
	}
	for _, r := range rows {
		me.Notifications = int(r.Notifications)
		if !r.ForumID.Valid {
			continue
		}
		readable, err := s.Perm.Can(ctx, v, r.ForumID.Int64, domain.PermRead)
		if err != nil {
			return dto.Me{}, err
		}
		if !readable {
			continue
		}
		me.Unread.Threads += int(r.Threads.Int64)
		me.Unread.Participated += int(r.Participated.Int64)
		me.Unread.Watching += int(r.Watching.Int64)
	}
	return me, nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// pageSize reads prefs_json's page_size, which docs/06 bounds to 10..50. What
// else prefs_json holds is undecided, so this reads the one key and ignores
// the rest; anything missing, malformed or out of range is the default.
func pageSize(prefs string) int {
	var p struct {
		PageSize json.RawMessage `json:"page_size"`
	}
	var n int64
	// Decoding into an int64 refuses "30", 30.5 and anything else that is
	// not a JSON integer; null leaves 0, which is out of range.
	if json.Unmarshal([]byte(prefs), &p) != nil || json.Unmarshal(p.PageSize, &n) != nil ||
		n < minPageSize || n > maxPageSize {
		return defaultPageSize
	}
	return int(n)
}
