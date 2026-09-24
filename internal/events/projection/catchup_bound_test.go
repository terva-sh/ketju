//go:build !race

package projection

import (
	"sync/atomic"
	"testing"
	"time"
)

// catchUpBound is AC4's bound: catch-up over ten thousand threads with no
// drift, on the CI runner. The race detector slows everything several times
// over and says nothing about speed, so this file is built without it and CI
// runs it in a step of its own (settled in grooming, 2026-09-24).
const catchUpBound = 2 * time.Second

func TestCatchUpTenThousandUnderBound(t *testing.T) {
	f := newFixture(t)
	f.tenThousand(10_000)
	var reopens atomic.Int64
	rep, err := f.w.CatchUp(t.Context(), reopenWith(f.cache, &reopens))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Scanned != 10_000 {
		t.Fatalf("scanned %d threads, want 10000", rep.Scanned)
	}
	t.Logf("catch-up over %d threads with no drift took %s (bound %s)", rep.Scanned, rep.Elapsed, catchUpBound)
	if rep.Elapsed > catchUpBound {
		t.Errorf("catch-up took %s, over the %s bound", rep.Elapsed, catchUpBound)
	}
}
