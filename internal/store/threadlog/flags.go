package threadlog

// Flag bits, for both the record frame and the index entry.
//
// The two sets live in one file because the dangerous thing about them is the
// relationship between them, not either set alone: "machine-authored" is bit 0
// of a frame's flags and bit 5 of an index entry's, the same fact at two
// different numbers. Kept apart, that is an invitation to write 1 where 32
// belongs. Kept together, with [FrameFlags.ToEntryFlags] as the only crossing,
// it is one reviewed function.
//
// docs/03 is the format spec and the tables there are the wire truth. These
// constants are the binding declaration for anything in this repository:
// callers use the name, never the literal. That is the whole point of the
// ticket that added them — the assignment used to live only in a Markdown
// table while three separate call sites had to agree on the number 1.
//
// Nothing here validates. The parser stays flag-agnostic on purpose: refusing
// a bit this build does not know would destroy the forward compatibility that
// [FrameHeader] exists to provide and that TestFrameUnknownFlagBitsRoundTrip
// protects.

// FrameFlags are the bits in a record frame's flags field (docs/03, "Frame
// flags").
//
// A distinct type from [EntryFlags] deliberately. The compiler is the only
// thing that reliably stops the two being crossed, because they describe
// overlapping facts at different bit positions and both are 16 bits wide.
type FrameFlags uint16

const (
	// FrameFlagMachineAuthored is bit 0: the actor was an agent account when
	// this record was written.
	//
	// The server sets it from users.kind (docs/04) at append time and a client
	// never supplies it. It is not redundant with the author's account: kind
	// is current state and can change, while the log records what was true
	// when the post was written, and no projection can reconstruct that
	// afterwards.
	//
	// docs/09 makes this assignment a Phase 0 exit criterion, and the format
	// freeze is the moment it stops being editable.
	FrameFlagMachineAuthored FrameFlags = 1 << 0

	// FrameFlagsReserved is every bit not yet assigned: 1 through 15, written
	// zero.
	//
	// Named rather than left implicit so the next assignment is obvious and
	// not a guess. The next bit to hand out is the lowest one still in here,
	// which is bit 1.
	FrameFlagsReserved FrameFlags = ^FrameFlagMachineAuthored

	// FrameFlagsKnown is every bit this build assigns meaning to. A reader
	// preserves everything outside it untouched.
	FrameFlagsKnown FrameFlags = FrameFlagMachineAuthored
)

// Has reports whether every bit in want is set.
func (f FrameFlags) Has(want FrameFlags) bool { return f&want == want }

// Unknown returns the bits this build has no meaning for.
//
// Useful for a diagnostic — `ketju dump` saying a record carries flags from a
// newer writer — and never for a decision. Acting on it would make an older
// binary refuse a log a newer one wrote.
func (f FrameFlags) Unknown() FrameFlags { return f &^ FrameFlagsKnown }

// EntryFlags are the per-post bits an index entry carries so that a lens can
// answer without reading the log (docs/03, "Index entry").
//
// Six bits used, ten spare. docs/03 names those ten as the spare capacity for
// this format — the four reserved bytes at offset 36 are NOT general slack,
// they are held for the segment number that segment rollover would add to the
// offset field.
type EntryFlags uint16

const (
	// EntryDeleted marks a soft-deleted post. The Post record's bytes are
	// still on disk and still readable for history; the reader returns a
	// tombstone from this bit without going near them.
	EntryDeleted EntryFlags = 1 << 0

	// EntryMoved marks a post split out into another thread. The post renders
	// as a pointer and its ordinal is never reused, which is what preserves
	// every permalink ever issued.
	EntryMoved EntryFlags = 1 << 1

	// EntryHiddenByMod marks a post hidden by moderation, which is distinct
	// from deleted: the author did not ask for it and may not see it.
	EntryHiddenByMod EntryFlags = 1 << 2

	// EntryHasAttachments lets a listing show a paperclip without decoding
	// every body on the page.
	EntryHasAttachments EntryFlags = 1 << 3

	// EntryEdited marks a post with a visible edit: an Edit that is not a
	// grace edit (TKT-01M29CKRHS). A post whose only edits are grace edits
	// has more than one revision and no edited flag.
	EntryEdited EntryFlags = 1 << 4

	// EntryMachineAuthored mirrors [FrameFlagMachineAuthored] — note the
	// different bit position, 5 here against 0 in the frame.
	//
	// Mirrored rather than referenced because the whole point of the index is
	// to answer without opening the log. Use [FrameFlags.ToEntryFlags] to
	// carry it across; do not write the constant from one set into the other.
	EntryMachineAuthored EntryFlags = 1 << 5

	// EntryFlagsReserved is every bit not yet assigned: 6 through 15, written
	// zero. The next bit to hand out is bit 6.
	EntryFlagsReserved EntryFlags = ^EntryFlagsKnown

	// EntryFlagsKnown is every bit this build assigns meaning to.
	EntryFlagsKnown EntryFlags = EntryDeleted | EntryMoved | EntryHiddenByMod |
		EntryHasAttachments | EntryEdited | EntryMachineAuthored
)

// Has reports whether every bit in want is set.
func (f EntryFlags) Has(want EntryFlags) bool { return f&want == want }

// Visible reports whether a post should render as itself rather than as a
// tombstone or a pointer.
//
// One place, so that a lens cannot answer this question differently from the
// read path by forgetting a bit.
func (f EntryFlags) Visible() bool {
	return f&(EntryDeleted|EntryMoved|EntryHiddenByMod) == 0
}

// ToEntryFlags carries the bits a frame and an index entry share across the
// gap between them.
//
// This is the only place the two numberings meet, and it exists because they
// disagree: machine-authored is bit 0 in a frame and bit 5 in an entry. The
// index builder mirrors the bit, a lens reads it back, and docs/03 says so in
// prose — which is three call sites and a Markdown table that all had to agree
// on a number, with nothing to stop them drifting.
//
// Only machine-authored crosses today. Every other entry flag is a property of
// the projection rather than of the record: deleted and restored come from
// LATER records than the one being indexed, and edited is a count of them, so
// none can be read off a single frame.
func (f FrameFlags) ToEntryFlags() EntryFlags {
	var e EntryFlags
	if f.Has(FrameFlagMachineAuthored) {
		e |= EntryMachineAuthored
	}
	return e
}
