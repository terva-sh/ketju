package threads

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"

	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db/views"
)

// A thread list's cursor is opaque to clients (docs/07): base64url, without
// padding, of "a|at|id" for the activity order or "c|at|id" for
// ?sort=created, where at and id are the last row's sort key and thread id.
// The order is in it so a cursor cannot be replayed against the other sort,
// where its key would mean a different column.
func encodeCursor(s views.Sort, k views.Key) string {
	return base64.RawURLEncoding.EncodeToString([]byte(sortTag(s) + "|" +
		strconv.FormatInt(k.At, 10) + "|" + strconv.FormatInt(k.ID, 10)))
}

var errBadCursor = problem.Coded(problem.CodeBadRequest, errors.New("threads: the cursor is not one this server issued"))

// decodeCursor reads a cursor for sort s. Empty is the first page. Anything
// that is not exactly what encodeCursor writes for s is bad-request.
func decodeCursor(s views.Sort, c string) (*views.Key, error) {
	if c == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return nil, errBadCursor
	}
	parts := strings.Split(string(b), "|")
	if len(parts) != 3 || parts[0] != sortTag(s) {
		return nil, errBadCursor
	}
	at, err1 := strconv.ParseInt(parts[1], 10, 64)
	id, err2 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil || id <= 0 {
		return nil, errBadCursor
	}
	k := views.Key{At: at, ID: id}
	// Only the canonical spelling: "+5" or "05" would name the same key,
	// and a cursor has one form.
	if encodeCursor(s, k) != c {
		return nil, errBadCursor
	}
	return &k, nil
}

func sortTag(s views.Sort) string {
	if s == views.ByCreated {
		return "c"
	}
	return "a"
}

// parseSort reads ?sort=: empty is the activity order, "created" is
// creation order (docs/06), and anything else is bad-request.
func parseSort(s string) (views.Sort, error) {
	switch s {
	case "":
		return views.ByActivity, nil
	case "created":
		return views.ByCreated, nil
	}
	return 0, problem.Coded(problem.CodeBadRequest, errors.New("threads: sort is empty or \"created\""))
}
