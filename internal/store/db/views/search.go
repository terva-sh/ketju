package views

import (
	"context"
	"database/sql"
	"strings"
)

// SearchParams is one page of a full-text search (docs/07 "Search",
// TKT-01M29CRJ3B). The API builds it (TKT-01M29CTY8T).
type SearchParams struct {
	// Match is the FTS5 query, built by MatchAll and never from a reader's
	// text directly.
	Match string

	// Forums are the forums the viewer may read, from perm.ReadableForums.
	// They are a condition of the one statement that matches, orders and
	// pages, so a page's rows and its length never depend on a forum the
	// viewer cannot read. Empty matches nothing.
	Forums []int64

	// Forum, when set, keeps only that forum's own threads.
	Forum *int64

	// Author, when set, keeps only that member's posts and threads.
	Author *int64

	// After and Before bound created_at in Unix ms: After is the first
	// included, Before the first not.
	After, Before *int64

	Limit, Offset int
}

// SearchRow is one hit. Ordinal 0 is the thread's title.
type SearchRow struct {
	ThreadID  int64
	Ordinal   int64
	Revision  int64
	AuthorID  int64
	CreatedAt int64

	// DocTitle is the title the row indexed, on a title row; the API checks
	// it is still the thread's title before serving the hit.
	DocTitle string

	// Snippet is plain text with each match between render.SnippetOpen and
	// render.SnippetClose.
	Snippet string

	ThreadSlug, ThreadTitle string
	ForumSlug, ForumName    string
}

// Search runs one page of a search. It is hand-written because sqlc cannot
// parse FTS5's whole-table MATCH (TKT-01M29BW9Q1's note). Rows come newest
// first, by created_at and then the row; nothing orders by FTS5's rank,
// whose statistics span every forum (docs/07 "Search").
//
// t.post_count > 0 leaves out a thread with no post yet, as the thread page
// does.
func Search(ctx context.Context, tx *sql.Tx, p SearchParams) ([]SearchRow, error) {
	q := `SELECT d.thread_id, d.ordinal, d.revision, d.author_id, d.created_at, d.title,
       snippet(search, -1, char(57344), char(57345), '…', 24),
       t.slug, t.title, f.slug, f.name
FROM search
JOIN search_docs AS d ON d.id = search.rowid
JOIN threads AS t ON t.id = d.thread_id
JOIN forums AS f ON f.id = t.forum_id
WHERE search MATCH ?
  AND t.forum_id IN (SELECT value FROM json_each(?))
  AND t.post_count > 0`
	args := []any{p.Match, forumList(p.Forums)}
	if p.Forum != nil {
		q += ` AND t.forum_id = ?`
		args = append(args, *p.Forum)
	}
	if p.Author != nil {
		q += ` AND d.author_id = ?`
		args = append(args, *p.Author)
	}
	if p.After != nil {
		q += ` AND d.created_at >= ?`
		args = append(args, *p.After)
	}
	if p.Before != nil {
		q += ` AND d.created_at < ?`
		args = append(args, *p.Before)
	}
	q += ` ORDER BY d.created_at DESC, d.id DESC LIMIT ? OFFSET ?`
	args = append(args, p.Limit, p.Offset)

	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchRow
	for rows.Next() {
		var r SearchRow
		if err := rows.Scan(&r.ThreadID, &r.Ordinal, &r.Revision, &r.AuthorID, &r.CreatedAt, &r.DocTitle,
			&r.Snippet, &r.ThreadSlug, &r.ThreadTitle, &r.ForumSlug, &r.ForumName); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MatchAll builds the FTS5 query that matches rows holding every term. Each
// term is an FTS5 string, its inner quotes doubled, so it is matched as the
// words it holds and a term of several words is a phrase. No operator,
// column filter, prefix or NEAR reaches FTS5 from a reader. The API parses q
// into terms (TKT-01M29CTY8T).
func MatchAll(terms []string) string {
	quoted := make([]string, 0, len(terms))
	for _, t := range terms {
		quoted = append(quoted, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " ")
}
