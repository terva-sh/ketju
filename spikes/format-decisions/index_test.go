package format

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/fxamacker/cbor/v2"
)

// The index width question is not really about 24 bytes of memory. It is about
// whether an author, chain, or since lens can be answered from the in-memory
// index alone, or whether it has to go back to the log (or the database) for
// every post it filters. These benchmarks measure that difference on a 50k
// post thread, which docs/03 calls the largest plausible one.

const nPosts = 50000

type builtLog struct {
	path    string
	e40     []Entry40
	e16     []Entry16
	authors []uint64
	size    int64
}

var (
	buildOnce sync.Once
	built     *builtLog
	buildErr  error
)

func getLog(tb testing.TB) *builtLog {
	tb.Helper()
	buildOnce.Do(func() {
		built, buildErr = buildLog(spikeDir(tb), nPosts)
	})
	if buildErr != nil {
		tb.Fatal(buildErr)
	}
	return built
}

func buildLog(dir string, n int) (*builtLog, error) {
	path := filepath.Join(dir, "index-bench.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)

	r := rand.New(rand.NewSource(11))
	// A thread with 40 participants, unevenly active, which is what a small
	// community thread looks like.
	participants := make([]uint64, 40)
	for i := range participants {
		participants[i] = uint64(1000 + i)
	}

	out := &builtLog{path: path}
	out.e40 = make([]Entry40, 0, n)
	out.e16 = make([]Entry16, 0, n)
	out.authors = make([]uint64, 0, n)

	var off int64 = 64 // file header
	if _, err := w.Write(make([]byte, 64)); err != nil {
		return nil, err
	}
	ts := time.Now().Add(-30 * 24 * time.Hour).UnixMilli()
	for i := 0; i < n; i++ {
		// Zipf-ish: a few people write most of the posts.
		idx := int(r.ExpFloat64() * 6)
		if idx >= len(participants) {
			idx = r.Intn(len(participants))
		}
		author := participants[idx]

		target := 300 + r.Intn(3500)
		body, err := cbor.Marshal(GenBody(r, target))
		if err != nil {
			return nil, err
		}
		var replyTo uint32
		if i > 0 && r.Intn(2) == 0 {
			replyTo = uint32(1 + r.Intn(i))
		}
		frame := EncodeFrame(2, 1, 0, uint32(i+1), 1, ts, author, body)
		if _, err := w.Write(frame); err != nil {
			return nil, err
		}
		out.e40 = append(out.e40, Entry40{
			Offset: off, Length: uint32(len(frame)), ReplyTo: replyTo,
			Author: author, Created: ts, Revision: 1,
		})
		out.e16 = append(out.e16, Entry16{Offset: off, Length: uint32(len(frame))})
		out.authors = append(out.authors, author)
		off += int64(len(frame))
		ts += int64(1000 + r.Intn(120000))
	}
	if err := w.Flush(); err != nil {
		return nil, err
	}
	out.size = off
	return out, nil
}

// BenchmarkAuthorLens40 filters by author from the in-memory index alone.
func BenchmarkAuthorLens40(b *testing.B) {
	lg := getLog(b)
	target := lg.authors[0]
	b.ReportAllocs()
	for b.Loop() {
		hits := 0
		for i := range lg.e40 {
			if lg.e40[i].Author == target {
				hits++
			}
		}
		if hits == 0 {
			b.Fatal("no hits")
		}
	}
}

// BenchmarkAuthorLens16 filters by author when the entry carries only an
// offset, so the author has to come from the log. The log is warm in the page
// cache here, which flatters this path: cold, it is far worse.
func BenchmarkAuthorLens16(b *testing.B) {
	lg := getLog(b)
	f, err := os.Open(lg.path)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	target := lg.authors[0]
	prefix := make([]byte, FrameFixed)
	b.ReportAllocs()
	for b.Loop() {
		hits := 0
		for i := range lg.e16 {
			if _, err := f.ReadAt(prefix, lg.e16[i].Offset); err != nil {
				b.Fatal(err)
			}
			if AuthorAt(prefix) == target {
				hits++
			}
		}
		if hits == 0 {
			b.Fatal("no hits")
		}
	}
}

// TestIndexWidthReport prints the sizing and the rebuild cost that the width
// decision trades against.
func TestIndexWidthReport(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 100 MB log")
	}
	lg := getLog(t)

	s40 := unsafe.Sizeof(Entry40{})
	s16 := unsafe.Sizeof(Entry16{})
	fmt.Println("\n=== index sizing for a 50k post thread ===")
	fmt.Printf("log size            %.1f MB\n", float64(lg.size)/(1<<20))
	fmt.Printf("entry width         %d B (full) vs %d B (offset only)\n", s40, s16)
	fmt.Printf("in-memory index     %.2f MB vs %.2f MB  (difference %.2f MB)\n",
		float64(uintptr(nPosts)*s40)/(1<<20),
		float64(uintptr(nPosts)*s16)/(1<<20),
		float64(uintptr(nPosts)*(s40-s16))/(1<<20))
	fmt.Printf("512 open threads at 50 posts each: %.2f MB vs %.2f MB\n",
		float64(512*50*s40)/(1<<20), float64(512*50*s16)/(1<<20))

	// Rebuild: walk every frame header without decoding a body, which is what
	// the reindexer does. Informational for the freeze; TKT-01M29C2WV6 owns
	// the real benchmark.
	f, err := os.Open(lg.path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, 1<<20)
	start := time.Now()
	var off int64 = 64
	var frames int
	for off < lg.size {
		n, err := f.ReadAt(buf, off)
		if n == 0 && err != nil {
			break
		}
		p := 0
		for p+FrameFixed <= n {
			l := int(uint32(buf[p]) | uint32(buf[p+1])<<8 | uint32(buf[p+2])<<16 | uint32(buf[p+3])<<24)
			total := 4 + l + 4
			if p+total > n {
				break
			}
			frames++
			p += total
		}
		if p == 0 {
			break
		}
		off += int64(p)
	}
	elapsed := time.Since(start)
	fmt.Printf("header-only walk    %d frames in %v (%.0f MB/s)\n",
		frames, elapsed.Round(time.Millisecond),
		float64(lg.size)/(1<<20)/elapsed.Seconds())
	if frames != nPosts {
		t.Fatalf("walked %d frames, want %d", frames, nPosts)
	}
}
