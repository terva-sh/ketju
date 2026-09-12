package threadlog

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"math"
	"testing"
)

func TestFileHeaderRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		h    FileHeader
	}{
		{"zero", FileHeader{}},
		{"typical", NewFileHeader(0x3f7a9c21e0000001, 1757500000000, 42)},
		{"max", FileHeader{
			Version:      math.MaxUint16,
			HeaderLength: math.MaxUint16,
			ThreadID:     math.MaxUint64,
			CreatedMS:    math.MaxInt64,
			ForumID:      math.MaxUint64,
		}},
		{"negative timestamp", FileHeader{
			Version:      FormatVersion,
			HeaderLength: FileHeaderSize,
			CreatedMS:    math.MinInt64,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := tc.h.AppendTo(nil)
			if len(buf) != FileHeaderSize {
				t.Fatalf("encoded %d bytes, want %d", len(buf), FileHeaderSize)
			}
			got, err := ParseFileHeader(buf)
			if err != nil {
				t.Fatalf("ParseFileHeader: %v", err)
			}
			if got != tc.h {
				t.Errorf("round trip changed the header\n got %+v\nwant %+v", got, tc.h)
			}
		})
	}
}

func TestFileHeaderAppendToPreservesPrefix(t *testing.T) {
	prefix := []byte("keep me")
	buf := NewFileHeader(7, 8, 9).AppendTo(prefix)

	if string(buf[:len(prefix)]) != "keep me" {
		t.Fatalf("prefix was overwritten: %q", buf[:len(prefix)])
	}
	if _, err := ParseFileHeader(buf[len(prefix):]); err != nil {
		t.Fatalf("header after a prefix did not parse: %v", err)
	}
}

// The reserved bytes are the format's room to grow. A writer leaves them zero,
// and a reader must not care what it finds there, or the first version to use
// them breaks every older binary.
func TestFileHeaderReservedBytesAreWrittenZero(t *testing.T) {
	buf := FileHeader{
		Version:      math.MaxUint16,
		HeaderLength: math.MaxUint16,
		ThreadID:     math.MaxUint64,
		CreatedMS:    -1,
		ForumID:      math.MaxUint64,
	}.AppendTo(nil)

	for _, r := range []struct {
		name   string
		lo, hi int
	}{
		{"reserved at 12", fhReserved1, fhReserved1 + 4},
		{"reserved at 40", fhReserved2, fhReserved2 + 20},
	} {
		for i := r.lo; i < r.hi; i++ {
			if buf[i] != 0 {
				t.Errorf("%s: byte %d is %#02x, want zero", r.name, i, buf[i])
			}
		}
	}
}

func TestFileHeaderToleratesNonZeroReserved(t *testing.T) {
	want := NewFileHeader(0x1122334455667788, 1757500000000, 99)
	buf := want.AppendTo(nil)

	// Stand in for a future version that put the reserved bytes to use and
	// checksummed what it wrote.
	for i := fhReserved1; i < fhReserved1+4; i++ {
		buf[i] = 0xFF
	}
	for i := fhReserved2; i < fhReserved2+20; i++ {
		buf[i] = byte(i)
	}
	binary.LittleEndian.PutUint32(buf[fhCRC:], crc32.Checksum(buf[:fhCRC], castagnoli))

	got, err := ParseFileHeader(buf)
	if err != nil {
		t.Fatalf("non-zero reserved bytes were rejected: %v", err)
	}
	if got != want {
		t.Errorf("reserved bytes leaked into the parsed header\n got %+v\nwant %+v", got, want)
	}
}

func TestFileHeaderRejectsShortBuffer(t *testing.T) {
	full := NewFileHeader(1, 2, 3).AppendTo(nil)
	for n := 0; n < FileHeaderSize; n++ {
		_, err := ParseFileHeader(full[:n])
		if !errors.Is(err, ErrShortBuffer) {
			t.Fatalf("with %d bytes: got %v, want ErrShortBuffer", n, err)
		}
	}
}

func TestFileHeaderRejectsBadMagic(t *testing.T) {
	buf := NewFileHeader(1, 2, 3).AppendTo(nil)
	copy(buf, "NOTALOG!")
	binary.LittleEndian.PutUint32(buf[fhCRC:], crc32.Checksum(buf[:fhCRC], castagnoli))

	// Checksum is valid, so only the magic can reject this.
	if _, err := ParseFileHeader(buf); !errors.Is(err, ErrBadMagic) {
		t.Fatalf("got %v, want ErrBadMagic", err)
	}
}

func TestFileHeaderDetectsCorruption(t *testing.T) {
	for i := 0; i < FileHeaderSize; i++ {
		buf := NewFileHeader(0x3f7a9c21e0000001, 1757500000000, 42).AppendTo(nil)
		buf[i] ^= 0xFF

		_, err := ParseFileHeader(buf)
		if err == nil {
			t.Fatalf("corrupting byte %d was not detected", i)
		}
		want := ErrChecksum
		if i < len(Magic) {
			want = ErrBadMagic
		}
		if !errors.Is(err, want) {
			t.Errorf("corrupting byte %d: got %v, want %v", i, err, want)
		}
	}
}

func TestFileHeaderCheckVersion(t *testing.T) {
	if err := NewFileHeader(1, 2, 3).CheckVersion(); err != nil {
		t.Fatalf("current version rejected: %v", err)
	}

	future := NewFileHeader(1, 2, 3)
	future.Version = FormatVersion + 1
	err := future.CheckVersion()
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("got %v, want ErrUnsupportedVersion", err)
	}

	// A future file must still parse, so the version can be reported rather
	// than guessed at.
	if _, err := ParseFileHeader(future.AppendTo(nil)); err != nil {
		t.Fatalf("a future version failed to parse: %v", err)
	}
}
