package projection

import (
	"testing"

	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestClassify is the rule catch-up acts on and fsck reports (docs/03
// "Durability and recovery"): longer is drift, shorter or missing is
// corruption, and a header-only log nobody projected is unfinished.
func TestClassify(t *testing.T) {
	const h = threadlog.FileHeaderSize
	for _, tc := range []struct {
		name           string
		log, projected int64
		want           Class
	}{
		{"current", 500, 500, ClassCurrent},
		{"log longer than its projection", 600, 500, ClassDrift},
		{"log shorter than its projection", 400, 500, ClassCorrupt},
		{"a projection with no log", Missing, 500, ClassCorrupt},
		{"a projection of the header alone with no log", Missing, h, ClassCorrupt},
		{"a projection of zero bytes with no log", Missing, 0, ClassCorrupt},
		{"frames the projection never saw", 600, Missing, ClassDrift},
		{"a header-only log nobody projected", h, Missing, ClassUnfinished},
		{"a header-only log projected to its header", h, h, ClassCurrent},
		{"a log shorter than a header, never projected", h - 1, Missing, ClassCorrupt},
		{"an empty log, never projected", 0, Missing, ClassCorrupt},
		{"neither", Missing, Missing, ClassCurrent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.log, tc.projected); got != tc.want {
				t.Errorf("Classify(%d, %d) = %v, want %v", tc.log, tc.projected, got, tc.want)
			}
		})
	}
}
