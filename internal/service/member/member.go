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

// RequireStaff admits an active moderator or admin, by role, as the admin
// API's staff gate does (internal/web/api/admin.go), and refuses anyone
// else as Require does, or forbidden for a member. A service behind that
// gate asks again, so a caller that forgot the gate is still refused.
func RequireStaff(v domain.Viewer, what string) error {
	if err := Require(v, what); err != nil {
		return err
	}
	if v.Role != domain.RoleAdmin && v.Role != domain.RoleModerator {
		return problem.Coded(problem.CodeForbidden, fmt.Errorf("only staff may %s", what))
	}
	return nil
}

// RequireAdmin admits an active admin, and refuses anyone else as
// RequireStaff does.
func RequireAdmin(v domain.Viewer, what string) error {
	if err := Require(v, what); err != nil {
		return err
	}
	if v.Role != domain.RoleAdmin {
		return problem.Coded(problem.CodeForbidden, fmt.Errorf("only an admin may %s", what))
	}
	return nil
}
