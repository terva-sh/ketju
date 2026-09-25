package oidctest

import (
	"go/ast"
	"go/build/constraint"
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
// that lacks the e2e tag, whatever other tags are set: with e2e false, no
// assignment of the other tags the line names satisfies it. Setting them
// all true is not enough to show that, since a tag can be negated
// (review on PR #82: e2e || (linux && !darwin)).
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
			return !satisfiableWithout(x, "e2e")
		}
	}
	return false
}

// satisfiableWithout reports whether some assignment of x's tags, with tag
// false, makes x true. A line naming too many tags to try every assignment
// counts as satisfiable, which fails the guard rather than passing it.
func satisfiableWithout(x constraint.Expr, tag string) bool {
	var tags []string
	var walk func(constraint.Expr)
	walk = func(e constraint.Expr) {
		switch e := e.(type) {
		case *constraint.TagExpr:
			if e.Tag != tag && !slices.Contains(tags, e.Tag) {
				tags = append(tags, e.Tag)
			}
		case *constraint.NotExpr:
			walk(e.X)
		case *constraint.AndExpr:
			walk(e.X)
			walk(e.Y)
		case *constraint.OrExpr:
			walk(e.X)
			walk(e.Y)
		}
	}
	walk(x)
	if len(tags) > 16 {
		return true
	}
	for set := 0; set < 1<<len(tags); set++ {
		if x.Eval(func(t string) bool {
			i := slices.Index(tags, t)
			return i >= 0 && set&(1<<i) != 0
		}) {
			return true
		}
	}
	return false
}

func TestE2EOnly(t *testing.T) {
	for src, want := range map[string]bool{
		"//go:build e2e\n\npackage p\n":                               true,
		"//go:build e2e && linux\n\npackage p\n":                      true,
		"//go:build e2e || linux\n\npackage p\n":                      false,
		"//go:build !e2e\n\npackage p\n":                              false,
		"//go:build linux\n\npackage p\n":                             false,
		"//go:build e2e || (linux && !darwin)\n\npackage p\n":         false,
		"//go:build e2e || !linux\n\npackage p\n":                     false,
		"//go:build e2e && !linux\n\npackage p\n":                     true,
		"//go:build (e2e || linux) && (e2e || !linux)\n\npackage p\n": true,
		"// e2e\npackage p\n":                                         false,
		"package p\n\n//go:build e2e\n":                               false,
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
