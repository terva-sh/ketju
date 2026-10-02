//go:build !race

package projection

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// catchUpBound is AC4's bound: catch-up over ten thousand threads with no
// drift, on the CI runner. The race detector slows everything several times
// over and says nothing about speed, so this file is built without it and CI
// runs it in a step of its own (settled in grooming, 2026-09-24).
const catchUpBound = 2 * time.Second

func TestCatchUpTenThousandUnderBound(t *testing.T) {
	f := newFixture(t)
	f.tenThousand(10_000)
	var reopens atomic.Int64
	rep, err := f.w.CatchUp(t.Context(), reopenWith(f.cache, &reopens))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Scanned != 10_000 {
		t.Fatalf("scanned %d threads, want 10000", rep.Scanned)
	}
	t.Logf("catch-up over %d threads with no drift took %s (bound %s)", rep.Scanned, rep.Elapsed, catchUpBound)
	if rep.Elapsed > catchUpBound {
		t.Errorf("catch-up took %s, over the %s bound", rep.Elapsed, catchUpBound)
	}

	// The projection writer renders a source that carries a quotation to
	// record its backlinks (TKT-01M3YME9AZ). A thread with nothing to catch
	// up renders nothing, so the bound above does not move. This measures
	// what projecting does pay: ten thousand posts over a hundred threads are
	// caught up from nothing, once with one post in every hundred quoting and
	// once with none, and both times are logged for the ticket. Projecting
	// ten thousand posts is not what the 2 s bound is for, so the quoting
	// corpus is held to the plain one instead, with room for a shared
	// runner's noise: the check catches a render grown pathological, not a
	// few per cent.
	plain := projectCorpus(t, 0)
	quoted := projectCorpus(t, 100)
	if limit := 2*plain + time.Second; quoted > limit {
		t.Errorf("catch-up with one post in a hundred quoting took %s, over %s (twice the plain corpus's %s, plus a second)",
			quoted, limit, plain)
	}
}

// projectCorpus catches up a hundred threads of a hundred posts each from
// nothing, one post in every quoting (none for zero), and returns how long
// the catch-up took.
func projectCorpus(t *testing.T, every int) time.Duration {
	t.Helper()
	f := newFixture(t)
	const threads, posts = 100, 100
	n, want := 0, 0
	for i := range threads {
		id, _ := f.thread(fmt.Sprintf("Corpus %d", i))
		for p := range posts {
			src := "a post with **some** markdown and a [link](https://example.com)"
			if n++; every > 0 && n%every == 0 && p > 0 {
				src = quote(uint32(p)) // the post above
				want++
			}
			f.append(id, threadlog.Append{Type: threadlog.TypePost, Author: 5,
				Body: &threadlog.PostBody{Source: src}})
		}
	}
	var reopens atomic.Int64
	rep, err := f.w.CatchUp(t.Context(), reopenWith(f.cache, &reopens))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Scanned != threads || len(rep.Failed) != 0 || len(rep.Corrupt) != 0 {
		t.Fatalf("catch-up report %+v", rep)
	}
	if got := f.countQuotes(); got != want {
		t.Fatalf("post_quotes holds %d rows, want %d", got, want)
	}
	t.Logf("catch-up projecting %d posts, %d of them quoting, took %s", threads*posts, want, rep.Elapsed)
	return rep.Elapsed
}
