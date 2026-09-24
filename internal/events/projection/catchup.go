package projection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Recover opens a thread the way the server does, repairing a torn tail, and
// returns its log length after the repair. Serve supplies it from the thread
// cache, which is the one writer a log may have; catch-up never writes a log
// itself.
type Recover func(ctx context.Context, id threadid.ID) (int64, error)

// Report is what one catch-up found.
type Report struct {
	// Scanned is how many logs the walk found.
	Scanned int

	// Closed is how many gaps were closed, and Bytes how much log they
	// replayed.
	Closed int
	Bytes  int64

	// Corrupt are the threads whose log is shorter than their projection,
	// including a projection with no log at all. They are quarantined: see
	// Writer.Quarantined.
	Corrupt []threadid.ID

	// Failed are the threads with a gap that could not be closed. A thread
	// whose projection failed is in the writer's behind set and Run retries
	// it; one that could not be opened at all is only reported.
	Failed []threadid.ID

	// Strays are files under threads/ that are not a thread's log or index
	// at the path its id names.
	Strays []string

	Elapsed time.Duration
}

// CatchUp closes the gap between every log and its projection (docs/03
// "Durability and recovery"). A crash between a log write and the database
// write leaves exactly such a gap, and serve runs this before the listener
// accepts a request, so nothing is served from a projection that is behind.
// It is also the Sweep a batch reporting dropped events calls.
//
// The walk opens no log. The projected lengths come from one query, the log
// sizes from the directory entries, and a thread whose two agree costs a
// stat. Only a thread with a gap is opened, through reopen, and projected up
// to the length reopen reports: a torn final frame is truncated by that
// open, so the projection stops at the last whole frame rather than failing
// on the torn one.
//
// A log shorter than its projection cannot come from a crash, because the
// projection is written after the log, so it is corruption. It is logged at
// error level and quarantined, and the rest keep serving; nothing about it
// is rewritten. A catch-up replaces the quarantine with what it found, so a
// thread an operator repaired leaves it at the next.
//
// Each thread's gap is closed in its own transaction. A process that dies
// partway leaves every finished thread consistent and every other one still
// showing its gap, and the next catch-up picks up from there.
//
// The error is for a catch-up that could not look: the query, or the walk.
// What it found wrong with a thread is in the Report.
func (w *Writer) CatchUp(ctx context.Context, reopen Recover) (Report, error) {
	start := time.Now()
	var rep Report
	if reopen == nil {
		return rep, errors.New("projection: CatchUp needs a Recover to open a thread with a gap")
	}

	projected := map[threadid.ID]int64{}
	err := w.db.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		rows, err := q.ListProjectionLengths(ctx)
		for _, r := range rows {
			projected[threadid.ID(r.ThreadID)] = r.LogLength
		}
		return err
	})
	if err != nil {
		return rep, fmt.Errorf("projection: catch-up: reading projection_state: %w", err)
	}

	logs, strays, err := w.walk()
	if err != nil {
		return rep, fmt.Errorf("projection: catch-up: %w", err)
	}
	rep.Scanned, rep.Strays = len(logs), strays
	for _, path := range strays {
		w.log.Warn("a file under threads/ is not a thread's log or index", "path", path)
	}

	corrupt := map[threadid.ID]bool{}
	quarantine := func(id threadid.ID, logLength, at int64) {
		corrupt[id] = true
		w.log.Error("a thread's log is shorter than its projection; it is quarantined and served as 503 "+
			"until repaired. A crash cannot cause this, because the projection is written after the log, "+
			"unless storage.fsync is \"never\", where a crash can lose log writes the database kept",
			"thread", id, "log_length", logLength, "projected", at)
	}

	ids := make([]threadid.ID, 0, len(logs))
	for id := range logs {
		ids = append(ids, id)
	}
	slices.Sort(ids) // a log line order an operator can follow, and a test can

	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		size := logs[id]
		at, known := projected[id]
		if !known {
			at = threadlog.FileHeaderSize // a thread the projection never saw
		}
		switch {
		case size == at:
			continue
		case size < at:
			quarantine(id, size, at)
			continue
		}

		// A gap. Open the thread for its real length, which a torn tail makes
		// shorter than the file.
		length, err := reopen(ctx, id)
		if err != nil {
			rep.Failed = append(rep.Failed, id)
			w.log.Error("a thread with a projection gap could not be opened; it stays behind",
				"thread", id, "log_size", size, "projected", at, "err", err)
			continue
		}
		switch {
		case length < at:
			quarantine(id, length, at)
			continue
		case length == at:
			continue // the gap was a torn tail, and the open removed it
		}
		if err := w.project(ctx, id, length); err != nil {
			rep.Failed = append(rep.Failed, id)
			continue
		}
		rep.Closed++
		rep.Bytes += length - at
	}

	// A projection whose log is gone is the shortest log there is.
	for id, at := range projected {
		if _, ok := logs[id]; !ok {
			quarantine(id, 0, at)
		}
	}

	for id := range corrupt {
		rep.Corrupt = append(rep.Corrupt, id)
	}
	slices.Sort(rep.Corrupt)
	rep.Elapsed = time.Since(start)
	w.mu.Lock()
	w.quarantined = corrupt
	w.last = &rep
	w.mu.Unlock()

	w.log.Info("projection catch-up finished",
		"scanned", rep.Scanned, "closed", rep.Closed, "bytes", rep.Bytes,
		"corrupt", len(rep.Corrupt), "failed", len(rep.Failed), "strays", len(rep.Strays),
		"elapsed", rep.Elapsed)
	return rep, nil
}

// Quarantined reports whether the last catch-up found thread id's log
// shorter than its projection. Such a thread is served as 503 until an
// operator repairs it (docs/03).
func (w *Writer) Quarantined(id threadid.ID) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.quarantined[id]
}

// LastCatchUp is the report of the last catch-up that finished, for the
// health and metrics endpoints; false before the first.
func (w *Writer) LastCatchUp() (Report, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.last == nil {
		return Report{}, false
	}
	return *w.last, true
}

// walk lists every log under threads/ with its size, from the directory
// entries alone, following the two-level shard layout (threadid.Dir). A file
// that is not a log or index at the path its id names is a stray, which is
// how a log in the wrong shard is caught. No threads directory at all is a
// data directory with no threads yet.
func (w *Writer) walk() (map[threadid.ID]int64, []string, error) {
	top := filepath.Join(w.root, threadid.ThreadsDir)
	logs := map[threadid.ID]int64{}
	var strays []string

	read := func(dir string) ([]fs.DirEntry, error) {
		es, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", dir, err)
		}
		return es, nil
	}
	l1, err := os.ReadDir(top)
	if errors.Is(err, fs.ErrNotExist) {
		return logs, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", top, err)
	}
	for _, a := range l1 {
		pa := filepath.Join(top, a.Name())
		if !a.IsDir() {
			strays = append(strays, pa)
			continue
		}
		l2, err := read(pa)
		if err != nil {
			return nil, nil, err
		}
		for _, b := range l2 {
			pb := filepath.Join(pa, b.Name())
			if !b.IsDir() {
				strays = append(strays, pb)
				continue
			}
			files, err := read(pb)
			if err != nil {
				return nil, nil, err
			}
			for _, f := range files {
				path := filepath.Join(pb, f.Name())
				if filepath.Ext(f.Name()) == threadid.IndexExt && !f.IsDir() {
					continue
				}
				id, err := threadid.FromLogPath(path)
				// FromLogPath also refuses a file in a shard its id does not name.
				if err != nil || f.IsDir() || filepath.Ext(path) != threadid.LogExt {
					strays = append(strays, path)
					continue
				}
				info, err := f.Info()
				if err != nil {
					return nil, nil, fmt.Errorf("reading the size of %s: %w", path, err)
				}
				logs[id] = info.Size()
			}
		}
	}
	return logs, strays, nil
}
