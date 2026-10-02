package threadlog

import (
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"unsafe"
)

// countingReaderAt wraps a file and records every read made through it.
//
// The two hardest criteria on this ticket are negatives — "with no scan" and
// "with no log read" — and a negative cannot be shown by a test that just gets
// the right answer. Counting the reads is the only way to tell an index that
// seeks from one that scans and happens to be fast.
type countingReaderAt struct {
	r     io.ReaderAt
	calls int
	bytes int
	offs  []int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	c.calls++
	c.bytes += len(p)
	c.offs = append(c.offs, off)
	return c.r.ReadAt(p, off)
}

func TestIndexHeaderRoundTrip(t *testing.T) {
	want := IndexHeader{ThreadID: 0x3f7a9c21e0000001, LogLength: 1 << 20, PostCount: 4096}

	got, err := ParseIndexHeader(want.AppendTo(nil))
	if err != nil {
		t.Fatalf("ParseIndexHeader: %v", err)
	}
	if got != want {
		t.Errorf("round trip changed the header: got %+v, want %+v", got, want)
	}
}

func TestIndexHeaderRejectsLogMagic(t *testing.T) {
	// The mistake this guards: a backup that swapped a .log and a .idx.
	log := NewFileHeader(1, 2, 3).AppendTo(nil)
	if _, err := ParseIndexHeader(log); !errors.Is(err, ErrBadMagic) {
		t.Errorf("parsing a log file as an index: error = %v, want ErrBadMagic", err)
	}
}

// Criterion: header CRC covers the header, and a corrupt header forces a
// rebuild rather than a bad read.
//
// Every byte is flipped in turn, so this proves coverage of the whole header
// rather than of whichever field the test author happened to pick.
func TestIndexHeaderCorruptionForcesRebuild(t *testing.T) {
	good := IndexHeader{ThreadID: 99, LogLength: 4096, PostCount: 7}.AppendTo(nil)

	for i := range good[:ihCRC] {
		bad := append([]byte(nil), good...)
		bad[i] ^= 0xff

		h, err := ParseIndexHeader(bad)
		if err == nil {
			t.Fatalf("byte %d corrupted and the header parsed anyway as %+v", i, h)
		}
		// A flipped magic byte is caught as bad magic, everything else by the
		// CRC. Both are conditions the caller must rebuild from; neither may
		// ever return a header the caller would act on.
		if !errors.Is(err, ErrChecksum) && !errors.Is(err, ErrBadMagic) {
			t.Errorf("byte %d: error = %v, want ErrChecksum or ErrBadMagic", i, err)
		}
		if !RebuildRequired(err) {
			t.Errorf("byte %d: RebuildRequired(%v) = false, a corrupt header must force a rebuild", i, err)
		}
		if h != (IndexHeader{}) {
			t.Errorf("byte %d: a failed parse returned %+v, not the zero header", i, h)
		}
	}
}

func TestIndexTrustIsAStatComparison(t *testing.T) {
	entries := []IndexEntry{{Offset: 64, FrameLength: 40, Revision: 1}}
	raw := NewIndex(IndexHeader{ThreadID: 1, LogLength: 104}, entries).AppendTo(nil)

	if _, err := OpenIndex(raw, 104); err != nil {
		t.Errorf("an index whose log length matches stat() should open: %v", err)
	}

	// Stale: the log grew past what the index covers. The repair is a partial
	// replay, so the error must be distinguishable from corruption.
	_, err := OpenIndex(raw, 512)
	if !errors.Is(err, ErrIndexStale) {
		t.Errorf("a grown log: error = %v, want ErrIndexStale", err)
	}
	if !RebuildRequired(err) {
		t.Error("a stale index must not be trusted")
	}

	// An index claiming MORE than the log holds is impossible, not merely
	// stale — a truncated or replaced log — and must never be trusted.
	if _, err := OpenIndex(raw, 64); err == nil {
		t.Error("an index claiming to cover more than the log exists should be refused")
	}
}

func TestRebuildRequiredIgnoresABadQuestion(t *testing.T) {
	ix := NewIndex(IndexHeader{}, []IndexEntry{{Revision: 1}})

	_, err := ix.Entry(900)
	if !errors.Is(err, ErrNoSuchOrdinal) {
		t.Fatalf("error = %v, want ErrNoSuchOrdinal", err)
	}
	if RebuildRequired(err) {
		t.Error("asking for a post that does not exist must not condemn the index file")
	}
	if RebuildRequired(nil) {
		t.Error("RebuildRequired(nil) must be false")
	}
}

// Criterion: the entry for ordinal n is read with one pread at the computed
// offset, with no scan.
func TestReadEntryAtIsOnePreadNoScan(t *testing.T) {
	const posts = 5000

	entries := make([]IndexEntry, posts)
	for i := range entries {
		entries[i] = IndexEntry{
			Offset: uint64(1000 + i*64), FrameLength: 64,
			Author: uint64(i%7) + 1, CreatedMS: int64(1_700_000_000_000) + int64(i),
			Revision: 1,
		}
	}
	raw := NewIndex(IndexHeader{ThreadID: 1, LogLength: 1}, entries).AppendTo(nil)

	path := filepath.Join(t.TempDir(), "thread.idx")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	// The last post in a five thousand post thread: the case a scan would be
	// slowest at and a seek is indifferent to.
	c := &countingReaderAt{r: f}
	got, err := ReadEntryAt(c, posts)
	if err != nil {
		t.Fatalf("ReadEntryAt: %v", err)
	}

	if got != entries[posts-1] {
		t.Errorf("entry = %+v, want %+v", got, entries[posts-1])
	}
	if c.calls != 1 {
		t.Errorf("reaching the last of %d posts took %d reads, want exactly 1", posts, c.calls)
	}
	if c.bytes != IndexEntrySize {
		t.Errorf("read %d bytes, want exactly one %d byte entry", c.bytes, IndexEntrySize)
	}
	wantOff := int64(IndexHeaderSize + (posts-1)*IndexEntrySize)
	if len(c.offs) != 1 || c.offs[0] != wantOff {
		t.Errorf("read at %v, want a single read at %d (32 + 40*(n-1))", c.offs, wantOff)
	}

	// Ordinal 0 is the thread, not a post, and must not resolve to the first
	// entry by accident.
	if _, err := EntryFileOffset(0); !errors.Is(err, ErrNoSuchOrdinal) {
		t.Errorf("EntryFileOffset(0) error = %v, want ErrNoSuchOrdinal", err)
	}
	if _, err := ReadEntryAt(f, posts+1); !errors.Is(err, ErrNoSuchOrdinal) {
		t.Errorf("reading past the end: error = %v, want ErrNoSuchOrdinal", err)
	}
}

// Criterion: tombstoned and moved posts are answerable from index flags with
// no log read, proven by a test that opens the log file read-only and counts
// reads.
func TestTombstonesAnswerWithoutTouchingTheLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "thread.log")

	// A real log on disk, with real bodies, so that "no reads" means the
	// bytes were genuinely there to be read and were not.
	raw := NewFileHeader(1, 1_700_000_000_000, 3).AppendTo(nil)
	var entries []IndexEntry
	for i := range 6 {
		body, err := EncodeBody(EncodingCBOR, &PostBody{Source: "post body that nobody should read"})
		if err != nil {
			t.Fatalf("EncodeBody: %v", err)
		}
		off := len(raw)
		raw, err = AppendFrame(raw, FrameHeader{
			Type: TypePost, Encoding: EncodingCBOR, Ordinal: uint32(i + 1),
			Revision: 1, TimestampMS: int64(1_700_000_000_000) + int64(i), Actor: 42,
		}, body)
		if err != nil {
			t.Fatalf("AppendFrame: %v", err)
		}

		var flags EntryFlags
		switch i {
		case 1:
			flags = EntryDeleted
		case 3:
			flags = EntryMoved
		case 4:
			flags = EntryHiddenByMod
		}
		entries = append(entries, IndexEntry{
			Offset: uint64(off), FrameLength: uint32(len(raw) - off),
			Author: 42, CreatedMS: int64(1_700_000_000_000) + int64(i), Revision: 1, Flags: flags,
		})
	}
	if err := os.WriteFile(logPath, raw, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	logFile, err := os.Open(logPath) // read-only, as the criterion asks
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer logFile.Close()
	counted := &countingReaderAt{r: logFile}

	ix := NewIndex(IndexHeader{ThreadID: 1, LogLength: uint64(len(raw)), PostCount: 6}, entries)

	var tombstoned []uint32
	for n := uint32(1); n <= uint32(ix.Len()); n++ {
		dead, err := ix.Tombstoned(n)
		if err != nil {
			t.Fatalf("Tombstoned(%d): %v", n, err)
		}
		if dead {
			tombstoned = append(tombstoned, n)
		}
	}

	if want := []uint32{2, 4, 5}; !reflect.DeepEqual(tombstoned, want) {
		t.Errorf("tombstoned = %v, want %v", tombstoned, want)
	}
	if counted.calls != 0 {
		t.Errorf("answering tombstones cost %d log reads (%d bytes); the index exists so it costs none",
			counted.calls, counted.bytes)
	}

	// The other lenses docs/03 justifies the 40-byte width with, same promise.
	if got := ix.ByAuthor(42); len(got) != 6 {
		t.Errorf("ByAuthor = %v, want all six", got)
	}
	if got := ix.Since(1_700_000_000_004); !reflect.DeepEqual(got, []uint32{5, 6}) {
		t.Errorf("Since = %v, want [5 6]", got)
	}
	if counted.calls != 0 {
		t.Errorf("the author and since lenses cost %d log reads, want 0", counted.calls)
	}
}

// Criterion: the in-memory form of a 50k-post index stays within the 2 MB
// stated in docs/03.
func TestFiftyThousandPostIndexFitsTheMemoryBudget(t *testing.T) {
	const posts = 50_000
	const budget = 2 << 20 // 2 MiB, the docs/03 figure

	// On a 32-bit build a uint64 aligns to 4 bytes, so the struct packs
	// tighter than its 40-byte disk form, and only a larger one blows the
	// budget there (TKT-01M3VVZE4G runs this package under GOARCH=386).
	got := unsafe.Sizeof(IndexEntry{})
	if got > IndexEntrySize || (unsafe.Alignof(uint64(0)) == 8 && got != IndexEntrySize) {
		t.Fatalf("IndexEntry is %d bytes in memory, want %d — a field reordered into "+
			"padding blows the budget silently", got, IndexEntrySize)
	}

	entries := make([]IndexEntry, posts)
	ix := NewIndex(IndexHeader{PostCount: posts}, entries)

	used := uintptr(cap(ix.entries)) * unsafe.Sizeof(IndexEntry{})
	if used > budget {
		t.Errorf("%d entries use %d bytes, over the %d byte budget", posts, used, budget)
	}
	t.Logf("%d entries: %d bytes (%.2f MiB), budget %d", posts, used, float64(used)/(1<<20), budget)
}

func TestIndexEntryRoundTripAndFlags(t *testing.T) {
	want := IndexEntry{
		Offset: 1 << 40, CreatedMS: 1_700_000_000_123, Author: 0xdeadbeefcafe,
		FrameLength: 4096, ReplyTo: 17, Revision: 9,
		Flags: EntryEdited | EntryMachineAuthored | EntryHasAttachments,
	}

	got, err := ParseIndexEntry(want.AppendTo(nil))
	if err != nil {
		t.Fatalf("ParseIndexEntry: %v", err)
	}
	if got != want {
		t.Errorf("round trip changed the entry: got %+v, want %+v", got, want)
	}

	if !got.Flags.Has(EntryEdited | EntryMachineAuthored) {
		t.Error("Has should require every named bit")
	}
	if got.Flags.Has(EntryDeleted) {
		t.Error("Has reported a bit that was never set")
	}
	if !got.Flags.Visible() {
		t.Error("edited and machine-authored are not reasons to hide a post")
	}

	for _, f := range []EntryFlags{EntryDeleted, EntryMoved, EntryHiddenByMod} {
		if (IndexEntry{Flags: f}).Flags.Visible() {
			t.Errorf("flag %#b should make a post render as something other than itself", f)
		}
	}
}

// Six flag bits used, ten spare — the spare capacity docs/03 names for this
// format. The reserved bytes at offset 36 are NOT that capacity; they are held
// for segment rollover.
func TestFlagBitsAreTheOnesDocumented(t *testing.T) {
	all := EntryDeleted | EntryMoved | EntryHiddenByMod | EntryHasAttachments |
		EntryEdited | EntryMachineAuthored

	for i, f := range []EntryFlags{
		EntryDeleted, EntryMoved, EntryHiddenByMod,
		EntryHasAttachments, EntryEdited, EntryMachineAuthored,
	} {
		if f != 1<<i {
			t.Errorf("flag %d is %#b, want %#b — bit positions are on disk and cannot move", i, f, 1<<i)
		}
	}
	if n := popcount(uint16(all)); n != 6 {
		t.Errorf("%d flag bits defined, docs/03 names 6 with 10 spare", n)
	}
}

// Reserved bytes are written zero but tolerated non-zero, so a future version
// that uses them for the segment number stays readable here.
func TestIndexEntryToleratesNonZeroReserved(t *testing.T) {
	want := IndexEntry{Offset: 64, FrameLength: 40, Revision: 1}
	raw := want.AppendTo(nil)

	for i := ieReserved; i < IndexEntrySize; i++ {
		if raw[i] != 0 {
			t.Fatalf("reserved byte %d was written as %#x, want zero", i, raw[i])
		}
		raw[i] = 0xab
	}

	got, err := ParseIndexEntry(raw)
	if err != nil {
		t.Fatalf("ParseIndexEntry: %v", err)
	}
	if got != want {
		t.Errorf("reserved bytes leaked into the entry: got %+v, want %+v", got, want)
	}
}

func TestParseIndexCatchesATruncatedFile(t *testing.T) {
	full := NewIndex(
		IndexHeader{ThreadID: 1, LogLength: 500, PostCount: 3},
		// Possible entries, so the intact file parses (TKT-01M3VRFRFW).
		[]IndexEntry{
			{Offset: 64, FrameLength: 64, Revision: 1},
			{Offset: 128, FrameLength: 64, Revision: 1},
			{Offset: 192, FrameLength: 64, Revision: 1},
		},
	).AppendTo(nil)

	// Lose the last entry, as a torn write would.
	if _, err := ParseIndex(full[:len(full)-IndexEntrySize]); !errors.Is(err, ErrMalformed) {
		t.Errorf("a truncated index: error = %v, want ErrMalformed", err)
	}
	// Lose part of an entry.
	if _, err := ParseIndex(full[:len(full)-7]); !errors.Is(err, ErrMalformed) {
		t.Errorf("a partial entry: error = %v, want ErrMalformed", err)
	}
	// Intact.
	ix, err := ParseIndex(full)
	if err != nil {
		t.Fatalf("ParseIndex: %v", err)
	}
	if ix.Len() != 3 {
		t.Errorf("Len = %d, want 3", ix.Len())
	}
}

func TestEntriesPageIsACopy(t *testing.T) {
	entries := make([]IndexEntry, 10)
	for i := range entries {
		entries[i] = IndexEntry{Offset: uint64(i), Revision: 1}
	}
	ix := NewIndex(IndexHeader{PostCount: 10}, entries)

	page := ix.Entries(3, 5)
	if len(page) != 3 || page[0].Offset != 2 || page[2].Offset != 4 {
		t.Errorf("Entries(3,5) = %+v, want ordinals 3..5", page)
	}

	// A copy, not an alias. The actor mutates an entry in place when a post is
	// edited or deleted, so a caller holding an alias would see it change
	// underneath — and would race the actor doing it.
	page[0].Offset = 0xdeadbeef
	if again := ix.Entries(3, 3); again[0].Offset != 2 {
		t.Error("mutating a returned page changed the index; Entries must copy")
	}
	if got := ix.Entries(9, 100); len(got) != 2 {
		t.Errorf("a page past the end should clamp, got %d entries", len(got))
	}
	if got := ix.Entries(11, 20); got != nil {
		t.Errorf("a page entirely past the end should be empty, got %+v", got)
	}
	if got := ix.Entries(5, 3); got != nil {
		t.Errorf("a reversed range should be empty, got %+v", got)
	}
}

func TestRepliesAndMachineAuthoredLenses(t *testing.T) {
	ix := NewIndex(IndexHeader{PostCount: 5}, []IndexEntry{
		{Revision: 1},
		{Revision: 1, ReplyTo: 1},
		{Revision: 1, ReplyTo: 1, Flags: EntryMachineAuthored},
		{Revision: 1, ReplyTo: 2},
		{Revision: 1, Flags: EntryMachineAuthored},
	})

	if got := ix.Replies(1); !reflect.DeepEqual(got, []uint32{2, 3}) {
		t.Errorf("Replies(1) = %v, want [2 3]", got)
	}
	if got := ix.Replies(0); got != nil {
		t.Errorf("Replies(0) = %v, want nil — ordinal 0 is the thread", got)
	}
	if got := ix.MachineAuthored(); !reflect.DeepEqual(got, []uint32{3, 5}) {
		t.Errorf("MachineAuthored = %v, want [3 5]", got)
	}
}

func popcount(x uint16) int {
	n := 0
	for ; x != 0; x &= x - 1 {
		n++
	}
	return n
}

// Criterion (TKT-01M3VRFRFW): an entry that cannot describe a frame in the
// log its header covers makes the index malformed, so the open rebuilds it
// from the log rather than trusting it. A zeroed entry, which is what a
// power cut leaves where a new entry's data did not persist, is one.
func TestAnImpossibleEntryIsMalformed(t *testing.T) {
	const logLength = FileHeaderSize + 1000
	good := IndexEntry{Offset: FileHeaderSize + 100, FrameLength: 200, Author: 1, CreatedMS: 1, Revision: 1}
	parse := func(e IndexEntry) error {
		ix := NewIndex(IndexHeader{ThreadID: 1, LogLength: logLength, PostCount: 2},
			[]IndexEntry{{Offset: FileHeaderSize, FrameLength: 100, Revision: 1}, e})
		_, err := ParseIndex(ix.AppendTo(nil))
		return err
	}
	for _, c := range []struct {
		name string
		edit func(e *IndexEntry)
	}{
		{"a zero entry", func(e *IndexEntry) { *e = IndexEntry{} }},
		{"offset 0", func(e *IndexEntry) { e.Offset = 0 }},
		{"an offset inside the file header", func(e *IndexEntry) { e.Offset = FileHeaderSize - 1 }},
		{"a frame shorter than its fixed part", func(e *IndexEntry) { e.FrameLength = FrameFixedSize - 1 }},
		{"a frame longer than the largest", func(e *IndexEntry) { e.FrameLength = MaxFrameSize + 1 }},
		{"a frame ending one past the covered log", func(e *IndexEntry) { e.FrameLength = uint32(logLength - e.Offset + 1) }},
		{"an offset whose sum with the length wraps", func(e *IndexEntry) {
			e.Offset, e.FrameLength = math.MaxUint64-10, FrameFixedSize
		}},
		{"an offset past the covered log", func(e *IndexEntry) { e.Offset = logLength + 1 }},
		{"revision 0", func(e *IndexEntry) { e.Revision = 0 }},
	} {
		e := good
		c.edit(&e)
		err := parse(e)
		if !errors.Is(err, ErrMalformed) || !RebuildRequired(err) {
			t.Errorf("%s (%+v): ParseIndex = %v, want ErrMalformed", c.name, e, err)
		}
	}
	if err := parse(good); err != nil {
		t.Errorf("a possible entry: %v", err)
	}
	atEnd := good
	atEnd.FrameLength = uint32(logLength - atEnd.Offset)
	if err := parse(atEnd); err != nil {
		t.Errorf("an entry ending exactly at the covered log: %v", err)
	}
	smallest := good
	smallest.FrameLength = FrameFixedSize
	if err := parse(smallest); err != nil {
		t.Errorf("an entry of the smallest frame: %v", err)
	}
}
