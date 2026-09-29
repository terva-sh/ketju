// Package notify writes notifications (docs/04 "notifications"). Each
// function takes the caller's *dbq.Queries, so the rows commit in the
// transaction of the act that causes them, or not at all: a notification
// lost between a commit and a later publish would never be replayed, since
// only thread logs are (TKT-01M29CP2N0).
//
// Notifications are in-app only. Nothing here sends mail, and no mail
// setting exists (docs/10 item 12); TestNoMailIsSent holds that.
//
// Nothing here marks a notification read either. read_at says the recipient
// read it, so closing an application leaves its new_application rows
// unread, and GET /me's count (MeCounts) stops counting them once the
// application is no longer pending.
package notify

import (
	"context"
	"database/sql"
	"time"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// NewApplication tells staff that application appID arrived: one
// new_application row for each active user with role moderator or admin
// (docs/05 "mode = approval" step 3). It returns how many were written.
func NewApplication(ctx context.Context, q *dbq.Queries, appID int64, now time.Time) (int64, error) {
	return q.NotifyStaffOfApplication(ctx, dbq.NotifyStaffOfApplicationParams{
		ApplicationID: sql.NullInt64{Int64: appID, Valid: true}, At: now.UnixMilli()})
}

// ApplicationDecided tells an approved applicant, as userID, the member the
// approval created, that moderator actorID approved application appID. A
// denied applicant has no user to hold the row, and learns the decision from
// the status page instead.
func ApplicationDecided(ctx context.Context, q *dbq.Queries, userID, appID, actorID int64, now time.Time) error {
	return q.NotifyApplicationDecided(ctx, dbq.NotifyApplicationDecidedParams{UserID: userID,
		ApplicationID: sql.NullInt64{Int64: appID, Valid: true}, ActorID: sql.NullInt64{Int64: actorID, Valid: true},
		At: now.UnixMilli()})
}
