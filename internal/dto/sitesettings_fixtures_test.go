package dto

import "encoding/json"

// The admin site settings screen's case (TKT-01M29CXK2H), registered into
// the shared corpus as admin_fixtures_test.go's are. The settings are an
// admin's alone, so there is no other viewer class.

func init() {
	corpus["settings."+tAdmin] = siteSettings()
}

// siteSettings is the listing with one key an admin changed from the
// screen, one changed from the command line and one never set, so each of
// the nullable fields appears both null and set. The real listing has
// every key in the registry; three pin the shape.
func siteSettings() SiteSettings {
	root := Author{Handle: "root", DisplayName: "Site Admin", Role: "admin", Kind: "human"}
	return SiteSettings{
		Settings: []SiteSetting{
			{
				Key: "flags.auto_hide_threshold", Doc: "Open flags on a post that hide it pending review.",
				Kind: "integer", Allowed: "a whole number, 1 or more", Choices: []string{},
				Default: json.RawMessage(`3`), Value: json.RawMessage(`3`), IsDefault: true,
			},
			{
				Key: "registration.mode", Doc: `How a new visitor becomes a member: "open" or "approval".`,
				Kind: "text", Allowed: `one of "open", "approval"`, Choices: []string{"open", "approval"},
				Default: json.RawMessage(`"approval"`), Value: json.RawMessage(`"open"`),
				UpdatedAt: ptr(Millis(1757600000000)),
			},
			{
				Key: "site_name", Doc: "The site's name, shown in the header and page titles.",
				Kind: "text", Allowed: "text of 1 to 100 characters", Choices: []string{},
				Default: json.RawMessage(`"Ketju"`), Value: json.RawMessage(`"The Supper Club"`),
				UpdatedAt: ptr(Millis(1757610000000)), UpdatedBy: &root,
			},
		},
		Config: []ConfigSection{
			{Name: "listen", Reason: "Read before the database opens, so it cannot be kept in the database. The server binds it at startup.",
				Keys: []string{"listen"}},
			{Name: "oidc", Reason: "It holds provider secrets, and is needed before anyone can sign in.",
				Keys: []string{"oidc.providers[].key", "oidc.providers[].issuer", "oidc.providers[].client_secret"}},
		},
	}
}
