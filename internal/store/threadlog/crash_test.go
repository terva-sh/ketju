package threadlog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
)

// A power cut lands wherever it lands. These tests put it everywhere.
//
// The existing recovery suite covers the final frame being cut short and the
// file header being cut into. What it does not cover, and what a crash
// actually produces, is the rest of the space: a cut at any byte of the whole
// log, a byte of the final frame arriving wrong rather than not arriving, and
// a length field that survives the crash reading as a plausible number while
// naming bytes that were never written.

// durablePrefix returns the largest frame boundary at or below cut, which is
// the only answer recovery is allowed to give.
func durablePrefix(lg builtLog, cut int64) (end int64, frames int) {
	end = FileHeaderSize
	for _, fe := range lg.frameEnds {
		if fe > cut {
			break
		}
		end = fe
		frames++
	}
	return end, frames
}

// indexAt returns the index a reader must end up with when the durable prefix
// ends at end.
//
// Derived by replaying to that point rather than counted from the frame list.
// buildLog's frames are not one post each — it writes a thread event, six
// posts, an edit, two deletes, a restore and a rename — and a test that
// assumes otherwise is asserting its own arithmetic rather than the recovery.
func indexAt(t *testing.T, lg builtLog, end int64) *Index {
	t.Helper()
	want := &Index{Header: IndexHeader{ThreadID: lg.incremental.Header.ThreadID}}
	if _, err := replayInto(want, bytes.NewReader(lg.bytes), FileHeaderSize, end); err != nil {
		t.Fatalf("building the expected index at %d: %v", end, err)
	}
	want.Header.LogLength = uint64(end)
	want.Header.PostCount = uint32(want.Len())
	return want
}

// Criterion: every truncation offset recovers to a valid prefix with no error
// escaping to a caller, and the recovered post count is exactly the number of
// frames that were fully durable.
//
// Every offset in the whole log, not only the final frame: a crash during a
// group commit leaves the tail of a multi-frame write missing, so the cut can
// land several frames back.
func TestEveryTruncationRecoversToTheDurablePrefix(t *testing.T) {
	lg := buildLog(t)

	for cut := int64(FileHeaderSize); cut <= int64(len(lg.bytes)); cut++ {
		wantEnd, _ := durablePrefix(lg, cut)

		rec, err := Open(bytes.NewReader(lg.bytes[:cut]), cut, nil)
		if err != nil {
			t.Fatalf("cut at %d of %d: %v", cut, len(lg.bytes), err)
		}
		if rec.LogLength != wantEnd {
			t.Fatalf("cut at %d: valid prefix = %d, want %d", cut, rec.LogLength, wantEnd)
		}
		if rec.Torn != cut-wantEnd {
			t.Fatalf("cut at %d: torn = %d, want %d", cut, rec.Torn, cut-wantEnd)
		}
		want := indexAt(t, lg, wantEnd)
		if got := rec.Index.Len(); got != want.Len() {
			t.Fatalf("cut at %d: recovered %d posts, want %d — recovery must not keep a frame "+
				"that was not fully durable, nor drop one that was", cut, got, want.Len())
		}
		sameIndex(t, rec.Index, want, "cut at "+itoa(int(cut)))
		// Whatever survived must be readable, not merely counted.
		if rec.Index.Len() > 0 {
			r := NewReader(bytes.NewReader(lg.bytes[:cut]), rec.Index)
			posts, err := r.Range(1, uint32(rec.Index.Len()))
			if err != nil {
				t.Fatalf("cut at %d: the recovered index points at unreadable posts: %v", cut, err)
			}
			if len(posts) != rec.Index.Len() {
				t.Fatalf("cut at %d: index holds %d posts, %d read back", cut, rec.Index.Len(), len(posts))
			}
		}
	}
}

// Criterion: a truncation that leaves a plausible but wrong length field is
// caught by CRC rather than accepted.
//
// This is the case the CRC covers the length for. A crash can leave the four
// bytes of a length field on disk while the body they describe never arrived,
// and the number sitting there is a number — nothing about it looks wrong. If
// it happens to name fewer bytes than the frame really had, those bytes parse
// as a whole frame and the CRC is the only thing standing between a reader and
// a confidently wrong record.
func TestAPlausibleButWrongLengthIsCaughtByTheChecksum(t *testing.T) {
	lg := buildLog(t)
	lastGood := lg.frameEnds[len(lg.frameEnds)-2]
	realLen := binary.LittleEndian.Uint32(lg.bytes[lastGood+frLength:])

	// Every shorter length that still leaves a frame inside the file. Each one
	// is a number a reader could believe.
	tried := 0
	for shorter := uint32(frameLengthBase); shorter < realLen; shorter++ {
		total := int64(4 + shorter + 4)
		if lastGood+total > int64(len(lg.bytes)) {
			continue
		}
		tried++

		damaged := bytes.Clone(lg.bytes)
		binary.LittleEndian.PutUint32(damaged[lastGood+frLength:], shorter)
		// Cut the file exactly where the forged length says the frame ends, so
		// the frame is not short — it is complete, and wrong.
		cut := lastGood + total

		rec, err := Open(bytes.NewReader(damaged[:cut]), cut, nil)
		if err != nil {
			// Refused outright is fine: the reader did not believe it.
			continue
		}
		if rec.LogLength != lastGood {
			t.Fatalf("length forged to %d (real %d): recovery accepted %d bytes, want the prefix at %d — "+
				"a forged length was believed", shorter, realLen, rec.LogLength, lastGood)
		}
		if want := indexAt(t, lg, lastGood); rec.Index.Len() != want.Len() {
			t.Fatalf("length forged to %d: recovered %d posts, want %d", shorter, rec.Index.Len(), want.Len())
		}
	}
	if tried == 0 {
		t.Fatal("no forged length fitted inside the file; the test checked nothing")
	}
	t.Logf("checked %d plausible-but-wrong lengths", tried)
}

// Corrupting the length of a frame in the MIDDLE of the log is different: the
// bytes after it were durable, so silently truncating to it would delete posts
// that survived the crash. That must be an error, not a recovery.
func TestAForgedLengthMidLogIsAnErrorNotATruncation(t *testing.T) {
	lg := buildLog(t)
	start := lg.frameEnds[1] // the third frame, with several after it
	realLen := binary.LittleEndian.Uint32(lg.bytes[start+frLength:])

	damaged := bytes.Clone(lg.bytes)
	binary.LittleEndian.PutUint32(damaged[start+frLength:], realLen-1)

	rec, err := Open(bytes.NewReader(damaged), int64(len(damaged)), nil)
	if err == nil {
		t.Fatalf("a forged length mid-log was accepted, recovering %d posts; the frames after it were "+
			"durable and would have been silently discarded", rec.Index.Len())
	}
	if !errors.Is(err, ErrChecksum) && !errors.Is(err, ErrMalformed) {
		t.Errorf("error = %v, want ErrChecksum or ErrMalformed", err)
	}
}

// The ticket asks for corruption as well as truncation: a byte that arrived
// wrong rather than not arriving.
//
// In the final frame this is indistinguishable from a partially flushed write
// — the disk may have written the sectors out of order — so the rule is to
// treat it as a torn tail and fall back to the previous boundary. Every byte
// of that frame, including its length field and its CRC.
func TestASingleBadByteInTheFinalFrameFallsBackToThePreviousBoundary(t *testing.T) {
	lg := buildLog(t)
	lastGood := lg.frameEnds[len(lg.frameEnds)-2]
	wantPosts := indexAt(t, lg, lastGood).Len()

	for at := lastGood; at < int64(len(lg.bytes)); at++ {
		for _, mask := range []byte{0x01, 0x80, 0xff} {
			damaged := bytes.Clone(lg.bytes)
			damaged[at] ^= mask

			rec, err := Open(bytes.NewReader(damaged), int64(len(damaged)), nil)
			if err != nil {
				// Refusing is acceptable; believing it is not. A corrupt
				// length can name a frame that runs past EOF, which is a
				// short read rather than a bad checksum.
				continue
			}
			if rec.LogLength > lastGood {
				t.Fatalf("byte %d (%d into the final frame) flipped with %#02x: recovery kept %d bytes, "+
					"want at most %d — a damaged final frame was accepted",
					at, at-lastGood, mask, rec.LogLength, lastGood)
			}
			if rec.Index.Len() > wantPosts {
				t.Fatalf("byte %d flipped with %#02x: recovered %d posts, want at most %d",
					at, mask, rec.Index.Len(), wantPosts)
			}
		}
	}
}

// A flip anywhere before the final frame, OUTSIDE a length field, must be
// refused. Nothing rewrites those bytes, so a bad CRC there is corruption, and
// truncating to it would throw away posts that were acknowledged.
func TestASingleBadByteBeforeTheFinalFrameIsRefused(t *testing.T) {
	lg := buildLog(t)
	secondLast := lg.frameEnds[len(lg.frameEnds)-2]
	inLength := lengthFieldBytes(lg)

	for at := int64(FileHeaderSize); at < secondLast; at++ {
		if inLength[at] {
			continue // see TestACorruptLengthIsIndistinguishableFromATear
		}
		damaged := bytes.Clone(lg.bytes)
		damaged[at] ^= 0xff

		rec, err := Open(bytes.NewReader(damaged), int64(len(damaged)), nil)
		if err == nil {
			t.Fatalf("byte %d flipped was accepted, recovering %d posts and %d bytes; "+
				"the frames after it were durable", at, rec.Index.Len(), rec.LogLength)
		}
	}
}

// lengthFieldBytes marks the four bytes of every frame's length field.
func lengthFieldBytes(lg builtLog) map[int64]bool {
	out := map[int64]bool{}
	mark := func(start int64) {
		for i := int64(0); i < 4; i++ {
			out[start+i] = true
		}
	}
	mark(FileHeaderSize)
	for _, fe := range lg.frameEnds {
		mark(fe)
	}
	return out
}

// The length field is the one place where corruption and a torn write cannot
// be told apart, and it is worth pinning down rather than leaving as a gap in
// another test.
//
// A frame whose length overruns the end of the file is exactly what an
// interrupted write leaves behind, so recovery treats it as a torn tail and
// falls back to the last good boundary. A flipped bit in a length field
// produces the same shape, and no amount of checksum helps: the CRC sits at
// the end of the frame, and the length is what says where that is. The bytes
// are unreachable before they can be checked.
//
// Measured on this log: 35 of the 762 byte positions before the final frame
// can be flipped and accepted, and every one of them is inside a length field.
//
// The consequence is bounded, and the bound is the point. Recovery never
// invents a post and never reports a prefix LONGER than the truth — the
// failure is losing durable records on a rebuild, not serving wrong ones. And
// a rebuild is the rare path: the sidecar index records the log length it is
// valid to, so an ordinary open trusts it and never walks the frames at all,
// which the last case here checks.
func TestACorruptLengthIsIndistinguishableFromATear(t *testing.T) {
	lg := buildLog(t)
	secondLast := lg.frameEnds[len(lg.frameEnds)-2]
	inLength := lengthFieldBytes(lg)
	full := int64(len(lg.bytes))

	accepted := 0
	for at := int64(FileHeaderSize); at < secondLast; at++ {
		if !inLength[at] {
			continue
		}
		damaged := bytes.Clone(lg.bytes)
		damaged[at] ^= 0xff

		rec, err := Open(bytes.NewReader(damaged), full, nil)
		if err != nil {
			continue // refused, which is the better outcome where it happens
		}
		accepted++

		// Never longer than the truth, and never more posts than really exist.
		if rec.LogLength > full {
			t.Fatalf("byte %d: recovery claims %d bytes, past the %d-byte file", at, rec.LogLength, full)
		}
		if rec.Index.Len() > lg.incremental.Len() {
			t.Fatalf("byte %d: recovery invented posts — %d against %d real",
				at, rec.Index.Len(), lg.incremental.Len())
		}
		// And what it did keep must be a real prefix, readable end to end.
		if want := indexAt(t, lg, rec.LogLength); rec.Index.Len() != want.Len() {
			t.Fatalf("byte %d: recovered %d posts at prefix %d, want %d",
				at, rec.Index.Len(), rec.LogLength, want.Len())
		}
	}
	if accepted == 0 {
		t.Fatal("no corrupt length was accepted; this test is no longer describing the format")
	}
	t.Logf("%d corrupt length fields were taken for a torn tail", accepted)

	// The protection in practice: with a valid sidecar, an ordinary open
	// trusts the index and never parses a frame, so a corrupt length in the
	// middle of the log does not truncate anything.
	sidecar := lg.incremental.AppendTo(nil)
	for at := int64(FileHeaderSize); at < secondLast; at++ {
		if !inLength[at] {
			continue
		}
		damaged := bytes.Clone(lg.bytes)
		damaged[at] ^= 0xff

		rec, err := Open(bytes.NewReader(damaged), full, sidecar)
		if err != nil {
			t.Fatalf("byte %d with a valid sidecar: %v", at, err)
		}
		if rec.Outcome != Trusted {
			t.Errorf("byte %d with a valid sidecar: outcome %v, want Trusted", at, rec.Outcome)
		}
		if rec.LogLength != full {
			t.Errorf("byte %d with a valid sidecar: kept %d bytes, want %d", at, rec.LogLength, full)
		}
	}
}

// Criterion: a log truncated inside the file header is refused with a clear
// error rather than producing an empty thread.
//
// The existing suite checks the boundary. This checks the message is worth
// reading, because an operator meeting this has lost a thread's identity and
// needs to be told that rather than shown a zero.
func TestTheHeaderTruncationErrorSaysWhatHappened(t *testing.T) {
	lg := buildLog(t)
	for _, n := range []int64{0, 1, FileHeaderSize / 2, FileHeaderSize - 1} {
		_, err := Open(bytes.NewReader(lg.bytes[:n]), n, nil)
		if !errors.Is(err, ErrLogTooShort) {
			t.Fatalf("%d bytes: %v, want ErrLogTooShort", n, err)
		}
		msg := err.Error()
		if !bytes.Contains([]byte(msg), []byte(fmt.Sprint(FileHeaderSize))) {
			t.Errorf("%d bytes: message %q does not mention the %d-byte header it needed",
				n, msg, FileHeaderSize)
		}
	}
}

// A crash between the log fsync and the index write leaves a stale sidecar.
// That is the ordinary case the header's log length exists for, and it must
// extend rather than rebuild or mistrust.
func TestACrashBetweenLogAndIndexExtendsTheSidecar(t *testing.T) {
	lg := buildLog(t)

	for _, upTo := range lg.frameEnds[:len(lg.frameEnds)-1] {
		// The sidecar as it stood when the crash hit: valid, but describing
		// less log than is now on disk.
		stale := &Index{Header: IndexHeader{ThreadID: lg.incremental.Header.ThreadID}}
		if _, err := replayInto(stale, bytes.NewReader(lg.bytes), FileHeaderSize, upTo); err != nil {
			t.Fatalf("building a stale index to %d: %v", upTo, err)
		}
		stale.Header.LogLength = uint64(upTo)
		stale.Header.PostCount = uint32(stale.Len())

		rec, err := Open(bytes.NewReader(lg.bytes), int64(len(lg.bytes)), stale.AppendTo(nil))
		if err != nil {
			t.Fatalf("sidecar stale at %d: %v", upTo, err)
		}
		if rec.Outcome != Extended {
			t.Errorf("sidecar stale at %d: outcome %v, want Extended", upTo, rec.Outcome)
		}
		if rec.Replayed != int64(len(lg.bytes))-upTo {
			t.Errorf("sidecar stale at %d: replayed %d bytes, want %d",
				upTo, rec.Replayed, int64(len(lg.bytes))-upTo)
		}
		sameIndex(t, rec.Index, lg.incremental, "extended from "+itoa(int(upTo)))
	}
}
