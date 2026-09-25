package threads

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"

	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db/views"
)

// cursor is where a thread list's next page starts: after Key, in Forum's
// list in Sort's order, leaving out Pinned, the forum's own threads that the
// first page showed as pins. Carrying them is what keeps a pin that expires
// or is removed between pages from appearing twice (review on PR #84).
type cursor struct {
	Sort   views.Sort
	Forum  int64
	Key    views.Key
	Pinned []int64
}

// A cursor is opaque to clients (docs/07): base64url, without padding, of
// "sort|forum|at|id|pinned", where sort is a for activity or c for created,
// and pinned is the thread ids joined by commas, or empty.
//
// It is a position, not a credential: it is not signed, and a client that
// writes one by hand gets the page after that position, which it could read
// anyway. What decodeCursor refuses is one that is malformed or was made for
// another forum or the other sort, where its key would mean something else.
func (c cursor) encode() string {
	pinned := make([]string, len(c.Pinned))
	for i, id := range c.Pinned {
		pinned[i] = strconv.FormatInt(id, 10)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join([]string{sortTag(c.Sort),
		strconv.FormatInt(c.Forum, 10), strconv.FormatInt(c.Key.At, 10), strconv.FormatInt(c.Key.ID, 10),
		strings.Join(pinned, ",")}, "|")))
}

var errBadCursor = problem.Coded(problem.CodeBadRequest,
	errors.New("threads: the cursor is malformed, or was made for another forum or sort"))

// decodeCursor reads a cursor for forum's list in sort s. Empty is the first
// page. Anything that is not exactly what encode writes for s and forum is
// bad-request.
func decodeCursor(s views.Sort, forum int64, text string) (*cursor, error) {
	if text == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return nil, errBadCursor
	}
	parts := strings.Split(string(b), "|")
	if len(parts) != 5 {
		return nil, errBadCursor
	}
	f, err1 := strconv.ParseInt(parts[1], 10, 64)
	at, err2 := strconv.ParseInt(parts[2], 10, 64)
	id, err3 := strconv.ParseInt(parts[3], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil || id <= 0 {
		return nil, errBadCursor
	}
	c := cursor{Sort: s, Forum: f, Key: views.Key{At: at, ID: id}}
	if parts[4] != "" {
		for p := range strings.SplitSeq(parts[4], ",") {
			n, err := strconv.ParseInt(p, 10, 64)
			if err != nil || n <= 0 {
				return nil, errBadCursor
			}
			c.Pinned = append(c.Pinned, n)
		}
	}
	// Only the canonical spelling for this sort and forum: that refuses
	// the other sort's tag, another forum's id, and "+5" or "05" for 5.
	if c.Forum != forum || c.encode() != text {
		return nil, errBadCursor
	}
	return &c, nil
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
