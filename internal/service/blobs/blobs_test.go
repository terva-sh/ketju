package blobs_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/forms"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/blobs"
	"github.com/terva-sh/ketju/internal/service/blobs/blobstest"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/store/blob"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
)

// store is a blob service over a fresh database with two members, alice
// and carol, and one public forum, general, where members may upload.
type store struct {
	t     *testing.T
	db    *db.DB
	svc   *blobs.Service
	root  string
	users map[string]int64
}

const maxBytes = 4096

func newStore(t *testing.T) *store {
	t.Helper()
	d := dbtest.Open(t, db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	p := perm.New(d)
	s := &store{t: t, db: d, root: t.TempDir(), users: map[string]int64{}}
	s.svc = &blobs.Service{DB: d, Root: s.root, Perm: p, Threads: &threads.Service{DB: d, Perm: p}, Max: maxBytes,
		Now: func() time.Time { return time.UnixMilli(1_790_000_000_000) }}
	var forum int64
	s.write(func(q *dbq.Queries) error {
		for _, h := range []string{"alice", "carol"} {
			id, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: h, DisplayName: h,
				Status: "active", Role: domain.RoleMember, Kind: "human", CreatedAt: 1, UpdatedAt: 1})
			if err != nil {
				return err
			}
			s.users[h] = id
		}
		var err error
		forum, err = q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "general", Name: "General",
			Visibility: domain.VisibilityPublic, CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	var members int64
	s.read(func(q *dbq.Queries) error {
		g, err := q.GetGroupBySlug(t.Context(), domain.GroupMembers)
		members = g.ID
		return err
	})
	if err := p.Grant(t.Context(), audit.By{}, forum, members, domain.PermUpload); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *store) write(fn func(q *dbq.Queries) error) {
	s.t.Helper()
	if err := s.db.Write(s.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error { return fn(q) }); err != nil {
		s.t.Fatal(err)
	}
}

func (s *store) read(fn func(q *dbq.Queries) error) {
	s.t.Helper()
	if err := s.db.Read(s.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error { return fn(q) }); err != nil {
		s.t.Fatal(err)
	}
}

func (s *store) viewer(h string) domain.Viewer {
	return domain.Viewer{UserID: s.users[h], Role: domain.RoleMember, Active: true, SessionKey: "s-" + h}
}

var general = blobs.Target{Forum: "general"}

func (s *store) upload(who, name string, b []byte) (string, error) {
	s.t.Helper()
	a, err := s.svc.Upload(s.t.Context(), s.viewer(who), general, name, bytes.NewReader(b))
	return a.Hash, err
}

// staged lists what is in blobs/tmp/.
func (s *store) staged() []string {
	s.t.Helper()
	entries, err := os.ReadDir(blob.TmpDir(s.root))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		s.t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// files lists the blob files on disk, by name.
func (s *store) files() []string {
	s.t.Helper()
	paths, err := filepath.Glob(filepath.Join(s.root, blob.Dir, blob.Algorithm, "*", "*"))
	if err != nil {
		s.t.Fatal(err)
	}
	var out []string
	for _, p := range paths {
		out = append(out, filepath.Base(p))
	}
	return out
}

func (s *store) count(query string, args ...any) int {
	s.t.Helper()
	var n int
	if err := s.db.Read(s.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(s.t.Context(), query, args...).Scan(&n)
	}); err != nil {
		s.t.Fatal(err)
	}
	return n
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// fieldCode is the one field error err carries, as "field code".
func fieldCode(err error) string {
	var inv *forms.Invalid
	if !errors.As(err, &inv) || len(inv.Fields) != 1 {
		return ""
	}
	return inv.Fields[0].Field + " " + inv.Fields[0].Code
}

// TestUploadSniffsNotClaims: the stored type is what the bytes are, never
// what the name says: a PNG named notes.txt is image/png with its size,
// and HTML named x.png is refused.
func TestUploadSniffsNotClaims(t *testing.T) {
	s := newStore(t)
	img := blobstest.PNG(3, 2, 1)
	a, err := s.svc.Upload(t.Context(), s.viewer("alice"), general, "notes.txt", bytes.NewReader(img))
	if err != nil {
		t.Fatal(err)
	}
	if a.MIME != "image/png" || a.Hash != sum(img) || a.Size != int64(len(img)) || a.OriginalName != "notes.txt" ||
		a.Width == nil || *a.Width != 3 || a.Height == nil || *a.Height != 2 {
		t.Fatalf("answer = %+v", a)
	}
	_, err = s.upload("alice", "x.png", blobstest.Refused["html"])
	if got := fieldCode(err); got != "file type_not_allowed" || problem.FromError(err) != problem.CodeValidationFailed {
		t.Fatalf("HTML named x.png: %v (%q)", err, got)
	}
	for name, b := range map[string][]byte{"a.jpg": blobstest.JPEG(4, 5, 1), "a.gif": blobstest.GIF(6, 7, 1),
		"a.pdf": blobstest.PDF(1), "a.txt": blobstest.Text(1)} {
		a, err := s.svc.Upload(t.Context(), s.viewer("alice"), general, name, bytes.NewReader(b))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		image := strings.HasPrefix(a.MIME, "image/")
		if image != (a.Width != nil) || image != (a.Height != nil) {
			t.Errorf("%s: %s with width %v height %v", name, a.MIME, a.Width, a.Height)
		}
	}
}

// TestUploadRefusesOutsideAllowlist: SVG, HTML, WebP, a ZIP and an ELF
// binary are each type_not_allowed, store nothing and leave nothing staged.
func TestUploadRefusesOutsideAllowlist(t *testing.T) {
	s := newStore(t)
	for kind, b := range blobstest.Refused {
		_, err := s.upload("alice", "file."+kind, b)
		if got := fieldCode(err); got != "file type_not_allowed" {
			t.Errorf("%s: %v", kind, err)
		}
	}
	if n := s.count(`SELECT count(*) FROM blobs`); n != 0 || len(s.files()) != 0 || len(s.staged()) != 0 {
		t.Fatalf("refusals stored something: %d rows, files %v, staged %v", n, s.files(), s.staged())
	}
}

// TestUploadImageMustDecodeConfig: an image's magic followed by garbage is
// refused, for each image type: the sniff alone is not enough.
func TestUploadImageMustDecodeConfig(t *testing.T) {
	s := newStore(t)
	garbage := bytes.Repeat([]byte{0xde, 0xad}, 64)
	for name, magic := range map[string][]byte{
		"png": []byte("\x89PNG\r\n\x1a\n"), "jpeg": {0xff, 0xd8, 0xff}, "gif": []byte("GIF89a"),
	} {
		_, err := s.upload("alice", "x."+name, append(slices.Clone(magic), garbage...))
		if got := fieldCode(err); got != "file type_not_allowed" {
			t.Errorf("%s magic and garbage: %v", name, err)
		}
	}
	if len(s.staged()) != 0 || len(s.files()) != 0 {
		t.Fatalf("staged %v, files %v", s.staged(), s.files())
	}
}

// TestUploadDedupOneFileOneRow: the same bytes from two members are one
// file and one blobs row, at refcount 0, with an upload row for each.
func TestUploadDedupOneFileOneRow(t *testing.T) {
	s := newStore(t)
	img := blobstest.PNG(2, 2, 7)
	for _, who := range []string{"alice", "carol"} {
		if _, err := s.upload(who, who+".png", img); err != nil {
			t.Fatal(err)
		}
	}
	if files := s.files(); !slices.Equal(files, []string{sum(img)}) {
		t.Fatalf("files = %v", files)
	}
	if n := s.count(`SELECT count(*) FROM blobs WHERE hash = ? AND refcount = 0 AND unreferenced_since IS NOT NULL`, sum(img)); n != 1 {
		t.Fatalf("%d blobs rows at refcount 0", n)
	}
	if n := s.count(`SELECT count(*) FROM blob_uploads WHERE hash = ?`, sum(img)); n != 2 {
		t.Fatalf("%d upload rows, want 2", n)
	}
}

// TestUploadAnswerSameForKnownBytes: bytes another member sent first are
// answered in the same shape as new ones, with the name this viewer sent,
// so an upload says nothing about who else has the file.
func TestUploadAnswerSameForKnownBytes(t *testing.T) {
	s := newStore(t)
	img := blobstest.PNG(5, 4, 3)
	first, err := s.svc.Upload(t.Context(), s.viewer("alice"), general, "holiday.png", bytes.NewReader(img))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.svc.Upload(t.Context(), s.viewer("carol"), general, "mine.png", bytes.NewReader(img))
	if err != nil {
		t.Fatal(err)
	}
	if second.OriginalName != "mine.png" {
		t.Fatalf("carol's answer names %q", second.OriginalName)
	}
	second.OriginalName = first.OriginalName
	if *first.Width != *second.Width || *first.Height != *second.Height {
		t.Fatalf("dimensions differ: %+v %+v", first, second)
	}
	first.Width, first.Height, second.Width, second.Height = nil, nil, nil, nil
	if first != second {
		t.Fatalf("answers differ: %+v then %+v", first, second)
	}
}

// TestReuploadKeepsFirstName: a member's second upload of the same bytes
// under another name answers with the first name, and changes no row.
func TestReuploadKeepsFirstName(t *testing.T) {
	s := newStore(t)
	img := blobstest.PNG(2, 3, 9)
	if _, err := s.upload("alice", "a.png", img); err != nil {
		t.Fatal(err)
	}
	a, err := s.svc.Upload(t.Context(), s.viewer("alice"), general, "b.png", bytes.NewReader(img))
	if err != nil {
		t.Fatal(err)
	}
	if a.OriginalName != "a.png" {
		t.Fatalf("the second upload answers %q, want a.png", a.OriginalName)
	}
	if n := s.count(`SELECT count(*) FROM blob_uploads WHERE original_name = 'a.png'`); n != 1 {
		t.Fatal("the upload row was renamed")
	}
}

// TestUploadOfQuarantinedBytesBlocked: bytes identical to a quarantined
// blob are refused as blocked, for anyone, and the staged file goes.
func TestUploadOfQuarantinedBytesBlocked(t *testing.T) {
	s := newStore(t)
	img := blobstest.PNG(2, 2, 4)
	h, err := s.upload("alice", "a.png", img)
	if err != nil {
		t.Fatal(err)
	}
	s.write(func(q *dbq.Queries) error {
		_, err := q.SetBlobStatus(t.Context(), dbq.SetBlobStatusParams{Status: "quarantined", Hash: h})
		return err
	})
	for _, who := range []string{"alice", "carol"} {
		if _, err := s.upload(who, "again.png", img); fieldCode(err) != "file blocked" {
			t.Errorf("%s: %v", who, err)
		}
	}
	if n := s.count(`SELECT count(*) FROM blob_uploads WHERE user_id = ?`, s.users["carol"]); n != 0 || len(s.staged()) != 0 {
		t.Fatalf("a blocked upload left %d rows and staged %v", n, s.staged())
	}
}

// TestUploadNameSanitized: a stored name is a base name with no control
// characters or separators, at most 255 bytes cut on a character, and
// "file" when nothing is left.
func TestUploadNameSanitized(t *testing.T) {
	long := strings.Repeat("é", 200) // 400 bytes
	for in, want := range map[string]string{
		"../../etc/passwd":    "passwd",
		`C:\Users\x\shot.png`: "shot.png",
		"a\x00b\nc\td.png":    "abcd.png",
		"  spaced.png  ":      "spaced.png",
		"":                    "file",
		"..":                  "file",
		"/":                   "file",
		"\x7f\x01":            "file",
		"bad\xffutf8.txt":     "bad\uFFFDutf8.txt",
		long:                  strings.Repeat("é", 127),
	} {
		got := blobs.CleanName(in)
		if got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
		if len(got) > blobs.MaxName {
			t.Errorf("CleanName(%q) is %d bytes", in, len(got))
		}
	}
	// And through an upload.
	s := newStore(t)
	a, err := s.svc.Upload(t.Context(), s.viewer("alice"), general, "../x/\x00shot.png", bytes.NewReader(blobstest.PNG(1, 1, 1)))
	if err != nil || a.OriginalName != "shot.png" {
		t.Fatalf("%+v %v", a, err)
	}
}

// failing reads n bytes of b and then fails, as a client that goes away
// mid-body does.
type failing struct {
	r io.Reader
}

func (f *failing) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if errors.Is(err, io.EOF) {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}

// TestRefusedUploadLeavesNoStagedFile: whatever refuses or fails an upload,
// blobs/tmp/ is empty when it returns, and no blob file or row is left.
func TestRefusedUploadLeavesNoStagedFile(t *testing.T) {
	quarantined := blobstest.PNG(2, 2, 66)
	cases := []struct {
		name  string
		body  io.Reader
		setup func(s *store)
		want  func(err error) bool
	}{
		{"over the cap", bytes.NewReader(make([]byte, maxBytes+1)), nil,
			func(err error) bool { return problem.FromError(err) == problem.CodePayloadTooLarge }},
		{"off the allowlist", bytes.NewReader(blobstest.Refused["svg"]), nil,
			func(err error) bool { return fieldCode(err) == "file type_not_allowed" }},
		{"a failed DecodeConfig", bytes.NewReader([]byte("\x89PNG\r\n\x1a\nnot really")), nil,
			func(err error) bool { return fieldCode(err) == "file type_not_allowed" }},
		{"quarantined bytes", bytes.NewReader(quarantined), func(s *store) {
			h, err := s.upload("carol", "q.png", quarantined)
			if err != nil {
				t.Fatal(err)
			}
			s.write(func(q *dbq.Queries) error {
				_, err := q.SetBlobStatus(t.Context(), dbq.SetBlobStatusParams{Status: "quarantined", Hash: h})
				return err
			})
		}, func(err error) bool { return fieldCode(err) == "file blocked" }},
		{"a failed database write", bytes.NewReader(blobstest.PNG(2, 2, 67)), func(s *store) {
			blobs.SetAfterCommit(s.svc, func() error { return errors.New("injected") })
		}, func(err error) bool { return err != nil && strings.Contains(err.Error(), "injected") }},
		{"a client that went away", &failing{r: bytes.NewReader(blobstest.PNG(2, 2, 68))}, nil,
			func(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newStore(t)
			if c.setup != nil {
				c.setup(s)
			}
			before := s.files()
			_, err := s.svc.Upload(t.Context(), s.viewer("alice"), general, "f", c.body)
			if !c.want(err) {
				t.Fatalf("err = %v", err)
			}
			if staged := s.staged(); len(staged) != 0 {
				t.Fatalf("staged files left: %v", staged)
			}
			if after := s.files(); !slices.Equal(after, before) {
				t.Fatalf("blob files went from %v to %v", before, after)
			}
			if n := s.count(`SELECT count(*) FROM blob_uploads WHERE user_id = ?`, s.users["alice"]); n != 0 {
				t.Fatalf("%d upload rows for alice", n)
			}
		})
	}
}

// TestUploadCommitFailureLeavesNoOrphan: a transaction that fails after the
// file was placed leaves no file and no row. When the cleanup write fails
// too, the file is left with no row, and the next orphan removal, which the
// collector runs, takes it.
func TestUploadCommitFailureLeavesNoOrphan(t *testing.T) {
	s := newStore(t)
	img := blobstest.PNG(3, 3, 5)
	blobs.SetAfterCommit(s.svc, func() error { return errors.New("injected") })
	if _, err := s.upload("alice", "a.png", img); err == nil {
		t.Fatal("the upload succeeded")
	}
	if files, n := s.files(), s.count(`SELECT count(*) FROM blobs`); len(files) != 0 || n != 0 {
		t.Fatalf("left files %v and %d rows", files, n)
	}

	blobs.SetCleanup(s.svc, func(context.Context, string) error { return errors.New("cleanup injected") })
	if _, err := s.upload("alice", "a.png", img); err == nil {
		t.Fatal("the upload succeeded")
	}
	if files := s.files(); !slices.Equal(files, []string{sum(img)}) {
		t.Fatalf("files = %v, want the orphan", files)
	}
	if _, err := s.svc.Authorize(t.Context(), s.viewer("alice"), sum(img)); !errors.Is(err, blobs.ErrNotFound) {
		t.Fatalf("the orphan is served: %v", err)
	}
	removed, err := blobs.RemoveIfOrphan(t.Context(), s.db, s.root, sum(img))
	if err != nil || !removed || len(s.files()) != 0 {
		t.Fatalf("removed %t, %v, files %v", removed, err, s.files())
	}

	// A file whose row exists is never an orphan.
	blobs.SetAfterCommit(s.svc, nil)
	if _, err := s.upload("alice", "a.png", img); err != nil {
		t.Fatal(err)
	}
	if removed, err := blobs.RemoveIfOrphan(t.Context(), s.db, s.root, sum(img)); err != nil || removed {
		t.Fatalf("a file with a row was removed: %t %v", removed, err)
	}
}

// TestUploadCapIsExact: a file of exactly Max bytes is stored; one more is
// refused.
func TestUploadCapIsExact(t *testing.T) {
	s := newStore(t)
	text := bytes.Repeat([]byte("a"), maxBytes)
	if _, err := s.upload("alice", "max.txt", text); err != nil {
		t.Fatalf("a file of exactly the cap: %v", err)
	}
	if _, err := s.upload("alice", "over.txt", append(text, 'b')); problem.FromError(err) != problem.CodePayloadTooLarge {
		t.Fatalf("one byte over: %v", err)
	}
}

// TestSweepSparesAPausedUpload is Terva review 1735: a client that pauses
// mid-body for longer than the collector's hour keeps its staged file, and
// the upload succeeds when the body resumes. Once the upload is done, an
// old staged file of the same name is swept as before.
func TestSweepSparesAPausedUpload(t *testing.T) {
	s := newStore(t)
	img := blobstest.PNG(3, 3, 81)
	pr, pw := io.Pipe()
	type result struct {
		hash string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		a, err := s.svc.Upload(t.Context(), s.viewer("alice"), general, "slow.png", pr)
		done <- result{a.Hash, err}
	}()
	if _, err := pw.Write(img[:8]); err != nil {
		t.Fatal(err)
	}
	staged := s.staged()
	if len(staged) != 1 {
		t.Fatalf("staged = %v, want the paused upload's file", staged)
	}
	path := filepath.Join(blob.TmpDir(s.root), staged[0])
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if n, err := blob.SweepTmp(s.root, time.Hour, time.Now()); err != nil || n != 0 {
		t.Fatalf("SweepTmp = %d, %v; want the paused upload's file kept", n, err)
	}
	if _, err := pw.Write(img[8:]); err != nil {
		t.Fatal(err)
	}
	pw.Close()
	r := <-done
	if r.err != nil || r.hash != sum(img) {
		t.Fatalf("the resumed upload = %q, %v", r.hash, r.err)
	}

	// Finished, the upload holds nothing: a stale file left under the same
	// name is swept.
	if err := os.WriteFile(path, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if n, err := blob.SweepTmp(s.root, time.Hour, time.Now()); err != nil || n != 1 {
		t.Fatalf("SweepTmp = %d, %v; want the stale file removed", n, err)
	}
}
