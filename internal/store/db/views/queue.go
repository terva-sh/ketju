package views

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// The moderator queue (docs/08 "Flags", TKT-01M29CRJ4E): open flags, one
// item per flagged post, and pending applications, in one list by newest
// time first. It is a view because the forums a viewer moderates are a list
// of any length, which sqlc cannot bind as part of a UNION.
//
// Both halves are optional. Flags are those in the forums the caller passes
// (perm.ModeratedForums), in threads that are not archived: an archived
// thread is read-only, its flags included, so no action could clear them.
// Applications are listed only when the caller says the viewer may review
// them. QueueItems and QueueCount build their WHERE clauses in one place,
// queueUnion, so the count /me shows and the list cannot disagree.

// Queue item kinds, as the list sorts them: a flag sorts after an
// application of the same time.
const (
	QueueKindApplication = 0
	QueueKindFlag        = 1
)

// QueueCursor is the keyset of a queue page: the last item's time, kind and
// keys. A flag's keys are its thread and ordinal; an application's are its
// id and 0. The zero cursor is the start of the list.
type QueueCursor struct {
	At, Kind, K1, K2 int64
	Set              bool
}

// QueueRow is one item. A flag item has its thread's slug and title and its
// open flag count; an application item has its identity's provider, email
// and verified flag, each null once the identity was removed.
type QueueRow struct {
	At, Kind, K1, K2 int64
	Slug, Title      string
	Flags            int64
	Provider, Email  sql.NullString
	Verified         sql.NullInt64
}

// queueUnion is the queue as one SELECT of (at, kind, k1, k2, slug, title,
// flags, provider, email, verified), or "" for a viewer with no queue.
func queueUnion(forums []int64, applications bool) (string, []any) {
	var parts []string
	var args []any
	if len(forums) > 0 {
		parts = append(parts, `SELECT max(f.created_at) AS at, `+strconv.Itoa(QueueKindFlag)+` AS kind, f.thread_id AS k1,
			f.ordinal AS k2, t.slug AS slug, t.title AS title, count(*) AS flags,
			NULL AS provider, NULL AS email, NULL AS verified
		FROM flags AS f JOIN threads AS t ON t.id = f.thread_id
		WHERE f.status = 'open' AND t.archived = 0 AND t.forum_id IN (SELECT value FROM json_each(?))
		GROUP BY f.thread_id, f.ordinal`)
		args = append(args, forumList(forums))
	}
	if applications {
		parts = append(parts, `SELECT a.created_at, `+strconv.Itoa(QueueKindApplication)+`, a.id, 0, '', '', 0,
			i.provider_key, i.email, i.email_verified
		FROM applications AS a LEFT JOIN identities AS i ON i.id = a.identity_id
		WHERE a.status = 'pending'`)
	}
	return strings.Join(parts, "\nUNION ALL\n"), args
}

// QueueItems is up to limit items after cursor, newest first. Ties on time
// go to the flag, then to the higher keys, so the order is total and a page
// repeats nothing.
func QueueItems(ctx context.Context, tx *sql.Tx, forums []int64, applications bool, after QueueCursor, limit int) ([]QueueRow, error) {
	union, args := queueUnion(forums, applications)
	if union == "" {
		return nil, nil
	}
	// The name comment is what a statement trace reports, as sqlc's are.
	q := "-- name: QueueItems\n" +
		`SELECT at, kind, k1, k2, slug, title, flags, provider, email, verified FROM (` + union + `)`
	if after.Set {
		q += ` WHERE (at, kind, k1, k2) < (?, ?, ?, ?)`
		args = append(args, after.At, after.Kind, after.K1, after.K2)
	}
	q += ` ORDER BY at DESC, kind DESC, k1 DESC, k2 DESC LIMIT ?`
	args = append(args, limit)
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueueRow
	for rows.Next() {
		var r QueueRow
		if err := rows.Scan(&r.At, &r.Kind, &r.K1, &r.K2, &r.Slug, &r.Title, &r.Flags, &r.Provider, &r.Email, &r.Verified); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// QueueCount is how many items QueueItems would list across every page, in
// one statement.
func QueueCount(ctx context.Context, tx *sql.Tx, forums []int64, applications bool) (int, error) {
	union, args := queueUnion(forums, applications)
	if union == "" {
		return 0, nil
	}
	var n int
	err := tx.QueryRowContext(ctx, "-- name: QueueCount\n"+`SELECT count(*) FROM (`+union+`)`, args...).Scan(&n)
	return n, err
}

// QueueReport is one open flag on a post the queue lists.
type QueueReport struct {
	ID, Thread, Ordinal, Reporter int64
	Reason, Note                  string
	CreatedAt                     int64
}

// QueuePost names one flagged post.
type QueuePost struct{ Thread, Ordinal int64 }

// QueueReports is the open flags of posts, newest first within each post,
// through the flags_post index.
func QueueReports(ctx context.Context, tx *sql.Tx, posts []QueuePost) ([]QueueReport, error) {
	if len(posts) == 0 {
		return nil, nil
	}
	pairs := make([]string, len(posts))
	args := make([]any, 0, 2*len(posts))
	for i, p := range posts {
		pairs[i] = "(?, ?)"
		args = append(args, p.Thread, p.Ordinal)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, thread_id, ordinal, reporter_id, reason, note, created_at FROM flags
		WHERE status = 'open' AND (thread_id, ordinal) IN (VALUES `+strings.Join(pairs, ", ")+`)
		ORDER BY thread_id, ordinal, created_at DESC, id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueueReport
	for rows.Next() {
		var r QueueReport
		if err := rows.Scan(&r.ID, &r.Thread, &r.Ordinal, &r.Reporter, &r.Reason, &r.Note, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
