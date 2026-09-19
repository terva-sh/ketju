package dto

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// modulePath is this repository's module, from go.mod.
const modulePath = "github.com/terva-sh/ketju"

// TestTheDTOPackageImportsNothingOfOurs is AC1's structural half.
//
// The criterion asks that these types live in one package that neither store
// code nor handler code duplicates. Half of that cannot be tested yet, because
// no handler exists to check for a duplicate shape. What can be tested is the
// property that makes the sharing possible at all: this package sits below
// everything, so there is no direction in which a dependency could point back.
//
// An import of internal/store here would be the first step of the failure the
// criterion describes -- a DTO shaped around a row rather than around what a
// client needs, at which point the HTML client inherits the database's
// vocabulary and "shared" quietly becomes "coupled". An import of internal/web
// would be worse: an import cycle waiting for the handler that returns one of
// these.
//
// Test files are exempt. The corpus is a test file and a future one may well
// want a helper from elsewhere; what must stay clean is what ships.
func TestTheDTOPackageImportsNothingOfOurs(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parsing the package: %v", err)
	}

	found := false
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			found = true
			for _, imp := range file.Imports {
				path, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					t.Fatalf("%s: unquoting %s: %v", name, imp.Path.Value, err)
				}
				if strings.HasPrefix(path, modulePath) {
					t.Errorf("%s imports %s. These types are shared by every layer above, so they must not depend on any of them.",
						filepath.Base(name), path)
				}
			}
		}
	}
	if !found {
		t.Fatal("no non-test files parsed, so this test is asserting nothing")
	}
}
