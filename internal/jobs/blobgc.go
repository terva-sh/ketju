package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/terva-sh/ketju/internal/store/blob"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// BlobGCInterval is how often serve runs the blob garbage collector, and
// BlobGrace how long a blob must have sat at zero references before it may
// go (docs/02 "Background work", docs/04 "blobs").
//
// The grace is the safety margin. It covers an upload whose post has not
// been written yet, since an upload starts at zero references, and a
// reference that is dropped and then restored.
const (
	BlobGCInterval = 24 * time.Hour
	BlobGrace      = 7 * 24 * time.Hour
)

// maxBlobCandidates bounds one pass. Whatever is left is collected by the
// next one, a day later; nothing is lost by the wait.
const maxBlobCandidates = 10_000

// ErrHeld means a blob GC pass deleted nothing because it could not be sure
// what the thread logs reference. It is reported as a failure, so an
// operator sees it, and the next pass tries again.
var ErrHeld = errors.New("blob gc held")

// BlobGC deletes blobs nothing refers to (docs/03 "Attachments"). It is
// deliberately conservative, because what it deletes has no other copy.
// A blob is deleted only when, at the moment of the delete,
//
//   - its refcount is zero and has been since before the grace period, and
//     no user's avatar and no draft names it (DeleteCollectableBlob checks
//     all of this in the statement that deletes, so a reference added after
//     the candidates were read wins); and
//   - its hash appears in no record of any thread log, soft-deleted posts
//     and superseded revisions included, whatever the refcount says. The
//     logs are the source of truth and the refcount is bookkeeping about
//     them, so a count that has drifted cannot free a blob a post names.
//
// A pass that cannot read every log cleanly deletes nothing, and neither
// does one run while Hold names a reason.
//
// The row delete and the file removal happen inside one write transaction,
// so they are serialised with every other database writer; the upload path
// is expected to write a blob's file and row the same way (docs/03).
type BlobGC struct {
	DB *db.DB

	// Root is the data directory: blobs/ and threads/ are under it.
	Root string

	// Now is the clock; nil means time.Now.
	Now func() time.Time

	// Hold, when set, is asked before each pass. A non-empty answer is a
	// reason the logs cannot be trusted to be complete right now, such as a
	// thread the projection has quarantined, and the pass deletes nothing.
	Hold func() string

	// afterScan runs between the log scan and the deletes. Tests use it to
	// commit a reference at the worst moment.
	afterScan func()
}

func (g *BlobGC) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

// Job is the collector as a scheduler job, run daily.
func (g *BlobGC) Job() Job {
	return Job{Name: "blob gc", Every: BlobGCInterval, Run: g.Run}
}

// Run is one pass. Its Result counts the blobs removed, and its attributes
// say how many candidates there were and why the rest were kept.
func (g *BlobGC) Run(ctx context.Context) (Result, error) {
	cutoff := g.now().Add(-BlobGrace).UnixMilli()
	if g.Hold != nil {
		if reason := g.Hold(); reason != "" {
			return Result{}, fmt.Errorf("%w: %s; nothing was deleted", ErrHeld, reason)
		}
	}

	var candidates []string
	if err := g.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		candidates, err = q.BlobGCCandidates(ctx, dbq.BlobGCCandidatesParams{Cutoff: cutoff, Max: maxBlobCandidates})
		return err
	}); err != nil {
		return Result{}, fmt.Errorf("blob gc: reading candidates: %w", err)
	}
	if len(candidates) == 0 {
		return Result{Attrs: []any{"candidates", 0}}, nil
	}

	want := make(map[string]bool, len(candidates))
	for _, h := range candidates {
		want[h] = true
	}
	inLogs, err := referencedInLogs(ctx, g.Root, want)
	if err != nil {
		return Result{Attrs: []any{"candidates", len(candidates)}},
			fmt.Errorf("%w: the thread logs could not all be read, so nothing was deleted: %w", ErrHeld, err)
	}
	if g.afterScan != nil {
		g.afterScan()
	}

	var (
		removed, changed, failed int64
		errs                     []error
	)
	for _, h := range candidates {
		if inLogs[h] {
			continue
		}
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		gone, err := g.collect(ctx, h, cutoff)
		switch {
		case err != nil:
			failed++
			errs = append(errs, err)
		case gone:
			removed++
		default:
			changed++
		}
	}
	return Result{Removed: removed, Attrs: []any{"candidates", len(candidates),
		"kept_referenced_in_log", len(inLogs), "kept_changed", changed, "failed", failed}}, errors.Join(errs...)
}

// collect deletes one blob's row, if it is still collectable, and its file,
// in one write transaction. A file that cannot be removed rolls the row
// back, so a row never outlives its file by the collector's hand. A missing
// file is not an error: it is what a pass whose commit failed after the
// removal leaves for the next one.
func (g *BlobGC) collect(ctx context.Context, hash string, cutoff int64) (bool, error) {
	path, err := blob.Path(g.Root, hash)
	if err != nil {
		return false, fmt.Errorf("blob gc: %q: %w", hash, err)
	}
	var gone bool
	err = g.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		n, err := q.DeleteCollectableBlob(ctx, dbq.DeleteCollectableBlobParams{Hash: hash, Cutoff: cutoff})
		if err != nil || n == 0 {
			return err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		gone = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("blob gc: %s: %w", hash, err)
	}
	return gone, nil
}

// referencedInLogs reports which of want appear in any record of any thread
// log under root. Every file named *.log under threads/ is read, whatever
// shard it sits in, so a log someone moved still counts. Any frame that
// cannot be read, or any body in an encoding this build cannot search, is
// an error: the caller cannot then know what the logs reference.
func referencedInLogs(ctx context.Context, root string, want map[string]bool) (map[string]bool, error) {
	dir := filepath.Join(root, threadid.ThreadsDir)
	if _, err := os.Stat(dir); err != nil {
		return nil, err
	}
	found := map[string]bool{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(p) != threadid.LogExt {
			return nil
		}
		return scanLog(p, want, found)
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

// scanLog adds to found every hash in want that one log mentions.
func scanLog(path string, want, found map[string]bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	var bad error
	err = threadlog.ScanFrames(f, st.Size(), func(r threadlog.ScanRecord) bool {
		switch {
		case r.Err != nil:
			bad = fmt.Errorf("%s: the frame at %d: %w", path, r.Offset, r.Err)
			return false
		case r.Header.Encoding != threadlog.EncodingCBOR && r.Header.Encoding != threadlog.EncodingJSON:
			// A body this build cannot search might name a blob in a form
			// the search below would miss.
			bad = fmt.Errorf("%s: the frame at %d has body encoding %d", path, r.Offset, r.Header.Encoding)
			return false
		}
		hexMentions(r.Body, want, found)
		return true
	})
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return bad
}

// hexMentions adds to found every hash in want that appears in b as
// sixty-four hex digits, in either case, anywhere, including inside a
// longer run of hex. Both body encodings store strings as their bytes, so
// an attachment names its blob this way whatever shape the attachment
// element takes. A false match only keeps a blob.
func hexMentions(b []byte, want, found map[string]bool) {
	var win [blob.HashLen]byte
	for i := 0; i < len(b); {
		if !isHex(b[i]) {
			i++
			continue
		}
		j := i
		for j < len(b) && isHex(b[j]) {
			j++
		}
		for k := i; k+blob.HashLen <= j; k++ {
			for x := range blob.HashLen {
				c := b[k+x]
				if 'A' <= c && c <= 'F' {
					c += 'a' - 'A'
				}
				win[x] = c
			}
			if want[string(win[:])] {
				found[string(win[:])] = true
			}
		}
		i = j
	}
}

func isHex(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}
