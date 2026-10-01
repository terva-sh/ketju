package blobs

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sync"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/blob"
)

// Thumbnails (docs/03 "Attachments", docs/07 "Attachments", TKT-01M29CYS9W).
//
// A thumbnail is a cache beside its blob, made on the first request that
// needs it and made again whenever it is missing. Making one decodes bytes
// a member uploaded, so:
//   - one decode runs at a time in the process, which bounds memory to one
//     blob.MaxDecodeBytes;
//   - two readers of a new image wait for one decode, not two;
//   - an image that failed is remembered, best effort, in memory, so a
//     reload does not pay for the decode again while it stays in the list.

// FailedThumbs is how many hashes whose thumbnail failed are remembered.
// One evicted by newer failures may be decoded once more, bounded like any
// other decode (Terva review 1712).
const FailedThumbs = 4096

// thumbs is the process's thumbnail state: the one-decode semaphore, the
// decodes in flight by hash, and the failures.
type thumbs struct {
	sem chan struct{}

	mu       sync.Mutex
	inflight map[string]*thumbCall
	failed   *lru
}

// thumbCall is one decode that other readers of the same hash wait for.
type thumbCall struct {
	done chan struct{}
	err  error
}

func (s *Service) thumbState() *thumbs {
	s.thumbsOnce.Do(func() {
		s.thumbs = &thumbs{sem: make(chan struct{}, 1), inflight: map[string]*thumbCall{}, failed: newLRU(FailedThumbs)}
	})
	return s.thumbs
}

// thumbnail is the maker, blob.Thumbnail unless a test replaced it.
func (s *Service) thumbnail(f io.ReadSeeker, mime string) ([]byte, string, error) {
	if s.Thumbnailer != nil {
		return s.Thumbnailer(f, mime)
	}
	return blob.Thumbnail(f, mime)
}

// Thumb opens the thumbnail of the blob for hash for v, making it if it is
// missing, and returns it with its type. The caller closes it.
//
// v is admitted by Authorize, exactly as for the blob, so a thumbnail
// reaches only the viewers its blob reaches (docs/14). Everything else is
// ErrNotFound: a refusal, a blob of a type with no thumbnail, and an image
// that could not be thumbnailed, whose blob is still served.
func (s *Service) Thumb(ctx context.Context, v domain.Viewer, hash string) (*os.File, string, error) {
	b, err := s.Authorize(ctx, v, hash)
	if err != nil {
		return nil, "", err
	}
	ctype, ok := blob.ThumbType(b.Mime)
	if !ok {
		return nil, "", ErrNotFound
	}
	path, err := blob.ThumbPath(s.Root, hash)
	if err != nil {
		return nil, "", ErrNotFound
	}
	// A thumbnail removed between being made and being opened, by the
	// collector or a person, is made again; twice is enough for any race
	// that is not a fault.
	for range 2 {
		f, err := os.Open(path)
		if err == nil {
			return f, ctype, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, "", err
		}
		if err := s.makeThumb(ctx, hash, b.Mime); err != nil {
			return nil, "", err
		}
	}
	return nil, "", fmt.Errorf("blobs: the thumbnail of %s was removed as it was made", hash)
}

// errThumbFailed is a thumbnail that could not be made from its blob's
// bytes: too large, or not decodable. It answers not-found.
var errThumbFailed = fmt.Errorf("%w: no thumbnail", ErrNotFound)

// makeThumb makes the thumbnail for hash unless it is there already, has
// failed before, or another request is making it, in which case it waits
// for that one.
func (s *Service) makeThumb(ctx context.Context, hash, mime string) error {
	st := s.thumbState()
	for {
		st.mu.Lock()
		if st.failed.has(hash) {
			st.mu.Unlock()
			return errThumbFailed
		}
		if c, ok := st.inflight[hash]; ok {
			st.mu.Unlock()
			select {
			case <-c.done:
			case <-ctx.Done():
				return ctx.Err()
			}
			// The maker's request went away while it waited for its turn;
			// this one has not, so it takes the decode on.
			if isCtxErr(c.err) && ctx.Err() == nil {
				continue
			}
			return c.err
		}
		c := &thumbCall{done: make(chan struct{})}
		st.inflight[hash] = c
		st.mu.Unlock()

		c.err = s.generate(ctx, st, hash, mime)

		st.mu.Lock()
		delete(st.inflight, hash)
		st.mu.Unlock()
		close(c.done)
		return c.err
	}
}

func isCtxErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// generate waits for the decode semaphore and makes the thumbnail.
func (s *Service) generate(ctx context.Context, st *thumbs, hash, mime string) error {
	select {
	case st.sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-st.sem }()

	path, err := blob.ThumbPath(s.Root, hash)
	if err != nil {
		return ErrNotFound
	}
	// A maker that finished just before this one became the maker has
	// already written it.
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	src, err := s.Path(hash)
	if err != nil {
		return ErrNotFound
	}
	f, err := os.Open(src)
	if errors.Is(err, fs.ErrNotExist) {
		// As for the blob: a row with no file, which an operator must hear
		// about, and to the reader a thumbnail that is not there.
		s.log().ErrorContext(ctx, "blobs: a blob's row has no file", "hash", hash)
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	data, _, err := s.thumbnail(f, mime)
	f.Close()
	if errors.Is(err, blob.ErrTooLarge) || errors.Is(err, blob.ErrDecode) || errors.Is(err, blob.ErrNotImage) {
		st.mu.Lock()
		st.failed.add(hash)
		st.mu.Unlock()
		// The hash and the reason, never the bytes.
		s.log().WarnContext(ctx, "blobs: no thumbnail for an image; its blob is still served", "hash", hash, "err", err)
		return errThumbFailed
	}
	if err != nil {
		return err
	}
	return s.writeThumb(path, data)
}

// writeThumb puts data at path through a staged file and a rename, so no
// reader ever opens half a thumbnail. Nothing is synced: a thumbnail lost
// to a power cut is made again.
func (s *Service) writeThumb(path string, data []byte) error {
	f, err := blob.Stage(s.Root)
	if err != nil {
		return err
	}
	staged := f.Name()
	defer blob.Unstage(staged)
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(staged, path)
	}
	if err != nil {
		if rerr := os.Remove(staged); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			s.log().Error("blobs: removing a staged thumbnail", "path", staged, "err", rerr)
		}
		return fmt.Errorf("blobs: writing a thumbnail: %w", err)
	}
	return nil
}

// lru is a set of at most n strings that forgets the least recently added
// or seen first. Its caller locks it.
type lru struct {
	n     int
	order *list.List
	at    map[string]*list.Element
}

func newLRU(n int) *lru { return &lru{n: n, order: list.New(), at: map[string]*list.Element{}} }

func (l *lru) has(k string) bool {
	e, ok := l.at[k]
	if ok {
		l.order.MoveToFront(e)
	}
	return ok
}

func (l *lru) add(k string) {
	if e, ok := l.at[k]; ok {
		l.order.MoveToFront(e)
		return
	}
	l.at[k] = l.order.PushFront(k)
	for l.order.Len() > l.n {
		last := l.order.Back()
		l.order.Remove(last)
		delete(l.at, last.Value.(string))
	}
}
