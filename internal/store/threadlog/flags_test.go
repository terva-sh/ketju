package threadlog

import "testing"

// Criterion: frame flag bit 0 is an exported, documented constant.
//
// The number is asserted against a literal on purpose. A test that said
// FrameFlagMachineAuthored == FrameFlagMachineAuthored would pass through any
// edit; this one fails if the bit ever moves, which is exactly what the format
// freeze is about to make illegal.
func TestFrameFlagBitZeroIsMachineAuthored(t *testing.T) {
	if FrameFlagMachineAuthored != 1 {
		t.Errorf("FrameFlagMachineAuthored = %#b, docs/03 assigns it bit 0", FrameFlagMachineAuthored)
	}
	if FrameFlagsKnown != FrameFlagMachineAuthored {
		t.Errorf("FrameFlagsKnown = %#b, bit 0 is the only assignment docs/03 makes", FrameFlagsKnown)
	}
}

// Criterion: the reserved bits are named so the next assignment is unambiguous.
func TestFrameReservedBitsAreEveryBitButZero(t *testing.T) {
	if FrameFlagsReserved != 0xfffe {
		t.Errorf("FrameFlagsReserved = %#x, want 0xfffe (bits 1 through 15)", FrameFlagsReserved)
	}
	if FrameFlagsReserved&FrameFlagsKnown != 0 {
		t.Error("reserved and assigned bits overlap; one of them is wrong")
	}
	if FrameFlagsReserved|FrameFlagsKnown != 0xffff {
		t.Error("reserved and assigned together must account for all 16 bits")
	}

	// The next bit to hand out is the lowest reserved one. Spelling it out
	// means the next person does not have to derive it.
	//
	// Via variables because -x on an unsigned CONSTANT is a compile error
	// rather than a wraparound; on a variable it is the two's complement trick
	// that isolates the lowest set bit.
	fr := FrameFlagsReserved
	if next := fr & -fr; next != 1<<1 {
		t.Errorf("next free frame flag is %#b, want bit 1", next)
	}
	er := EntryFlagsReserved
	if next := er & -er; next != 1<<6 {
		t.Errorf("next free entry flag is %#b, want bit 6", next)
	}
}

func TestEntryReservedBitsAreTheTenDocumented(t *testing.T) {
	if EntryFlagsReserved != 0xffc0 {
		t.Errorf("EntryFlagsReserved = %#x, want 0xffc0 (bits 6 through 15)", EntryFlagsReserved)
	}
	if EntryFlagsReserved&EntryFlagsKnown != 0 {
		t.Error("reserved and assigned entry bits overlap")
	}
	if n := popcount(uint16(EntryFlagsReserved)); n != 10 {
		t.Errorf("%d reserved entry bits, docs/03 names 10 as the spare capacity", n)
	}
}

// The trap this file exists to close: the same fact has different numbers in
// the two flag words. If these were ever equal, crossing them by hand would
// work by accident and break the day one of them moved.
func TestMachineAuthoredHasDifferentBitsInFrameAndEntry(t *testing.T) {
	if uint16(FrameFlagMachineAuthored) == uint16(EntryMachineAuthored) {
		t.Fatal("frame and entry machine-authored bits coincide; the ToEntryFlags " +
			"crossing is what keeps them honest and this test is what makes it necessary")
	}
	if FrameFlagMachineAuthored != 1<<0 || EntryMachineAuthored != 1<<5 {
		t.Errorf("machine-authored is frame bit %#b and entry bit %#b, docs/03 says 0 and 5",
			FrameFlagMachineAuthored, EntryMachineAuthored)
	}
}

func TestToEntryFlagsCarriesMachineAuthoredAcross(t *testing.T) {
	if got := FrameFlagMachineAuthored.ToEntryFlags(); got != EntryMachineAuthored {
		t.Errorf("ToEntryFlags = %#b, want %#b", got, EntryMachineAuthored)
	}
	if got := FrameFlags(0).ToEntryFlags(); got != 0 {
		t.Errorf("ToEntryFlags on no flags = %#b, want 0", got)
	}

	// Unknown bits from a newer writer must not leak into the entry. An entry
	// flag means something specific to a lens, and inventing one from a bit
	// this build cannot name would make the lens answer nonsense.
	if got := FrameFlags(0xfffe).ToEntryFlags(); got != 0 {
		t.Errorf("reserved frame bits produced entry flags %#b, want 0", got)
	}
	if got := FrameFlags(0xffff).ToEntryFlags(); got != EntryMachineAuthored {
		t.Errorf("ToEntryFlags with every bit set = %#b, want only machine-authored", got)
	}
}

func TestFrameFlagsHasAndUnknown(t *testing.T) {
	f := FrameFlagMachineAuthored | 1<<7 // bit 7 is a newer build's business

	if !f.Has(FrameFlagMachineAuthored) {
		t.Error("Has missed a bit that is set")
	}
	if f.Has(1 << 9) {
		t.Error("Has reported a bit that is not set")
	}
	if got := f.Unknown(); got != 1<<7 {
		t.Errorf("Unknown = %#b, want just bit 7", got)
	}
	if got := FrameFlagMachineAuthored.Unknown(); got != 0 {
		t.Errorf("Unknown on a fully understood flag word = %#b, want 0", got)
	}
}

// The constants must not have changed what goes on disk. Bit 0 set through the
// named constant has to produce the same byte as the literal the code used
// before this file existed.
func TestNamedFlagEncodesToTheSameByteAsTheLiteral(t *testing.T) {
	named, err := AppendFrame(nil, FrameHeader{
		Type: TypePost, Ordinal: 1, Revision: 1, Flags: FrameFlagMachineAuthored,
	}, []byte("b"))
	if err != nil {
		t.Fatal(err)
	}
	literal, err := AppendFrame(nil, FrameHeader{
		Type: TypePost, Ordinal: 1, Revision: 1, Flags: 1,
	}, []byte("b"))
	if err != nil {
		t.Fatal(err)
	}

	if string(named) != string(literal) {
		t.Fatalf("naming the bit changed the bytes:\n named %x\nliteral %x", named, literal)
	}
	if named[frFlags] != 1 || named[frFlags+1] != 0 {
		t.Errorf("flags on disk are %#x %#x, want little-endian 0x0001",
			named[frFlags], named[frFlags+1])
	}
}
