package threadlog

import (
	"encoding/binary"
	"fmt"
	"io"
)

// ScanRecord is one frame as the scanner found it.
//
// Err non-nil means the frame is damaged. Header and Body are then whatever
// could be recovered, which for a checksum failure is everything — the bytes
// parsed, they just do not add up. A tool reporting damage usually wants to
// show them anyway, because "ordinal 412 fails CRC" is worth far more to
// whoever is holding the broken thread than "a frame fails CRC".
type ScanRecord struct {
	Offset int64
	Size   int
	Header FrameHeader
	Body   []byte
	Err    error

	// Fatal means the scan stopped here. A checksum failure is not fatal: the
	// length was structurally sound, so the next frame's offset is known and
	// the walk continues. A length that is out of range or runs past the end
	// of the file IS fatal, because nothing after it can be located.
	Fatal bool
}

// ScanFrames walks every frame in a log without consulting an index.
//
// This is the primitive the offline tools are built on. dump and fsck both
// exist precisely for when the index or the database is the thing that is
// wrong, so neither may depend on one being right.
//
// fn returning false stops the walk.
func ScanFrames(r io.ReaderAt, size int64, fn func(ScanRecord) bool) error {
	if size < FileHeaderSize {
		return fmt.Errorf("%w: %d bytes, the file header alone is %d",
			ErrLogTooShort, size, FileHeaderSize)
	}

	for off := int64(FileHeaderSize); off < size; {
		var lb [4]byte
		if _, err := r.ReadAt(lb[:], off); err != nil {
			fn(ScanRecord{Offset: off, Err: fmt.Errorf("reading the length at %d: %w", off, err), Fatal: true})
			return nil
		}
		l := binary.LittleEndian.Uint32(lb[:])
		total := int64(4) + int64(l) + 4

		switch {
		case l < frameLengthBase:
			fn(ScanRecord{Offset: off, Err: fmt.Errorf(
				"%w: length %d is below the %d-byte minimum", ErrMalformed, l, frameLengthBase), Fatal: true})
			return nil
		case total > MaxFrameSize:
			fn(ScanRecord{Offset: off, Err: fmt.Errorf(
				"%w: frame of %d bytes exceeds the %d-byte maximum", ErrMalformed, total, MaxFrameSize), Fatal: true})
			return nil
		case off+total > size:
			fn(ScanRecord{Offset: off, Err: fmt.Errorf(
				"%w: frame of %d bytes at %d runs %d bytes past the end of the file",
				ErrShortBuffer, total, off, off+total-size), Fatal: true})
			return nil
		}

		buf := make([]byte, total)
		if _, err := r.ReadAt(buf, off); err != nil {
			fn(ScanRecord{Offset: off, Err: fmt.Errorf("reading %d bytes at %d: %w", total, off, err), Fatal: true})
			return nil
		}

		h, body, _, err := ParseFrame(buf)
		rec := ScanRecord{Offset: off, Size: int(total), Header: h, Body: body, Err: err}
		if err != nil {
			// The length was sound, so the next frame is findable. Report and
			// carry on: one bad record must not cost a reader the other
			// fifty thousand.
			rec.Header, rec.Body = headerOnly(buf), nil
		}
		if !fn(rec) {
			return nil
		}
		off += total
	}
	return nil
}

// headerOnly pulls the fixed part out of a frame whose checksum failed, so a
// damaged record can still be named rather than reported as an offset.
//
// The fields may themselves be the corrupt part. They are shown as what the
// bytes say, which is the only honest thing to show.
func headerOnly(frame []byte) FrameHeader {
	if len(frame) < FrameFixedSize {
		return FrameHeader{}
	}
	return FrameHeader{
		Type:        RecordType(frame[frType]),
		Encoding:    BodyEncoding(frame[frEncoding]),
		Flags:       FrameFlags(binary.LittleEndian.Uint16(frame[frFlags:])),
		Ordinal:     binary.LittleEndian.Uint32(frame[frOrdinal:]),
		Revision:    binary.LittleEndian.Uint32(frame[frRevision:]),
		TimestampMS: int64(binary.LittleEndian.Uint64(frame[frTime:])),
		Actor:       binary.LittleEndian.Uint64(frame[frActor:]),
	}
}
