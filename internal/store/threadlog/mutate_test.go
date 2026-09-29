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

// A moderator's Delete (by_moderator, TKT-01M29CRJ5J) sets hidden-by-mod and
// not deleted, an ordinary one the reverse, a Restore clears either, and a
// replay of the log builds the index the actor kept.
func TestModeratorDeleteSetsHiddenByMod(t *testing.T) {
	a, lg, _, _ := newThread(t, SyncNever)
	mustAppend(t, a, post("one"))
	mustAppend(t, a, post("two"))
	mustAppend(t, a, Append{Type: TypeDelete, Author: 7, Ordinal: 1, Body: &DeleteBody{Reason: "spam", ByModerator: true}})
	mustAppend(t, a, Append{Type: TypeDelete, Author: 42, Ordinal: 2, Body: &DeleteBody{}})
	e1, _ := a.Index().Entry(1)
	e2, _ := a.Index().Entry(2)
	if e1.Flags&(EntryDeleted|EntryHiddenByMod) != EntryHiddenByMod || e1.Author != 42 {
		t.Errorf("after a moderator's delete, entry %+v: want hidden-by-mod only, author 42", e1)
	}
	if e2.Flags&(EntryDeleted|EntryHiddenByMod) != EntryDeleted {
		t.Errorf("after an ordinary delete, entry %+v: want deleted only", e2)
	}
	if got := replayed(t, lg.bytes()); !reflect.DeepEqual(got.entries, a.Index().entries) {
		t.Errorf("replay disagrees with the actor:\n replay %+v\n  actor %+v", got.entries, a.Index().entries)
	}
	mustAppend(t, a, Append{Type: TypeRestore, Author: 7, Ordinal: 1, Body: &RestoreBody{},
		RequireAny: EntryDeleted | EntryHiddenByMod, Refuse: EntryMoved})
	if e, _ := a.Index().Entry(1); !e.Flags.Visible() {
		t.Errorf("after a restore, entry %+v is not visible", e)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}

// An auto-hide Delete (auto, TKT-01M29CP2PJ) sets hidden-by-mod and
// auto-hidden, a moderator's Delete of the same post would clear auto-hidden
// and keep it hidden, a Restore clears all three, and a replay of the log
// builds the index the actor kept, bit 6 included.
func TestAutoDeleteSetsAutoHidden(t *testing.T) {
	a, lg, _, _ := newThread(t, SyncNever)
	mustAppend(t, a, post("one"))
	mustAppend(t, a, post("two"))
	mustAppend(t, a, Append{Type: TypeDelete, Author: 0, Ordinal: 1, Body: &DeleteBody{Reason: "hidden pending review", Auto: true}})
	mustAppend(t, a, Append{Type: TypeDelete, Author: 0, Ordinal: 2, Body: &DeleteBody{Auto: true}})
	e1, _ := a.Index().Entry(1)
	if e1.Flags&(EntryDeleted|EntryHiddenByMod|EntryAutoHidden) != EntryHiddenByMod|EntryAutoHidden || e1.Author != 42 {
		t.Errorf("after an auto-hide, entry %+v: want hidden-by-mod and auto-hidden, author 42", e1)
	}
	// The moderator confirms the hide on #2: still hidden, no longer pending.
	mustAppend(t, a, Append{Type: TypeDelete, Author: 7, Ordinal: 2, Body: &DeleteBody{ByModerator: true},
		Require: EntryAutoHidden})
	if e2, _ := a.Index().Entry(2); e2.Flags&(EntryHiddenByMod|EntryAutoHidden) != EntryHiddenByMod {
		t.Errorf("after a moderator's delete of an auto-hidden post, entry %+v: want hidden-by-mod only", e2)
	}
	if got := replayed(t, lg.bytes()); !reflect.DeepEqual(got.entries, a.Index().entries) {
		t.Errorf("replay disagrees with the actor:\n replay %+v\n  actor %+v", got.entries, a.Index().entries)
	}
	mustAppend(t, a, Append{Type: TypeRestore, Author: 7, Ordinal: 1, Body: &RestoreBody{},
		RequireAny: EntryDeleted | EntryHiddenByMod, Refuse: EntryMoved})
	if e, _ := a.Index().Entry(1); !e.Flags.Visible() || e.Flags.Has(EntryAutoHidden) {
		t.Errorf("after a restore, entry %+v: want visible with auto-hidden clear", e)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestRestoreRequireAny: a Restore under RequireAny deleted|hidden takes a
// post that is either and refuses one that is neither, and Refuse still
// refuses a moved post, writing nothing each time it refuses.
func TestRestoreRequireAny(t *testing.T) {
	restore := Append{Type: TypeRestore, Ordinal: 1, Body: &RestoreBody{},
		RequireAny: EntryDeleted | EntryHiddenByMod, Refuse: EntryMoved}
	for _, c := range []struct {
		name  string
		flags EntryFlags
		ok    bool
	}{
		{"visible", 0, false},
		{"edited and visible", EntryEdited, false},
		{"deleted", EntryDeleted, true},
		{"hidden", EntryHiddenByMod, true},
		{"deleted and hidden", EntryDeleted | EntryHiddenByMod, true},
		{"moved", EntryMoved, false},
		{"hidden and moved", EntryHiddenByMod | EntryMoved, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, lg, _, _ := newThread(t, SyncNever)
			defer a.Close()
			mustAppend(t, a, post("one"))
			a.ix.mu.Lock()
			a.ix.entries[0].Flags |= c.flags
			a.ix.mu.Unlock()
			size := len(lg.bytes())
			_, err := a.Append(restore)
			if (err == nil) != c.ok {
				t.Fatalf("err %v, want ok=%v", err, c.ok)
			}
			if !c.ok {
				if !errors.Is(err, ErrPostState) {
					t.Errorf("err %v, want ErrPostState", err)
				}
				if len(lg.bytes()) != size {
					t.Errorf("a refused restore grew the log from %d to %d bytes", size, len(lg.bytes()))
				}
			}
		})
	}
}

// A Restore clears hidden-by-mod as well as deleted (settled in grooming:
// a moderator's Restore clears it, docs/08), from an entry with both set.
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
	// A moderator's Restore, and an author's, as the service writes them
	// (TKT-01M29CRJ5J).
	modRestore := Append{Type: TypeRestore, Ordinal: 1, RequireAny: EntryDeleted | EntryHiddenByMod, Refuse: EntryMoved}
	ownRestore := Append{Type: TypeRestore, Ordinal: 1, Require: EntryDeleted, Refuse: EntryHiddenByMod | EntryMoved}
	hide := staged{req: request{a: Append{Body: &DeleteBody{ByModerator: true}}}, header: FrameHeader{Type: TypeDelete, Ordinal: 1}}
	del := staged{header: FrameHeader{Type: TypeDelete, Ordinal: 1}}
	undo := staged{header: FrameHeader{Type: TypeRestore, Ordinal: 1}}
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
		// A staged moderator's Delete is read from its body as a hide
		// (TKT-01M29CRJ5J).
		{"moderator's restore, nothing ahead", modRestore, nil, false},
		{"moderator's restore behind a hide", modRestore, []staged{hide}, true},
		{"moderator's restore behind a delete", modRestore, []staged{del}, true},
		{"moderator's restore behind a hide and a restore", modRestore, []staged{hide, undo}, false},
		{"author's restore behind a hide", ownRestore, []staged{hide}, false},
		{"author's restore behind a delete", ownRestore, []staged{del}, true},
		{"author's restore behind a delete, a restore and a hide", ownRestore, []staged{del, undo, hide}, false},
		{"refusing hidden, behind a hide", Append{Type: TypeEdit, Ordinal: 1, Refuse: EntryHiddenByMod}, []staged{hide}, false},
		{"refusing deleted, behind a hide", edit, []staged{hide}, true},
		// A staged Edit sets the edited flag, as Apply will (review 1123).
		{"requiring edited, behind an edit", Append{Type: TypeDelete, Ordinal: 1, Require: EntryEdited}, stage(TypeEdit), true},
		{"requiring edited, nothing ahead", Append{Type: TypeDelete, Ordinal: 1, Require: EntryEdited}, nil, false},
		{"refusing edited, behind an edit", Append{Type: TypeDelete, Ordinal: 1, Refuse: EntryEdited}, stage(TypeEdit), false},
		// A staged grace Edit does not, as Apply will not (TKT-01M29CKRHS):
		// the body it is staged with is read, not only its type.
		{"refusing edited, behind a grace edit", Append{Type: TypeEdit, Ordinal: 1, Refuse: EntryEdited},
			[]staged{{req: request{a: Append{Body: &EditBody{Grace: true}}}, header: FrameHeader{Type: TypeEdit, Ordinal: 1}}}, true},
		{"refusing edited, behind a grace edit and a visible one", Append{Type: TypeEdit, Ordinal: 1, Refuse: EntryEdited},
			[]staged{
				{req: request{a: Append{Body: &EditBody{Grace: true}}}, header: FrameHeader{Type: TypeEdit, Ordinal: 1}},
				{req: request{a: Append{Body: &EditBody{}}}, header: FrameHeader{Type: TypeEdit, Ordinal: 1}},
			}, false},
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
//
// The table reads an Edit's body (TKT-01M29CKRHS), so an Edit is tried with
// and without grace, and the body the table is given is the one an append
// holds, both as a pointer and as a value, while Apply decodes it from bytes.
func TestPostFlagEffectIsWhatApplyDoes(t *testing.T) {
	type try struct {
		name string
		ty   RecordType
		body any
	}
	var tries []try
	for ty := range catalogue {
		if ty == TypePost {
			continue // creates an entry rather than changing one
		}
		body, _ := NewBody(ty)
		tries = append(tries, try{ty.String(), ty, body})
	}
	tries = append(tries,
		try{"grace Edit", TypeEdit, &EditBody{Source: "x", Grace: true}},
		try{"grace Edit by value", TypeEdit, EditBody{Source: "x", Grace: true}},
		try{"Edit by value", TypeEdit, EditBody{Source: "x"}},
		try{"moderator's Delete", TypeDelete, &DeleteBody{ByModerator: true}},
		try{"moderator's Delete by value", TypeDelete, DeleteBody{ByModerator: true}},
		try{"Delete by value", TypeDelete, DeleteBody{Reason: "x"}},
		try{"auto-hide Delete", TypeDelete, &DeleteBody{Auto: true}},
		try{"auto-hide Delete by value", TypeDelete, DeleteBody{Auto: true}},
		try{"auto and by_moderator Delete", TypeDelete, &DeleteBody{Auto: true, ByModerator: true}})
	for _, c := range tries {
		raw, err := EncodeBody(EncodingCBOR, c.body)
		if err != nil {
			t.Fatal(err)
		}
		for start := range EntryFlagsKnown + 1 {
			if start&^EntryFlagsKnown != 0 {
				continue
			}
			ix := &Index{entries: []IndexEntry{{Revision: 1, Flags: start}}}
			h := FrameHeader{Type: c.ty, Encoding: EncodingCBOR, Ordinal: 1, Revision: 2}
			if err := ix.Apply(h, raw, 0, 0); err != nil {
				t.Fatalf("%s from %#x: %v", c.name, start, err)
			}
			set, clear := postFlagEffect(c.ty, c.body)
			if got, want := ix.entries[0].Flags, (start|set)&^clear; got != want {
				t.Errorf("%s from %#x: Apply left %#x, postFlagEffect says %#x", c.name, start, got, want)
			}
		}
	}
}

// TestGraceEditFlags: a grace Edit leaves the edited flag as it found it,
// clear on a post only ever grace-edited and set on one already edited, and
// any other Edit sets it. Nothing clears it (TKT-01M29CKRHS).
func TestGraceEditFlags(t *testing.T) {
	grace := &EditBody{Source: "x", Grace: true}
	for _, c := range []struct {
		name  string
		start EntryFlags
		body  *EditBody
		want  EntryFlags
	}{
		{"grace on the original", 0, grace, 0},
		{"grace on an edited post", EntryEdited, grace, EntryEdited},
		{"visible on the original", 0, &EditBody{Source: "x"}, EntryEdited},
		{"visible on an edited post", EntryEdited, &EditBody{Source: "x"}, EntryEdited},
		{"grace keeps other flags", EntryHasAttachments, grace, EntryHasAttachments},
	} {
		raw, err := EncodeBody(EncodingCBOR, c.body)
		if err != nil {
			t.Fatal(err)
		}
		ix := &Index{entries: []IndexEntry{{Revision: 1, Flags: c.start}}}
		if err := ix.Apply(FrameHeader{Type: TypeEdit, Encoding: EncodingCBOR, Ordinal: 1, Revision: 2}, raw, 7, 9); err != nil {
			t.Fatal(err)
		}
		e := ix.entries[0]
		if e.Flags != c.want || e.Revision != 2 || e.Offset != 7 {
			t.Errorf("%s: entry %+v, want flags %#x at revision 2, offset 7", c.name, e, c.want)
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
