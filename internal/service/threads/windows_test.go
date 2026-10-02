package threads

import (
	"math"
	"testing"
	"time"
)

// TestSecondsDuration: every count of seconds from 0 up converts exactly
// while a Duration holds it, and past that is the longest Duration there
// is, never a negative one that would close a window at once or turn slow
// mode off (TKT-01M3R33SR5). A negative count, which no setting admits, is
// 0 rather than a product that wrapped round to an open window.
func TestSecondsDuration(t *testing.T) {
	checkSecondsDuration(t, []secondsCase{
		{0, 0},
		{1, time.Second},
		{86400, 24 * time.Hour},
		{-1, 0},
		{math.MinInt, 0},
	})
	// Every count from 0 up, sampled by doubling across the whole range,
	// never gives a negative Duration and never gives less than a smaller
	// count did.
	prev := time.Duration(0)
	for n := 1; n > 0; n *= 2 {
		for _, m := range []int{n - 1, n, n + 1} {
			if m < 0 {
				continue
			}
			if d := secondsDuration(m); d < 0 {
				t.Fatalf("secondsDuration(%d) is negative: %d", m, d)
			}
		}
		d := secondsDuration(n)
		if d < prev {
			t.Fatalf("secondsDuration(%d) = %d, less than the smaller count's %d", n, d, prev)
		}
		prev = d
	}
}

// secondsCase is one row of a secondsDuration table.
type secondsCase struct {
	n    int
	want time.Duration
}

func checkSecondsDuration(t *testing.T, cases []secondsCase) {
	t.Helper()
	for _, c := range cases {
		got := secondsDuration(c.n)
		if got != c.want {
			t.Errorf("secondsDuration(%d) = %d, want %d", c.n, got, c.want)
		}
		if got < 0 {
			t.Errorf("secondsDuration(%d) is negative: %d", c.n, got)
		}
	}
}
