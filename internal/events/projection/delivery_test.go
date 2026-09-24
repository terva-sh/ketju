package projection

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"

	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// randomRecord appends one random record to a thread that has posts
// ordinals 1..posts, and returns the new post count.
func randomRecord(f *fixture, rng *rand.Rand, id threadid.ID, posts uint32) (uint32, int64) {
	target := uint32(1)
	if posts > 0 {
		target = 1 + rng.Uint32N(posts)
	}
	switch k := rng.IntN(10); {
	case k < 4 || posts == 0:
		var reply uint32
		if posts > 0 && rng.IntN(2) == 0 {
			reply = target
		}
		return posts + 1, f.post(id, uint64(1+rng.IntN(4)), reply, uint64(rng.IntN(6)))
	case k == 4:
		m := []uint64{uint64(rng.IntN(6))}
		body := &threadlog.EditBody{Source: "edit", ReplyTo: uint32(rng.IntN(int(target)))}
		if rng.IntN(2) == 0 {
			body.Mentions = &m
		}
		return posts, f.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: 2, Ordinal: target, Body: body})
	case k == 5:
		return posts, f.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: 1, Ordinal: target, Body: &threadlog.DeleteBody{}})
	case k == 6:
		return posts, f.append(id, threadlog.Append{Type: threadlog.TypeRestore, Author: 1, Ordinal: target, Body: &threadlog.RestoreBody{}})
	case k == 7:
		scopes := []string{"forum", "global"}
		if rng.IntN(2) == 0 {
			return posts, f.append(id, threadlog.Append{Type: threadlog.TypeThreadPinned, Author: 1,
				Body: &threadlog.ThreadPinnedBody{Scope: scopes[rng.IntN(2)], Until: int64(rng.IntN(2)) * 1_800_000_000_000}})
		}
		return posts, f.append(id, threadlog.Append{Type: threadlog.TypeThreadUnpinned, Author: 1,
			Body: &threadlog.ThreadUnpinnedBody{Scope: scopes[rng.IntN(2)]}})
	case k == 8:
		if rng.IntN(2) == 0 {
			return posts, f.append(id, threadlog.Append{Type: threadlog.TypeThreadLocked, Author: 1, Body: &threadlog.ThreadLockedBody{}})
		}
		return posts, f.append(id, threadlog.Append{Type: threadlog.TypeThreadUnlocked, Author: 1, Body: &threadlog.ThreadUnlockedBody{}})
	default:
		tags := []string{"a", "b", "c"}[:rng.IntN(4)]
		return posts, f.append(id, threadlog.Append{Type: threadlog.TypeThreadTagged, Author: 1, Body: &threadlog.ThreadTaggedBody{Tags: tags}})
	}
}

// TestIncrementalEqualsFull is the property half of AC1: a random log
// projected in random steps, each step a range from where the last ended,
// produces exactly the rows one projection of the whole log does.
func TestIncrementalEqualsFull(t *testing.T) {
	cases := 25
	if testing.Short() {
		cases = 5
	}
	for c := range cases {
		t.Run(fmt.Sprint(c), func(t *testing.T) {
			rng := rand.New(rand.NewPCG(uint64(c), 0xC2))
			f := newFixture(t)
			id, end := f.thread("Hello", "meta")
			lengths := []int64{end}
			var posts uint32
			for range 5 + rng.IntN(40) {
				posts, end = randomRecord(f, rng, id, posts)
				lengths = append(lengths, end)
			}
			for i := 0; i < len(lengths); i += 1 + rng.IntN(6) {
				f.project(id, lengths[i])
			}
			f.project(id, end)
			stepped := f.dump()

			f.clear()
			f.project(id, end)
			if full := f.dump(); full != stepped {
				t.Fatalf("stepped and full projections differ:\n%s\n---\n%s", stepped, full)
			}
		})
	}
}

// TestPerThreadOrderUnderInterleaving is AC2. Many threads append at once,
// each publishing its events from its own acknowledgement path, through a
// real bus. Each thread's frames must be applied in log order, and its rows
// must equal a projection of its whole log made afterwards.
func TestPerThreadOrderUnderInterleaving(t *testing.T) {
	f := newFixture(t)
	var mu sync.Mutex
	seen := map[threadid.ID][]int64{}
	f.w.applied = func(id threadid.ID, off int64) {
		mu.Lock()
		seen[id] = append(seen[id], off)
		mu.Unlock()
	}
	bus := events.New(events.Options{})
	if err := bus.Subscribe(f.w, 0); err != nil {
		t.Fatal(err)
	}
	if err := bus.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	const threads, records = 16, 25
	ids := make([]threadid.ID, threads)
	ends := make([]int64, threads)
	for i := range ids {
		ids[i], ends[i] = f.thread(fmt.Sprintf("T%d", i))
		bus.Publish(events.Event{Thread: ids[i], LogLength: ends[i]})
	}
	var wg sync.WaitGroup
	for i := range ids {
		wg.Go(func() {
			rng := rand.New(rand.NewPCG(uint64(i), 0xB05))
			var posts uint32
			for range records {
				var end int64
				posts, end = randomRecord(f, rng, ids[i], posts)
				bus.Publish(events.Event{Thread: ids[i], LogLength: end})
				ends[i] = end
			}
		})
	}
	wg.Wait()
	if err := bus.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if l := f.w.Lag(); l.Threads != 0 {
		t.Fatalf("the writer is behind after the bus drained: %+v", l)
	}

	for _, id := range ids {
		offs := seen[id]
		for i := 1; i < len(offs); i++ {
			if offs[i] <= offs[i-1] {
				t.Fatalf("thread %s: frame at %d applied after %d", id, offs[i], offs[i-1])
			}
		}
		if len(offs) != records+1 {
			t.Errorf("thread %s: %d frames applied, want %d", id, len(offs), records+1)
		}
	}
	incremental := f.dump()
	f.clear()
	for i, id := range ids {
		f.project(id, ends[i])
	}
	if full := f.dump(); full != incremental {
		t.Fatalf("rows from the bus differ from a full projection:\n%s\n---\n%s", incremental, full)
	}
}

// TestFailureIsRetriedAndReportedAsLag is AC4. While the log cannot be read
// the thread is kept and reported as lag, and the event is not handed back
// to the bus as dropped; once it can, a retry catches up to rows identical to
// a clean projection.
func TestFailureIsRetriedAndReportedAsLag(t *testing.T) {
	f := newFixture(t)
	id, _ := f.thread("Hello")
	f.post(id, 5, 0)
	end := f.post(id, 6, 1)

	realOpen := f.w.open
	broken := errors.New("disk on fire")
	f.w.open = func(string) (logFile, error) { return nil, broken }

	if err := f.w.Deliver(t.Context(), events.Batch{Events: []events.Event{{Thread: id, LogLength: end - 10}}}); err != nil {
		t.Fatalf("Deliver returned %v; a held failure must not be reported to the bus as a drop", err)
	}
	// A later event while behind moves the target on, and an older one
	// arriving late does not move it back.
	for _, n := range []int64{end, end - 10} {
		if err := f.w.Deliver(t.Context(), events.Batch{Events: []events.Event{{Thread: id, LogLength: n}}}); err != nil {
			t.Fatal(err)
		}
	}
	l := f.w.Lag()
	if l.Threads != 1 || l.Bytes != end-threadlog.FileHeaderSize {
		t.Fatalf("lag while failing is %+v, want 1 thread and %d bytes", l, end-threadlog.FileHeaderSize)
	}
	if err := f.w.Retry(t.Context()); !errors.Is(err, broken) {
		t.Fatalf("Retry while still failing returned %v", err)
	}
	if strings.Contains(f.dump(), "threads: 1 rows") {
		t.Fatal("a failed projection left rows behind")
	}

	// Recovery through Deliver: a batch about another thread retries this
	// one too, so a stalled thread does not wait for its own next event.
	f.w.open = realOpen
	other, otherEnd := f.thread("Other")
	if err := f.w.Deliver(t.Context(), events.Batch{Events: []events.Event{{Thread: other, LogLength: otherEnd}}}); err != nil {
		t.Fatal(err)
	}
	if l := f.w.Lag(); l.Threads != 0 || l.Bytes != 0 {
		t.Fatalf("lag after recovery is %+v", l)
	}
	recovered := f.dump()
	f.clear()
	f.project(id, end)
	f.project(other, otherEnd)
	if clean := f.dump(); clean != recovered {
		t.Fatalf("recovered rows differ from a clean projection:\n%s\n---\n%s", recovered, clean)
	}
}

// TestDroppedTriggersSweep: a batch reporting dropped events calls Sweep, and
// a sweep that fails is returned so the bus reports the hole again.
func TestDroppedTriggersSweep(t *testing.T) {
	f := newFixture(t)
	sweeps := 0
	var fail error
	f.w.sweep = func(context.Context) error { sweeps++; return fail }

	if err := f.w.Deliver(t.Context(), events.Batch{Dropped: 3}); err != nil || sweeps != 1 {
		t.Fatalf("got %v after %d sweeps, want nil after 1", err, sweeps)
	}
	if err := f.w.Deliver(t.Context(), events.Batch{}); err != nil || sweeps != 1 {
		t.Fatalf("a batch with nothing dropped swept (%d) or failed (%v)", sweeps, err)
	}
	fail = errors.New("sweep failed")
	if err := f.w.Deliver(t.Context(), events.Batch{Dropped: 1}); !errors.Is(err, fail) {
		t.Fatalf("a failed sweep returned %v", err)
	}
}

// rawLog writes a log by hand, for record types the actor will not append.
func rawLog(t *testing.T, f *fixture, recs ...struct {
	typ  threadlog.RecordType
	body any
}) (threadid.ID, int64) {
	t.Helper()
	id := threadid.ID(uint64(1_600_000_000_000)<<16 | f.next.Add(1))
	log := threadlog.NewFileHeader(uint64(id), 1, 7).AppendTo(nil)
	var ord uint32
	for _, r := range recs {
		raw, err := cbor.Marshal(r.body)
		if err != nil {
			t.Fatal(err)
		}
		h := threadlog.FrameHeader{Type: r.typ, Encoding: threadlog.EncodingCBOR, Revision: 1, TimestampMS: 5, Actor: 3}
		if r.typ == threadlog.TypePost {
			ord++
			h.Ordinal = ord
		}
		if log, err = threadlog.AppendFrame(log, h, raw); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(id.Dir(f.root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(id.LogPath(f.root), log, 0o644); err != nil {
		t.Fatal(err)
	}
	return id, int64(len(log))
}

type rec = struct {
	typ  threadlog.RecordType
	body any
}

// TestUnknownTypes: an unknown advisory type and a reserved one are stepped
// over, as the index steps over them. An unknown critical type stops the
// thread, which then shows as lag.
func TestUnknownTypes(t *testing.T) {
	f := newFixture(t)
	created := rec{threadlog.TypeThreadCreated, &threadlog.ThreadCreatedBody{Title: "T", Slug: "t", ForumID: 7}}
	post := rec{threadlog.TypePost, &threadlog.PostBody{Source: "x"}}
	future := map[string]any{"from": "a later phase"}

	id, end := rawLog(t, f, created, post, rec{threadlog.RecordType(99), future},
		rec{threadlog.TypePostsSplitOut, future}, post)
	f.project(id, end)
	if d := f.dump(); !strings.Contains(d, "post_count=2") || !strings.Contains(d, "post_meta: 2 rows") {
		t.Fatalf("skipping the advisory and reserved records lost the posts around them:\n%s", d)
	}

	id, end = rawLog(t, f, created, post, rec{threadlog.RecordType(0x90), future}, post)
	if err := f.w.Deliver(t.Context(), events.Batch{Events: []events.Event{{Thread: id, LogLength: end}}}); err != nil {
		t.Fatal(err)
	}
	if l := f.w.Lag(); l.Threads != 1 {
		t.Fatalf("a critical unknown type left lag %+v; the thread should be stalled", l)
	}
	if _, err := f.w.ProjectThread(t.Context(), id, end); !errors.Is(err, threadlog.ErrCriticalRecordType) {
		t.Fatalf("got %v, want ErrCriticalRecordType", err)
	}
}

// TestBehindUntilTheTargetIsReached: a thread whose target moved past what a
// projection reached stays behind, with the lag it still has.
func TestBehindUntilTheTargetIsReached(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Hello")
	end := f.post(id, 5, 0)
	f.w.behind[id] = &lag{target: end, projected: threadlog.FileHeaderSize}

	// A failed attempt at a lower length, as a concurrent Retry holding an
	// older target makes, does not lower the target.
	realOpen := f.w.open
	f.w.open = func(string) (logFile, error) { return nil, errors.New("no") }
	f.w.project(t.Context(), id, created)
	f.w.open = realOpen
	if l := f.w.Lag(); l.Bytes != end-threadlog.FileHeaderSize {
		t.Fatalf("a failed attempt at %d changed the lag to %+v", created, l)
	}

	if err := f.w.project(t.Context(), id, created); err != nil {
		t.Fatal(err)
	}
	if l := f.w.Lag(); l.Threads != 1 || l.Bytes != end-created {
		t.Fatalf("after reaching %d of %d the lag is %+v, want 1 thread and %d bytes", created, end, l, end-created)
	}
	if err := f.w.project(t.Context(), id, end); err != nil {
		t.Fatal(err)
	}
	if l := f.w.Lag(); l.Threads != 0 {
		t.Fatalf("after reaching the target the lag is %+v", l)
	}
}

// TestDamageStopsTheThread: a frame that fails its checksum is never
// projected past; the thread stops there.
func TestDamageStopsTheThread(t *testing.T) {
	f := newFixture(t)
	created := rec{threadlog.TypeThreadCreated, &threadlog.ThreadCreatedBody{Title: "T", Slug: "t", ForumID: 7}}
	post := rec{threadlog.TypePost, &threadlog.PostBody{Source: "x"}}
	id, end := rawLog(t, f, created, post, post)
	path := id.LogPath(f.root)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b[end-6] ^= 0xff // inside the last frame's body
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.ProjectThread(t.Context(), id, end); !errors.Is(err, threadlog.ErrChecksum) {
		t.Fatalf("got %v, want ErrChecksum", err)
	}
	if strings.Contains(f.dump(), "post_meta: 1 rows") || strings.Contains(f.dump(), "post_meta: 2 rows") {
		t.Fatal("rows from before the damage were kept from a failed projection")
	}
}

// TestRunRetriesUntilCaughtUp: Run keeps retrying a behind thread, backing
// off while it fails, and catches it up once the log can be read.
func TestRunRetriesUntilCaughtUp(t *testing.T) {
	floor, ceiling := retryFloor, retryCeiling
	retryFloor, retryCeiling = time.Millisecond, 4*time.Millisecond
	t.Cleanup(func() { retryFloor, retryCeiling = floor, ceiling })

	f := newFixture(t)
	id, end := f.thread("Hello")
	var fails atomic.Int32
	realOpen := f.w.open
	f.w.open = func(path string) (logFile, error) {
		if fails.Add(1) <= 5 {
			return nil, errors.New("not yet")
		}
		return realOpen(path)
	}
	if err := f.w.Deliver(t.Context(), events.Batch{Events: []events.Event{{Thread: id, LogLength: end}}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { f.w.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for f.w.Lag().Threads != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("Run did not catch up; lag %+v after %d attempts", f.w.Lag(), fails.Load())
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if n := fails.Load(); n < 6 {
		t.Fatalf("caught up after %d attempts; the first five were set to fail", n)
	}
}
