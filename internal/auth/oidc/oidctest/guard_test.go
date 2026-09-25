package oidctest

import (
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// An IdP that approves anyone is a test fixture, and a way in if it were
// ever mounted by the server: nothing but a test may import this package,
// and a file whose build constraint leaves it out of every build without
// the e2e tag, which is ketju's Playwright build and is never released
// (TKT-01M29CGBY1).
func TestOnlyTestsImportTheFakeIdP(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected the module root at %s: %v", root, err)
	}
	const self = "github.com/terva-sh/ketju/internal/auth/oidc/oidctest"
	fset := token.NewFileSet()
	seen := 0
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
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly|parser.ParseComments)
		if err != nil {
			return err
		}
		seen++
		if e2eOnly(f) {
			return nil
		}
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == self {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s imports the fake IdP outside a test", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen < 50 {
		t.Fatalf("checked only %d files; the walk is not seeing the module", seen)
	}
}

// e2eOnly reports whether f's //go:build line excludes it from every build
// that lacks the e2e tag, whatever other tags are set.
func e2eOnly(f *ast.File) bool {
	for _, g := range f.Comments {
		if g.Pos() > f.Package {
			break
		}
		for _, c := range g.List {
			if !constraint.IsGoBuild(c.Text) {
				continue
			}
			x, err := constraint.Parse(c.Text)
			if err != nil {
				return false
			}
			return !x.Eval(func(tag string) bool { return tag != "e2e" })
		}
	}
	return false
}

func TestE2EOnly(t *testing.T) {
	for src, want := range map[string]bool{
		"//go:build e2e\n\npackage p\n":          true,
		"//go:build e2e && linux\n\npackage p\n": true,
		"//go:build e2e || linux\n\npackage p\n": false,
		"//go:build !e2e\n\npackage p\n":         false,
		"//go:build linux\n\npackage p\n":        false,
		"// e2e\npackage p\n":                    false,
		"package p\n\n//go:build e2e\n":          false,
	} {
		f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, parser.ImportsOnly|parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		if got := e2eOnly(f); got != want {
			t.Errorf("%q: e2eOnly = %v, want %v", src, got, want)
		}
	}
}
