package audit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// actionName is what an action looks like: <noun>.<verb>, lower case.
var actionName = regexp.MustCompile(`^[a-z]+\.[a-z_]+$`)

// TestActionsSorted: the list is sorted, has no duplicate, and every entry
// is shaped as an action, so the filter the admin view draws from it reads
// in order and offers nothing twice.
func TestActionsSorted(t *testing.T) {
	if !slices.IsSorted(Actions) {
		t.Error("audit.Actions is not sorted")
	}
	if len(slices.Compact(slices.Clone(Actions))) != len(Actions) {
		t.Error("audit.Actions lists an action twice")
	}
	for _, a := range Actions {
		if !actionName.MatchString(a) {
			t.Errorf("%q is not shaped as an action, <noun>.<verb>", a)
		}
	}
}

// TestActionsDocumented is TKT-01M29CP2R4's fifth criterion held in place:
// docs/04 "audit_log" lists every action in Actions, and no action that is
// not there, each against a target type that exists.
func TestActionsDocumented(t *testing.T) {
	b, err := os.ReadFile("../../docs/04-data-model.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	start := strings.Index(doc, "<!-- audit-actions:begin -->")
	end := strings.Index(doc, "<!-- audit-actions:end -->")
	if start < 0 || end < start {
		t.Fatal("docs/04 has no audit-actions:begin/audit-actions:end block")
	}
	var listed []string
	for _, line := range strings.Split(doc[start:end], "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 4 {
			t.Errorf("docs/04 audit-actions row %q has no target type", line)
			continue
		}
		action := strings.Trim(strings.TrimSpace(cells[1]), "`")
		listed = append(listed, action)
		if typ := strings.Trim(strings.TrimSpace(cells[2]), "`"); !slices.Contains(Types, typ) {
			t.Errorf("docs/04 lists %q against %q, which is not a target type", action, typ)
		}
	}
	for _, a := range Actions {
		if !slices.Contains(listed, a) {
			t.Errorf("docs/04 does not list the action %q", a)
		}
	}
	for _, a := range listed {
		if !slices.Contains(Actions, a) {
			t.Errorf("docs/04 lists %q, which is not in audit.Actions", a)
		}
	}
	if len(listed) != len(Actions) {
		t.Errorf("docs/04 lists %d actions and audit.Actions %d; one lists an action twice", len(listed), len(Actions))
	}
}

// TestActionsCoverTheWriters is how the list stays complete without
// Append refusing what it lacks: every action constant in the module, a
// const whose name starts with Action and whose value is shaped as an
// action, and every string literal passed to Append as its action, must be
// in Actions. A ticket that adds an action and forgets the list fails here
// rather than leaving the admin view's filter one short.
func TestActionsCoverTheWriters(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected the module root at %s: %v", root, err)
	}
	fset := token.NewFileSet()
	found := map[string]string{} // action -> where
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
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		literal := func(x ast.Expr) (string, bool) {
			lit, ok := x.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return "", false
			}
			s, err := strconv.Unquote(lit.Value)
			return s, err == nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.GenDecl:
				if n.Tok != token.CONST {
					return true
				}
				for _, spec := range n.Specs {
					vs := spec.(*ast.ValueSpec)
					for i, name := range vs.Names {
						if !strings.HasPrefix(name.Name, "Action") || i >= len(vs.Values) {
							continue
						}
						if s, ok := literal(vs.Values[i]); ok && actionName.MatchString(s) {
							found[s] = fset.Position(name.Pos()).String()
						}
					}
				}
			case *ast.CallExpr:
				// audit.Append(ctx, q, at, by, action, target, details)
				sel, ok := n.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Append" || len(n.Args) != 7 {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "audit" {
					return true
				}
				if s, ok := literal(n.Args[4]); ok {
					found[s] = fset.Position(n.Pos()).String()
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Today's writers name 45 actions between them; far fewer means the walk
	// stopped seeing the module rather than that the actions went away.
	if len(found) < 40 {
		t.Fatalf("found only %d actions in the module: %v", len(found), found)
	}
	for a, where := range found {
		if !slices.Contains(Actions, a) {
			t.Errorf("%s writes the action %q, which audit.Actions and docs/04 do not list", where, a)
		}
	}
}
