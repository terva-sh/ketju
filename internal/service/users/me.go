// Package users answers questions about the viewer's own account, and the
// admin users screen's about everyone's (admin.go).
package users

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/terva-sh/ketju/internal/auth/agentcred"
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
	// QueueCount is /me's queue: the moderator queue's item count for the
	// viewer, or nil for one with no queue (threads.Service.QueueCount,
	// TKT-01M29CRJ4E). Nil here answers null for everyone.
	QueueCount func(ctx context.Context, v domain.Viewer) (*int, error)
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
// One statement is the counts, grouped by forum. The other is perm's batch,
// only when its cache does not already know: the moderator queue's scope,
// every forum at once (perm.ModeratedForums), which also decides which of the
// counted forums the viewer may read (CanEach), so a member who moderates
// nothing pays nothing for the queue. A viewer who moderates somewhere pays
// one queue count statement more (TKT-01M29CRJ4E). Nothing opens a thread
// log; the counts are projections.
//
// An approval-mode applicant gets the anonymous shape with pre_user set: a
// null user stays the one signed-out signal (settled in grooming). Anyone
// else who is not a member, a suspended user above all, gets the anonymous
// shape outright: their account is not theirs to read once suspended.
//
// An agent authenticated by its credential (agentcred) has no session. Its
// fields come from the credential lookup, which read them from the users row
// as the session lookup does, so its /me runs the same statements after the
// middleware's as a member's (TKT-01M3QH3X58). The credential middleware
// lets through only a live credential of an active agent, and answers
// anything else 401 before this runs; v.Member() is still the guard here.
func (s *Service) Me(ctx context.Context) (dto.Me, error) {
	v := domain.ViewerFrom(ctx)
	sess, ok := session.From(ctx)
	if v.PreUser && ok && sess.Kind == session.KindPreUser {
		me := anonymousMe()
		me.PreUser = &dto.PreUser{Provider: sess.Provider}
		return me, nil
	}
	u, ok := accountOf(ctx, v)
	if !v.Member() || !ok {
		return anonymousMe(), nil
	}
	me := dto.Me{
		User: &dto.Author{Handle: u.Handle, DisplayName: u.DisplayName, Avatar: u.AvatarBlob, Role: u.Role, Kind: u.Kind},
		Settings: dto.Settings{Timezone: orDefault(u.Timezone, defaultTimezone), Locale: orDefault(u.Locale, defaultLocale),
			PageSize: pageSize(u.PrefsJSON)},
	}

	// First, because the queue's scope evaluates every forum in one
	// statement on a cold permission cache (perm.ModeratedForums), which
	// also answers the read checks below, so a member who moderates nothing
	// adds no statement to the budget.
	if s.QueueCount != nil {
		var err error
		if me.Queue, err = s.QueueCount(ctx, v); err != nil {
			return dto.Me{}, err
		}
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

// viewerAccount is the viewer's own fields /me shows, from whichever way
// the request authenticated.
type viewerAccount struct {
	Handle, DisplayName, AvatarBlob, Role, Kind, Timezone, Locale, PrefsJSON string
}

// accountOf is the viewer's account: from its session when it has one with
// a user, or else from its agent credential. Each is taken only when it
// names the viewer's own user, so a context whose viewer and credential
// disagree reads as nobody rather than as either.
func accountOf(ctx context.Context, v domain.Viewer) (viewerAccount, bool) {
	if sess, ok := session.From(ctx); ok {
		if u := sess.User; u != nil && u.ID == v.UserID {
			return viewerAccount{Handle: u.Handle, DisplayName: u.DisplayName, AvatarBlob: u.AvatarBlob, Role: u.Role,
				Kind: u.Kind, Timezone: u.Timezone, Locale: u.Locale, PrefsJSON: u.PrefsJSON}, true
		}
		return viewerAccount{}, false
	}
	if c, ok := agentcred.From(ctx); ok && c.UserID == v.UserID {
		a := c.User
		return viewerAccount{Handle: a.Handle, DisplayName: a.DisplayName, AvatarBlob: a.AvatarBlob, Role: c.Role,
			Kind: a.Kind, Timezone: a.Timezone, Locale: a.Locale, PrefsJSON: a.PrefsJSON}, true
	}
	return viewerAccount{}, false
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
