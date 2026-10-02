package threadlog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"sync"
)

// IndexMagic is the signature at the head of a sidecar index file.
//
// Different from [Magic] on purpose. The index is derived data and the log is
// the source of truth, so the one mistake that must never be silent is reading
// one as the other — restoring a backup that swapped the pair, say. A distinct
// signature turns that into [ErrBadMagic] on the first read.
var IndexMagic = [8]byte{'T', 'H', 'R', 'D', 'I', 'D', 'X', '1'}

const (
	// IndexHeaderSize is the fixed header at the head of an index file.
	IndexHeaderSize = 32

	// IndexEntrySize is one entry, one post ordinal.
	//
	// Settled at 40 bytes and measured, not guessed: on a 50 000 post thread,
	// filtering by author takes 15 us from these entries against 19.8 ms when
	// the entry carries only an offset and the author must be read back from
	// the log. The 24 extra bytes buy a factor of 1300, and that is the
	// warm-cache case. See docs/03.
	IndexEntrySize = 40
)

// Field offsets within the index header, from the docs/03 table.
const (
	ihMagic     = 0  // 8 bytes
	ihThreadID  = 8  // 8
	ihLogLength = 16 // 8
	ihPostCount = 24 // 4
	ihCRC       = 28 // 4, over bytes 0..27
)

// Field offsets within an index entry, from the docs/03 table.
const (
	ieOffset      = 0  // 8 bytes
	ieFrameLength = 8  // 4
	ieReplyTo     = 12 // 4
	ieAuthor      = 16 // 8
	ieCreatedMS   = 24 // 8
	ieRevision    = 32 // 2
	ieFlags       = 34 // 2
	ieReserved    = 36 // 4
)

// ErrIndexStale means the index does not cover the whole log. It is not
// damage: it is the ordinary result of a crash between the log fsync and the
// index header write, and the repair is to replay the records past
// [IndexHeader.LogLength].
var ErrIndexStale = errors.New("threadlog: index does not cover the log")

// ErrNoSuchOrdinal means an ordinal is outside the range this index holds.
var ErrNoSuchOrdinal = errors.New("threadlog: no such ordinal")

// IndexHeader is the fixed header of a sidecar index.
//
// LogLength is the field that matters. It records how much of the log this
// index is known to describe, which turns trusting an index into a comparison
// against stat() rather than an act of faith.
type IndexHeader struct {
	ThreadID  uint64
	LogLength uint64
	PostCount uint32
}

// AppendTo appends the encoded index header to dst.
func (h IndexHeader) AppendTo(dst []byte) []byte {
	start := len(dst)
	dst = append(dst, make([]byte, IndexHeaderSize)...)
	b := dst[start:]

	copy(b[ihMagic:], IndexMagic[:])
	binary.LittleEndian.PutUint64(b[ihThreadID:], h.ThreadID)
	binary.LittleEndian.PutUint64(b[ihLogLength:], h.LogLength)
	binary.LittleEndian.PutUint32(b[ihPostCount:], h.PostCount)
	binary.LittleEndian.PutUint32(b[ihCRC:], crc32.Checksum(b[:ihCRC], castagnoli))
	return dst
}

// ParseIndexHeader decodes the index header at the start of b.
//
// The CRC covers every other field, so a torn or corrupt header is reported as
// [ErrChecksum] rather than believed. A caller that gets any error here
// rebuilds the index from the log; see [RebuildRequired], which exists so that
// decision is made in one place rather than by each caller guessing which
// errors are recoverable.
func ParseIndexHeader(b []byte) (IndexHeader, error) {
	if len(b) < IndexHeaderSize {
		return IndexHeader{}, fmt.Errorf("%w: index header needs %d bytes, got %d",
			ErrShortBuffer, IndexHeaderSize, len(b))
	}
	if !bytes.Equal(b[ihMagic:ihMagic+len(IndexMagic)], IndexMagic[:]) {
		return IndexHeader{}, fmt.Errorf("%w: index begins with %q", ErrBadMagic, b[ihMagic:ihMagic+len(IndexMagic)])
	}

	stored := binary.LittleEndian.Uint32(b[ihCRC:])
	computed := crc32.Checksum(b[:ihCRC], castagnoli)
	if stored != computed {
		return IndexHeader{}, &ChecksumError{Part: "index header", Stored: stored, Computed: computed}
	}

	return IndexHeader{
		ThreadID:  binary.LittleEndian.Uint64(b[ihThreadID:]),
		LogLength: binary.LittleEndian.Uint64(b[ihLogLength:]),
		PostCount: binary.LittleEndian.Uint32(b[ihPostCount:]),
	}, nil
}

// Covers reports whether this index describes the whole of a log of logSize
// bytes.
//
// This is the entire trust decision, and it is deliberately a comparison and
// not a checksum of the log: verifying an index by rereading what it indexes
// would cost exactly what the index exists to avoid.
//
// An index claiming to be LONGER than the log is not merely stale, it is
// impossible — a truncated or replaced log — and reports false so the caller
// rebuilds.
func (h IndexHeader) Covers(logSize int64) bool {
	return logSize >= 0 && h.LogLength == uint64(logSize)
}

// RebuildRequired reports whether err means the index file cannot be trusted
// and must be rebuilt from the log.
//
// It is true for every way an index can be wrong about itself — bad magic, a
// failed header CRC, a self-inconsistent or truncated file, or a log length
// that does not match stat(). All of those are recoverable, because an index
// is derived data: a rebuild is a sequential read of the log and loses nothing.
//
// It is false for [ErrNoSuchOrdinal], which is the one error here that says
// nothing about the file. Asking for post 900 of a 40-post thread is a bad
// question, and throwing away a perfectly good index over it would turn a
// caller's bug into a storage cost.
func RebuildRequired(err error) bool {
	if err == nil || errors.Is(err, ErrNoSuchOrdinal) {
		return false
	}
	return errors.Is(err, ErrBadMagic) ||
		errors.Is(err, ErrChecksum) ||
		errors.Is(err, ErrMalformed) ||
		errors.Is(err, ErrShortBuffer) ||
		errors.Is(err, ErrIndexStale)
}

// OpenIndex parses an index file and decides whether it may be trusted for a
// log of logSize bytes.
//
// This is the whole open path from docs/03: read both headers, and if
// idx.log_length == stat(log).size the index is trusted, otherwise repair. The
// two failure modes are deliberately not merged — a corrupt index needs a full
// rebuild, while a stale one needs only the records past
// [IndexHeader.LogLength] replayed, and telling them apart is the difference
// between a sequential read of the whole log and reading a few kilobytes.
//
// Every error returned here satisfies [RebuildRequired]. A caller that cannot
// be bothered to distinguish them can rebuild and always be correct.
func OpenIndex(b []byte, logSize int64) (*Index, error) {
	ix, err := ParseIndex(b)
	if err != nil {
		return nil, err
	}
	if !ix.Header.Covers(logSize) {
		return nil, fmt.Errorf("%w: index covers %d bytes, log is %d",
			ErrIndexStale, ix.Header.LogLength, logSize)
	}
	return ix, nil
}

// IndexEntry is one post's entry.
//
// Exactly [IndexEntrySize] bytes in memory as well as on disk, which
// TestIndexEntryIsFortyBytesInMemory pins. That is not tidiness: the in-memory
// budget in docs/03 is the reason the width was argued over at all, and a field
// reordered into padding would blow it silently.
type IndexEntry struct {
	Offset      uint64 // of the CURRENT revision's record
	CreatedMS   int64  // of revision 1, so jump-to-date is stable across edits
	Author      uint64 // user id
	FrameLength uint32 // one pread of exactly the right size
	ReplyTo     uint32 // ordinal, 0 = none
	Revision    uint16 // current revision number
	Flags       EntryFlags
}

// AppendTo appends the encoded entry to dst.
//
// The four reserved bytes are written zero. They are held for the segment
// number described at the end of docs/03 and are not spare capacity for
// anything else.
func (e IndexEntry) AppendTo(dst []byte) []byte {
	start := len(dst)
	dst = append(dst, make([]byte, IndexEntrySize)...)
	b := dst[start:]

	binary.LittleEndian.PutUint64(b[ieOffset:], e.Offset)
	binary.LittleEndian.PutUint32(b[ieFrameLength:], e.FrameLength)
	binary.LittleEndian.PutUint32(b[ieReplyTo:], e.ReplyTo)
	binary.LittleEndian.PutUint64(b[ieAuthor:], e.Author)
	binary.LittleEndian.PutUint64(b[ieCreatedMS:], uint64(e.CreatedMS))
	binary.LittleEndian.PutUint16(b[ieRevision:], e.Revision)
	binary.LittleEndian.PutUint16(b[ieFlags:], uint16(e.Flags))
	return dst
}

// ParseIndexEntry decodes the entry at the start of b.
//
// There is no per-entry checksum, by design. An entry is 40 bytes of derived
// data guarded by the header's log-length comparison; paying four bytes and a
// CRC per post to protect something that is rebuilt by a sequential read would
// be the expensive way to get the same answer.
//
// Reserved bytes are not inspected, so a future version that puts them to use
// stays readable here.
func ParseIndexEntry(b []byte) (IndexEntry, error) {
	if len(b) < IndexEntrySize {
		return IndexEntry{}, fmt.Errorf("%w: index entry needs %d bytes, got %d",
			ErrShortBuffer, IndexEntrySize, len(b))
	}
	return IndexEntry{
		Offset:      binary.LittleEndian.Uint64(b[ieOffset:]),
		FrameLength: binary.LittleEndian.Uint32(b[ieFrameLength:]),
		ReplyTo:     binary.LittleEndian.Uint32(b[ieReplyTo:]),
		Author:      binary.LittleEndian.Uint64(b[ieAuthor:]),
		CreatedMS:   int64(binary.LittleEndian.Uint64(b[ieCreatedMS:])),
		Revision:    binary.LittleEndian.Uint16(b[ieRevision:]),
		Flags:       EntryFlags(binary.LittleEndian.Uint16(b[ieFlags:])),
	}, nil
}

// EntryFileOffset returns the byte offset of ordinal n's entry.
//
// This is the whole design in one line: 32 + 40*(n-1) is arithmetic, so
// reaching any post is a seek and never a scan, whether the thread has four
// posts or four hundred thousand.
//
// Ordinals are one-based. Ordinal 0 means "no post" — it is what a
// thread-level event carries in its frame — so it has no entry.
func EntryFileOffset(ordinal uint32) (int64, error) {
	if ordinal == 0 {
		return 0, fmt.Errorf("%w: ordinal 0 is the thread, not a post", ErrNoSuchOrdinal)
	}
	return IndexHeaderSize + int64(ordinal-1)*IndexEntrySize, nil
}

// ReadEntryAt reads one entry from an index file.
//
// One ReadAt of exactly [IndexEntrySize] bytes at the computed offset. It does
// not read the header, does not scan, and does not care how large the file is:
// the caller has already decided the index is trustworthy by comparing the
// header's log length against stat().
func ReadEntryAt(r io.ReaderAt, ordinal uint32) (IndexEntry, error) {
	off, err := EntryFileOffset(ordinal)
	if err != nil {
		return IndexEntry{}, err
	}

	var buf [IndexEntrySize]byte
	if _, err := r.ReadAt(buf[:], off); err != nil {
		if errors.Is(err, io.EOF) {
			return IndexEntry{}, fmt.Errorf("%w: ordinal %d is past the end of the index",
				ErrNoSuchOrdinal, ordinal)
		}
		return IndexEntry{}, fmt.Errorf("threadlog: reading index entry %d: %w", ordinal, err)
	}
	return ParseIndexEntry(buf[:])
}

// Index is the in-memory form: the header plus every entry, held contiguously.
//
// A slice rather than a map. Ordinals are dense and one-based, so the offset
// into the slice IS the ordinal minus one, and a map would cost a hash and
// roughly three times the memory to answer a question that subtraction already
// answers.
type Index struct {
	// mu makes an Index safe to read while the actor is writing it.
	//
	// It guards the ENTRIES only, and it is never held across I/O. That is the
	// property the read path depends on: a reader's worst case is waiting for
	// one batch of in-memory updates, measured in microseconds, and never for
	// an fsync. A reader that could block behind disk would make a busy thread
	// slow to read, which is the whole thing docs/03 keeps readers off the
	// actor's channel to avoid.
	mu      sync.RWMutex
	Header  IndexHeader
	entries []IndexEntry
}

// NewIndex builds an in-memory index. The entries are indexed by ordinal-1.
func NewIndex(h IndexHeader, entries []IndexEntry) *Index {
	return &Index{Header: h, entries: entries}
}

// ParseIndex decodes a whole index file.
//
// The header's post count is checked against the number of entries actually
// present, so a truncated index file is caught here rather than at the first
// read past the end.
func ParseIndex(b []byte) (*Index, error) {
	h, err := ParseIndexHeader(b)
	if err != nil {
		return nil, err
	}

	body := b[IndexHeaderSize:]
	if len(body)%IndexEntrySize != 0 {
		return nil, fmt.Errorf("%w: index has %d bytes after the header, not a multiple of %d",
			ErrMalformed, len(body), IndexEntrySize)
	}
	n := len(body) / IndexEntrySize
	if uint32(n) != h.PostCount {
		return nil, fmt.Errorf("%w: header claims %d posts, file holds %d entries",
			ErrMalformed, h.PostCount, n)
	}

	entries := make([]IndexEntry, n)
	for i := range entries {
		e, err := ParseIndexEntry(body[i*IndexEntrySize:])
		if err != nil {
			return nil, err
		}
		entries[i] = e
	}
	return &Index{Header: h, entries: entries}, nil
}

// AppendTo encodes the whole index, header first.
func (ix *Index) AppendTo(dst []byte) []byte {
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	h := ix.Header
	h.PostCount = uint32(len(ix.entries))
	dst = h.AppendTo(dst)
	for _, e := range ix.entries {
		dst = e.AppendTo(dst)
	}
	return dst
}

// Len is the number of posts.
func (ix *Index) Len() int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.entries)
}

// PastLast reports whether ordinal o is past the last of n posts.
//
// Every bound check of an ordinal against a post count goes through here
// (TKT-01M3VVZE4G). An ordinal is a uint32 and a count an int, which is 32
// bits on 386, arm, mips and mipsle. There int(4294967295) is -1, so a
// bound that converts the ordinal to int before comparing passes when it
// should fail, and the slice index after it panics. Comparing in uint64 is
// right on every word size. A negative n holds no posts, so every ordinal
// is past it.
func PastLast(o uint32, n int) bool {
	return n < 0 || uint64(o) > uint64(n)
}

// Entry returns the entry for a one-based ordinal.
func (ix *Index) Entry(ordinal uint32) (IndexEntry, error) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if ordinal == 0 || PastLast(ordinal, len(ix.entries)) {
		return IndexEntry{}, fmt.Errorf("%w: ordinal %d, index holds %d posts",
			ErrNoSuchOrdinal, ordinal, len(ix.entries))
	}
	return ix.entries[ordinal-1], nil
}

// Entries returns the entries for ordinals from..to inclusive, clamped to what
// the index holds.
//
// The result is a COPY. An earlier version returned a subslice, which is
// cheaper and was wrong the moment a reader could run beside the actor: the
// actor mutates an entry in place when a post is edited or deleted, so a
// caller holding an alias would see a flag change under it, and the race
// detector would be right to complain.
//
// A page is a few dozen entries of 40 bytes, so the copy is well under a
// kilobyte and buys the caller a stable view for as long as it needs one.
func (ix *Index) Entries(from, to uint32) []IndexEntry {
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	if from == 0 {
		from = 1
	}
	if PastLast(from, len(ix.entries)) || to < from {
		return nil
	}
	if PastLast(to, len(ix.entries)) {
		to = uint32(len(ix.entries))
	}
	return append([]IndexEntry(nil), ix.entries[from-1:to]...)
}

// Tombstoned reports whether a post renders as a tombstone or a pointer rather
// than as itself.
//
// Answered from flags, so a page of deleted posts costs no log reads at all.
func (ix *Index) Tombstoned(ordinal uint32) (bool, error) {
	e, err := ix.Entry(ordinal)
	if err != nil {
		return false, err
	}
	return !e.Flags.Visible(), nil
}

// Select returns the ordinals whose entries keep accepts, in ascending
// order. It is the one walk every lens makes (TKT-01M29CRJ1P): keep sees each
// post once, lowest ordinal first, and answers keep or skip, so a filter
// built on it can drop posts and can never reorder them.
//
// keep runs under the index's read lock and must not call back into the
// Index. A sync.RWMutex read lock taken twice deadlocks against a writer
// waiting between the two, so a lens that needs a second look at the index,
// such as a reply chain's ancestor walk, takes it before calling Select.
func (ix *Index) Select(keep func(ordinal uint32, e IndexEntry) bool) []uint32 {
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	var out []uint32
	for i, e := range ix.entries {
		if o := uint32(i) + 1; keep(o, e) {
			out = append(out, o)
		}
	}
	return out
}

// ByAuthor returns the ordinals authored by a user, in ordinal order.
//
// The author lens docs/03 measured: 15 us over 50 000 posts here, against
// 19.8 ms when the author has to be read back from the log.
func (ix *Index) ByAuthor(author uint64) []uint32 {
	return ix.Select(func(_ uint32, e IndexEntry) bool { return e.Author == author })
}

// Since returns the ordinals created at or after ms.
//
// CreatedMS is revision 1's timestamp, so "since my last visit" does not drag
// a four-year-old post back into view because somebody fixed a typo in it.
func (ix *Index) Since(ms int64) []uint32 {
	return ix.Select(func(_ uint32, e IndexEntry) bool { return e.CreatedMS >= ms })
}

// Replies returns the ordinals that reply directly to ordinal.
func (ix *Index) Replies(ordinal uint32) []uint32 {
	if ordinal == 0 {
		return nil
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	var out []uint32
	for i, e := range ix.entries {
		if e.ReplyTo == ordinal {
			out = append(out, uint32(i)+1)
		}
	}
	return out
}

// ReplyCounts returns how many posts reply directly to each of ordinals, in
// one pass over the index rather than one Replies call per post on a page.
func (ix *Index) ReplyCounts(ordinals []uint32) map[uint32]int {
	out := make(map[uint32]int, len(ordinals))
	for _, o := range ordinals {
		out[o] = 0
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	for _, e := range ix.entries {
		if _, want := out[e.ReplyTo]; want && e.ReplyTo != 0 {
			out[e.ReplyTo]++
		}
	}
	return out
}

// MachineAuthored returns the ordinals written by an agent account.
//
// The agent lens, from the mirrored frame flag bit, with no body decoded.
func (ix *Index) MachineAuthored() []uint32 {
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	var out []uint32
	for i, e := range ix.entries {
		if e.Flags.Has(EntryMachineAuthored) {
			out = append(out, uint32(i)+1)
		}
	}
	return out
}
