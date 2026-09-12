// Package threadlog implements the on-disk format for a thread log: the file
// header that opens it, the record frames that fill it, and the CRC32C that
// guards both.
//
// A thread log is append-only and is the source of truth for post content.
// The sidecar index beside it and every database row describing the thread are
// derived, and both can be rebuilt from the log alone. See
// docs/03-thread-storage.md for the format tables and the reasoning.
//
// All multi-byte integers are little-endian.
//
// # Zero copy
//
// Parsing does not copy. A body returned by [ParseFrame] aliases the caller's
// buffer, which is what lets the read path coalesce several consecutive
// records into one read and slice the result rather than allocating per post.
// A caller that keeps a body alive past the buffer must copy it first.
//
// # Trusting the length field
//
// A frame's length is read from disk, and the parser sizes work from it. Every
// entry point therefore bounds it against [MaxFrameSize] before anything is
// allocated or sliced, so a corrupt length is refused rather than believed. No
// exported function in this package panics on malformed input.
package threadlog

import (
	"errors"
	"fmt"
	"hash/crc32"
)

// FormatVersion is the on-disk format version this package writes. A change to
// the layout is a version bump with a migration path, never an edit in place.
const FormatVersion uint16 = 1

// Magic is the signature at the head of every thread log file.
var Magic = [8]byte{'N', 'E', 'O', 'F', 'T', 'H', 'R', 'D'}

// castagnoli is CRC32C, the checksum used throughout the format.
var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// Errors returned by this package. Every error matches one of these through
// [errors.Is], so callers branch on the sentinel and read the message for
// detail rather than matching on strings.
var (
	// ErrShortBuffer means the buffer is smaller than the structure it was
	// asked to hold. It is the expected result of reading a partial frame at
	// the end of a coalesced read, not necessarily damage.
	ErrShortBuffer = errors.New("threadlog: buffer too short")

	// ErrBadMagic means the file does not begin with [Magic].
	ErrBadMagic = errors.New("threadlog: not a thread log")

	// ErrChecksum means a CRC32C did not match the bytes it covers. Errors
	// carrying this also satisfy a type assertion to [*ChecksumError].
	ErrChecksum = errors.New("threadlog: checksum mismatch")

	// ErrMalformed means a field is self-inconsistent, such as a frame length
	// below the fixed part or beyond [MaxFrameSize].
	ErrMalformed = errors.New("threadlog: malformed")

	// ErrUnsupportedVersion means the file was written by a different format
	// version than this build understands.
	ErrUnsupportedVersion = errors.New("threadlog: unsupported format version")
)

// ChecksumError reports a CRC32C mismatch and what the damaged structure
// claimed about itself. Ordinal is meaningful only when Part is "frame", and
// it comes from bytes that failed their own checksum, so treat it as a hint
// for a log line rather than as fact.
type ChecksumError struct {
	Part     string // "file header" or "frame"
	Ordinal  uint32
	Stored   uint32
	Computed uint32
}

func (e *ChecksumError) Error() string {
	if e.Part == "frame" {
		return fmt.Sprintf("threadlog: checksum mismatch in frame claiming ordinal %d: stored %#08x, computed %#08x",
			e.Ordinal, e.Stored, e.Computed)
	}
	return fmt.Sprintf("threadlog: checksum mismatch in %s: stored %#08x, computed %#08x",
		e.Part, e.Stored, e.Computed)
}

// Is reports that a ChecksumError matches [ErrChecksum].
func (e *ChecksumError) Is(target error) bool { return target == ErrChecksum }
