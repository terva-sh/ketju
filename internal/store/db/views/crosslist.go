package views

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// List is one of the cross-forum lists (docs/06 "Thread list views",
// TKT-01M29CTY7K).
type List int

const (
	// Unread is the threads with an unread post by /me's rule, newest
	// activity first.
	Unread List = iota
	// New is the threads the viewer has no read position in, started after
	// their mark, newest started first.
	New
	// Participated is the threads where the viewer has a post that was not
	// moved out, read or unread, newest activity first.
	Participated
	// Watching is the threads with the viewer's thread-level watch, newest
	// activity first.
	Watching
)

// Lists is every List, in the order docs/06 names them.
var Lists = []List{Unread, New, Participated, Watching}

// String is the list's name in its route.
func (l List) String() string {
	switch l {
	case Unread:
		return "unread"
	case New:
		return "new"
	case Participated:
		return "participated"
	case Watching:
		return "watching"
	}
	return fmt.Sprintf("List(%d)", int(l))
}

// Key is r's position in l: created_at for New, last_post_at for the rest.
func (l List) Key(r ThreadRow) Key {
	if l == New {
		return Key{At: r.CreatedAt, ID: r.ID}
	}
	return Key{At: r.LastPostAt, ID: r.ID}
}

// CrossParams selects one page of a cross-forum list.
type CrossParams struct {
	List List
	// UserID is the viewer, a member: every list is theirs.
	UserID int64
	// Forums is the forums the viewer may read now (perm.ReadableForums).
	// A thread in any other forum is in no list. It is bound as one JSON
	// parameter, so no viewer reaches SQLite's limit on bound parameters.
	Forums []int64
	// After is the cursor: the page starts after it. Nil is the first page.
	After *Key
	Limit int
}

// CrossList is a page of p.List for p.UserID, in the list's order after
// p.After, with every row's read state as a forum's list carries it.
//
// Every list holds only threads with a post, in a forum of p.Forums, whose
// effective level for the viewer is not mute (docs/04 "watches"). Then:
//   - Unread: the last post is past the read position and after
//     mark_all_read_at, which is MeCounts's rule for a thread.
//   - New: no read position, and started after the mark (any thread with
//     no mark).
//   - Participated: a post by the viewer that was not moved out.
//   - Watching: a thread-level watch.
//
// Unread and New walk the whole site in their order (threads_activity,
// threads_created) from the cursor and stop at a page; the mark bounds the
// walk, as a constant the index range can use. Participated and Watching
// start from the viewer's own rows, which grow with what they have done
// rather than with the site, and sort those. The keyset is (sort column,
// id), so a thread bumped while a reader pages moves above the cursor and is
// not seen twice.
//
// The rows do not carry what the caller must still check: whether the
// thread is quarantined. The caller drops those after the cursor is taken,
// so a page that loses one still continues where it read to.
func CrossList(ctx context.Context, tx *sql.Tx, p CrossParams) ([]ThreadRow, error) {
	q, args := crossListSQL(p)
	return scanRows(ctx, tx, q, args...)
}

// Membership is which of the lists, read or unread aside, one thread is in
// for one viewer, by CrossList's rules, and which of /me's badges it counts
// in: Unread, New, and Participated and Watching only while unread.
type Membership struct {
	ThreadID, ForumID                   int64
	Unread, New, Participated, Watching bool
}

// Memberships is ids' Membership for userID, for /me to take the threads no
// list may show out of its counts: a quarantined thread is counted by
// MeCounts, which cannot know of it, and dropped by every list
// (TKT-01M29CTY7K). It does not ask whether the viewer may read the forum;
// the caller does, as for MeCounts's rows. A thread with no row is left out.
func Memberships(ctx context.Context, tx *sql.Tx, userID int64, ids []int64) ([]Membership, error) {
	b, err := json.Marshal(ids)
	if err != nil {
		panic(err) // []int64 always marshals
	}
	rows, err := tx.QueryContext(ctx, `
SELECT t.id, t.forum_id,
       t.last_post_at > `+markOf+` AND t.last_post_ordinal > coalesce(rp.last_read_ordinal, 0) AS unread,
       t.created_at > `+markOf+` AND rp.user_id IS NULL AS new,
       EXISTS (SELECT 1 FROM post_meta AS pm
               WHERE pm.author_id = ? AND pm.thread_id = t.id AND pm.moved = 0) AS participated,
       coalesce(tw.level, '') = 'watch' AS watching
FROM threads AS t
LEFT JOIN read_positions AS rp ON rp.user_id = ? AND rp.thread_id = t.id
LEFT JOIN watches AS tw ON tw.user_id = ? AND tw.thread_id = t.id
LEFT JOIN watches AS fw ON fw.user_id = ? AND fw.forum_id = t.forum_id
WHERE t.id IN (SELECT value FROM json_each(?))
  AND t.last_post_ordinal > 0
  AND `+notMuted, userID, userID, userID, userID, userID, userID, string(b))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Membership
	for rows.Next() {
		var m Membership
		var unread, isNew, part, watch int64
		if err := rows.Scan(&m.ThreadID, &m.ForumID, &unread, &isNew, &part, &watch); err != nil {
			return nil, err
		}
		m.Unread, m.New = unread != 0, isNew != 0
		m.Participated, m.Watching = part != 0, watch != 0
		out = append(out, m)
	}
	return out, rows.Err()
}

// notMuted is the effective level, as rowColumns selects it, short of mute.
const notMuted = `coalesce(tw.level, fw.level, '') <> 'mute'`

// markOf is the viewer's mark as a constant for an index range, -1 with
// none, so "after the mark" is true of every time. Its one parameter is the
// viewer.
const markOf = `coalesce((SELECT u.mark_all_read_at FROM users AS u WHERE u.id = ?), -1)`

func crossListSQL(p CrossParams) (string, []any) {
	forums, err := json.Marshal(p.Forums)
	if err != nil {
		panic(err) // []int64 always marshals
	}
	if p.Forums == nil {
		forums = []byte("[]")
	}
	col := "t.last_post_at"
	var q string
	args := []any{p.UserID, p.UserID, p.UserID, p.UserID}
	switch p.List {
	case Unread:
		q = selectRows("threads_activity") + `
WHERE t.last_post_at > ` + markOf + `
  AND t.last_post_ordinal > coalesce(rp.last_read_ordinal, 0)`
		args = append(args, p.UserID)
	case New:
		col = "t.created_at"
		q = selectRows("threads_created") + `
WHERE t.created_at > ` + markOf + `
  AND rp.user_id IS NULL`
		args = append(args, p.UserID)
	case Participated:
		q = fmt.Sprintf(rowColumns, "") + `
WHERE t.id IN (SELECT pm.thread_id FROM post_meta AS pm WHERE pm.author_id = ? AND pm.moved = 0)`
		args = append(args, p.UserID)
	case Watching:
		q = fmt.Sprintf(rowColumns, "") + `
WHERE t.id IN (SELECT w.thread_id FROM watches AS w INDEXED BY watches_thread
               WHERE w.user_id = ? AND w.thread_id IS NOT NULL AND w.level = 'watch')`
		args = append(args, p.UserID)
	default:
		panic(fmt.Sprintf("views: no list %d", p.List))
	}
	q += `
  AND t.last_post_ordinal > 0
  AND t.forum_id IN (SELECT value FROM json_each(?))
  AND ` + notMuted
	args = append(args, string(forums))
	if p.After != nil {
		q += `
  AND (` + col + `, t.id) < (?, ?)`
		args = append(args, p.After.At, p.After.ID)
	}
	q += fmt.Sprintf(`
ORDER BY %s DESC, t.id DESC LIMIT ?`, col)
	args = append(args, p.Limit)
	return q, args
}
