// Package threadid mints and parses the 64-bit identifiers that name a thread,
// and derives the path a thread's files live at.
//
// Ids are time-sortable: the millisecond sits in the high bits, so listing the
// threads directory is chronological without opening a single file. That is the
// property the whole layout is arranged around, and it is why the sequence goes
// in the low bits rather than the other way about.
package threadid

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ID names one thread for the life of the data directory.
type ID uint64

const (
	// seqBits is the width of the per-process sequence. The remaining 48 bits
	// carry the millisecond, which runs to the year 10889; 16 bits of sequence
	// is 65536 ids in one millisecond, or 65M a second, which no single writer
	// process is going to reach.
	seqBits = 16
	maxSeq  = 1<<seqBits - 1

	// maxMS is the largest representable timestamp.
	maxMS = int64(1)<<(64-seqBits) - 1
)

// ErrTimestampRange means the clock has moved past what 48 bits can hold.
//
// It cannot happen before the year 10889. It is an error rather than a mask
// because masking would wrap the high bits and silently destroy the ordering
// this type exists to provide, and an identifier that quietly stops sorting is
// worse than one that refuses to be minted.
var ErrTimestampRange = errors.New("threadid: timestamp is beyond 48 bits")

// Timestamp returns the millisecond the id was minted at.
func (id ID) Timestamp() time.Time {
	return time.UnixMilli(int64(id) >> seqBits).UTC()
}

// Sequence returns the per-millisecond counter the id carries.
func (id ID) Sequence() uint16 { return uint16(uint64(id) & maxSeq) }

// Generator hands out ids for one process.
//
// The zero value is not usable; call NewGenerator.
type Generator struct {
	mu     sync.Mutex
	now    func() time.Time
	lastMS int64
	seq    uint16
	// primed distinguishes "no id yet" from "an id at time zero", which
	// matters only to a test with an injected clock, but a generator whose
	// first id depends on whether the fake clock reads zero is a generator
	// with a bug waiting for a leap second.
	primed bool
}

// NewGenerator returns a generator reading the system clock.
func NewGenerator() *Generator { return &Generator{now: time.Now} }

// newGeneratorAt returns a generator reading an injected clock, for tests.
func newGeneratorAt(now func() time.Time) *Generator { return &Generator{now: now} }

// New mints the next id.
//
// Ids from one generator are strictly increasing, which callers depend on for
// more than uniqueness: a listing sorted by name is a listing sorted by
// creation, and a thread that appeared later must never sort earlier.
func (g *Generator) New() (ID, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	ms := g.now().UnixMilli()

	if !g.primed || ms > g.lastMS {
		// A fresh millisecond. Start its sequence over.
		g.seq = 0
	} else {
		// Same millisecond, or the clock stepped backwards: NTP, a suspend, a
		// VM migration. Either way hold the last millisecond and keep
		// counting. The id stays monotonic and its timestamp is briefly
		// stale, which is the right way round — a stale timestamp misleads a
		// reader, a non-monotonic id corrupts an ordering that files on disk
		// are already sorted by.
		ms = g.lastMS
		if g.seq == maxSeq {
			var err error
			if ms, err = g.advance(ms); err != nil {
				return 0, err
			}
			g.seq = 0
		} else {
			g.seq++
		}
	}

	if ms < 0 || ms > maxMS {
		return 0, fmt.Errorf("%w: %d", ErrTimestampRange, ms)
	}

	g.lastMS, g.primed = ms, true
	return ID(uint64(ms)<<seqBits | uint64(g.seq)), nil
}

// advance borrows the next millisecond after the sequence is exhausted.
//
// It does not wait for the clock. Waiting would block every caller while this
// one holds the mutex, and it would be a spin whose length is decided by how
// far into the millisecond we happen to be. Stepping forward costs a timestamp
// that runs slightly ahead of the clock until the clock catches up, which the
// backwards-clock path above already handles, and it keeps New wait-free.
//
// Reached only after 65536 ids inside one millisecond.
func (g *Generator) advance(ms int64) (int64, error) {
	if ms >= maxMS {
		return 0, fmt.Errorf("%w: cannot advance past %d", ErrTimestampRange, ms)
	}
	return ms + 1, nil
}

// Crockford base32: no I, L, O or U, so an id read off a screen and typed back
// does not turn into a different id. Thirteen characters carry 65 bits, and the
// leading one therefore holds only four significant bits.
const (
	alphabet   = "0123456789abcdefghjkmnpqrstvwxyz"
	EncodedLen = 13
)

var decodeTable = buildDecodeTable()

func buildDecodeTable() [256]int8 {
	var t [256]int8
	for i := range t {
		t[i] = -1
	}
	for i := 0; i < len(alphabet); i++ {
		c := alphabet[i]
		t[c] = int8(i)
		if c >= 'a' && c <= 'z' {
			t[c-'a'+'A'] = int8(i)
		}
	}
	// Crockford's confusables. A person transcribing an id writes what the
	// glyph looks like, so accept the lookalike and map it to what it must
	// have been.
	t['i'], t['I'], t['l'], t['L'] = 1, 1, 1, 1
	t['o'], t['O'] = 0, 0
	return t
}

// String returns the canonical base32 form, which is what a URL carries.
func (id ID) String() string {
	var b [EncodedLen]byte
	v := uint64(id)
	for i := EncodedLen - 1; i >= 0; i-- {
		b[i] = alphabet[v&31]
		v >>= 5
	}
	return string(b[:])
}

// ErrMalformed means a string is not an id.
var ErrMalformed = errors.New("threadid: malformed id")

// Parse reads the base32 form.
//
// Everything that is not exactly one id is refused rather than coerced: the
// wrong length, a character outside the alphabet, and a leading character
// carrying more than four significant bits, which would overflow 64 bits and
// silently alias a different thread.
func Parse(s string) (ID, error) {
	if len(s) != EncodedLen {
		return 0, fmt.Errorf("%w: got %d characters, want %d", ErrMalformed, len(s), EncodedLen)
	}
	var v uint64
	for i := 0; i < len(s); i++ {
		d := decodeTable[s[i]]
		if d < 0 {
			return 0, fmt.Errorf("%w: %q is not in the alphabet", ErrMalformed, s[i])
		}
		if i == 0 && d > 15 {
			return 0, fmt.Errorf("%w: leading %q overflows 64 bits", ErrMalformed, s[i])
		}
		v = v<<5 | uint64(d)
	}
	return ID(v), nil
}

// Hex returns the 16-character lowercase form used for filenames. The shard is
// taken from the first two bytes of this, so it is zero-padded and fixed width.
func (id ID) Hex() string { return fmt.Sprintf("%016x", uint64(id)) }

// ParseHex reads the filename form, for tools walking the threads directory.
func ParseHex(s string) (ID, error) {
	if len(s) != 16 {
		return 0, fmt.Errorf("%w: got %d hex characters, want 16", ErrMalformed, len(s))
	}
	var v uint64
	for i := 0; i < len(s); i++ {
		c := s[i]
		var d uint64
		switch {
		case c >= '0' && c <= '9':
			d = uint64(c - '0')
		case c >= 'a' && c <= 'f':
			d = uint64(c-'a') + 10
		default:
			// Uppercase is refused on purpose. A filename is compared byte for
			// byte on disk, so admitting two spellings of one id would let a
			// thread exist at two paths on a case-sensitive filesystem.
			return 0, fmt.Errorf("%w: %q is not lowercase hex", ErrMalformed, c)
		}
		v = v<<4 | d
	}
	return ID(v), nil
}

// MarshalText and UnmarshalText make an ID usable in JSON, which is where the
// API will meet it.
func (id ID) MarshalText() ([]byte, error) { return []byte(id.String()), nil }

func (id *ID) UnmarshalText(b []byte) error {
	v, err := Parse(strings.TrimSpace(string(b)))
	if err != nil {
		return err
	}
	*id = v
	return nil
}
