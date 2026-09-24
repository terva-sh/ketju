package settings

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

// TestWritesGoThroughSet is how "an unknown key is rejected rather than
// stored" holds for every writer and not only for callers who remember:
// PutSetting may be called only here, where Set checks the key and the value
// first and writes the audit row. Test files are exempt, since a fixture
// writing around Set is how TestStoredValueThatNoLongerDecodes works.
func TestWritesGoThroughSet(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected the module root at %s: %v", root, err)
	}
	allowed := []string{
		filepath.Join(root, "internal", "settings") + string(filepath.Separator),
		filepath.Join(root, "internal", "store", "db") + string(filepath.Separator),
	}
	fset := token.NewFileSet()
	checked := 0
	err = filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
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
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		for _, a := range allowed {
			if strings.HasPrefix(path, a) {
				return nil
			}
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		checked++
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "PutSetting" {
					t.Errorf("%s calls PutSetting; settings are written through settings.Set, "+
						"which refuses unknown keys and ill-typed values and writes the audit row",
						fset.Position(call.Pos()))
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("no files checked")
	}
}
