package threads

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/forms"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/views"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Search's limits (docs/07 "Search"): a page, how far a search reaches, and
// how long q may be.
const (
	SearchPageSize = DefaultPageSize
	SearchReach    = 1000
	SearchMaxRunes = 200
	SearchMaxWords = 16
)

// SearchQuery is GET /search's parameters as the query string gives them.
// An empty field does not filter.
type SearchQuery struct {
	Q      string
	Forum  string // a slug
	Author string // a handle
	// After and Before are Unix ms: After the first included, Before the
	// first not.
	After, Before string
	Cursor        string
}

// Search is GET /search (docs/07 "Search"): one page of the posts and
// thread titles matching q, in the forums v may read, newest first.
//
// It never widens access (docs/14 "Invariants, added"). Two things make
// that so, and neither trusts the index alone:
//
//   - The forums v may read (perm.ReadableForums) are a condition of the one
//     statement that matches, orders and pages, so a page's rows, their
//     order and their number say nothing about a forum v cannot read.
//   - Every hit is judged again when it is served, against its thread's log
//     (judge). The index can trail the log, and the projection the
//     statement joined can trail it too, so a hit is served only if, at the
//     log length the request read, its thread is in a forum v may read and
//     its post is still visible at the revision that was indexed, or its
//     title is still the thread's title.
//
// A judged-out hit is passed over and the page is filled from further rows,
// so a page is short only at the end of the results or of SearchReach. The
// cursor is an offset into the statement's rows, which hold only readable
// forums, so it too depends on nothing v cannot read.
//
// An inactive viewer is served as anonymous, as the thread page serves one.
func (s *Service) Search(ctx context.Context, v domain.Viewer, in SearchQuery) (dto.SearchResults, error) {
	out := dto.SearchResults{Results: []dto.SearchResult{}}
	if s.SearchIndexing != nil {
		out.Indexing = s.SearchIndexing()
	}
	if !v.Member() {
		v = domain.Anonymous()
	}
	terms, err := parseTerms(in.Q)
	if err != nil {
		return out, err
	}
	p := views.SearchParams{Match: views.MatchAll(terms)}
	if p.After, err = searchMillis("after", in.After); err != nil {
		return out, err
	}
	if p.Before, err = searchMillis("before", in.Before); err != nil {
		return out, err
	}
	offset, err := decodeSearchCursor(in.Cursor)
	if err != nil {
		return out, err
	}
	if in.Forum != "" {
		f, err := s.readForum(ctx, v, in.Forum)
		if err != nil {
			return out, err
		}
		p.Forum = &f.ID
	}
	if p.Forums, err = s.Perm.ReadableForums(ctx, v); err != nil {
		return out, err
	}
	if in.Author != "" {
		// As the audit log's actor is found: a handle nobody has, an
		// anonymized account's old one included, is an empty page rather
		// than an error, so the answer says nothing of who exists.
		var u dbq.User
		err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
			var err error
			u, err = q.GetUserByHandle(ctx, in.Author)
			return err
		})
		if errors.Is(err, sql.ErrNoRows) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		p.Author = &u.ID
	}

	readable := make(map[int64]bool, len(p.Forums))
	for _, f := range p.Forums {
		readable[f] = true
	}
	type shown struct {
		hit  searchHit
		next int // the offset just past this row
	}
	var page []shown
	for off := offset; len(page) <= SearchPageSize && off < SearchReach; {
		p.Offset, p.Limit = off, min(SearchPageSize+1, SearchReach-off)
		var rows []views.SearchRow
		if err := s.DB.Read(ctx, func(_ *dbq.Queries, tx *sql.Tx) error {
			var err error
			rows, err = views.Search(ctx, tx, p)
			return err
		}); err != nil {
			return out, err
		}
		hits, err := s.judge(ctx, rows, readable, p.Forum)
		if err != nil {
			return out, err
		}
		for i := range rows {
			if h, ok := hits[i]; ok && len(page) <= SearchPageSize {
				page = append(page, shown{hit: h, next: off + i + 1})
			}
		}
		if len(rows) < p.Limit {
			break // the results end here
		}
		off += len(rows)
	}
	if len(page) > SearchPageSize {
		page = page[:SearchPageSize]
		out.NextCursor = encodeSearchCursor(page[SearchPageSize-1].next)
	}

	ids := map[int64]bool{}
	for _, it := range page {
		ids[it.hit.author] = true
	}
	authors, err := s.users(ctx, ids)
	if err != nil {
		return out, err
	}
	for _, it := range page {
		r := it.hit.result
		r.Author = authors[it.hit.author]
		out.Results = append(out.Results, r)
	}
	return out, nil
}

// searchHit is a row judge kept: the result without its author, and whose
// author it is.
type searchHit struct {
	result dto.SearchResult
	author int64
}

// judge decides which of a batch's rows v is served, by index into rows.
// Each thread is opened once, for all its rows in the batch.
//
// Under the thread, one snapshot of its index gives the log length at and
// the entries of the batch's posts. The thread's projection row is then
// brought up to at if it trails, as a suspension's hide does (authorThread,
// Terva reviews 1495 and 1503), and read again. The thread's hits are kept
// only if that row is at or past at, has a post, and is in a forum v may
// read (and the forum asked for). So a ThreadMoved into a forum v cannot
// read is judged by the forum it moved to, even when the statement read the
// row before the move was projected (Terva review 1701). A thread that
// cannot be projected has its hits dropped and the failure logged: the
// check fails closed. A quarantined thread's hits are dropped, as its
// endpoints refuse it.
//
// Then each row: a post's only if its entry is visible and is the revision,
// author and time the row indexed, so no snippet shows a revision the
// thread no longer shows; a title's only if it is the row's title, which is
// the title at at or later (Terva review 1701). The link a hit carries, the
// thread's title and slug and its forum's, is drawn from that row, never
// from the statement's join, which may trail.
func (s *Service) judge(ctx context.Context, rows []views.SearchRow, readable map[int64]bool, only *int64) (map[int]searchHit, error) {
	byThread := map[int64][]int{}
	var order []int64
	for i, r := range rows {
		if _, ok := byThread[r.ThreadID]; !ok {
			order = append(order, r.ThreadID)
		}
		byThread[r.ThreadID] = append(byThread[r.ThreadID], i)
	}
	out := map[int]searchHit{}
	for _, tid := range order {
		id := threadid.ID(tid)
		if s.Quarantined != nil && s.Quarantined(id) {
			continue
		}
		var ordinals []uint32
		for _, i := range byThread[tid] {
			if o := rows[i].Ordinal; o > 0 {
				ordinals = append(ordinals, uint32(o))
			}
		}
		slices.Sort(ordinals)
		ordinals = slices.Compact(ordinals)
		err := s.Threads.With(id, func(th *threadcache.Thread) error {
			snap, err := s.stable(th, func(ix *threadlog.Index, _ int64) ([]uint32, error) {
				// An ordinal past the index is not a post this thread
				// shows; it is left out rather than failing the search.
				n := uint32(ix.Len())
				return slices.DeleteFunc(slices.Clone(ordinals), func(o uint32) bool { return o > n }), nil
			})
			if err != nil {
				return err
			}
			row, forum, ok, err := s.threadAt(ctx, id, snap.logLength)
			if err != nil || !ok {
				return err
			}
			if row.PostCount == 0 || !readable[row.ForumID] || (only != nil && *only != row.ForumID) {
				return nil
			}
			entries := make(map[uint32]threadlog.IndexEntry, len(snap.entries))
			for i, e := range snap.entries {
				entries[snap.ordinals[i]] = e
			}
			for _, i := range byThread[tid] {
				r := rows[i]
				author := r.AuthorID
				if r.Ordinal == 0 {
					if r.DocTitle != row.Title {
						continue
					}
					author = row.AuthorID
				} else {
					if e, ok := entries[uint32(r.Ordinal)]; !ok || !servesPost(e, r) {
						continue
					}
				}
				out[i] = searchHit{author: author, result: dto.SearchResult{
					Thread:    dto.ThreadLink{ID: id.String(), Slug: row.Slug, Title: row.Title},
					Forum:     dto.ForumRef{Slug: forum.Slug, Name: forum.Name},
					Ordinal:   uint32(r.Ordinal),
					CreatedAt: dto.Millis(r.CreatedAt),
					Snippet:   splitSnippet(r.Snippet),
				}}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// servesPost says whether a post's hit r may be served, given the post's
// index entry e in the snapshot the request read: the post renders as
// itself to every reader (not deleted, hidden, moved or redacted), and it
// is the revision, author and time the row indexed. A row that differs
// describes text the thread no longer shows, or filters on facts that are
// not the post's.
func servesPost(e threadlog.IndexEntry, r views.SearchRow) bool {
	return e.Flags.Visible() && int64(e.Revision) == r.Revision &&
		int64(e.Author) == r.AuthorID && e.CreatedMS == r.CreatedAt
}

// threadAt is thread id's projection row and forum as of log length at or
// later, for judge, which holds the thread open. A row that trails at is
// projected up to it and read again. ok is false when the row cannot be
// brought that far, by a failed projection, which is logged, or by a
// service with no Project: the caller then serves none of the thread's
// hits.
func (s *Service) threadAt(ctx context.Context, id threadid.ID, at int64) (dbq.Thread, dbq.Forum, bool, error) {
	var (
		row   dbq.Thread
		forum dbq.Forum
	)
	read := func() error {
		return s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
			var err error
			if row, err = q.GetThread(ctx, int64(id)); err != nil {
				return err
			}
			forum, err = q.GetForum(ctx, row.ForumID)
			return err
		})
	}
	err := read()
	if errors.Is(err, sql.ErrNoRows) {
		return row, forum, false, nil
	}
	if err != nil {
		return row, forum, false, err
	}
	if row.LogLengthProjected >= at {
		return row, forum, true, nil
	}
	if err := s.projectNow(ctx, id, at); err != nil {
		if ctx.Err() != nil {
			return row, forum, false, ctx.Err()
		}
		s.logger().Warn("search: a thread that trails its log could not be projected; its results are left out",
			"thread", id.String(), "at", at, "error", err)
		return row, forum, false, nil
	}
	if err := read(); err != nil {
		return row, forum, false, err
	}
	return row, forum, row.LogLengthProjected >= at, nil
}

// parseTerms reads q into the terms views.MatchAll quotes (docs/07
// "Search"). q is trimmed, and must then be 1 to SearchMaxRunes characters
// and hold at most SearchMaxWords words. Words are split on white space;
// a double-quoted run of words is one term, a phrase, and a quote left open
// ends its phrase at the end of q. Nothing else is syntax: every other
// character is part of a word, and FTS5's tokenizer decides what in it is
// searchable.
//
// A failure is validation-failed with a field error on q: required, too_long
// or too_many (docs/07 "Field errors on validation-failed").
func parseTerms(q string) ([]string, error) {
	q = strings.TrimSpace(q)
	switch n := utf8.RuneCountInString(q); {
	case n == 0:
		return nil, invalidQ(forms.CodeRequired)
	case n > SearchMaxRunes:
		return nil, invalidQ(forms.CodeTooLong)
	}
	var terms []string
	words := 0
	for i, part := range strings.Split(q, `"`) {
		fields := strings.FieldsFunc(part, unicode.IsSpace)
		words += len(fields)
		switch {
		case len(fields) == 0:
		case i%2 == 1: // inside quotes
			terms = append(terms, strings.Join(fields, " "))
		default:
			terms = append(terms, fields...)
		}
	}
	switch {
	case words > SearchMaxWords:
		return nil, invalidQ(forms.CodeTooMany)
	case len(terms) == 0:
		// Only quotes: nothing to search for.
		return nil, invalidQ(forms.CodeRequired)
	}
	return terms, nil
}

func invalidQ(code string) error {
	return &forms.Invalid{Kind: forms.ErrInvalidAnswers, Fields: []forms.FieldError{{Field: "q", Code: code}}}
}

// searchMillis reads after or before: absent, or a non-negative integer of
// Unix ms. Anything else is bad-request.
func searchMillis(name, text string) (*int64, error) {
	if text == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil || n < 0 {
		return nil, problem.Coded(problem.CodeBadRequest, fmt.Errorf("threads: search %s is %q; it is Unix ms", name, text))
	}
	return &n, nil
}

// A search cursor is opaque to clients (docs/07 "Search"): base64url,
// without padding, of the decimal offset into the statement's rows just
// past the last result a page showed. The statement's rows are the
// viewer's readable forums' alone, so an offset says nothing of any other.
// It is a position, not a credential: one written by hand reaches results
// its writer could search for anyway. The server issues offsets from 1 to
// SearchReach-1, and reads back exactly those, in exactly that spelling.
func encodeSearchCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

var errBadSearchCursor = problem.Coded(problem.CodeBadRequest, errors.New("threads: the search cursor is malformed"))

// decodeSearchCursor reads a search cursor. Empty is the first page, offset
// 0; anything encodeSearchCursor would not have written is bad-request.
func decodeSearchCursor(text string) (int, error) {
	if text == "" {
		return 0, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return 0, errBadSearchCursor
	}
	n, err := strconv.Atoi(string(b))
	if err != nil || n < 1 || n >= SearchReach || encodeSearchCursor(n) != text {
		return 0, errBadSearchCursor
	}
	return n, nil
}

// splitSnippet cuts FTS5's snippet text at render.SnippetOpen and
// render.SnippetClose into plain-text segments, each saying whether it
// matched, with neighbours alike merged and empty ones dropped. render.Plain
// strips both characters from every indexed post, and the indexer from
// every title, so only FTS5 places them. A marker out of place, an open
// inside a match or a close outside one, is dropped and never shown.
func splitSnippet(s string) []dto.SnippetPart {
	out := []dto.SnippetPart{}
	var b strings.Builder
	in := false
	flush := func() {
		if b.Len() == 0 {
			return
		}
		if n := len(out); n > 0 && out[n-1].Match == in {
			out[n-1].Text += b.String()
		} else {
			out = append(out, dto.SnippetPart{Text: b.String(), Match: in})
		}
		b.Reset()
	}
	for _, r := range s {
		switch r {
		case render.SnippetOpen:
			if !in {
				flush()
				in = true
			}
		case render.SnippetClose:
			if in {
				flush()
				in = false
			}
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return out
}
