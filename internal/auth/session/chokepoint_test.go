package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The one door (issue.go). These tests are AC1 of TKT-01M29C7ZZR: a second
// way to create a session fails the build rather than waiting for a review
// to notice it, which is how Gitea's approval bypass shipped (docs/09).

// doorCallers are the functions allowed to call the generated CreateSession.
// insert is the door. sessiontest's insert signs users in for handler tests
// in other packages, and is allowed only because nothing but a test may
// import it, which the scan also checks.
var doorCallers = []string{
	"internal/auth/session/session.go:insert",
	"internal/auth/session/sessiontest/sessiontest.go:insert",
}

const sessiontestImport = "github.com/terva-sh/ketju/internal/auth/session/sessiontest"

// theQuery is the one statement that may insert into sessions: the query
// named CreateSession in this file, once (review 499).
const theQuery = "internal/store/db/queries/identity.sql"

// insertCallers are the functions allowed to call insert, which writes
// whatever its decide function returns and checks nothing itself: a third
// caller in this package would be a door with no status check that adds no
// CreateSession caller (review 499).
var insertCallers = []string{
	"internal/auth/session/issue.go:IssuePreUser",
	"internal/auth/session/issue.go:IssueUser",
}

// table matches the sessions table however SQLite lets it be spelled:
// quoted with double quotes, backticks or brackets, and qualified by a
// schema such as main or temp (review 499).
const table = `(?:["` + "`" + `\[]?\w+["` + "`" + `\]]?\s*\.\s*)?["` + "`" + `\[]?sessions["` + "`" + `\]]?`

var (
	sqlInsert   = regexp.MustCompile(`(?is)\b(insert|replace)\b[^;]*?\binto\s+` + table + `(?:\W|$)`)
	sqlUpdate   = regexp.MustCompile(`(?is)\bupdate\s+(or\s+\w+\s+)?` + table + `\s+set\s+(.*?)(\bwhere\b|;|$)`)
	sqlComment  = regexp.MustCompile(`--[^\n]*`)
	sqlcName    = regexp.MustCompile(`(?m)^--\s*name:\s*(\w+)`)
	updatableOK = []string{"last_used_at"}
)

// scan reports every way into the sessions table under root that is not the
// door, and the callers of CreateSession it found, as root-relative
// "file:func". Test files are exempt: a schema test may write a row
// directly, and only a running server can let anyone in.
func scan(t *testing.T, root string) (doors, callers, inserters []string) {
	t.Helper()
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if e.IsDir() {
			switch {
			case path != root && (e.Name() == ".git" || e.Name() == "spikes" || e.Name() == "node_modules" || e.Name() == "testdata"):
				return filepath.SkipDir
			case rel == "internal/store/db/dbq":
				// Generated from the queries, which are scanned as SQL.
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case strings.HasSuffix(path, "_test.go"):
			return nil
		case strings.HasSuffix(path, ".go"):
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			d, c, i := scanGo(rel, f)
			doors, callers, inserters = append(doors, d...), append(callers, c...), append(inserters, i...)
		case strings.HasSuffix(path, ".sql"):
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			doors = append(doors, scanSQL(rel, string(b))...)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return doors, callers, inserters
}

func scanGo(rel string, f *ast.File) (doors, callers, inserters []string) {
	inSession := path.Dir(rel) == "internal/auth/session"
	for _, imp := range f.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p == sessiontestImport {
			doors = append(doors, rel+": imports sessiontest outside a test")
		}
	}
	for _, decl := range f.Decls {
		fn := "(package)"
		if d, ok := decl.(*ast.FuncDecl); ok {
			fn = d.Name.Name
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				if n.Sel.Name == "CreateSession" {
					callers = append(callers, rel+":"+fn)
				}
				if inSession && n.Sel.Name == "insert" {
					inserters = append(inserters, rel+":"+fn)
				}
			case *ast.BasicLit:
				if n.Kind != token.STRING {
					break
				}
				s, err := strconv.Unquote(n.Value)
				if err != nil {
					s = n.Value
				}
				if sqlInsert.MatchString(s) {
					doors = append(doors, rel+":"+fn+": writes INSERT INTO sessions by hand")
				}
				if bad := badUpdates(s); len(bad) > 0 {
					doors = append(doors, rel+":"+fn+": updates sessions."+strings.Join(bad, ", sessions."))
				}
			}
			return true
		})
	}
	return doors, callers, inserters
}

// scanSQL checks a query or migration file. The one insert is the query
// named CreateSession, which only insert calls. An update may move only the
// columns in updatableOK: one that set kind, user_id or identity_id could
// turn an applicant's pre-user session into a member's, a door with no
// INSERT in it.
func scanSQL(rel, src string) (doors []string) {
	starts := sqlcName.FindAllStringSubmatchIndex(src, -1)
	type stmt struct{ name, body string }
	var stmts []stmt
	if len(starts) == 0 {
		stmts = []stmt{{"", src}}
	}
	for i, m := range starts {
		end := len(src)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		if i == 0 && m[0] > 0 {
			stmts = append(stmts, stmt{"", src[:m[0]]})
		}
		stmts = append(stmts, stmt{src[m[2]:m[3]], src[m[1]:end]})
	}
	door := rel == theQuery
	for _, s := range stmts {
		body := sqlComment.ReplaceAllString(s.body, "")
		// The first CreateSession in the door's file is the door, and uses
		// the exemption up whether or not it passes: it may hold one insert,
		// and no later statement may borrow its name.
		exempt := door && s.name == "CreateSession"
		if exempt {
			door = false
		}
		if n := len(sqlInsert.FindAllString(body, -1)); n > 0 && !(exempt && n == 1) {
			doors = append(doors, rel+":"+s.name+": inserts into sessions")
		}
		if bad := badUpdates(body); len(bad) > 0 {
			doors = append(doors, rel+":"+s.name+": updates sessions."+strings.Join(bad, ", sessions."))
		}
	}
	return doors
}

// badUpdates lists the columns an UPDATE of sessions in sql sets beyond
// updatableOK.
func badUpdates(sql string) (bad []string) {
	for _, m := range sqlUpdate.FindAllStringSubmatch(sql, -1) {
		for _, a := range strings.Split(m[2], ",") {
			col, _, _ := strings.Cut(a, "=")
			col = strings.Trim(strings.TrimSpace(col), "\"`")
			if col != "" && !slices.Contains(updatableOK, strings.ToLower(col)) {
				bad = append(bad, col)
			}
		}
	}
	return bad
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected the module root at %s: %v", root, err)
	}
	return root
}

func TestOneDoorIntoSessions(t *testing.T) {
	doors, callers, inserters := scan(t, moduleRoot(t))
	for _, d := range doors {
		t.Errorf("a second way into the sessions table: %s. Sessions are created only by "+
			"IssueUser and IssuePreUser in internal/auth/session/issue.go (TKT-01M29C7ZZR).", d)
	}
	slices.Sort(callers)
	if !slices.Equal(callers, doorCallers) {
		t.Errorf("CreateSession is called from %v, want exactly %v. A new caller is a new door: "+
			"create sessions through IssueUser or IssuePreUser.", callers, doorCallers)
	}
	slices.Sort(inserters)
	if !slices.Equal(inserters, insertCallers) {
		t.Errorf("insert is called from %v, want exactly %v. insert checks nothing itself: "+
			"a session is created through IssueUser or IssuePreUser, which decide who may have one.", inserters, insertCallers)
	}
	// The query the door runs is found, so the SQL scan is reading the
	// queries rather than passing on nothing.
	b, err := os.ReadFile(filepath.Join(moduleRoot(t), theQuery))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "-- name: CreateSession :exec") || !sqlInsert.Match(b) {
		t.Fatal("the scan's patterns do not match the query the door runs")
	}
}

// The scan is live: a tree with a door of every kind it knows is caught on
// each, and a test file doing the same is not.
func TestTheScanFindsASecondDoor(t *testing.T) {
	doors, callers, inserters := scan(t, filepath.Join("testdata", "chokepoint"))
	slices.Sort(doors)
	want := []string{
		"internal/other/door.go: imports sessiontest outside a test",
		"internal/other/door.go:promote: updates sessions.kind, sessions.user_id",
		"internal/other/door.go:qualified: updates sessions.user_id",
		"internal/other/door.go:qualified: writes INSERT INTO sessions by hand",
		"internal/other/door.go:sneak: writes INSERT INTO sessions by hand",
		"internal/store/db/migrations/0099_seed.sql:: inserts into sessions",
		"internal/store/db/migrations/0100_named.sql:CreateSession: inserts into sessions",
		"internal/store/db/queries/extra.sql:AdoptSession: updates sessions.user_id",
		"internal/store/db/queries/extra.sql:CreateSession: inserts into sessions",
		"internal/store/db/queries/extra.sql:CreateSessionAgain: inserts into sessions",
		"internal/store/db/queries/extra.sql:TouchAgain: updates sessions.kind",
		"internal/store/db/queries/identity.sql:CreateSession: inserts into sessions",
		"internal/store/db/queries/identity.sql:CreateSession: inserts into sessions",
		"internal/store/db/queries/identity.sql:Qualified: inserts into sessions",
		"internal/store/db/queries/preamble.sql:: inserts into sessions",
	}
	if !slices.Equal(doors, want) {
		t.Errorf("found\n  %s\nwant\n  %s", strings.Join(doors, "\n  "), strings.Join(want, "\n  "))
	}
	if !slices.Equal(callers, []string{"internal/other/door.go:sneak"}) {
		t.Errorf("CreateSession callers %v, want the one in door.go", callers)
	}
	slices.Sort(inserters)
	if !slices.Equal(inserters, []string{"internal/auth/session/backdoor.go:Backdoor"}) {
		t.Errorf("insert callers %v, want the one in backdoor.go", inserters)
	}
}
