package threadlog

import (
	"bytes"
	"errors"
	"reflect"
	"sync"
	"testing"
)

// Tests for TKT-01M29CA9BP: Edit, Delete and Restore as appends, and the
// preconditions that keep an Edit off a deleted post.

// mustAppend appends a and fails the test on an error.
func mustAppend(t *testing.T, a *Actor, req Append) Result {
	t.Helper()
	res, err := a.Append(req)
	if err != nil {
		t.Fatalf("appending %s: %v", req.Type, err)
	}
	return res
}

// frameAt finds the frame starting at offset in log.
func frameAt(t *testing.T, log []byte, offset int64) ScanRecord {
	t.Helper()
	recs, err := scanAll(t, log)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if r.Offset == offset {
			return r
		}
	}
	t.Fatalf("no frame starts at %d", offset)
	return ScanRecord{}
}

// replayed is the index a full replay of log builds, with no sidecar.
func replayed(t *testing.T, log []byte) *Index {
	t.Helper()
	rec, err := Open(bytes.NewReader(log), int64(len(log)), nil)
	if err != nil {
		t.Fatal(err)
	}
	return rec.Index
}

// AC1: an edit appends a record, increments the revision, and repoints the
// index entry at the new revision. An Edit whose body carries no reply_to
// keeps the post's (settled in grooming), and replay agrees with the actor.
func TestEditRepointsTheEntryAtTheNewRevision(t *testing.T) {
	a, lg, _, _ := newThread(t, SyncNever)
	mustAppend(t, a, post("first"))
	mustAppend(t, a, Append{Type: TypePost, Author: 42, Body: &PostBody{Source: "v1", ReplyTo: 1}})
	before := a.LogLength()
	res := mustAppend(t, a, Append{Type: TypeEdit, Author: 42, Ordinal: 2, Body: &EditBody{Source: "v2"}})
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if res.Ordinal != 2 || res.Revision != 2 || res.LogLength <= before {
		t.Fatalf("edit result %+v (log was %d)", res, before)
	}

	e, err := a.Index().Entry(2)
	if err != nil {
		t.Fatal(err)
	}
	f := frameAt(t, lg.bytes(), before)
	if f.Header.Type != TypeEdit || e.Offset != uint64(before) || e.FrameLength != uint32(f.Size) {
		t.Errorf("entry %+v does not point at the edit frame (%s at %d, %d bytes)", e, f.Header.Type, f.Offset, f.Size)
	}
	if e.Revision != 2 || !e.Flags.Has(EntryEdited) {
		t.Errorf("entry %+v: want revision 2 and the edited flag", e)
	}
	if e.ReplyTo != 1 {
		t.Errorf("an edit with no reply_to changed the post's to %d, want it kept at 1", e.ReplyTo)
	}
	p, err := NewReader(bytes.NewReader(lg.bytes()), a.Index()).One(2)
	if err != nil {
		t.Fatal(err)
	}
	if p.Source != "v2" || p.Revision != 2 || p.ReplyTo != 1 {
		t.Errorf("read back %+v", p)
	}
	if got := replayed(t, lg.bytes()); !reflect.DeepEqual(got.entries, a.Index().entries) {
		t.Errorf("replay disagrees with the actor:\n replay %+v\n  actor %+v", got.entries, a.Index().entries)
	}
}

// An Edit that does name a reply_to still sets it: keeping the old value is
// only for an absent key.
func TestEditWithAReplyToSetsIt(t *testing.T) {
	a, _, _, _ := newThread(t, SyncNever)
	mustAppend(t, a, post("one"))
	mustAppend(t, a, post("two"))
	mustAppend(t, a, Append{Type: TypePost, Author: 42, Body: &PostBody{Source: "three", ReplyTo: 1}})
	mustAppend(t, a, Append{Type: TypeEdit, Author: 42, Ordinal: 3, Body: &EditBody{Source: "three'", ReplyTo: 2}})
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if e, _ := a.Index().Entry(3); e.ReplyTo != 2 {
		t.Errorf("reply_to %d, want 2", e.ReplyTo)
	}
}

// AC2: a delete leaves the original Post bytes intact and sets the deleted
// flag in the index.
func TestDeleteLeavesThePostBytesAndSetsTheFlag(t *testing.T) {
	a, lg, _, _ := newThread(t, SyncNever)
	mustAppend(t, a, post("keep me on disk"))
	e0, err := a.Index().Entry(1)
	if err != nil {
		t.Fatal(err)
	}
	orig := append([]byte(nil), lg.bytes()[e0.Offset:e0.Offset+uint64(e0.FrameLength)]...)
	mustAppend(t, a, Append{Type: TypeDelete, Author: 42, Ordinal: 1, Body: &DeleteBody{Reason: "mine"}, Refuse: EntryDeleted})
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	log := lg.bytes()
	f := frameAt(t, log, int64(e0.Offset))
	if f.Err != nil || f.Header.Type != TypePost {
		t.Fatalf("the Post frame at %d is now %s (err %v)", e0.Offset, f.Header.Type, f.Err)
	}
	if got := log[f.Offset : f.Offset+int64(f.Size)]; !bytes.Equal(got, orig) {
		t.Error("the Post frame's bytes changed after the delete")
	}
	e, err := a.Index().Entry(1)
	if err != nil {
		t.Fatal(err)
	}
	if !e.Flags.Has(EntryDeleted) || e.Offset != e0.Offset || e.Revision != 1 {
		t.Errorf("entry after delete %+v, before %+v: want the deleted flag and nothing else moved", e, e0)
	}
	p, err := NewReader(bytes.NewReader(log), a.Index()).One(1)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Tombstone || p.Source != "" {
		t.Errorf("a deleted post read back as %+v, want a tombstone", p)
	}
	hidden, err := NewReader(bytes.NewReader(log), a.Index()).Hidden(1)
	if err != nil || hidden.Source != "keep me on disk" {
		t.Errorf("the deleted body is not readable for history: %+v, %v", hidden, err)
	}
}

// AC3: a restore clears the flag and the post returns at its current
// revision: edit, delete, restore reads revision 2.
func TestRestoreReturnsThePostAtItsCurrentRevision(t *testing.T) {
	a, lg, _, _ := newThread(t, SyncNever)
	mustAppend(t, a, post("v1"))
	mustAppend(t, a, Append{Type: TypeEdit, Author: 42, Ordinal: 1, Body: &EditBody{Source: "v2"}, Refuse: EntryDeleted})
	mustAppend(t, a, Append{Type: TypeDelete, Author: 42, Ordinal: 1, Body: &DeleteBody{}, Refuse: EntryDeleted})
	mustAppend(t, a, Append{Type: TypeRestore, Author: 42, Ordinal: 1, Body: &RestoreBody{}, Require: EntryDeleted})
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	p, err := NewReader(bytes.NewReader(lg.bytes()), a.Index()).One(1)
	if err != nil {
		t.Fatal(err)
	}
	if p.Tombstone || p.Flags.Has(EntryDeleted) || p.Source != "v2" || p.Revision != 2 {
		t.Errorf("restored post %+v, want revision 2 visible", p)
	}
	if got := replayed(t, lg.bytes()); !reflect.DeepEqual(got.entries, a.Index().entries) {
		t.Errorf("replay disagrees with the actor:\n replay %+v\n  actor %+v", got.entries, a.Index().entries)
	}
}

// A Restore clears hidden-by-mod as well as deleted (settled in grooming:
// a moderator's Restore clears it, docs/08). Nothing in this build sets the
// flag through a record yet, so it is set on the entry directly.
func TestRestoreClearsHiddenByMod(t *testing.T) {
	ix := &Index{}
	ix.entries = []IndexEntry{{Revision: 1, Flags: EntryDeleted | EntryHiddenByMod | EntryEdited}}
	body, err := EncodeBody(EncodingCBOR, &RestoreBody{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ix.Apply(FrameHeader{Type: TypeRestore, Encoding: EncodingCBOR, Ordinal: 1}, body, 0, 0); err != nil {
		t.Fatal(err)
	}
	if got := ix.entries[0].Flags; got != EntryEdited {
		t.Errorf("flags after restore %#x, want only edited (%#x)", got, EntryEdited)
	}
}

// AC4: editing a deleted post is refused rather than silently reviving it,
// and every precondition failure writes nothing.
func TestPreconditionsRefuseAndWriteNothing(t *testing.T) {
	a, lg, _, _ := newThread(t, SyncNever)
	mustAppend(t, a, post("one"))
	mustAppend(t, a, Append{Type: TypeDelete, Author: 42, Ordinal: 1, Body: &DeleteBody{}, Refuse: EntryDeleted})
	mustAppend(t, a, post("two"))

	for _, c := range []struct {
		name string
		req  Append
	}{
		{"edit of a deleted post", Append{Type: TypeEdit, Ordinal: 1, Body: &EditBody{Source: "revived?"}, Refuse: EntryDeleted}},
		{"delete of a deleted post", Append{Type: TypeDelete, Ordinal: 1, Body: &DeleteBody{}, Refuse: EntryDeleted}},
		{"restore of a visible post", Append{Type: TypeRestore, Ordinal: 2, Body: &RestoreBody{}, Require: EntryDeleted}},
	} {
		size := len(lg.bytes())
		if _, err := a.Append(c.req); !errors.Is(err, ErrPostState) {
			t.Errorf("%s: err %v, want ErrPostState", c.name, err)
		}
		if len(lg.bytes()) != size {
			t.Errorf("%s: the log grew from %d to %d bytes", c.name, size, len(lg.bytes()))
		}
	}
	if _, err := a.Append(Append{Type: TypeThreadLocked, Body: &ThreadLockedBody{}, Refuse: EntryDeleted}); !errors.Is(err, ErrMalformed) {
		t.Errorf("a precondition with no ordinal: %v, want ErrMalformed", err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if e, _ := a.Index().Entry(1); e.Revision != 1 {
		t.Errorf("a refused edit moved the revision to %d", e.Revision)
	}
}

// The check sees records staged ahead of it in the same batch, folded by the
// same table Apply uses. Without that, a Delete and an Edit that the actor
// drains into one batch would both pass against the index as it stood.
func TestPreconditionsSeeTheBatchAhead(t *testing.T) {
	a, _, _, _ := newThread(t, SyncNever)
	mustAppend(t, a, post("one"))
	defer a.Close()

	stage := func(types ...RecordType) []staged {
		var s []staged
		for _, ty := range types {
			s = append(s, staged{header: FrameHeader{Type: ty, Ordinal: 1}})
		}
		return s
	}
	edit := Append{Type: TypeEdit, Ordinal: 1, Refuse: EntryDeleted}
	restore := Append{Type: TypeRestore, Ordinal: 1, Require: EntryDeleted}
	for _, c := range []struct {
		name  string
		req   Append
		ahead []staged
		ok    bool
	}{
		{"edit, nothing ahead", edit, nil, true},
		{"edit behind a delete", edit, stage(TypeDelete), false},
		{"edit behind a delete and a restore", edit, stage(TypeDelete, TypeRestore), true},
		{"edit behind an edit", edit, stage(TypeEdit), true},
		{"restore behind a delete", restore, stage(TypeDelete), true},
		{"restore behind a delete and a restore", restore, stage(TypeDelete, TypeRestore), false},
		{"edit behind another post's delete", edit, []staged{{header: FrameHeader{Type: TypeDelete, Ordinal: 2}}}, true},
		// A staged Edit sets the edited flag, as Apply will (review 1123).
		{"requiring edited, behind an edit", Append{Type: TypeDelete, Ordinal: 1, Require: EntryEdited}, stage(TypeEdit), true},
		{"requiring edited, nothing ahead", Append{Type: TypeDelete, Ordinal: 1, Require: EntryEdited}, nil, false},
		{"refusing edited, behind an edit", Append{Type: TypeDelete, Ordinal: 1, Refuse: EntryEdited}, stage(TypeEdit), false},
	} {
		err := a.checkState(c.req, c.ahead)
		if (err == nil) != c.ok {
			t.Errorf("%s: err %v, want ok=%v", c.name, err, c.ok)
		}
		if err != nil && !errors.Is(err, ErrPostState) {
			t.Errorf("%s: err %v, want ErrPostState", c.name, err)
		}
	}
}

// TestPostFlagEffectIsWhatApplyDoes holds the precondition fold to Apply:
// for every catalogued type that names an existing post, the flags Apply
// leaves on the entry are the flags postFlagEffect says, from every starting
// state. A flag change added to Apply and not to the table fails here, which
// is how a staged Edit's edited flag went missing (review 1123 on PR #95).
func TestPostFlagEffectIsWhatApplyDoes(t *testing.T) {
	for ty := range catalogue {
		if ty == TypePost {
			continue // creates an entry rather than changing one
		}
		body, _ := NewBody(ty)
		raw, err := EncodeBody(EncodingCBOR, body)
		if err != nil {
			t.Fatal(err)
		}
		for start := range EntryFlagsKnown + 1 {
			if start&^EntryFlagsKnown != 0 {
				continue
			}
			ix := &Index{entries: []IndexEntry{{Revision: 1, Flags: start}}}
			h := FrameHeader{Type: ty, Encoding: EncodingCBOR, Ordinal: 1, Revision: 2}
			if err := ix.Apply(h, raw, 0, 0); err != nil {
				t.Fatalf("%s from %#x: %v", ty, start, err)
			}
			set, clear := postFlagEffect(ty)
			if got, want := ix.entries[0].Flags, (start|set)&^clear; got != want {
				t.Errorf("%s from %#x: Apply left %#x, postFlagEffect says %#x", ty, start, got, want)
			}
		}
	}
}

// AC4 under concurrency: Delete, Edit and Restore fired at one post from
// separate goroutines, 1000 rounds, under -race. Whatever order the actor
// takes them in, no Edit frame follows a Delete for that ordinal without a
// Restore in between, and the index the actor kept is the one a replay
// builds.
func TestEditNeverFollowsADeleteUnderConcurrency(t *testing.T) {
	a, lg, _, _ := newThread(t, SyncNever)
	mustAppend(t, a, post("contested"))

	const rounds = 1000
	var wg sync.WaitGroup
	var mu sync.Mutex
	unexpected := []error{}
	fire := func(req Append) {
		defer wg.Done()
		if _, err := a.Append(req); err != nil && !errors.Is(err, ErrPostState) {
			mu.Lock()
			unexpected = append(unexpected, err)
			mu.Unlock()
		}
	}
	for range rounds {
		wg.Add(3)
		go fire(Append{Type: TypeDelete, Author: 42, Ordinal: 1, Body: &DeleteBody{}, Refuse: EntryDeleted})
		go fire(Append{Type: TypeEdit, Author: 42, Ordinal: 1, Body: &EditBody{Source: "again"}, Refuse: EntryDeleted})
		go fire(Append{Type: TypeRestore, Author: 42, Ordinal: 1, Body: &RestoreBody{}, Require: EntryDeleted})
	}
	wg.Wait()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if len(unexpected) > 0 {
		t.Fatalf("%d appends failed other than by precondition, first: %v", len(unexpected), unexpected[0])
	}

	recs, err := scanAll(t, lg.bytes())
	if err != nil {
		t.Fatal(err)
	}
	deleted := false
	var edits, deletes, restores int
	for _, r := range recs {
		if r.Header.Ordinal != 1 {
			continue
		}
		switch r.Header.Type {
		case TypeEdit:
			edits++
			if deleted {
				t.Fatalf("an Edit at offset %d follows a Delete with no Restore between", r.Offset)
			}
		case TypeDelete:
			deletes++
			if deleted {
				t.Fatalf("a Delete at offset %d follows a Delete with no Restore between", r.Offset)
			}
			deleted = true
		case TypeRestore:
			restores++
			if !deleted {
				t.Fatalf("a Restore at offset %d restores a post that was not deleted", r.Offset)
			}
			deleted = false
		}
	}
	if edits == 0 || deletes == 0 || restores == 0 {
		t.Errorf("the race was not exercised: %d edits, %d deletes, %d restores", edits, deletes, restores)
	}
	e, _ := a.Index().Entry(1)
	if e.Flags.Has(EntryDeleted) != deleted || int(e.Revision) != edits+1 {
		t.Errorf("entry %+v, the log says deleted=%v after %d edits", e, deleted, edits)
	}
	if got := replayed(t, lg.bytes()); !reflect.DeepEqual(got.entries, a.Index().entries) {
		t.Error("replay disagrees with the actor after the race")
	}
}
