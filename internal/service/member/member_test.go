package member

import (
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
)

func TestRequire(t *testing.T) {
	for _, c := range []struct {
		name string
		v    domain.Viewer
		want problem.Code // "" for admitted
	}{
		{"a member", domain.Viewer{UserID: 1, Active: true}, ""},
		{"anonymous", domain.Anonymous(), problem.CodeUnauthenticated},
		{"inactive", domain.Viewer{UserID: 1}, problem.CodeUnauthenticated},
		{"a pre-user", domain.Viewer{PreUser: true, IdentityID: 3}, problem.CodeForbidden},
		// A viewer somehow both is a member: the membership wins.
		{"a member flagged pre-user", domain.Viewer{UserID: 1, Active: true, PreUser: true}, ""},
	} {
		err := Require(c.v, "do this")
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: refused: %v", c.name, err)
		case c.want != "" && (err == nil || problem.FromError(err) != c.want):
			t.Errorf("%s: %v, want %s", c.name, err, c.want)
		}
	}
}
