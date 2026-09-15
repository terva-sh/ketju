package threadlog

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"
)

// The four numbers docs/09 makes the Phase 0 exit criterion, measured against
// the real engine rather than the spike:
//
//	append p99            under 10 ms with group fsync
//	random-ordinal read   under 100 us warm
//	cold read             from a page cache miss
//	index rebuild         of a 100 MB log under 2 s
//
// Repeatable from one command, which is the point of these living in the
// repository rather than in a shell history:
//
//	go test ./internal/store/threadlog/ -run '^$' -bench 'Benchmark' \
//	    -benchtime=1x -timeout=30m
//
// Benchmark files go to $HOME/.cache/ketju-bench, NOT the system temp dir,
// which is usually tmpfs and where fsync does nothing at all. Override with
// -bench.dir. Read every append figure against BenchmarkFsyncFloor: if the
// floor is a few microseconds, the storage is lying and the durability numbers
// mean nothing.
//
// Every latency benchmark reports percentiles as custom metrics, because a
// mean hides exactly the tail these targets are about. ns/op is left alone so
// the usual tooling still works.

var (
	benchPosts = flag.Int("bench.posts", 50_000,
		"posts in the synthetic thread used for read benchmarks")
	benchRebuildMB = flag.Int("bench.rebuildmb", 100,
		"log size in MB for the index rebuild benchmark")
	benchDir = flag.String("bench.dir", "",
		"directory on REAL storage for benchmark files; empty means $HOME/.cache/ketju-bench. "+
			"Never the system temp dir, which is commonly tmpfs")
	benchAllowVolatile = flag.Bool("bench.allow-volatile-fs", false,
		"run even where fsync is a no-op. For checking the benchmarks still RUN, as CI does. "+
			"Any durability number produced under this flag is meaningless and must not be recorded")
)

// benchRootOnce resolves the working directory once and refuses tmpfs.
var benchRootOnce = sync.OnceValues(func() (string, error) {
	dir := *benchDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".cache", "ketju-bench")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := checkDurableFS(dir); err != nil && !*benchAllowVolatile {
		return "", err
	}
	return dir, nil
})

// benchRoot returns a directory whose fsync reaches a disk.
//
// The system temp directory is deliberately not the default. TMPDIR is
// frequently unset and /tmp is frequently tmpfs, where fsync returns without
// doing anything — see checkDurableFS for what that cost the first run of
// these benchmarks.
func benchRoot(b *testing.B) string {
	b.Helper()
	dir, err := benchRootOnce()
	if err != nil {
		b.Fatalf("benchmark storage: %v\n\nPass -bench.dir=PATH pointing at real storage.", err)
	}
	return dir
}

// benchTempDir returns a fresh subdirectory of benchRoot, removed afterwards.
func benchTempDir(b *testing.B) string {
	b.Helper()
	dir, err := os.MkdirTemp(benchRoot(b), "run-")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// BenchmarkFsyncFloor is the reference every other number here is read
// against: what one 4 KB write plus fsync costs on this storage.
//
// Without it a reader cannot tell a fast engine from a dishonest disk. On a
// guest the host's cache mode is invisible from inside, so this is the closest
// thing to evidence that an fsync reached something persistent, and it belongs
// beside the append figures rather than in a footnote.
func BenchmarkFsyncFloor(b *testing.B) {
	dir := benchTempDir(b)
	f, err := os.CreateTemp(dir, "floor-")
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()

	buf := make([]byte, 4096)
	lat := make([]time.Duration, b.N)
	b.ResetTimer()
	for i := range b.N {
		start := time.Now()
		if _, err := f.Write(buf); err != nil {
			b.Fatal(err)
		}
		if err := f.Sync(); err != nil {
			b.Fatal(err)
		}
		lat[i] = time.Since(start)
	}
	b.StopTimer()
	reportLatency(b, lat)
}

// corpus is a thread built once per process and shared by the read benchmarks.
// Building 50k posts takes long enough that doing it per benchmark would
// dominate what is being measured.
type corpus struct {
	dir     string
	logPath string
	posts   int
	idxRaw  []byte
	logSize int64
}

var (
	corpusOnce  sync.Map // key -> *sync.Once
	corpusCache sync.Map // key -> *corpus
)

func getCorpus(b *testing.B, posts int, edited bool) *corpus {
	b.Helper()
	root := benchRoot(b)
	key := fmt.Sprintf("%d-%v", posts, edited)
	onceAny, _ := corpusOnce.LoadOrStore(key, &sync.Once{})
	onceAny.(*sync.Once).Do(func() {
		c, err := buildCorpus(root, posts, edited)
		if err != nil {
			b.Fatalf("building the corpus: %v", err)
		}
		corpusCache.Store(key, c)
	})
	v, ok := corpusCache.Load(key)
	if !ok {
		b.Fatalf("corpus %s was not built", key)
	}
	return v.(*corpus)
}

// buildCorpus writes a thread to a real file. Real files, because the numbers
// that matter here are fsync and page cache numbers, and an in-memory fake
// would measure neither.
func buildCorpus(root string, posts int, edited bool) (*corpus, error) {
	dir, err := os.MkdirTemp(root, "corpus-")
	if err != nil {
		return nil, err
	}
	logPath := filepath.Join(dir, "bench.log")
	idxPath := filepath.Join(dir, "bench.idx")

	lf, err := os.OpenFile(logPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, err
	}
	if _, err := lf.Write(NewFileHeader(1, 1_700_000_000_000, 7).AppendTo(nil)); err != nil {
		return nil, err
	}
	xf, err := os.OpenFile(idxPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, err
	}

	// SyncNever while building: this is setup, and fsyncing 50k times to
	// prepare a read benchmark would take longer than every benchmark here
	// put together.
	a := NewActor(&Index{Header: IndexHeader{ThreadID: 1}}, FileHeaderSize, lf, xf, SyncNever)
	r := rand.New(rand.NewSource(7))
	for i := range posts {
		if _, err := a.Append(Append{
			Type: TypePost, Author: uint64(1 + i%50), TimeMS: 1_700_000_000_000 + int64(i),
			Body: &PostBody{Source: postText(r, i)},
		}); err != nil {
			return nil, err
		}
	}
	if edited {
		// A quarter of the posts edited, which is what breaks the contiguity
		// the read path coalesces on: an edited post's current revision lives
		// wherever the edit was appended, so it is no longer beside its
		// neighbours.
		for i := 1; i <= posts; i += 4 {
			if _, err := a.Append(Append{
				Type: TypeEdit, Ordinal: uint32(i), Author: 1,
				TimeMS: 1_700_000_100_000 + int64(i),
				Body:   &EditBody{Source: postText(r, i) + " (edited)"},
			}); err != nil {
				return nil, err
			}
		}
	}
	if err := a.Close(); err != nil {
		return nil, err
	}
	if err := lf.Close(); err != nil {
		return nil, err
	}
	if err := xf.Close(); err != nil {
		return nil, err
	}

	fi, err := os.Stat(logPath)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(idxPath)
	if err != nil {
		return nil, err
	}
	return &corpus{dir: dir, logPath: logPath, posts: posts, idxRaw: raw, logSize: fi.Size()}, nil
}

func postText(r *rand.Rand, i int) string {
	// Roughly the shape of a real message: a few sentences, varying.
	n := 120 + r.Intn(400)
	b := make([]byte, 0, n)
	b = fmt.Appendf(b, "post %d: ", i)
	for len(b) < n {
		b = append(b, "the quick brown fox jumps over the lazy dog. "...)
	}
	return string(b[:n])
}

// ---------- 1. append latency ----------

// Criterion: results are compared across the fsync policies so the default is
// chosen on evidence.
func BenchmarkAppendLatency(b *testing.B) {
	for _, policy := range []SyncPolicy{SyncAlways, SyncGroup, SyncNever} {
		for _, writers := range []int{1, 8} {
			b.Run(fmt.Sprintf("%s/writers=%d", policy, writers), func(b *testing.B) {
				benchAppend(b, policy, writers)
			})
		}
	}
}

func benchAppend(b *testing.B, policy SyncPolicy, writers int) {
	dir := benchTempDir(b)
	lf, err := os.OpenFile(filepath.Join(dir, "a.log"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		b.Fatal(err)
	}
	defer lf.Close()
	if _, err := lf.Write(NewFileHeader(1, 1, 7).AppendTo(nil)); err != nil {
		b.Fatal(err)
	}
	xf, err := os.OpenFile(filepath.Join(dir, "a.idx"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		b.Fatal(err)
	}
	defer xf.Close()

	a := NewActor(&Index{Header: IndexHeader{ThreadID: 1}}, FileHeaderSize, lf, xf, policy)
	defer a.Close()

	n := b.N
	if n < writers {
		n = writers
	}
	lat := make([]time.Duration, n)
	body := &PostBody{Source: "a message of an entirely ordinary length, as most of them are"}

	b.ResetTimer()
	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := w; i < n; i += writers {
				start := time.Now()
				if _, err := a.Append(Append{Type: TypePost, Author: 1, Body: body}); err != nil {
					b.Errorf("append: %v", err)
					return
				}
				lat[i] = time.Since(start)
			}
		}()
	}
	wg.Wait()
	b.StopTimer()

	reportLatency(b, lat)
}

// ---------- 2. warm random-ordinal read ----------

func BenchmarkRandomOrdinalReadWarm(b *testing.B) {
	for _, edited := range []bool{false, true} {
		name := "clean"
		if edited {
			name = "edited"
		}
		b.Run(name, func(b *testing.B) {
			c := getCorpus(b, *benchPosts, edited)
			f, err := os.Open(c.logPath)
			if err != nil {
				b.Fatal(err)
			}
			defer f.Close()

			rec, err := Open(f, c.logSize, c.idxRaw)
			if err != nil {
				b.Fatal(err)
			}
			if rec.Outcome != Trusted {
				b.Fatalf("corpus index was not trusted (%v); the read is measuring a replay", rec.Outcome)
			}
			rd := NewReader(f, rec.Index)

			// Warm the cache deliberately, so this measures a warm read
			// rather than whatever the build happened to leave behind.
			if _, err := rd.Range(1, uint32(rec.Index.Len())); err != nil {
				b.Fatal(err)
			}

			r := rand.New(rand.NewSource(11))
			lat := make([]time.Duration, b.N)
			b.ResetTimer()
			for i := range b.N {
				ord := uint32(1 + r.Intn(rec.Index.Len()))
				start := time.Now()
				if _, err := rd.One(ord); err != nil {
					b.Fatalf("reading ordinal %d: %v", ord, err)
				}
				lat[i] = time.Since(start)
			}
			b.StopTimer()
			reportLatency(b, lat)
		})
	}
}

// A page of posts is the shape the thread endpoint actually asks for, and it
// is where coalescing earns its keep. Measured on a clean thread and an edited
// one, because an edited post breaks the run and pays its own read — so
// measuring only the clean case would flatter the design.
func BenchmarkPageRead(b *testing.B) {
	const page = 25
	for _, edited := range []bool{false, true} {
		name := "clean"
		if edited {
			name = "edited"
		}
		b.Run(name, func(b *testing.B) {
			c := getCorpus(b, *benchPosts, edited)
			f, err := os.Open(c.logPath)
			if err != nil {
				b.Fatal(err)
			}
			defer f.Close()
			rec, err := Open(f, c.logSize, c.idxRaw)
			if err != nil {
				b.Fatal(err)
			}
			rd := NewReader(f, rec.Index)
			if _, err := rd.Range(1, uint32(rec.Index.Len())); err != nil {
				b.Fatal(err)
			}

			r := rand.New(rand.NewSource(13))
			lat := make([]time.Duration, b.N)
			b.ResetTimer()
			for i := range b.N {
				from := uint32(1 + r.Intn(rec.Index.Len()-page))
				start := time.Now()
				if _, err := rd.Range(from, from+page-1); err != nil {
					b.Fatal(err)
				}
				lat[i] = time.Since(start)
			}
			b.StopTimer()
			reportLatency(b, lat)
		})
	}
}

// ---------- 3. cold read ----------

func BenchmarkRandomOrdinalReadCold(b *testing.B) {
	if !canDropPageCache() {
		b.Skipf("page cache eviction is unavailable on %s", runtime.GOOS)
	}
	c := getCorpus(b, *benchPosts, false)

	lat := make([]time.Duration, 0, b.N)
	r := rand.New(rand.NewSource(17))
	for range b.N {
		f, err := os.Open(c.logPath)
		if err != nil {
			b.Fatal(err)
		}
		rec, err := Open(f, c.logSize, c.idxRaw)
		if err != nil {
			f.Close()
			b.Fatal(err)
		}
		rd := NewReader(f, rec.Index)
		if err := dropFromPageCache(f); err != nil {
			f.Close()
			b.Skipf("could not evict the page cache: %v", err)
		}

		ord := uint32(1 + r.Intn(rec.Index.Len()))
		start := time.Now()
		_, err = rd.One(ord)
		d := time.Since(start)
		f.Close()
		if err != nil {
			b.Fatalf("reading ordinal %d: %v", ord, err)
		}
		lat = append(lat, d)
	}
	reportLatency(b, lat)
}

// ---------- 4. index rebuild ----------

func BenchmarkIndexRebuild(b *testing.B) {
	targetBytes := int64(*benchRebuildMB) << 20

	// Enough posts to reach the target size, measured rather than guessed.
	probe := getCorpus(b, 2_000, false)
	perPost := float64(probe.logSize-FileHeaderSize) / 2_000
	posts := int(float64(targetBytes) / perPost)

	c := getCorpus(b, posts, false)
	b.Logf("log is %.1f MB over %d posts (%.0f bytes per post)",
		float64(c.logSize)/(1<<20), c.posts, perPost)

	// Warm and cold are different questions. Warm says how fast the parser is;
	// cold says how long an operator actually waits, because a rebuild happens
	// after a crash or a lost sidecar, when nothing is in the page cache.
	for _, cold := range []bool{false, true} {
		name := "warm"
		if cold {
			name = "cold"
			if !canDropPageCache() {
				continue
			}
		}
		b.Run(name, func(b *testing.B) {
			lat := make([]time.Duration, 0, b.N)
			for range b.N {
				f, err := os.Open(c.logPath)
				if err != nil {
					b.Fatal(err)
				}
				if cold {
					if err := dropFromPageCache(f); err != nil {
						f.Close()
						b.Skipf("could not evict the page cache: %v", err)
					}
				}
				start := time.Now()
				// A nil index is what a missing or unusable sidecar looks
				// like, which is the case a rebuild exists for.
				rec, err := Open(f, c.logSize, nil)
				d := time.Since(start)
				f.Close()
				if err != nil {
					b.Fatal(err)
				}
				if rec.Outcome != Rebuilt {
					b.Fatalf("outcome %v, want Rebuilt; this is not measuring a rebuild", rec.Outcome)
				}
				if rec.Index.Len() != c.posts {
					b.Fatalf("rebuilt %d posts, want %d", rec.Index.Len(), c.posts)
				}
				lat = append(lat, d)
			}
			reportLatency(b, lat)
			b.ReportMetric(float64(c.logSize)/(1<<20), "MB")
			if len(lat) > 0 {
				sorted := append([]time.Duration(nil), lat...)
				sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
				med := sorted[len(sorted)/2].Seconds()
				b.ReportMetric(float64(c.logSize)/(1<<20)/med, "MB/s")
			}
		})
	}
}

// reportLatency adds percentiles as custom metrics. A mean is the one statistic
// none of these targets are about.
func reportLatency(b *testing.B, lat []time.Duration) {
	b.Helper()
	if len(lat) == 0 {
		return
	}
	s := append([]time.Duration(nil), lat...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })

	at := func(q float64) float64 {
		i := int(q * float64(len(s)))
		if i >= len(s) {
			i = len(s) - 1
		}
		return float64(s[i].Nanoseconds()) / 1e6 // milliseconds
	}
	b.ReportMetric(at(0.50), "p50ms")
	b.ReportMetric(at(0.95), "p95ms")
	b.ReportMetric(at(0.99), "p99ms")
	b.ReportMetric(float64(s[len(s)-1].Nanoseconds())/1e6, "maxms")
}
