package threadlog

import (
	"bytes"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// Tests for what a suspension's hide and exact restore need of the log
// (TKT-01M29CP2QK): the Delete's optional suspension key, the AtLength
// precondition, and the walk of a thread's removals.

// The key round-trips through both encodings, is absent when zero, and a
// body written before it existed decodes with it zero, as by_moderator's
// did.
func TestDeleteSuspensionKeyRoundTrips(t *testing.T) {
	in := DeleteBody{Reason: "suspension", ByModerator: true, Suspension: 1234}
	for _, enc := range []BodyEncoding{EncodingCBOR, EncodingJSON} {
		raw, err := EncodeBody(enc, &in)
		if err != nil {
			t.Fatal(err)
		}
		v, err := DecodeBody(TypeDelete, enc, raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := *v.(*DeleteBody); got != in {
			t.Errorf("encoding %d: round trip gave %+v, want %+v", enc, got, in)
		}
	}

	raw, err := EncodeBody(EncodingCBOR, &DeleteBody{Reason: "spam", ByModerator: true})
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]any
	if err := cbor.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	if _, ok := keys["suspension"]; ok {
		t.Errorf("a Delete with no suspension wrote the key: %v", keys)
	}

	// A body as a build before the key wrote it.
	old, err := cbor.Marshal(map[string]any{"reason": "spam", "by_moderator": true})
	if err != nil {
		t.Fatal(err)
	}
	v, err := DecodeBody(TypeDelete, EncodingCBOR, old)
	if err != nil {
		t.Fatal(err)
	}
	if got := *v.(*DeleteBody); got != (DeleteBody{Reason: "spam", ByModerator: true}) {
		t.Errorf("an older body decoded as %+v", got)
	}
	js, err := json.Marshal(map[string]any{"reason": "spam"})
	if err != nil {
		t.Fatal(err)
	}
	if v, err := DecodeBody(TypeDelete, EncodingJSON, js); err != nil || v.(*DeleteBody).Suspension != 0 {
		t.Errorf("an older JSON body decoded as %+v (%v)", v, err)
	}
}

// The key changes nothing the index does: a Delete carrying it hides the
// post exactly as a moderator's Delete without it does, and replay agrees.
func TestDeleteSuspensionKeyLeavesTheIndexAlone(t *testing.T) {
	a, lg, _, _ := newThread(t, SyncNever)
	mustAppend(t, a, post("one"))
	mustAppend(t, a, post("two"))
	mustAppend(t, a, Append{Type: TypeDelete, Author: 7, Ordinal: 1, Body: &DeleteBody{ByModerator: true, Suspension: 9}})
	mustAppend(t, a, Append{Type: TypeDelete, Author: 7, Ordinal: 2, Body: &DeleteBody{ByModerator: true}})
	e1, _ := a.Index().Entry(1)
	e2, _ := a.Index().Entry(2)
	if e1.Flags != e2.Flags || !e1.Flags.Has(EntryHiddenByMod) {
		t.Errorf("flags %#x with the key and %#x without: want the same, hidden-by-mod", e1.Flags, e2.Flags)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if got := replayed(t, lg.bytes()); got.entries[0] != a.Index().entries[0] {
		t.Errorf("replay %+v, actor %+v", got.entries[0], a.Index().entries[0])
	}
}

// AtLength admits a record only at the length it names: the current length
// is written, a stale one is ErrLogMoved and writes nothing, and of many
// records racing for one length exactly one is written, whether or not
// they share a batch.
func TestAppendAtLength(t *testing.T) {
	a, _, _, _ := newThread(t, SyncGroup)
	mustAppend(t, a, post("one"))
	stale := a.LogLength()
	mustAppend(t, a, post("two"))
	at := a.LogLength()

	if _, err := a.Append(Append{Type: TypeDelete, Author: 7, Ordinal: 1, Body: &DeleteBody{}, AtLength: stale}); !errors.Is(err, ErrLogMoved) {
		t.Fatalf("a stale length: %v, want ErrLogMoved", err)
	}
	if a.LogLength() != at {
		t.Fatalf("a refused append moved the log from %d to %d", at, a.LogLength())
	}
	if e, _ := a.Index().Entry(1); !e.Flags.Visible() {
		t.Fatalf("a refused Delete changed the entry: %+v", e)
	}
	res := mustAppend(t, a, Append{Type: TypeDelete, Author: 7, Ordinal: 1, Body: &DeleteBody{}, AtLength: at})
	if res.LogLength <= at {
		t.Fatalf("the admitted append reported length %d, was %d", res.LogLength, at)
	}

	const racers = 32
	at = a.LogLength()
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		ok   int
		errs []error
	)
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := a.Append(Append{Type: TypePost, Author: 42, Body: &PostBody{Source: "race"}, AtLength: at})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				ok++
			} else {
				errs = append(errs, err)
			}
		}()
	}
	wg.Wait()
	if ok != 1 {
		t.Fatalf("%d of %d appends at one length were written, want 1", ok, racers)
	}
	for _, err := range errs {
		if !errors.Is(err, ErrLogMoved) {
			t.Errorf("a losing racer: %v, want ErrLogMoved", err)
		}
	}
	if n := a.Index().Len(); n != 3 {
		t.Errorf("the index holds %d posts, want 3", n)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}

// Removals reports every Delete with its body and every Restore, in log
// order, from any frame boundary, and skips everything else.
func TestRemovals(t *testing.T) {
	a, lg, _, _ := newThread(t, SyncNever)
	mustAppend(t, a, post("one"))
	mustAppend(t, a, post("two"))
	mustAppend(t, a, Append{Type: TypeDelete, Author: 7, Ordinal: 1, Body: &DeleteBody{ByModerator: true, Suspension: 5}})
	mid := a.LogLength()
	mustAppend(t, a, Append{Type: TypeEdit, Author: 42, Ordinal: 2, Body: &EditBody{Source: "two, again"}})
	mustAppend(t, a, Append{Type: TypeRestore, Author: 7, Ordinal: 1, Body: &RestoreBody{}})
	mustAppend(t, a, Append{Type: TypeDelete, Author: 42, Ordinal: 2, Body: &DeleteBody{Reason: "mine"}})
	end := a.LogLength()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	r := NewReader(bytes.NewReader(lg.bytes()), a.Index())

	type seen struct {
		ordinal uint32
		del     *DeleteBody
	}
	walk := func(from int64) []seen {
		var out []seen
		if err := r.Removals(from, end, func(o uint32, d *DeleteBody) { out = append(out, seen{o, d}) }); err != nil {
			t.Fatal(err)
		}
		return out
	}
	all := walk(FileHeaderSize)
	if len(all) != 3 || all[0].ordinal != 1 || all[0].del == nil || all[0].del.Suspension != 5 ||
		all[1].ordinal != 1 || all[1].del != nil ||
		all[2].ordinal != 2 || all[2].del == nil || all[2].del.Reason != "mine" {
		t.Fatalf("removals %+v", all)
	}
	if rest := walk(mid); len(rest) != 2 || rest[0].del != nil {
		t.Errorf("from the middle: %+v, want the Restore then the Delete", rest)
	}
	if none := walk(end); len(none) != 0 {
		t.Errorf("from the end: %+v", none)
	}

	// A damaged frame stops the walk with its error.
	b := lg.bytes()
	b[len(b)-2] ^= 0xff
	bad := NewReader(bytes.NewReader(b), a.Index())
	if err := bad.Removals(FileHeaderSize, end, func(uint32, *DeleteBody) {}); err == nil {
		t.Error("a damaged frame was walked over")
	}
}

// Restored finds one Restore by its post, its actor and its millisecond, and
// nothing that differs in any of them (TKT-01M29CRJ4E).
func TestRestored(t *testing.T) {
	a, lg, _, _ := newThread(t, SyncNever)
	mustAppend(t, a, post("one"))
	mustAppend(t, a, post("two"))
	mustAppend(t, a, Append{Type: TypeDelete, Author: 7, Ordinal: 1, TimeMS: 900, Body: &DeleteBody{ByModerator: true}})
	mustAppend(t, a, Append{Type: TypeRestore, Author: 8, Ordinal: 1, TimeMS: 1000, Body: &RestoreBody{}})
	mustAppend(t, a, Append{Type: TypeDelete, Author: 7, Ordinal: 1, TimeMS: 1100, Body: &DeleteBody{ByModerator: true}})
	end := a.LogLength()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	r := NewReader(bytes.NewReader(lg.bytes()), a.Index())
	for _, c := range []struct {
		name    string
		ordinal uint32
		actor   uint64
		ms      int64
		want    bool
	}{
		{"that restore", 1, 8, 1000, true},
		{"another moderator", 1, 7, 1000, false},
		{"another time", 1, 8, 1001, false},
		{"another post", 2, 8, 1000, false},
		{"a Delete's actor and time", 1, 7, 900, false},
	} {
		got, err := r.Restored(FileHeaderSize, end, c.ordinal, c.actor, c.ms)
		if err != nil || got != c.want {
			t.Errorf("%s: %t, %v; want %t", c.name, got, err, c.want)
		}
	}

	b := lg.bytes()
	b[len(b)-2] ^= 0xff
	bad := NewReader(bytes.NewReader(b), a.Index())
	if got, err := bad.Restored(FileHeaderSize, end, 1, 9, 1); err == nil || got {
		t.Errorf("a damaged frame: %t, %v", got, err)
	}
}
