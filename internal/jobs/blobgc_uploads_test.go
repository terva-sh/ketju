package jobs

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/blobs"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/store/blob"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for what the collector does about uploads (TKT-01M29CXK0K, docs/03
// "Blob garbage collection"): stale staged files, blob files no row names,
// and upload rows.

// hashOf is the hash of seed's bytes.
func hashOf(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

// aged writes a file at path, last modified at.
func (w *gcWorld) aged(path string, at time.Time) {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("bytes"), 0o640); err != nil {
		w.t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		w.t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// TestCollectorSweepsStaleStagedFiles: a staged upload two hours old is
// what a crash mid-upload leaves, and goes; one a minute old may be an
// upload in progress, and stays.
func TestCollectorSweepsStaleStagedFiles(t *testing.T) {
	w := newGCWorld(t)
	stale := filepath.Join(blob.TmpDir(w.root), "upload-stale")
	fresh := filepath.Join(blob.TmpDir(w.root), "upload-fresh")
	w.aged(stale, w.now.Add(-2*time.Hour))
	w.aged(fresh, w.now.Add(-time.Minute))
	w.mustRun()
	if exists(stale) || !exists(fresh) {
		t.Fatalf("stale left: %v, fresh kept: %v", exists(stale), exists(fresh))
	}
}

// TestCollectorRemovesOrphanBlobFiles: an hour-old blob file no row and no
// log names is removed. A fresh one is kept, since its upload may still be
// committing; so is one whose row exists, one a log names, and anything
// that is not named by a hash.
func TestCollectorRemovesOrphanBlobFiles(t *testing.T) {
	w := newGCWorld(t)
	old := w.now.Add(-2 * time.Hour)
	path := func(seed string) (string, string) {
		h := hashOf(seed)
		p, err := blob.Path(w.root, h)
		if err != nil {
			t.Fatal(err)
		}
		return h, p
	}
	_, orphan := path("orphan")
	_, fresh := path("fresh")
	withRow := w.upload("with a row", w.now)
	_, withRowPath := path("with a row")
	named, namedPath := path("named by a log")
	// A thumbnail is not a blob file, and one whose blob has a row stays.
	thumb := withRowPath
	w.aged(orphan, old)
	w.aged(fresh, w.now.Add(-time.Minute))
	w.aged(namedPath, old)
	w.aged(thumb+".thumb", old)
	if err := os.Chtimes(withRowPath, old, old); err != nil {
		t.Fatal(err)
	}
	w.thread("", record{threadlog.TypePost, threadlog.EncodingCBOR,
		&threadlog.PostBody{Source: "x", Attachments: []string{named}}})

	res := w.mustRun()
	if exists(orphan) {
		t.Error("the orphan was kept")
	}
	if !exists(fresh) || !exists(withRowPath) || !exists(namedPath) || !exists(thumb+".thumb") {
		t.Errorf("removed too much: fresh %v, with a row %v, named %v, thumb %v",
			exists(fresh), exists(withRowPath), exists(namedPath), exists(thumb+".thumb"))
	}
	if !w.kept(withRow) {
		t.Error("the blob with a row lost it")
	}
	found := false
	for i := 0; i+1 < len(res.Attrs); i += 2 {
		if res.Attrs[i] == "orphans" && res.Attrs[i+1] == int64(1) {
			found = true
		}
	}
	if !found {
		t.Errorf("the result does not count one orphan: %v", res.Attrs)
	}

	// A held pass removes no orphan, and no stale staged file either
	// (Terva review 1732).
	w.aged(orphan, old)
	stale := filepath.Join(blob.TmpDir(w.root), "upload-held")
	if err := os.MkdirAll(blob.TmpDir(w.root), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.aged(stale, w.now.Add(-2*time.Hour))
	w.gc.Hold = func() string { return "a thread is quarantined" }
	if _, err := w.run(); err == nil {
		t.Fatal("the held pass succeeded")
	}
	if !exists(orphan) {
		t.Error("a held pass removed an orphan")
	}
	if !exists(stale) {
		t.Error("a held pass removed a stale staged file")
	}
}

// TestCollectorRemovesBlobUploads: a collected blob's upload rows go with
// its row, in the same transaction, by the foreign key's cascade.
func TestCollectorRemovesBlobUploads(t *testing.T) {
	w := newGCWorld(t)
	gone := w.upload("collected", w.now.Add(-8*24*time.Hour))
	kept := w.upload("referenced", w.now.Add(-8*24*time.Hour))
	w.addRef(kept)
	var user int64
	w.write(func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		if user, err = q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "alice", DisplayName: "Alice",
			Status: "active", Role: domain.RoleMember, Kind: "human", CreatedAt: 1, UpdatedAt: 1}); err != nil {
			return err
		}
		for _, h := range []string{gone, kept} {
			if err := q.InsertBlobUpload(t.Context(), dbq.InsertBlobUploadParams{Hash: h, UserID: user,
				OriginalName: "f", UploadedAt: 1}); err != nil {
				return err
			}
		}
		return nil
	})
	if res := w.mustRun(); res.Removed != 1 {
		t.Fatalf("removed %d", res.Removed)
	}
	count := func(h string) (n int) {
		w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM blob_uploads WHERE hash = ?`, h).Scan(&n)
		})
		return n
	}
	if count(gone) != 0 || count(kept) != 1 {
		t.Fatalf("upload rows: collected %d, kept %d", count(gone), count(kept))
	}
}

// TestUploadRacesCollector: an upload of a blob's bytes while a pass is
// deciding to collect it is ordered before or after the delete, never
// between, so its file is present exactly when its row is.
func TestUploadRacesCollector(t *testing.T) {
	w := newGCWorld(t)
	p := perm.New(w.db)
	var members int64
	w.write(func(q *dbq.Queries, _ *sql.Tx) error {
		if _, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "alice", DisplayName: "Alice",
			Status: "active", Role: domain.RoleMember, Kind: "human", CreatedAt: 1, UpdatedAt: 1}); err != nil {
			return err
		}
		if _, err := q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "general", Name: "General",
			Visibility: domain.VisibilityPublic, CreatedAt: 1, UpdatedAt: 1}); err != nil {
			return err
		}
		g, err := q.GetGroupBySlug(t.Context(), domain.GroupMembers)
		members = g.ID
		return err
	})
	if err := p.Grant(t.Context(), audit.By{}, 0, members, domain.PermUpload); err != nil {
		t.Fatal(err)
	}
	var clock time.Time
	svc := &blobs.Service{DB: w.db, Root: w.root, Perm: p, Threads: &threads.Service{DB: w.db, Perm: p},
		Now: func() time.Time { return clock }}
	var alice int64
	w.db.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUserByHandle(t.Context(), "alice")
		alice = u.ID
		return err
	})
	v := domain.Viewer{UserID: alice, Role: domain.RoleMember, Active: true}
	upload := func(b []byte) string {
		t.Helper()
		a, err := svc.Upload(t.Context(), v, blobs.Target{Forum: "general"}, "f.txt", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		return a.Hash
	}

	// Uploaded again between the scan and the delete: the upload restarted
	// the seven days, so the delete finds the blob no longer collectable.
	clock = w.now.Add(-8 * 24 * time.Hour)
	before := upload([]byte("uploaded again mid-pass"))
	w.gc.afterScan = func() {
		clock = w.now
		upload([]byte("uploaded again mid-pass"))
	}
	if res := w.mustRun(); res.Removed != 0 {
		t.Fatalf("removed %d", res.Removed)
	}
	if !w.kept(before) {
		t.Fatal("the re-uploaded blob was collected")
	}

	// Uploaded again after the pass deleted it: the upload puts row and
	// file back together.
	w.gc.afterScan = nil
	clock = w.now.Add(-8 * 24 * time.Hour)
	after := upload([]byte("uploaded again after"))
	if res := w.mustRun(); res.Removed != 1 {
		t.Fatalf("removed %d", res.Removed)
	}
	if w.kept(after) {
		t.Fatal("not collected")
	}
	clock = w.now
	upload([]byte("uploaded again after"))
	if !w.kept(after) {
		t.Fatal("the upload after the pass left no blob")
	}
}
