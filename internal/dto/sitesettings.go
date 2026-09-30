package dto

import "encoding/json"

// The admin site settings screen (TKT-01M29CXK2H, docs/07 "Site settings").
// Every response here is an admin's alone: the settings change what the
// site does, and holding moderate somewhere opens none of it (08 "Decided
// 2026-09-30").

// SiteSettings is GET /admin/settings: every runtime setting, and the parts
// of config.toml it does not hold, named with the reason.
type SiteSettings struct {
	// Settings is every key the registry holds, sorted by key. The screen
	// draws one field per key from it, so a key added later appears without
	// a client change.
	Settings []SiteSetting `json:"settings"`
	// Config is every top-level section of config.toml, in the file's
	// order, with no values.
	Config []ConfigSection `json:"config"`
}

// SiteSetting is one runtime setting, as the registry describes it and the
// database holds it.
type SiteSetting struct {
	Key string `json:"key"`
	Doc string `json:"doc"`
	// Kind is "text", "integer" or "boolean".
	Kind string `json:"kind"`
	// Allowed says in words what the key takes: "a whole number, 1 or
	// more". The screen shows it beside the field and again when a change
	// is refused, since the refusal's title is fixed text.
	Allowed string `json:"allowed"`
	// Choices is every value a key with a closed list takes, which the
	// screen draws as a select. Empty, never null, for any other key.
	Choices []string `json:"choices"`
	// Default and Value are JSON of the key's kind. Value is the default
	// when the key has never been set.
	Default   json.RawMessage `json:"default"`
	Value     json.RawMessage `json:"value"`
	IsDefault bool            `json:"is_default"`
	// UpdatedAt and UpdatedBy are who last set the key, and when. Set from
	// the screen, both are set. Set from the command line, UpdatedBy is
	// null and UpdatedAt holds the time. Never set, both are null.
	UpdatedAt *Millis `json:"updated_at"`
	UpdatedBy *Author `json:"updated_by"`
}

// ConfigSection is one top-level section of config.toml: its name, why it
// is set in the file rather than here, and the keys under it.
type ConfigSection struct {
	Name   string   `json:"name"`
	Reason string   `json:"reason"`
	Keys   []string `json:"keys"`
}
