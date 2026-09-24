// Package member is the one rule every member-only endpoint shares: who is
// refused, and with which problem.
//
// It is its own package because two services need it (threads for the
// composer preview, users for mention autocomplete) and the answer must not
// drift between them, and because a handler has to be able to ask it before
// reading a request: a stranger who sends a malformed request is still a
// stranger, and is told so rather than how to fix the request (review 522).
package member

import (
	"fmt"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
)

// Require admits a member and refuses anyone else. A pre-user session is
// forbidden: the forum knows who they are, and they may not do this yet.
// Anyone else who is not a member, an inactive account included, is
// unauthenticated, as the thread endpoints treat them. what names the action
// in the error, for the log.
func Require(v domain.Viewer, what string) error {
	switch {
	case v.Member():
		return nil
	case v.PreUser:
		return problem.Coded(problem.CodeForbidden, fmt.Errorf("a pre-user session cannot %s", what))
	default:
		return problem.Coded(problem.CodeUnauthenticated, fmt.Errorf("%s needs a member session", what))
	}
}
