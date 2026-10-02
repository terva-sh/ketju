//go:build !(386 || arm || mips || mipsle)

package threads

import (
	"math"
	"testing"
	"time"
)

// TestSecondsDurationPast32Bits holds the rows of TestSecondsDuration whose
// counts do not fit a 32-bit int: the most seconds a Duration holds, one
// past it, the largest int, and ten billion either side of zero. A 32-bit
// build cannot compile most of them, and its largest int is well inside a
// Duration, so they live here behind a build constraint, and the rest of
// the table compiles and holds on every word size (TKT-01M3VVZE4G).
func TestSecondsDurationPast32Bits(t *testing.T) {
	const most = int(math.MaxInt64 / int64(time.Second))
	longest := time.Duration(math.MaxInt64)
	checkSecondsDuration(t, []secondsCase{
		{most, time.Duration(most) * time.Second},
		{most + 1, longest},
		{10_000_000_000, longest},
		{math.MaxInt, longest},
		{-10_000_000_000, 0},
	})
}
