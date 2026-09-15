package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

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

	// DriftBytes is how far a projection is behind this log. Not yet
	// measurable: projection_state arrives with TKT-01M29BYYJ8 in Phase 1.
	DriftBytes *int64 `json:"drift_bytes,omitempty"`
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

	// DriftChecked is false until there is a database to compare against, and
	// is reported so a clean run cannot be mistaken for a run that checked.
	DriftChecked bool `json:"drift_checked"`
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

Safe against a live data directory. The log is never written, and fsck scans
each log to the size it had when opened, so an append arriving mid-scan is
simply not counted yet. --rebuild writes only the index, which is derived and
disposable: the worst a concurrent server can do to it is leave one that needs
rebuilding again.

exit codes:
  0  clean
  1  corruption found
  2  usage
  3  drift found between a log and its projection

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

	paths, err := findLogs(*root)
	if err != nil {
		return err
	}

	sum := fsckSummary{DriftChecked: false}
	for _, p := range paths {
		rep := checkThread(p, *rebuild)
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

	if *asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(sum); err != nil {
			return err
		}
	} else {
		writeFsckText(w, sum, *quiet)
	}

	if sum.Damaged > 0 {
		return errCorrupt{fmt.Errorf("%d of %d thread(s) damaged", sum.Damaged, sum.Threads)}
	}
	return nil
}

func writeFsckText(w io.Writer, sum fsckSummary, quiet bool) {
	for _, r := range sum.Reports {
		switch {
		case r.Error != "":
			fmt.Fprintf(w, "DAMAGED  %s  %s\n", r.ID, r.Error)
		case r.Damaged > 0:
			fmt.Fprintf(w, "DAMAGED  %s  %d record(s) failed their checksum, %d record(s) readable\n",
				r.ID, r.Damaged, r.Records)
		case r.Truncated > 0:
			fmt.Fprintf(w, "TORN     %s  %d byte(s) past the last complete record; an interrupted write\n",
				r.ID, r.Truncated)
		case !quiet:
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
	} else {
		fmt.Fprintf(w, "no corruption found\n")
	}
	if !sum.DriftChecked {
		fmt.Fprintf(w, "projection drift not checked: there is no database yet (TKT-01M29BYYJ8)\n")
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
	rep.ID = id.String()

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
