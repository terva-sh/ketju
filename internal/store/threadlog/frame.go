package threadlog

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

const (
	// FrameFixedSize is the fixed part of a frame, from the length field
	// through the actor id. The body begins immediately after it.
	//
	// The split is the point of the format: this much can be read and
	// understood without decoding a body, which is what lets the reindexer and
	// the index builder walk an entire log cheaply.
	FrameFixedSize = 32

	// frameLengthBase is what the length field counts besides the body: the
	// fixed part minus the four bytes of the length field itself. So
	//
	//	L = frameLengthBase + len(body)
	//
	// the body occupies bytes [FrameFixedSize, 4+L), and the whole frame is
	// 4 + L + 4 bytes including the length field and the trailing CRC.
	frameLengthBase = FrameFixedSize - 4 // 28

	// MaxFrameSize caps the total on-disk size of one frame.
	//
	// The length field is read from disk and everything else is sized from it,
	// so it is bounded before it is believed.
	//
	// This is a FORMAT PARAMETER, not a defensive guess: a reader refusing a
	// frame the writer was willing to produce is a compatibility break. Frozen
	// at 1 MiB in TKT-01M29C5Y1E after measuring the largest legitimate body,
	// which is not a post. Every record type shares this bound, and the
	// binding case is PostsSplitOut carrying ordinals[] — about 250 KB for a
	// split naming all 50 000 posts of the largest plausible thread, so the
	// real margin is 4x to 7x rather than the 16x that "a post body is capped
	// at 64 KB" suggests. See "Format freeze" in docs/03.
	MaxFrameSize = 1 << 20
)

// Field offsets within the fixed part of a frame, from the docs/03 table.
const (
	frLength   = 0  // 4 bytes
	frType     = 4  // 1
	frEncoding = 5  // 1
	frFlags    = 6  // 2
	frOrdinal  = 8  // 4
	frRevision = 12 // 4
	frTime     = 16 // 8
	frActor    = 24 // 8
	frBody     = 32
)

// RecordType identifies what a record says. The catalogue of types and their
// bodies is defined separately; this package only moves the byte.
type RecordType uint8

// BodyEncoding names how a body is encoded.
type BodyEncoding uint8

const (
	// EncodingCBOR is the default that writers use.
	EncodingCBOR BodyEncoding = 1
	// EncodingJSON is supported for dumps and debugging.
	EncodingJSON BodyEncoding = 2
)

// FrameHeader is the fixed part of a record frame.
//
// Flags carries bits this build may not know. They round-trip untouched, which
// is how a log written by a newer version stays readable. See [FrameFlags] for
// the bit assignments; nothing in the parser interprets them.
type FrameHeader struct {
	Type        RecordType
	Encoding    BodyEncoding
	Flags       FrameFlags
	Ordinal     uint32
	Revision    uint32
	TimestampMS int64
	Actor       uint64
}

// AppendFrame appends an encoded frame to dst and returns the extended slice.
//
// It returns [ErrMalformed] if the resulting frame would exceed
// [MaxFrameSize], so a writer cannot produce a frame this package's parser
// would then refuse to read.
func AppendFrame(dst []byte, h FrameHeader, body []byte) ([]byte, error) {
	total := FrameFixedSize + len(body) + 4
	if total > MaxFrameSize {
		return dst, fmt.Errorf("%w: frame of %d bytes exceeds the %d byte cap",
			ErrMalformed, total, MaxFrameSize)
	}

	start := len(dst)
	dst = append(dst, make([]byte, total)...)
	b := dst[start:]

	l := uint32(frameLengthBase + len(body))
	binary.LittleEndian.PutUint32(b[frLength:], l)
	b[frType] = byte(h.Type)
	b[frEncoding] = byte(h.Encoding)
	binary.LittleEndian.PutUint16(b[frFlags:], uint16(h.Flags))
	binary.LittleEndian.PutUint32(b[frOrdinal:], h.Ordinal)
	binary.LittleEndian.PutUint32(b[frRevision:], h.Revision)
	binary.LittleEndian.PutUint64(b[frTime:], uint64(h.TimestampMS))
	binary.LittleEndian.PutUint64(b[frActor:], h.Actor)
	copy(b[frBody:], body)
	binary.LittleEndian.PutUint32(b[4+l:], crc32.Checksum(b[:4+l], castagnoli))

	return dst, nil
}

// ParseFrameHeader decodes the fixed part of the frame at the start of b and
// returns the total on-disk size of that frame.
//
// It reads no further than [FrameFixedSize] bytes and does not verify the
// checksum, because verifying would mean reading the body this function exists
// to avoid. Walking a log is therefore: parse, act on the header, advance by
// the returned size.
//
// The body may well be damaged, and this function will not say so. Anything
// that consumes a body must go through [ParseFrame].
func ParseFrameHeader(b []byte) (FrameHeader, int, error) {
	if len(b) < FrameFixedSize {
		return FrameHeader{}, 0, fmt.Errorf("%w: frame header needs %d bytes, got %d",
			ErrShortBuffer, FrameFixedSize, len(b))
	}

	l := binary.LittleEndian.Uint32(b[frLength:])
	if l < frameLengthBase {
		return FrameHeader{}, 0, fmt.Errorf("%w: frame length %d is below the %d byte minimum",
			ErrMalformed, l, frameLengthBase)
	}
	// Bound before widening, so a hostile length cannot overflow the addition
	// on a 32-bit int.
	if l > MaxFrameSize-8 {
		return FrameHeader{}, 0, fmt.Errorf("%w: frame length %d exceeds the %d byte cap",
			ErrMalformed, l, MaxFrameSize)
	}

	return FrameHeader{
		Type:        RecordType(b[frType]),
		Encoding:    BodyEncoding(b[frEncoding]),
		Flags:       FrameFlags(binary.LittleEndian.Uint16(b[frFlags:])),
		Ordinal:     binary.LittleEndian.Uint32(b[frOrdinal:]),
		Revision:    binary.LittleEndian.Uint32(b[frRevision:]),
		TimestampMS: int64(binary.LittleEndian.Uint64(b[frTime:])),
		Actor:       binary.LittleEndian.Uint64(b[frActor:]),
	}, int(l) + 8, nil
}

// ParseFrame decodes the frame at the start of b and verifies its checksum.
// It returns the header, the body, and the total on-disk size of the frame.
//
// The returned body aliases b and is valid only while b is. The checksum
// covers the length field as well as the header and body, so a corrupted
// length cannot pass as a valid frame.
func ParseFrame(b []byte) (FrameHeader, []byte, int, error) {
	h, total, err := ParseFrameHeader(b)
	if err != nil {
		return FrameHeader{}, nil, 0, err
	}
	if len(b) < total {
		return FrameHeader{}, nil, 0, fmt.Errorf("%w: frame needs %d bytes, got %d",
			ErrShortBuffer, total, len(b))
	}

	end := total - 4 // first byte of the trailing CRC
	stored := binary.LittleEndian.Uint32(b[end:])
	computed := crc32.Checksum(b[:end], castagnoli)
	if stored != computed {
		return FrameHeader{}, nil, 0, &ChecksumError{
			Part: "frame", Ordinal: h.Ordinal, Stored: stored, Computed: computed,
		}
	}

	return h, b[frBody:end], total, nil
}
