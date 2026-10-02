package threadlog

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

// The tests in this file are about one bug class (TKT-01M3VVZE4G): an
// ordinal is a uint32 and a post count is an int, which is 32 bits on 386,
// arm, mips and mipsle. There int(4294967295) is -1, so `int(o) > n` passes
// a bound it should fail. CI runs this package under GOARCH=386 as well as
// natively, and on amd64 each of these passes whether or not the bound is
// written correctly, so the 386 run is the one that proves anything.

// lastOrdinal is the largest ordinal a frame can name.
const lastOrdinal = 1<<32 - 1

// threePosts is an index of three plausible posts.
func threePosts() *Index {
	entries := make([]IndexEntry, 3)
	for i := range entries {
		entries[i] = IndexEntry{
			Offset: uint64(FileHeaderSize + i*64), FrameLength: 64,
			Author: 1, CreatedMS: int64(1_700_000_000_000) + int64(i), Revision: 1,
		}
	}
	return NewIndex(IndexHeader{ThreadID: 1, LogLength: uint64(FileHeaderSize + 3*64)}, entries)
}

// TestEntryPastTheEndIsAnErrorNotAPanic: Entry past the last post returns
// ErrNoSuchOrdinal, at the largest ordinal as well as at one past the end.
// Under 386 the largest one used to index the slice at 4294967294.
func TestEntryPastTheEndIsAnErrorNotAPanic(t *testing.T) {
	ix := threePosts()
	for _, o := range []uint32{0, 4, lastOrdinal, 1 << 31} {
		if _, err := ix.Entry(o); !errors.Is(err, ErrNoSuchOrdinal) {
			t.Errorf("Entry(%d) on three posts: %v, want ErrNoSuchOrdinal", o, err)
		}
	}
	if _, err := ix.Entry(3); err != nil {
		t.Errorf("Entry(3) on three posts: %v", err)
	}
}

// TestEntriesPastTheEndAreClamped: a range that runs past the last post is
// clamped to it, and one that starts past it is empty.
func TestEntriesPastTheEndAreClamped(t *testing.T) {
	ix := threePosts()
	for _, c := range []struct {
		from, to uint32
		want     int
	}{
		{2, lastOrdinal, 2},
		{1, 1 << 31, 3},
		{0, lastOrdinal, 3},
		{lastOrdinal, lastOrdinal, 0},
		{1 << 31, lastOrdinal, 0},
		{4, 4, 0},
		{3, 2, 0},
	} {
		got := ix.Entries(c.from, c.to)
		if len(got) != c.want {
			t.Errorf("Entries(%d, %d) on three posts: %d entries, want %d", c.from, c.to, len(got), c.want)
		}
	}
	if got := ix.Entries(2, lastOrdinal); len(got) == 2 && got[0].Offset != uint64(FileHeaderSize+64) {
		t.Errorf("Entries(2, %d) starts at offset %d, want post 2's", uint32(lastOrdinal), got[0].Offset)
	}
}

// TestReplayRefusesAnOrdinalPastTheEnd: a log whose Delete names the largest
// ordinal is malformed, and replaying it is an error rather than a panic.
func TestReplayRefusesAnOrdinalPastTheEnd(t *testing.T) {
	lg := buildLog(t)
	raw := lg.bytes
	for _, typ := range []RecordType{TypeDelete, TypeEdit} {
		var v any = &DeleteBody{Reason: "spam"}
		if typ == TypeEdit {
			v = &EditBody{Source: "rewritten"}
		}
		body, err := EncodeBody(EncodingCBOR, v)
		if err != nil {
			t.Fatalf("EncodeBody: %v", err)
		}
		bad, err := AppendFrame(append([]byte(nil), raw...), FrameHeader{
			Type: typ, Encoding: EncodingCBOR, Ordinal: lastOrdinal, Revision: 2,
			TimestampMS: 1_700_000_000_500, Actor: 9,
		}, body)
		if err != nil {
			t.Fatalf("AppendFrame: %v", err)
		}
		_, err = Open(bytes.NewReader(bad), int64(len(bad)), nil)
		if !errors.Is(err, ErrMalformed) {
			t.Errorf("type %d naming ordinal %d: Open = %v, want ErrMalformed", typ, uint32(lastOrdinal), err)
		}
	}
}

// TestPastLast is the helper every bound check calls, on both sides of
// 2^31 and at the largest count an int holds on the build running it.
func TestPastLast(t *testing.T) {
	for _, c := range []struct {
		o    uint32
		n    int
		past bool
	}{
		{0, 0, false},
		{1, 0, true},
		{3, 3, false},
		{4, 3, true},
		{lastOrdinal, 3, true},
		{1 << 31, 3, true},
		{1<<31 - 1, 1<<31 - 1, false},
		{1 << 31, 1<<31 - 1, true},
		{lastOrdinal, math.MaxInt, uint64(lastOrdinal) > uint64(math.MaxInt)},
		{1, -1, true},
		{0, -1, true},
	} {
		if got := PastLast(c.o, c.n); got != c.past {
			t.Errorf("PastLast(%d, %d) = %v, want %v", c.o, c.n, got, c.past)
		}
	}
}
