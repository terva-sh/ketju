package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// write puts a config file in a fresh directory and returns its path.
func write(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return path
}

// env builds a getenv from a map, so a precedence test never touches the
// process environment and the tests can run in parallel.
func env(pairs map[string]string) func(string) string {
	return func(k string) string { return pairs[k] }
}

// minimal is the smallest config that loads.
const minimal = `
data_dir = "/tmp/ketju-test-data"
`

func mustLoad(t *testing.T, path string, getenv func(string) string) *Config {
	t.Helper()
	c, _, err := Load(path, getenv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return c
}

// loadErr returns the structured error, failing if the load succeeded.
func loadErr(t *testing.T, path string, getenv func(string) string) *Error {
	t.Helper()
	_, _, err := Load(path, getenv)
	if err == nil {
		t.Fatal("Load succeeded; want an error")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("Load returned %T (%v); want *config.Error", err, err)
	}
	return ce
}

func hasKey(e *Error, key string) bool {
	for _, p := range e.Problems {
		if p.Key == key {
			return true
		}
	}
	return false
}

func TestLoadsAMinimalConfigWithDocumentedDefaults(t *testing.T) {
	t.Parallel()

	c := mustLoad(t, write(t, minimal), env(nil))
	if c.Listen != DefaultListen {
		t.Errorf("Listen = %q, want %q", c.Listen, DefaultListen)
	}
	if c.Storage.Fsync != DefaultFsync {
		t.Errorf("Fsync = %q, want %q", c.Storage.Fsync, DefaultFsync)
	}
	if c.Storage.OpenThreads != DefaultOpenThreads {
		t.Errorf("OpenThreads = %d, want %d", c.Storage.OpenThreads, DefaultOpenThreads)
	}
	if c.Storage.RenderedCacheMB != DefaultRenderedCacheMB {
		t.Errorf("RenderedCacheMB = %d, want %d", c.Storage.RenderedCacheMB, DefaultRenderedCacheMB)
	}
	if c.Storage.ResponseCacheMB != DefaultResponseCacheMB {
		t.Errorf("ResponseCacheMB = %d, want %d", c.Storage.ResponseCacheMB, DefaultResponseCacheMB)
	}
}

// TestAnExplicitZeroIsNotAnAbsentKey is the reason defaults are driven by
// presence rather than by zero values. Writing 0 means "off"; leaving the key
// out means "use the default", and conflating them silently substitutes 256 MB
// for what the operator asked for.
func TestLogDefaultsAndOverrides(t *testing.T) {
	c := mustLoad(t, write(t, minimal), env(nil))
	if c.Log.Level != DefaultLogLevel || c.Log.Format != DefaultLogFormat {
		t.Errorf("log defaults = %q/%q, want %q/%q",
			c.Log.Level, c.Log.Format, DefaultLogLevel, DefaultLogFormat)
	}

	c = mustLoad(t, write(t, minimal+"\n[log]\nlevel = \"debug\"\nformat = \"text\"\n"), env(nil))
	if c.Log.Level != "debug" || c.Log.Format != "text" {
		t.Errorf("log = %q/%q, want debug/text", c.Log.Level, c.Log.Format)
	}

	c = mustLoad(t, write(t, minimal), env(map[string]string{
		"KETJU_LOG_LEVEL": "warn", "KETJU_LOG_FORMAT": "json",
	}))
	if c.Log.Level != "warn" || c.Log.Format != "json" {
		t.Errorf("env override gave %q/%q, want warn/json", c.Log.Level, c.Log.Format)
	}
}

func TestABadLogLevelOrFormatIsNamed(t *testing.T) {
	for _, c := range []struct{ body, key string }{
		{minimal + "\n[log]\nlevel = \"verbose\"\n", "log.level"},
		{minimal + "\n[log]\nformat = \"yaml\"\n", "log.format"},
	} {
		e := loadErr(t, write(t, c.body), env(nil))
		// The problem has to name the key, or an operator reading "invalid
		// value" has to guess which of two settings they mistyped.
		if !hasKey(e, c.key) {
			t.Errorf("the error does not name %s: %v", c.key, e)
		}
	}
}

func TestAnExplicitZeroIsNotAnAbsentKey(t *testing.T) {
	t.Parallel()

	c := mustLoad(t, write(t, minimal+`
[storage]
rendered_cache_mb = 0
`), env(nil))
	if c.Storage.RenderedCacheMB != 0 {
		t.Errorf("rendered_cache_mb written as 0 became %d", c.Storage.RenderedCacheMB)
	}
	if c.Storage.ResponseCacheMB != DefaultResponseCacheMB {
		t.Errorf("the untouched sibling key became %d, want the default %d",
			c.Storage.ResponseCacheMB, DefaultResponseCacheMB)
	}
}

func TestLoadsTheFullExample(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src, err := os.ReadFile("testdata/full.toml")
	if err != nil {
		t.Fatalf("reading testdata: %v", err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "authelia-secret"), []byte("club-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// data_dir in the example is absolute and does not exist here, which is
	// allowed; point it somewhere real to keep the test honest about the rest.
	c := mustLoad(t, path, env(map[string]string{"KETJU_DATA_DIR": dir}))

	if c.Listen != "0.0.0.0:9000" || c.SiteURL != "https://forum.example.org" {
		t.Errorf("listen/site_url = %q/%q", c.Listen, c.SiteURL)
	}
	if c.Storage.Fsync != "always" || c.Storage.OpenThreads != 128 {
		t.Errorf("storage = %+v", c.Storage)
	}
	if len(c.OIDC.Providers) != 2 {
		t.Fatalf("got %d providers, want 2", len(c.OIDC.Providers))
	}
	authelia, ok := c.Provider("authelia")
	if !ok {
		t.Fatal("provider authelia missing")
	}
	if got := authelia.ClientSecret.Value(); got != "club-secret" {
		t.Errorf("client_secret_file resolved to %q, want %q", got, "club-secret")
	}
	if authelia.GroupMap["forum-mods"] != "moderators" {
		t.Errorf("group_map = %v", authelia.GroupMap)
	}
}

// TestInvalidValuesAreRefusedByName is the first acceptance criterion: a bad
// value stops startup and the message names the key. Each case asserts the key
// rather than the wording, because the wording is meant to be improved and the
// key is the contract.
func TestInvalidValuesAreRefusedByName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		key  string
	}{
		{"no data_dir", `listen = ":8080"`, "data_dir"},
		{"listen without a port", minimal + "\nlisten = \"localhost\"", "listen"},
		{"listen port out of range", minimal + "\nlisten = \":70000\"", "listen"},
		{"listen port not a number", minimal + "\nlisten = \":http\"", "listen"},
		{"unknown fsync policy", minimal + "\n[storage]\nfsync = \"gruop\"", "storage.fsync"},
		{"zero open threads", minimal + "\n[storage]\nopen_threads = 0", "storage.open_threads"},
		{"negative cache", minimal + "\n[storage]\nrendered_cache_mb = -1", "storage.rendered_cache_mb"},
		{"site_url is not a URL", minimal + "\nsite_url = \"forum.example.org\"", "site_url"},
		{"site_url has a query", minimal + "\nsite_url = \"https://x.example/?a=1\"", "site_url"},
		{"bad admin email", minimal + "\n[bootstrap]\nadmin_emails = [\"nobody\"]", "bootstrap.admin_emails"},
		{"bare admin subject", minimal + "\n[bootstrap]\nadmin_subjects = [\"1100000\"]", "bootstrap.admin_subjects"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := loadErr(t, write(t, tc.body), env(nil))
			if !hasKey(e, tc.key) {
				t.Errorf("problems %v do not name %q\n%v", e.Keys(), tc.key, e)
			}
		})
	}
}

func TestProviderProblemsAreRefusedByName(t *testing.T) {
	t.Parallel()

	base := minimal + "\nsite_url = \"https://x.example\"\n"
	cases := []struct {
		name string
		body string
		key  string
	}{
		{
			"no issuer",
			base + "\n[[oidc.providers]]\nkey = \"g\"\nname = \"G\"\nclient_id = \"c\"\nclient_secret = \"s\"",
			"oidc.providers.g.issuer",
		},
		{
			"plaintext issuer",
			base + "\n[[oidc.providers]]\nkey = \"g\"\nname = \"G\"\nissuer = \"http://idp.example\"\nclient_id = \"c\"\nclient_secret = \"s\"",
			"oidc.providers.g.issuer",
		},
		{
			"issuer with a trailing slash",
			base + "\n[[oidc.providers]]\nkey = \"g\"\nname = \"G\"\nissuer = \"https://idp.example/oidc/\"\nclient_id = \"c\"\nclient_secret = \"s\"",
			"oidc.providers.g.issuer",
		},
		{
			"no secret anywhere",
			base + "\n[[oidc.providers]]\nkey = \"g\"\nname = \"G\"\nissuer = \"https://idp.example\"\nclient_id = \"c\"",
			"oidc.providers.g.client_secret",
		},
		{
			"secret given twice",
			base + "\n[[oidc.providers]]\nkey = \"g\"\nname = \"G\"\nissuer = \"https://idp.example\"\nclient_id = \"c\"\nclient_secret = \"s\"\nclient_secret_file = \"f\"",
			"oidc.providers.g.client_secret_file",
		},
		{
			"missing key",
			base + "\n[[oidc.providers]]\nname = \"G\"\nissuer = \"https://idp.example\"\nclient_id = \"c\"\nclient_secret = \"s\"",
			"oidc.providers[0].key",
		},
		{
			"uppercase key",
			base + "\n[[oidc.providers]]\nkey = \"Google\"\nname = \"G\"\nissuer = \"https://idp.example\"\nclient_id = \"c\"\nclient_secret = \"s\"",
			"oidc.providers.Google.key",
		},
		{
			"group_map without a claim",
			base + "\n[[oidc.providers]]\nkey = \"g\"\nname = \"G\"\nissuer = \"https://idp.example\"\nclient_id = \"c\"\nclient_secret = \"s\"\ngroup_map = { a = \"b\" }",
			"oidc.providers.g.group_map",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := loadErr(t, write(t, tc.body), env(nil))
			if !hasKey(e, tc.key) {
				t.Errorf("problems %v do not name %q\n%v", e.Keys(), tc.key, e)
			}
		})
	}
}

// TestDuplicateProviderKeysAreRefused: the key is what an identity row points
// at, so two providers sharing one would silently merge two populations of
// users.
func TestDuplicateProviderKeysAreRefused(t *testing.T) {
	t.Parallel()

	body := minimal + `
site_url = "https://x.example"

[[oidc.providers]]
key = "sso"
name = "One"
issuer = "https://one.example"
client_id = "c"
client_secret = "s"

[[oidc.providers]]
key = "sso"
name = "Two"
issuer = "https://two.example"
client_id = "c"
client_secret = "s"
`
	e := loadErr(t, write(t, body), env(nil))
	if !hasKey(e, "oidc.providers.sso.key") {
		t.Errorf("problems %v do not name the duplicate key\n%v", e.Keys(), e)
	}
}

// TestSiteURLIsRequiredOnlyWithAProvider: a forum with no OIDC provider has
// nothing to build a redirect URI for, and demanding the key anyway would be a
// validation that exists to be satisfied rather than to catch anything.
func TestSiteURLIsRequiredOnlyWithAProvider(t *testing.T) {
	t.Parallel()

	if _, _, err := Load(write(t, minimal), env(nil)); err != nil {
		t.Errorf("a config with no providers and no site_url failed: %v", err)
	}
	body := minimal + `
[[oidc.providers]]
key = "g"
name = "G"
issuer = "https://idp.example"
client_id = "c"
client_secret = "s"
`
	e := loadErr(t, write(t, body), env(nil))
	if !hasKey(e, "site_url") {
		t.Errorf("problems %v do not name site_url\n%v", e.Keys(), e)
	}
}

func TestUnknownKeyIsRefusedByName(t *testing.T) {
	t.Parallel()

	e := loadErr(t, write(t, minimal+"\nlissten = \":8080\""), env(nil))
	if !hasKey(e, "lissten") {
		t.Errorf("problems %v do not name the unknown key\n%v", e.Keys(), e)
	}
}

// TestMovedKeysSayWhereTheyWent covers the split the ticket exists to protect.
// A runtime setting written into config.toml is refused, and the message names
// the settings table rather than saying "unknown key", which would invite the
// reader to hunt for a new spelling that does not exist.
func TestMovedKeysSayWhereTheyWent(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		minimal + "\n[registration]\nmode = \"approval\"",
		minimal + "\nsite_name = \"Our Forum\"",
	} {
		e := loadErr(t, write(t, body), env(nil))
		var found bool
		for _, p := range e.Problems {
			if strings.Contains(p.Msg, "settings table") {
				found = true
			}
		}
		if !found {
			t.Errorf("a moved key was refused without naming where it belongs:\n%v", e)
		}
	}
}

func TestAllProblemsAreReportedAtOnce(t *testing.T) {
	t.Parallel()

	body := `
listen = "localhost"
site_url = "nope"
[storage]
fsync = "sometimes"
open_threads = -3
`
	e := loadErr(t, write(t, body), env(nil))
	want := []string{"data_dir", "listen", "storage.fsync", "storage.open_threads"}
	for _, k := range want {
		if !hasKey(e, k) {
			t.Errorf("problems %v are missing %q", e.Keys(), k)
		}
	}
	if len(e.Problems) < len(want) {
		t.Errorf("got %d problems, want at least %d: %v", len(e.Problems), len(want), e.Keys())
	}
}

func TestSyncPolicyMapsToTheStore(t *testing.T) {
	t.Parallel()

	for body, want := range map[string]threadlog.SyncPolicy{
		minimal: threadlog.SyncGroup,
		minimal + "\n[storage]\nfsync = \"always\"": threadlog.SyncAlways,
		minimal + "\n[storage]\nfsync = \"never\"":  threadlog.SyncNever,
	} {
		c := mustLoad(t, write(t, body), env(nil))
		if got := c.SyncPolicy(); got != want {
			t.Errorf("fsync %q mapped to %v, want %v", c.Storage.Fsync, got, want)
		}
	}
}

func TestMissingFileIsNamed(t *testing.T) {
	t.Parallel()

	_, _, err := Load(filepath.Join(t.TempDir(), "nope.toml"), env(nil))
	if err == nil {
		t.Fatal("loading a missing file succeeded")
	}
	if !strings.Contains(err.Error(), "nope.toml") {
		t.Errorf("error %q does not name the file", err)
	}
}

func TestMalformedTOMLNamesTheLine(t *testing.T) {
	t.Parallel()

	_, _, err := Load(write(t, "listen = \n"), env(nil))
	if err == nil {
		t.Fatal("loading malformed TOML succeeded")
	}
	if !strings.Contains(err.Error(), "line") {
		t.Errorf("error %q does not name a line", err)
	}
}

// TestMovedKeysAreSettings: every key config.toml refuses as "moved to the
// settings table" is a settings key, or a table whose keys are, so the message
// never sends a reader to a setting that does not exist.
func TestMovedKeysAreSettings(t *testing.T) {
	var names []string
	for _, s := range settings.All() {
		names = append(names, s.Name)
	}
	for moved := range movedKeys {
		found := false
		for _, n := range names {
			if n == moved || strings.HasPrefix(n, moved+".") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("movedKeys sends %q to the settings table, which has no such key", moved)
		}
	}
}

// A relative data_dir names a directory beside the config file, whatever the
// working directory; an absolute one, from the file or the environment, is
// left alone.
func TestRelativeDataDirResolvesAgainstTheConfigFile(t *testing.T) {
	path := write(t, "data_dir = \"data\"\n")
	t.Chdir(t.TempDir())
	c := mustLoad(t, path, env(nil))
	if want := filepath.Join(filepath.Dir(path), "data"); c.DataDir != want {
		t.Errorf("data_dir = %q, want %q", c.DataDir, want)
	}
	if c := mustLoad(t, path, env(map[string]string{"KETJU_DATA_DIR": "rel/env"})); c.DataDir != filepath.Join(filepath.Dir(path), "rel", "env") {
		t.Errorf("a relative data_dir from the environment = %q", c.DataDir)
	}
	if c := mustLoad(t, write(t, "data_dir = \"/srv/ketju\"\n"), env(nil)); c.DataDir != "/srv/ketju" {
		t.Errorf("an absolute data_dir = %q", c.DataDir)
	}
}

// The cookie key lives under the data directory unless the file says
// otherwise, and a relative cookie_key_file is beside the config file, like
// data_dir.
func TestCookieKeyPath(t *testing.T) {
	path := write(t, "data_dir = \"/srv/ketju\"\n")
	if c := mustLoad(t, path, env(nil)); c.CookieKeyPath() != "/srv/ketju/secrets/cookie.key" {
		t.Errorf("the default cookie key path = %q", c.CookieKeyPath())
	}
	rel := write(t, "data_dir = \"/srv/ketju\"\ncookie_key_file = \"keys/cookie.key\"\n")
	t.Chdir(t.TempDir())
	if c := mustLoad(t, rel, env(nil)); c.CookieKeyPath() != filepath.Join(filepath.Dir(rel), "keys", "cookie.key") {
		t.Errorf("a relative cookie_key_file = %q", c.CookieKeyPath())
	}
	if c := mustLoad(t, path, env(map[string]string{"KETJU_COOKIE_KEY_FILE": "/run/secrets/cookie"})); c.CookieKeyPath() != "/run/secrets/cookie" {
		t.Errorf("cookie_key_file from the environment = %q", c.CookieKeyPath())
	}
}
