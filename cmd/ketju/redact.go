package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/events/search"
	"github.com/terva-sh/ketju/internal/fsutil"
	"github.com/terva-sh/ketju/internal/jobs"
	"github.com/terva-sh/ketju/internal/lock"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/store/blob"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

const redactUsage = `usage: ketju redact --root DIR --operator HANDLE --reason TEXT [--title TEXT] [--dry-run] THREAD ORDINAL|title
       ketju redact --root DIR --resume [--dry-run]

Hard-redacts a post, or a thread's title, for a legal takedown or an erasure
request. It is the one operation that rewrites a thread log, so it runs with
ketju serve stopped and holds the data directory for its whole run.

The first form redacts post ORDINAL of thread THREAD (its base32 id, as URLs
spell it): every revision's text, mentions, edit reasons and attachment list
are removed from the log, and from every copy a split or merge made of the
post, in every thread. Each rewritten frame keeps its length, so the log
only grows. A Redacted record is appended, the post becomes a tombstone to
everyone, moderators included, and blobs no other post names are deleted.
Quotes of the post in other posts are listed and left alone: redact them
separately if the takedown covers them. OPERATOR must be an active admin, and
the reason is required. Both are recorded in the log and the audit log.

With title in place of ORDINAL it redacts the thread's title instead: every
title and slug the thread has had is blanked, and the thread is renamed to
"Title removed", or to --title TEXT. A title may be redacted more than once.

Before touching any log, the command writes redaction.journal to the data
directory. While it exists, ketju serve refuses to start. If the command is
interrupted, the second form, ketju redact --root DIR --resume, finishes it;
so does running the first form again for the same target.

--dry-run writes nothing: it prints the plan, or with a redaction pending,
the work that remains.

The search index loses the redacted text too: each thread's rows are
brought current after its log is rewritten, the row of every redacted post
is deleted and a redacted title's row rewritten, and then the database is
compacted (VACUUM) and its write-ahead log folded in, every run, so no page
of the database file keeps the text. That rewrites the database and needs
free disk of about its size. A database not yet migrated to the search index
has nothing to clear.

Backups and filesystem snapshots taken before the run still hold the text.
The command cannot reach them.

exit codes:
  0  redacted or resumed, or with --dry-run the plan printed
  1  a log is damaged or cannot be read (run ketju fsck), or no post, thread
     or member has the name given
  2  usage, including an operator who is not an active admin, a post
     already redacted, and a different target while a redaction is pending
  4  the data directory is in use, or a read or write failed; rerun to resume

`

// journalName is the redaction journal, in the data directory.
const journalName = "redaction.journal"

// defaultReplacement is a redacted title's replacement unless --title names
// one (docs/03 "Hard redaction").
const defaultReplacement = "Title removed"

// redactOpts are the seams a test needs. The zero value is what the binary
// runs with.
type redactOpts struct {
	// now is the clock; nil is time.Now.
	now func() time.Time

	// entropy is where a journal id's random bits come from; nil is
	// crypto/rand.
	entropy io.Reader

	// fail, when set, is called at each named step and a non-nil answer
	// stops the run there, as a crash would. A step inside a transaction
	// rolls it back.
	fail func(step string) error
}

func (o redactOpts) clock() time.Time {
	if o.now != nil {
		return o.now()
	}
	return time.Now()
}

func (o redactOpts) step(name string) error {
	if o.fail != nil {
		return o.fail(name)
	}
	return nil
}

// redactJournal is redaction.journal: everything a resumed run needs, fixed
// before the first log is touched. The attachment hashes are read from the
// logs as they were then, since a swapped log no longer lists them (Terva
// review 1345).
type redactJournal struct {
	ID          string          `json:"id"`
	OperatorID  int64           `json:"operator_id"`
	Reason      string          `json:"reason"`
	CreatedAt   int64           `json:"created_at"`
	Named       redactTarget    `json:"named"`
	Replacement string          `json:"replacement,omitempty"`
	Targets     []redactTarget  `json:"targets"`
	Quotes      []redactQuote   `json:"quotes"`
	Skipped     []redactSkipped `json:"skipped,omitempty"`
}

// redactTarget is one post to clear, or a thread's title.
type redactTarget struct {
	Thread      string   `json:"thread"`
	Ordinal     uint32   `json:"ordinal,omitempty"`
	Title       bool     `json:"title,omitempty"`
	Revisions   []uint32 `json:"revisions,omitempty"`
	Attachments []string `json:"attachments,omitempty"`
}

// redactQuote is a revision of another post that quotes a target.
type redactQuote struct {
	Thread   string `json:"thread"`
	Ordinal  uint32 `json:"ordinal"`
	Revision uint32 `json:"revision"`
	Quotes   uint32 `json:"quotes"`
}

// redactSkipped is a member of the copy set with nothing to clear: its log
// or its post is gone, or it was redacted before.
type redactSkipped struct {
	Thread  string `json:"thread"`
	Ordinal uint32 `json:"ordinal"`
	Why     string `json:"why"`
}

func (t redactTarget) String() string {
	if t.Title {
		return t.Thread + " title"
	}
	return t.Thread + "/" + strconv.FormatUint(uint64(t.Ordinal), 10)
}

// names reports whether t and u are the same post, or the same title.
func (t redactTarget) names(u redactTarget) bool {
	return t.Thread == u.Thread && t.Ordinal == u.Ordinal && t.Title == u.Title
}

func (t redactTarget) id() threadid.ID {
	id, _ := threadid.Parse(t.Thread)
	return id
}

// postRef names a post across threads.
type postRef struct {
	Thread  threadid.ID
	Ordinal uint32
}

func runRedact(args []string) error {
	ctx, stop := signalContext()
	defer stop()
	return redactMain(ctx, os.Stdout, os.Stderr, args, redactOpts{})
}

// redactArgs is the command line, parsed and checked.
type redactArgs struct {
	root, operator, reason string
	title                  *string // --title, when given
	dryRun, resume         bool
	named                  redactTarget
}

func parseRedactArgs(errOut io.Writer, args []string) (redactArgs, error) {
	fs_ := flag.NewFlagSet("redact", flag.ContinueOnError)
	fs_.SetOutput(errOut)
	var a redactArgs
	fs_.StringVar(&a.root, "root", ".", "data directory")
	fs_.StringVar(&a.operator, "operator", "", "the handle of the admin running the redaction")
	fs_.StringVar(&a.reason, "reason", "", "why, recorded in the log and the audit log")
	title := fs_.String("title", "", "a redacted title's replacement (default \""+defaultReplacement+"\")")
	fs_.BoolVar(&a.dryRun, "dry-run", false, "print the plan, or the remaining work, and write nothing")
	fs_.BoolVar(&a.resume, "resume", false, "finish the pending redaction")
	fs_.Usage = func() {
		fmt.Fprint(fs_.Output(), redactUsage)
		fs_.PrintDefaults()
	}
	if err := fs_.Parse(args); err != nil {
		return a, errUsage{err}
	}
	set := map[string]bool{}
	fs_.Visit(func(f *flag.Flag) { set[f.Name] = true })
	bad := func(msg string) (redactArgs, error) {
		fs_.Usage()
		return a, errUsage{errors.New("redact: " + msg)}
	}

	if a.resume {
		if set["operator"] || set["reason"] || set["title"] || fs_.NArg() != 0 {
			return bad("--resume takes only --root and --dry-run: the operator, the reason and the targets are in the journal")
		}
		return a, nil
	}
	switch {
	case fs_.NArg() != 2:
		return bad("name a THREAD and an ORDINAL, or title; or finish a pending redaction with --resume")
	case strings.TrimSpace(a.operator) == "":
		return bad("--operator HANDLE is required: the admin running the redaction")
	case strings.TrimSpace(a.reason) == "":
		return bad("--reason TEXT is required")
	}
	id, err := threadid.Parse(fs_.Arg(0))
	if err != nil {
		return bad(fmt.Sprintf("%q is not a thread id: %v", fs_.Arg(0), err))
	}
	a.named.Thread = id.String()
	if fs_.Arg(1) == "title" {
		a.named.Title = true
		if set["title"] {
			t, err := threads.CheckTitle(*title)
			if err != nil {
				return bad("--title: " + strings.TrimPrefix(err.Error(), "threads: "))
			}
			a.title = &t
		}
		return a, nil
	}
	if set["title"] {
		return bad("--title replaces a redacted title, so it goes only with the target title")
	}
	n, err := strconv.ParseUint(fs_.Arg(1), 10, 32)
	if err != nil || n == 0 {
		return bad(fmt.Sprintf("%q is not a post ordinal, or title", fs_.Arg(1)))
	}
	a.named.Ordinal = uint32(n)
	return a, nil
}

func redactMain(ctx context.Context, out, errOut io.Writer, args []string, o redactOpts) error {
	a, err := parseRedactArgs(errOut, args)
	if err != nil {
		return err
	}

	threadsDir := filepath.Join(a.root, threadid.ThreadsDir)
	if fi, err := os.Stat(threadsDir); err != nil || !fi.IsDir() {
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return errUsage{fmt.Errorf("redact: %s has no threads directory; is --root the data directory?", a.root)}
		}
		return fmt.Errorf("redact: %w", err)
	}
	if err := needDatabase("redact", a.root); err != nil {
		return err
	}
	lk, err := lock.Acquire(a.root, "redact")
	if err != nil {
		var held *lock.HeldError
		if errors.As(err, &held) {
			return fmt.Errorf("redact: %w Redaction rewrites thread logs a running server holds open: "+
				"stop ketju serve, then run redact again", err)
		}
		return fmt.Errorf("redact: %w", err)
	}
	defer lk.Release()

	d, err := openDatabase(ctx, "redact", a.root, false)
	if err != nil {
		return err
	}
	defer d.Close()
	r := &redactor{root: a.root, db: d, out: out, o: o}
	if r.search, err = searchIndexer(ctx, d, a.root); err != nil {
		return err
	}

	// A pending journal comes first, before any check on the post's state:
	// right after the named post's swap it already reads as redacted, and
	// refusing it then would leave the work unfinishable (Terva review 1360).
	j, err := readJournal(a.root)
	switch {
	case err != nil:
		return err
	case j != nil && (a.resume || j.Named.names(a.named)):
		if a.dryRun {
			return r.report(ctx, j)
		}
		fmt.Fprintf(out, "resuming redaction %s of %s\n", j.ID, j.Named)
		return r.run(ctx, j)
	case j != nil:
		return errUsage{fmt.Errorf("redact: redaction %s of %s is pending; finish it first with "+
			"ketju redact --root %s --resume", j.ID, j.Named, a.root)}
	case a.resume:
		return errUsage{fmt.Errorf("redact: no redaction is pending in %s; there is nothing to resume", a.root)}
	}

	j, err = r.plan(ctx, a)
	if err != nil {
		return err
	}
	r.printPlan(j)
	if err := r.printSearchRows(ctx, j); err != nil {
		return err
	}
	if a.dryRun {
		fmt.Fprintln(out, "dry run: nothing was written")
		return nil
	}
	if err := writeJournal(a.root, j); err != nil {
		return fmt.Errorf("redact: writing the journal: %w", err)
	}
	if err := o.step("journal"); err != nil {
		return err
	}
	return r.run(ctx, j)
}

// redactor is one run over a data directory whose lock it holds.
type redactor struct {
	root string
	db   *db.DB
	out  io.Writer
	o    redactOpts

	// search keeps the search index's copy of the text in step (docs/03
	// "Hard redaction", "The search index"); nil when the database has no
	// search index yet.
	search *search.Indexer
}

// searchIndexer is the indexer the run brings the search index current
// with, or nil for a database whose migrations stop before the search index,
// which has nothing to clear: serve's migration later builds its index from
// the already-redacted logs. A database holding only some of the search
// tables, as one dropped by hand leaves it, is refused before anything is
// touched, since the tables left may hold the text and the run could not say
// it had cleared them.
func searchIndexer(ctx context.Context, d *db.DB, root string) (*search.Indexer, error) {
	var missing []string
	if err := d.Read(ctx, func(_ *dbq.Queries, tx *sql.Tx) error {
		var err error
		missing, err = db.MissingSearchTables(ctx, tx)
		return err
	}); err != nil {
		return nil, fmt.Errorf("redact: looking for the search index: %w", err)
	}
	switch {
	case len(missing) == len(db.SearchTables)+1:
		return nil, nil
	case len(missing) > 0:
		return nil, fmt.Errorf("redact: the database lacks the search table(s) %s but has the others, so they "+
			"were dropped by hand, and the ones left may hold the text: restore the database from a backup, "+
			"then run redact again; nothing was changed", strings.Join(missing, ", "))
	}
	x, err := search.New(search.Options{DB: d, Root: root, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		return nil, fmt.Errorf("redact: %w", err)
	}
	return x, nil
}

func journalPath(root string) string { return filepath.Join(root, journalName) }

// readJournal reads the pending journal, or nil when there is none.
func readJournal(root string) (*redactJournal, error) {
	b, err := os.ReadFile(journalPath(root))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redact: reading %s: %w", journalPath(root), err)
	}
	var j redactJournal
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&j); err != nil || j.ID == "" || len(j.Targets) == 0 {
		return nil, errCorrupt{fmt.Errorf("redact: %s cannot be read (%v); it is the only record of a "+
			"redaction in progress, so keep it and ask for help rather than deleting it", journalPath(root), err)}
	}
	return &j, nil
}

// writeJournal writes the journal durably: to a temporary file, fsynced,
// renamed into place, and the directory fsynced, all before any log is
// touched (Terva reviews 1271 and 1278).
func writeJournal(root string, j *redactJournal) error {
	b, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFile(journalPath(root), journalPath(root)+".tmp", 0o600, append(b, '\n'))
}

// plan resolves the operator, checks the named target, and finds everything
// to clear. It writes nothing.
func (r *redactor) plan(ctx context.Context, a redactArgs) (*redactJournal, error) {
	var op dbq.User
	err := r.db.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		op, err = q.GetUserByHandle(ctx, a.operator)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound{fmt.Errorf("redact: no member has the handle %q", a.operator)}
	}
	if err != nil {
		return nil, fmt.Errorf("redact: looking up %q: %w", a.operator, err)
	}
	if op.Role != domain.RoleAdmin || op.Status != domain.StatusActive {
		return nil, errUsage{fmt.Errorf("redact: %s is not an active admin, and only an admin may redact (docs/08)", op.Handle)}
	}

	now := r.o.clock()
	jid, err := newULID(now, r.o.entropy)
	if err != nil {
		return nil, err
	}
	j := &redactJournal{ID: jid, OperatorID: op.ID, Reason: strings.TrimSpace(a.reason), CreatedAt: now.UnixMilli(),
		Named: a.named, Quotes: []redactQuote{}}

	named := a.named.id()
	if _, err := os.Stat(named.LogPath(r.root)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errNotFound{fmt.Errorf("redact: there is no thread %s", a.named.Thread)}
		}
		return nil, fmt.Errorf("redact: %w", err)
	}

	if a.named.Title {
		j.Replacement = defaultReplacement
		if a.title != nil {
			j.Replacement = *a.title
		}
		// No origin to follow, no attachments and no quotes: a split
		// gives the new thread its own title, and nothing quotes a title.
		j.Targets = []redactTarget{{Thread: a.named.Thread, Title: true}}
		if _, err := scanPosts(named.LogPath(r.root), func(threadlog.FrameHeader, any, []byte) error { return nil }); err != nil {
			return nil, err
		}
		return j, nil
	}

	// The copy set: the whole origin component holding the named post,
	// followed both ways to a fixed point (Terva review 1345).
	edges, err := originEdges(ctx, r.root)
	if err != nil {
		return nil, err
	}
	ref := postRef{Thread: named, Ordinal: a.named.Ordinal}
	for _, p := range copySet(edges, ref) {
		t, why, err := r.survey(p)
		if err != nil {
			return nil, err
		}
		if why != "" {
			if p == ref {
				if why == "already redacted" {
					return nil, errUsage{fmt.Errorf("redact: %s is already redacted", a.named)}
				}
				return nil, errNotFound{fmt.Errorf("redact: %s: %s", a.named, why)}
			}
			j.Skipped = append(j.Skipped, redactSkipped{Thread: p.Thread.String(), Ordinal: p.Ordinal, Why: why})
			continue
		}
		j.Targets = append(j.Targets, t)
	}
	if j.Quotes, err = r.quotes(j.Targets); err != nil {
		return nil, err
	}
	return j, nil
}

// survey reads one member of the copy set from its log as it is now: every
// revision, and the attachments every Post and Edit of it lists (Terva
// review 1351). why is set, and t empty, when there is nothing to clear.
func (r *redactor) survey(p postRef) (t redactTarget, why string, err error) {
	path := p.Thread.LogPath(r.root)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return t, "its thread's log is gone", nil
	}
	t = redactTarget{Thread: p.Thread.String(), Ordinal: p.Ordinal}
	var posted, redacted bool
	seen := map[string]bool{}
	if _, err := scanPosts(path, func(h threadlog.FrameHeader, v any, body []byte) error {
		if b, ok := v.(*threadlog.RedactedBody); ok && !b.Title && b.Ordinal == p.Ordinal {
			redacted = true
		}
		if h.Ordinal != p.Ordinal || (h.Type != threadlog.TypePost && h.Type != threadlog.TypeEdit) {
			return nil
		}
		posted = posted || h.Type == threadlog.TypePost
		t.Revisions = append(t.Revisions, h.Revision)
		hashes, err := threadlog.AttachmentsOf(h, body)
		if err != nil {
			return err
		}
		for _, x := range hashes {
			if !seen[x] {
				seen[x] = true
				t.Attachments = append(t.Attachments, x)
			}
		}
		return nil
	}); err != nil {
		return t, "", err
	}
	switch {
	case !posted:
		return redactTarget{}, "the thread has no such post", nil
	case redacted:
		return redactTarget{}, "already redacted", nil
	}
	sort.Strings(t.Attachments)
	return t, "", nil
}

// quotes lists every revision of every other post, in each thread holding a
// target, whose source quotes a target. Quotes are same-thread only.
func (r *redactor) quotes(targets []redactTarget) ([]redactQuote, error) {
	byThread := map[string]map[uint32]bool{}
	for _, t := range targets {
		if byThread[t.Thread] == nil {
			byThread[t.Thread] = map[uint32]bool{}
		}
		byThread[t.Thread][t.Ordinal] = true
	}
	out := []redactQuote{}
	for _, th := range sortedKeys(byThread) {
		want := byThread[th]
		id, _ := threadid.Parse(th)
		if _, err := scanPosts(id.LogPath(r.root), func(h threadlog.FrameHeader, v any, _ []byte) error {
			if want[h.Ordinal] {
				return nil
			}
			var src string
			switch b := v.(type) {
			case *threadlog.PostBody:
				src = b.Source
			case *threadlog.EditBody:
				src = b.Source
			default:
				return nil
			}
			res, err := render.Render(src)
			if err != nil {
				return fmt.Errorf("rendering %s/%d revision %d to find its quotes: %w", th, h.Ordinal, h.Revision, err)
			}
			for _, q := range res.Quotes {
				if want[q.Ordinal] {
					out = append(out, redactQuote{Thread: th, Ordinal: h.Ordinal, Revision: h.Revision, Quotes: q.Ordinal})
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// scanPosts walks every frame of one log, decoding each body this build
// knows, and returns the log's length. A damaged frame, a torn tail or a
// critical record this build does not know stops it: a redaction plans from
// a sound log or not at all.
func scanPosts(path string, fn func(h threadlog.FrameHeader, v any, body []byte) error) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("redact: %w", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("redact: %w", err)
	}
	var bad error
	end := int64(threadlog.FileHeaderSize)
	err = threadlog.ScanFrames(f, st.Size(), func(rec threadlog.ScanRecord) bool {
		if rec.Err != nil {
			bad = errCorrupt{fmt.Errorf("redact: %s: the frame at %d is damaged; run ketju fsck: %w", path, rec.Offset, rec.Err)}
			return false
		}
		v, err := threadlog.DecodeBody(rec.Header.Type, rec.Header.Encoding, rec.Body)
		if err != nil && !threadlog.SkipUnknown(err) {
			bad = errCorrupt{fmt.Errorf("redact: %s: the frame at %d: %w", path, rec.Offset, err)}
			return false
		}
		if err := fn(rec.Header, v, rec.Body); err != nil {
			bad = errCorrupt{fmt.Errorf("redact: %s: the frame at %d: %w", path, rec.Offset, err)}
			return false
		}
		end = rec.Offset + int64(rec.Size)
		return true
	})
	if err != nil {
		return 0, errCorrupt{fmt.Errorf("redact: %s: %w", path, err)}
	}
	if bad != nil {
		return 0, bad
	}
	if end != st.Size() {
		return 0, errCorrupt{fmt.Errorf("redact: %s ends in %d bytes that are not a whole frame; "+
			"start ketju serve once to cut the torn write, stop it, and run redact again", path, st.Size()-end)}
	}
	return end, nil
}

// originEdges reads every log under threads/ and returns, for every post
// with an origin, the post it was copied from. Any log that cannot be read
// stops the plan: a copy in it could not be found.
func originEdges(ctx context.Context, root string) (map[postRef]postRef, error) {
	edges := map[postRef]postRef{}
	err := filepath.WalkDir(filepath.Join(root, threadid.ThreadsDir), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(p) != threadid.LogExt {
			return nil
		}
		id, err := threadid.FromLogPath(p)
		if err != nil {
			return nil // a stray, which catch-up reports; not a thread
		}
		_, err = scanPosts(p, func(h threadlog.FrameHeader, v any, _ []byte) error {
			if b, ok := v.(*threadlog.PostBody); ok && b.Origin != nil {
				edges[postRef{Thread: id, Ordinal: h.Ordinal}] = postRef{Thread: threadid.ID(b.Origin.ThreadID), Ordinal: b.Origin.Ordinal}
			}
			return nil
		})
		return err
	})
	if err != nil {
		var c errCorrupt
		if errors.As(err, &c) {
			return nil, err
		}
		return nil, fmt.Errorf("redact: reading the thread logs for copies: %w", err)
	}
	return edges, nil
}

// copySet is the connected component of the origin graph that holds named:
// its source, its source's source, every copy of any of them, and so on,
// following edges both ways until nothing new is added. It is sorted, and
// always holds named.
func copySet(edges map[postRef]postRef, named postRef) []postRef {
	adj := map[postRef][]postRef{}
	for from, to := range edges {
		adj[from] = append(adj[from], to)
		adj[to] = append(adj[to], from)
	}
	seen := map[postRef]bool{named: true}
	queue := []postRef{named}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, n := range adj[p] {
			if !seen[n] {
				seen[n] = true
				queue = append(queue, n)
			}
		}
	}
	out := make([]postRef, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b postRef) int {
		if a.Thread != b.Thread {
			if a.Thread < b.Thread {
				return -1
			}
			return 1
		}
		return int(a.Ordinal) - int(b.Ordinal)
	})
	return out
}

func (r *redactor) printPlan(j *redactJournal) {
	if j.Named.Title {
		fmt.Fprintf(r.out, "redact the title of thread %s, renaming it to %q\n", j.Named.Thread, j.Replacement)
		return
	}
	fmt.Fprintf(r.out, "redact %s and every copy of it:\n", j.Named)
	for _, t := range j.Targets {
		fmt.Fprintf(r.out, "  %s  revisions %v, attachments %d\n", t, t.Revisions, len(t.Attachments))
	}
	for _, s := range j.Skipped {
		fmt.Fprintf(r.out, "  %s/%d  skipped: %s\n", s.Thread, s.Ordinal, s.Why)
	}
	r.printQuotes(j)
}

func (r *redactor) printQuotes(j *redactJournal) {
	if len(j.Quotes) == 0 {
		return
	}
	fmt.Fprintln(r.out, "quotes of it in other posts, left as they are (redact them separately if the takedown covers them):")
	for _, q := range j.Quotes {
		fmt.Fprintf(r.out, "  %s/%d revision %d quotes #%d\n", q.Thread, q.Ordinal, q.Revision, q.Quotes)
	}
}

// byThread groups the journal's targets by thread, in thread id order.
func byThread(j *redactJournal) (ids []threadid.ID, groups map[threadid.ID][]redactTarget) {
	groups = map[threadid.ID][]redactTarget{}
	for _, t := range j.Targets {
		id := t.id()
		if groups[id] == nil {
			ids = append(ids, id)
		}
		groups[id] = append(groups[id], t)
	}
	slices.Sort(ids)
	return ids, groups
}

// swapped reports whether a thread's log already carries this journal's
// work: every target ordinal marked, or a title marker carrying this
// journal's id. Some but not all of a log's ordinals marked is damage, since
// one atomic swap writes all of them (Terva review 1351).
func swapped(path string, j *redactJournal, ts []redactTarget) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("redact: %w", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false, err
	}
	markers, err := threadlog.Markers(f, st.Size())
	if err != nil {
		return false, errCorrupt{fmt.Errorf("redact: %s: %w", path, err)}
	}
	if ts[0].Title {
		for _, m := range markers {
			if m.Title && m.Journal == j.ID {
				return true, nil
			}
		}
		return false, nil
	}
	marked := map[uint32]bool{}
	for _, m := range markers {
		if !m.Title {
			marked[m.Ordinal] = true
		}
	}
	n := 0
	for _, t := range ts {
		if marked[t.Ordinal] {
			n++
		}
	}
	switch n {
	case 0:
		return false, nil
	case len(ts):
		return true, nil
	}
	return false, errCorrupt{fmt.Errorf("redact: %s carries the Redacted records of %d of its %d posts in "+
		"redaction %s; one swap writes all of them, so the log is damaged. The journal is left in place",
		path, n, len(ts), j.ID)}
}

// run does, or finishes, the journal's work: each log rewritten once, its
// index and projection brought current, the references dropped and the
// audit row written in one transaction, the blobs deleted, and the journal
// removed. Every step is safe to repeat, which is what a resume is.
func (r *redactor) run(ctx context.Context, j *redactJournal) error {
	w, err := projection.New(projection.Options{
		DB: r.db, Root: r.root,
		Sweep:  func(context.Context) error { return errors.New("redact has no event bus") },
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		return fmt.Errorf("redact: %w", err)
	}
	if r.search != nil {
		// An index another version built is emptied here, as serve's start
		// would empty it, and holds no text at all from then on.
		if err := r.search.Prepare(ctx); err != nil {
			return fmt.Errorf("redact: %w", err)
		}
	}
	ids, groups := byThread(j)
	for _, id := range ids {
		ts := groups[id]
		path := id.LogPath(r.root)
		done, err := swapped(path, j, ts)
		if err != nil {
			return err
		}
		if !done {
			if err := r.rewrite(path, j, ts); err != nil {
				return err
			}
		} else if err := os.Remove(path + ".redact"); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("redact: removing a leftover rewrite: %w", err)
		}
		size, err := r.reindex(id)
		if err != nil {
			return err
		}
		if err := r.o.step("index:" + id.String()); err != nil {
			return err
		}
		if _, err := w.ProjectThread(ctx, id, size); err != nil {
			return fmt.Errorf("redact: bringing the projection of %s current: %w", id, err)
		}
		if err := r.o.step("projected:" + id.String()); err != nil {
			return err
		}
		if r.search != nil {
			// Catch-up reads the appended Redacted records: a redacted
			// post's row goes, and a redacted title's row becomes the
			// replacement.
			if err := r.search.IndexThread(ctx, id, size); err != nil {
				return fmt.Errorf("redact: bringing the search index of %s current: %w", id, err)
			}
			if err := r.o.step("searched:" + id.String()); err != nil {
				return err
			}
		}
		fmt.Fprintf(r.out, "rewrote %s\n", id)
	}

	if j.Named.Title {
		if err := r.auditTitle(ctx, j); err != nil {
			return err
		}
	} else {
		if err := r.dropReferences(ctx, j); err != nil {
			return err
		}
		if err := r.o.step("committed"); err != nil {
			return err
		}
		if err := r.deleteBlobs(ctx, j); err != nil {
			return err
		}
	}

	if r.search != nil {
		if err := r.clearSearch(ctx, j); err != nil {
			return err
		}
	}

	if err := fsutil.Remove(journalPath(r.root)); err != nil {
		return fmt.Errorf("redact: removing the journal: %w", err)
	}
	r.printQuotes(j)
	fmt.Fprintf(r.out, "redaction %s of %s is complete. Backups and filesystem snapshots taken before it "+
		"still hold the text.\n", j.ID, j.Named)
	return nil
}

// rewrite replaces one log with its redacted copy: written beside it,
// fsynced, renamed over it, and the directory fsynced.
func (r *redactor) rewrite(path string, j *redactJournal, ts []redactTarget) error {
	src, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("redact: %w", err)
	}
	defer src.Close()
	st, err := src.Stat()
	if err != nil {
		return err
	}
	ro := threadlog.RedactOptions{TimeMS: r.o.clock().UnixMilli(), Operator: uint64(j.OperatorID),
		Reason: j.Reason, Journal: j.ID}
	id := ts[0].id()
	err = fsutil.ReplaceFile(path, path+".redact", st.Mode().Perm(), func(dst *os.File) error {
		var err error
		if ts[0].Title {
			_, err = threadlog.RedactTitle(src, st.Size(), dst,
				threadlog.ThreadRenamedBody{Title: j.Replacement, Slug: threads.Slug(j.Replacement)}, ro)
		} else {
			ordinals := make([]uint32, len(ts))
			for i, t := range ts {
				ordinals[i] = t.Ordinal
			}
			_, err = threadlog.RedactPosts(src, st.Size(), dst, ordinals, ro)
		}
		if errors.Is(err, threadlog.ErrRedact) {
			return errCorrupt{err}
		}
		return err
	}, func() error { return r.o.step("renamed:" + id.String()) })
	if err != nil {
		var c errCorrupt
		if errors.As(err, &c) {
			return err
		}
		return fmt.Errorf("redact: rewriting %s: %w", path, err)
	}
	return r.o.step("synced:" + id.String())
}

// reindex brings a thread's index current with its log, which after a
// rewrite means replaying only the appended frames, and writes it durably.
func (r *redactor) reindex(id threadid.ID) (int64, error) {
	path := id.LogPath(r.root)
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("redact: %w", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	idx, err := os.ReadFile(id.IndexPath(r.root))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, fmt.Errorf("redact: %w", err)
	}
	rec, err := threadlog.Open(f, st.Size(), idx)
	if err != nil {
		return 0, errCorrupt{fmt.Errorf("redact: opening %s: %w", id, err)}
	}
	if rec.Torn != 0 {
		return 0, errCorrupt{fmt.Errorf("redact: %s ends in a torn write of %d bytes", id, rec.Torn)}
	}
	if rec.Outcome != threadlog.Trusted {
		if err := fsutil.WriteFile(id.IndexPath(r.root), id.IndexPath(r.root)+".redact", 0o644, rec.Index.AppendTo(nil)); err != nil {
			return 0, fmt.Errorf("redact: writing the index of %s: %w", id, err)
		}
	}
	return rec.LogLength, nil
}

// searchKeys are the search rows a journal clears: every target post, its
// copies included, and every skipped member of the copy set, whose rows
// should already be gone and are deleted all the same. A title is its
// thread's row 0, which is rewritten rather than deleted.
func searchKeys(j *redactJournal) (posts []postRef, titles []threadid.ID) {
	for _, t := range j.Targets {
		if t.Title {
			titles = append(titles, t.id())
			continue
		}
		posts = append(posts, postRef{Thread: t.id(), Ordinal: t.Ordinal})
	}
	for _, s := range j.Skipped {
		if id, err := threadid.Parse(s.Thread); err == nil {
			posts = append(posts, postRef{Thread: id, Ordinal: s.Ordinal})
		}
	}
	return posts, titles
}

// clearSearch takes the redacted text out of the search index whatever
// state the index was in, then out of every page of the database file
// (docs/03 "Hard redaction", "The search index"). In one transaction it
// deletes the row of every journaled post, rewrites a redacted title's row
// from its replacement, and merges the FTS5 index into one segment
// (optimize). Then VACUUM rebuilds the file from its live pages, so a page
// an earlier edit, delete or merge freed keeps nothing, and a checkpoint
// folds the rebuilt pages into ketju.db and truncates the WAL: until it
// completes, ketju.db still holds the old pages. A checkpoint a reader holds
// back fails the run with the journal kept, and a resume tries again. Every
// part is idempotent, so a resumed run repeats it harmlessly, and it runs on
// every run, not only when a term is found.
func (r *redactor) clearSearch(ctx context.Context, j *redactJournal) error {
	posts, titles := searchKeys(j)
	err := r.db.Write(ctx, func(_ *dbq.Queries, tx *sql.Tx) error {
		for _, p := range posts {
			if err := db.DropSearchDoc(ctx, tx, int64(p.Thread), int64(p.Ordinal)); err != nil {
				return err
			}
		}
		for _, id := range titles {
			if err := r.search.PutTitle(ctx, tx, id, j.Replacement); err != nil {
				return err
			}
		}
		if err := db.OptimizeSearchIndex(ctx, tx); err != nil {
			return err
		}
		return r.o.step("search-rows")
	})
	if err != nil {
		return fmt.Errorf("redact: clearing the search index: %w", err)
	}
	if err := r.db.Vacuum(ctx); err != nil {
		return fmt.Errorf("redact: compacting the database (VACUUM needs free disk of about its size): %w", err)
	}
	if err := r.o.step("vacuumed"); err != nil {
		return err
	}
	if err := r.db.Checkpoint(ctx); err != nil {
		return fmt.Errorf("redact: folding the compacted database into ketju.db: %w; rerun to resume", err)
	}
	return r.o.step("search-cleared")
}

// printSearchRows says how many search rows the run clears or rewrites,
// from a read.
func (r *redactor) printSearchRows(ctx context.Context, j *redactJournal) error {
	if r.search == nil {
		fmt.Fprintln(r.out, "search index: none yet, nothing to clear")
		return nil
	}
	posts, titles := searchKeys(j)
	n := 0
	err := r.db.Read(ctx, func(_ *dbq.Queries, tx *sql.Tx) error {
		count := func(id threadid.ID, ordinal uint32) error {
			var c int
			err := tx.QueryRowContext(ctx, `SELECT count(*) FROM search_docs WHERE thread_id = ? AND ordinal = ?`,
				int64(id), int64(ordinal)).Scan(&c)
			n += c
			return err
		}
		for _, p := range posts {
			if err := count(p.Thread, p.Ordinal); err != nil {
				return err
			}
		}
		for _, id := range titles {
			if err := count(id, 0); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("redact: reading the search index: %w", err)
	}
	fmt.Fprintf(r.out, "search index: %d row(s) to clear or rewrite, then the database is compacted\n", n)
	return nil
}

// postTarget is the named post's audit target.
func postTarget(j *redactJournal) audit.Target {
	return audit.Post(j.Named.id(), j.Named.Ordinal)
}

// dropReferences drops each redacted post's blob references and writes the
// post.redacted row, in one transaction that first looks for that row and
// does nothing if it is there. The row is the marker that the drops
// happened, so a resumed run drops each reference exactly once (Terva
// review 1345). It lists the blobs planned for deletion, never ones
// deleted: those get their own rows as they go (Terva review 1351).
func (r *redactor) dropReferences(ctx context.Context, j *redactJournal) error {
	target := postTarget(j)
	now := r.o.clock()
	return r.db.Write(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
		seen, err := q.AuditExists(ctx, dbq.AuditExistsParams{TargetType: target.Type, TargetID: target.ID, Action: "post.redacted"})
		if err != nil || seen {
			return err
		}
		type dropped struct {
			Post string `json:"post"`
			Blob string `json:"blob"`
		}
		drops, drift := []dropped{}, []dropped{}
		targets := []map[string]any{}
		planned := map[string]bool{}
		for _, t := range j.Targets {
			targets = append(targets, map[string]any{"thread": t.Thread, "ordinal": t.Ordinal, "revisions": t.Revisions})
			for _, h := range t.Attachments {
				planned[h] = true
				var n int64
				err := tx.QueryRowContext(ctx, `SELECT refcount FROM blobs WHERE hash = ?`, h).Scan(&n)
				switch {
				case errors.Is(err, sql.ErrNoRows) || err == nil && n <= 0:
					// Drift: never driven below zero, and noted.
					drift = append(drift, dropped{Post: t.String(), Blob: h})
					continue
				case err != nil:
					return err
				}
				if _, err := q.DropBlobRef(ctx, dbq.DropBlobRefParams{At: now.UnixMilli(), Hash: h}); err != nil {
					return err
				}
				drops = append(drops, dropped{Post: t.String(), Blob: h})
			}
		}
		details := map[string]any{
			"reason": j.Reason, "journal": j.ID, "targets": targets,
			"references_dropped": drops, "blobs_planned": sortedKeys(planned), "quotes": j.Quotes,
		}
		if len(drift) > 0 {
			details["references_missing"] = drift
		}
		if _, err := audit.Append(ctx, q, now, audit.By{Actor: j.OperatorID, Via: "cli"}, "post.redacted", target, details); err != nil {
			return err
		}
		return r.o.step("references")
	})
}

// auditTitle writes the title redaction's one row, which holds no title,
// old or new. It is looked for by the journal's id, since a title may be
// redacted again later.
func (r *redactor) auditTitle(ctx context.Context, j *redactJournal) error {
	target := audit.Thread(j.Named.id())
	return r.db.Write(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
		seen, err := titleAudited(ctx, tx, j)
		if err != nil || seen {
			return err
		}
		blanked, err := r.titleFramesBlanked(j)
		if err != nil {
			return err
		}
		_, err = audit.Append(ctx, q, r.o.clock(), audit.By{Actor: j.OperatorID, Via: "cli"}, "thread.title_redacted", target,
			map[string]any{"reason": j.Reason, "journal": j.ID, "frames_blanked": blanked,
				"replacement_by_operator": j.Replacement != defaultReplacement})
		return err
	})
}

func titleAudited(ctx context.Context, tx *sql.Tx, j *redactJournal) (bool, error) {
	var seen bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM audit_log WHERE action = 'thread.title_redacted'
		AND target_type = ? AND target_id = ? AND json_extract(details_json, '$.journal') = ?)`,
		audit.TypeThread, j.Named.Thread, j.ID).Scan(&seen)
	return seen, err
}

// errFound stops a scan early.
var errFound = errors.New("found")

// titleFramesBlanked counts the title frames before this journal's marker
// in the rewritten log: what the rewrite blanked.
func (r *redactor) titleFramesBlanked(j *redactJournal) (int, error) {
	n := 0
	_, err := scanPosts(j.Named.id().LogPath(r.root), func(h threadlog.FrameHeader, v any, _ []byte) error {
		switch b := v.(type) {
		case *threadlog.ThreadCreatedBody, *threadlog.ThreadRenamedBody:
			n++
		case *threadlog.RedactedBody:
			if b.Title && b.Journal == j.ID {
				return errFound
			}
		}
		return nil
	})
	if errors.Is(err, errFound) {
		return n, nil
	}
	if err == nil {
		err = errCorrupt{fmt.Errorf("redact: %s has no title marker of redaction %s", j.Named.Thread, j.ID)}
	}
	return 0, err
}

// deleteBlobs deletes every planned blob that nothing names any more: no
// log, no avatar, no draft, and no reference. Each goes in its own
// transaction, which deletes the row and writes its blob.deleted row and
// touches no file; the file goes only after that commits (Terva review
// 1360). A blob whose row is already gone, a resumed run's, has its file
// removed and gets no second row.
func (r *redactor) deleteBlobs(ctx context.Context, j *redactJournal) error {
	planned := map[string]bool{}
	for _, t := range j.Targets {
		for _, h := range t.Attachments {
			planned[h] = true
		}
	}
	if len(planned) == 0 {
		return nil
	}
	named, err := jobs.ReferencedInLogs(ctx, r.root, planned)
	if err != nil {
		return fmt.Errorf("redact: the thread logs could not all be read, so no blob was deleted: %w", err)
	}
	for _, h := range sortedKeys(planned) {
		if named[h] {
			fmt.Fprintf(r.out, "kept blob %s: another post still names it\n", h)
			continue
		}
		path, err := blob.Path(r.root, h)
		if err != nil {
			fmt.Fprintf(r.out, "kept %q: not a blob hash\n", h)
			continue
		}
		var gone, deleted bool
		err = r.db.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
			exists, err := q.BlobExists(ctx, h)
			if err != nil {
				return err
			}
			if !exists {
				gone = true
				return nil
			}
			n, err := q.DeleteCollectableBlob(ctx, dbq.DeleteCollectableBlobParams{Hash: h, Cutoff: r.o.clock().UnixMilli()})
			if err != nil || n == 0 {
				return err
			}
			if _, err := audit.Append(ctx, q, r.o.clock(), audit.By{Actor: j.OperatorID, Via: "cli"}, "blob.deleted",
				audit.Blob(h), map[string]any{"journal": j.ID}); err != nil {
				return err
			}
			deleted = true
			return r.o.step("blob-tx:" + h)
		})
		if err != nil {
			return fmt.Errorf("redact: deleting blob %s: %w", h, err)
		}
		if !gone && !deleted {
			fmt.Fprintf(r.out, "kept blob %s: another post, an avatar or a draft still refers to it\n", h)
			continue
		}
		if err := r.o.step("blob-committed:" + h); err != nil {
			return err
		}
		// The thumbnail goes with the bytes it was made from
		// (TKT-01M29CYS9W); it is a cache, so nothing else holds it.
		for _, p := range []string{path, path + ".gc", path + blob.ThumbSuffix} {
			if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("redact: removing %s: %w", p, err)
			}
		}
		if err := fsutil.SyncDir(filepath.Dir(path)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("redact: %w", err)
		}
		if deleted {
			fmt.Fprintf(r.out, "deleted blob %s\n", h)
		}
		if err := r.o.step("blob:" + h); err != nil {
			return err
		}
	}
	return nil
}

// report is a dry run with a journal pending: what is left, and nothing
// written.
func (r *redactor) report(ctx context.Context, j *redactJournal) error {
	fmt.Fprintf(r.out, "redaction %s of %s is pending (created %s, reason %q)\n", j.ID, j.Named,
		time.UnixMilli(j.CreatedAt).UTC().Format(time.RFC3339), j.Reason)
	ids, groups := byThread(j)
	for _, id := range ids {
		done, err := swapped(id.LogPath(r.root), j, groups[id])
		if err != nil {
			return err
		}
		state := "not yet rewritten"
		if done {
			state = "rewritten"
		}
		fmt.Fprintf(r.out, "  log %s: %s\n", id, state)
	}
	var committed bool
	err := r.db.Read(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		if j.Named.Title {
			committed, err = titleAudited(ctx, tx, j)
			return err
		}
		t := postTarget(j)
		committed, err = q.AuditExists(ctx, dbq.AuditExistsParams{TargetType: t.Type, TargetID: t.ID, Action: "post.redacted"})
		return err
	})
	if err != nil {
		return fmt.Errorf("redact: %w", err)
	}
	fmt.Fprintf(r.out, "  audit row: %s\n", map[bool]string{true: "written", false: "not yet written"}[committed])
	for _, t := range j.Targets {
		for _, h := range t.Attachments {
			var row bool
			if err := r.db.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
				var err error
				row, err = q.BlobExists(ctx, h)
				return err
			}); err != nil {
				return fmt.Errorf("redact: %w", err)
			}
			path, _ := blob.Path(r.root, h)
			_, ferr := os.Stat(path)
			fmt.Fprintf(r.out, "  blob %s: row %s, file %s\n", h,
				map[bool]string{true: "present", false: "gone"}[row],
				map[bool]string{true: "present", false: "gone"}[ferr == nil])
		}
	}
	if err := r.printSearchRows(ctx, j); err != nil {
		return err
	}
	fmt.Fprintf(r.out, "dry run: nothing was written; ketju redact --root %s --resume finishes it\n", r.root)
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// newULID is a ULID: 48 bits of Unix milliseconds and 80 random bits, in
// Crockford's base32. The journal's id, which every marker it writes
// carries, so a resumed run can tell its own marker from an earlier one's.
func newULID(now time.Time, entropy io.Reader) (string, error) {
	if entropy == nil {
		entropy = rand.Reader
	}
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], uint64(now.UnixMilli())<<16)
	if _, err := io.ReadFull(entropy, b[6:]); err != nil {
		return "", fmt.Errorf("redact: generating the journal id: %w", err)
	}
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	hi := binary.BigEndian.Uint64(b[:8])
	lo := binary.BigEndian.Uint64(b[8:])
	out := make([]byte, 26)
	// 128 bits as 26 five-bit digits, the first holding the top 3 bits.
	for i := 25; i >= 0; i-- {
		out[i] = alphabet[lo&31]
		lo = lo>>5 | hi<<59
		hi >>= 5
	}
	return string(out), nil
}
