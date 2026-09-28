package identity

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/terva-sh/ketju/internal/auth/oidc"
	"github.com/terva-sh/ketju/internal/config"
	"github.com/terva-sh/ketju/internal/perm"
)

// Match is why a sign-in matched the bootstrap configuration.
type Match struct {
	// Via is "subject" or "email".
	Via string
	// UnverifiedEmail is set, with no match, when the address is listed in
	// bootstrap.admin_emails but the provider did not assert
	// email_verified. The sign-in logs it, so an operator whose first login
	// did not make them admin can find out why.
	UnverifiedEmail bool
}

// BootstrapMatch decides whether id is one the configuration names as an
// admin (docs/05 "Principles"). A subject matches when "provider:subject" is
// listed in AdminSubjects. An email matches only when the provider asserted
// email_verified, compared on the whole address without regard to case:
// an unverified address is whatever the person typed into the IdP, and
// honoring it would hand admin to anyone who can type the operator's
// address. The subject is the precise form and is tried first.
func BootstrapMatch(cfg config.Bootstrap, id *oidc.Identity) (Match, bool) {
	if slices.Contains(cfg.AdminSubjects, id.Provider+":"+id.Subject) {
		return Match{Via: "subject"}, true
	}
	if id.Email == "" {
		return Match{}, false
	}
	listed := slices.ContainsFunc(cfg.AdminEmails, func(e string) bool { return strings.EqualFold(e, id.Email) })
	switch {
	case listed && id.EmailVerified:
		return Match{Via: "email"}, true
	case listed:
		return Match{UnverifiedEmail: true}, false
	}
	return Match{}, false
}

// ApplyBootstrap makes userID an admin if id matches the bootstrap
// configuration and they have never been promoted, and records it in
// audit_log. It is idempotent: a later sign-in, including one after the user
// was demoted, changes nothing ("first login" as settled in grooming for
// TKT-01M29C800G). Registration calls it for the user it creates, as Login
// does for one who already exists. It reports whether it promoted.
func (s *Service) ApplyBootstrap(ctx context.Context, userID int64, id *oidc.Identity) (bool, error) {
	m, ok := BootstrapMatch(s.Bootstrap, id)
	if m.UnverifiedEmail {
		s.logger().WarnContext(ctx, "a sign-in's email is listed in bootstrap.admin_emails, but the provider "+
			"did not assert email_verified, so it was not made admin", "provider", id.Provider, "user", userID)
	}
	if !ok {
		return false, nil
	}
	if s.Perm == nil {
		return false, fmt.Errorf("identity: a sign-in matches the bootstrap configuration, but no permission evaluator is set")
	}
	promoted, err := s.Perm.Promote(ctx, userID, perm.Promotion{Once: true,
		Details: map[string]string{"via": "bootstrap", "match": m.Via, "provider": id.Provider}})
	if err != nil {
		return false, fmt.Errorf("identity: bootstrap promotion: %w", err)
	}
	if promoted {
		s.logger().InfoContext(ctx, "made admin by the bootstrap configuration", "provider", id.Provider,
			"match", m.Via, "user", userID)
	}
	return promoted, nil
}
