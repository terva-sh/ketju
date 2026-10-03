package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestRedactRefusesUnfinishedMove (TKT-01M29CXJZZ): while a move's source
// record is written and its target does not yet hold every copy at its
// original's revision, redact refuses, as drift, and writes nothing: a copy
// still to be written has no origin edge, and its text would outlive the
// redaction once serve finished the move. Once the copy and its Edit are
// written, the redaction follows the copy's origin and clears both.
func TestRedactRefusesUnfinishedMove(t *testing.T) {
	w := newRedactWorld(t)
	a, b := w.thread("Source"), w.thread("Target")
	src := w.post(a, "MOVED-SECRET")
	w.edit(a, src, &threadlog.EditBody{Source: "MOVED-SECRET, edited"})
	w.append(a, threadlog.Append{Type: threadlog.TypePostsSplitOut, Author: uint64(w.users["mod"]),
		Body: &threadlog.PostsSplitOutBody{Ordinals: []uint32{src}, TargetThreadID: uint64(b)}})
	w.append(b, threadlog.Append{Type: threadlog.TypePostsMergedIn, Author: uint64(w.users["mod"]),
		Body: &threadlog.PostsMergedInBody{SourceThreadID: uint64(a), Ordinals: []uint32{src}}})
	cp := w.copyOf(b, postRef{a, src}, "MOVED-SECRET")
	w.ready()

	was := [][]byte{w.log(a), w.log(b)}
	_, err := w.redact("", postArgs(a, src)...)
	var drift errDrift
	if !errors.As(err, &drift) {
		t.Fatalf("redacting mid-move: %v, want drift", err)
	}
	if !bytes.Equal(w.log(a), was[0]) || !bytes.Equal(w.log(b), was[1]) {
		t.Fatal("a refused redaction changed a log")
	}

	c, err := threadcache.New(w.data, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	w.cache = c
	w.edit(b, cp, &threadlog.EditBody{Source: "MOVED-SECRET, edited"})
	w.ready()
	w.mustRedact(postArgs(a, src)...)
	for _, id := range [][]byte{w.log(a), w.log(b)} {
		if bytes.Contains(id, []byte("MOVED-SECRET")) {
			t.Error("a log still holds the moved text")
		}
	}
}

// TestRedactFindsAMovedCopyAmongOthers (Terva review 2007, finding 3): a
// moved post may have another copy elsewhere; the finished move is judged
// by the copy in its own target, whichever other copies there are.
func TestRedactFindsAMovedCopyAmongOthers(t *testing.T) {
	w := newRedactWorld(t)
	a, b, c := w.thread("Source"), w.thread("Target"), w.thread("Elsewhere")
	src := w.post(a, "MANY-COPIES")
	w.edit(a, src, &threadlog.EditBody{Source: "MANY-COPIES, edited"})
	w.copyOf(c, postRef{a, src}, "MANY-COPIES")
	w.append(a, threadlog.Append{Type: threadlog.TypePostsSplitOut, Author: uint64(w.users["mod"]),
		Body: &threadlog.PostsSplitOutBody{Ordinals: []uint32{src}, TargetThreadID: uint64(b)}})
	w.append(b, threadlog.Append{Type: threadlog.TypePostsMergedIn, Author: uint64(w.users["mod"]),
		Body: &threadlog.PostsMergedInBody{SourceThreadID: uint64(a), Ordinals: []uint32{src}}})
	cp := w.copyOf(b, postRef{a, src}, "MANY-COPIES")
	w.edit(b, cp, &threadlog.EditBody{Source: "MANY-COPIES, edited"})
	// And one more in the target itself, at revision 1 against the
	// source's 2: only the move's copy, the first after the PostsMergedIn,
	// decides (Terva review 2020).
	w.copyOf(b, postRef{a, src}, "MANY-COPIES")
	w.ready()
	// Several runs, since the old fault depended on map order.
	for range 8 {
		if _, err := originEdges(t.Context(), w.data); err != nil {
			t.Fatalf("a finished move read as unfinished: %v", err)
		}
	}
	w.mustRedact(postArgs(a, src)...)
	for _, id := range [][]byte{w.log(a), w.log(b), w.log(c)} {
		if bytes.Contains(id, []byte("MANY-COPIES")) {
			t.Error("a log still holds the text")
		}
	}
}
