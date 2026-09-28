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
	"time"

	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/lock"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

const reindexUsage = `usage: ketju reindex [--root DIR] [--projections] [--search] [--json]

Rebuilds the database's projection tables (threads, posts, tags, mentions)
from the thread logs under DIR. This is how a lost or damaged database is
recovered: content comes back from the logs, and members, groups and
permissions come back only from a database backup. See the README,
"Recovering a lost or damaged database".

--projections is what reindex does, and the default. --search rebuilds the
search index, which arrives in Phase 2.

The database is created if it is missing. Only the projection tables are
emptied and rewritten; members, sessions, read positions and settings are
left as they are. The logs are only read.

Reindex also raises the next member id above the largest member id the logs
name, so a member who joins later never takes the id of an old post's author.
A post whose author the database lacks shows as a deleted member. Over a
database that is not behind the logs, this changes nothing.

Not safe beside a running server, so it refuses to start while ketju serve
holds the data directory, and holds it itself until it finishes, so serve
cannot start midway. Progress goes to stderr, at most every 2 seconds; the
report goes to stdout.

exit codes:
  0  every thread projected (a torn final write is left out and reported)
  1  a thread's log is damaged, or its records contradict each other; the
     other threads are projected, and the report names each failed thread
     and why (ketju fsck details any damage)
  2  usage, or DIR has no threads directory
  4  the data directory is in use, or the database or a log could not be
     read or written; the run stops there

`

// reindexOpts are the seams a test needs. The zero value is what the binary
// runs with.
type reindexOpts struct {
	// every is the least time between progress lines; zero means 2 s.
	every time.Duration

	// now reads the clock the progress lines are spaced by; nil is time.Now.
	now func() time.Time
}

// reindexSummary is the report, as --json prints it.
type reindexSummary struct {
	Threads    int             `json:"threads"`
	Projected  int             `json:"projected"`
	Bytes      int64           `json:"bytes"`
	Torn       []string        `json:"torn"`
	Unfinished []string        `json:"unfinished"`
	Failed     []reindexFailed `json:"failed"`
	Strays     []string        `json:"strays"`
	ElapsedMS  int64           `json:"elapsed_ms"`

	// Members is how many members the database has. Zero with threads to
	// show means the database came back without its backup.
	Members int64 `json:"members"`

	// MaxMemberID is the largest member id the logs name, as author,
	// mention or the actor of any record. MemberIDFloor is the id every new
	// member is given one above, and MemberIDFloorWas what it was before
	// this run: reindex raised it when the database was behind the logs, so
	// no new member takes an id an old post names.
	MaxMemberID      int64 `json:"max_member_id"`
	MemberIDFloor    int64 `json:"member_id_floor"`
	MemberIDFloorWas int64 `json:"member_id_floor_was"`
}

type reindexFailed struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

func runReindex(args []string) error {
	ctx, stop := signalContext()
	defer stop()
	return reindexMain(ctx, os.Stdout, os.Stderr, args, reindexOpts{})
}

func reindexMain(ctx context.Context, out, errOut io.Writer, args []string, o reindexOpts) error {
	fs_ := flag.NewFlagSet("reindex", flag.ContinueOnError)
	fs_.SetOutput(errOut)
	root := fs_.String("root", ".", "data directory")
	projections := fs_.Bool("projections", true, "rebuild the projection tables (the default)")
	search := fs_.Bool("search", false, "rebuild the search index (not yet built)")
	asJSON := fs_.Bool("json", false, "print the report as JSON instead of text")
	fs_.Usage = func() {
		fmt.Fprint(fs_.Output(), reindexUsage)
		fs_.PrintDefaults()
	}
	if err := fs_.Parse(args); err != nil {
		return errUsage{err}
	}
	if fs_.NArg() != 0 {
		fs_.Usage()
		return errUsage{errors.New("reindex takes no positional arguments")}
	}
	if *search {
		return errUsage{errors.New("reindex --search: there is no search index to rebuild yet; " +
			"it arrives with full-text search (TKT-01M29CRJ3B)")}
	}
	if !*projections {
		// With search not yet built, the projections are all there is to do;
		// rebuilding them anyway would do what the caller turned off.
		return errUsage{errors.New("reindex --projections=false leaves nothing to rebuild")}
	}
	every := o.every
	if every <= 0 {
		every = 2 * time.Second
	}
	now := o.now
	if now == nil {
		now = time.Now
	}

	// A mistyped --root would otherwise get a new, empty database and report
	// success over nothing. The threads directory is what makes it a data
	// directory; serve creates it at its first start.
	threadsDir := filepath.Join(*root, threadid.ThreadsDir)
	if fi, err := os.Stat(threadsDir); err != nil || !fi.IsDir() {
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return errUsage{fmt.Errorf("reindex: %s has no threads directory; is --root the data directory?", *root)}
		}
		return fmt.Errorf("reindex: %w", err)
	}

	lk, err := lock.Acquire(*root, "reindex")
	if err != nil {
		var held *lock.HeldError
		if errors.As(err, &held) {
			return fmt.Errorf("reindex: %w Reindex rewrites the tables a running server reads and writes: "+
				"stop ketju serve, then run reindex again", err)
		}
		return fmt.Errorf("reindex: %w", err)
	}
	defer lk.Release()

	if err := os.MkdirAll(filepath.Join(*root, "db"), 0o750); err != nil {
		return fmt.Errorf("reindex: %w", err)
	}
	dbPath := filepath.Join(*root, "db", "ketju.db")
	d, err := db.Open(ctx, db.Options{Path: dbPath})
	if err != nil {
		return fmt.Errorf("reindex: opening %s: %w", dbPath, err)
	}
	defer d.Close()
	if err := d.Migrate(ctx); err != nil {
		return fmt.Errorf("reindex: migrating %s: %w", dbPath, err)
	}

	w, err := projection.New(projection.Options{
		DB: d, Root: *root,
		// Reindex subscribes the writer to no bus, so nothing can report a
		// dropped event for this to sweep up after.
		Sweep:  func(context.Context) error { return errors.New("reindex has no event bus") },
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		return fmt.Errorf("reindex: %w", err)
	}

	var last time.Time
	progress := func(p projection.ReindexProgress) {
		switch t := now(); {
		case p.Done == 0:
			last = t
			fmt.Fprintf(errOut, "reindex: %d thread(s), %s of log, under %s\n", p.Total, bytesText(p.TotalBytes), *root)
		case p.Done == p.Total:
			// The summary follows.
		case t.Sub(last) >= every:
			last = t
			fmt.Fprintf(errOut, "reindex: %d/%d threads, %s of %s%s\n",
				p.Done, p.Total, bytesText(p.Bytes), bytesText(p.TotalBytes), rateText(p))
		}
	}
	rep, err := w.Reindex(ctx, progress)
	switch {
	case err != nil && !rep.Cleared:
		return fmt.Errorf("reindex: %w; the database was not changed", err)
	case err != nil:
		return fmt.Errorf("reindex: %w; the projection tables are partly rebuilt: run reindex again "+
			"(or start serve, whose catch-up finishes the job)", err)
	}

	var members int64
	if err := d.Read(ctx, func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&members)
	}); err != nil {
		return fmt.Errorf("reindex: counting members: %w", err)
	}

	sum := reindexSummary{
		Threads: rep.Threads, Projected: rep.Projected, Bytes: rep.Bytes,
		Torn: ids(rep.Torn), Unfinished: ids(rep.Unfinished), Failed: []reindexFailed{},
		Strays: rep.Strays, ElapsedMS: rep.Elapsed.Milliseconds(), Members: members,
		MaxMemberID: rep.MaxMemberID, MemberIDFloor: rep.FloorAfter, MemberIDFloorWas: rep.FloorBefore,
	}
	if sum.Strays == nil {
		sum.Strays = []string{}
	}
	for _, f := range rep.Failed {
		sum.Failed = append(sum.Failed, reindexFailed{ID: f.Thread.String(), Error: f.Err.Error()})
	}
	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(sum); err != nil {
			return err
		}
	} else {
		writeReindexText(out, sum, rep.Elapsed)
	}
	if members == 0 && rep.Projected > 0 {
		fmt.Fprintf(errOut, "reindex: WARNING: the database has no members, so every post shows as "+
			"written by a deleted member. New members are given ids above every id the posts name, and "+
			"no post is attributed to them. To bring the members back, restore the database from a "+
			"backup and run reindex again before starting ketju serve.\n")
	}

	if len(rep.Failed) > 0 {
		return errCorrupt{fmt.Errorf("%d of %d thread(s) have a log that cannot be projected; "+
			"the report names each and why, and ketju fsck details any damage",
			len(rep.Failed), rep.Threads)}
	}
	return nil
}

func writeReindexText(w io.Writer, s reindexSummary, elapsed time.Duration) {
	for _, id := range s.Torn {
		fmt.Fprintf(w, "TORN     %s  its last write was interrupted and is left out; "+
			"serve cuts it off when it opens the thread\n", id)
	}
	for _, id := range s.Unfinished {
		fmt.Fprintf(w, "EMPTY    %s  holds only its file header: its creation never finished\n", id)
	}
	for _, f := range s.Failed {
		fmt.Fprintf(w, "FAILED   %s  %s\n", f.ID, f.Error)
	}
	for _, p := range s.Strays {
		fmt.Fprintf(w, "STRAY    %s  is not a thread's log or index at the path its id names\n", p)
	}
	fmt.Fprintf(w, "reindexed %d of %d thread(s), %s of log, in %s\n",
		s.Projected, s.Threads, bytesText(s.Bytes), elapsed.Round(time.Millisecond))
	if len(s.Failed) > 0 {
		fmt.Fprintf(w, "%d thread(s) FAILED and have no projection\n", len(s.Failed))
	}
	if s.MemberIDFloor > s.MemberIDFloorWas {
		fmt.Fprintf(w, "member ids: the logs name ids up to %d and the database had handed out up to %d; "+
			"new members now get ids above %d, and a post by an id the database lacks shows as a deleted member\n",
			s.MaxMemberID, s.MemberIDFloorWas, s.MemberIDFloor)
	} else {
		fmt.Fprintf(w, "member ids: the logs name ids up to %d; new members get ids above %d, unchanged\n",
			s.MaxMemberID, s.MemberIDFloor)
	}
}

func ids(in []threadid.ID) []string {
	out := make([]string, len(in))
	for i, id := range in {
		out[i] = id.String()
	}
	return out
}

// bytesText spells a byte count for a person.
func bytesText(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// rateText is the throughput so far and the time left at that rate.
func rateText(p projection.ReindexProgress) string {
	secs := p.Elapsed.Seconds()
	if secs <= 0 || p.Bytes <= 0 {
		return ""
	}
	rate := float64(p.Bytes) / secs
	left := time.Duration(float64(p.TotalBytes-p.Bytes) / rate * float64(time.Second))
	return fmt.Sprintf(", %s/s, about %s left", bytesText(int64(rate)), left.Round(time.Second))
}
