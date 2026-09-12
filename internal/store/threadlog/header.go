package threadlog

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

// FileHeaderSize is the size of the file header, written once at offset zero.
const FileHeaderSize = 64

// Field offsets within the file header, from the docs/03 table.
const (
	fhMagic     = 0  // 8 bytes
	fhVersion   = 8  // 2
	fhHeaderLen = 10 // 2
	fhReserved1 = 12 // 4
	fhThreadID  = 16 // 8
	fhCreatedMS = 24 // 8
	fhForumID   = 32 // 8
	fhReserved2 = 40 // 20
	fhCRC       = 60 // 4, over bytes 0..59
)

// FileHeader is the structure at the head of a thread log.
//
// ForumID records where the thread was created and is informational. Moves are
// recorded as thread events in the log, so this field does not follow them and
// must not be read as the thread's current home.
//
// HeaderLength exists so a reader that meets a future version can skip to the
// first frame without understanding the header it skipped. This package always
// writes [FileHeaderSize].
type FileHeader struct {
	Version      uint16
	HeaderLength uint16
	ThreadID     uint64
	CreatedMS    int64
	ForumID      uint64
}

// NewFileHeader builds a header for a new log at the current format version.
func NewFileHeader(threadID uint64, createdMS int64, forumID uint64) FileHeader {
	return FileHeader{
		Version:      FormatVersion,
		HeaderLength: FileHeaderSize,
		ThreadID:     threadID,
		CreatedMS:    createdMS,
		ForumID:      forumID,
	}
}

// AppendTo appends the encoded header to dst and returns the extended slice.
//
// Reserved bytes are written as zero. They are covered by the checksum, so a
// future version that puts them to use computes its CRC over the values it
// wrote and stays readable by [ParseFileHeader].
func (h FileHeader) AppendTo(dst []byte) []byte {
	start := len(dst)
	dst = append(dst, make([]byte, FileHeaderSize)...)
	b := dst[start:]

	copy(b[fhMagic:], Magic[:])
	binary.LittleEndian.PutUint16(b[fhVersion:], h.Version)
	binary.LittleEndian.PutUint16(b[fhHeaderLen:], h.HeaderLength)
	binary.LittleEndian.PutUint64(b[fhThreadID:], h.ThreadID)
	binary.LittleEndian.PutUint64(b[fhCreatedMS:], uint64(h.CreatedMS))
	binary.LittleEndian.PutUint64(b[fhForumID:], h.ForumID)
	binary.LittleEndian.PutUint32(b[fhCRC:], crc32.Checksum(b[:fhCRC], castagnoli))
	return dst
}

// ParseFileHeader decodes the header at the start of b.
//
// It validates what it can prove: the length of the buffer, the magic, and the
// checksum. It does not judge the version, because a caller that meets a newer
// file needs to read the version in order to report it. Call
// [FileHeader.CheckVersion] before interpreting any frame that follows.
//
// Reserved bytes are not inspected. A non-zero value in them is not an error.
func ParseFileHeader(b []byte) (FileHeader, error) {
	if len(b) < FileHeaderSize {
		return FileHeader{}, fmt.Errorf("%w: file header needs %d bytes, got %d",
			ErrShortBuffer, FileHeaderSize, len(b))
	}
	if !bytes.Equal(b[fhMagic:fhMagic+len(Magic)], Magic[:]) {
		return FileHeader{}, fmt.Errorf("%w: file begins with %q", ErrBadMagic, b[fhMagic:fhMagic+len(Magic)])
	}

	stored := binary.LittleEndian.Uint32(b[fhCRC:])
	computed := crc32.Checksum(b[:fhCRC], castagnoli)
	if stored != computed {
		return FileHeader{}, &ChecksumError{Part: "file header", Stored: stored, Computed: computed}
	}

	return FileHeader{
		Version:      binary.LittleEndian.Uint16(b[fhVersion:]),
		HeaderLength: binary.LittleEndian.Uint16(b[fhHeaderLen:]),
		ThreadID:     binary.LittleEndian.Uint64(b[fhThreadID:]),
		CreatedMS:    int64(binary.LittleEndian.Uint64(b[fhCreatedMS:])),
		ForumID:      binary.LittleEndian.Uint64(b[fhForumID:]),
	}, nil
}

// CheckVersion reports whether this build can interpret the log's frames.
func (h FileHeader) CheckVersion() error {
	if h.Version != FormatVersion {
		return fmt.Errorf("%w: log is version %d, this build understands version %d",
			ErrUnsupportedVersion, h.Version, FormatVersion)
	}
	return nil
}
