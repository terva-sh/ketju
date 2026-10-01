package blobs_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/terva-sh/ketju/internal/service/blobs"
	"github.com/terva-sh/ketju/internal/service/blobs/blobstest"
	"github.com/terva-sh/ketju/internal/store/blob"
)

// Tests for making thumbnails (TKT-01M29CYS9W). They call the maker below
// Authorize, which the HTTP tests in internal/web/api cover with every row
// of the blob access table.

// thumbRoot writes n PNG blobs under a fresh data directory and returns it
// with their hashes.
func thumbRoot(t *testing.T, n int) (string, []string) {
	t.Helper()
	root := t.TempDir()
	var hashes []string
	for i := range n {
		b := blobstest.PNG(300, 200, byte(i))
		sum := sha256.Sum256(b)
		h := hex.EncodeToString(sum[:])
		p, err := blob.Path(root, h)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o640); err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, h)
	}
	return root, hashes
}

func thumbExists(t *testing.T, root, hash string) bool {
	t.Helper()
	p, err := blob.ThumbPath(root, hash)
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(p)
	return err == nil
}

// pacedMaker is a Thumbnailer that spends a second of the bubble's clock
// inside the real one, and counts its calls and how many were inside at
// once.
type pacedMaker struct {
	calls, inside, most atomic.Int32
	fail                atomic.Pointer[error]
}

func (m *pacedMaker) make(f io.ReadSeeker, mime string) ([]byte, string, error) {
	m.calls.Add(1)
	n := m.inside.Add(1)
	for {
		most := m.most.Load()
		if n <= most || m.most.CompareAndSwap(most, n) {
			break
		}
	}
	defer m.inside.Add(-1)
	time.Sleep(time.Second)
	if err := m.fail.Load(); err != nil {
		return nil, "", *err
	}
	return blob.Thumbnail(f, mime)
}

func (m *pacedMaker) failWith(err error) { m.fail.Store(&err) }

// TestThumbnailMadeOnceAndCached: two readers of a new image make one
// decode, the second waiting for the first; a later reader decodes
// nothing; and a thumbnail that has gone is made again.
func TestThumbnailMadeOnceAndCached(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root, hashes := thumbRoot(t, 1)
		h := hashes[0]
		m := &pacedMaker{}
		svc := &blobs.Service{Root: root, Thumbnailer: m.make}
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range 2 {
			wg.Go(func() { errs[i] = blobs.MakeThumb(svc, t.Context(), h, "image/png") })
		}
		synctest.Wait()
		if got := m.inside.Load(); got != 1 {
			t.Fatalf("%d decodes inside at once", got)
		}
		wg.Wait()
		if errs[0] != nil || errs[1] != nil {
			t.Fatalf("errors: %v", errs)
		}
		if got := m.calls.Load(); got != 1 {
			t.Fatalf("two readers made %d decodes", got)
		}
		if !thumbExists(t, root, h) {
			t.Fatal("no thumbnail on disk")
		}
		// Made: nothing decodes it again.
		if err := blobs.MakeThumb(svc, t.Context(), h, "image/png"); err != nil || m.calls.Load() != 1 {
			t.Fatalf("a third reader: %v, %d decodes", err, m.calls.Load())
		}
		// Gone: made again on the next request.
		p, _ := blob.ThumbPath(root, h)
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
		if err := blobs.MakeThumb(svc, t.Context(), h, "image/png"); err != nil || m.calls.Load() != 2 {
			t.Fatalf("after removal: %v, %d decodes", err, m.calls.Load())
		}
		if !thumbExists(t, root, h) {
			t.Fatal("not made again")
		}
	})
}

// TestThumbnailsDecodeOneAtATime: three requests for three new images never
// overlap inside the decoder, so memory is bounded by one decode's budget.
func TestThumbnailsDecodeOneAtATime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root, hashes := thumbRoot(t, 3)
		m := &pacedMaker{}
		svc := &blobs.Service{Root: root, Thumbnailer: m.make}
		var wg sync.WaitGroup
		start := time.Now()
		for _, h := range hashes {
			wg.Go(func() {
				if err := blobs.MakeThumb(svc, t.Context(), h, "image/png"); err != nil {
					t.Error(err)
				}
			})
		}
		synctest.Wait()
		if got := m.inside.Load(); got != 1 {
			t.Fatalf("%d decodes running at once", got)
		}
		wg.Wait()
		if m.most.Load() != 1 || m.calls.Load() != 3 {
			t.Fatalf("%d calls, at most %d at once", m.calls.Load(), m.most.Load())
		}
		if took := time.Since(start); took != 3*time.Second {
			t.Fatalf("three one-second decodes took %v", took)
		}
		for _, h := range hashes {
			if !thumbExists(t, root, h) {
				t.Errorf("%s has no thumbnail", h)
			}
		}
	})
}

// TestThumbnailWaiterTakesOverFromCancelledMaker: when the request making a
// thumbnail goes away while it waits for the semaphore, a request waiting
// on it makes the thumbnail instead of failing with someone else's
// cancellation.
func TestThumbnailWaiterTakesOverFromCancelledMaker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root, hashes := thumbRoot(t, 2)
		busy, h := hashes[0], hashes[1]
		m := &pacedMaker{}
		svc := &blobs.Service{Root: root, Thumbnailer: m.make}
		var wg sync.WaitGroup
		// busy holds the semaphore for a second.
		wg.Go(func() { blobs.MakeThumb(svc, t.Context(), busy, "image/png") })
		synctest.Wait()
		ctx, cancel := context.WithCancel(t.Context())
		var first, second error
		wg.Go(func() { first = blobs.MakeThumb(svc, ctx, h, "image/png") })
		synctest.Wait()
		wg.Go(func() { second = blobs.MakeThumb(svc, t.Context(), h, "image/png") })
		synctest.Wait()
		cancel()
		wg.Wait()
		if !errors.Is(first, context.Canceled) {
			t.Errorf("the cancelled maker: %v", first)
		}
		if second != nil || !thumbExists(t, root, h) {
			t.Fatalf("the waiter: %v, made %v", second, thumbExists(t, root, h))
		}
		if m.calls.Load() != 2 {
			t.Fatalf("%d decodes", m.calls.Load())
		}
	})
}

// TestThumbnailFailureRemembered: an image that fails to decode answers
// not-found, and is not decoded again while it is in the failure list;
// other errors, such as a full disk, are not remembered.
func TestThumbnailFailureRemembered(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root, hashes := thumbRoot(t, 2)
		m := &pacedMaker{}
		m.failWith(fmt.Errorf("%w: a test", blob.ErrDecode))
		svc := &blobs.Service{Root: root, Thumbnailer: m.make}
		// Two readers at once wait for the one decode and share its failure.
		var wg sync.WaitGroup
		for range 2 {
			wg.Go(func() {
				if err := blobs.MakeThumb(svc, t.Context(), hashes[0], "image/png"); !errors.Is(err, blobs.ErrNotFound) {
					t.Errorf("a failed decode: %v", err)
				}
			})
		}
		wg.Wait()
		for range 2 {
			if err := blobs.MakeThumb(svc, t.Context(), hashes[0], "image/png"); !errors.Is(err, blobs.ErrNotFound) {
				t.Fatalf("a failed decode, again: %v", err)
			}
		}
		if m.calls.Load() != 1 {
			t.Fatalf("%d decodes of one bad image", m.calls.Load())
		}
		m.failWith(errors.New("disk full"))
		for range 2 {
			if err := blobs.MakeThumb(svc, t.Context(), hashes[1], "image/png"); err == nil || errors.Is(err, blobs.ErrNotFound) {
				t.Fatalf("an I/O failure: %v", err)
			}
		}
		if m.calls.Load() != 3 {
			t.Fatalf("an I/O failure was remembered: %d decodes", m.calls.Load())
		}
		if thumbExists(t, root, hashes[0]) || thumbExists(t, root, hashes[1]) {
			t.Fatal("a failure left a thumbnail")
		}
	})
}

// TestFailedSetForgetsOldest: the failure list holds FailedThumbs hashes and
// forgets the least recently seen first.
func TestFailedSetForgetsOldest(t *testing.T) {
	l := blobs.NewFailedSet(blobs.FailedThumbs)
	for i := range blobs.FailedThumbs {
		l.Add(fmt.Sprint(i))
	}
	l.Has("0") // seen, so 1 is now the oldest
	l.Add("new")
	if !l.Has("0") || l.Has("1") || !l.Has("new") || !l.Has(fmt.Sprint(blobs.FailedThumbs-1)) {
		t.Fatal("the wrong entry was forgotten")
	}
}
