package api

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/service/blobs/blobstest"
	"github.com/terva-sh/ketju/internal/store/blob"
)

// Tests for serving thumbnails over HTTP (TKT-01M29CYS9W, docs/07
// "Attachments"). Who is served one is TestThumbnailServedOnlyToReaders and
// TestRevalidationRechecksAccess, beside the blob's.

// countThumbs makes the world's blob service count the thumbnails it makes.
func (bw *blobWorld) countThumbs() *atomic.Int32 {
	n := new(atomic.Int32)
	bw.blobs.Thumbnailer = func(f io.ReadSeeker, mime string) ([]byte, string, error) {
		n.Add(1)
		return blob.Thumbnail(f, mime)
	}
	return n
}

// TestThumbnailHeaders: a thumbnail carries the blob's headers, its own type
// and "<hash>.thumb" as its ETag, and is the image fitted inside 160 px.
func TestThumbnailHeaders(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	p := bw.uploaded("carol", "a.png", blobstest.PNG(640, 320, 60))
	j := bw.uploaded("carol", "b.jpg", blobstest.JPEG(320, 640, 60))
	g := bw.uploaded("carol", "c.gif", blobstest.GIF(100, 50, 60))
	created[dto.CreatedPost](t, bw.replyWith("carol", id, p.Hash, j.Hash, g.Hash))

	for _, c := range []struct {
		hash, ctype string
		w, h        int
	}{
		{p.Hash, "image/png", 160, 80},
		{j.Hash, "image/jpeg", 80, 160},
		{g.Hash, "image/png", 100, 50},
	} {
		rec := bw.fetch("alice", c.hash+"/thumb")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", c.ctype, rec.Code, rec.Body)
		}
		for k, want := range map[string]string{
			"Content-Type":                 c.ctype,
			"Cache-Control":                "private, no-cache",
			"Vary":                         "Cookie",
			"ETag":                         `"` + c.hash + `.thumb"`,
			"X-Content-Type-Options":       "nosniff",
			"Content-Security-Policy":      "default-src 'none'; sandbox",
			"Cross-Origin-Resource-Policy": "same-origin",
			"Content-Disposition":          "inline",
		} {
			if got := rec.Header().Get(k); got != want {
				t.Errorf("%s %s: %q, want %q", c.ctype, k, got, want)
			}
		}
		var img image.Image
		var err error
		if c.ctype == "image/jpeg" {
			img, err = jpeg.Decode(rec.Body)
		} else {
			img, err = png.Decode(rec.Body)
		}
		if err != nil {
			t.Fatalf("%s: %v", c.ctype, err)
		}
		if b := img.Bounds(); b.Dx() != c.w || b.Dy() != c.h {
			t.Errorf("%s: %v, want %dx%d", c.ctype, b, c.w, c.h)
		}
		if rec := bw.fetch("alice", c.hash+"/thumb", "If-None-Match", `"`+c.hash+`.thumb"`); rec.Code != http.StatusNotModified {
			t.Errorf("%s: If-None-Match: %d", c.ctype, rec.Code)
		}
		// The blob's own ETag is not the thumbnail's.
		if rec := bw.fetch("alice", c.hash+"/thumb", "If-None-Match", `"`+c.hash+`"`); rec.Code != http.StatusOK {
			t.Errorf("%s: the blob's ETag: %d", c.ctype, rec.Code)
		}
	}
	r := httptest.NewRequest(http.MethodHead, "/blobs/"+p.Hash+"/thumb", nil)
	r.Header.Set("X-Test-Viewer", "alice")
	head := httptest.NewRecorder()
	bw.h.ServeHTTP(head, r)
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Type") != "image/png" {
		t.Errorf("HEAD: %d, %d bytes", head.Code, head.Body.Len())
	}
}

// TestThumbnailCachedOverHTTP: the first request makes the thumbnail, later
// ones serve the file, and one removed is made again.
func TestThumbnailCachedOverHTTP(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	made := bw.countThumbs()
	a := bw.uploaded("carol", "a.png", blobstest.PNG(400, 300, 61))
	created[dto.CreatedPost](t, bw.replyWith("carol", id, a.Hash))
	first := bw.fetch("alice", a.Hash+"/thumb")
	second := bw.fetch("carol", a.Hash+"/thumb")
	if first.Code != http.StatusOK || second.Code != http.StatusOK || !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Fatalf("%d then %d", first.Code, second.Code)
	}
	if n := made.Load(); n != 1 {
		t.Fatalf("two requests made %d thumbnails", n)
	}
	path, err := blob.ThumbPath(bw.data, a.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if rec := bw.fetch("alice", a.Hash+"/thumb"); rec.Code != http.StatusOK || made.Load() != 2 {
		t.Fatalf("after removal: %d, %d made", rec.Code, made.Load())
	}
	if staged := bw.staged(); len(staged) != 0 {
		t.Fatalf("staged files left: %v", staged)
	}
}

// TestThumbnailDecodeFailureLeavesBlobServable: a PNG whose header is valid
// and whose pixels are cut short uploads, since the upload reads only the
// header. Its thumbnail is not-found, as for a hash nothing has; the blob
// is still served and the post still carries its attachment; and a second
// request for the thumbnail decodes nothing.
func TestThumbnailDecodeFailureLeavesBlobServable(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	made := bw.countThumbs()
	whole := blobstest.PNG(200, 200, 62)
	cut := whole[:len(whole)-16]
	a := bw.uploaded("carol", "cut.png", cut)
	p := created[dto.CreatedPost](t, bw.replyWith("carol", id, a.Hash))

	missing := blobAnswer(bw.fetch("alice", zeroHash+"/thumb"))
	if got := blobAnswer(bw.fetch("alice", a.Hash+"/thumb")); got != missing {
		t.Errorf("the failed thumbnail differs from a missing one:\n%s\n---\n%s", got, missing)
	}
	if rec := bw.fetch("alice", a.Hash); rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), cut) {
		t.Fatalf("the blob: %d", rec.Code)
	}
	if got := bw.pageAttachments("alice", id, int(p.Ordinal)); len(got) != 1 || got[0].Hash != a.Hash {
		t.Fatalf("the post's attachments: %+v", got)
	}
	if rec := bw.fetch("alice", a.Hash+"/thumb"); rec.Code != http.StatusNotFound {
		t.Fatalf("again: %d", rec.Code)
	}
	if n := made.Load(); n != 1 {
		t.Fatalf("%d decodes of one bad image", n)
	}
	path, err := blob.ThumbPath(bw.data, a.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a failure left a thumbnail: %v", err)
	}
}

// TestThumbnailNotForNonImages: a PDF and a text file have no thumbnail,
// answered as a hash nothing has, and their blobs are still served.
func TestThumbnailNotForNonImages(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	made := bw.countThumbs()
	pdf := bw.uploaded("carol", "a.pdf", blobstest.PDF(63))
	txt := bw.uploaded("carol", "a.txt", blobstest.Text(63))
	created[dto.CreatedPost](t, bw.replyWith("carol", id, pdf.Hash, txt.Hash))
	missing := blobAnswer(bw.fetch("alice", zeroHash+"/thumb"))
	for _, h := range []string{pdf.Hash, txt.Hash} {
		if got := blobAnswer(bw.fetch("alice", h+"/thumb")); got != missing {
			t.Errorf("%s:\n%s", h, got)
		}
		if rec := bw.fetch("alice", h); rec.Code != http.StatusOK {
			t.Errorf("the blob: %d", rec.Code)
		}
	}
	if n := made.Load(); n != 0 {
		t.Fatalf("%d thumbnails made of non-images", n)
	}
}

// zeroHash is a well-formed hash nothing has.
const zeroHash = "0000000000000000000000000000000000000000000000000000000000000000"
