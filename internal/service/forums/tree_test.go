package forums

import (
	"bytes"
	"database/sql"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

func forum(id, parent int64) dbq.Forum {
	f := dbq.Forum{ID: id}
	if parent != 0 {
		f.ParentID = sql.NullInt64{Int64: parent, Valid: true}
	}
	return f
}

// TestListed is AC1's builder: roots and each root's children keep the rows'
// order whichever way round the levels arrive, a child whose parent is a
// child or missing is logged and left out rather than lifted, and a hidden
// root hides its children.
func TestListed(t *testing.T) {
	var log bytes.Buffer
	s := &Service{Logger: slog.New(slog.NewTextHandler(&log, nil))}
	rows := []dbq.Forum{
		forum(11, 1), // a child before its root
		forum(1, 0),
		forum(12, 1),
		forum(2, 0),
		forum(21, 2),
		forum(111, 11), // a grandchild, which the triggers refuse
		forum(3, 0),
		forum(31, 3),  // under a hidden root, readable itself
		forum(41, 99), // under a forum not in the table
	}
	readable := map[int64]bool{1: true, 11: true, 12: true, 2: true, 111: true, 31: true, 41: true}
	var got []string
	for _, r := range s.listed(rows, readable) {
		var kids []string
		for _, c := range r.children {
			kids = append(kids, itoa(c.ID))
		}
		got = append(got, itoa(r.row.ID)+"["+strings.Join(kids, " ")+"]")
	}
	if want := "1[11 12] 2[]"; strings.Join(got, " ") != want {
		t.Errorf("got %s, want %s", strings.Join(got, " "), want)
	}
	for _, id := range []string{"forum=111", "forum=41"} {
		if !strings.Contains(log.String(), id) {
			t.Errorf("no log line for %s:\n%s", id, log.String())
		}
	}
	if strings.Contains(log.String(), "forum=31") {
		t.Errorf("a child under a hidden root is not a broken database:\n%s", log.String())
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
