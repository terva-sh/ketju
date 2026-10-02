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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// threadReport is what fsck found in one thread.
type threadReport struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Bytes   int64  `json:"bytes"`
	Records int    `json:"records"`
	Posts   int    `json:"posts"`

	// Damaged counts records that failed their checksum. Truncated is the
	// bytes past the last complete frame, which is an interrupted write rather
	// than damage and is reported separately for exactly that reason.
	Damaged   int   `json:"damaged,omitempty"`
	Truncated int64 `json:"truncated_bytes,omitempty"`

	IndexRebuilt bool   `json:"index_rebuilt,omitempty"`
	Error        string `json:"error,omitempty"`

	// Projection is how the log stands against its projection_state row,
	// by projection.Classify, the rule serve's catch-up acts on: "current",
	// "drift" (the log is longer), "unprojected" (frames and no row, which is
	// drift of the whole log), "unfinished" (only a file header and no row),
	// or "corrupt" (the log is shorter, or there is no log: serve quarantines
	// the thread). Empty when drift was not checked, and for a log reported
	// damaged, whose damage is the finding.
	Projection string `json:"projection,omitempty"`

	// Projected is the projection_state row's length, when there is a row.
	Projected *int64 `json:"projected_bytes,omitempty"`

	// DriftBytes is how far the projection is behind this log's last whole
	// record, for drift and unprojected.
	DriftBytes int64 `json:"drift_bytes,omitempty"`

	// id is the thread the path names, when it names one at the right
	// shard, and whole the length to the last whole frame: what serve's
	// catch-up projects to once an open has cut a torn tail.
	id    threadid.ID
	hasID bool
	whole int64
}

func (r threadReport) clean() bool {
	return r.Error == "" && r.Damaged == 0
}

type fsckSummary struct {
	Threads   int            `json:"threads"`
	Records   int            `json:"records"`
	Posts     int            `json:"posts"`
	Damaged   int            `json:"damaged_threads"`
	Truncated int            `json:"truncated_threads"`
	Rebuilt   int            `json:"indexes_rebuilt"`
	Reports   []threadReport `json:"threads_detail"`

	// DriftChecked is whether the projection was compared with the logs. It
	// is false when there is no database or it could not be read, and
	// DriftNote says which, so a clean run cannot be mistaken for a run that
	// checked.
	DriftChecked bool   `json:"drift_checked"`
	DriftNote    string `json:"drift_note,omitempty"`

	// DriftThreads are the threads whose log is ahead of their projection,
	// and DriftBytes the sum of how far. Quarantined are those whose log is
	// shorter than their projection, or missing: serve quarantines each.
	DriftThreads int   `json:"drift_threads"`
	DriftBytes   int64 `json:"drift_bytes"`
	Quarantined  int   `json:"quarantined_threads"`

	// Unfinished are logs holding only their file header, with no row:
	// a thread whose creation never finished. Not an error, and not
	// current either; catch-up reports them the same way.
	Unfinished int `json:"unfinished_threads"`
}

func runFsck(args []string) error { return fsckMain(os.Stdout, args) }

func fsckMain(w io.Writer, args []string) error {
	fs_ := flag.NewFlagSet("fsck", flag.ContinueOnError)
	root := fs_.String("root", ".", "data directory")
	rebuild := fs_.Bool("rebuild", false, "rewrite each thread's index from its log")
	asJSON := fs_.Bool("json", false, "emit the report as JSON instead of text")
	quiet := fs_.Bool("quiet", false, "print only threads with something wrong")
	fs_.Usage = func() {
		fmt.Fprint(fs_.Output(), `usage: ketju fsck [--root DIR] [--rebuild] [--json] [--quiet]

Walks every thread log under DIR, verifies every checksum, and reports what it
found. Reads only, unless --rebuild is given.

When DIR/db/ketju.db exists, fsck also opens it read-only and compares each
log with the length the database has projected it to. A log longer than its
projection is drift: a crash between the two writes leaves it, and ketju serve
closes it at startup. A log shorter than its projection, or a projection with
no log, cannot come from a crash. Serve quarantines that thread and answers
503 for it until an operator repairs it, and fsck marks it QUARANT.

Safe against a live data directory, and takes no lock. Neither the logs nor
the database are written, and fsck scans each log to the size it had when
opened, so an append arriving mid-scan is simply not counted yet. It reads the
database before the logs, so a live write can show as drift, which catch-up is
closing, but never as a quarantined thread. --rebuild writes only the index,
which is derived and disposable: the worst a concurrent server can do to it is
leave one that needs rebuilding again.

exit codes:
  0  clean
  1  corruption found: a damaged log, or a thread serve would quarantine
  2  usage
  3  drift found between a log and its projection, and no corruption

`)
		fs_.PrintDefaults()
	}
	if err := fs_.Parse(args); err != nil {
		return errUsage{err}
	}
	if fs_.NArg() != 0 {
		fs_.Usage()
		return errUsage{errors.New("fsck takes no positional arguments")}
	}

	sum, err := fsckWalk(*root, *rebuild)
	if err != nil {
		return err
	}

	if *asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(sum); err != nil {
			return err
		}
	} else {
		writeFsckText(w, sum, *quiet)
	}

	switch {
	case sum.Damaged > 0 || sum.Quarantined > 0:
		var found []string
		if sum.Damaged > 0 {
			found = append(found, fmt.Sprintf("%d of %d thread(s) damaged", sum.Damaged, sum.Threads))
		}
		if sum.Quarantined > 0 {
			found = append(found, fmt.Sprintf("%d thread(s) shorter than their projection, or missing", sum.Quarantined))
		}
		return errCorrupt{errors.New(strings.Join(found, "; "))}
	case sum.DriftThreads > 0:
		return errDrift{fmt.Errorf("%d thread(s) ahead of their projection by %d byte(s)", sum.DriftThreads, sum.DriftBytes)}
	}
	return nil
}

// fsckWalk is fsck's walk without its report: every log under root checked,
// and compared with its projection when the database can be read. `ketju
// backup` verifies its copy with it (TKT-01M29CXK35).
func fsckWalk(root string, rebuild bool) (fsckSummary, error) {
	// The projection is read before the logs are listed or sized. Logs only
	// grow and the projection is written after the log, so in this order a
	// write landing mid-run can make a thread look behind its log, never
	// ahead of it, and a new thread's row never looks like one whose log is
	// gone.
	projected, driftNote := readProjection(context.Background(), root)

	paths, err := findLogs(root)
	if err != nil {
		return fsckSummary{}, err
	}

	sum := fsckSummary{DriftChecked: projected != nil, DriftNote: driftNote}
	for _, p := range paths {
		rep := checkThread(p, rebuild)
		sum.Reports = append(sum.Reports, rep)
		sum.Threads++
		sum.Records += rep.Records
		sum.Posts += rep.Posts
		if !rep.clean() {
			sum.Damaged++
		}
		if rep.Truncated > 0 {
			sum.Truncated++
		}
		if rep.IndexRebuilt {
			sum.Rebuilt++
		}
	}
	if projected != nil {
		compareProjection(&sum, projected)
	}
	return sum, nil
}

// readProjection reads every thread's projected length from the data
// directory's database, opened read-only. It returns nil, with the reason,
// when there is no database or it cannot be read: checking the logs is fsck's
// first job and does not depend on the database.
func readProjection(ctx context.Context, root string) (map[threadid.ID]int64, string) {
	path := adminDBPath(root)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, "no database at " + path
	}
	d, err := db.OpenReadOnly(ctx, path)
	if err != nil {
		return nil, err.Error()
	}
	defer d.Close()
	out := map[threadid.ID]int64{}
	err = d.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		rows, err := q.ListProjectionLengths(ctx)
		for _, r := range rows {
			out[threadid.ID(r.ThreadID)] = r.LogLength
		}
		return err
	})
	if err != nil {
		return nil, fmt.Sprintf("reading projection_state from %s: %v", path, err)
	}
	return out, ""
}

// compareProjection classifies each log fsck walked against its projection,
// by the rule serve's catch-up acts on, and reports every projection whose
// log is not at the path its id names.
func compareProjection(sum *fsckSummary, projected map[threadid.ID]int64) {
	seen := map[threadid.ID]bool{}
	for i := range sum.Reports {
		r := &sum.Reports[i]
		if !r.hasID {
			continue
		}
		seen[r.id] = true
		if !r.clean() {
			continue
		}
		at, ok := projected[r.id]
		if ok {
			r.Projected = &at
		} else {
			at = projection.Missing
		}
		switch projection.Classify(r.whole, at) {
		case projection.ClassCurrent:
			r.Projection = "current"
		case projection.ClassUnfinished:
			r.Projection = "unfinished"
			sum.Unfinished++
		case projection.ClassCorrupt:
			r.Projection = "corrupt"
			sum.Quarantined++
		case projection.ClassDrift:
			r.Projection = "drift"
			if !ok {
				r.Projection = "unprojected"
				at = threadlog.FileHeaderSize
			}
			r.DriftBytes = r.whole - at
			sum.DriftThreads++
			sum.DriftBytes += r.DriftBytes
		}
	}

	var gone []threadReport
	for id, at := range projected {
		if seen[id] || projection.Classify(projection.Missing, at) != projection.ClassCorrupt {
			continue
		}
		gone = append(gone, threadReport{ID: id.String(), Projection: "corrupt", Projected: &at, id: id, hasID: true})
		sum.Quarantined++
	}
	sort.Slice(gone, func(i, j int) bool { return gone[i].id < gone[j].id })
	sum.Reports = append(sum.Reports, gone...)
}

func writeFsckText(w io.Writer, sum fsckSummary, quiet bool) {
	for _, r := range sum.Reports {
		found := true
		switch {
		case r.Error != "":
			fmt.Fprintf(w, "DAMAGED  %s  %s\n", r.ID, r.Error)
		case r.Damaged > 0:
			fmt.Fprintf(w, "DAMAGED  %s  %d record(s) failed their checksum, %d record(s) readable\n",
				r.ID, r.Damaged, r.Records)
		case r.Projection == "corrupt" && r.Path == "":
			fmt.Fprintf(w, "QUARANT  %s  projected to %d bytes, but the thread has no log\n", r.ID, *r.Projected)
		case r.Projection == "corrupt":
			fmt.Fprintf(w, "QUARANT  %s  the log is %d bytes to its last whole record, shorter than its projection of %d\n",
				r.ID, r.whole, *r.Projected)
		default:
			found = false
		}
		if r.Truncated > 0 {
			found = true
			fmt.Fprintf(w, "TORN     %s  %d byte(s) past the last complete record; an interrupted write\n",
				r.ID, r.Truncated)
		}
		switch r.Projection {
		case "drift":
			found = true
			fmt.Fprintf(w, "DRIFT    %s  the log is %d byte(s) ahead of its projection\n", r.ID, r.DriftBytes)
		case "unprojected":
			found = true
			fmt.Fprintf(w, "DRIFT    %s  never projected: %d byte(s) of records\n", r.ID, r.DriftBytes)
		}
		if !found && !quiet {
			fmt.Fprintf(w, "ok       %s  %d record(s), %d post(s), %d bytes\n",
				r.ID, r.Records, r.Posts, r.Bytes)
		}
		if r.IndexRebuilt && !quiet {
			fmt.Fprintf(w, "         %s  index rebuilt\n", r.ID)
		}
	}

	fmt.Fprintf(w, "\n%d thread(s), %d record(s), %d post(s)\n", sum.Threads, sum.Records, sum.Posts)
	if sum.Rebuilt > 0 {
		fmt.Fprintf(w, "%d index(es) rebuilt\n", sum.Rebuilt)
	}
	if sum.Truncated > 0 {
		fmt.Fprintf(w, "%d thread(s) with a torn tail\n", sum.Truncated)
	}
	if sum.Damaged > 0 {
		fmt.Fprintf(w, "%d thread(s) DAMAGED\n", sum.Damaged)
	}
	if sum.Quarantined > 0 {
		fmt.Fprintf(w, "%d thread(s) QUARANTINED: the log is shorter than its projection, or missing; "+
			"serve answers 503 for each until it is repaired\n", sum.Quarantined)
	}
	if sum.Damaged == 0 && sum.Quarantined == 0 {
		fmt.Fprintf(w, "no corruption found\n")
	}
	if sum.Unfinished > 0 {
		fmt.Fprintf(w, "%d thread(s) unfinished: the log holds only its header and nothing projected it; "+
			"its creation never finished, and there is nothing to project\n", sum.Unfinished)
	}
	switch {
	case !sum.DriftChecked:
		fmt.Fprintf(w, "projection drift not checked: %s\n", sum.DriftNote)
	case sum.DriftThreads > 0:
		fmt.Fprintf(w, "%d thread(s) with projection drift, %d byte(s) behind\n", sum.DriftThreads, sum.DriftBytes)
		fmt.Fprintf(w, "drift found beside a running ketju serve may be lag that catch-up is closing; "+
			"rerun with serve stopped to confirm it\n")
	case sum.Quarantined > 0 || sum.Damaged > 0:
		// A quarantined thread is not current, and a damaged log is not
		// compared at all, so "every thread" would be false (review 1536).
		fmt.Fprintf(w, "no projection drift found; the QUARANT and DAMAGED threads above are not current\n")
	case sum.Unfinished > 0:
		// An unfinished thread has no projection to be current (review 1537).
		fmt.Fprintf(w, "no projection drift found; every thread but the unfinished ones is current\n")
	default:
		fmt.Fprintf(w, "projection current for every thread\n")
	}
}

// findLogs walks the threads directory. Sorted, so two runs over an unchanged
// directory produce byte-identical output and can be diffed.
func findLogs(root string) ([]string, error) {
	dir := filepath.Join(root, threadid.ThreadsDir)
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(p) == threadid.LogExt {
			out = append(out, p)
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, errUsage{fmt.Errorf("no threads directory under %s", root)}
	}
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// checkThread walks one log and optionally rewrites its index.
func checkThread(path string, rebuild bool) threadReport {
	rep := threadReport{Path: path}

	id, err := threadid.FromLogPath(path)
	if err != nil {
		// A file in the wrong shard, or not named after an id at all. Somebody
		// moved it or wrote it by hand, and either way the tool that finds it
		// is the one that should say so.
		rep.ID = filepath.Base(path)
		rep.Error = err.Error()
		return rep
	}
	rep.ID, rep.id, rep.hasID = id.String(), id, true

	f, err := os.Open(path)
	if err != nil {
		rep.Error = err.Error()
		return rep
	}
	defer f.Close()

	// Size is taken once, up front. An append arriving mid-scan lands past
	// this point and is simply not counted, which is what makes running
	// against a live directory safe rather than merely unlikely to break.
	fi, err := f.Stat()
	if err != nil {
		rep.Error = err.Error()
		return rep
	}
	rep.Bytes = fi.Size()

	var lastGood int64 = threadlog.FileHeaderSize
	scanErr := threadlog.ScanFrames(f, fi.Size(), func(r threadlog.ScanRecord) bool {
		if r.Err != nil {
			if r.Fatal {
				// Past the last complete record. If it reaches exactly to the
				// end of the file this is a torn write, not damage: nothing
				// rewrites those bytes and an interrupted append leaves
				// precisely this shape.
				if errors.Is(r.Err, threadlog.ErrShortBuffer) {
					rep.Truncated = fi.Size() - lastGood
				} else {
					rep.Damaged++
					rep.Error = r.Err.Error()
				}
				return false
			}
			rep.Damaged++
			return true
		}
		rep.Records++
		if r.Header.Type == threadlog.TypePost {
			rep.Posts++
		}
		lastGood = r.Offset + int64(r.Size)
		return true
	})
	if scanErr != nil {
		rep.Error = scanErr.Error()
		return rep
	}
	rep.whole = lastGood

	if rebuild && rep.Damaged == 0 && rep.Error == "" {
		if err := rebuildIndex(f, id, path, lastGood); err != nil {
			rep.Error = err.Error()
		} else {
			rep.IndexRebuilt = true
		}
	}
	return rep
}

// rebuildIndex writes a fresh index for a log, covering the valid prefix.
//
// Written to a temporary file and renamed, so a reader either sees the whole
// old index or the whole new one. A half-written index is the one outcome that
// would turn a repair into the damage it was meant to fix.
func rebuildIndex(f *os.File, id threadid.ID, logPath string, upTo int64) error {
	rec, err := threadlog.Open(f, upTo, nil)
	if err != nil {
		return fmt.Errorf("rebuilding: %w", err)
	}

	dir := filepath.Dir(logPath)
	tmp, err := os.CreateTemp(dir, ".fsck-idx-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(rec.Index.AppendTo(nil)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), id.IndexPath(rootOf(logPath, id)))
}

// rootOf recovers the data root from a log path and its id, so the rebuilt
// index lands beside the log rather than wherever the caller was standing.
func rootOf(logPath string, id threadid.ID) string {
	// <root>/threads/<b0>/<b1>/<hex>.log — four components above the root.
	d := filepath.Dir(logPath)        // .../threads/b0/b1
	d = filepath.Dir(filepath.Dir(d)) // .../threads
	return filepath.Dir(d)            // <root>
}
