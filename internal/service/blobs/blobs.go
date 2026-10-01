// Package blobs is the service behind attachments (docs/03 "Attachments",
// docs/07 "Attachments", TKT-01M29CXK0K): uploading a file, checking a
// post's attachments, and deciding who may read a blob.
//
// The rule it exists to keep is docs/14's: a blob reaches only a viewer who
// may read, at that moment, the body of a post naming it in its
// attachments. Authorize asks the thread service that question for each
// such post, so a blob and the post it is on can never disagree.
package blobs

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/terva-sh/ketju/internal/config"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/forms"
	"github.com/terva-sh/ketju/internal/fsutil"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/store/blob"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// MaxAttachments is the most blobs one post may attach (docs/07
// "Attachments").
const MaxAttachments = 10

// MaxName is the longest stored file name, in bytes of UTF-8.
const MaxName = 255

// refPage is how many of a blob's posts Authorize reads at a time.
const refPage = 100

// The codes a refused upload or attach carries in its validation-failed
// body (docs/07 "Attachments").
const (
	CodeTypeNotAllowed = "type_not_allowed"
	CodeBlocked        = "blocked"
	CodeNotYours       = "not_yours"
	CodeTooMany        = "too_many"
	CodeUnknown        = "unknown"
)

// Service uploads blobs and answers who may read them.
type Service struct {
	DB *db.DB
	// Root is the data directory; blobs/ is under it.
	Root    string
	Perm    *perm.Evaluator
	Threads *threads.Service
	// Max is the largest file an upload stores, in bytes:
	// limits.attachment_mb. Zero is config.DefaultAttachmentMB.
	Max int64
	// Now is the clock rows are stamped with; nil is time.Now.
	Now    func() time.Time
	Logger *slog.Logger

	// afterCommit, when set, runs inside the upload's transaction once the
	// file is in place, and its error fails the transaction; cleanup, when
	// set, replaces the second transaction that removes a file whose row
	// did not commit. Tests use them to fail the upload at its worst
	// moment.
	afterCommit func() error
	cleanup     func(ctx context.Context, hash string) error
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) max() int64 {
	if s.Max > 0 {
		return s.Max
	}
	return int64(config.DefaultAttachmentMB) << 20
}

func (s *Service) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// Target is where an upload is going: a thread, or a forum by slug, for a
// new thread. upload is granted per forum (docs/04 "permissions"), so an
// upload must name one.
type Target struct {
	Thread    threadid.ID
	HasThread bool
	Forum     string
}

// invalid is a validation-failed refusal naming one field and its code.
func invalid(field, code string) error {
	return &forms.Invalid{Kind: forms.ErrInvalidAnswers, Fields: []forms.FieldError{{Field: field, Code: code}}}
}

// MayUpload admits v to upload into target, or says why not, the way a post
// there is refused: a pre-user is forbidden and anyone else who is not a
// member unauthenticated; a forum hidden from v, or one v cannot read, is
// answered as the thread and forum endpoints answer it; then v needs upload
// there, and an archived forum takes none but an admin's, read from the
// forum's row so an archive refuses at once whatever perm's cache holds.
// The upload handler asks it before reading the body.
func (s *Service) MayUpload(ctx context.Context, v domain.Viewer, t Target) (forumID int64, err error) {
	if err := member.Require(v, "upload"); err != nil {
		return 0, err
	}
	var archived bool
	switch {
	case t.HasThread:
		forumID, archived, err = s.Threads.ForumOf(ctx, v, t.Thread)
	case t.Forum != "":
		forumID, archived, err = s.Threads.ForumBySlug(ctx, v, t.Forum)
	default:
		return 0, problem.Coded(problem.CodeBadRequest, errors.New("blobs: an upload names a thread or a forum"))
	}
	if err != nil {
		return 0, err
	}
	switch d, err := s.Perm.Check(ctx, v, forumID, domain.PermUpload); {
	case err != nil:
		return 0, err
	case d == perm.Hidden:
		return 0, problem.Coded(problem.CodeNotFound, errors.New("blobs: the forum is hidden from this viewer"))
	case d == perm.Deny:
		return 0, problem.Coded(problem.CodeForbidden, errors.New("blobs: this viewer may not upload here"))
	}
	if archived && v.Role != domain.RoleAdmin {
		return 0, problem.Coded(problem.CodeForbidden, errors.New("blobs: the forum is archived"))
	}
	return forumID, nil
}

// Upload stores the file r carries, named name, for v, who MayUpload has
// admitted, and answers it as docs/07 "Attachments" says: the same shape
// whether or not the bytes were known, with the name v first uploaded them
// under.
//
// The bytes are written to a staged file in blobs/tmp/ while they are read,
// and never more than Max+1 of them: more than Max is payload-too-large.
// The type is sniffed from the bytes, never taken from the name or the
// part's header. Every refusal and failure removes the staged file before
// it returns, the client going away mid-body included (docs/03).
//
// The file is moved into place and the row written in one database write,
// so no collector pass interleaves (docs/03 "Blob garbage collection"). If
// that write fails after the file was placed, a second write removes the
// file while no row names it; failing that, the collector's orphan sweep
// does. A file with no row is never served, since serving needs the row.
func (s *Service) Upload(ctx context.Context, v domain.Viewer, t Target, name string, r io.Reader) (dto.Attachment, error) {
	var out dto.Attachment
	if _, err := s.MayUpload(ctx, v, t); err != nil {
		return out, err
	}

	f, err := blob.Stage(s.Root)
	if err != nil {
		return out, err
	}
	staged := f.Name()
	committed := false
	defer func() {
		f.Close()
		if !committed {
			if rerr := os.Remove(staged); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
				s.log().Error("blobs: removing a staged upload", "path", staged, "err", rerr)
			}
		}
		blob.Unstage(staged)
	}()

	sum := sha256.New()
	max := s.max()
	limit := max
	if limit < math.MaxInt64 {
		limit++
	}
	n, err := io.Copy(io.MultiWriter(f, sum), io.LimitReader(r, limit))
	if err != nil {
		return out, fmt.Errorf("blobs: reading the upload: %w", err)
	}
	if n > max {
		return out, problem.Coded(problem.CodePayloadTooLarge, fmt.Errorf("blobs: the file is over %d bytes", max))
	}
	hash := hex.EncodeToString(sum.Sum(nil))

	mime, width, height, err := sniff(f)
	if err != nil {
		return out, err
	}
	name = cleanName(name)
	if err := f.Sync(); err != nil {
		return out, fmt.Errorf("blobs: syncing the upload: %w", err)
	}
	if err := f.Close(); err != nil {
		return out, fmt.Errorf("blobs: closing the upload: %w", err)
	}

	var (
		stored dbq.Blob
		named  string
		placed bool
	)
	now := s.now().UnixMilli()
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		b, err := q.GetBlob(ctx, hash)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			b = dbq.Blob{Hash: hash, Size: n, Mime: mime, Width: width, Height: height}
			if err := q.CreateUploadedBlob(ctx, dbq.CreateUploadedBlobParams{
				Hash: hash, Size: n, Mime: mime, OriginalName: name,
				UploadedBy: sql.NullInt64{Int64: v.UserID, Valid: true}, UploadedAt: now,
				Width: width, Height: height,
			}); err != nil {
				return err
			}
		case err != nil:
			return err
		case b.Status != "ok":
			return invalid("file", CodeBlocked)
		default:
			if err := q.RefreshBlob(ctx, dbq.RefreshBlobParams{At: now, Hash: hash}); err != nil {
				return err
			}
		}
		stored = b
		if err := q.InsertBlobUpload(ctx, dbq.InsertBlobUploadParams{
			Hash: hash, UserID: v.UserID, OriginalName: name, UploadedAt: now,
		}); err != nil {
			return err
		}
		if named, err = q.BlobUploadedBy(ctx, dbq.BlobUploadedByParams{Hash: hash, UserID: v.UserID}); err != nil {
			return err
		}
		placed, err = blob.Commit(staged, s.Root, hash)
		if err != nil {
			return err
		}
		// Placed or not, the staged file is gone: moved into place, or
		// removed beside an identical one.
		committed = true
		if s.afterCommit != nil {
			return s.afterCommit()
		}
		return nil
	})
	if err != nil {
		if placed {
			s.removeOrphan(hash)
		}
		return out, err
	}
	out = dto.Attachment{Hash: hash, Size: stored.Size, MIME: stored.Mime, OriginalName: named}
	if stored.Width.Valid && stored.Height.Valid {
		w, h := int(stored.Width.Int64), int(stored.Height.Int64)
		out.Width, out.Height = &w, &h
	}
	return out, nil
}

// removeOrphan removes the blob file an upload placed when its transaction
// did not commit, in a second write that first checks no row names it: an
// upload of the same bytes may have committed in between. A failure is
// logged; the collector removes the orphan once it is an hour old.
func (s *Service) removeOrphan(hash string) {
	// The request may be gone; the file is the server's to clean up.
	ctx := context.Background()
	remove := s.cleanup
	if remove == nil {
		remove = func(ctx context.Context, hash string) error { return RemoveOrphan(ctx, s.DB, s.Root, hash) }
	}
	if err := remove(ctx, hash); err != nil {
		s.log().Error("blobs: removing the file of an upload that did not commit; the collector removes it later",
			"hash", hash, "err", err)
	}
}

// RemoveOrphan removes the blob file for hash if no row names it, inside one
// database write, so no upload placing the same bytes interleaves. It
// reports whether it removed one. The upload path and the collector share
// it.
func RemoveOrphan(ctx context.Context, d *db.DB, root, hash string) error {
	_, err := RemoveIfOrphan(ctx, d, root, hash)
	return err
}

// RemoveIfOrphan is RemoveOrphan, reporting whether a file was removed.
func RemoveIfOrphan(ctx context.Context, d *db.DB, root, hash string) (bool, error) {
	path, err := blob.Path(root, hash)
	if err != nil {
		return false, err
	}
	var removed bool
	err = d.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		exists, err := q.BlobExists(ctx, hash)
		if err != nil || exists {
			return err
		}
		switch err := os.Remove(path); {
		case err == nil:
			removed = true
		case !errors.Is(err, os.ErrNotExist):
			return err
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	if removed {
		if err := fsutil.SyncDir(filepath.Dir(path)); err != nil {
			return true, err
		}
	}
	return removed, nil
}

// allowed maps a type http.DetectContentType names to the type stored, for
// the types docs/07 "Attachments" allows, with the image decoder each image
// must pass.
var allowed = map[string]struct {
	stored string
	decode func(io.Reader) (image.Config, error)
}{
	"image/png":                 {"image/png", png.DecodeConfig},
	"image/jpeg":                {"image/jpeg", jpeg.DecodeConfig},
	"image/gif":                 {"image/gif", gif.DecodeConfig},
	"application/pdf":           {"application/pdf", nil},
	"text/plain; charset=utf-8": {"text/plain; charset=utf-8", nil},
}

// sniff decides the staged file's type from its first 512 bytes, and an
// image's dimensions from its own decoder's DecodeConfig. A type off the
// allowlist, or an image its decoder refuses, is type_not_allowed.
func sniff(f *os.File) (mime string, width, height sql.NullInt64, err error) {
	head := make([]byte, 512)
	k, err := f.ReadAt(head, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", width, height, fmt.Errorf("blobs: reading the upload back: %w", err)
	}
	detected := http.DetectContentType(head[:k])
	a, ok := allowed[detected]
	if !ok {
		return "", width, height, invalid("file", CodeTypeNotAllowed)
	}
	if a.decode == nil {
		return a.stored, width, height, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", width, height, err
	}
	cfg, err := a.decode(f)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return "", width, height, invalid("file", CodeTypeNotAllowed)
	}
	return a.stored, sql.NullInt64{Int64: int64(cfg.Width), Valid: true},
		sql.NullInt64{Int64: int64(cfg.Height), Valid: true}, nil
}

// cleanName is the name a file is stored under: its base name only, with
// no control characters or path separators, invalid UTF-8 replaced, and at
// most MaxName bytes, cut on a character boundary; "file" when nothing is
// left.
func cleanName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.ToValidUTF8(name, "�")
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "." || name == ".." {
		name = ""
	}
	for len(name) > MaxName {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	if name == "" {
		return "file"
	}
	return name
}

// Check is threads.Attachments.Check: the hashes as the Post will record
// them, each once in the order sent, or why the attach is refused (docs/07
// "Attachments"). v needs upload in forumID, and each hash must be a blob v
// uploaded that is not quarantined.
func (s *Service) Check(ctx context.Context, v domain.Viewer, forumID int64, hashes []string) ([]string, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	var list []string
	for _, h := range hashes {
		if !blob.ValidHash(h) {
			return nil, invalid("attachments", CodeUnknown)
		}
		if !seen[h] {
			seen[h] = true
			list = append(list, h)
		}
	}
	if len(list) > MaxAttachments {
		return nil, invalid("attachments", CodeTooMany)
	}
	can, err := s.Perm.Can(ctx, v, forumID, domain.PermUpload)
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, problem.Coded(problem.CodeForbidden, errors.New("blobs: this viewer may not attach files here"))
	}
	if err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		return attachable(ctx, q, v.UserID, list)
	}); err != nil {
		return nil, err
	}
	return list, nil
}

// Recheck is threads.Attachments.Recheck: Check's rules for each blob,
// asked again through q, the caller's write transaction. A draft names
// its attachments without a reference, and the collector deletes a blob
// at zero that no draft names, so a draft saved after a pass deleted the
// blob would keep a hash with no bytes. Asked inside the write that saves
// the draft, the question is ordered against the collector's delete, which
// is a write too: either the blob is gone and the save is refused, or the
// draft is saved first and the delete keeps the blob (TKT-01M3V73DP6, Terva
// review 1749). Permission is not asked again; Check asked it on this
// request.
func (s *Service) Recheck(ctx context.Context, q *dbq.Queries, userID int64, hashes []string) error {
	if len(hashes) == 0 {
		return nil
	}
	return attachable(ctx, q, userID, hashes)
}

// attachable refuses the first of hashes, already distinct and valid, that
// userID may not attach: one with no blob, one they did not upload, or one
// quarantined.
func attachable(ctx context.Context, q *dbq.Queries, userID int64, hashes []string) error {
	rows, err := q.AttachCandidates(ctx, dbq.AttachCandidatesParams{UserID: userID, Hashes: hashes})
	if err != nil {
		return err
	}
	byHash := make(map[string]dbq.AttachCandidatesRow, len(rows))
	for _, r := range rows {
		byHash[r.Hash] = r
	}
	for _, h := range hashes {
		r, ok := byHash[h]
		switch {
		case !ok:
			return invalid("attachments", CodeUnknown)
		case !r.Uploaded:
			// Before the status: a blob v did not upload is not v's to
			// learn anything about.
			return invalid("attachments", CodeNotYours)
		case r.Status != "ok":
			return invalid("attachments", CodeBlocked)
		}
	}
	return nil
}

// AddRefs is threads.Attachments.AddRefs: each blob's refcount raised in one
// write, before the Post is appended (docs/03). A row that does not change
// is a blob collected since Check, and refuses the lot.
func (s *Service) AddRefs(ctx context.Context, hashes []string) error {
	return s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		for _, h := range hashes {
			n, err := q.AddBlobRef(ctx, h)
			if err != nil {
				return err
			}
			if n != 1 {
				return invalid("attachments", CodeUnknown)
			}
		}
		return nil
	})
}

// DropRefs is threads.Attachments.DropRefs: each blob's refcount lowered
// again, in one write.
func (s *Service) DropRefs(ctx context.Context, hashes []string) error {
	now := s.now().UnixMilli()
	return s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		for _, h := range hashes {
			if _, err := q.DropBlobRef(ctx, dbq.DropBlobRefParams{At: now, Hash: h}); err != nil {
				return err
			}
		}
		return nil
	})
}

// ErrNotFound is every refusal Authorize gives: a hash that is malformed,
// has no blob, is quarantined, or is on no post v may read. They are one
// error so that no answer tells them apart (docs/07 "Attachments").
var ErrNotFound = problem.Coded(problem.CodeNotFound, errors.New("blobs: no such blob"))

// Authorize answers the blob for hash if v may read it now, and ErrNotFound
// otherwise: v must be able to read the body of a post naming the hash in
// its attachments, by the thread page's rules (threads.CanSeePostBody). The
// posts are read a page at a time and each thread is asked about once per
// call for whether v is admitted to it.
func (s *Service) Authorize(ctx context.Context, v domain.Viewer, hash string) (dbq.Blob, error) {
	var b dbq.Blob
	if !blob.ValidHash(hash) {
		return b, ErrNotFound
	}
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		b, err = q.BlobForServing(ctx, hash)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	if err != nil {
		return b, err
	}
	afterThread, afterOrdinal := int64(math.MinInt64), int64(0)
	for {
		var refs []dbq.PostAttachmentRefsRow
		if err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
			var err error
			refs, err = q.PostAttachmentRefs(ctx, dbq.PostAttachmentRefsParams{
				Hash: hash, AfterThread: afterThread, AfterOrdinal: afterOrdinal, Max: refPage})
			return err
		}); err != nil {
			return b, err
		}
		// The rows come in (thread, ordinal) order, so each thread's posts
		// are one run, and v is admitted to each thread once per page.
		for i := 0; i < len(refs); {
			j := i
			var ordinals []uint32
			for ; j < len(refs) && refs[j].ThreadID == refs[i].ThreadID; j++ {
				if o := refs[j].Ordinal; o > 0 && o <= math.MaxUint32 {
					ordinals = append(ordinals, uint32(o))
				}
			}
			ok, err := s.Threads.CanSeeAnyPostBody(ctx, v, threadid.ID(refs[i].ThreadID), ordinals)
			if err != nil {
				return b, err
			}
			if ok {
				return b, nil
			}
			i = j
		}
		if len(refs) < refPage {
			return b, ErrNotFound
		}
		last := refs[len(refs)-1]
		afterThread, afterOrdinal = last.ThreadID, last.Ordinal
	}
}

// Path is where the blob for hash is stored.
func (s *Service) Path(hash string) (string, error) { return blob.Path(s.Root, hash) }

// compile-time check: the service is what posting needs.
var _ threads.Attachments = (*Service)(nil)
