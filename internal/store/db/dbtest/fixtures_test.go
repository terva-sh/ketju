package dbtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// migrateAllowed are the directories whose tests may still call Migrate, as
// settled in grooming (TKT-01M3R5P3CN):
//   - internal/store/db, whose tests test Migrate itself and could not import
//     this package without a cycle;
//   - internal/store/db/dbtest, whose dump tests and template tests use a
//     fresh migration as the reference the template is held to;
//   - cmd/ketju, whose tests exercise serve's real startup.
var migrateAllowed = []string{
	filepath.Join("internal", "store", "db"),
	filepath.Join("internal", "store", "db", "dbtest"),
	filepath.Join("cmd", "ketju"),
}

// TestFixturesUseTheTemplate keeps new fixtures on the template. A test that
// migrates its own database pays for every migration under -race, once per
// test, and that cost is what ran internal/perm past Go's package timeout on
// a loaded runner. It fails on any .Migrate( call in a _test.go file outside
// migrateAllowed, naming the file and line; Open is the replacement.
//
// It walks the module from its root, in the style of internal/store/db's
// TestOnlyDriverGoImportsADriver, and skips any directory holding a go.mod of
// its own (spikes/, and any checkout nested in this one), since that is
// another module.
func TestFixturesUseTheTemplate(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected the module root at %s: %v", root, err)
	}
	fset := token.NewFileSet()
	checked, allowedCalls := 0, 0
	err = filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			if path == root {
				return nil
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "testdata" {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		checked++
		rel, _ := filepath.Rel(root, path)
		dir := filepath.Dir(rel)
		allowed := false
		for _, a := range migrateAllowed {
			allowed = allowed || dir == a
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Migrate" {
				return true
			}
			if allowed {
				allowedCalls++
				return true
			}
			t.Errorf("%s:%d calls .Migrate. Open the database with dbtest.Open, which copies a template "+
				"migrated once per test binary, instead of migrating it again for this test (TKT-01M3R5P3CN).",
				rel, fset.Position(call.Pos()).Line)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 100 {
		t.Fatalf("checked only %d test files; the walk is not seeing the module", checked)
	}
	// The allowed directories do call Migrate; finding none means the
	// matcher, not the module, has changed.
	if allowedCalls == 0 {
		t.Fatal("found no .Migrate call even in the directories allowed one; the matcher is not seeing calls")
	}
}
