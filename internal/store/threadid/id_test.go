package threadid

import (
	"errors"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"
)

// frozen returns a clock that does not advance until it is told to.
func frozen(ms int64) (func() time.Time, func(int64)) {
	var mu sync.Mutex
	cur := ms
	return func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return time.UnixMilli(cur)
		}, func(to int64) {
			mu.Lock()
			defer mu.Unlock()
			cur = to
		}
}

// Criterion: ids generated in the same millisecond are distinct and monotonic
// within a process.
//
// The clock is frozen rather than sampled, because minting a thousand ids and
// hoping they land in one millisecond tests the scheduler, not the generator.
func TestIdsInOneMillisecondAreDistinctAndMonotonic(t *testing.T) {
	now, _ := frozen(1_700_000_000_000)
	g := NewGeneratorAt(now)

	const n = 10_000
	seen := make(map[ID]bool, n)
	var prev ID
	for i := range n {
		id, err := g.New()
		if err != nil {
			t.Fatalf("id %d: %v", i, err)
		}
		if seen[id] {
			t.Fatalf("id %d (%s) was handed out twice", i, id)
		}
		seen[id] = true
		if i > 0 && id <= prev {
			t.Fatalf("id %d (%s) does not follow %s", i, id, prev)
		}
		prev = id
	}

	// All in one millisecond means the timestamp is shared and the sequence
	// did the work.
	first := prev.Timestamp()
	if got := time.UnixMilli(1_700_000_000_000).UTC(); !first.Equal(got) {
		t.Errorf("timestamp drifted to %v, want %v", first, got)
	}
}

// Concurrent minting must give the same guarantee. The generator is the one
// piece of this package with state, so it is the one piece that can race.
func TestConcurrentGenerationIsUniqueAndOrdered(t *testing.T) {
	g := NewGenerator()

	const (
		workers = 16
		each    = 500
	)
	out := make([][]ID, workers)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids := make([]ID, each)
			for i := range each {
				id, err := g.New()
				if err != nil {
					t.Errorf("worker %d: %v", w, err)
					return
				}
				ids[i] = id
			}
			out[w] = ids
		}()
	}
	wg.Wait()

	seen := make(map[ID]bool, workers*each)
	for w, ids := range out {
		// Within one worker the ids must still be increasing: the generator
		// hands them out under a lock, so a worker can never observe its own
		// second id before its first.
		for i, id := range ids {
			if seen[id] {
				t.Fatalf("worker %d id %d (%s) collided", w, i, id)
			}
			seen[id] = true
			if i > 0 && id <= ids[i-1] {
				t.Fatalf("worker %d: %s does not follow %s", w, id, ids[i-1])
			}
		}
	}
	if len(seen) != workers*each {
		t.Errorf("got %d distinct ids, want %d", len(seen), workers*each)
	}
}

// 65536 ids inside one millisecond exhausts the sequence. The generator must
// move to the next millisecond rather than wrap and repeat itself.
func TestSequenceExhaustionMovesToTheNextMillisecond(t *testing.T) {
	now, _ := frozen(1_700_000_000_000)
	g := NewGeneratorAt(now)

	var prev ID
	for i := 0; i <= maxSeq+2; i++ {
		id, err := g.New()
		if err != nil {
			t.Fatalf("id %d: %v", i, err)
		}
		if i > 0 && id <= prev {
			t.Fatalf("id %d (%s) does not follow %s", i, id, prev)
		}
		prev = id
	}

	// The clock never moved, so the generator had to step the millisecond on
	// its own to keep going.
	if got, want := prev.Timestamp(), time.UnixMilli(1_700_000_000_001).UTC(); !got.Equal(want) {
		t.Errorf("after exhausting the sequence the timestamp is %v, want %v", got, want)
	}
	if prev.Sequence() > 2 {
		t.Errorf("sequence is %d, want it reset near zero for the new millisecond", prev.Sequence())
	}
}

// A clock that steps backwards must not produce an id that sorts before one
// already handed out. NTP, a suspend and a VM migration all do this, and the
// files on disk are sorted by these ids.
func TestClockGoingBackwardsStillYieldsIncreasingIds(t *testing.T) {
	now, set := frozen(1_700_000_000_000)
	g := NewGeneratorAt(now)

	before, err := g.New()
	if err != nil {
		t.Fatal(err)
	}

	set(1_699_999_990_000) // ten seconds into the past
	after, err := g.New()
	if err != nil {
		t.Fatal(err)
	}

	if after <= before {
		t.Fatalf("after the clock went back, %s does not follow %s", after, before)
	}
	if got, want := after.Timestamp(), before.Timestamp(); !got.Equal(want) {
		t.Errorf("timestamp moved to %v; it should hold at %v until the clock catches up", got, want)
	}
}

// Criterion: the base32 form round-trips to the same 64-bit id.
func TestBase32RoundTrips(t *testing.T) {
	cases := []ID{0, 1, 31, 32, 0xffff, 1 << 63, ^ID(0), 0x3f7a9c21e0000001}
	r := rand.New(rand.NewSource(1))
	for range 2000 {
		cases = append(cases, ID(r.Uint64()))
	}

	for _, id := range cases {
		s := id.String()
		if len(s) != EncodedLen {
			t.Fatalf("%016x encoded to %d characters, want %d", uint64(id), len(s), EncodedLen)
		}
		back, err := Parse(s)
		if err != nil {
			t.Fatalf("parsing %q (from %016x): %v", s, uint64(id), err)
		}
		if back != id {
			t.Fatalf("%016x round-tripped to %016x via %q", uint64(id), uint64(back), s)
		}
	}
}

// Criterion: an invalid form is rejected rather than silently coerced.
//
// Coercion is the failure that matters. A parser that trims, pads or masks
// turns one person's typo into a valid id for somebody else's thread.
func TestParseRejectsRatherThanCoerces(t *testing.T) {
	full := ^ID(0)
	bad := []struct {
		in, why string
	}{
		{"", "empty"},
		{"0", "far too short"},
		{strings.Repeat("0", EncodedLen-1), "one short"},
		{strings.Repeat("0", EncodedLen+1), "one long"},
		{"0000000000 00", "space inside"},
		{" 0000000000000", "leading space"},
		{"0000000000000 ", "trailing space"},
		{"000000000000u", "u is not in the Crockford alphabet"},
		{"000000000000!", "punctuation"},
		{"0000-00000-000", "hyphenated"},
		{"z000000000000", "leading character overflows 64 bits"},
		{"g000000000000", "leading character overflows 64 bits by one"},
	}
	for _, c := range bad {
		if got, err := Parse(c.in); err == nil {
			t.Errorf("Parse(%q) = %016x, want an error (%s)", c.in, uint64(got), c.why)
		} else if !errors.Is(err, ErrMalformed) {
			t.Errorf("Parse(%q) gave %v, want ErrMalformed", c.in, err)
		}
	}

	// The boundary: 'f' is 15 and fits, 'g' is 16 and does not. The largest id
	// must survive the round trip its encoding implies.
	if s := full.String(); s[0] != 'f' {
		t.Errorf("the all-ones id encodes with leading %q, want 'f'", s[0])
	}
	if got, err := Parse(full.String()); err != nil || got != full {
		t.Errorf("Parse(%q) = %016x, %v; want the all-ones id", full.String(), uint64(got), err)
	}
}

// Crockford exists so a person can read an id off a screen and type it back.
// The lookalikes must land on the character they look like, and case must not
// matter, but neither may change which thread you get.
func TestParseAcceptsCrockfordConfusables(t *testing.T) {
	canonical := "1a2b3c4d5e6f7"
	want, err := Parse(canonical)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{
		"1A2B3C4D5E6F7", // shouted
		"la2b3c4d5e6f7", // lowercase L for one
		"La2b3c4d5e6f7", // uppercase L for one
		"Ia2b3c4d5e6f7", // uppercase i for one
		"ia2b3c4d5e6f7", // lowercase i for one
	} {
		got, err := Parse(v)
		if err != nil {
			t.Errorf("Parse(%q): %v", v, err)
			continue
		}
		if got != want {
			t.Errorf("Parse(%q) = %016x, want %016x", v, uint64(got), uint64(want))
		}
	}

	// O maps to zero, and the canonical form never spells it that way.
	o, err := Parse("O" + canonical[1:])
	if err != nil {
		t.Fatal(err)
	}
	if o.String()[0] != '0' {
		t.Errorf("O did not normalise to 0: %s", o)
	}
}

// The hex form names a file. Two spellings of one id would let a thread exist
// at two paths at once on a case-sensitive filesystem, so only one is legal.
func TestParseHexIsStrictAboutCase(t *testing.T) {
	id := ID(0x3f7a9c21e0000001)
	if got := id.Hex(); got != "3f7a9c21e0000001" {
		t.Fatalf("Hex() = %q", got)
	}
	back, err := ParseHex(id.Hex())
	if err != nil || back != id {
		t.Fatalf("ParseHex(%q) = %016x, %v", id.Hex(), uint64(back), err)
	}
	for _, bad := range []string{"3F7A9C21E0000001", "3f7a9c21e000001", "3f7a9c21e00000011", "3f7a9c21e000000g"} {
		if _, err := ParseHex(bad); !errors.Is(err, ErrMalformed) {
			t.Errorf("ParseHex(%q) was accepted, want ErrMalformed", bad)
		}
	}
}

// Ids are meant to sort chronologically as text, which is what makes a
// directory listing a timeline. That only holds if the encoding is fixed width
// and the alphabet is in ascending order, so it is worth asserting rather than
// assuming.
func TestEncodedFormSortsInTheSameOrderAsTheId(t *testing.T) {
	now, set := frozen(1_700_000_000_000)
	g := NewGeneratorAt(now)

	var ids []ID
	var strs []string
	for i := range 500 {
		if i%7 == 0 {
			set(1_700_000_000_000 + int64(i))
		}
		id, err := g.New()
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		strs = append(strs, id.String())
	}
	for i := 1; i < len(ids); i++ {
		if (ids[i] > ids[i-1]) != (strs[i] > strs[i-1]) {
			t.Fatalf("numeric and textual order disagree at %d: %s vs %s", i, strs[i-1], strs[i])
		}
	}
}

func TestTextMarshalling(t *testing.T) {
	id := ID(0x3f7a9c21e0000001)
	b, err := id.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	var back ID
	if err := back.UnmarshalText(b); err != nil {
		t.Fatal(err)
	}
	if back != id {
		t.Errorf("round trip gave %016x, want %016x", uint64(back), uint64(id))
	}
	if err := back.UnmarshalText([]byte("nonsense")); !errors.Is(err, ErrMalformed) {
		t.Errorf("UnmarshalText took nonsense: %v", err)
	}
}

// Parse reads a URL path segment, which is the least trusted input in the
// system. It must refuse everything it does not understand without panicking.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"", "0", "0000000000000", "fzzzzzzzzzzzz", "1a2b3c4d5e6f7",
		"ZZZZZZZZZZZZZ", "0000-00000-000", "000000000000u",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		id, err := Parse(s)
		if err != nil {
			return
		}
		// Anything accepted must re-encode to something that parses back to
		// the same id. A parser that accepts a form it cannot reproduce is one
		// that has coerced its input.
		again, err := Parse(id.String())
		if err != nil {
			t.Fatalf("Parse accepted %q as %016x, but its own encoding %q does not parse: %v",
				s, uint64(id), id.String(), err)
		}
		if again != id {
			t.Fatalf("Parse(%q) = %016x, re-encoded to %q which parses as %016x",
				s, uint64(id), id.String(), uint64(again))
		}
	})
}
