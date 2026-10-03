package threads

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"slices"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/respcache"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// pagePosts is the page's posts through the response cache (TKT-01M29CCBY0).
//
// Only the posts are cached. The header and pagination are drawn from the
// projection row, the forum and the tags, which the request reads anyway for
// its access check and viewer block, so caching them would save a small
// marshal and pin a forum rename until the thread next moved. The viewer
// block is never cached: View builds it for each request.
//
// The key names everything the posts differ by (respcache.Key). What it
// cannot name before the posts are drawn, the handles their mentions resolved
// and the state of the posts they quote, is stored beside them and checked on
// every hit, as the render cache does, so a user registering a mentioned
// handle is seen at once. There is no invalidation: a mutation of the thread
// moves the log length, and so the key.
//
// A request with after_log_length is not cached: it is a filtered page, and
// rare.
func (s *Service) pagePosts(ctx context.Context, th *threadcache.Thread, id threadid.ID, snap snapshot,
	authors map[int64]dto.Author, q Query, size int, projected int64, a access, lensKey string) ([]dto.Post, error) {
	if q.HasAfter || !s.Responses.Enabled() {
		return s.posts(ctx, th, id, snap, authors, q, a)
	}
	// The class is read from the snapshot the posts are drawn from, so a
	// page that shows this viewer their own removed post is keyed to them
	// (TKT-01M29CKRJ9), and one that does not stays shared.
	class := pageClass(a, snap)
	digest, err := authorsDigest(authors)
	if err != nil {
		return nil, err
	}
	// The lens is keyed in its resolved form, and the page's ordinals beside
	// it, so two selections that land on the same page number never share
	// an entry, nor a lens page and the canonical one (TKT-01M29CRJ1P).
	key := respcache.Key{
		Thread: id, LogLength: snap.logLength, Projected: projected,
		Page: snap.page, PageSize: size, Lens: lensKey, Selection: ordinalsDigest(snap.ordinals),
		Renderer: render.Version, Class: class, Authors: digest,
	}
	resolver := postResolver(th)
	current := func(d render.Deps) bool {
		handles := s.handleMemo(ctx)
		// A failed lookup is not an answer: miss, and let the fresh draw
		// fail the page (review 392).
		return quotesCurrent(d.Quotes, resolver) && handles.current(d.Handles) && handles.err == nil
	}
	// Where moved posts went is drawn for this viewer after the posts are
	// taken or stored, so the cached posts never hold it: whether a viewer
	// may read another thread is not in the key (drawMoves).
	if body, ok := s.Responses.Get(key, current); ok {
		var out []dto.Post
		if err := json.Unmarshal(body, &out); err == nil && out != nil {
			return out, s.drawMoves(ctx, a, id, out)
		}
	}
	out, deps, err := s.postsDeps(ctx, th, id, snap, authors, q, a)
	if err != nil {
		return nil, err
	}
	if body, err := json.Marshal(out); err == nil {
		s.Responses.Put(key, body, deps)
	}
	return out, s.drawMoves(ctx, a, id, out)
}

// authorsDigest hashes the authors' rows as the posts draw them, in user id
// order. It hashes the JSON the posts carry, so a field added to dto.Author
// is covered without anyone remembering to add it here.
func authorsDigest(authors map[int64]dto.Author) ([32]byte, error) {
	type row struct {
		ID     int64      `json:"id"`
		Author dto.Author `json:"author"`
	}
	rows := make([]row, 0, len(authors))
	for id, a := range authors {
		rows = append(rows, row{id, a})
	}
	slices.SortFunc(rows, func(a, b row) int { return cmp.Compare(a.ID, b.ID) })
	b, err := json.Marshal(rows)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(b), nil
}

// ordinalsDigest hashes the page's ordinals, in order, for the cache key.
func ordinalsDigest(ordinals []uint32) [32]byte {
	b := make([]byte, 0, 4*len(ordinals))
	for _, o := range ordinals {
		b = binary.LittleEndian.AppendUint32(b, o)
	}
	return sha256.Sum256(b)
}
