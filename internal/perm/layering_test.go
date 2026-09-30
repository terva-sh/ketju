package perm

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

// decisionWrites are the dbq methods whose writes change a permission
// decision. Only this package may call them, through the mutation methods
// that invalidate the cache (mutate.go).
var decisionWrites = map[string]bool{
	"GrantPermission":      true,
	"RevokePermission":     true,
	"AddAdminMembership":   true,
	"AddIdPMembership":     true,
	"DeleteIdPMemberships": true,
	"RemoveIdPMembership":  true,
	"RemoveMembership":     true,
	"SetForumVisibility":   true,
	"SetForumArchived":     true,
	"SetUserRole":          true,
	// A status decides what a viewer may do, so the account lifecycle's
	// writes are here too, and each goes with its audit row (lifecycle.go).
	"SetUserStatus":        true,
	"AnonymizeUser":        true,
	"DeleteUserIdentities": true,
}

// TestMutationsGoThroughPerm is how AC4's "any group or permission write
// invalidates the cache" holds by construction rather than by care: a call to
// one of decisionWrites anywhere but here and the store's own package fails
// the build's tests. Test files are exempt, since a fixture writing behind the
// cache's back is how the TTL is tested.
func TestMutationsGoThroughPerm(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected the module root at %s: %v", root, err)
	}
	allowed := []string{
		filepath.Join(root, "internal", "perm") + string(filepath.Separator),
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
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && decisionWrites[sel.Sel.Name] {
				t.Errorf("%s calls %s. Writes that change a permission decision go through "+
					"internal/perm, which invalidates the cache; a direct call is invisible for up to %v.",
					fset.Position(call.Pos()), sel.Sel.Name, TTL)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 20 {
		t.Fatalf("checked only %d Go files; the walk is not seeing the module", checked)
	}
}
