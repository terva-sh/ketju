package dto

// The member settings cases (TKT-01M29CCC0X): GET and PATCH /settings, the
// session list, the identity list, and the answer to removing an identity.
// They are not named settings.*, because settings.admin is the site
// settings (sitesettings_fixtures_test.go).

func init() {
	corpus["user-settings."+tMember] = Settings{Timezone: "Europe/Helsinki", Locale: "en", PageSize: 40, Theme: "dark", Shortcuts: false}

	// Two devices: this one, signed in through GitHub, and a phone whose
	// sign-in recorded no provider.
	corpus["sessions."+tMember] = SessionList{Sessions: []SessionInfo{
		{ID: "6f1c0a9e2b7d4c58a3e1f0b29d8c7e61", Current: true, Device: "Firefox on Linux",
			UserAgent: "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0", IP: "192.0.2.10",
			Provider: "github", CreatedAt: 1757400000000, LastUsedAt: 1757500000000},
		{ID: "0d2e4f6a8c1b3d5e7f9a0b2c4d6e8f10", Device: "Safari on iPhone",
			UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.6 Mobile/15E148 Safari/604.1",
			IP:        "198.51.100.7", CreatedAt: 1757000000000, LastUsedAt: 1757300000000},
	}}

	// Three identities: the one this session came through, a second on a
	// configured provider, and one on a provider the operator removed, which
	// does not count towards keeping the account reachable and so is
	// removable while a configured one remains.
	corpus["identities."+tMember] = IdentityList{Identities: []IdentityInfo{
		{ID: 3, Provider: "github", ProviderName: "GitHub", Configured: true, Email: ptr("alice@example.org"),
			CreatedAt: 1757000000000, LastLoginAt: ptr(Millis(1757400000000)), Current: true, Removable: true},
		{ID: 8, Provider: "club", ProviderName: "The Club's sign-in", Configured: true,
			CreatedAt: 1757100000000, Removable: true},
		{ID: 9, Provider: "oldidp", CreatedAt: 1756000000000, LastLoginAt: ptr(Millis(1756100000000)), Removable: true},
	}}

	corpus["identity-removed."+tMember] = IdentityRemoved{SignedOut: true}

	// GET and PATCH /settings/profile (TKT-01M3V6XVVG): the bio as its
	// Markdown source, a link and a mention in it.
	corpus["profile-settings."+tMember] = ProfileSettings{DisplayName: "Alice Liddell",
		Bio:    "Reading group regular. Ask @carol about the [book list](https://example.org/books).",
		Avatar: "c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2"}
}
