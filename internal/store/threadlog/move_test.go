package threadlog

import (
	"bytes"
	"errors"
	"slices"
	"sync"
	"testing"
)

// Tests for the move records in the log and the actor (TKT-01M29CXJZZ,
// docs/03 "How a move is written").

func splitOut(target uint64, ords ...uint32) Append {
	return Append{Type: TypePostsSplitOut, Author: 5, Body: &PostsSplitOutBody{Ordinals: ords, TargetThreadID: target}}
}

func mergedIn(source uint64, ords ...uint32) Append {
	return Append{Type: TypePostsMergedIn, Author: 5, Body: &PostsMergedInBody{SourceThreadID: source, Ordinals: ords}}
}

// reopen is the index the next open builds from what the actor left: the
// trusted index file when its header covers the log, and the outcome.
func reopen(t *testing.T, lg *fakeLog, ixf *fakeIndex) (*Index, Outcome) {
	t.Helper()
	b := lg.bytes()
	rec, err := Open(bytes.NewReader(b), int64(len(b)), ixf.bytes())
	if err != nil {
		t.Fatal(err)
	}
	return rec.Index, rec.Outcome
}

// replay is the index rebuilt from the log alone.
func replay(t *testing.T, lg *fakeLog) *Index {
	t.Helper()
	b := lg.bytes()
	rec, err := Open(bytes.NewReader(b), int64(len(b)), nil)
	if err != nil {
		t.Fatal(err)
	}
	return rec.Index
}

// lastIndex is the last position of op in ops before end, or -1.
func lastIndex(ops []string, op string, end int) int {
	for i := min(end, len(ops)) - 1; i >= 0; i-- {
		if ops[i] == op {
			return i
		}
	}
	return -1
}

func movedOrdinals(t *testing.T, ix *Index) []uint32 {
	t.Helper()
	var out []uint32
	for o := uint32(1); o <= uint32(ix.Len()); o++ {
		e, err := ix.Entry(o)
		if err != nil {
			t.Fatal(err)
		}
		if e.Flags.Has(EntryMoved) {
			out = append(out, o)
		}
	}
	return out
}

// TestSplitOutMarksMoved is criterion 1 at the log: a PostsSplitOut marks
// the posts it names as moved and renumbers nothing, so every ordinal, and
// so every permalink, still resolves. The index file the actor leaves is
// trusted on the next open and says the same, because the split's entries
// are written before the header that covers them; and a replay from the log
// alone agrees.
func TestSplitOutMarksMoved(t *testing.T) {
	for _, policy := range []SyncPolicy{SyncAlways, SyncGroup} {
		a, lg, ixf, rec := newThread(t, policy)
		for _, s := range []string{"one", "two", "three", "four", "five"} {
			if _, err := a.Append(post(s)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := a.Append(splitOut(2, 4, 2)); err != nil {
			t.Fatal(err)
		}
		// The split's batch wrote its two entries and synced the index
		// after the log's sync and before the header.
		ops := rec.snapshot()
		header := lastIndex(ops, "index.header", len(ops))
		logSync := lastIndex(ops, "log.sync", header)
		idxSync := lastIndex(ops, "index.sync", header)
		entries := 0
		for _, op := range ops[logSync+1 : max(logSync+1, idxSync)] {
			if op == "index.entry" {
				entries++
			}
		}
		if idxSync < logSync || entries < 2 {
			t.Errorf("%v: the split's batch did %v; want two entries and an index sync between the log sync and the header",
				policy, ops[max(0, logSync):])
		}
		if err := a.Close(); err != nil {
			t.Fatal(err)
		}
		ix, outcome := reopen(t, lg, ixf)
		if outcome != Trusted {
			t.Fatalf("%v: the reopened index was %v, want Trusted", policy, outcome)
		}
		for name, ix := range map[string]*Index{"live": a.Index(), "reopened": ix, "replayed": replay(t, lg)} {
			if ix.Len() != 5 {
				t.Errorf("%v %s: %d posts after the split, want 5", policy, name, ix.Len())
			}
			if got := movedOrdinals(t, ix); !slices.Equal(got, []uint32{2, 4}) {
				t.Errorf("%v %s: moved %v, want [2 4]", policy, name, got)
			}
		}
	}
}

// TestSplitOutRefusesUnknownOrMovedOrdinal: the actor refuses a split that
// names a post it does not hold, one not shown as itself, or one already
// moved, and one that is not thread-level or names this thread, and writes
// nothing. After a split, no record may name a moved post.
func TestSplitOutRefusesUnknownOrMovedOrdinal(t *testing.T) {
	a, lg, _, _ := newThread(t, SyncGroup)
	for _, s := range []string{"one", "two", "three"} {
		if _, err := a.Append(post(s)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.Append(Append{Type: TypeDelete, Ordinal: 3, Author: 42, Body: &DeleteBody{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Append(splitOut(2, 1)); err != nil {
		t.Fatal(err)
	}
	n := len(lg.bytes())
	for _, c := range []struct {
		name string
		app  Append
		want error
	}{
		{"a post the thread does not hold", splitOut(2, 9), ErrNoSuchOrdinal},
		{"a deleted post", splitOut(2, 3), ErrPostState},
		{"a post already moved", splitOut(2, 1), ErrPostState},
		{"this thread", splitOut(1, 2), ErrMalformed},
		{"a frame ordinal", Append{Type: TypePostsSplitOut, Ordinal: 2, Body: &PostsSplitOutBody{Ordinals: []uint32{2}, TargetThreadID: 2}}, ErrMalformed},
		{"a merge with no post", mergedIn(2), ErrMalformed},
		{"an edit of the moved post", Append{Type: TypeEdit, Ordinal: 1, Author: 42, Body: &EditBody{Source: "x"}}, ErrPostState},
		{"a delete of the moved post", Append{Type: TypeDelete, Ordinal: 1, Author: 42, Body: &DeleteBody{}}, ErrPostState},
		{"a restore of the moved post", Append{Type: TypeRestore, Ordinal: 1, Author: 42, Body: &RestoreBody{}}, ErrPostState},
	} {
		if _, err := a.Append(c.app); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	if len(lg.bytes()) != n {
		t.Errorf("the refusals wrote %d bytes", len(lg.bytes())-n)
	}

	// In one batch, a record staged behind a split that moves its post is
	// refused, and a second split of the same post too.
	staging := []staged{{app: splitOut(2, 2), header: FrameHeader{Type: TypePostsSplitOut}}}
	if _, _, err := a.frameFor(Append{Type: TypeEdit, Ordinal: 2, Author: 42, Body: &EditBody{Source: "x"}}, staging); !errors.Is(err, ErrPostState) {
		t.Errorf("an edit staged behind the split of its post: %v, want ErrPostState", err)
	}
	if _, _, err := a.frameFor(splitOut(2, 2), staging); !errors.Is(err, ErrPostState) {
		t.Errorf("a split staged behind a split of the same post: %v, want ErrPostState", err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestSplitFoldIsWhatApplyDoes holds the actor's view of a staged split to
// Apply's: from every starting state, the flags flagsAfter gives a post a
// staged split names are the flags Apply leaves on it, and a post the split
// does not name is untouched.
func TestSplitFoldIsWhatApplyDoes(t *testing.T) {
	raw, err := EncodeBody(EncodingCBOR, &PostsSplitOutBody{Ordinals: []uint32{1}, TargetThreadID: 2})
	if err != nil {
		t.Fatal(err)
	}
	for start := range EntryFlagsKnown + 1 {
		if start&^EntryFlagsKnown != 0 {
			continue
		}
		ix := &Index{entries: []IndexEntry{{Revision: 1, Flags: start}, {Revision: 1, Flags: start}}}
		a := &Actor{ix: ix}
		staging := []staged{{app: splitOut(2, 1), header: FrameHeader{Type: TypePostsSplitOut}}}
		want := [2]EntryFlags{}
		for o := uint32(1); o <= 2; o++ {
			f, err := a.flagsAfter(o, staging)
			if err != nil {
				t.Fatal(err)
			}
			want[o-1] = f
		}
		if err := ix.Apply(FrameHeader{Type: TypePostsSplitOut, Encoding: EncodingCBOR, Revision: 1}, raw, 0, 0); err != nil {
			t.Fatal(err)
		}
		for o := range 2 {
			if got := ix.entries[o].Flags; got != want[o] {
				t.Errorf("post %d from %#x: Apply left %#x, flagsAfter says %#x", o+1, start, got, want[o])
			}
		}
	}
}

// TestApplySplitIsStrictOnShapeLenientOnState: the index fold refuses a
// split that names a post it does not hold, or one with a frame ordinal,
// and changes nothing; it marks a post already moved or deleted all the
// same, so a log holding such a split still replays.
func TestApplySplitIsStrictOnShapeLenientOnState(t *testing.T) {
	body := func(ords ...uint32) []byte {
		raw, err := EncodeBody(EncodingCBOR, &PostsSplitOutBody{Ordinals: ords, TargetThreadID: 2})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	h := FrameHeader{Type: TypePostsSplitOut, Encoding: EncodingCBOR, Revision: 1}
	ix := &Index{entries: []IndexEntry{{Revision: 1}, {Revision: 1, Flags: EntryDeleted}, {Revision: 1, Flags: EntryMoved}}}
	if err := ix.Apply(h, body(1, 4), 0, 0); !errors.Is(err, ErrMalformed) {
		t.Errorf("a split past the last post: %v, want ErrMalformed", err)
	}
	if ix.entries[0].Flags != 0 {
		t.Errorf("a refused split marked post 1: %#x", ix.entries[0].Flags)
	}
	bad := h
	bad.Ordinal = 1
	if err := ix.Apply(bad, body(1), 0, 0); !errors.Is(err, ErrMalformed) {
		t.Errorf("a split with a frame ordinal: %v, want ErrMalformed", err)
	}
	if err := ix.Apply(h, body(2, 3), 0, 0); err != nil {
		t.Errorf("a split of a deleted and a moved post: %v", err)
	}
	if !ix.entries[1].Flags.Has(EntryMoved|EntryDeleted) || !ix.entries[2].Flags.Has(EntryMoved) {
		t.Errorf("flags %#x and %#x after the split", ix.entries[1].Flags, ix.entries[2].Flags)
	}
}

// TestEntryCopiedSet: a Post that carries an origin gets the copied flag,
// derived from the record alone, live and on replay; one without does not.
func TestEntryCopiedSet(t *testing.T) {
	a, lg, _, _ := newThread(t, SyncGroup)
	if _, err := a.Append(post("here")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Append(Append{Type: TypePost, Author: 42,
		Body: &PostBody{Source: "copied", Origin: &PostOrigin{ThreadID: 9, Ordinal: 3}}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	for name, ix := range map[string]*Index{"live": a.Index(), "replayed": replay(t, lg)} {
		e1, _ := ix.Entry(1)
		e2, _ := ix.Entry(2)
		if e1.Flags.Has(EntryCopied) || !e2.Flags.Has(EntryCopied) || !e2.Flags.Visible() {
			t.Errorf("%s: flags %#x and %#x, want copied on post 2 only", name, e1.Flags, e2.Flags)
		}
	}
}

// TestAppendRunContiguous: a run's records land in order, with consecutive
// ordinals for its posts and each Edit after its post, and no record
// appended at the same moment lands between them.
func TestAppendRunContiguous(t *testing.T) {
	for round := range 20 {
		a, lg, _, _ := newThread(t, SyncGroup)
		if _, err := a.Append(post("first")); err != nil {
			t.Fatal(err)
		}
		run := []Append{
			mergedIn(9, 3, 4),
			{Type: TypePost, Author: 7, Body: &PostBody{Source: "copy of 3", Origin: &PostOrigin{ThreadID: 9, Ordinal: 3}}},
			{Type: TypePost, Author: 8, Body: &PostBody{Source: "copy of 4", Origin: &PostOrigin{ThreadID: 9, Ordinal: 4}}},
		}
		var wg sync.WaitGroup
		var res []Result
		var runErr error
		for i := range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if i == 4 {
					res, runErr = a.AppendRun(run)
					return
				}
				if _, err := a.Append(post("racing")); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		if runErr != nil {
			t.Fatalf("round %d: %v", round, runErr)
		}
		if len(res) != 3 || res[1].Ordinal+1 != res[2].Ordinal || res[0].Ordinal != 0 {
			t.Fatalf("round %d: results %+v", round, res)
		}
		if err := a.Close(); err != nil {
			t.Fatal(err)
		}
		types := recordTypes(t, lg.bytes())
		at := slices.Index(types, TypePostsMergedIn)
		if at < 0 || at+3 > len(types) || types[at+1] != TypePost || types[at+2] != TypePost {
			t.Fatalf("round %d: the log is %v; the run is not contiguous", round, types)
		}
		e, _ := a.Index().Entry(res[1].Ordinal)
		if e.Author != 7 || !e.Flags.Has(EntryCopied) {
			t.Errorf("round %d: the first copy is %+v", round, e)
		}
	}
}

// TestAppendRunAtLength: a run decided at a length the log has moved past
// is refused whole, and so is a run one of whose records refuses, and
// neither writes a byte; the error names the record.
func TestAppendRunAtLength(t *testing.T) {
	a, lg, _, _ := newThread(t, SyncGroup)
	if _, err := a.Append(post("first")); err != nil {
		t.Fatal(err)
	}
	stale := int64(len(lg.bytes()))
	if _, err := a.Append(post("second")); err != nil {
		t.Fatal(err)
	}
	n := len(lg.bytes())
	m := mergedIn(9, 1)
	m.AtLength = stale
	if _, err := a.AppendRun([]Append{m, post("copy")}); !errors.Is(err, ErrLogMoved) {
		t.Errorf("a run at a stale length: %v, want ErrLogMoved", err)
	}
	bad := []Append{mergedIn(9, 1), post("copy"), {Type: TypeEdit, Ordinal: 40, Author: 7, Body: &EditBody{Source: "x"}}}
	if _, err := a.AppendRun(bad); !errors.Is(err, ErrNoSuchOrdinal) {
		t.Errorf("a run with an edit of no post: %v, want ErrNoSuchOrdinal", err)
	}
	if len(lg.bytes()) != n {
		t.Errorf("the refused runs wrote %d bytes", len(lg.bytes())-n)
	}
	m.AtLength = int64(n)
	res, err := a.AppendRun([]Append{m, post("copy"), {Type: TypeEdit, Ordinal: 3, Author: 7, Body: &EditBody{Source: "copy, edited"}}})
	if err != nil {
		t.Fatal(err)
	}
	if res[1].Ordinal != 3 || res[2].Revision != 2 {
		t.Errorf("results %+v", res)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}
