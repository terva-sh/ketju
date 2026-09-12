package format

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// Post sizes to measure. docs/03 assumes a 2 KB average post; the short and
// long ends bracket what a real thread holds.
var sizes = []struct {
	name   string
	target int
}{
	{"short_300B", 300},
	{"typical_2KB", 2048},
	{"long_8KB", 8192},
}

func bodies(target int, n int) []PostBody {
	r := rand.New(rand.NewSource(1))
	out := make([]PostBody, n)
	for i := range out {
		out[i] = GenBody(r, target)
	}
	return out
}

func BenchmarkEncode(b *testing.B) {
	for _, s := range sizes {
		set := bodies(s.target, 64)
		b.Run("cbor/"+s.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				if _, err := cbor.Marshal(set[i%len(set)]); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("json/"+s.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				if _, err := json.Marshal(set[i%len(set)]); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecode(b *testing.B) {
	for _, s := range sizes {
		set := bodies(s.target, 64)
		cb := make([][]byte, len(set))
		jb := make([][]byte, len(set))
		for i, p := range set {
			cb[i], _ = cbor.Marshal(p)
			jb[i], _ = json.Marshal(p)
		}
		b.Run("cbor/"+s.name, func(b *testing.B) {
			b.ReportAllocs()
			var out PostBody
			for i := 0; b.Loop(); i++ {
				if err := cbor.Unmarshal(cb[i%len(cb)], &out); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("json/"+s.name, func(b *testing.B) {
			b.ReportAllocs()
			var out PostBody
			for i := 0; b.Loop(); i++ {
				if err := json.Unmarshal(jb[i%len(jb)], &out); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// TestEncodedSize reports on-disk cost, which is the other half of the
// encoding decision and the half a ns/op number hides.
func TestEncodedSize(t *testing.T) {
	fmt.Println("\n=== encoded body size (mean over 256 bodies) ===")
	fmt.Printf("%-14s %10s %10s %10s %8s\n", "size", "source", "cbor", "json", "json/cbor")
	for _, s := range sizes {
		set := bodies(s.target, 256)
		var src, c, j int
		for _, p := range set {
			src += len(p.Source)
			cb, err := cbor.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			jb, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			c += len(cb)
			j += len(jb)
		}
		n := len(set)
		fmt.Printf("%-14s %10d %10d %10d %8.3f\n",
			s.name, src/n, c/n, j/n, float64(j)/float64(c))
	}

	// Round-trip check, so the numbers above are for encoders that work.
	r := rand.New(rand.NewSource(7))
	p := GenBody(r, 2048)
	for _, tc := range []struct {
		name      string
		marshal   func(any) ([]byte, error)
		unmarshal func([]byte, any) error
	}{
		{"cbor", cbor.Marshal, cbor.Unmarshal},
		{"json", json.Marshal, json.Unmarshal},
	} {
		raw, err := tc.marshal(p)
		if err != nil {
			t.Fatalf("%s marshal: %v", tc.name, err)
		}
		var got PostBody
		if err := tc.unmarshal(raw, &got); err != nil {
			t.Fatalf("%s unmarshal: %v", tc.name, err)
		}
		if got.Source != p.Source || got.ReplyTo != p.ReplyTo || len(got.Mentions) != len(p.Mentions) {
			t.Fatalf("%s round trip lost data", tc.name)
		}
	}
}
