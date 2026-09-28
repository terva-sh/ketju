package domain

import "strings"

// HandleCase is one row of the shared handle table.
type HandleCase struct {
	Handle string
	Valid  bool
	Why    string

	// Grandfathered marks a handle that ValidateHandle refuses to a new
	// account but the users CHECK still stores, because an account may
	// already hold it: a trailing '-' (TKT-01M3ANX1NF).
	Grandfathered bool
}

// HandleCases exercises every clause of the handle rule. Both
// ValidateHandle's test and the users table's CHECK test run it, so a change
// to one rule that is not made to the other fails a test. The two rules
// differ only in the rows marked Grandfathered.
var HandleCases = []HandleCase{
	{Handle: "abc", Valid: true, Why: "minimum length"},
	{Handle: "drew", Valid: true, Why: "letters"},
	{Handle: "a1_b-c", Valid: true, Why: "every allowed class"},
	{Handle: "9lives", Valid: true, Why: "may start with a digit"},
	{Handle: "abcdefghijklmnopqrstuvwxyz0123", Valid: true, Why: "maximum length, 30"},
	{Handle: "ab", Valid: false, Why: "too short"},
	{Handle: "abcdefghijklmnopqrstuvwxyz01234", Valid: false, Why: "too long, 31"},
	{Handle: "Drew", Valid: false, Why: "uppercase is not stored"},
	{Handle: "_drew", Valid: false, Why: "may not start with _"},
	{Handle: "-drew", Valid: false, Why: "may not start with -"},
	{Handle: "dr ew", Valid: false, Why: "space"},
	{Handle: "dr.ew", Valid: false, Why: "dot"},
	{Handle: "drew@x", Valid: false, Why: "at sign"},
	{Handle: "dréw", Valid: false, Why: "non-ASCII letter"},
	{Handle: "drеw", Valid: false, Why: "Cyrillic е, a homoglyph"},
	{Handle: "", Valid: false, Why: "empty"},
	// A trailing '-' is refused to a new handle, and stored if one exists.
	{Handle: "bob-", Why: "ends in -", Grandfathered: true},
	{Handle: "a--", Why: "only dashes after the first character, minimum length", Grandfathered: true},
	{Handle: "a" + strings.Repeat("-", 29), Why: "only dashes after the first character, maximum length", Grandfathered: true},
	{Handle: "a" + strings.Repeat("-", 30), Why: "only dashes after the first character, too long"},
	{Handle: "b_-", Why: "ends in - after _", Grandfathered: true},
	{Handle: "b-_", Valid: true, Why: "a - before the last character is allowed"},
	{Handle: "bob_", Valid: true, Why: "a trailing _ is allowed"},
	{Handle: "a__", Valid: true, Why: "only underscores after the first character"},
	// SQLite's length() and GLOB stop at a NUL, so this is the case that
	// separates checking the characters from checking every byte.
	{Handle: "abc\x00!", Valid: false, Why: "embedded NUL, with a forbidden byte after it"},
	{Handle: "abc\x00", Valid: false, Why: "trailing NUL"},
}
