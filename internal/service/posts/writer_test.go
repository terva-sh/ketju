package posts

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestHoldGivesUpOnAStalledAppend (Terva review 1943): an append that
// stalls mid-write makes a pending Hold give up at its deadline, the
// appends that queued behind the Hold then go on, the stalled one finishes
// when it is let go, and a second Hold then succeeds.
func TestHoldGivesUpOnAStalledAppend(t *testing.T) {
	var g gate
	g.enter() // the stalled append

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	type held struct {
		release func()
		err     error
		took    time.Duration
	}
	result := make(chan held, 1)
	start := time.Now()
	go func() {
		r, err := g.hold(ctx)
		result <- held{r, err, time.Since(start)}
	}()
	// Appends that arrive while the Hold is pending queue behind it.
	waitHeld(t, &g)
	var queued sync.WaitGroup
	var done atomic.Int32
	for range 3 {
		queued.Add(1)
		go func() {
			defer queued.Done()
			g.enter()
			done.Add(1)
			g.leave()
		}()
	}
	h := <-result
	if !errors.Is(h.err, context.DeadlineExceeded) {
		t.Fatalf("Hold over a stalled append = %v, want DeadlineExceeded", h.err)
	}
	if h.took > 2*time.Second {
		t.Errorf("Hold gave up after %s; its deadline was 100ms", h.took)
	}
	waitFor(t, "the queued appends to go on", func() bool { return done.Load() == 3 })
	queued.Wait()

	g.leave() // the stalled append finishes
	r, err := g.hold(context.Background())
	if err != nil {
		t.Fatalf("a second Hold = %v", err)
	}
	r()
	r() // release twice is harmless
}

// TestHoldWaitsForInFlightAppends: an append in flight when Hold is called
// finishes before Hold returns, and none starts between Hold and release.
func TestHoldWaitsForInFlightAppends(t *testing.T) {
	var g gate
	var inside, violations atomic.Int32
	var holding atomic.Bool
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				g.enter()
				inside.Add(1)
				if holding.Load() {
					violations.Add(1)
				}
				time.Sleep(50 * time.Microsecond)
				inside.Add(-1)
				g.leave()
			}
		}()
	}
	for range 50 {
		release, err := g.hold(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		holding.Store(true)
		if n := inside.Load(); n != 0 {
			t.Errorf("Hold returned with %d append(s) in flight", n)
		}
		if _, err := g.hold(context.Background()); !errors.Is(err, ErrHeld) {
			t.Errorf("a second Hold while one is in force = %v, want ErrHeld", err)
		}
		time.Sleep(200 * time.Microsecond)
		holding.Store(false)
		release()
	}
	close(stop)
	wg.Wait()
	if n := violations.Load(); n != 0 {
		t.Errorf("%d append(s) started while appends were held", n)
	}
}

// TestHoldStopsARealAppend: through Writer.Append, an append made while a
// Hold is in force lands only after release.
func TestHoldStopsARealAppend(t *testing.T) {
	c, err := threadcache.New(t.TempDir(), threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	id := threadid.ID(1_700_000_000_000<<16 | 1)
	th, err := c.Create(id, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Release(th)
	var w Writer
	release, err := w.Hold(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	appended := make(chan error, 1)
	go func() {
		_, err := w.Append(th, threadlog.Append{Type: threadlog.TypeThreadCreated, Author: 1, TimeMS: 1,
			Body: &threadlog.ThreadCreatedBody{Title: "T", Slug: "t", ForumID: 1}})
		appended <- err
	}()
	select {
	case err := <-appended:
		t.Fatalf("an append landed while held: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if n := th.Actor.LogLength(); n != threadlog.FileHeaderSize {
		t.Fatalf("the log grew to %d while held", n)
	}
	release()
	if err := <-appended; err != nil {
		t.Fatal(err)
	}
}

func waitHeld(t *testing.T, g *gate) {
	t.Helper()
	waitFor(t, "the Hold to take effect", func() bool {
		g.mu.Lock()
		defer g.mu.Unlock()
		return g.held
	})
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// appendAllowed are the files that may call an actor's Append directly.
// Writer is the seam; e2e.go seeds a data directory for the browser tests
// before serve starts, and is never served.
var appendAllowed = []string{
	"internal/service/posts/writer.go",
	"cmd/ketju/e2e.go",
}

// TestEveryAppendGoesThroughTheWriter: no file outside appendAllowed calls
// .Actor.Append, so every append the server makes passes the gate a backup
// holds, and publishes its event (TKT-01M29CXK35). It walks the non-test Go
// files of the module, as the session chokepoint test does.
func TestEveryAppendGoesThroughTheWriter(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("not at the module root: %v", err)
	}
	var found []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if e.IsDir() {
			if rel != "." && (strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules" || e.Name() == "testdata") {
				return filepath.SkipDir
			}
			if rel != "." {
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir // another module: spikes/
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Append" {
				return true
			}
			inner, ok := sel.X.(*ast.SelectorExpr)
			if ok && inner.Sel.Name == "Actor" && !slices.Contains(appendAllowed, rel) {
				found = append(found, fset.Position(sel.Pos()).String())
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Errorf("thread appends outside posts.Writer, which a backup's hold would not stop:\n  %s",
			strings.Join(found, "\n  "))
	}
}
