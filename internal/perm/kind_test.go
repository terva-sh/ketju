package perm

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// kindAllowed are where domain.KindAgent may be named, as "file" or
// "file:func" relative to the module root (TKT-01M29P9RSW). An agent is a
// member like any other: roles apply unchanged, so no permission may ask
// the account's kind. The kind decides only these: the machine-authored
// frame flag, stamped at append time; the credential; provisioning; and the
// limiter's agent policy. domain declares the kinds.
var kindAllowed = []string{
	"internal/domain/",
	"internal/service/threads/create.go:authorFlags",
	"internal/auth/agentcred/",
	"internal/service/users/agents.go",
	"internal/web/api/agents.go",
}

// TestNoKindChecksInPermissions fails on a reference to domain.KindAgent
// anywhere kindAllowed does not name. Test files are exempt.
func TestNoKindChecksInPermissions(t *testing.T) {
	var found []string
	walkSource(t, func(rel string, f *ast.File, fset *token.FileSet) {
		for _, d := range f.Decls {
			fn, _ := d.(*ast.FuncDecl)
			ast.Inspect(d, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "KindAgent" {
					return true
				}
				if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "domain" {
					return true
				}
				where := rel
				if fn != nil {
					where += ":" + fn.Name.Name
				}
				found = append(found, where)
				if !kindPermitted(where) {
					t.Errorf("%s names domain.KindAgent. An agent is a member like any other; "+
						"only the frame-flag stamp, the credential, provisioning and the limiter's agent policy "+
						"may ask an account's kind (TKT-01M29P9RSW)", fset.Position(sel.Pos()))
				}
				return true
			})
		}
	})
	// Not vacuous: the stamp names it today.
	if !slices.Contains(found, "internal/service/threads/create.go:authorFlags") {
		t.Errorf("the scan did not find the frame-flag stamp's reference; it found %v", found)
	}
}

func kindPermitted(where string) bool {
	for _, a := range kindAllowed {
		if where == a || strings.HasSuffix(a, "/") && strings.HasPrefix(where, a) ||
			!strings.Contains(a, ":") && strings.HasPrefix(where, a+":") {
			return true
		}
	}
	return false
}

// credentialWriters are the one package each agent credential write may be
// called from: issuing in agentcred, which checks the agent is active in
// the same transaction, and revoking here, which writes the audit row.
var credentialWriters = map[string]string{
	"InsertAgentCredential":  "internal/auth/agentcred/",
	"RevokeAgentCredentials": "internal/perm/",
}

// TestCredentialWritesHaveOneHome fails on a call to a credential write
// from anywhere but its package.
func TestCredentialWritesHaveOneHome(t *testing.T) {
	seen := map[string]bool{}
	walkSource(t, func(rel string, f *ast.File, fset *token.FileSet) {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			home, ok := credentialWriters[sel.Sel.Name]
			if !ok || strings.HasPrefix(rel, "internal/store/db/") {
				return true
			}
			seen[sel.Sel.Name] = true
			if !strings.HasPrefix(rel, home) {
				t.Errorf("%s calls %s, which only %s may", fset.Position(call.Pos()), sel.Sel.Name, home)
			}
			return true
		})
	})
	for name := range credentialWriters {
		if !seen[name] {
			t.Errorf("no call to %s found; the scan is not seeing the module", name)
		}
	}
}

// walkSource parses every non-test Go file under the module root and calls
// fn with its root-relative, slash-separated path.
func walkSource(t *testing.T, fn func(rel string, f *ast.File, fset *token.FileSet)) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
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
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		checked++
		fn(filepath.ToSlash(rel), f, fset)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 20 {
		t.Fatalf("checked only %d Go files; the walk is not seeing the module", checked)
	}
}
