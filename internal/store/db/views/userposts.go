package views

import (
	"context"
	"database/sql"
	"encoding/json"
)

// PostKey is a keyset position in a user's post history: the post's
// created_at, then its thread and ordinal, which break ties between posts
// written in the same millisecond. A page holds the posts strictly after it
// in the history's order, newest first.
type PostKey struct {
	At      int64
	Thread  int64
	Ordinal int64
}

// UserPostRow is one post in a user's history, with what a link to it needs.
//
// It carries no deleted flag. post_meta's trails the log, and the history
// asks the thread's index instead, for every post, so a post deleted or
// restored since the projection last heard is shown as it now is.
type UserPostRow struct {
	ThreadID    int64
	Ordinal     int64
	CreatedAt   int64
	ThreadSlug  string
	ThreadTitle string
	ForumSlug   string
	ForumName   string
}

// Key is r's position in the history.
func (r UserPostRow) Key() PostKey {
	return PostKey{At: r.CreatedAt, Thread: r.ThreadID, Ordinal: r.Ordinal}
}

// UserPostsParams selects one batch of a user's history.
type UserPostsParams struct {
	AuthorID int64
	// Forums are the forums the viewer may read. A post in any other forum
	// is left out, so the history says nothing about forums the viewer
	// cannot see. Empty selects nothing.
	Forums []int64
	// After is the cursor: the batch starts after it. Nil is the start.
	After *PostKey
	Limit int
}

// UserPosts is a batch of p.AuthorID's posts in p.Forums, newest first,
// after p.After, deleted or not: the candidates for a page of the history
// (docs/07 GET /users/{handle}/posts), which the caller narrows by each
// post's index entry.
//
// It reads post_meta through post_meta_author, (author_id, created_at). The
// table is WITHOUT ROWID, so that index also carries the primary key
// (thread_id, ordinal), and the whole order is the index's: the keyset
// comparison seeks rather than reading the posts before the cursor. A moved
// post is left out, since it renders as a pointer to where it went and the
// post it became is the author's in its new thread; a post is never moved
// back, so the projection's flag cannot be stale in the direction that
// would hide one.
func UserPosts(ctx context.Context, tx *sql.Tx, p UserPostsParams) ([]UserPostRow, error) {
	q, args := userPostsSQL(p)
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserPostRow
	for rows.Next() {
		var r UserPostRow
		if err := rows.Scan(&r.ThreadID, &r.Ordinal, &r.CreatedAt, &r.ThreadSlug, &r.ThreadTitle,
			&r.ForumSlug, &r.ForumName); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func userPostsSQL(p UserPostsParams) (string, []any) {
	q := `
SELECT pm.thread_id, pm.ordinal, pm.created_at, t.slug, t.title, f.slug, f.name
FROM post_meta AS pm INDEXED BY post_meta_author
JOIN threads AS t ON t.id = pm.thread_id
JOIN forums AS f ON f.id = t.forum_id
WHERE pm.author_id = ? AND pm.moved = 0
  AND t.forum_id IN (SELECT value FROM json_each(?))`
	args := []any{p.AuthorID, forumList(p.Forums)}
	if p.After != nil {
		q += ` AND (pm.created_at, pm.thread_id, pm.ordinal) < (?, ?, ?)`
		args = append(args, p.After.At, p.After.Thread, p.After.Ordinal)
	}
	q += ` ORDER BY pm.created_at DESC, pm.thread_id DESC, pm.ordinal DESC LIMIT ?`
	args = append(args, p.Limit)
	return q, args
}

// ThreadPosts is one thread's share of a user's posts, for the profile's
// post count.
type ThreadPosts struct {
	ThreadID int64
	// Projected is how far into the thread's log the projection has got
	// (threads.log_length_projected). Where it has reached the whole log,
	// NotDeleted is exact; where it has not, the caller asks the index about
	// Ordinals instead.
	Projected  int64
	NotDeleted int64
	// Ordinals are every post of the user's in the thread that post_meta
	// holds, deleted or not, in ordinal order: the same candidates UserPosts
	// gives the history.
	Ordinals []uint32
}

// UserThreadPosts is authorID's non-moved posts in forums, grouped by
// thread, in thread order (docs/07 GET /users/{handle}: the post count).
func UserThreadPosts(ctx context.Context, tx *sql.Tx, authorID int64, forums []int64) ([]ThreadPosts, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT pm.thread_id, t.log_length_projected, pm.ordinal, pm.deleted
FROM post_meta AS pm INDEXED BY post_meta_author
JOIN threads AS t ON t.id = pm.thread_id
WHERE pm.author_id = ? AND pm.moved = 0
  AND t.forum_id IN (SELECT value FROM json_each(?))
ORDER BY pm.thread_id, pm.ordinal`, authorID, forumList(forums))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ThreadPosts
	for rows.Next() {
		var id, projected, ordinal, deleted int64
		if err := rows.Scan(&id, &projected, &ordinal, &deleted); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].ThreadID != id {
			out = append(out, ThreadPosts{ThreadID: id, Projected: projected})
		}
		t := &out[len(out)-1]
		t.Ordinals = append(t.Ordinals, uint32(ordinal))
		if deleted == 0 {
			t.NotDeleted++
		}
	}
	return out, rows.Err()
}

// forumList binds a set of forum ids as one parameter, so no number of
// forums can reach SQLite's limit on bound parameters.
func forumList(ids []int64) string {
	if ids == nil {
		ids = []int64{}
	}
	b, err := json.Marshal(ids)
	if err != nil {
		panic(err) // []int64 always marshals
	}
	return string(b)
}
