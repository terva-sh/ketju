package respcache

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/terva-sh/ketju/internal/render"
)

func key(n int, class string) Key {
	return Key{Thread: 1, LogLength: int64(n), Page: 1, PageSize: 25, Renderer: render.Version, Class: class}
}

// TestEveryKeyFieldSeparates: two keys differing in any one field are two
// entries, so no field is decorative.
func TestEveryKeyFieldSeparates(t *testing.T) {
	base := Key{Thread: 7, LogLength: 100, Projected: 100, Page: 2, PageSize: 25, Lens: "",
		Renderer: render.Version, Class: "member", Authors: [32]byte{1}}
	variants := map[string]func(*Key){
		"thread":     func(k *Key) { k.Thread++ },
		"log length": func(k *Key) { k.LogLength++ },
		"projected":  func(k *Key) { k.Projected-- },
		"page":       func(k *Key) { k.Page++ },
		"page size":  func(k *Key) { k.PageSize = 10 },
		"lens":       func(k *Key) { k.Lens = "author:drew" },
		"selection":  func(k *Key) { k.Selection[0] = 1 },
		"renderer":   func(k *Key) { k.Renderer++ },
		"class":      func(k *Key) { k.Class = "moderator" },
		"authors":    func(k *Key) { k.Authors[0] = 2 },
	}
	c := New(1 << 20)
	c.Put(base, []byte(`["base"]`), render.Deps{})
	for name, change := range variants {
		k := base
		change(&k)
		if _, ok := c.Get(k, nil); ok {
			t.Errorf("a key differing only in %s hit the base entry", name)
		}
	}
	if b, ok := c.Get(base, nil); !ok || string(b) != `["base"]` {
		t.Fatalf("the base key got %q, %v", b, ok)
	}
}

// TestBoundedInBytes is AC3: the cache never holds more than its bound,
// evicts least recently used first, refuses an entry larger than the bound,
// and its hit ratio is the counted hits over lookups.
func TestBoundedInBytes(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 1000)
	per := int64(len(body)) + entryOverhead
	c := New(per * 3)
	for i := range 5 {
		c.Put(key(i, "anon"), body, render.Deps{})
		if s := c.Stats(); s.Bytes > s.Max {
			t.Fatalf("after %d puts %d bytes against a bound of %d", i+1, s.Bytes, s.Max)
		}
	}
	// 0 and 1 are gone; 2, 3, 4 remain. Touch 2 so 3 is next out.
	for i, want := range []bool{false, false, true, true, true} {
		if _, ok := c.Get(key(i, "anon"), nil); ok != want {
			t.Errorf("entry %d present=%v, want %v", i, ok, want)
		}
	}
	c.Get(key(2, "anon"), nil)
	c.Put(key(5, "anon"), body, render.Deps{})
	if _, ok := c.Get(key(3, "anon"), nil); ok {
		t.Error("the least recently used entry survived an eviction")
	}
	if _, ok := c.Get(key(2, "anon"), nil); !ok {
		t.Error("a recently used entry was evicted")
	}
	c.Put(key(9, "anon"), bytes.Repeat([]byte("y"), int(per*3)), render.Deps{})
	if _, ok := c.Get(key(9, "anon"), nil); ok {
		t.Error("an entry larger than the whole bound was stored")
	}

	s := c.Stats()
	if s.Entries != 3 || s.Bytes != 3*per || s.Evictions != 3 || s.Rejected != 1 || s.Inserts != 6 {
		t.Errorf("stats %+v: want 3 entries of %d bytes, 3 evictions, 1 rejected, 6 inserts", s, per)
	}
	// Lookups: 5 in the loop (3 hits), then 2 (hit), 3 (miss), 2 (hit), 9 (miss).
	if s.Hits != 5 || s.Misses != 4 || s.HitRatio() != 5.0/9.0 {
		t.Errorf("hits %d misses %d ratio %v: want 5, 4, 5/9", s.Hits, s.Misses, s.HitRatio())
	}
}

// TestReplacingAnEntryKeepsTheCount: a second Put under one key replaces
// the entry and charges only the difference.
func TestReplacingAnEntryKeepsTheCount(t *testing.T) {
	c := New(1 << 20)
	c.Put(key(1, "anon"), []byte("aaaa"), render.Deps{})
	c.Put(key(1, "anon"), []byte("bb"), render.Deps{})
	if s := c.Stats(); s.Entries != 1 || s.Bytes != 2+entryOverhead || s.Inserts != 1 {
		t.Fatalf("stats %+v", s)
	}
	if b, _ := c.Get(key(1, "anon"), nil); string(b) != "bb" {
		t.Fatalf("got %q", b)
	}
}

// TestStaleDependenciesMiss: an entry whose dependencies the caller no
// longer accepts is a miss and counted stale; the caller's next Put replaces
// it.
func TestStaleDependenciesMiss(t *testing.T) {
	c := New(1 << 20)
	deps := render.Deps{Handles: []render.HandleLookup{{Typed: "dana"}}}
	c.Put(key(1, "member"), []byte("[]"), deps)
	var seen render.Deps
	if _, ok := c.Get(key(1, "member"), func(d render.Deps) bool { seen = d; return false }); ok {
		t.Fatal("a rejected entry was served")
	}
	if len(seen.Handles) != 1 || seen.Handles[0].Typed != "dana" {
		t.Fatalf("the check saw %+v", seen)
	}
	if s := c.Stats(); s.Stale != 1 || s.Misses != 1 || s.Hits != 0 {
		t.Fatalf("stats %+v", s)
	}
	if _, ok := c.Get(key(1, "member"), func(render.Deps) bool { return true }); !ok {
		t.Fatal("an accepted entry missed")
	}
}

// TestPutCopies: the caller's buffer is not the stored one.
func TestPutCopies(t *testing.T) {
	c := New(1 << 20)
	b := []byte(`["one"]`)
	c.Put(key(1, "anon"), b, render.Deps{})
	copy(b, `["two"]`)
	if got, _ := c.Get(key(1, "anon"), nil); string(got) != `["one"]` {
		t.Fatalf("the stored body followed the caller's buffer: %q", got)
	}
}

// TestDisabled: a zero bound stores nothing and a nil cache is safe.
func TestDisabled(t *testing.T) {
	for _, c := range []*Cache{New(0), NewMB(0), New(-1), nil} {
		c.Put(key(1, "anon"), []byte("[]"), render.Deps{})
		if _, ok := c.Get(key(1, "anon"), nil); ok || c.Enabled() {
			t.Errorf("a disabled cache served an entry")
		}
		if s := c.Stats(); s.Entries != 0 {
			t.Errorf("a disabled cache holds %d entries", s.Entries)
		}
	}
	if NewMB(2).Stats().Max != 2<<20 {
		t.Error("NewMB does not convert megabytes")
	}
}

// TestConcurrentUse runs readers and writers of several classes together
// under -race, and checks that a class's lookup only ever returns that
// class's body.
func TestConcurrentUse(t *testing.T) {
	c := New(64 << 10)
	var wg sync.WaitGroup
	for _, class := range []string{"anon", "member", "moderator"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 2000 {
				k := key(i%50, class)
				want := fmt.Sprintf("%s-%d", class, i%50)
				if b, ok := c.Get(k, func(render.Deps) bool { return true }); ok && string(b) != want {
					t.Errorf("%s got %q", want, b)
					return
				}
				c.Put(k, []byte(want), render.Deps{})
				_ = c.Stats()
			}
		}()
	}
	wg.Wait()
	if s := c.Stats(); s.Bytes > s.Max {
		t.Fatalf("%d bytes against a bound of %d", s.Bytes, s.Max)
	}
}

// TestNoInvalidation is AC1's structural half: the package exports nothing
// that forgets an entry, so no caller can have been written to depend on
// being told. A mutation reaches the cache only by moving the key.
func TestNoInvalidation(t *testing.T) {
	fset := token.NewFileSet()
	var exported []string
	err := filepath.WalkDir(".", func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.IsExported() {
				exported = append(exported, fn.Name.Name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(exported)
	// Counters only reads (TKT-01M29CEE7S).
	want := []string{"Counters", "Enabled", "Get", "HitRatio", "New", "NewMB", "Put", "Stats"}
	if !slices.Equal(exported, want) {
		t.Fatalf("respcache exports %v; want exactly %v. A method that forgets entries is invalidation, "+
			"which TKT-01M29CCBY0 rules out: put what changed into the key instead", exported, want)
	}
}
