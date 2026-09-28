package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
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

// blobPage is how many candidates a pass takes at a time. Each page costs
// one read of every thread log, so a pass over many candidates reads the
// logs once per page. It is a variable so a test can make pages small.
var blobPage int64 = 10_000

// trashSuffix names a blob file the collector has moved aside in the
// transaction that deletes its row. The move is undone if that transaction
// does not commit, and finished once it has; a pass that finds one left
// over, after a crash or a failed restore, settles it by whether the row is
// still there (recoverTrash). No upload writes a name with this suffix.
const trashSuffix = ".gc"

// ErrHeld means a blob GC pass deleted nothing because it could not be sure
// what the thread logs reference. It is reported as a failure, so an
// operator sees it, and the next pass tries again.
var ErrHeld = errors.New("blob gc held")

// BlobGC deletes blobs nothing refers to (docs/03 "Blob garbage
// collection"). It is deliberately conservative, because what it deletes
// has no other copy. A blob is deleted only when, at the moment of the
// delete,
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
// The bytes are never gone while the row remains. The row delete and a
// rename of the file to a trash name happen inside one write transaction,
// so they are serialised with every other database writer, the upload path
// included (docs/03). The rename is undone if the transaction does not
// commit, and the trash file removed once it has.
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

	// afterScan runs between each page's log scan and its deletes. Tests use
	// it to commit a reference at the worst moment.
	afterScan func()

	// rename is os.Rename; tests replace it to fail the restore.
	rename func(from, to string) error
}

func (g *BlobGC) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

func (g *BlobGC) mv(from, to string) error {
	if g.rename != nil {
		return g.rename(from, to)
	}
	return os.Rename(from, to)
}

// Job is the collector as a scheduler job, run daily.
func (g *BlobGC) Job() Job {
	return Job{Name: "blob gc", Every: BlobGCInterval, Run: g.Run}
}

// Run is one pass. Its Result counts the blobs removed, and its attributes
// say how many candidates there were and why the rest were kept.
func (g *BlobGC) Run(ctx context.Context) (Result, error) {
	cutoff := g.now().Add(-BlobGrace).UnixMilli()
	restored, err := g.recoverTrash(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("blob gc: settling an interrupted removal: %w", err)
	}
	if g.Hold != nil {
		if reason := g.Hold(); reason != "" {
			return Result{}, fmt.Errorf("%w: %s; nothing was deleted", ErrHeld, reason)
		}
	}

	var (
		candidates, inLog, removed, changed, failed int64
		errs                                        []error
		afterSince                                  int64 = math.MinInt64
		afterHash                                   string
	)
	result := func() Result {
		attrs := []any{"candidates", candidates, "kept_referenced_in_log", inLog, "kept_changed", changed,
			"failed", failed}
		if restored > 0 {
			attrs = append(attrs, "restored", restored)
		}
		return Result{Removed: removed, Attrs: attrs}
	}
	for {
		var page []dbq.BlobGCCandidatesRow
		if err := g.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
			var err error
			page, err = q.BlobGCCandidates(ctx, dbq.BlobGCCandidatesParams{Cutoff: cutoff,
				AfterSince: afterSince, AfterHash: afterHash, Max: blobPage})
			return err
		}); err != nil {
			return result(), errors.Join(append(errs, fmt.Errorf("blob gc: reading candidates: %w", err))...)
		}
		if len(page) == 0 {
			return result(), errors.Join(errs...)
		}
		candidates += int64(len(page))
		last := page[len(page)-1]
		afterSince, afterHash = last.UnreferencedSince, last.Hash

		want := make(map[string]bool, len(page))
		for _, c := range page {
			want[c.Hash] = true
		}
		found, err := referencedInLogs(ctx, g.Root, want)
		if err != nil {
			return result(), errors.Join(append(errs,
				fmt.Errorf("%w: the thread logs could not all be read, so nothing more was deleted: %w", ErrHeld, err))...)
		}
		inLog += int64(len(found))
		if g.afterScan != nil {
			g.afterScan()
		}
		for _, c := range page {
			if found[c.Hash] {
				continue
			}
			if err := ctx.Err(); err != nil {
				return result(), errors.Join(append(errs, err)...)
			}
			gone, err := g.collect(ctx, c.Hash, cutoff)
			switch {
			case gone:
				removed++
			case err == nil:
				changed++
			default:
				failed++
			}
			if err != nil {
				errs = append(errs, err)
			}
		}
		if int64(len(page)) < blobPage {
			return result(), errors.Join(errs...)
		}
	}
}

// collect deletes one blob's row, if it is still collectable, and moves its
// file aside, in one write transaction; then removes the moved file. A file
// that cannot be moved rolls the row back. A transaction that does not
// commit has the move undone, so the row never outlives the bytes. gone
// reports that the row was deleted; err can still be set then, when only
// the trash file could not be removed, which the next pass finishes.
func (g *BlobGC) collect(ctx context.Context, hash string, cutoff int64) (gone bool, err error) {
	path, err := blob.Path(g.Root, hash)
	if err != nil {
		return false, fmt.Errorf("blob gc: %q: %w", hash, err)
	}
	trash := path + trashSuffix
	var deleted, moved bool
	err = g.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		n, err := q.DeleteCollectableBlob(ctx, dbq.DeleteCollectableBlobParams{Hash: hash, Cutoff: cutoff})
		if err != nil || n == 0 {
			return err
		}
		switch err := g.mv(path, trash); {
		case err == nil:
			moved = true
		case !errors.Is(err, fs.ErrNotExist):
			return err
		}
		deleted = true
		return nil
	})
	if err != nil {
		if moved {
			// The row is still there, so the bytes go back. Content
			// addressing makes this safe even if an upload of the same bytes
			// has since written the file: the two are identical.
			if rerr := g.mv(trash, path); rerr != nil {
				err = errors.Join(err, fmt.Errorf("restoring the file (the next pass restores it): %w", rerr))
			}
		}
		return false, fmt.Errorf("blob gc: %s: %w", hash, err)
	}
	if moved {
		if err := os.Remove(trash); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return deleted, fmt.Errorf("blob gc: %s: removing the moved file (the next pass removes it): %w", hash, err)
		}
	}
	return deleted, nil
}

// recoverTrash settles every file a previous pass moved aside and did not
// finish with: a crash between the move and the commit, a restore that
// failed, or a removal that failed. If the row is still there the bytes go
// back; if it is gone, so does the file. Each is decided inside a write
// transaction, so no upload interleaves with it. It returns how many it
// restored.
func (g *BlobGC) recoverTrash(ctx context.Context) (int64, error) {
	pattern := filepath.Join(g.Root, blob.Dir, blob.Algorithm, "*", "*"+trashSuffix)
	trash, err := filepath.Glob(pattern)
	if err != nil {
		return 0, err
	}
	var restored int64
	for _, t := range trash {
		hash := strings.TrimSuffix(filepath.Base(t), trashSuffix)
		path, err := blob.Path(g.Root, hash)
		if err != nil || path+trashSuffix != t {
			continue // not a name the collector writes; leave it for a person
		}
		err = g.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
			exists, err := q.BlobExists(ctx, hash)
			if err != nil {
				return err
			}
			if !exists {
				return os.Remove(t)
			}
			if _, err := os.Stat(path); err == nil {
				return os.Remove(t) // an upload has put identical bytes back
			}
			if err := g.mv(t, path); err != nil {
				return err
			}
			restored++
			return nil
		})
		if err != nil {
			return restored, fmt.Errorf("%s: %w", t, err)
		}
	}
	return restored, nil
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
