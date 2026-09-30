package identity

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// handleWriters are the sqlc queries that write users.handle, and whether a
// caller must pass HandleFree first. AnonymizeUser is exempt: it writes
// deleted-user-<id>, which CheckHandle refuses to anyone and a trigger pins
// to the row's own id (migration 0012), so no one else can hold it. A query
// added here that gives out a handle someone chose is not exempt.
var handleWriters = map[string]bool{
	"CreateUser":    true,
	"AnonymizeUser": false,
	// A handle change (rename.go, TKT-01M3NRP3W9).
	"RenameUser": true,
}

var (
	queryName    = regexp.MustCompile(`(?m)^-- name: (\w+)`)
	writesHandle = regexp.MustCompile(`(?is)\bINSERT\s+INTO\s+users\b|\bUPDATE\s+users\s+SET\b[^;]*\bhandle\s*=`)
)

// TestEveryHandleWriteChecksHandleFree is Terva review 1450 on TKT-01M29CKRFS:
// the reservation of an anonymized account's handle holds only if every path
// that gives out a handle asks HandleFree, not Register alone. It finds every
// query that writes users.handle, which must be listed above, and every Go
// file that calls one that is not exempt, which must call HandleFree. A new
// allocation path (a rename, an approved application, an auto-approved
// registration) fails here until it does.
func TestEveryHandleWriteChecksHandleFree(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	queries, err := filepath.Glob(filepath.Join(root, "internal", "store", "db", "queries", "*.sql"))
	if err != nil || len(queries) == 0 {
		t.Fatalf("no queries found: %v", err)
	}
	var found []string
	for _, f := range queries {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		idx := queryName.FindAllStringSubmatchIndex(src, -1)
		for i, m := range idx {
			end := len(src)
			if i+1 < len(idx) {
				end = idx[i+1][0]
			}
			name := src[m[2]:m[3]]
			if !writesHandle.MatchString(src[m[1]:end]) {
				continue
			}
			found = append(found, name)
			if _, ok := handleWriters[name]; !ok {
				t.Errorf("%s writes users.handle but is not in handleWriters: say whether it must pass HandleFree", name)
			}
		}
	}
	for name := range handleWriters {
		if !slices.Contains(found, name) {
			t.Errorf("handleWriters lists %s, which no longer writes users.handle", name)
		}
	}

	callers := map[string][]string{}
	err = filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "dbq" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(b)
		for name, checked := range handleWriters {
			if !checked || !strings.Contains(src, "."+name+"(") {
				continue
			}
			rel, _ := filepath.Rel(root, path)
			callers[name] = append(callers[name], rel)
			if !strings.Contains(src, "HandleFree(") {
				t.Errorf("%s calls %s without HandleFree", rel, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Not vacuous: Register is a caller today.
	if !slices.Contains(callers["CreateUser"], filepath.Join("internal", "service", "identity", "register.go")) {
		t.Errorf("CreateUser callers found: %v; register.go should be one", callers["CreateUser"])
	}
}

// TestOnlyIdentityRenames: a rename writes its release row and its audit row
// in the transaction that renames (rename.go), so RenameUser is called from
// this package and nowhere else outside the store. A second caller would be
// a rename with no reservation, no redirect and no record (TKT-01M3NRP3W9).
func TestOnlyIdentityRenames(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	var callers []string
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "db" && filepath.Base(filepath.Dir(path)) == "store" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), ".RenameUser(") {
			rel, _ := filepath.Rel(root, path)
			callers = append(callers, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join("internal", "service", "identity", "rename.go")}
	if !slices.Equal(callers, want) {
		t.Errorf("RenameUser is called from %v, want only %v", callers, want)
	}
}
