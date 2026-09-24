package db

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// driverImports are the import-path prefixes of SQLite drivers.
var driverImports = []string{
	"github.com/ncruces/go-sqlite3",
	"modernc.org/sqlite",
	"github.com/mattn/go-sqlite3",
	"zombiezen.com/go/sqlite",
	"crawshaw.io/sqlite",
}

// TestOnlyDriverGoImportsADriver is AC7. The rule "SQL stays driver-agnostic"
// holds only while one file knows which driver is in use: modernc stays a
// drop-in fallback (docs/09) exactly as long as switching means editing
// driver.go and nothing else. Test files count too, since a test that reaches
// into the driver is a test that breaks on the switch.
//
// spikes/ is skipped: those are separate modules that exist to compare drivers.
func TestOnlyDriverGoImportsADriver(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected the module root at %s: %v", root, err)
	}
	allowed := filepath.Join(root, "internal", "store", "db", "driver.go")

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
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		checked++
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			for _, d := range driverImports {
				if (p == d || strings.HasPrefix(p, d+"/")) && path != allowed {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s imports %s. Only internal/store/db/driver.go may import a SQLite driver; "+
						"everything else goes through database/sql so the driver can be swapped.", rel, p)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 50 {
		t.Fatalf("checked only %d Go files; the walk is not seeing the module", checked)
	}
}
