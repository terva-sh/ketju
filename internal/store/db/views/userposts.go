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
type UserPostRow struct {
	ThreadID    int64
	Ordinal     int64
	CreatedAt   int64
	ThreadSlug  string
	ThreadTitle string
	ForumSlug   string
	ForumName   string
	// Deleted is post_meta's flag, which trails the log; the caller checks
	// the index before it trusts it either way.
	Deleted bool
}

// Key is r's position in the history.
func (r UserPostRow) Key() PostKey {
	return PostKey{At: r.CreatedAt, Thread: r.ThreadID, Ordinal: r.Ordinal}
}

// UserPostsParams selects one page of a user's history.
type UserPostsParams struct {
	AuthorID int64
	// Forums are the forums the viewer may read. A post in any other forum
	// is left out, so the page says nothing about forums the viewer cannot
	// see. Empty selects nothing.
	Forums []int64
	// WithDeleted keeps deleted posts, for the viewers who see them marked
	// (docs/07 GET /users/{handle}/posts): the author, moderators and admins.
	WithDeleted bool
	// After is the cursor: the page starts after it. Nil is the first page.
	After *PostKey
	Limit int
}

// UserPosts is a page of p.AuthorID's posts in p.Forums, newest first, after
// p.After (docs/07 GET /users/{handle}/posts).
//
// It reads post_meta through post_meta_author, (author_id, created_at). The
// table is WITHOUT ROWID, so that index also carries the primary key
// (thread_id, ordinal), and the whole order is the index's: the keyset
// comparison seeks rather than reading the posts before the cursor. A moved
// post is left out, since it renders as a pointer to where it went and the
// post it became is the author's in its new thread.
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
		var deleted int64
		if err := rows.Scan(&r.ThreadID, &r.Ordinal, &r.CreatedAt, &r.ThreadSlug, &r.ThreadTitle,
			&r.ForumSlug, &r.ForumName, &deleted); err != nil {
			return nil, err
		}
		r.Deleted = deleted != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func userPostsSQL(p UserPostsParams) (string, []any) {
	q := `
SELECT pm.thread_id, pm.ordinal, pm.created_at, t.slug, t.title, f.slug, f.name, pm.deleted
FROM post_meta AS pm INDEXED BY post_meta_author
JOIN threads AS t ON t.id = pm.thread_id
JOIN forums AS f ON f.id = t.forum_id
WHERE pm.author_id = ? AND pm.moved = 0
  AND t.forum_id IN (SELECT value FROM json_each(?))`
	args := []any{p.AuthorID, forumList(p.Forums)}
	if !p.WithDeleted {
		q += ` AND pm.deleted = 0`
	}
	if p.After != nil {
		q += ` AND (pm.created_at, pm.thread_id, pm.ordinal) < (?, ?, ?)`
		args = append(args, p.After.At, p.After.Thread, p.After.Ordinal)
	}
	q += ` ORDER BY pm.created_at DESC, pm.thread_id DESC, pm.ordinal DESC LIMIT ?`
	args = append(args, p.Limit)
	return q, args
}

// UserPostCount is the profile's post count (docs/07 GET /users/{handle}):
// authorID's posts in forums, not deleted and not moved. It is UserPosts's
// set without WithDeleted, counted, so the count is what a member's history
// lists.
func UserPostCount(ctx context.Context, tx *sql.Tx, authorID int64, forums []int64) (int64, error) {
	var n int64
	err := tx.QueryRowContext(ctx, `
SELECT count(*)
FROM post_meta AS pm INDEXED BY post_meta_author
JOIN threads AS t ON t.id = pm.thread_id
WHERE pm.author_id = ? AND pm.moved = 0 AND pm.deleted = 0
  AND t.forum_id IN (SELECT value FROM json_each(?))`, authorID, forumList(forums)).Scan(&n)
	return n, err
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
