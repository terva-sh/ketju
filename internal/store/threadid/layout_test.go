package threadid

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Criterion: the path for an id is derived by a pure function, with no
// directory scan.
//
// Proven by deriving paths under a root that does not exist. Anything that
// looked at the filesystem would have to fail or return something different;
// a pure function neither knows nor cares.
func TestPathIsDerivedWithoutTouchingTheFilesystem(t *testing.T) {
	id := ID(0x3f7a9c21e0000001)
	missing := filepath.Join(t.TempDir(), "no", "such", "root")

	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the test's premise is wrong: %s exists", missing)
	}

	want := filepath.Join(missing, "threads", "3f", "7a")
	if got := id.Dir(missing); got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
	if got, want := id.LogPath(missing), filepath.Join(want, "3f7a9c21e0000001.log"); got != want {
		t.Errorf("LogPath = %q, want %q", got, want)
	}
	if got, want := id.IndexPath(missing), filepath.Join(want, "3f7a9c21e0000001.idx"); got != want {
		t.Errorf("IndexPath = %q, want %q", got, want)
	}

	// Still nothing on disk: derivation must not create as a side effect.
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("deriving a path created %s", missing)
	}
}

// The layout in docs/03 is the contract. If this test has to change, the
// document has to change with it.
func TestLayoutMatchesTheDocumentedTree(t *testing.T) {
	id, err := ParseHex("3f7a9c21e0000001")
	if err != nil {
		t.Fatal(err)
	}
	got := filepath.ToSlash(id.LogPath("data"))
	if want := "data/threads/3f/7a/3f7a9c21e0000001.log"; got != want {
		t.Errorf("LogPath = %q, want %q (docs/03)", got, want)
	}
}

// Every id must shard somewhere, including the edges, and the shard must
// always be two levels of exactly two hex characters.
func TestEveryIdShardsIntoTwoByteDirectories(t *testing.T) {
	for _, id := range []ID{0, 1, 255, 256, 1 << 63, ^ID(0), 0x00ff000000000000} {
		dir := filepath.ToSlash(id.Dir("r"))
		parts := strings.Split(dir, "/")
		if len(parts) != 4 || parts[0] != "r" || parts[1] != ThreadsDir {
			t.Fatalf("id %016x: Dir = %q, want r/threads/<b0>/<b1>", uint64(id), dir)
		}
		h := id.Hex()
		if parts[2] != h[0:2] || parts[3] != h[2:4] {
			t.Errorf("id %016x: shard %s/%s, want %s/%s", uint64(id), parts[2], parts[3], h[0:2], h[2:4])
		}
		for _, p := range parts[2:] {
			if len(p) != 2 {
				t.Errorf("id %016x: shard component %q is not two characters", uint64(id), p)
			}
		}
	}
}

func TestFromLogPathRoundTrips(t *testing.T) {
	id := ID(0x3f7a9c21e0000001)
	for _, p := range []string{id.LogPath("data"), id.IndexPath("data"), id.LogPath("/var/lib/ketju")} {
		got, err := FromLogPath(p)
		if err != nil {
			t.Errorf("FromLogPath(%q): %v", p, err)
			continue
		}
		if got != id {
			t.Errorf("FromLogPath(%q) = %016x, want %016x", p, uint64(got), uint64(id))
		}
	}
}

// A file in the wrong shard is a real fault: something moved it, or wrote it by
// hand. Reporting it is the job of the tools that will call this, so it must
// not be quietly accepted.
func TestFromLogPathRejectsAMisplacedFile(t *testing.T) {
	bad := []struct {
		path, why string
	}{
		{"data/threads/00/7a/3f7a9c21e0000001.log", "wrong first shard"},
		{"data/threads/3f/00/3f7a9c21e0000001.log", "wrong second shard"},
		{"data/threads/3f7a9c21e0000001.log", "not sharded at all"},
		{"data/threads/3f/7a/3f7a9c21e0000001.txt", "not a thread file"},
		{"data/threads/3f/7a/3f7a9c21e0000001", "no extension"},
		{"data/threads/3f/7a/notanid.log", "not an id"},
		{"data/threads/3F/7A/3F7A9C21E0000001.log", "uppercase"},
	}
	for _, c := range bad {
		if got, err := FromLogPath(c.path); err == nil {
			t.Errorf("FromLogPath(%q) = %016x, want an error (%s)", c.path, uint64(got), c.why)
		} else if !errors.Is(err, ErrMalformed) {
			t.Errorf("FromLogPath(%q) gave %v, want ErrMalformed", c.path, err)
		}
	}
}

// The shard exists so a directory does not grow without bound. Ids carry the
// millisecond in their high bits, so the rate the second shard byte turns over
// at is what actually decides how full a directory gets. layout.go claims
// about 50 days; this measures it rather than trusting the comment.
//
// Measured between two consecutive turnovers, not from an arbitrary start: the
// distance from a random instant to the next boundary is uniformly anything up
// to a full period, and asserting on that would be asserting on where the test
// happened to begin.
func TestSecondShardByteTurnsOverAboutEveryFiftyDays(t *testing.T) {
	const day = int64(24 * 60 * 60 * 1000)
	base := int64(1_700_000_000_000)

	shard := func(ms int64) string { return ID(uint64(ms) << seqBits).Hex()[2:4] }

	turnoverAfter := func(from int64) int64 {
		s := shard(from)
		for d := int64(1); d <= 400; d++ {
			if shard(from+d*day) != s {
				return d
			}
		}
		return 0
	}

	first := turnoverAfter(base)
	if first == 0 {
		t.Fatalf("no turnover within 400 days of %d", base)
	}
	period := turnoverAfter(base + first*day)
	if period == 0 {
		t.Fatalf("no second turnover within 400 days of %d", base+first*day)
	}

	// 2^32 ms is 49.7 days, and this is sampled a day at a time.
	if period < 48 || period > 51 {
		t.Errorf("second shard byte turns over every %d days; layout.go claims about 50", period)
	}
}

// And the first byte, which decides how many top-level shard directories ever
// exist. About 35 years means a board sees one or two in its life.
func TestFirstShardByteStepsOnceInDecades(t *testing.T) {
	const year = int64(365.25 * 24 * 60 * 60 * 1000)
	base := int64(1_700_000_000_000)
	shard := func(ms int64) string { return ID(uint64(ms) << seqBits).Hex()[0:2] }

	if shard(base) != shard(base+10*year) {
		t.Errorf("the first shard byte moved within ten years; layout.go claims about 35")
	}
	if shard(base) == shard(base+80*year) {
		t.Errorf("the first shard byte did not move within eighty years")
	}
}
