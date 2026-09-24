package render

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// goldmarkImporters walks the Go files under root and returns those outside
// root's internal/render that import goldmark, and how many inside do.
func goldmarkImporters(t *testing.T, root string) (outside []string, inside int) {
	t.Helper()
	self := filepath.Join(root, "internal", "render")
	fset := token.NewFileSet()
	seen := 0
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			switch e.Name() {
			case ".git", "spikes", "node_modules", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		seen++
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if p != "github.com/yuin/goldmark" && !strings.HasPrefix(p, "github.com/yuin/goldmark/") {
				continue
			}
			if filepath.Dir(path) == self {
				inside++
				continue
			}
			rel, _ := filepath.Rel(root, path)
			outside = append(outside, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen == 0 {
		t.Fatalf("no Go files under %s", root)
	}
	return outside, inside
}

// AC4 of TKT-01M29C2WWG: nothing but this package parses markdown. The
// projection writer, the notifier and the search indexer take mentions,
// quotes and reply_to from a Result, rather than each parsing the source in
// its own slightly different way, and the only way to parse it is goldmark.
func TestOnlyRenderImportsGoldmark(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected the module root at %s: %v", root, err)
	}
	outside, inside := goldmarkImporters(t, root)
	for _, rel := range outside {
		t.Errorf("%s imports goldmark: only internal/render parses markdown. "+
			"Take mentions, quotes and reply_to from render.Result instead.", rel)
	}
	if inside == 0 {
		t.Fatal("found goldmark nowhere in internal/render; the walk is not seeing the module")
	}
}

// The walk is live: a tree with goldmark imported beside the renderer finds
// that import, in a test file too, and not the renderer's own.
func TestTheGoldmarkWalkFindsAStrayImport(t *testing.T) {
	outside, inside := goldmarkImporters(t, filepath.Join("testdata", "layering"))
	slices.Sort(outside)
	if want := []string{"internal/notify/notify.go", "internal/notify/notify_test.go"}; !slices.Equal(outside, want) || inside != 1 {
		t.Errorf("outside %v, inside %d; want %v and 1", outside, inside, want)
	}
}
