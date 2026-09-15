package threadlog

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// Outcome says how an index was obtained when a thread was opened.
//
// Worth reporting rather than hiding, because the three cost wildly different
// amounts — a stat comparison, a few kilobytes of replay, or a sequential read
// of the whole log — and an operator seeing Rebuilt on every open has a problem
// that no error will ever tell them about.
type Outcome int

const (
	// Trusted means the index covered the log and nothing was read from it.
	Trusted Outcome = iota

	// Extended means the index was good but short, so the records past its
	// recorded log length were replayed onto it. The ordinary result of a
	// crash between the log fsync and the index header write.
	Extended

	// Rebuilt means the index was missing, unreadable, or inconsistent, so it
	// was rebuilt by replaying the whole log.
	Rebuilt
)

func (o Outcome) String() string {
	switch o {
	case Trusted:
		return "trusted"
	case Extended:
		return "extended"
	case Rebuilt:
		return "rebuilt"
	}
	return fmt.Sprintf("Outcome(%d)", int(o))
}

// Recovery is the result of opening a thread.
type Recovery struct {
	Index   *Index
	Outcome Outcome

	// Header is the log's file header.
	Header FileHeader

	// LogLength is the length of the valid prefix: the end of the last frame
	// that was fully durable. Bytes past it are a torn write and belong to
	// nobody.
	LogLength int64

	// Torn is how many bytes sit past LogLength. Non-zero means the last write
	// did not survive, which is expected after a power cut and alarming
	// otherwise.
	Torn int64

	// Replayed is how many bytes of log were read to produce the index. Zero
	// when Trusted.
	Replayed int64
}

// ErrLogTooShort means the file is smaller than the fixed file header, so
// there is nothing to open — not even an empty thread.
//
// Distinguished from an empty thread on purpose. A log truncated inside its own
// header has lost the thread id and the creation time, and reporting that as a
// thread with no posts would invent a thread that never existed.
var ErrLogTooShort = errors.New("threadlog: log is shorter than its file header")

// Open reads a thread's log and sidecar index and returns an index that
// describes the log.
//
// This is the open path from docs/03: read both headers, and if the index's
// recorded log length equals the log's size, trust it. Otherwise replay from
// the index's recorded length. A missing or corrupt index is rebuilt by a full
// replay.
//
// idx may be nil or empty, which is the missing-index case and is not an error.
//
// The log is read through a ReaderAt rather than a path so that a caller can
// count what was read, which is how the "trusted with no replay" property is
// tested rather than asserted.
func Open(log io.ReaderAt, logSize int64, idx []byte) (*Recovery, error) {
	if logSize < FileHeaderSize {
		return nil, fmt.Errorf("%w: %d bytes, need at least %d",
			ErrLogTooShort, logSize, FileHeaderSize)
	}

	var hb [FileHeaderSize]byte
	if _, err := log.ReadAt(hb[:], 0); err != nil {
		return nil, fmt.Errorf("threadlog: reading file header: %w", err)
	}
	fh, err := ParseFileHeader(hb[:])
	if err != nil {
		return nil, err
	}
	if err := fh.CheckVersion(); err != nil {
		return nil, err
	}

	// The fast path, and the one that has to stay cheap: a comparison against
	// stat(), with not one byte of the log body read.
	if ix, err := OpenIndex(idx, logSize); err == nil {
		return &Recovery{
			Index: ix, Outcome: Trusted, Header: fh, LogLength: logSize,
		}, nil
	} else if !RebuildRequired(err) {
		return nil, err
	}

	// A readable index that is merely short is worth keeping: replaying a few
	// kilobytes beats re-reading a hundred megabytes. Anything else is rebuilt.
	base := &Index{Header: IndexHeader{ThreadID: fh.ThreadID}}
	from := int64(FileHeaderSize)
	outcome := Rebuilt

	if partial, err := ParseIndex(idx); err == nil && partial.Header.LogLength <= uint64(logSize) &&
		partial.Header.LogLength >= FileHeaderSize {
		base = partial
		from = int64(partial.Header.LogLength)
		outcome = Extended
	}

	end, err := replayInto(base, log, from, logSize)
	if err != nil {
		return nil, err
	}

	base.Header.ThreadID = fh.ThreadID
	base.Header.LogLength = uint64(end)
	base.Header.PostCount = uint32(len(base.entries))

	return &Recovery{
		Index:     base,
		Outcome:   outcome,
		Header:    fh,
		LogLength: end,
		Torn:      logSize - end,
		Replayed:  end - from,
	}, nil
}

// replayInto folds every whole frame in [from, logSize) into ix and returns the
// offset just past the last one that was complete and intact.
//
// A torn final frame is where this earns its keep. The last write may have
// landed partially, so the tail can be a short frame, a frame whose length
// field is plausible but wrong, or a frame whose body never arrived. All three
// stop the replay at the last good boundary rather than failing, because the
// bytes past that boundary were never acknowledged to anyone.
//
// Damage anywhere EARLIER is different and is returned. A bad CRC in the middle
// of a log is not a torn write — nothing rewrites those bytes — so it is
// corruption, and truncating a log back to it would delete durable posts.
func replayInto(ix *Index, log io.ReaderAt, from, logSize int64) (int64, error) {
	// Strictly larger than MaxFrameSize, so a single frame can never straddle
	// two windows. That turns "nothing whole in this window" into an
	// unambiguous statement about the file rather than about the buffer.
	const chunk = 2 * MaxFrameSize

	buf := make([]byte, 0, chunk)
	off := from

	for off < logSize {
		want := logSize - off
		if want > chunk {
			want = chunk
		}
		buf = buf[:want]
		if _, err := log.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
			return 0, fmt.Errorf("threadlog: reading log at %d: %w", off, err)
		}

		consumed, err := applyFrames(ix, buf, off, logSize)
		if err != nil {
			return 0, err
		}
		if consumed == 0 {
			// Nothing whole in this window. Either the tail is torn, or a
			// single frame is larger than the window — and it cannot be, since
			// MaxFrameSize is at most the window size.
			break
		}
		off += consumed
	}

	return off, nil
}

// applyFrames folds whole frames from b into ix and reports how many bytes it
// consumed. base is b's offset within the log.
//
// Telling a torn write from corruption is the whole job here, and the two are
// not interchangeable: discarding a torn tail loses bytes nobody was promised,
// while discarding corruption loses posts that were acknowledged as durable.
// The distinction is made from the FILE, not from the buffer — an earlier
// version asked whether the remaining bytes reached EOF, which is true of every
// frame when the window covers the whole log, and so quietly truncated a log to
// its first post on any mid-log damage.
//
// What a pure truncation actually looks like, which is what makes this
// decidable: the log is cut at some offset inside the final frame. Either the
// 4-byte length field survived intact, in which case it reports the frame's
// true length and the file is too short to hold it; or it did not survive, in
// which case there are fewer than 32 bytes left and the header will not parse.
// Both are short-buffer shapes. Neither is a checksum failure.
//
// So a checksum failure means every byte the frame claimed was present and one
// of them is wrong. That is corruption — unless the frame ends exactly at EOF,
// where a partially-flushed final write can leave a frame that is whole in
// length and wrong in content.
func applyFrames(ix *Index, b []byte, base, logSize int64) (int64, error) {
	var consumed int64

	for {
		rest := b[consumed:]
		if len(rest) == 0 {
			return consumed, nil
		}

		at := base + consumed
		toEOF := logSize - at
		damaged := func(err error) (int64, error) {
			return 0, fmt.Errorf("threadlog: log damaged at offset %d: %w", at, err)
		}

		_, size, err := ParseFrameHeader(rest)
		if err != nil {
			if int64(len(rest)) < toEOF {
				// The window ended mid-header. Not damage; read more.
				return consumed, nil
			}
			// Fewer than a header's worth of bytes left in the FILE: the final
			// write did not finish.
			if errors.Is(err, ErrShortBuffer) || errors.Is(err, ErrMalformed) {
				return consumed, nil
			}
			return damaged(err)
		}

		if int64(size) > toEOF {
			// The frame claims more than the file holds. This is the ordinary
			// signature of a cut inside the final frame.
			//
			// It is also indistinguishable from a corrupted length field in a
			// mid-log frame, which would discard every post after it. Nothing
			// here can tell those apart: the CRC that would settle it covers
			// bytes this frame says are present and are not. `ketju fsck`
			// (TKT-01M29C2WVT) is where that belongs, with the whole file and
			// an operator to answer to.
			return consumed, nil
		}
		if int64(len(rest)) < int64(size) {
			// Whole in the file, split across the window. Read more.
			return consumed, nil
		}

		h, body, _, err := ParseFrame(rest)
		if err != nil {
			if errors.Is(err, ErrChecksum) && at+int64(size) == logSize {
				// Whole in length, wrong in content, and nothing after it: a
				// final write that reached the disk in pieces.
				return consumed, nil
			}
			return damaged(err)
		}

		if err := ix.Apply(h, body, uint64(at), size); err != nil {
			return 0, fmt.Errorf("threadlog: applying the record at offset %d: %w", at, err)
		}
		consumed += int64(size)
	}
}

// Truncate cuts a log back to its valid prefix, discarding a torn final write.
//
// This is the one place the engine deletes bytes on purpose, and it is safe
// only because of two facts that hold together: there is exactly one writer, so
// nothing else can be appending while this runs, and every frame carries a CRC
// over its own length field, so a partial write cannot masquerade as a whole
// one. Remove either and this becomes data loss.
//
// A no-op when nothing was torn, so a caller can call it unconditionally.
func (r *Recovery) Truncate(f *os.File) error {
	if r.Torn == 0 {
		return nil
	}
	if err := f.Truncate(r.LogLength); err != nil {
		return fmt.Errorf("threadlog: truncating a torn tail of %d bytes: %w", r.Torn, err)
	}
	return f.Sync()
}
