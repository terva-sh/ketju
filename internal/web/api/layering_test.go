package api

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The layering fence (TKT-01M3YME9DM). A handler here parses a request,
// calls a service and writes the answer; it does not read a store itself
// (TKT-01M29BKBMC, criterion 1). The package has to import
// internal/auth/session, to read the request's session and to clear the
// cookie, so a ban on that import is impossible. What is banned is what
// reaches a store:
//
//   - an import under internal/store, other than threadid (id parsing) and
//     db/views (the cross-list enum);
//   - an import of database/sql;
//   - a reference to the type session.Store, except in the Deps type, where
//     serve hands the store in, and in build, which wires it into
//     identity.Service. The exemption is the Deps type alone, not a type
//     declared beside it in one type ( ... ) group;
//   - a call through a field of that type (h.sessions.ListForUser), except
//     in build;
//   - a selector .DB, except in build, which reads the database handle off
//     the users service to construct the services it wires.
//
// The scan reads source, not types: golang.org/x/tools is not a dependency,
// and none of the rules needs a type. Test files are exempt, as the fixtures
// a handler test builds need the database.

const layeringModule = "github.com/terva-sh/ketju"

// storeImportsAllowed are the store packages a handler may import: neither
// reaches a database.
var storeImportsAllowed = []string{
	layeringModule + "/internal/store/threadid",
	layeringModule + "/internal/store/db/views",
}

const sessionImport = layeringModule + "/internal/auth/session"

// wiring is the one function allowed to touch a store, and wiringType the
// one type allowed to name session.Store.
const (
	wiring     = "build"
	wiringType = "Deps"
)

// layeringFinding is one reach into a store, as "file:line: what".
type layeringFinding struct {
	rule string
	at   string
	what string
}

func (f layeringFinding) String() string { return f.at + ": " + f.what }

// The layering rules, as the findings name them.
const (
	ruleStoreImport = "store-import"
	ruleSQLImport   = "sql-import"
	ruleStoreType   = "store-type"
	ruleStoreCall   = "store-call"
	ruleDBSelector  = "db-selector"
)

// scanLayering applies the rules to the non-test Go files in dir, and
// returns what it found and how many files it parsed.
func scanLayering(dir string) ([]layeringFinding, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, 0, err
		}
		files = append(files, f)
	}

	var out []layeringFinding
	at := func(p token.Pos) string {
		pos := fset.Position(p)
		return fmt.Sprintf("%s:%d", filepath.Base(pos.Filename), pos.Line)
	}

	// The fields declared with a session.Store type, by name, across the
	// package: a call through one of them is a call into the store.
	storeFields := map[string]bool{}
	for _, f := range files {
		local := sessionName(f)
		if local == "" {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			st, ok := n.(*ast.StructType)
			if !ok {
				return true
			}
			for _, fld := range st.Fields.List {
				if isStoreType(fld.Type, local) {
					for _, nm := range fld.Names {
						storeFields[nm.Name] = true
					}
				}
			}
			return true
		})
	}

	for _, f := range files {
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return nil, 0, err
			}
			switch {
			case path == "database/sql":
				out = append(out, layeringFinding{ruleSQLImport, at(imp.Pos()), "imports database/sql"})
			case (path == layeringModule+"/internal/store" || strings.HasPrefix(path, layeringModule+"/internal/store/")) &&
				!slices.Contains(storeImportsAllowed, path):
				out = append(out, layeringFinding{ruleStoreImport, at(imp.Pos()), "imports the store package " + path})
			}
		}

		local := sessionName(f)
		// Each unit is scanned on its own: a declaration, or one type of a
		// grouped type declaration, so that the Deps exemption covers the
		// Deps type and not a type declared beside it (review 1961).
		type unit struct {
			node        ast.Node
			typeAllowed bool
		}
		var units []unit
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == wiring {
				continue
			}
			if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
				for _, s := range gd.Specs {
					ts, ok := s.(*ast.TypeSpec)
					units = append(units, unit{s, ok && ts.Name.Name == wiringType})
				}
				continue
			}
			units = append(units, unit{d, false})
		}
		for _, u := range units {
			typeAllowed := u.typeAllowed
			ast.Inspect(u.node, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch {
				case local != "" && isPkgSel(sel, local, "Store"):
					if !typeAllowed {
						out = append(out, layeringFinding{ruleStoreType, at(sel.Pos()), "names session.Store"})
					}
				case sel.Sel.Name == "DB":
					out = append(out, layeringFinding{ruleDBSelector, at(sel.Sel.Pos()),
						"reads the database handle " + exprString(sel)})
				default:
					if inner, ok := sel.X.(*ast.SelectorExpr); ok && storeFields[inner.Sel.Name] {
						out = append(out, layeringFinding{ruleStoreCall, at(sel.Sel.Pos()),
							"calls the session store through " + exprString(inner) + " (" + sel.Sel.Name + ")"})
					}
				}
				return true
			})
		}
	}
	return out, len(files), nil
}

// sessionName is the name f imports the session package under, or "" when
// it does not import it.
func sessionName(f *ast.File) string {
	for _, imp := range f.Imports {
		if path, _ := strconv.Unquote(imp.Path.Value); path != sessionImport {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "session"
	}
	return ""
}

// isStoreType reports whether e is session.Store or *session.Store.
func isStoreType(e ast.Expr, local string) bool {
	if st, ok := e.(*ast.StarExpr); ok {
		e = st.X
	}
	sel, ok := e.(*ast.SelectorExpr)
	return ok && isPkgSel(sel, local, "Store")
}

func isPkgSel(sel *ast.SelectorExpr, pkg, name string) bool {
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg && sel.Sel.Name == name
}

func exprString(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprString(e.X) + "." + e.Sel.Name
	case *ast.CallExpr:
		return exprString(e.Fun) + "(...)"
	}
	return "(expression)"
}

// TestHandlersReachNoStore is TKT-01M3YME9DM: no handler in this package
// reads a store, by the rules above.
func TestHandlersReachNoStore(t *testing.T) {
	found, parsed, err := scanLayering(".")
	if err != nil {
		t.Fatal(err)
	}
	if parsed == 0 {
		t.Fatal("no non-test files parsed, so this test is asserting nothing")
	}
	for _, f := range found {
		t.Errorf("%s. A handler calls a service, and the service reads the store; "+
			"move this behind a service method (TKT-01M29BKBMC criterion 1).", f)
	}
}

// TestTheLayeringScanFindsAViolation runs the scan over fixtures that each
// break one rule, and one that keeps them all, so TestHandlersReachNoStore
// cannot pass by finding nothing.
func TestTheLayeringScanFindsAViolation(t *testing.T) {
	for _, tc := range []struct {
		dir  string
		want []string // rules, in the order the scan reports them
	}{
		{"allowed", nil},
		{"store_import", []string{ruleStoreImport}},
		{"sql_import", []string{ruleSQLImport}},
		{"store_type", []string{ruleStoreType}},
		{"store_call", []string{ruleStoreCall}},
		{"store_type_grouped", []string{ruleStoreType}},
		{"db_selector", []string{ruleDBSelector}},
	} {
		t.Run(tc.dir, func(t *testing.T) {
			found, parsed, err := scanLayering(filepath.Join("testdata", "layering", tc.dir))
			if err != nil {
				t.Fatal(err)
			}
			if parsed == 0 {
				t.Fatal("the fixture has no non-test files")
			}
			var got []string
			for _, f := range found {
				got = append(got, f.rule)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("rules found %v, want %v; findings: %v", got, tc.want, found)
			}
		})
	}
}
