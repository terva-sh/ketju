// Package views holds the hand-written view queries: the ones whose shape
// depends on the request (a sort, a cursor, a lens), which docs/04 keeps
// beside the sqlc set rather than in it. They are still plain database/sql
// with ? parameters, so the rule that SQL stays driver-agnostic holds here as
// it does in dbq.
//
// Everything here reads projections. No query opens or needs a thread log.
package views

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Sort is a thread list's order.
type Sort int

const (
	// ByActivity is last_post_at, newest first: the default (docs/06).
	ByActivity Sort = iota
	// ByCreated is created_at, newest first: ?sort=created, for
	// announcement forums.
	ByCreated
)

// Key is a keyset position: the sort column's value and the thread id that
// breaks ties. A page holds the rows strictly after it in the list's order.
type Key struct {
	At int64
	ID int64
}

// ThreadRow is one thread in a list, with the viewer's read state.
type ThreadRow struct {
	ID               int64
	ForumID          int64
	Slug             string
	Title            string
	AuthorID         int64
	CreatedAt        int64
	PostCount        int64
	LastPostAt       int64
	LastPostAuthorID int64
	LastPostOrdinal  int64
	Locked           bool
	PinnedScope      sql.NullString
	PinnedUntil      sql.NullInt64
	// LastReadOrdinal is the viewer's read position, 0 with no row.
	LastReadOrdinal int64
	// Unread is the viewer's unread posts in the thread, by docs/04's
	// per-post rule (dbq's UnreadCount): past the read position and after
	// mark_all_read_at.
	Unread int64
}

// Key is r's position in a list sorted by s.
func (r ThreadRow) Key(s Sort) Key {
	if s == ByCreated {
		return Key{At: r.CreatedAt, ID: r.ID}
	}
	return Key{At: r.LastPostAt, ID: r.ID}
}

// rowColumns is what every thread list selects, for scanRows. The viewer is
// bound twice, as the first two parameters: users AS me, then read_positions.
//
// The unread count is UnreadCount's rule for each row without reading a post
// where the answer is known without one: nothing past the read position is
// 0; with no mark, ordinals are dense and one-based, so it is a subtraction;
// a last post at or before the mark is 0. Only a thread with posts on both
// sides of the mark counts post_meta, by a primary key range that reads the
// posts past the read position. TestUnreadMatchesUnreadCount holds the two
// rules together.
const rowColumns = `
SELECT t.id, t.forum_id, t.slug, t.title, t.author_id, t.created_at, t.post_count, t.last_post_at,
       t.last_post_author_id, t.last_post_ordinal, t.locked, t.pinned_scope, t.pinned_until,
       coalesce(rp.last_read_ordinal, 0),
       CASE
         WHEN t.last_post_ordinal <= coalesce(rp.last_read_ordinal, 0) THEN 0
         WHEN me.mark_all_read_at IS NULL THEN t.last_post_ordinal - coalesce(rp.last_read_ordinal, 0)
         WHEN t.last_post_at <= me.mark_all_read_at THEN 0
         ELSE (SELECT count(*) FROM post_meta AS pm
               WHERE pm.thread_id = t.id AND pm.ordinal > coalesce(rp.last_read_ordinal, 0)
                 AND pm.created_at > me.mark_all_read_at)
       END
FROM threads AS t%s
LEFT JOIN users AS me ON me.id = ?
LEFT JOIN read_positions AS rp ON rp.user_id = ? AND rp.thread_id = t.id
`

// selectRows is rowColumns reading threads through index. Naming the index
// makes a query whose index has gone fail rather than quietly scan.
func selectRows(index string) string {
	return fmt.Sprintf(rowColumns, " INDEXED BY "+index)
}

// activePin is true of a thread pinned now: a pin whose pinned_until has
// passed is no pin, and its thread sorts with the rest. Its one parameter is
// now.
const activePin = `(t.pinned_scope IS NOT NULL AND (t.pinned_until IS NULL OR t.pinned_until > ?))`

// ListParams selects one page of a forum's threads below its pins.
type ListParams struct {
	ForumID int64
	// UserID is the viewer, 0 for an anonymous one, whose read state is then
	// meaningless and should be ignored by the caller.
	UserID int64
	Sort   Sort
	// After is the cursor: the page starts after it. Nil is the first page.
	After *Key
	// Pinned is the forum's threads its first page shows as pins, from
	// PinnedThreads, which every page leaves out and a later page's cursor
	// carries. Leaving out exactly these, whatever has happened to their
	// pins since, is what keeps a pin that expires or is removed between
	// pages from being shown twice (review on PR #84).
	Pinned []int64
	Limit  int
}

// ThreadList is a page of p.ForumID's threads with a post, less p.Pinned, in
// p.Sort's order after p.After.
//
// The cursor is a keyset on (sort column, id), compared as a row value, so
// SQLite seeks in threads_forum_activity or threads_forum_created (both end
// in the rowid) rather than reading and discarding the rows before it. A
// thread bumped while a reader pages moves above the cursor: it is not seen
// twice, and nothing that stays below the cursor is skipped.
func ThreadList(ctx context.Context, tx *sql.Tx, p ListParams) ([]ThreadRow, error) {
	q, args := threadListSQL(p)
	return scanRows(ctx, tx, q, args...)
}

func threadListSQL(p ListParams) (string, []any) {
	col, index := "t.last_post_at", "threads_forum_activity"
	if p.Sort == ByCreated {
		col, index = "t.created_at", "threads_forum_created"
	}
	q := selectRows(index) + `
WHERE t.forum_id = ? AND t.last_post_ordinal > 0`
	args := []any{p.UserID, p.UserID, p.ForumID}
	if p.After != nil {
		q += ` AND (` + col + `, t.id) < (?, ?)`
		args = append(args, p.After.At, p.After.ID)
	}
	if len(p.Pinned) > 0 {
		// One parameter however many pins, so no cursor can reach SQLite's
		// limit on bound parameters (review on PR #84).
		ids, err := json.Marshal(p.Pinned)
		if err != nil {
			panic(err) // []int64 always marshals
		}
		q += ` AND t.id NOT IN (SELECT value FROM json_each(?))`
		args = append(args, string(ids))
	}
	q += fmt.Sprintf(` ORDER BY %s DESC, t.id DESC LIMIT ?`, col)
	args = append(args, p.Limit)
	return q, args
}

// MaxPinned bounds the pins that head a list, and so the pins a list's
// cursor carries. A community pins a handful; the bound is there so neither
// a page nor a cursor grows without limit if one pins everything (review on
// PR #84). The caller applies it after dropping the pins the viewer may not
// see, so pins from a hidden forum never take a visible pin's place (review
// 3 on PR #84); pins past it sort with the rest, as unpinned threads do.
const MaxPinned = 50

// PinnedThreads is every pin that could head forumID's list: global pins
// from every forum, then forumID's own forum pins, each newest activity
// first. It reads only pinned rows (threads_pinned), which moderators alone
// create. The caller drops the global pins from forums the viewer may not
// read, perm answering for all their forums at once, then keeps the first
// MaxPinned.
func PinnedThreads(ctx context.Context, tx *sql.Tx, forumID, userID, now int64) ([]ThreadRow, error) {
	q, args := pinnedSQL(forumID, userID, now)
	return scanRows(ctx, tx, q, args...)
}

func pinnedSQL(forumID, userID, now int64) (string, []any) {
	q := selectRows("threads_pinned") + `
WHERE t.last_post_ordinal > 0 AND ` + activePin + `
  AND (t.pinned_scope = 'global' OR (t.pinned_scope = 'forum' AND t.forum_id = ?))
ORDER BY t.pinned_scope = 'global' DESC, t.last_post_at DESC, t.id DESC`
	return q, []any{userID, userID, now, forumID}
}

func scanRows(ctx context.Context, tx *sql.Tx, q string, args ...any) ([]ThreadRow, error) {
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ThreadRow
	for rows.Next() {
		var r ThreadRow
		var locked int64
		if err := rows.Scan(&r.ID, &r.ForumID, &r.Slug, &r.Title, &r.AuthorID, &r.CreatedAt, &r.PostCount,
			&r.LastPostAt, &r.LastPostAuthorID, &r.LastPostOrdinal, &locked, &r.PinnedScope, &r.PinnedUntil,
			&r.LastReadOrdinal, &r.Unread); err != nil {
			return nil, err
		}
		r.Locked = locked != 0
		out = append(out, r)
	}
	return out, rows.Err()
}
