package threadlog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func scanAll(t *testing.T, b []byte) ([]ScanRecord, error) {
	t.Helper()
	var out []ScanRecord
	err := ScanFrames(bytes.NewReader(b), int64(len(b)), func(r ScanRecord) bool {
		out = append(out, r)
		return true
	})
	return out, err
}

// The scanner is what the offline tools stand on, so it has to agree with the
// index about where every record is — that correspondence is the only way a
// dump line can be matched to an entry.
func TestScanFindsEveryFrameWhereTheIndexSaysItIs(t *testing.T) {
	lg := buildLog(t)
	got, err := scanAll(t, lg.bytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(lg.frameEnds) {
		t.Fatalf("scanned %d frames, the log has %d", len(got), len(lg.frameEnds))
	}

	start := int64(FileHeaderSize)
	for i, r := range got {
		if r.Err != nil {
			t.Errorf("frame %d: %v", i, r.Err)
		}
		if r.Offset != start {
			t.Errorf("frame %d is at %d, want %d", i, r.Offset, start)
		}
		if got, want := r.Offset+int64(r.Size), lg.frameEnds[i]; got != want {
			t.Errorf("frame %d ends at %d, want %d", i, got, want)
		}
		start = lg.frameEnds[i]
	}
}

// A checksum failure is not fatal: the length was sound, so the next frame is
// findable and the walk continues. This is what lets one bad record cost a
// reader one record rather than the whole thread.
func TestScanContinuesPastAChecksumFailure(t *testing.T) {
	lg := buildLog(t)
	clean, err := scanAll(t, lg.bytes)
	if err != nil {
		t.Fatal(err)
	}

	damaged := bytes.Clone(lg.bytes)
	damaged[clean[2].Offset+FrameFixedSize+1] ^= 0xff

	got, err := scanAll(t, damaged)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(clean) {
		t.Fatalf("scanned %d frames after damage, want %d", len(got), len(clean))
	}
	if got[2].Err == nil {
		t.Error("the damaged frame was reported as sound")
	}
	if !errors.Is(got[2].Err, ErrChecksum) {
		t.Errorf("error is %v, want ErrChecksum", got[2].Err)
	}
	if got[2].Fatal {
		t.Error("a checksum failure was marked fatal")
	}
	// The header is still reported, because "ordinal 412 fails CRC" is worth
	// more to whoever is holding the broken thread than "a frame fails CRC".
	if got[2].Header.Type != clean[2].Header.Type {
		t.Errorf("damaged frame reports type %v, want %v", got[2].Header.Type, clean[2].Header.Type)
	}
	for i := 3; i < len(got); i++ {
		if got[i].Err != nil {
			t.Errorf("frame %d after the damage: %v", i, got[i].Err)
		}
	}
}

// A length that cannot be trusted ends the walk, because the next frame's
// offset is derived from it and guessing would produce fiction.
func TestScanStopsWhenTheNextFrameCannotBeFound(t *testing.T) {
	lg := buildLog(t)
	clean, _ := scanAll(t, lg.bytes)

	for _, tc := range []struct {
		name string
		len  uint32
	}{
		{"below the minimum", 3},
		{"past the maximum", MaxFrameSize + 1},
		{"past the end of the file", 0x00ff_0000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			damaged := bytes.Clone(lg.bytes)
			binary.LittleEndian.PutUint32(damaged[clean[2].Offset:], tc.len)

			got, err := scanAll(t, damaged)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 3 {
				t.Fatalf("scanned %d frames, want 3 (two good and the fatal one)", len(got))
			}
			if !got[2].Fatal {
				t.Errorf("frame with length %d is not marked fatal", tc.len)
			}
			if got[2].Err == nil {
				t.Error("no error reported")
			}
		})
	}
}

func TestScanRefusesALogWithoutAFileHeader(t *testing.T) {
	lg := buildLog(t)
	for _, n := range []int{0, 1, FileHeaderSize - 1} {
		if err := ScanFrames(bytes.NewReader(lg.bytes[:n]), int64(n), func(ScanRecord) bool {
			t.Fatalf("%d bytes: a record was reported from a log with no header", n)
			return false
		}); !errors.Is(err, ErrLogTooShort) {
			t.Errorf("%d bytes: %v, want ErrLogTooShort", n, err)
		}
	}
}

// A log holding nothing but its header is legal and scans to nothing.
func TestScanOfAnEmptyLogYieldsNothing(t *testing.T) {
	hdr := NewFileHeader(1, 1, 1).AppendTo(nil)
	got, err := scanAll(t, hdr)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("scanned %d frames from a log with no records", len(got))
	}
}

// Returning false stops the walk, which is what a tool with a --limit needs.
func TestScanStopsWhenTheCallbackSaysSo(t *testing.T) {
	lg := buildLog(t)
	n := 0
	if err := ScanFrames(bytes.NewReader(lg.bytes), int64(len(lg.bytes)), func(ScanRecord) bool {
		n++
		return n < 3
	}); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("callback ran %d times after asking to stop at 3", n)
	}
}
