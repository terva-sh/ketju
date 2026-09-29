package threadlog

import (
	"errors"
	"fmt"
	"runtime"
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

type request struct {
	a     Append
	reply chan answer
}

type answer struct {
	res Result
	err error
}

// staged is one request that has been encoded and given its numbers but not
// yet written. Encoding the whole batch before touching the file means a
// malformed request fails without having changed anything on disk.
type staged struct {
	req    request
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
	//    request fails alone rather than poisoning the batch.
	for _, r := range batch {
		offset := base + int64(len(buf))
		if r.a.AtLength != 0 && r.a.AtLength != offset {
			r.reply <- answer{err: fmt.Errorf("%w: decided at %d, the record would be written at %d",
				ErrLogMoved, r.a.AtLength, offset)}
			continue
		}
		h, body, err := a.frameFor(r.a, staging)
		if err != nil {
			r.reply <- answer{err: err}
			continue
		}

		next, err := AppendFrame(buf, h, body)
		if err != nil {
			r.reply <- answer{err: err}
			continue
		}

		staging = append(staging, staged{
			req: r, header: h, body: body, offset: offset, size: len(next) - len(buf),
			res: Result{Ordinal: h.Ordinal, Revision: h.Revision},
		})
		buf = next
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
	a.applying.Add(1)
	for i := range staging {
		s := &staging[i]
		if err := a.ix.Apply(s.header, s.body, uint64(s.offset), s.size); err != nil {
			// The record is durable and the in-memory index would not take it.
			// The log is the truth, so the append stands and the index is left
			// marked stale for the next open to rebuild.
			a.headerDirty = true
			a.noteErr(fmt.Errorf("threadlog: index diverged at ordinal %d: %w", s.header.Ordinal, err))
			continue
		}
		applied[i] = true
		s.res.LogLength = a.logLength
	}
	a.published.Store(a.logLength)
	a.applying.Add(1)
	for i := range staging {
		if applied[i] {
			a.writeEntry(staging[i].header.Ordinal)
		}
	}
	a.headerDirty = true
	a.flushHeader()

	// 5. Answer.
	for i := range staging {
		staging[i].req.reply <- answer{res: staging[i].res}
	}
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
		e, err := a.ix.Entry(req.Ordinal)
		if err != nil {
			return h, nil, err
		}
		rev := uint32(e.Revision)
		for _, s := range staging {
			if s.header.Type == TypeEdit && s.header.Ordinal == req.Ordinal {
				rev = s.header.Revision
			}
		}
		h.Revision = rev + 1

	default:
		h.Ordinal = req.Ordinal
		h.Revision = 1
		if req.Ordinal != 0 {
			if _, err := a.ix.Entry(req.Ordinal); err != nil {
				return h, nil, err
			}
		}
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

// checkState holds req to its Require, RequireAny and Refuse preconditions.
// The flags are the index's, then every record staged ahead of req in this
// batch folded in order through [postFlagEffect], the table Apply uses, so
// the check sees the post exactly as it will stand when req is applied.
func (a *Actor) checkState(req Append, staging []staged) error {
	if req.Ordinal == 0 {
		return fmt.Errorf("%w: a precondition needs an ordinal", ErrMalformed)
	}
	e, err := a.ix.Entry(req.Ordinal)
	if err != nil {
		return err
	}
	flags := e.Flags
	for _, s := range staging {
		if s.header.Ordinal == req.Ordinal {
			set, clear := postFlagEffect(s.header.Type, s.req.a.Body)
			flags = (flags | set) &^ clear
		}
	}
	if !flags.Has(req.Require) || flags&req.Refuse != 0 || (req.RequireAny != 0 && flags&req.RequireAny == 0) {
		return fmt.Errorf("%w: %s of post %d needs flags %#x set, one of %#x set and %#x clear, and it has %#x",
			ErrPostState, req.Type, req.Ordinal, uint16(req.Require), uint16(req.RequireAny), uint16(req.Refuse), uint16(flags))
	}
	return nil
}

// writeEntry writes one index entry, without fsync.
//
// A failure is not fatal. The log already holds the record, so the entry is
// rebuildable, and the header is left dirty so the next open notices.
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
		a.headerDirty = true
		a.noteErr(fmt.Errorf("threadlog: writing index entry %d: %w", ordinal, err))
	}
}

// flushHeader writes the index header, which is what makes the index trusted on
// the next open. Never fsyncs: that happens once, on Close.
//
// Called only after the log fsync has returned.
func (a *Actor) flushHeader() {
	if !a.headerDirty {
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
	for i := range staging {
		staging[i].req.reply <- answer{err: err}
	}
}

func (a *Actor) noteErr(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closeErr == nil {
		a.closeErr = err
	}
}
