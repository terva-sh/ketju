package identity

import (
	"testing"

	"github.com/terva-sh/ketju/internal/auth/oidc"
	"github.com/terva-sh/ketju/internal/config"
)

// AC2: an email matches only when the provider asserts email_verified; a
// subject matches whatever the email says.
func TestBootstrapMatch(t *testing.T) {
	cfg := config.Bootstrap{
		AdminEmails:   []string{"Me@Example.org"},
		AdminSubjects: []string{"authelia:8f2c"},
	}
	cases := []struct {
		name string
		cfg  config.Bootstrap
		id   oidc.Identity
		want Match
		ok   bool
	}{
		{name: "verified email", cfg: cfg,
			id:   oidc.Identity{Provider: "google", Subject: "1", Email: "me@example.org", EmailVerified: true},
			want: Match{Via: "email"}, ok: true},
		{name: "unverified email", cfg: cfg,
			id:   oidc.Identity{Provider: "google", Subject: "1", Email: "me@example.org"},
			want: Match{UnverifiedEmail: true}},
		{name: "email differs only in case", cfg: cfg,
			id:   oidc.Identity{Provider: "google", Subject: "1", Email: "ME@EXAMPLE.ORG", EmailVerified: true},
			want: Match{Via: "email"}, ok: true},
		{name: "a longer address is not a match", cfg: cfg,
			id: oidc.Identity{Provider: "google", Subject: "1", Email: "me@example.org.evil", EmailVerified: true}},
		{name: "a suffix is not a match", cfg: cfg,
			id: oidc.Identity{Provider: "google", Subject: "1", Email: "notme@example.org", EmailVerified: true}},
		{name: "subject, no email", cfg: cfg,
			id:   oidc.Identity{Provider: "authelia", Subject: "8f2c"},
			want: Match{Via: "subject"}, ok: true},
		{name: "subject, unverified listed email", cfg: cfg,
			id:   oidc.Identity{Provider: "authelia", Subject: "8f2c", Email: "me@example.org"},
			want: Match{Via: "subject"}, ok: true},
		{name: "subject at another provider", cfg: cfg,
			id: oidc.Identity{Provider: "google", Subject: "8f2c"}},
		{name: "subject differs in case", cfg: cfg,
			id: oidc.Identity{Provider: "authelia", Subject: "8F2C"}},
		{name: "empty config", cfg: config.Bootstrap{},
			id: oidc.Identity{Provider: "authelia", Subject: "8f2c", Email: "me@example.org", EmailVerified: true}},
		{name: "no email claim", cfg: config.Bootstrap{AdminEmails: []string{"me@example.org"}},
			id: oidc.Identity{Provider: "google", Subject: "1", EmailVerified: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := BootstrapMatch(c.cfg, &c.id)
			if got != c.want || ok != c.ok {
				t.Errorf("BootstrapMatch = %+v, %v; want %+v, %v", got, ok, c.want, c.ok)
			}
		})
	}
}
