// Package users answers questions about the viewer's own account, and the
// admin users screen's about everyone's (admin.go).
package users

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

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
	// Now is the clock the admin detail counts live sessions by; nil is
	// time.Now.
	Now func() time.Time
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
// The shell inlines it on every page load, so it has a budget, settled in
// grooming: two statements for the handler, with the middleware's session
// lookup, which already returned every user field shown here, outside it.
// One statement is the counts, grouped by forum. The other is perm deciding
// which of those forums the viewer may read, in one batch (CanEach), and only
// when perm's cache does not already know. Nothing opens a thread log; the
// counts are projections.
//
// An approval-mode applicant gets the anonymous shape with pre_user set: a
// null user stays the one signed-out signal (settled in grooming). Anyone
// else who is not a member, a suspended user above all, gets the anonymous
// shape outright: their account is not theirs to read once suspended.
func (s *Service) Me(ctx context.Context) (dto.Me, error) {
	v := domain.ViewerFrom(ctx)
	sess, ok := session.From(ctx)
	if v.PreUser && ok && sess.Kind == session.KindPreUser {
		me := anonymousMe()
		me.PreUser = &dto.PreUser{Provider: sess.Provider}
		return me, nil
	}
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
	var forums []int64
	for _, r := range rows {
		if r.ForumID.Valid {
			forums = append(forums, r.ForumID.Int64)
		}
	}
	readable := map[int64]bool{}
	if len(forums) > 0 {
		var err error
		if readable, err = s.Perm.CanEach(ctx, v, forums, domain.PermRead); err != nil {
			return dto.Me{}, err
		}
	}
	for _, r := range rows {
		me.Notifications = int(r.Notifications)
		if !r.ForumID.Valid || !readable[r.ForumID.Int64] {
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
