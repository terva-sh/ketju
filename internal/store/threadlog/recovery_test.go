package threadlog

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// builtLog is a log plus the index an incremental maintainer produced while
// writing it — the thread actor's half of the property that TKT-01M29C2WT7
// will test properly. Building it here as the records are appended, rather
// than by replaying afterwards, is what makes the comparison mean anything.
type builtLog struct {
	bytes []byte
	// incremental is the index as maintained record by record at write time.
	incremental *Index
	// frameEnds[i] is the offset just past frame i, so a test can cut the log
	// at a known boundary.
	frameEnds []int64
}

func buildLog(t *testing.T) builtLog {
	t.Helper()

	const threadID = 0x3f7a9c21e0000001
	out := builtLog{
		bytes:       NewFileHeader(threadID, 1_700_000_000_000, 7).AppendTo(nil),
		incremental: &Index{Header: IndexHeader{ThreadID: threadID}},
	}

	add := func(h FrameHeader, v any) {
		body, err := EncodeBody(EncodingCBOR, v)
		if err != nil {
			t.Fatalf("EncodeBody: %v", err)
		}
		off := len(out.bytes)
		out.bytes, err = AppendFrame(out.bytes, h, body)
		if err != nil {
			t.Fatalf("AppendFrame: %v", err)
		}
		size := len(out.bytes) - off

		// The actor's side: fold the record in as it is written.
		if err := out.incremental.Apply(h, body, uint64(off), size); err != nil {
			t.Fatalf("incremental Apply: %v", err)
		}
		out.frameEnds = append(out.frameEnds, int64(len(out.bytes)))
	}

	ts := int64(1_700_000_000_000)
	add(FrameHeader{Type: TypeThreadCreated, Encoding: EncodingCBOR, TimestampMS: ts, Actor: 1},
		&ThreadCreatedBody{Title: "Why we left Slack", Slug: "why-we-left-slack", ForumID: 7})

	for i := 1; i <= 6; i++ {
		h := FrameHeader{
			Type: TypePost, Encoding: EncodingCBOR, Ordinal: uint32(i), Revision: 1,
			TimestampMS: ts + int64(i), Actor: uint64(100 + i%3),
		}
		b := &PostBody{Source: "post number " + string(rune('0'+i))}
		if i > 1 {
			b.ReplyTo = uint32(i - 1)
		}
		if i == 4 {
			b.Attachments = []string{"sha256:abcd"}
		}
		if i == 5 {
			// An agent wrote this one, so the frame flag crosses into the entry.
			h.Flags = FrameFlagMachineAuthored
		}
		add(h, b)
	}

	// An edit, a delete of an already-edited post, a restore, and a thread
	// event with no per-post effect.
	add(FrameHeader{Type: TypeEdit, Encoding: EncodingCBOR, Ordinal: 2, Revision: 2,
		TimestampMS: ts + 100, Actor: 101}, &EditBody{Source: "post number 2, fixed", Reason: "typo", ReplyTo: 1})
	add(FrameHeader{Type: TypeDelete, Encoding: EncodingCBOR, Ordinal: 2, Revision: 2,
		TimestampMS: ts + 101, Actor: 9}, &DeleteBody{Reason: "spam"})
	add(FrameHeader{Type: TypeRestore, Encoding: EncodingCBOR, Ordinal: 2, Revision: 2,
		TimestampMS: ts + 102, Actor: 9}, &RestoreBody{Reason: "misflagged"})
	add(FrameHeader{Type: TypeDelete, Encoding: EncodingCBOR, Ordinal: 5, Revision: 1,
		TimestampMS: ts + 103, Actor: 9}, &DeleteBody{Reason: "off topic"})
	add(FrameHeader{Type: TypeThreadRenamed, Encoding: EncodingCBOR,
		TimestampMS: ts + 104, Actor: 1}, &ThreadRenamedBody{Title: "Leaving Slack", Slug: "leaving-slack"})

	out.incremental.Header.LogLength = uint64(len(out.bytes))
	out.incremental.Header.PostCount = uint32(out.incremental.Len())
	return out
}

func sameIndex(t *testing.T, got, want *Index, what string) {
	t.Helper()
	if got.Header != want.Header {
		t.Errorf("%s: header = %+v, want %+v", what, got.Header, want.Header)
	}
	if !reflect.DeepEqual(got.entries, want.entries) {
		t.Errorf("%s: entries differ", what)
		for i := range max(len(got.entries), len(want.entries)) {
			var g, w string
			if i < len(got.entries) {
				g = shortEntry(got.entries[i])
			}
			if i < len(want.entries) {
				w = shortEntry(want.entries[i])
			}
			if g != w {
				t.Errorf("  ordinal %d:\n    got  %s\n    want %s", i+1, g, w)
			}
		}
	}
	// The byte-identical claim the design rests on, checked on the encoding
	// rather than only on the structs.
	if !bytes.Equal(got.AppendTo(nil), want.AppendTo(nil)) {
		t.Errorf("%s: encoded index files differ", what)
	}
}

func shortEntry(e IndexEntry) string {
	return "off=" + itoa(int(e.Offset)) + " len=" + itoa(int(e.FrameLength)) +
		" reply=" + itoa(int(e.ReplyTo)) + " author=" + itoa(int(e.Author)) +
		" rev=" + itoa(int(e.Revision)) + " flags=" + itoa(int(e.Flags))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// Criterion: an index whose recorded log length matches the file size is
// trusted with no replay, counted in a test.
func TestOpenTrustsAMatchingIndexWithoutReplay(t *testing.T) {
	lg := buildLog(t)
	idx := lg.incremental.AppendTo(nil)

	c := &countingReaderAt{r: bytes.NewReader(lg.bytes)}
	rec, err := Open(c, int64(len(lg.bytes)), idx)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if rec.Outcome != Trusted {
		t.Errorf("outcome = %s, want trusted", rec.Outcome)
	}
	if rec.Replayed != 0 {
		t.Errorf("replayed %d bytes, want 0", rec.Replayed)
	}
	if c.bytes != FileHeaderSize {
		t.Errorf("read %d bytes of a %d byte log; a trusted index should cost "+
			"the 64-byte file header and nothing else", c.bytes, len(lg.bytes))
	}
	sameIndex(t, rec.Index, lg.incremental, "trusted")
}

// Criterion: a short index triggers replay from its recorded length rather
// than a full rebuild.
func TestOpenExtendsAShortIndexRatherThanRebuilding(t *testing.T) {
	lg := buildLog(t)

	// An index that stopped four records early: the crash-between-fsync-and-
	// index-header case that docs/03 says is the ordinary one.
	cut := lg.frameEnds[len(lg.frameEnds)-5]
	partial := &Index{Header: IndexHeader{ThreadID: lg.incremental.Header.ThreadID}}
	if _, err := replayInto(partial, bytes.NewReader(lg.bytes), FileHeaderSize, cut); err != nil {
		t.Fatalf("building the partial index: %v", err)
	}
	partial.Header.LogLength = uint64(cut)
	partial.Header.PostCount = uint32(partial.Len())

	c := &countingReaderAt{r: bytes.NewReader(lg.bytes)}
	rec, err := Open(c, int64(len(lg.bytes)), partial.AppendTo(nil))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if rec.Outcome != Extended {
		t.Errorf("outcome = %s, want extended", rec.Outcome)
	}
	if want := int64(len(lg.bytes)) - cut; rec.Replayed != want {
		t.Errorf("replayed %d bytes, want %d (from the index's recorded length to the end)",
			rec.Replayed, want)
	}
	// The point of extending rather than rebuilding: the bytes before the cut
	// are never read.
	if int64(c.bytes) >= int64(len(lg.bytes)) {
		t.Errorf("read %d bytes of a %d byte log; extending must not re-read the prefix",
			c.bytes, len(lg.bytes))
	}
	sameIndex(t, rec.Index, lg.incremental, "extended")
}

// Criterion: a missing or CRC-failing index is rebuilt by full replay and
// matches the incrementally maintained index.
func TestOpenRebuildsAndMatchesTheIncrementalIndex(t *testing.T) {
	lg := buildLog(t)
	good := lg.incremental.AppendTo(nil)

	cases := []struct {
		name string
		idx  []byte
	}{
		{"missing", nil},
		{"empty", []byte{}},
		{"corrupt header CRC", func() []byte { b := bytes.Clone(good); b[ihThreadID] ^= 0xff; return b }()},
		{"bad magic", func() []byte { b := bytes.Clone(good); b[0] = 'X'; return b }()},
		{"truncated entries", good[:len(good)-IndexEntrySize]},
		{"claims more than the log holds", func() []byte {
			ix := &Index{Header: lg.incremental.Header, entries: lg.incremental.entries}
			ix.Header.LogLength = uint64(len(lg.bytes)) + 4096
			return ix.AppendTo(nil)
		}()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, err := Open(bytes.NewReader(lg.bytes), int64(len(lg.bytes)), tc.idx)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if rec.Outcome != Rebuilt {
				t.Errorf("outcome = %s, want rebuilt", rec.Outcome)
			}
			if rec.Torn != 0 {
				t.Errorf("reported %d torn bytes on an intact log", rec.Torn)
			}
			sameIndex(t, rec.Index, lg.incremental, tc.name)
		})
	}
}

// Criterion: a torn final frame is truncated to the last good boundary and the
// thread opens with a valid prefix.
//
// Every offset inside the final frame, not a chosen one. TKT-01M29C2WTJ takes
// this further — every frame, plus single-byte corruption — but the property
// belongs here with the code that implements it.
func TestTornFinalFrameRecoversToAValidPrefix(t *testing.T) {
	lg := buildLog(t)
	lastGood := lg.frameEnds[len(lg.frameEnds)-2]

	// The index as it stood before the final record was written, which is what
	// a reader must end up with however the tear landed.
	want := &Index{Header: IndexHeader{ThreadID: lg.incremental.Header.ThreadID}}
	if _, err := replayInto(want, bytes.NewReader(lg.bytes), FileHeaderSize, lastGood); err != nil {
		t.Fatalf("building the expected index: %v", err)
	}
	want.Header.LogLength = uint64(lastGood)
	want.Header.PostCount = uint32(want.Len())

	for cut := lastGood + 1; cut < int64(len(lg.bytes)); cut++ {
		torn := lg.bytes[:cut]
		rec, err := Open(bytes.NewReader(torn), cut, nil)
		if err != nil {
			t.Fatalf("cut at %d (%d bytes into the final frame): %v", cut, cut-lastGood, err)
		}
		if rec.LogLength != lastGood {
			t.Fatalf("cut at %d: valid prefix = %d, want %d", cut, rec.LogLength, lastGood)
		}
		if rec.Torn != cut-lastGood {
			t.Fatalf("cut at %d: torn = %d, want %d", cut, rec.Torn, cut-lastGood)
		}
		sameIndex(t, rec.Index, want, "torn at "+itoa(int(cut)))
	}

	// Cutting exactly on the boundary is not a tear at all.
	rec, err := Open(bytes.NewReader(lg.bytes[:lastGood]), lastGood, nil)
	if err != nil {
		t.Fatalf("cut on the boundary: %v", err)
	}
	if rec.Torn != 0 {
		t.Errorf("a cut exactly on a frame boundary reported %d torn bytes", rec.Torn)
	}
}

// Criterion: recovery of a log truncated inside the 64-byte file header is
// refused with a clear error rather than producing an empty thread.
func TestLogTruncatedInsideTheFileHeaderIsRefused(t *testing.T) {
	lg := buildLog(t)

	for n := int64(0); n < FileHeaderSize; n++ {
		rec, err := Open(bytes.NewReader(lg.bytes[:n]), n, nil)
		if !errors.Is(err, ErrLogTooShort) {
			t.Fatalf("%d bytes: error = %v, want ErrLogTooShort", n, err)
		}
		if rec != nil {
			t.Fatalf("%d bytes: returned a thread with %d posts; a log that lost its "+
				"header has lost its identity and must not open", n, rec.Index.Len())
		}
	}
}

// Damage in the MIDDLE of a log is not a torn write and must not be truncated
// away. Nothing rewrites those bytes, so a bad CRC there is corruption, and
// cutting the log back to it would delete posts that were durable.
func TestMidLogCorruptionIsAnErrorNotATruncation(t *testing.T) {
	lg := buildLog(t)

	// Corrupt a byte inside the third frame's body.
	victim := lg.frameEnds[1] + 40
	damaged := bytes.Clone(lg.bytes)
	damaged[victim] ^= 0xff

	rec, err := Open(bytes.NewReader(damaged), int64(len(damaged)), nil)
	if err == nil {
		t.Fatalf("corruption at offset %d was accepted, recovering %d posts", victim, rec.Index.Len())
	}
	if !errors.Is(err, ErrChecksum) {
		t.Errorf("error = %v, want ErrChecksum", err)
	}
}

// A critical unknown record type stops recovery, by the docs/03 rule. An
// advisory one does not.
func TestRecoveryHonoursTheUnknownTypeRule(t *testing.T) {
	for _, tc := range []struct {
		name    string
		typ     RecordType
		wantErr bool
	}{
		{"advisory", 0x63, false},
		{"critical", 0xe3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lg := buildLog(t)
			body, err := EncodeBody(EncodingCBOR, &RestoreBody{Reason: "from the future"})
			if err != nil {
				t.Fatal(err)
			}
			log, err := AppendFrame(lg.bytes, FrameHeader{
				Type: tc.typ, Encoding: EncodingCBOR, Revision: 1,
			}, body)
			if err != nil {
				t.Fatal(err)
			}

			_, err = Open(bytes.NewReader(log), int64(len(log)), nil)
			if tc.wantErr {
				if !errors.Is(err, ErrCriticalRecordType) {
					t.Errorf("error = %v, want ErrCriticalRecordType", err)
				}
				return
			}
			if err != nil {
				t.Errorf("an advisory unknown type stopped recovery: %v", err)
			}
		})
	}
}

func TestTruncateRemovesTheTornBytes(t *testing.T) {
	lg := buildLog(t)
	lastGood := lg.frameEnds[len(lg.frameEnds)-2]
	torn := append(bytes.Clone(lg.bytes[:lastGood]), lg.bytes[lastGood:lastGood+9]...)

	path := filepath.Join(t.TempDir(), "thread.log")
	if err := os.WriteFile(path, torn, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	rec, err := Open(f, int64(len(torn)), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if rec.Torn != 9 {
		t.Fatalf("torn = %d, want 9", rec.Torn)
	}
	if err := rec.Truncate(f); err != nil {
		t.Fatalf("Truncate: %v", err)
	}

	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != lastGood {
		t.Errorf("file is %d bytes after truncation, want %d", fi.Size(), lastGood)
	}

	// Re-opening now trusts an index written against the truncated log, which
	// is the whole point of cutting the tail rather than tolerating it.
	again, err := Open(f, fi.Size(), rec.Index.AppendTo(nil))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if again.Outcome != Trusted {
		t.Errorf("reopen outcome = %s, want trusted", again.Outcome)
	}

	// And it is a no-op on an intact log.
	if err := (&Recovery{Torn: 0}).Truncate(nil); err != nil {
		t.Errorf("Truncate on an intact log: %v", err)
	}
}

// Apply is the shared definition of what a record does to the index, so the
// flags it derives are worth pinning directly rather than only through replay.
func TestApplyDerivesEntryFlagsFromTheRecord(t *testing.T) {
	lg := buildLog(t)
	ix := lg.incremental

	post4, err := ix.Entry(4)
	if err != nil {
		t.Fatal(err)
	}
	if !post4.Flags.Has(EntryHasAttachments) {
		t.Error("post 4 carried an attachment and the entry does not say so")
	}

	post5, err := ix.Entry(5)
	if err != nil {
		t.Fatal(err)
	}
	if !post5.Flags.Has(EntryMachineAuthored) {
		t.Error("post 5 was written with the machine-authored frame flag; " +
			"ToEntryFlags should have carried it into the entry")
	}
	if !post5.Flags.Has(EntryDeleted) {
		t.Error("post 5 was deleted and the entry does not say so")
	}

	post2, err := ix.Entry(2)
	if err != nil {
		t.Fatal(err)
	}
	if !post2.Flags.Has(EntryEdited) {
		t.Error("post 2 was edited and the entry does not say so")
	}
	if post2.Flags.Has(EntryDeleted) {
		t.Error("post 2 was deleted and then restored; the flag should be clear")
	}
	if post2.Revision != 2 {
		t.Errorf("post 2 revision = %d, want 2", post2.Revision)
	}
	if post2.CreatedMS != 1_700_000_000_002 {
		t.Errorf("post 2 CreatedMS = %d; an edit must not move revision 1's timestamp",
			post2.CreatedMS)
	}
}

func TestApplyRefusesAnOrdinalThatBreaksTheSequence(t *testing.T) {
	ix := &Index{}
	body, err := EncodeBody(EncodingCBOR, &PostBody{Source: "x"})
	if err != nil {
		t.Fatal(err)
	}

	// A gap: the first post must be ordinal 1.
	if err := ix.Apply(FrameHeader{Type: TypePost, Encoding: EncodingCBOR, Ordinal: 2, Revision: 1},
		body, 64, 80); !errors.Is(err, ErrMalformed) {
		t.Errorf("a gapped ordinal: error = %v, want ErrMalformed", err)
	}
	// An edit to a post that does not exist.
	eb, err := EncodeBody(EncodingCBOR, &EditBody{Source: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ix.Apply(FrameHeader{Type: TypeEdit, Encoding: EncodingCBOR, Ordinal: 1, Revision: 2},
		eb, 64, 80); !errors.Is(err, ErrMalformed) {
		t.Errorf("an edit to a missing post: error = %v, want ErrMalformed", err)
	}
}
