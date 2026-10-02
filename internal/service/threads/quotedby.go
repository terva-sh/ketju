package threads

import (
	"context"
	"database/sql"
	"slices"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// quotedByChunk bounds how many ordinals one QuotedBy statement names, well
// under SQLite's limit on bound parameters.
const quotedByChunk = 500

// quotedBy is each of posts' quote backlinks: the ordinals of the posts that
// quote it, ascending (docs/06 "Post anatomy", TKT-01M3YME9AZ). A post
// served without HTML is not asked about, since it carries none.
//
// The rows come from the post_quotes projection, and visibleQuoting decides
// which quoting posts are listed. The response cache keys the projection's
// length (respcache.Key.Projected) and the log's, so a page drawn while the
// projection lagged is not served once it has caught up, and a page drawn
// before a quoting post was hidden is not served after.
func (s *Service) quotedBy(ctx context.Context, th *threadcache.Thread, id threadid.ID, snap snapshot,
	posts []threadlog.Post) (map[uint32][]int, error) {
	var asked []int64
	for _, p := range posts {
		if !p.Tombstone {
			asked = append(asked, int64(p.Ordinal))
		}
	}
	if len(asked) == 0 {
		return nil, nil
	}
	var rows []dbq.QuotedByRow
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		for part := range slices.Chunk(asked, quotedByChunk) {
			got, err := q.QuotedBy(ctx, dbq.QuotedByParams{ThreadID: int64(id), Quoted: part})
			if err != nil {
				return err
			}
			rows = append(rows, got...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return visibleQuoting(th.Index, snap.n, rows), nil
}

// visibleQuoting keeps the quoting posts every reader is shown as themselves,
// grouped by the post they quote. post_meta has already dropped the deleted
// and moved ones it knows of (QuotedBy); the index is ahead of it and also
// knows hidden, redacted and moved posts, so a quoting post is kept only
// while its entry is Visible(). That is what keeps a backlink from saying
// what a removed or moved post quoted, to moderators too, and what lets every
// viewer class share the list (docs/06). A quoting post past n, the post
// count the page was drawn at, is not listed: the page does not have it yet.
func visibleQuoting(ix *threadlog.Index, n int, rows []dbq.QuotedByRow) map[uint32][]int {
	out := map[uint32][]int{}
	for _, r := range rows {
		quoting := uint32(r.Ordinal)
		if r.Ordinal <= 0 || int64(r.Ordinal) > int64(n) {
			continue
		}
		e, err := ix.Entry(quoting)
		if err != nil || !e.Flags.Visible() {
			continue
		}
		out[uint32(r.QuotedOrdinal)] = append(out[uint32(r.QuotedOrdinal)], int(r.Ordinal))
	}
	for o := range out {
		slices.Sort(out[o])
		out[o] = slices.Compact(out[o])
	}
	return out
}
