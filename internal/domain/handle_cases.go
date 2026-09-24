package domain

// HandleCase is one row of the shared handle table.
type HandleCase struct {
	Handle string
	Valid  bool
	Why    string
}

// HandleCases exercises every clause of the handle rule. Both
// ValidateHandle's test and the users table's CHECK test run it, so a change
// to one rule that is not made to the other fails a test.
var HandleCases = []HandleCase{
	{"abc", true, "minimum length"},
	{"drew", true, "letters"},
	{"a1_b-c", true, "every allowed class"},
	{"9lives", true, "may start with a digit"},
	{"abcdefghijklmnopqrstuvwxyz0123", true, "maximum length, 30"},
	{"ab", false, "too short"},
	{"abcdefghijklmnopqrstuvwxyz01234", false, "too long, 31"},
	{"Drew", false, "uppercase is not stored"},
	{"_drew", false, "may not start with _"},
	{"-drew", false, "may not start with -"},
	{"dr ew", false, "space"},
	{"dr.ew", false, "dot"},
	{"drew@x", false, "at sign"},
	{"dréw", false, "non-ASCII letter"},
	{"drеw", false, "Cyrillic е, a homoglyph"},
	{"", false, "empty"},
}
