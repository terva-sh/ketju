package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/terva-sh/ketju/internal/backup"
	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/lock"
	"github.com/terva-sh/ketju/internal/store/blob"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

const backupUsage = `usage: ketju backup [--root DIR] [--json] DEST

Writes a consistent backup of the data directory DIR to DEST, which must not
exist and must lie outside DIR; its parent must exist. The backup holds the
database, every thread log and every attachment. It leaves out the thread
indexes and thumbnails, which are rebuilt when first needed, uploads still
being staged, secrets/ and config.toml. Keep your own copy of config.toml
and secrets/.

With ketju serve running, backup asks it over the control socket
DIR/ketju.sock for a snapshot: serve holds new posts for the moment it takes
to bring the database up to date and copy it, and reads go on meanwhile. Each
log is then copied up to the length that snapshot records, so the backup's
logs and database agree exactly. With serve stopped, backup takes the data
directory's lock instead, copies everything as it stands, and brings the
copy's database up to its logs. The source is never written.

A database another version of ketju wrote is copied at its own schema and
not migrated, so the backup is one the binary that wrote it can restore.
Back up before replacing the binary to upgrade; the README's "Upgrading"
section has the steps. Such a copy is not brought up to its logs either: a
log ahead of its database is reported, and serve closes the gap when it
next starts on the copy.

The backup is written beside DEST and renamed to DEST only once ketju fsck's
checks pass on it. To restore, copy DEST into an empty data directory, add
your config.toml and secrets/, and start ketju serve. The README has the
steps.

exit codes:
  0  written and verified
  1  the copy failed verification: a damaged log or a log shorter than its
     projection, which comes from the source (run ketju fsck on it), or an
     attachment a post names that the source has and the copy does not; the
     unverified copy is kept beside DEST and named. An attachment the source
     lacks too is reported and does not fail the backup
  2  usage, DEST exists or is inside DIR, or DIR has no threads directory
  4  the data directory is in use by something other than serve, a hard
     redaction is pending, serve could not be reached or refused, or a read
     or write failed; nothing is left beside DEST

`

// backupOpts are the seams a test needs. The zero value is what the binary
// runs with.
type backupOpts struct {
	// ask sends the snapshot request; nil is backup.Ask.
	ask func(ctx context.Context, path string) (backup.Answer, func(), error)
	// beforeLock runs just before the lock is taken; a test starts a
	// redaction there.
	beforeLock func()
}

// backupSummary is the report, as --json prints it.
type backupSummary struct {
	Mode     string `json:"mode"` // "online" or "offline"
	Dest     string `json:"dest"`
	Threads  int    `json:"threads"`
	Bytes    int64  `json:"bytes"`
	Blobs    int    `json:"blobs"`
	PausedMS int64  `json:"paused_ms,omitempty"`
	CaughtUp int    `json:"caught_up"`
	// Schema is "current" when the database's schema is this binary's,
	// and otherwise "older" or "newer": the copy is then left at it,
	// neither migrated nor caught up.
	Schema string `json:"schema"`
	// Drift is the threads whose copied log is ahead of the copied
	// database, which only a copy at another schema can have.
	Drift int `json:"drift_threads,omitempty"`
	// MissingBlobs are attachments posts name that the source itself
	// lacks, so the backup does too.
	MissingBlobs int   `json:"missing_blobs,omitempty"`
	ElapsedMS    int64 `json:"elapsed_ms"`
}

func runBackup(args []string) error {
	ctx, stop := signalContext()
	defer stop()
	return backupMain(ctx, os.Stdout, os.Stderr, args, backupOpts{})
}

func backupMain(ctx context.Context, out, errOut io.Writer, args []string, o backupOpts) (err error) {
	start := time.Now()
	fs_ := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs_.SetOutput(errOut)
	root := fs_.String("root", ".", "data directory")
	asJSON := fs_.Bool("json", false, "print the report as JSON instead of text")
	fs_.Usage = func() {
		fmt.Fprint(fs_.Output(), backupUsage)
		fs_.PrintDefaults()
	}
	if err := fs_.Parse(args); err != nil {
		return errUsage{err}
	}
	if fs_.NArg() != 1 {
		fs_.Usage()
		return errUsage{errors.New("backup takes one argument, DEST")}
	}
	dest := filepath.Clean(fs_.Arg(0))
	if _, err := os.Lstat(dest); err == nil {
		return errUsage{fmt.Errorf("backup: %s already exists; give a new path", dest)}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("backup: %w", err)
	}
	parent := filepath.Dir(dest)
	if fi, err := os.Stat(parent); err != nil || !fi.IsDir() {
		return errUsage{fmt.Errorf("backup: %s, the directory DEST goes in, does not exist", parent)}
	}
	if fi, err := os.Stat(filepath.Join(*root, threadid.ThreadsDir)); err != nil || !fi.IsDir() {
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return errUsage{fmt.Errorf("backup: %s has no threads directory; is --root the data directory?", *root)}
		}
		return fmt.Errorf("backup: %w", err)
	}
	// A backup inside the data directory it copies would be walked by its
	// own copy: under blobs/ it copies itself into itself until the disk is
	// full (Terva review 1978). Compared with symlinks resolved, so an alias
	// of the directory is caught too.
	if inside, err := within(*root, parent); err != nil {
		return fmt.Errorf("backup: %w", err)
	} else if inside {
		return errUsage{fmt.Errorf("backup: %s is inside the data directory %s; put the backup outside it", dest, *root)}
	}
	if err := redactionPending(*root); err != nil {
		return err
	}

	partial, err := os.MkdirTemp(parent, filepath.Base(dest)+".partial-")
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			os.RemoveAll(partial)
		}
	}()
	for _, d := range []string{"db", threadid.ThreadsDir, blob.Dir} {
		if err := os.MkdirAll(filepath.Join(partial, d), 0o750); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
	}
	sum := backupSummary{Dest: dest}

	if o.beforeLock != nil {
		o.beforeLock()
	}
	lk, lerr := lock.Acquire(*root, "backup")
	var held *lock.HeldError
	switch {
	case lerr == nil:
		// Held to the end, so serve, reindex and redact cannot start midway.
		defer lk.Release()
		// Checked again under the lock: a redact that started after the
		// first check and stopped partway has released the lock by now and
		// left its journal (Terva review 1983).
		if err := redactionPending(*root); err != nil {
			return err
		}
		sum.Mode = "offline"
		if err := backupOffline(ctx, *root, partial, &sum); err != nil {
			return err
		}
	case errors.As(lerr, &held) && held.Holder != nil && held.Holder.Purpose == "serve":
		sum.Mode = "online"
		if err := backupOnline(ctx, *root, partial, o, &sum); err != nil {
			return err
		}
	case errors.As(lerr, &held):
		return fmt.Errorf("backup: %w Backup runs beside ketju serve, or alone; wait for the holder to finish", lerr)
	default:
		return fmt.Errorf("backup: %w", lerr)
	}

	n, err := copyBlobs(*root, partial)
	if err != nil {
		return err
	}
	sum.Blobs = n

	caught, schema, err := catchUpCopy(ctx, partial)
	if err != nil {
		return err
	}
	sum.CaughtUp, sum.Schema = caught, schema

	verify, err := fsckWalk(partial, false)
	if err != nil {
		return fmt.Errorf("backup: verifying the copy: %w", err)
	}
	// Drift fails a copy at this binary's schema, which the catch-up has
	// just closed. A copy at another schema keeps what its source had.
	drift := verify.DriftThreads > 0 && schema == schemaCurrent
	if !verify.DriftChecked || verify.Damaged > 0 || verify.Quarantined > 0 || drift {
		keep = true
		writeFsckText(errOut, verify, true)
		return errCorrupt{fmt.Errorf("backup: the copy in %s failed verification: %d damaged, %d shorter than "+
			"their projection, %d ahead of it%s. This comes from the source: run ketju fsck --root %s",
			partial, verify.Damaged, verify.Quarantined, verify.DriftThreads, driftNote(verify), *root)}
	}

	sum.Drift = verify.DriftThreads

	// fsck's walk reads the logs and the projection, not the blob store, so
	// the attachments the copy's posts name are checked here (Terva review
	// 1982). One the source has and the copy lacks fails the copy. One the
	// source lacks too is reported and does not: the copy is as whole as its
	// source, and refusing would refuse every backup of it.
	lost, absent, err := checkBlobs(ctx, *root, partial)
	if err != nil {
		return fmt.Errorf("backup: checking the copy's attachments: %w", err)
	}
	if len(lost) > 0 {
		keep = true
		return errCorrupt{fmt.Errorf("backup: the copy in %s failed verification: %d attachment(s) its posts "+
			"name were not copied, among them %s", partial, len(lost), lost[0])}
	}
	sum.MissingBlobs = len(absent)

	if err := dropEmptyWAL(adminDBPath(partial)); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if err := syncTree(partial); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if err := os.Rename(partial, dest); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	keep = true
	if err := syncDirPath(parent); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	sum.ElapsedMS = time.Since(start).Milliseconds()
	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(sum)
	}
	fmt.Fprintf(out, "backed up %s to %s (%s): %d thread(s), %d byte(s) of log, %d blob(s)",
		*root, dest, sum.Mode, sum.Threads, sum.Bytes, sum.Blobs)
	if sum.Mode == "online" {
		fmt.Fprintf(out, "; posts held for %d ms", sum.PausedMS)
	}
	fmt.Fprintf(out, "; verified in %s\n", time.Since(start).Round(time.Millisecond))
	if sum.MissingBlobs > 0 {
		fmt.Fprintf(out, "%d attachment(s) that posts name are missing from %s itself, so from the backup too\n",
			sum.MissingBlobs, *root)
	}
	if sum.Schema != schemaCurrent {
		fmt.Fprintf(out, "the database's schema is %s than this binary's, so the copy was left at it, not migrated\n", sum.Schema)
		if sum.Drift > 0 {
			fmt.Fprintf(out, "%d thread log(s) are ahead of the database; serve closes that when it next starts on the copy\n", sum.Drift)
		}
	}
	return nil
}

// What catchUpCopy found the copy's schema to be, against this binary's.
const (
	schemaCurrent = "current"
	schemaOlder   = "older"
	schemaNewer   = "newer"
)

// within reports whether dir is root or below it, once both are absolute
// and their symlinks resolved.
func within(root, dir string) (bool, error) {
	resolve := func(p string) (string, error) {
		abs, err := filepath.Abs(p)
		if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(abs)
	}
	r, err := resolve(root)
	if err != nil {
		return false, err
	}
	d, err := resolve(dir)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(r, d)
	if err != nil {
		return false, nil // on another volume: not inside
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))), nil
}

// redactionPending is an error naming the journal when a hard redaction
// is pending in root.
func redactionPending(root string) error {
	if _, err := os.Stat(journalPath(root)); err == nil {
		return fmt.Errorf("backup: %s exists: a hard redaction is pending. Finish it with "+
			"ketju redact --root %s --resume, then back up", journalPath(root), root)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("backup: %w", err)
	}
	return nil
}

func driftNote(s fsckSummary) string {
	if !s.DriftChecked {
		return "; the copy's database could not be read: " + s.DriftNote
	}
	return ""
}

// backupOffline copies a stopped data directory: the database by VACUUM
// INTO from a read-only handle, and each log whole, to its last whole frame.
func backupOffline(ctx context.Context, root, partial string, sum *backupSummary) error {
	src := adminDBPath(root)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("backup: %s: %w; a data directory serve has never started has nothing to back up", src, err)
	}
	d, err := db.OpenReadOnly(ctx, src)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	err = d.SnapshotInto(ctx, adminDBPath(partial))
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	logs, _, err := threadid.WalkLogs(root)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	for _, id := range sortedIDs(logs) {
		n, err := wholeLength(id.LogPath(root), logs[id])
		if err != nil {
			return err
		}
		if err := copyPrefix(id.LogPath(root), id.LogPath(partial), n); err != nil {
			return err
		}
		sum.Threads++
		sum.Bytes += n
	}
	return nil
}

// wholeLength is the length of a log to its last whole frame, or its size
// when it cannot be read as a log: the copy is then verified, and the
// damage reported, rather than hidden.
func wholeLength(path string, size int64) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("backup: %w", err)
	}
	defer f.Close()
	rec, err := threadlog.Open(f, size, nil)
	if err != nil {
		return size, nil
	}
	return rec.LogLength, nil
}

// backupOnline asks serve for a snapshot, moves it into the copy, and copies
// each log up to the length the snapshot projected it to. A log with no row
// in the snapshot is left out: a thread created after it, or one whose
// creation never finished.
func backupOnline(ctx context.Context, root, partial string, o backupOpts, sum *backupSummary) error {
	sock, err := backup.SocketPath(root)
	if err != nil {
		return fmt.Errorf("backup: %w; stop ketju serve and back up offline", err)
	}
	ask := o.ask
	if ask == nil {
		ask = backup.Ask
	}
	actx, cancel := context.WithTimeout(ctx, backup.PauseCap+30*time.Second)
	defer cancel()
	a, done, err := ask(actx, sock)
	// Closing the connection tells serve the staged database has been taken,
	// and it removes the staging directory; it does that too if this process
	// dies first. The removal here comes first, so none is left once backup
	// returns.
	defer done()
	if err != nil {
		return fmt.Errorf("backup: ketju serve holds %s, but its control socket %s did not answer: %w", root, sock, err)
	}
	if !a.OK {
		return fmt.Errorf("backup: ketju serve refused the snapshot: %s", a.Error)
	}
	staged := filepath.Join(root, a.Staging)
	if filepath.Dir(filepath.Dir(staged)) != filepath.Clean(root) ||
		!strings.HasPrefix(filepath.Base(filepath.Dir(staged)), backup.StagingPrefix) {
		return fmt.Errorf("backup: serve named %q as its snapshot, which is not a staging file", a.Staging)
	}
	defer os.RemoveAll(filepath.Dir(staged))
	sum.PausedMS = a.PausedMS
	if err := moveFile(staged, adminDBPath(partial)); err != nil {
		return err
	}

	projected := map[threadid.ID]int64{}
	d, err := db.OpenReadOnly(ctx, adminDBPath(partial))
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	err = d.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		rows, err := q.ListProjectionLengths(ctx)
		for _, r := range rows {
			projected[threadid.ID(r.ThreadID)] = r.LogLength
		}
		return err
	})
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("backup: reading the snapshot's projection: %w", err)
	}
	ids := make([]threadid.ID, 0, len(projected))
	for id := range projected {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		n := projected[id]
		if err := copyPrefix(id.LogPath(root), id.LogPath(partial), n); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// The verification reports it as a projection with no log.
				continue
			}
			return err
		}
		sum.Threads++
		sum.Bytes += n
	}
	return nil
}

// copyPrefix copies the first n bytes of src to a new file dst, and fsyncs
// it. A source shorter than n is copied as it is, and the verification then
// reports the copy as shorter than its projection.
func copyPrefix(src, dst string, n int64) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	outf, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if _, err := io.Copy(outf, io.LimitReader(in, n)); err != nil {
		outf.Close()
		return fmt.Errorf("backup: copying %s: %w", src, err)
	}
	if err := outf.Sync(); err != nil {
		outf.Close()
		return fmt.Errorf("backup: %w", err)
	}
	return outf.Close()
}

// moveFile renames src to dst, or copies and fsyncs it across filesystems.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	fi, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if err := copyPrefix(src, dst, fi.Size()); err != nil {
		return err
	}
	return os.Remove(src)
}

// copyBlobs copies every blob, leaving out thumbnails and the staging
// directory. A blob is written before the post that names it, so every blob
// a copied log names is already on disk when this runs, after the logs.
func copyBlobs(root, partial string) (int, error) {
	top := filepath.Join(root, blob.Dir)
	n := 0
	err := filepath.WalkDir(top, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path == top {
				return filepath.SkipDir
			}
			return err
		}
		rel, err := filepath.Rel(top, path)
		if err != nil {
			return err
		}
		if e.IsDir() {
			if rel == blob.Tmp {
				return filepath.SkipDir
			}
			return nil
		}
		if !e.Type().IsRegular() || strings.HasSuffix(e.Name(), blob.ThumbSuffix) {
			return nil
		}
		fi, err := e.Info()
		if err != nil {
			return err
		}
		n++
		return copyPrefix(path, filepath.Join(partial, blob.Dir, rel), fi.Size())
	})
	if err != nil {
		return n, fmt.Errorf("backup: copying blobs: %w", err)
	}
	return n, nil
}

// checkBlobs reads the attachments the copy's database says its posts name,
// and returns those the copy lacks although the source has them (lost), and
// those the source lacks too (absent).
func checkBlobs(ctx context.Context, root, partial string) (lost, absent []string, err error) {
	d, err := db.OpenReadOnly(ctx, adminDBPath(partial))
	if err != nil {
		return nil, nil, err
	}
	defer d.Close()
	var hashes []string
	err = d.Read(ctx, func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT DISTINCT hash FROM post_attachments ORDER BY hash`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var h string
			if err := rows.Scan(&h); err != nil {
				return err
			}
			hashes = append(hashes, h)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, nil, err
	}
	exists := func(base, h string) (bool, error) {
		p, err := blob.Path(base, h)
		if err != nil {
			return false, fmt.Errorf("a post names the attachment %q: %w", h, err)
		}
		_, err = os.Stat(p)
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return err == nil, err
	}
	for _, h := range hashes {
		ok, err := exists(partial, h)
		if err != nil || ok {
			if err != nil {
				return nil, nil, err
			}
			continue
		}
		inSource, err := exists(root, h)
		if err != nil {
			return nil, nil, err
		}
		if inSource {
			lost = append(lost, h)
		} else {
			absent = append(absent, h)
		}
	}
	return lost, absent, d.Close()
}

// catchUpCopy runs the projection's catch-up over the copy, as serve's
// startup does, and returns how many threads it projected and the copy's
// schema against this binary's. Online it finds nothing to do. Offline it
// closes any drift the source had, so the copy's logs and projection agree.
// Only the copy is written; the indexes the catch-up's opens leave behind
// are removed again, because a backup holds none.
//
// A copy whose schema is not this binary's is left alone. Migrating it would
// make a backup the binary that wrote it refuses (TestNewerSchemaRefused),
// which is the backup an upgrade takes in case it has to go back; and the
// projection here is written for this binary's schema only.
func catchUpCopy(ctx context.Context, partial string) (int, string, error) {
	quiet := slog.New(slog.DiscardHandler)
	d, err := db.Open(ctx, db.Options{Path: adminDBPath(partial)})
	if err != nil {
		return 0, "", fmt.Errorf("backup: opening the copy's database: %w", err)
	}
	defer d.Close()
	switch n, err := d.Pending(ctx); {
	case errors.Is(err, db.ErrSchemaNewer):
		return 0, schemaNewer, d.Close()
	case err != nil:
		return 0, "", fmt.Errorf("backup: reading the copy's schema version: %w", err)
	case n > 0:
		return 0, schemaOlder, d.Close()
	}
	cache, err := threadcache.New(partial, threadcache.Options{Logger: quiet})
	if err != nil {
		return 0, "", fmt.Errorf("backup: %w", err)
	}
	w, err := projection.New(projection.Options{DB: d, Root: partial, Logger: quiet,
		Sweep: func(context.Context) error { return nil }})
	if err != nil {
		cache.Close()
		return 0, "", fmt.Errorf("backup: %w", err)
	}
	rep, err := w.CatchUp(ctx, func(_ context.Context, id threadid.ID) (int64, error) {
		var n int64
		err := cache.With(id, func(th *threadcache.Thread) error {
			n = th.Actor.LogLength()
			return nil
		})
		return n, err
	})
	if cerr := cache.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, "", fmt.Errorf("backup: catching the copy's projection up: %w", err)
	}
	if err := d.Close(); err != nil {
		return 0, "", fmt.Errorf("backup: closing the copy's database: %w", err)
	}
	if err := removeIndexes(partial); err != nil {
		return 0, "", err
	}
	return rep.Closed, schemaCurrent, nil
}

// dropEmptyWAL removes the -wal and -shm files that opening the copy's
// database left beside it, once the WAL is empty: closing checkpointed
// everything into the file, so the backup's database is that one file.
func dropEmptyWAL(path string) error {
	fi, err := os.Stat(path + "-wal")
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case fi.Size() != 0:
		return nil
	default:
		if err := os.Remove(path + "-wal"); err != nil {
			return err
		}
	}
	if err := os.Remove(path + "-shm"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// removeIndexes removes every thread index under root.
func removeIndexes(root string) error {
	return filepath.WalkDir(filepath.Join(root, threadid.ThreadsDir), func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !e.IsDir() && strings.HasSuffix(e.Name(), threadid.IndexExt) {
			return os.Remove(path)
		}
		return nil
	})
}

// syncTree fsyncs every file and directory under root, the directories
// after what they hold.
func syncTree(root string) error {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		serr := f.Sync()
		if cerr := f.Close(); serr == nil {
			serr = cerr
		}
		return serr
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := syncDirPath(dirs[i]); err != nil {
			return err
		}
	}
	return nil
}

func syncDirPath(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	serr := f.Sync()
	if cerr := f.Close(); serr == nil {
		serr = cerr
	}
	return serr
}

func sortedIDs(m map[threadid.ID]int64) []threadid.ID {
	ids := make([]threadid.ID, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
