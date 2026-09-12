package format

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
)

type policy int

const (
	// always fsyncs every append on its own.
	always policy = iota
	// groupDrain batches whatever is already queued and never waits. A lone
	// append pays no extra latency.
	groupDrain
	// groupWait holds the batch open for the window, trading latency at low
	// load for fewer fsyncs under load. This is the reading of docs/10 that
	// says "a 5 ms window".
	groupWait
)

func (p policy) String() string {
	switch p {
	case always:
		return "always"
	case groupDrain:
		return "group(drain)"
	default:
		return "group(wait 5ms)"
	}
}

type appendReq struct {
	frame []byte
	done  chan struct{}
}

const maxBatch = 256

func actor(f *os.File, in <-chan *appendReq, p policy, window time.Duration, syncs, batched *int64, wg *sync.WaitGroup) {
	defer wg.Done()
	batch := make([]*appendReq, 0, maxBatch)
	for first := range in {
		batch = append(batch[:0], first)
		switch p {
		case always:
		case groupDrain:
		drain:
			for len(batch) < maxBatch {
				select {
				case r, ok := <-in:
					if !ok {
						break drain
					}
					batch = append(batch, r)
				default:
					break drain
				}
			}
		case groupWait:
			timer := time.NewTimer(window)
		wait:
			for len(batch) < maxBatch {
				select {
				case r, ok := <-in:
					if !ok {
						break wait
					}
					batch = append(batch, r)
				case <-timer.C:
					break wait
				}
			}
			timer.Stop()
		}
		for _, r := range batch {
			if _, err := f.Write(r.frame); err != nil {
				panic(err)
			}
		}
		if err := f.Sync(); err != nil {
			panic(err)
		}
		atomic.AddInt64(syncs, 1)
		atomic.AddInt64(batched, int64(len(batch)))
		for _, r := range batch {
			close(r.done)
		}
	}
}

func pct(d []time.Duration, q float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	i := int(q * float64(len(d)-1))
	return d[i]
}

func runPolicy(t *testing.T, dir string, p policy, writers, perWriter int, frames [][]byte, rep int) {
	t.Helper()
	path := filepath.Join(dir, fmt.Sprintf("fsync-%d-%d.log", int(p), writers))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { f.Close(); os.Remove(path) }()

	// Warm up before timing. On a fresh file ext4 block allocation and journal
	// behaviour dominate the first appends, and an earlier version of this
	// benchmark attributed that cost to whichever policy happened to run first.
	for i := 0; i < 64; i++ {
		if _, err := f.Write(frames[i%len(frames)]); err != nil {
			t.Fatal(err)
		}
		if err := f.Sync(); err != nil {
			t.Fatal(err)
		}
	}

	in := make(chan *appendReq, 1024)
	var syncs, batched int64
	var actorWG sync.WaitGroup
	actorWG.Add(1)
	go actor(f, in, p, 5*time.Millisecond, &syncs, &batched, &actorWG)

	lat := make([][]time.Duration, writers)
	var wg sync.WaitGroup
	start := time.Now()
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			lat[w] = make([]time.Duration, 0, perWriter)
			for i := 0; i < perWriter; i++ {
				req := &appendReq{frame: frames[(w*perWriter+i)%len(frames)], done: make(chan struct{})}
				t0 := time.Now()
				in <- req
				<-req.done
				lat[w] = append(lat[w], time.Since(t0))
			}
		}(w)
	}
	wg.Wait()
	close(in)
	actorWG.Wait()
	elapsed := time.Since(start)

	all := make([]time.Duration, 0, writers*perWriter)
	for _, l := range lat {
		all = append(all, l...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })

	meanBatch := float64(batched) / float64(max64(syncs, 1))
	fmt.Printf("%-16s %7d %4d %10.3f %10.3f %10.3f %10.3f %8d %9.2f %10.0f\n",
		p.String(), writers, rep,
		ms(pct(all, 0.50)), ms(pct(all, 0.95)), ms(pct(all, 0.99)), ms(all[len(all)-1]),
		syncs, meanBatch, float64(len(all))/elapsed.Seconds())
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func ms(d time.Duration) float64 { return float64(d.Nanoseconds()) / 1e6 }

// TestFsyncLatency measures append-to-durable latency under the three
// policies. Numbers are only meaningful for the storage they were taken on,
// so the caller must record what that was.
func TestFsyncLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("touches the disk")
	}
	dir := spikeDir(t)

	r := rand.New(rand.NewSource(3))
	frames := make([][]byte, 128)
	for i := range frames {
		body, err := cbor.Marshal(GenBody(r, 2048))
		if err != nil {
			t.Fatal(err)
		}
		frames[i] = EncodeFrame(2, 1, 0, uint32(i+1), 1, time.Now().UnixMilli(), uint64(1000+i%40), body)
	}

	fmt.Println("\n=== append to durable, latency in ms ===")
	fmt.Printf("%-16s %7s %4s %10s %10s %10s %10s %8s %9s %10s\n",
		"policy", "writers", "rep", "p50", "p95", "p99", "max", "fsyncs", "meanBatch", "appends/s")
	// Three interleaved repetitions, so an order effect shows up as spread
	// between reps rather than as a difference between policies.
	for _, writers := range []int{1, 8} {
		per := 400
		if writers > 1 {
			per = 200
		}
		for rep := 1; rep <= 3; rep++ {
			for _, p := range []policy{always, groupDrain, groupWait} {
				runPolicy(t, dir, p, writers, per, frames, rep)
			}
		}
	}
}

// spikeDir keeps scratch files inside the workspace.
func spikeDir(tb testing.TB) string {
	tb.Helper()
	dir := os.Getenv("NF_SPIKE_DIR")
	if dir == "" {
		dir = ".tmp"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		tb.Fatal(err)
	}
	return dir
}
