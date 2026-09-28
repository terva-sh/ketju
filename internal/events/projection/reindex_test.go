package projection

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestReindexRebuildsFromEmptyAndStale: threads projected one by one, then
// reindexed twice, once over rows scribbled with state no record put there
// and once over emptied tables. Both come back to the rows the incremental
// projection made, and progress is reported before the first thread and
// after each.
func TestReindexRebuildsFromEmptyAndStale(t *testing.T) {
	f := newFixture(t)
	rng := rand.New(rand.NewPCG(7, 0xC5))
	var ids []threadid.ID
	for i := range 6 {
		id, end := f.thread(fmt.Sprintf("T%d", i), "meta")
		f.project(id, end)
		var posts uint32
		for range 10 + rng.IntN(20) {
			posts, end = randomRecord(f, rng, id, posts)
			f.project(id, end)
		}
		ids = append(ids, id)
	}
	incremental := f.dump()

	for _, setup := range []struct {
		name string
		run  func()
	}{
		{"stale", func() {
			f.exec(`UPDATE threads SET title = 'stale', post_count = 99, locked = 1 - locked`)
			f.exec(`UPDATE post_meta SET deleted = 1 - deleted, reply_to = 77`)
			f.exec(`INSERT INTO thread_tags (thread_id, tag) VALUES (1, 'orphan')`)
			f.exec(`INSERT INTO projection_state (thread_id, log_length, updated_at) VALUES (1, 999, 0)`)
			f.exec(`UPDATE forum_thread_counts SET threads = threads + 5`)
		}},
		{"empty", f.clear},
	} {
		t.Run(setup.name, func(t *testing.T) {
			setup.run()
			var seen []ReindexProgress
			rep, err := f.w.Reindex(t.Context(), func(p ReindexProgress) { seen = append(seen, p) })
			if err != nil {
				t.Fatal(err)
			}
			if got := f.dump(); got != incremental {
				t.Fatalf("reindexed rows differ from the incremental ones:\n%s\n---\n%s", incremental, got)
			}
			if rep.Threads != len(ids) || rep.Projected != len(ids) || len(rep.Failed)+len(rep.Torn)+len(rep.Unfinished) != 0 {
				t.Fatalf("report = %+v", rep)
			}
			if len(seen) != len(ids)+1 || seen[0].Done != 0 || seen[len(seen)-1].Done != len(ids) ||
				seen[0].TotalBytes == 0 || seen[len(seen)-1].Bytes != seen[0].TotalBytes {
				t.Fatalf("progress = %+v", seen)
			}
		})
	}
}

// TestReindexAroundBrokenLogs: one log of each kind reindex has to step
// around, beside a sound one. A torn tail is projected to its last whole
// frame; a header-only log has nothing to project; a checksum failure before
// the last frame and a file cut inside its header are failures with no rows; and none of them
// stops the sound thread. The logs are left byte for byte as they were.
func TestReindexAroundBrokenLogs(t *testing.T) {
	f := newFixture(t)
	sound, soundEnd := f.thread("Sound")
	soundEnd = max(soundEnd, f.post(sound, 1, 0))

	torn, tornEnd := f.thread("Torn")
	tornEnd = max(tornEnd, f.post(torn, 1, 0))

	// Damage before the last frame. In the last frame a checksum failure is
	// what a final write that reached the disk in pieces looks like, and it
	// is a torn tail to the engine and to reindex alike.
	damaged, _ := f.thread("Damaged")
	damagedAt := f.post(damaged, 1, 0)
	f.post(damaged, 1, 0)

	empty := threadid.ID(uint64(1_700_000_000_000)<<16 | 900)
	th, err := f.cache.Create(empty, 7, 1_700_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	f.cache.Release(th)

	short := threadid.ID(uint64(1_700_000_000_000)<<16 | 901)
	th, err = f.cache.Create(short, 7, 1_700_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	f.cache.Release(th)
	if err := f.cache.Close(); err != nil {
		t.Fatal(err)
	}

	// The incremental rows for what survives: the sound thread whole, and
	// the torn one up to its last whole frame.
	f.project(sound, soundEnd)
	f.project(torn, tornEnd)
	want := f.dump()

	tear(t, f.root, torn)
	b, err := os.ReadFile(damaged.LogPath(f.root))
	if err != nil {
		t.Fatal(err)
	}
	b[damagedAt-6] ^= 0xff
	if err := os.WriteFile(damaged.LogPath(f.root), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(short.LogPath(f.root), 20); err != nil {
		t.Fatal(err)
	}
	before := map[threadid.ID][]byte{}
	for _, id := range []threadid.ID{sound, torn, damaged, empty, short} {
		before[id], _ = os.ReadFile(id.LogPath(f.root))
	}

	rep, err := f.w.Reindex(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.dump(); got != want {
		t.Fatalf("rows differ:\n%s\n---\n%s", want, got)
	}
	if rep.Threads != 5 || rep.Projected != 2 || !slices.Equal(rep.Torn, []threadid.ID{torn}) ||
		!slices.Equal(rep.Unfinished, []threadid.ID{empty}) || len(rep.Failed) != 2 {
		t.Fatalf("report = %+v", rep)
	}
	failed := map[threadid.ID]error{}
	for _, fl := range rep.Failed {
		failed[fl.Thread] = fl.Err
	}
	if !errors.Is(failed[damaged], threadlog.ErrChecksum) {
		t.Errorf("damaged: %v, want ErrChecksum", failed[damaged])
	}
	if !errors.Is(failed[short], threadlog.ErrLogTooShort) {
		t.Errorf("short: %v, want ErrLogTooShort", failed[short])
	}
	for id, b := range before {
		if after, _ := os.ReadFile(id.LogPath(f.root)); string(after) != string(b) {
			t.Errorf("reindex changed the log of %s", id)
		}
	}
	if !strings.Contains(want, fmt.Sprintf("log_length=%d", tornEnd)) {
		t.Fatalf("the torn thread's projection does not stop at %d:\n%s", tornEnd, want)
	}
}

// TestReindexWalkFailureChangesNothing: a walk that cannot finish returns
// before the tables are emptied.
func TestReindexWalkFailureChangesNothing(t *testing.T) {
	f := newFixture(t)
	id, end := f.thread("Kept")
	f.project(id, end)
	want := f.dump()

	shard := id.Dir(f.root)
	if err := os.Chmod(shard, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(shard, 0o755) })
	if _, err := os.ReadDir(shard); err == nil {
		t.Skip("running as a user that can read an unreadable directory")
	}
	if _, err := f.w.Reindex(t.Context(), nil); err == nil {
		t.Fatal("reindex succeeded over a directory it could not read")
	}
	if got := f.dump(); got != want {
		t.Fatalf("a failed walk changed the projection:\n%s\n---\n%s", want, got)
	}
}
