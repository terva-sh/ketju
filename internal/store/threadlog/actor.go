package threadlog

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// LogWriter is the append side of a thread log. *os.File satisfies it.
type LogWriter interface {
	Write(p []byte) (int, error)
	Sync() error
}

// IndexWriter is the sidecar index, the only file this engine ever writes out
// of order. *os.File satisfies it.
type IndexWriter interface {
	WriteAt(p []byte, off int64) (int, error)
	Sync() error
}

// SyncPolicy says when an append is made durable.
type SyncPolicy int

const (
	// SyncGroup drains whatever is already queued and issues one fsync for the
	// whole batch. It never waits to form a batch, which is the entire point:
	// a lone append is exactly as fast as under SyncAlways, while a busy thread
	// collapses its fsyncs for free.
	//
	// "Group commit" usually implies a timer, and a timer makes every
	// low-traffic append pay the window. Drain-with-no-wait has no such cost
	// and no tuning knob to get wrong.
	SyncGroup SyncPolicy = iota

	// SyncAlways fsyncs every record on its own and forms no batches.
	SyncAlways

	// SyncNever never fsyncs the log. For tests, and for anyone who has decided
	// their power supply is somebody else's problem.
	SyncNever
)

func (p SyncPolicy) String() string {
	switch p {
	case SyncGroup:
		return "group"
	case SyncAlways:
		return "always"
	case SyncNever:
		return "never"
	}
	return fmt.Sprintf("SyncPolicy(%d)", int(p))
}

// Append is a request to add one record to a thread.
//
// Ordinal and Revision are absent for a new post on purpose: the actor owns
// post_count, so it assigns them. That is the lock dance this design exists to
// avoid — no caller has to ask "what number am I" and then race another caller
// to use the answer.
type Append struct {
	Type     RecordType
	Encoding BodyEncoding
	Flags    FrameFlags
	Author   uint64
	Body     any

	// Ordinal names the post a record concerns. Required for Edit, Delete and
	// Restore; assigned by the actor for Post; zero for thread events.
	Ordinal uint32

	// TimeMS is the record's timestamp. Zero means now.
	TimeMS int64

	// Require and Refuse are preconditions on the post Ordinal names: every
	// bit in Require must be set on its entry and none in Refuse, or the
	// append fails with [ErrPostState] and writes nothing. The actor checks
	// them against the flags as they stand after every earlier record,
	// including ones queued ahead in the same batch, which is what lets an
	// Edit refuse a deleted post without a Delete slipping in between a
	// caller's check and the write (TKT-01M29CA9BP). Zero checks nothing.
	Require, Refuse EntryFlags

	// RequireAny is a precondition checked the same way: at least one of
	// its bits must be set. A moderator's Restore takes a post that is
	// deleted or hidden, whichever it is (TKT-01M29CRJ5J). Zero checks
	// nothing.
	RequireAny EntryFlags

	// AtLength, when not zero, is the log length the record must be
	// written at: nothing may have been appended since the caller read the
	// log up to it, this batch's earlier records included, or the append
	// fails with [ErrLogMoved] and writes nothing. It lets a caller decide
	// from the log's records, which the index does not keep, and append
	// knowing no record it did not read came in between (a suspension's
	// exact restore, TKT-01M29CP2QK). A log is never zero bytes long, so
	// zero is free to mean "checks nothing".
	AtLength int64
}

// Result is what an append returns once the record is durable.
type Result struct {
	Ordinal   uint32
	Revision  uint32
	LogLength int64
}

// ErrActorClosed means the thread was closed before or during an append.
var ErrActorClosed = errors.New("threadlog: thread is closed")

// ErrPostState means an append's Require, RequireAny or Refuse precondition
// failed: the
// post is not in the state the record needs, such as an Edit of a deleted
// post. Nothing was written.
var ErrPostState = errors.New("threadlog: the post is not in a state this record applies to")

// ErrLogMoved means an append's AtLength precondition failed: the log had
// grown past the length the caller read. Nothing was written.
var ErrLogMoved = errors.New("threadlog: the log has moved on since the length this record was decided at")

// ErrArchived means the thread is archived: its log holds a ThreadArchived,
// or one is staged ahead in the same batch, or the actor was sealed. An
// archived thread takes no further record from anyone (docs/03). Nothing
// was written.
var ErrArchived = errors.New("threadlog: the thread is archived")

type request struct {
	a     Append
	reply chan answer

	// run, when not nil, is a run of records appended together, and
	// runReply takes its one answer; a and reply are then unused
	// (AppendRun).
	run      []Append
	runReply chan runAnswer
}

type answer struct {
	res Result
	err error
}

type runAnswer struct {
	res []Result
	err error
}

// staged is one record that has been encoded and given its numbers but not
// yet written. Encoding the whole batch before touching the file means a
// malformed request fails without having changed anything on disk. app is
// the record's Append, which for a run is one of req.run.
type staged struct {
	req    *request
	app    Append
	header FrameHeader
	body   []byte
	offset int64
	size   int
	res    Result
}

// Actor owns one open thread. Exactly one goroutine writes to the files, which
// is what makes offsets knowable before a write and ordinals assignable without
// a lock.
type Actor struct {
	log    LogWriter
	index  IndexWriter
	policy SyncPolicy

	reqs chan request
	quit chan struct{}
	done chan struct{}

	// inflight counts Appends that have been admitted but not yet answered.
	//
	// It is what lets the request channel stay OPEN for the actor's whole life.
	// Closing it from the receiving side to signal shutdown looks tidy and is a
	// panic waiting to happen: a send on a closed channel panics rather than
	// blocking, and selecting on a quit channel alongside the send does not
	// save you, because the send case is "ready" the moment the channel closes.
	inflight sync.WaitGroup

	// Owned by the actor goroutine alone.
	ix          *Index
	logLength   int64
	headerDirty bool

	// untrusted is set once the index file may disagree with the in-memory
	// index: an entry write failed, an index fsync failed, or a record would
	// not apply. From then on no header is written, for the actor's whole
	// life, because the header is what makes the next open trust every entry
	// in the file. The header on disk keeps its older length, so the next
	// open replays and writes the index back whole (TKT-01M3VHSYFR). Before
	// that ticket a failure here set headerDirty, and the flush that follows
	// every batch then wrote a header covering the entry that was wrong.
	untrusted bool

	// published is logLength as readers may see it, set only after the index
	// has applied the records that length covers. A reader that takes it and
	// then an index snapshot therefore never holds an index older than the
	// length it reports (TKT-01M29CA9AX).
	published atomic.Int64

	// applying is a sequence lock over the in-memory index: odd while a batch
	// is being applied, bumped to even once published covers it. [Actor.Stable]
	// uses it to hand a reader a length and an index that describe exactly the
	// same log, rather than an index that may be newer than the length.
	applying atomic.Uint64

	// sealed is set once the thread is archived: by the actor when it
	// writes a ThreadArchived, or by Seal for a thread opened archived.
	// Every append after it fails with ErrArchived.
	sealed atomic.Bool

	mu       sync.Mutex
	closeErr error
	closed   bool
}

// NewActor starts the goroutine that owns a thread.
//
// ix must describe the log as it currently stands, which is what [Open]
// returns. logLength is the log's size in bytes; the actor appends from there
// and never seeks.
func NewActor(ix *Index, logLength int64, log LogWriter, index IndexWriter, policy SyncPolicy) *Actor {
	a := &Actor{
		log: log, index: index, policy: policy,
		reqs: make(chan request), quit: make(chan struct{}), done: make(chan struct{}),
		ix: ix, logLength: logLength,
	}
	a.published.Store(logLength)
	go a.run()
	return a
}

// Append adds a record and returns once it is durable.
//
// The caller blocks until the record is on disk, which is the promise the write
// path is built around: a handler that has returned 200 has returned it about
// bytes that survived a power cut.
func (a *Actor) Append(req Append) (Result, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return Result{}, ErrActorClosed
	}
	a.inflight.Add(1)
	a.mu.Unlock()
	defer a.inflight.Done()

	r := request{a: req, reply: make(chan answer, 1)}
	select {
	case a.reqs <- r:
	case <-a.quit:
		return Result{}, ErrActorClosed
	}

	ans := <-r.reply
	return ans.res, ans.err
}

// AppendRun adds records as one contiguous run and returns once all of them
// are durable: they are staged in order, each against the index as the ones
// before it leave it, written with one write and made durable with one
// fsync, and nothing else lands between them. Either every record is
// written or none is, and the error says which record refused.
//
// AtLength on the first record holds the whole run to that length; on a
// later record it is ignored, since the run itself decides where that one
// lands. A move's target run is the caller (docs/03 "How a move is
// written"): its PostsMergedIn, each copy and the copy's Edits.
//
// A crash can still leave a prefix of the run on disk, as it can leave the
// prefix of any batch. The log is the truth, and the caller that needs the
// whole run, the move finisher, reads what is there and appends the rest.
func (a *Actor) AppendRun(reqs []Append) ([]Result, error) {
	if len(reqs) == 0 {
		return nil, nil
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, ErrActorClosed
	}
	a.inflight.Add(1)
	a.mu.Unlock()
	defer a.inflight.Done()

	r := request{run: reqs, runReply: make(chan runAnswer, 1)}
	select {
	case a.reqs <- r:
	case <-a.quit:
		return nil, ErrActorClosed
	}

	ans := <-r.runReply
	return ans.res, ans.err
}

// Close stops the actor, flushes the index header, and fsyncs the index.
//
// An error here does not mean an append was lost. The log is the source of
// truth and every acknowledged record is in it; a failure to flush the index
// means the next open replays instead of trusting, which is slower and correct.
func (a *Actor) Close() error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		<-a.done
		return a.closeErr
	}
	a.closed = true
	a.mu.Unlock()

	// No new Appends can be admitted now. Wait for the ones already admitted to
	// be answered before telling the loop to stop, so nothing is left holding a
	// reply channel nobody will ever write to.
	a.inflight.Wait()
	close(a.quit)

	<-a.done
	return a.closeErr
}

// Seal makes every later append fail with [ErrArchived]: the thread is
// archived. The actor seals itself when it writes a ThreadArchived; Seal is
// for a thread whose log already ended in one when it was opened, which the
// index does not record (threadcache's Options.Archived).
func (a *Actor) Seal() { a.sealed.Store(true) }

// Sealed reports whether the actor refuses appends because the thread is
// archived.
func (a *Actor) Sealed() bool { return a.sealed.Load() }

// Index returns the in-memory index. Valid only after Close; before then the
// actor goroutine owns it.
func (a *Actor) Index() *Index { return a.ix }

// LogLength is the log's length in bytes as of the last committed batch: the
// thread's version number. It is safe to call from any goroutine, and the
// index already reflects every record it covers, so read it before taking an
// index snapshot and the snapshot is at least that new.
func (a *Actor) LogLength() int64 { return a.published.Load() }

// Stable calls read with the published log length, and calls it again until
// no batch was applied to the index while it ran. What read saw of the index
// is then exactly the log at that length, neither older nor newer, which is
// what an ETag naming the length needs of the entries it was computed from.
//
// read may run more than once, so it must only read, and it should be quick:
// every batch applied while it runs costs it another pass.
func (a *Actor) Stable(read func(logLength int64)) {
	for {
		seq := a.applying.Load()
		if seq&1 == 1 {
			runtime.Gosched()
			continue
		}
		read(a.published.Load())
		if a.applying.Load() == seq {
			return
		}
	}
}

func (a *Actor) run() {
	defer close(a.done)

	batch := make([]request, 0, 64)
	for {
		select {
		case r := <-a.reqs:
			batch = append(batch[:0], r)
		case <-a.quit:
			a.shutdown()
			return
		}

		// Drain what is ALREADY queued, and only that. Waiting for more is what
		// would make a quiet thread slower than SyncAlways, which is exactly
		// the trade docs/03 refuses.
		if a.policy != SyncAlways {
		drain:
			for {
				select {
				case r := <-a.reqs:
					batch = append(batch, r)
				default:
					break drain
				}
			}
		}

		a.commit(batch)
	}
}

// shutdown writes and fsyncs the index on the way out. The header is written
// lazily during normal operation — it costs a syscall and buys only a faster
// next open — so this is where it is finally made durable.
func (a *Actor) shutdown() {
	a.flushHeader()
	if a.policy != SyncNever {
		if err := a.index.Sync(); err != nil {
			a.untrusted = true
			a.noteErr(fmt.Errorf("threadlog: syncing the index: %w", err))
		}
	}
}

// commit writes a batch of records, makes them durable, and only then touches
// the index.
//
// The ordering here is the load-bearing part of docs/03 step 4 and is not a
// matter of taste. The index header records the log length the index is valid
// up to, so writing it before the log fsync would let a crash leave a header
// claiming bytes that never reached the disk — an index confidently wrong, with
// no replay to notice. Written after, the worst case is a header claiming to be
// older than it is, which triggers a harmless replay.
func (a *Actor) commit(batch []request) {
	base := a.logLength
	var buf []byte
	staging := make([]staged, 0, len(batch))

	// 1. Encode and number everything. Nothing is on disk yet, so a bad
	//    request fails alone rather than poisoning the batch, and a run
	//    that refuses one of its records takes none of them.
	for i := range batch {
		r := &batch[i]
		apps := r.run
		if apps == nil {
			apps = []Append{r.a}
		}
		mark, markBuf := len(staging), len(buf)
		var err error
		for k, app := range apps {
			offset := base + int64(len(buf))
			if k == 0 && app.AtLength != 0 && app.AtLength != offset {
				err = fmt.Errorf("%w: decided at %d, the record would be written at %d",
					ErrLogMoved, app.AtLength, offset)
				break
			}
			h, body, ferr := a.frameFor(app, staging)
			if ferr == nil {
				var next []byte
				if next, ferr = AppendFrame(buf, h, body); ferr == nil {
					staging = append(staging, staged{
						req: r, app: app, header: h, body: body, offset: offset, size: len(next) - len(buf),
						res: Result{Ordinal: h.Ordinal, Revision: h.Revision},
					})
					buf = next
				}
			}
			if ferr != nil {
				err = ferr
				if r.run != nil {
					err = fmt.Errorf("record %d of the run, a %s: %w", k, app.Type, ferr)
				}
				break
			}
		}
		if err != nil {
			staging, buf = staging[:mark], buf[:markBuf]
			a.answer(r, nil, err)
		}
	}
	if len(staging) == 0 {
		return
	}

	// 2. One write for the whole batch.
	if _, err := a.log.Write(buf); err != nil {
		a.failAll(staging, fmt.Errorf("threadlog: appending to the log: %w", err))
		return
	}

	// 3. One fsync for the whole batch. Nothing below this line may run until
	//    it returns.
	if a.policy != SyncNever {
		if err := a.log.Sync(); err != nil {
			a.failAll(staging, fmt.Errorf("threadlog: syncing the log: %w", err))
			return
		}
	}

	// 4. Durable. Now, and only now, the index: the in-memory one first,
	//    inside the sequence lock and with no I/O, then publish, then the
	//    index file. A reader in Stable spins only for the in-memory part.
	a.logLength = base + int64(len(buf))
	applied := make([]bool, len(staging))
	changed := false
	a.applying.Add(1)
	for i := range staging {
		s := &staging[i]
		if err := a.ix.Apply(s.header, s.body, uint64(s.offset), s.size); err != nil {
			// The record is durable and the in-memory index would not take it.
			// The log is the truth, so the append stands, and no header is
			// written again: the next open replays from the last one and
			// rebuilds what this index lost.
			a.untrusted = true
			a.noteErr(fmt.Errorf("threadlog: index diverged at ordinal %d: %w", s.header.Ordinal, err))
			continue
		}
		applied[i] = true
		s.res.LogLength = a.logLength
		if changesEntry(s.header) {
			changed = true
		}
	}
	a.published.Store(a.logLength)
	a.applying.Add(1)
	// A ThreadArchived on disk seals the actor before the batch is answered,
	// so no later batch can follow it.
	for i := range staging {
		if staging[i].header.Type == TypeThreadArchived {
			a.sealed.Store(true)
		}
	}
	for i := range staging {
		if applied[i] {
			a.writeEntry(staging[i].header.Ordinal)
			// A split changes the entries of the posts it names, not of
			// its frame's ordinal 0. Each is written, and made durable
			// below with the rest, before the header that covers it: a
			// trusted index that missed one would show a moved post here.
			for _, o := range splitOrdinals(staging[i].header.Type, staging[i].app.Body) {
				a.writeEntry(o)
			}
		}
	}
	// A record that changed an existing entry, such as a Delete or an Edit,
	// rewrote that entry in place. Nothing orders that write before the
	// header's on disk, and a power cut that kept the header and lost the
	// entry would leave a trusted index showing a hidden post. So the entries
	// are made durable before the header that covers them is written
	// (TKT-01M3VHSYFR). A batch of new posts pays no extra fsync: a lost new
	// entry hides nothing (TKT-01M3VRFRFW).
	if changed && !a.untrusted && a.policy != SyncNever {
		if err := a.index.Sync(); err != nil {
			a.untrusted = true
			a.noteErr(fmt.Errorf("threadlog: syncing the index before its header: %w", err))
		}
	}
	a.headerDirty = true
	a.flushHeader()

	// 5. Answer.
	a.answerAll(staging, nil)
}

// answer replies to one request: a single record's answer, or a run's,
// whose results are res in order.
func (a *Actor) answer(r *request, res []Result, err error) {
	if r.run != nil {
		r.runReply <- runAnswer{res: res, err: err}
		return
	}
	var one Result
	if len(res) > 0 {
		one = res[0]
	}
	r.reply <- answer{res: one, err: err}
}

// answerAll replies to every request with records in staging, once each:
// with err when it is not nil, or else with each record's result.
func (a *Actor) answerAll(staging []staged, err error) {
	for i := 0; i < len(staging); {
		r := staging[i].req
		j := i
		var res []Result
		for j < len(staging) && staging[j].req == r {
			res = append(res, staging[j].res)
			j++
		}
		if err != nil {
			res = nil
		}
		a.answer(r, res, err)
		i = j
	}
}

// changesEntry reports whether a record rewrites an existing post's entry,
// which is every record Apply folds into an entry other than the Post that
// creates it. A PostsSplitOut names its posts in its body rather than its
// frame, and rewrites each.
func changesEntry(h FrameHeader) bool {
	switch h.Type {
	case TypeEdit, TypeDelete, TypeRestore, TypeRedacted:
		return h.Ordinal != 0
	case TypePostsSplitOut:
		return true
	}
	return false
}

// frameFor assigns the ordinal and revision for one request and encodes its
// body.
//
// staging is consulted rather than only the index, because several posts in one
// batch must get consecutive ordinals before any of them has been applied. An
// actor that read only the index would hand the same number to every post in a
// batch — which is the exact bug the single-writer design is supposed to make
// impossible, so it gets its own test.
func (a *Actor) frameFor(req Append, staging []staged) (FrameHeader, []byte, error) {
	// An archived thread is read-only to everyone (docs/03, docs/08
	// "Locking semantics"): nothing follows a ThreadArchived, whether it is
	// already in the log or staged ahead of req in this batch.
	if a.sealed.Load() || slices.ContainsFunc(staging, func(s staged) bool { return s.header.Type == TypeThreadArchived }) {
		return FrameHeader{}, nil, fmt.Errorf("%w: refusing a %s", ErrArchived, req.Type)
	}
	ts := req.TimeMS
	if ts == 0 {
		ts = time.Now().UnixMilli()
	}

	h := FrameHeader{
		Type: req.Type, Encoding: req.Encoding, Flags: req.Flags,
		TimestampMS: ts, Actor: req.Author,
	}
	if h.Encoding == 0 {
		h.Encoding = EncodingCBOR
	}

	switch req.Type {
	case TypePost:
		h.Ordinal = uint32(a.ix.Len()) + 1
		for _, s := range staging {
			if s.header.Type == TypePost {
				h.Ordinal++
			}
		}
		h.Revision = 1

	case TypeEdit:
		if req.Ordinal == 0 {
			return h, nil, fmt.Errorf("%w: an Edit must name an ordinal", ErrMalformed)
		}
		h.Ordinal = req.Ordinal
		rev, _, err := a.entryAfter(req.Ordinal, staging)
		if err != nil {
			return h, nil, err
		}
		h.Revision = rev + 1

	case TypeRedacted:
		// Written only by the offline ketju redact, which rewrites the log
		// with no actor running (docs/03 "Hard redaction"). A marker
		// appended here would tombstone a post whose text is still on disk.
		return h, nil, fmt.Errorf("%w: a Redacted record is written only by ketju redact", ErrMalformed)

	case TypePostsSplitOut, TypePostsMergedIn:
		// Thread-level, and held to their shape here rather than in the
		// index fold, which takes what the log says so that a thread
		// always replays (docs/03 "How a move is written").
		if req.Ordinal != 0 {
			return h, nil, fmt.Errorf("%w: a %s is thread-level and names no ordinal", ErrMalformed, req.Type)
		}
		h.Revision = 1
		if err := CheckMove(req.Body, a.ix.Header.ThreadID); err != nil {
			return h, nil, err
		}
		// Only a post shown as itself moves, as it stands after every
		// record staged ahead of this one, splits included.
		for _, o := range splitOrdinals(req.Type, req.Body) {
			f, err := a.flagsAfter(o, staging)
			if err != nil {
				return h, nil, err
			}
			if !f.Visible() {
				return h, nil, fmt.Errorf("%w: PostsSplitOut names post %d, which is not shown as itself (flags %#x)",
					ErrPostState, o, uint16(f))
			}
		}

	case TypeThreadCreated:
		// Held to its shape here, as a move is: a continues naming thread
		// 0, ordinal 0 or this thread is refused before it reaches the log
		// (TKT-01M29CYSC0).
		if req.Ordinal != 0 {
			return h, nil, fmt.Errorf("%w: a ThreadCreated is thread-level and names no ordinal", ErrMalformed)
		}
		h.Revision = 1
		if err := CheckThreadCreated(req.Body, a.ix.Header.ThreadID); err != nil {
			return h, nil, err
		}

	default:
		h.Ordinal = req.Ordinal
		h.Revision = 1
		if req.Ordinal != 0 {
			if _, _, err := a.entryAfter(req.Ordinal, staging); err != nil {
				return h, nil, err
			}
		}
	}

	// A redacted post takes no further record, whoever asks: an Edit would
	// give it a source again, and a Delete or Restore would change the
	// state of a post there is nothing left of (TKT-01M29CXJZ9). Nor does a
	// moved post: its revision is the one its copy must reach, and a record
	// here would show nowhere (docs/03 "How a move is written"). A caller's
	// own Refuse says so too; this holds for the callers that forget.
	if req.Type != TypePost && h.Ordinal != 0 {
		req.Refuse |= EntryRedacted | EntryMoved
	}
	if req.Require != 0 || req.Refuse != 0 || req.RequireAny != 0 {
		if err := a.checkState(req, staging); err != nil {
			return h, nil, err
		}
	}

	body, err := EncodeBody(h.Encoding, req.Body)
	if err != nil {
		return h, nil, err
	}
	return h, body, nil
}

// checkState holds req to its Require, RequireAny and Refuse preconditions,
// against the post as it will stand when req is applied ([Actor.flagsAfter]).
func (a *Actor) checkState(req Append, staging []staged) error {
	if req.Ordinal == 0 {
		return fmt.Errorf("%w: a precondition needs an ordinal", ErrMalformed)
	}
	flags, err := a.flagsAfter(req.Ordinal, staging)
	if err != nil {
		return err
	}
	if !flags.Has(req.Require) || flags&req.Refuse != 0 || (req.RequireAny != 0 && flags&req.RequireAny == 0) {
		return fmt.Errorf("%w: %s of post %d needs flags %#x set, one of %#x set and %#x clear, and it has %#x",
			ErrPostState, req.Type, req.Ordinal, uint16(req.Require), uint16(req.RequireAny), uint16(req.Refuse), uint16(flags))
	}
	return nil
}

// flagsAfter is ordinal's flags as they will stand once every record staged
// ahead in this batch is applied ([Actor.entryAfter]).
func (a *Actor) flagsAfter(ordinal uint32, staging []staged) (EntryFlags, error) {
	_, flags, err := a.entryAfter(ordinal, staging)
	return flags, err
}

// entryAfter is ordinal's revision and flags as they will stand once every
// record staged ahead in this batch is applied. It starts from the index's
// entry, or from the Post staged ahead that creates it, flagged as Apply
// flags a new post; then each staged record that names the post is folded in
// order through [postFlagEffect], the table Apply uses, an Edit takes the
// revision, and each staged split that names the post moves it. So a check,
// and a revision, see the post exactly as Apply will leave it, which is what
// lets one batch or run hold a post and the Edits that follow it (a move's
// copy, docs/03 "How a move is written").
func (a *Actor) entryAfter(ordinal uint32, staging []staged) (uint32, EntryFlags, error) {
	var rev uint32
	var flags EntryFlags
	e, err := a.ix.Entry(ordinal)
	found := err == nil
	if found {
		rev, flags = uint32(e.Revision), e.Flags
	}
	for _, s := range staging {
		switch {
		case s.header.Type == TypePost && s.header.Ordinal == ordinal:
			rev, flags, found = s.header.Revision, newPostFlags(s.header, s.app.Body), true
		case s.header.Ordinal == ordinal:
			set, clear := postFlagEffect(s.header.Type, s.app.Body)
			flags = (flags | set) &^ clear
			if s.header.Type == TypeEdit {
				rev = s.header.Revision
			}
		}
		if slices.Contains(splitOrdinals(s.header.Type, s.app.Body), ordinal) {
			flags |= EntryMoved
		}
	}
	if !found {
		return 0, 0, err
	}
	return rev, flags, nil
}

// writeEntry writes one index entry, without fsync.
//
// A failure is not fatal. The log already holds the record, so the entry is
// rebuildable, and the actor writes no header from then on, so the next open
// replays rather than trusting a file with this entry wrong.
func (a *Actor) writeEntry(ordinal uint32) {
	if ordinal == 0 {
		return // a thread event has no entry
	}
	e, err := a.ix.Entry(ordinal)
	if err != nil {
		return
	}
	off, err := EntryFileOffset(ordinal)
	if err != nil {
		return
	}
	if _, err := a.index.WriteAt(e.AppendTo(nil), off); err != nil {
		a.untrusted = true
		a.noteErr(fmt.Errorf("threadlog: writing index entry %d: %w", ordinal, err))
	}
}

// flushHeader writes the index header, which is what makes the index trusted on
// the next open. It fsyncs only when Close does, or before a batch that changed
// an existing entry (see commit).
//
// Called only after the log fsync has returned, and never once the actor is
// untrusted.
func (a *Actor) flushHeader() {
	if !a.headerDirty || a.untrusted {
		return
	}
	h := a.ix.Header
	h.LogLength = uint64(a.logLength)
	h.PostCount = uint32(a.ix.Len())
	a.ix.Header = h

	if _, err := a.index.WriteAt(h.AppendTo(nil), 0); err != nil {
		a.noteErr(fmt.Errorf("threadlog: writing the index header: %w", err))
		return
	}
	a.headerDirty = false
}

func (a *Actor) failAll(staging []staged, err error) {
	a.answerAll(staging, err)
}

func (a *Actor) noteErr(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closeErr == nil {
		a.closeErr = err
	}
}
