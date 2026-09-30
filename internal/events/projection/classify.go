package projection

import "github.com/terva-sh/ketju/internal/store/threadlog"

// Class is how a thread's log stands against its projection_state row
// (docs/03 "Durability and recovery").
type Class int

const (
	// ClassCurrent: the projection has reached the end of the log.
	ClassCurrent Class = iota

	// ClassUnfinished: a log holding only its file header and no row. Its
	// creation never finished and there is nothing to project.
	ClassUnfinished

	// ClassDrift: the log is longer than its projection, or has frames and
	// no row. A crash between the log write and the database write leaves
	// this, and catch-up closes it.
	ClassDrift

	// ClassCorrupt: the log is shorter than its projection, or the row has
	// no log. The projection is written after the log, so no crash causes
	// this. Serve quarantines the thread.
	ClassCorrupt
)

func (c Class) String() string {
	switch c {
	case ClassCurrent:
		return "current"
	case ClassUnfinished:
		return "unfinished"
	case ClassDrift:
		return "drift"
	case ClassCorrupt:
		return "corrupt"
	}
	return "unknown"
}

// Missing stands for an absent log or an absent projection_state row in
// Classify. A length of 0 cannot: a row may hold 0, and a log file may be
// empty, and each is still there.
const Missing int64 = -1

// Classify says how a log of logLen bytes stands against a projection of
// projected bytes, either of which may be Missing. It is the one rule that
// startup catch-up (CatchUp) acts on and `ketju fsck` reports, so the two
// cannot disagree about which thread serve quarantines (TKT-01M3A054TR).
//
// logLen is whatever length the caller trusts: catch-up passes the file size
// first and then the length an open that repaired a torn tail reports, and
// fsck passes the length to the last whole frame.
//
// Neither being present is ClassCurrent: there is nothing to reconcile.
func Classify(logLen, projected int64) Class {
	switch {
	case logLen == Missing && projected == Missing:
		return ClassCurrent
	case logLen == Missing:
		// A projection whose log is gone is the shortest log there is.
		return ClassCorrupt
	case projected == Missing:
		if logLen == threadlog.FileHeaderSize {
			return ClassUnfinished
		}
		// A thread the projection never saw is projected from the end of
		// its header.
		projected = threadlog.FileHeaderSize
	}
	switch {
	case logLen == projected:
		return ClassCurrent
	case logLen < projected:
		return ClassCorrupt
	default:
		return ClassDrift
	}
}
