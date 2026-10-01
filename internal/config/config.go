// Package config loads config.toml, applies KETJU_* environment overrides,
// resolves secrets held in files, and refuses to start on anything it cannot
// make sense of.
//
// # What belongs here and what does not
//
// config.toml holds what must be readable before the database exists:
// the listen address, the data directory, the site URL, the fsync policy,
// cache sizes, bootstrap admins, and the OIDC providers. Everything an admin
// changes while the server is running — the site name, the registration mode,
// slow mode — belongs in the settings table (docs/04), where a change
// takes effect without a restart and leaves an audit row.
//
// Putting a key in the wrong half is the mistake this package exists to
// prevent, and it is not a mistake that announces itself: a runtime setting in
// config.toml works, right up to the day an admin changes it in the UI and the
// file silently wins on the next restart. So keys that belong in the settings
// table are not ignored and not quietly accepted. They are refused by name,
// with the place they belong named in the same sentence.
//
// # Failing rather than defaulting
//
// An absent key takes a documented default. An invalid key is an error naming
// the key, the value, and the reason. What never happens is an invalid value
// becoming a zero value: a misspelled fsync policy must not become "no fsync
// at all", and a cache size of -1 must not become a cache size of zero.
//
// Load reports every problem it finds rather than the first, because fixing a
// config file one error per restart is how an operator gives up and copies
// somebody else's.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Defaults for keys that may be left out. They are constants rather than
// scattered literals so that `ketju config check` and the documentation can
// name the same numbers the loader uses.
const (
	DefaultListen           = "127.0.0.1:8080"
	DefaultFsync            = "group"
	DefaultOpenThreads      = threadcache.DefaultBound
	DefaultRenderedCacheMB  = 256
	DefaultResponseCacheMB  = 64
	DefaultProviderScopes   = "openid email profile"
	DefaultLogLevel         = "info"
	DefaultLogFormat        = "auto"
	DefaultAttachmentMB     = 10
	DefaultSSEConnections   = 1000
	DefaultSSEPerUser       = 8
	DefaultSSEAnonymous     = 100
	defaultMaxCacheMB       = 1 << 20 // 1 TB, a bound to catch a typo, not a policy
	defaultMaxOpenThreads   = 1 << 20
	minimumOpenThreadsAllow = 1
)

// Config is the whole file, after defaults, overrides and validation.
type Config struct {
	// Listen is the address the HTTP server binds, host:port.
	Listen string `toml:"listen"`

	// DataDir is the directory holding threads/, blobs/, db/ and config.toml.
	// Required: there is no default that is right on two machines. A relative
	// path is resolved against the config file's directory.
	DataDir string `toml:"data_dir"`

	// SiteURL is the externally visible base URL, with no trailing slash. It
	// is required once an OIDC provider is configured, because a redirect URI
	// cannot be derived from a listen address that may be behind a proxy.
	SiteURL string `toml:"site_url"`

	// CookieKeyFile is the 32-byte key that seals a sign-in's state into its
	// cookie (internal/auth/seal). Empty means secrets/cookie.key under
	// DataDir. Either way it is created, mode 0600, when absent, and must
	// then persist: a new key ends every sign-in in progress. A relative
	// path is resolved against the config file's directory.
	CookieKeyFile string `toml:"cookie_key_file"`

	Storage   Storage   `toml:"storage"`
	Log       Log       `toml:"log"`
	Bootstrap Bootstrap `toml:"bootstrap"`
	OIDC      OIDC      `toml:"oidc"`
	Limits    Limits    `toml:"limits"`

	// sources records where each key's value came from; see Source.
	sources map[string]Source
}

// Log configures the structured logger.
type Log struct {
	// Level is debug, info, warn or error.
	Level string `toml:"level"`

	// Format is json, text, or auto.
	//
	// auto writes text when the output is a terminal and JSON otherwise, which
	// is the rule docs/02 states -- text in development, JSON in production --
	// without asking an operator to remember a second setting. An explicit
	// value always wins, because "auto" guessing wrong inside a container that
	// happens to allocate a TTY is exactly when an operator needs to override
	// it.
	Format string `toml:"format"`
}

// CookieKeyPath is where the cookie key is: CookieKeyFile, or
// secrets/cookie.key under DataDir.
func (c *Config) CookieKeyPath() string {
	if c.CookieKeyFile != "" {
		return c.CookieKeyFile
	}
	return filepath.Join(c.DataDir, "secrets", "cookie.key")
}

// Storage is the part of the thread store an operator may tune.
type Storage struct {
	// Fsync is "group", "always" or "never". See docs/03; "never" is for
	// tests and imports and loses data on a crash.
	Fsync string `toml:"fsync"`

	// OpenThreads is how many threads stay open at once.
	OpenThreads int `toml:"open_threads"`

	// RenderedCacheMB bounds the rendered-post cache.
	RenderedCacheMB int `toml:"rendered_cache_mb"`

	// ResponseCacheMB bounds the serialized-response cache.
	ResponseCacheMB int `toml:"response_cache_mb"`
}

// Limits is the part of the request limits an operator may tune.
//
// The attachment cap and the live-update stream limits are here. The post
// body limit is fixed at 64 KB by
// docs/07 and lives in internal/web as a constant: it is part of what the API
// promises a client, not a dial, and an installation that quietly raised it
// would accept posts that another installation could not federate or import.
type Limits struct {
	// AttachmentMB caps a single upload. docs/07 makes this configurable
	// because what counts as a reasonable attachment depends entirely on who
	// is running the forum and what their disk looks like.
	AttachmentMB int `toml:"attachment_mb"`

	// SSEConnections caps the live-update streams (GET /events) open at once,
	// across every viewer. Each one is a held connection and a goroutine, so
	// the bound has to be chosen rather than discovered when a community
	// grows (docs/02 "Live updates").
	SSEConnections int `toml:"sse_connections"`

	// SSEPerUser caps one signed-in user's open streams: one per open tab.
	SSEPerUser int `toml:"sse_per_user"`

	// SSEAnonymous caps every anonymous stream together. It is one pool rather
	// than a per-address cap, because behind a reverse proxy every client has
	// the proxy's address.
	SSEAnonymous int `toml:"sse_anonymous"`
}

// Bootstrap names the accounts that become admin on first login, so that a
// new installation has a way in that does not involve a password.
type Bootstrap struct {
	// AdminEmails match against the email claim, compared without regard to
	// case, and only when the provider also asserts email_verified = true.
	// An address the provider has not verified is whatever someone typed
	// into it, so it never matches; the sign-in logs a warning instead. Use
	// AdminSubjects with a provider that does not send email_verified.
	AdminEmails []string `toml:"admin_emails"`

	// AdminSubjects are "provider_key:sub" pairs, which is the precise form:
	// an email can be reassigned by whoever runs the mail domain, a subject
	// cannot.
	AdminSubjects []string `toml:"admin_subjects"`
}

// OIDC groups the provider list. It exists as a struct so that the file reads
// as [[oidc.providers]], matching docs/05.
type OIDC struct {
	Providers []Provider `toml:"providers"`
}

// Provider is one configured identity provider.
//
// Providers are an allowlist by construction: only issuers listed here are
// ever contacted, and the login page shows only these.
type Provider struct {
	// Key is the stable identifier stored in identities.provider_key. It must
	// never change for a given provider, because every existing identity row
	// refers to it.
	Key string `toml:"key"`

	// Name is what the login button says.
	Name string `toml:"name"`

	// Issuer is the OIDC issuer URL, https and absolute.
	Issuer string `toml:"issuer"`

	// ClientID is the registered client identifier.
	ClientID string `toml:"client_id"`

	// ClientSecret is the secret inline. Prefer ClientSecretFile.
	ClientSecret Secret `toml:"client_secret"`

	// ClientSecretFile points at a file holding the secret, which is how a
	// secret reaches a container without being written into this file.
	// Exactly one of ClientSecret and ClientSecretFile must be set.
	ClientSecretFile string `toml:"client_secret_file"`

	// Scopes requested at authorization. Defaults to openid, email, profile.
	// "openid" is added if it is missing, since the flow is not OIDC without
	// it.
	Scopes []string `toml:"scopes"`

	// AllowedEmailDomains, when set, gates login on the email domain, and
	// then only an email the provider asserts as verified passes.
	AllowedEmailDomains []string `toml:"allowed_email_domains"`

	// AutoApprove lets this provider bypass approval-mode registration, for
	// the case where the IdP is the organisation's own directory. Someone
	// new from it registers without applying, and without a handle screen
	// when its preferred_username makes a free handle.
	AutoApprove bool `toml:"auto_approve"`

	// GroupsClaim names the claim carrying group membership, if any.
	GroupsClaim string `toml:"groups_claim"`

	// GroupMap maps an IdP group onto a local group on each login.
	GroupMap map[string]string `toml:"group_map"`

	// InsecureSkipIssuerCheck disables the issuer match on the ID token. It
	// exists for one provider family that returns an issuer differing from
	// the discovery URL (docs/09), it is off by default, and the registry
	// that acts on it logs loudly when it is on.
	InsecureSkipIssuerCheck bool `toml:"insecure_skip_issuer_check"`
}

// Load reads path, applies environment overrides from getenv, resolves secret
// files relative to the config file's directory, and validates the result.
//
// getenv may be nil, which means os.Getenv. It is a parameter so that the
// precedence tests do not have to mutate the process environment.
func Load(path string, getenv func(string) string) (*Config, []string, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, fmt.Errorf("config: %s does not exist: %w", path, fs.ErrNotExist)
		}
		return nil, nil, fmt.Errorf("config: reading %s: %w", path, err)
	}

	cfg := &Config{}
	dec := toml.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return nil, nil, decodeError(path, b, err)
	}

	// Which keys the file actually mentions, as dotted paths. Decoding alone
	// cannot tell "absent" from "set to the zero value", and the difference
	// matters: an absent rendered_cache_mb takes the default, while one
	// written as 0 means the operator wants the cache off. Filling that in
	// with 256 MB is exactly the silent substitution this package refuses.
	present := map[string]bool{}
	var raw map[string]any
	if err := toml.Unmarshal(b, &raw); err == nil {
		flatten("", raw, present)
		recordFileSources(cfg, raw)
	}

	problems := &problemList{file: path}

	applyEnv(cfg, getenv, present, problems)
	applyDefaults(cfg, present)

	// A relative data_dir is relative to the config file, not to wherever the
	// process happened to start: the same file must name the same directory
	// under systemd, in a container, and from an operator's shell (settled
	// in grooming for TKT-01M29BYYJG).
	if cfg.DataDir != "" && !filepath.IsAbs(cfg.DataDir) {
		if abs, err := filepath.Abs(filepath.Join(filepath.Dir(path), cfg.DataDir)); err == nil {
			cfg.DataDir = abs
		}
	}

	if cfg.CookieKeyFile != "" && !filepath.IsAbs(cfg.CookieKeyFile) {
		if abs, err := filepath.Abs(filepath.Join(filepath.Dir(path), cfg.CookieKeyFile)); err == nil {
			cfg.CookieKeyFile = abs
		}
	}

	warnings := resolveSecrets(cfg, filepath.Dir(path), problems)
	validate(cfg, problems)
	warnings = append(warnings, warn(cfg)...)

	if err := problems.err(); err != nil {
		return nil, warnings, err
	}
	return cfg, warnings, nil
}

// flatten records every key path the file mentions. Arrays of tables are
// recorded by their table name only; per-provider defaults are decided by the
// provider's own fields, not by whether the file mentioned them.
func flatten(prefix string, m map[string]any, out map[string]bool) {
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		out[path] = true
		if sub, ok := v.(map[string]any); ok {
			flatten(path, sub, out)
		}
	}
}

// Effective renders the configuration as the server understood it, in TOML,
// with every secret replaced by [redacted].
//
// This is what `ketju config check` prints and what belongs in a bug report.
// It is deliberately the whole thing rather than a diff against the defaults:
// the question an operator is answering is "what is this server actually
// doing", and a diff makes them hold the defaults in their head to find out.
func (c *Config) Effective() (string, error) {
	b, err := toml.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("config: rendering the effective configuration: %w", err)
	}
	return string(b), nil
}

// SyncPolicy translates the configured fsync policy into the store's type.
// Load has already refused anything else, so the default arm is unreachable
// and returns the safe policy rather than the fast one.
func (c *Config) SyncPolicy() threadlog.SyncPolicy {
	switch c.Storage.Fsync {
	case "always":
		return threadlog.SyncAlways
	case "never":
		return threadlog.SyncNever
	default:
		return threadlog.SyncGroup
	}
}

// Provider returns the provider with the given key.
func (c *Config) Provider(key string) (Provider, bool) {
	for _, p := range c.OIDC.Providers {
		if p.Key == key {
			return p, true
		}
	}
	return Provider{}, false
}

// applyDefaults fills in the keys neither the file nor the environment set.
// It is driven by presence rather than by zero values, so that a key written
// out as 0 keeps the meaning the operator gave it.
func applyDefaults(c *Config, present map[string]bool) {
	if !present["listen"] {
		c.Listen = DefaultListen
	}
	if !present["storage.fsync"] {
		c.Storage.Fsync = DefaultFsync
	}
	if !present["storage.open_threads"] {
		c.Storage.OpenThreads = DefaultOpenThreads
	}
	if !present["storage.rendered_cache_mb"] {
		c.Storage.RenderedCacheMB = DefaultRenderedCacheMB
	}
	if !present["storage.response_cache_mb"] {
		c.Storage.ResponseCacheMB = DefaultResponseCacheMB
	}
	if !present["log.level"] {
		c.Log.Level = DefaultLogLevel
	}
	if !present["log.format"] {
		c.Log.Format = DefaultLogFormat
	}
	if !present["limits.attachment_mb"] {
		c.Limits.AttachmentMB = DefaultAttachmentMB
	}
	if !present["limits.sse_connections"] {
		c.Limits.SSEConnections = DefaultSSEConnections
	}
	if !present["limits.sse_per_user"] {
		c.Limits.SSEPerUser = DefaultSSEPerUser
	}
	if !present["limits.sse_anonymous"] {
		c.Limits.SSEAnonymous = DefaultSSEAnonymous
	}
	for i := range c.OIDC.Providers {
		p := &c.OIDC.Providers[i]
		if len(p.Scopes) == 0 {
			p.Scopes = strings.Fields(DefaultProviderScopes)
		}
		if !slicesContains(p.Scopes, "openid") {
			// Not a default and not an error: a provider block without openid
			// is an OAuth2 flow, and this server only speaks OIDC. Adding it
			// is less surprising than failing on a scope list that looks
			// complete.
			p.Scopes = append([]string{"openid"}, p.Scopes...)
		}
	}
}

// resolveSecrets turns client_secret_file into a value. A relative path is
// resolved against the config file's directory so that a config directory can
// be moved as a unit.
func resolveSecrets(c *Config, dir string, p *problemList) []string {
	var warnings []string
	for i := range c.OIDC.Providers {
		pr := &c.OIDC.Providers[i]
		key := fmt.Sprintf("oidc.providers[%d]", i)
		if pr.Key != "" {
			key = fmt.Sprintf("oidc.providers.%s", pr.Key)
		}

		if pr.ClientSecretFile == "" {
			continue
		}
		if !pr.ClientSecret.IsZero() {
			p.add(key+".client_secret_file",
				"set together with client_secret; exactly one of the two must be given, so that there is never a question which one is in use")
			continue
		}
		path := pr.ClientSecretFile
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		s, err := readSecretFile(path)
		if err != nil {
			// The path is named, the reason is named, the contents are not
			// read into the message even on a partial read.
			p.add(key+".client_secret_file", fmt.Sprintf("cannot read %s: %v", path, redactPathError(err)))
			continue
		}
		pr.ClientSecret = s
		c.setSource(key+".client_secret", Source{Layer: LayerSecretFile, From: path})
		if w := secretFileWarning(path); w != "" {
			warnings = append(warnings, w)
		}
	}
	return warnings
}

// redactPathError keeps a filesystem error's reason without letting anything
// that might have been read into the message.
func redactPathError(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

func slicesContains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
