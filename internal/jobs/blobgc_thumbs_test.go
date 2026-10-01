package jobs

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/store/blob"
)

// Tests for what the collector does about thumbnails (TKT-01M29CYS9W,
// docs/03 "Blob garbage collection"): a thumbnail goes with its blob, and
// one whose blob has no row goes on the next pass.

// thumbOf writes a thumbnail for hash, as the blob service would.
func (w *gcWorld) thumbOf(hash string) string {
	w.t.Helper()
	p, err := blob.ThumbPath(w.root, hash)
	if err != nil {
		w.t.Fatal(err)
	}
	w.aged(p, w.now)
	return p
}

// TestCollectorRemovesThumbnail: a collected blob's thumbnail goes with it,
// and a kept blob's stays.
func TestCollectorRemovesThumbnail(t *testing.T) {
	w := newGCWorld(t)
	w.now = t0.Add(30 * 24 * time.Hour)
	due := w.upload("due", t0)
	young := w.upload("young", w.now)
	dueThumb, youngThumb := w.thumbOf(due), w.thumbOf(young)

	res := w.mustRun()
	if res.Removed != 1 || w.kept(due) || !w.kept(young) {
		t.Fatalf("removed %d", res.Removed)
	}
	if exists(dueThumb) {
		t.Error("the collected blob's thumbnail is still there")
	}
	if !exists(youngThumb) {
		t.Error("the kept blob's thumbnail was removed")
	}
	// Removed with the blob, not by the orphan sweep.
	if n := attr(res, "orphan_thumbs"); n != nil {
		t.Errorf("orphan_thumbs = %v", n)
	}
}

// TestCollectorRemovesOrphanThumbnail: a thumbnail whose blob has no row is
// removed, whatever its age; one whose blob has a row stays, and so does a
// file whose name is not a hash's; and a held pass removes none.
func TestCollectorRemovesOrphanThumbnail(t *testing.T) {
	w := newGCWorld(t)
	withRow := w.upload("with a row", w.now)
	kept := w.thumbOf(withRow)
	orphan := w.thumbOf(hashOf("no row"))
	stray := filepath.Join(filepath.Dir(orphan), "notes.thumb")
	w.aged(stray, w.now)

	w.gc.Hold = func() string { return "a thread is quarantined" }
	if _, err := w.run(); err == nil {
		t.Fatal("the held pass succeeded")
	}
	if !exists(orphan) {
		t.Fatal("a held pass removed an orphan thumbnail")
	}
	w.gc.Hold = nil

	res := w.mustRun()
	if exists(orphan) {
		t.Error("the orphan thumbnail was kept")
	}
	if !exists(kept) || !exists(stray) {
		t.Errorf("removed too much: with a row %v, stray %v", exists(kept), exists(stray))
	}
	if !w.kept(withRow) {
		t.Error("the blob lost its row")
	}
	if n := attr(res, "orphan_thumbs"); n != int64(1) {
		t.Errorf("orphan_thumbs = %v, want 1", n)
	}
}
