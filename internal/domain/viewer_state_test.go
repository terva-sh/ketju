package domain

import "testing"

// watchCases is every combination of a thread's own level and its forum's,
// with the level that wins (docs/04 "watches"). The SQL's half,
// TestEffectiveWatchSQLAgrees in internal/store/db/views, runs every
// combination through the query and takes EffectiveWatch as its reference.
var watchCases = []struct{ thread, forum, want string }{
	{WatchNone, WatchNone, WatchNone},
	{WatchNone, WatchWatch, WatchWatch},
	{WatchNone, WatchMute, WatchMute},
	{WatchWatch, WatchNone, WatchWatch},
	{WatchWatch, WatchWatch, WatchWatch},
	// A watched thread in a muted forum is watched.
	{WatchWatch, WatchMute, WatchWatch},
	{WatchMute, WatchNone, WatchMute},
	// A muted thread in a watched forum is muted: the case the ticket names.
	{WatchMute, WatchWatch, WatchMute},
	{WatchMute, WatchMute, WatchMute},
}

// TestEffectiveWatch is AC1's Go half: the thread's own setting beats the
// forum's in both directions, and an unset thread follows its forum.
func TestEffectiveWatch(t *testing.T) {
	if len(watchCases) != 9 {
		t.Fatalf("%d cases, want all 9 combinations", len(watchCases))
	}
	for _, c := range watchCases {
		if got := EffectiveWatch(c.thread, c.forum); got != c.want {
			t.Errorf("EffectiveWatch(%q, %q) = %q, want %q", c.thread, c.forum, got, c.want)
		}
	}
}

func TestValidWatchLevel(t *testing.T) {
	for level, want := range map[string]bool{"": true, "watch": true, "mute": true,
		"Watch": false, "muted": false, " ": false, "none": false} {
		if got := ValidWatchLevel(level); got != want {
			t.Errorf("ValidWatchLevel(%q) = %t, want %t", level, got, want)
		}
	}
}
