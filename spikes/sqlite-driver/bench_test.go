package sqlitedriver

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// BenchmarkProjectionWrite measures the unit of work the projection writer
// (TKT-01M29C2WW5) will commit: one transaction per thread per batch of
// appended posts, which upserts the threads row, inserts the posts' post_meta
// rows and their mentions, and advances projection_state.
//
// One op is one committed batch. ns/post divides by the batch size, so the
// rows compare across batch sizes. conns is how many goroutines, each with its
// own connection, commit concurrently; with BEGIN IMMEDIATE they serialise on
// the write lock, so conns measures contention handling, not parallel writes.
//
// sync=full is SQLite's default and the figure that matters, but it is not a
// like-for-like driver comparison: ncruces syncs with fdatasync and modernc,
// built without HAVE_FDATASYNC, with a full fsync. sync=off takes the disk out
// and leaves the drivers' own cost. Read both against BenchmarkSyncFloor.
//
// busy/op counts SQLITE_BUSY returned despite the busy_timeout. The batch is
// retried, up to maxBusyRetries, rather than failing the run, because whether
// a saturated pool of writers starves one of them for five seconds is itself
// the finding.
//
// The figures recorded on TKT-01M29BSPE8 come from:
//
//	go -C spikes/sqlite-driver test -run '^$' -bench . -benchtime=1s -count=3
//
// on a named machine, never a CI runner.
func BenchmarkProjectionWrite(b *testing.B) {
	for _, syncMode := range []string{"full", "off"} {
		for _, d := range drivers {
			for _, batch := range []int{1, 16, 256} {
				for _, conns := range []int{1, 4, 8} {
					name := fmt.Sprintf("sync=%s/driver=%s/batch=%d/conns=%d", syncMode, d.name, batch, conns)
					b.Run(name, func(b *testing.B) { benchProjectionWrite(b, d, syncMode, batch, conns) })
				}
			}
		}
	}
}

// maxBusyRetries bounds how many times one batch is retried after SQLITE_BUSY.
// The worst case observed was one busy return in several hundred batches, so
// three in a row means the lock is stuck, not contended.
const maxBusyRetries = 3

func benchProjectionWrite(b *testing.B, d driverUnderTest, syncMode string, batch, conns int) {
	db := openScratch(b, d, "&_pragma=synchronous("+syncMode+")")
	db.SetMaxOpenConns(conns)
	db.SetMaxIdleConns(conns)

	// Each worker owns a disjoint set of threads, so ordinals never collide
	// and every insert is a real insert rather than a conflict.
	const threadsPerWorker = 64
	workers := make([]*projector, conns)
	for w := range workers {
		workers[w] = newProjector(int64(w)*threadsPerWorker+1, threadsPerWorker)
	}

	var remaining, busy atomic.Int64
	remaining.Store(int64(b.N))
	var wg sync.WaitGroup
	errs := make(chan error, conns)

	b.ResetTimer()
	for _, p := range workers {
		wg.Go(func() {
			for remaining.Add(-1) >= 0 {
				for attempt := 1; ; attempt++ {
					err := p.commitBatch(db, batch)
					if err == nil {
						break
					}
					if !isBusy(err) {
						errs <- err
						return
					}
					busy.Add(1)
					// Each attempt may already have waited out the whole
					// busy_timeout, so a lock that never frees must end the
					// run rather than hang it (and CI with it).
					if attempt == maxBusyRetries {
						errs <- fmt.Errorf("batch still SQLITE_BUSY after %d attempts: %w", attempt, err)
						return
					}
				}
			}
		})
	}
	wg.Wait()
	b.StopTimer()

	close(errs)
	for err := range errs {
		b.Fatal(err)
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*batch), "ns/post")
	b.ReportMetric(float64(busy.Load())/float64(b.N), "busy/op")
}

// BenchmarkSyncFloor is the cost of the two sync calls on the scratch disk,
// with no SQLite involved: overwrite 4 KiB in place, then sync it. The gap
// between the two rows is the part of any sync=full difference that belongs
// to the disk rather than to either driver.
func BenchmarkSyncFloor(b *testing.B) {
	for _, c := range []struct {
		name string
		sync func(fd int) error
	}{
		{"fsync", unix.Fsync},
		{"fdatasync", unix.Fdatasync},
	} {
		b.Run(c.name, func(b *testing.B) {
			root, err := scratchRootOnce()
			if err != nil {
				b.Fatal(err)
			}
			f, err := os.CreateTemp(root, "floor-")
			if err != nil {
				b.Fatal(err)
			}
			defer os.Remove(f.Name())
			defer f.Close()
			page := make([]byte, 4096)
			if _, err := f.Write(page); err != nil {
				b.Fatal(err)
			}
			if err := f.Sync(); err != nil {
				b.Fatal(err)
			}
			for b.Loop() {
				if _, err := f.WriteAt(page, 0); err != nil {
					b.Fatal(err)
				}
				if err := c.sync(int(f.Fd())); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// projector plays the projection writer for a fixed set of threads, cycling
// through them and appending posts to each in turn.
type projector struct {
	first, count int64
	next         int64           // offset of the next thread to write, within the set
	ordinals     map[int64]int64 // thread id -> last projected ordinal
}

func newProjector(first, count int64) *projector {
	return &projector{first: first, count: count, ordinals: make(map[int64]int64)}
}

func (p *projector) commitBatch(db *sql.DB, batch int) error {
	thread := p.first + p.next
	p.next = (p.next + 1) % p.count
	from := p.ordinals[thread] + 1
	to := from + int64(batch) - 1
	now := time.Now().UnixMilli()

	// With _txlock=immediate this BEGIN is BEGIN IMMEDIATE on both drivers.
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Author ids spread across 200 users so the (author_id, created_at) index
	// sees realistic fan-out rather than one hot key.
	author := func(ord int64) int64 { return (thread*7+ord)%200 + 1 }

	if _, err := tx.Exec(`
		INSERT INTO threads (id, forum_id, slug, title, author_id, created_at,
			post_count, last_post_at, last_post_author_id, last_post_ordinal, log_length_projected)
		VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			post_count = excluded.post_count,
			last_post_at = excluded.last_post_at,
			last_post_author_id = excluded.last_post_author_id,
			last_post_ordinal = excluded.last_post_ordinal,
			log_length_projected = excluded.log_length_projected`,
		thread, fmt.Sprintf("thread-%d", thread), fmt.Sprintf("Thread %d", thread),
		author(1), now, to, now, author(to), to, to*512,
	); err != nil {
		return fmt.Errorf("upsert threads: %w", err)
	}

	insPost, err := tx.Prepare(`
		INSERT INTO post_meta (thread_id, ordinal, author_id, created_at,
			current_revision, reply_to, log_offset)
		VALUES (?, ?, ?, ?, 1, ?, ?)`)
	if err != nil {
		return err
	}
	defer insPost.Close()
	insMention, err := tx.Prepare(`
		INSERT INTO mentions (thread_id, ordinal, mentioned_user_id) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer insMention.Close()

	for ord := from; ord <= to; ord++ {
		var replyTo any
		if ord > 1 && ord%3 == 0 {
			replyTo = ord - 1
		}
		if _, err := insPost.Exec(thread, ord, author(ord), now, replyTo, ord*512); err != nil {
			return fmt.Errorf("insert post_meta: %w", err)
		}
		// One post in four mentions someone.
		if ord%4 == 0 {
			if _, err := insMention.Exec(thread, ord, (ord*13)%200+1); err != nil {
				return fmt.Errorf("insert mentions: %w", err)
			}
		}
	}

	if _, err := tx.Exec(`
		INSERT INTO projection_state (thread_id, log_length, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (thread_id) DO UPDATE SET
			log_length = excluded.log_length, updated_at = excluded.updated_at`,
		thread, to*512, now,
	); err != nil {
		return fmt.Errorf("advance projection_state: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	p.ordinals[thread] = to
	return nil
}
