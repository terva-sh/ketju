package threadlog

import (
	"bytes"
	"errors"
	"sync"
	"testing"
)

// Tests for an archived thread's actor (TKT-01M29CRJ64): after a
// ThreadArchived nothing more is appended, for anyone (docs/03, docs/08
// "Locking semantics").

func archived() Append {
	return Append{Type: TypeThreadArchived, Author: 1, Body: &ThreadArchivedBody{Reason: "closed history"}}
}

// recordTypes is every record type in the log, in order.
func recordTypes(t *testing.T, log []byte) []RecordType {
	t.Helper()
	var out []RecordType
	if err := ScanFrames(bytes.NewReader(log), int64(len(log)), func(r ScanRecord) bool {
		if r.Err != nil {
			t.Fatal(r.Err)
		}
		out = append(out, r.Header.Type)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestActorRefusesAfterThreadArchived: an actor that wrote a ThreadArchived
// refuses a Post, an Edit and a ThreadLocked with ErrArchived and writes
// nothing; a batch staging a ThreadArchived refuses what follows it; and
// Seal refuses at once on a fresh actor.
func TestActorRefusesAfterThreadArchived(t *testing.T) {
	a, lg, _, _ := newThread(t, SyncGroup)
	if _, err := a.Append(post("before")); err != nil {
		t.Fatal(err)
	}
	if a.Sealed() {
		t.Fatal("a fresh actor is sealed")
	}
	if _, err := a.Append(archived()); err != nil {
		t.Fatal(err)
	}
	if !a.Sealed() {
		t.Error("writing a ThreadArchived did not seal the actor")
	}
	n := len(lg.bytes())
	for _, req := range []Append{
		post("after"),
		{Type: TypeEdit, Ordinal: 1, Author: 42, Body: &EditBody{Source: "changed"}},
		{Type: TypeThreadLocked, Author: 1, Body: &ThreadLockedBody{}},
		archived(),
	} {
		if _, err := a.Append(req); !errors.Is(err, ErrArchived) {
			t.Errorf("a %s after the archive: %v, want ErrArchived", req.Type, err)
		}
	}
	if len(lg.bytes()) != n {
		t.Errorf("the refused appends wrote %d bytes", len(lg.bytes())-n)
	}
	if got := recordTypes(t, lg.bytes()); len(got) != 2 || got[1] != TypeThreadArchived {
		t.Errorf("the log holds %v, want the post and the archive", got)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	// In one batch, what is staged after a ThreadArchived is refused.
	b, _, _, _ := newThread(t, SyncGroup)
	staging := []staged{{header: FrameHeader{Type: TypeThreadArchived}}}
	if _, _, err := b.frameFor(post("queued behind the archive"), staging); !errors.Is(err, ErrArchived) {
		t.Errorf("a post staged after a ThreadArchived: %v, want ErrArchived", err)
	}
	if _, _, err := b.frameFor(post("queued alone"), nil); err != nil {
		t.Errorf("a post with nothing staged: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}

	// Seal refuses at once.
	c, lc, _, _ := newThread(t, SyncGroup)
	c.Seal()
	if _, err := c.Append(post("never")); !errors.Is(err, ErrArchived) {
		t.Errorf("a post to a sealed actor: %v, want ErrArchived", err)
	}
	if got := recordTypes(t, lc.bytes()); len(got) != 0 {
		t.Errorf("a sealed actor wrote %v", got)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestNothingLandsAfterAnArchiveInABatch: posts queued at the same moment as
// an archive, so that some share its batch, land before it or are refused.
// No round leaves a record after the ThreadArchived.
func TestNothingLandsAfterAnArchiveInABatch(t *testing.T) {
	for round := range 50 {
		rec := &recorder{}
		gate := make(chan struct{})
		lg := &fakeLog{rec: rec, data: NewFileHeader(1, 1, 1).AppendTo(nil), gate: gate}
		a := NewActor(&Index{Header: IndexHeader{ThreadID: 1}}, int64(len(lg.data)), lg, &fakeIndex{rec: rec}, SyncGroup)

		var wg sync.WaitGroup
		errs := make([]error, 12)
		wg.Add(1)
		go func() { defer wg.Done(); _, errs[0] = a.Append(post("gate holder")) }()
		for i := 1; i < len(errs); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if i == len(errs)/2 {
					_, errs[i] = a.Append(archived())
					return
				}
				_, errs[i] = a.Append(post("racing"))
			}()
		}
		close(gate)
		wg.Wait()
		if err := a.Close(); err != nil {
			t.Fatal(err)
		}
		if errs[len(errs)/2] != nil {
			t.Fatalf("round %d: the archive failed: %v", round, errs[len(errs)/2])
		}
		types := recordTypes(t, lg.bytes())
		if types[len(types)-1] != TypeThreadArchived {
			t.Fatalf("round %d: the log ends %v, after its ThreadArchived", round, types)
		}
		refused := 0
		for i, err := range errs {
			switch {
			case err == nil:
			case errors.Is(err, ErrArchived):
				refused++
			default:
				t.Fatalf("round %d: append %d: %v", round, i, err)
			}
		}
		if len(types)+refused != len(errs) {
			t.Fatalf("round %d: %d records and %d refusals for %d appends", round, len(types), refused, len(errs))
		}
	}
}
