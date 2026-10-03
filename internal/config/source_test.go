package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/settings"
)

// loadFull loads testdata/full.toml from a directory holding the secret
// file it names.
func loadFull(t *testing.T) *Config {
	t.Helper()
	dir := t.TempDir()
	src, err := os.ReadFile("testdata/full.toml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "authelia-secret"), []byte("club-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return mustLoad(t, path, env(nil))
}

// entryMap indexes Entries by key.
func entryMap(t *testing.T, c *Config) map[string]Entry {
	t.Helper()
	out := map[string]Entry{}
	for _, e := range c.Entries() {
		if _, dup := out[e.Key]; dup {
			t.Fatalf("Entries lists %q twice", e.Key)
		}
		out[e.Key] = e
	}
	return out
}

// TestSourcesNameEachLayer loads one config whose keys come from each layer
// and asserts every key's source: the file, a variable, a variable's _FILE
// companion, a provider's client_secret_file, and the defaults.
func TestSourcesNameEachLayer(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
listen = "0.0.0.0:9000"
data_dir = "data"
[storage]
rendered_cache_mb = 0
[[oidc.providers]]
key = "club"
name = "Club"
issuer = "https://auth.club.example"
client_id = "ketju"
client_secret_file = "club-secret"
[[oidc.providers]]
key = "google"
name = "Google"
issuer = "https://accounts.google.com"
client_id = "g"
client_secret_file = "never-read"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "club-secret"), []byte("s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	siteURL := filepath.Join(dir, "site-url")
	if err := os.WriteFile(siteURL, []byte("https://forum.example.org\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := mustLoad(t, path, env(map[string]string{
		"KETJU_LOG_LEVEL":                 "debug",
		"KETJU_SITE_URL_FILE":             siteURL,
		"KETJU_OIDC_GOOGLE_CLIENT_SECRET": "from-env",
	}))
	got := entryMap(t, c)

	for key, want := range map[string]Source{
		"listen":                                         {Layer: LayerFile},
		"data_dir":                                       {Layer: LayerFile},
		"storage.rendered_cache_mb":                      {Layer: LayerFile},
		"log.level":                                      {Layer: LayerEnv, From: "KETJU_LOG_LEVEL"},
		"site_url":                                       {Layer: LayerEnvFile, From: "KETJU_SITE_URL_FILE"},
		"oidc.providers.club.client_secret":              {Layer: LayerSecretFile, From: filepath.Join(dir, "club-secret")},
		"oidc.providers.club.client_secret_file":         {Layer: LayerFile},
		"oidc.providers.club.issuer":                     {Layer: LayerFile},
		"oidc.providers.club.scopes":                     {Layer: LayerDefault},
		"oidc.providers.google.client_secret":            {Layer: LayerEnv, From: "KETJU_OIDC_GOOGLE_CLIENT_SECRET"},
		"oidc.providers.google.client_secret_file":       {Layer: LayerEnv, From: "KETJU_OIDC_GOOGLE_CLIENT_SECRET"},
		"storage.fsync":                                  {Layer: LayerDefault},
		"storage.response_cache_mb":                      {Layer: LayerDefault},
		"log.format":                                     {Layer: LayerDefault},
		"limits.attachment_mb":                           {Layer: LayerDefault},
		"limits.unattached_upload_mb":                    {Layer: LayerDefault},
		"limits.sse_connections":                         {Layer: LayerDefault},
		"limits.sse_per_user":                            {Layer: LayerDefault},
		"limits.sse_anonymous":                           {Layer: LayerDefault},
		"bootstrap.admin_subjects":                       {Layer: LayerDefault},
		"oidc.providers.club.insecure_skip_issuer_check": {Layer: LayerDefault},
	} {
		e, ok := got[key]
		if !ok {
			t.Errorf("Entries has no %q", key)
			continue
		}
		if e.Source != want {
			t.Errorf("%s: source %v, want %v", key, e.Source, want)
		}
	}
	if v := got["storage.rendered_cache_mb"].Value; v != 0 {
		t.Errorf("an explicit 0 from the file listed as %v", v)
	}
	if v := got["log.level"].Value; v != "debug" {
		t.Errorf("log.level listed as %v, want the variable's value", v)
	}
}

// TestEntriesListEveryKey: every leaf field of Config is listed, so a key
// added to the struct cannot be missing from `ketju config check`. It counts
// the leaves by walking the type independently of Entries' own walk.
func TestEntriesListEveryKey(t *testing.T) {
	t.Parallel()

	c := loadFull(t)
	got := entryMap(t, c)

	var count func(reflect.Type) int
	count = func(t reflect.Type) int {
		n := 0
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			switch {
			case f.Type == reflect.TypeFor[Secret]():
				n++
			case f.Type.Kind() == reflect.Struct:
				n += count(f.Type)
			case f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() == reflect.Struct:
				n += len(c.OIDC.Providers) * count(f.Type.Elem())
			default:
				n++
			}
		}
		return n
	}
	if want := count(reflect.TypeFor[Config]()); len(got) != want {
		t.Errorf("Entries lists %d keys, Config has %d", len(got), want)
	}

	// full.toml sets every key it mentions to a non-default value; each of
	// them must be listed as coming from the file.
	for _, key := range []string{
		"listen", "data_dir", "site_url", "log.level", "log.format", "storage.fsync",
		"storage.open_threads", "storage.rendered_cache_mb", "storage.response_cache_mb",
		"bootstrap.admin_emails", "bootstrap.admin_subjects",
		"oidc.providers.google.client_secret", "oidc.providers.google.scopes",
		"oidc.providers.google.allowed_email_domains", "oidc.providers.authelia.groups_claim",
		"oidc.providers.authelia.group_map",
	} {
		if s := got[key].Source; s.Layer != LayerFile {
			t.Errorf("%s: source %v, want file", key, s)
		}
	}
	// The one secret-file key is listed as the file it came from.
	if s := got["oidc.providers.authelia.client_secret"].Source; s.Layer != LayerSecretFile {
		t.Errorf("authelia client_secret: source %v, want secret-file", s)
	}
}

// TestEntriesCarryASecretAsASecret: the value Entries hands a printer for a
// secret is the Secret type, never its string, so every printer redacts it.
func TestEntriesCarryASecretAsASecret(t *testing.T) {
	t.Parallel()

	c := loadFull(t)
	n := 0
	for _, e := range c.Entries() {
		if strings.HasSuffix(e.Key, ".client_secret") {
			n++
			if _, ok := e.Value.(Secret); !ok {
				t.Errorf("%s is listed as %T, not a Secret", e.Key, e.Value)
			}
		}
		if s, ok := e.Value.(string); ok && strings.Contains(s, "google-secret-in-file-please") {
			t.Errorf("%s carries the secret as a plain string", e.Key)
		}
	}
	if n != 2 {
		t.Errorf("found %d client_secret entries, want 2", n)
	}
}

// TestEveryRuntimeSettingIsRefusedNamingTheSettingsTable is the other half
// of TestMovedKeysAreSettings: each key the settings table holds, written
// into config.toml, is refused with a message sending the reader there,
// not with "unknown key".
func TestEveryRuntimeSettingIsRefusedNamingTheSettingsTable(t *testing.T) {
	t.Parallel()

	for _, s := range settings.All() {
		e := loadErr(t, write(t, minimal+"\n"+s.Name+" = "+s.Default+"\n"), env(nil))
		found := false
		for _, p := range e.Problems {
			if strings.Contains(p.Msg, "settings table") {
				found = true
			}
		}
		if !found {
			t.Errorf("%s in config.toml was refused without naming the settings table:\n%v", s.Name, e)
		}
	}
}

// TestWarningsNameTheirKeyAndDoNotFail: what docs/02 lists as a warning
// loads, and says which key it is about.
func TestWarningsNameTheirKeyAndDoNotFail(t *testing.T) {
	t.Parallel()

	provider := `
site_url = "https://forum.example.org"
[[oidc.providers]]
key = "odd"
name = "Odd"
issuer = "https://odd.example"
client_id = "c"
client_secret = "s"
`
	for _, c := range []struct{ body, key string }{
		{minimal + "\n[storage]\nfsync = \"never\"", "storage.fsync:"},
		{minimal + provider + "insecure_skip_issuer_check = true\n", "oidc.providers.odd.insecure_skip_issuer_check:"},
	} {
		_, warnings, err := Load(write(t, c.body), env(nil))
		if err != nil {
			t.Errorf("%s: a warning failed the load: %v", c.key, err)
			continue
		}
		if len(warnings) != 1 || !strings.HasPrefix(warnings[0], c.key) {
			t.Errorf("warnings %q, want one opening %q", warnings, c.key)
		}
	}

	_, warnings, err := Load(write(t, minimal), env(nil))
	if err != nil || len(warnings) != 0 {
		t.Errorf("a minimal config: warnings %q, err %v; want neither", warnings, err)
	}
}

// TestAutoApproveIsNotAWarning: auto_approve lets a provider register people
// under approval mode now (TKT-01M29CKRE9), so it is a plain setting, not
// one the check warns about.
func TestAutoApproveIsNotAWarning(t *testing.T) {
	t.Parallel()

	body := minimal + `
site_url = "https://forum.example.org"
[[oidc.providers]]
key = "club"
name = "Club SSO"
issuer = "https://sso.club.example"
client_id = "c"
client_secret = "s"
auto_approve = true
`
	c, warnings, err := Load(write(t, body), env(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings %q, want none", warnings)
	}
	if len(c.OIDC.Providers) != 1 || !c.OIDC.Providers[0].AutoApprove {
		t.Errorf("auto_approve did not load: %+v", c.OIDC.Providers)
	}
}

// A missing file is distinguishable from an invalid one, which is how
// config check tells exit 4 from exit 1.
func TestMissingFileIsNotExist(t *testing.T) {
	t.Parallel()

	_, _, err := Load(filepath.Join(t.TempDir(), "nope.toml"), env(nil))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err %v does not wrap fs.ErrNotExist", err)
	}
	var ce *Error
	if errors.As(err, &ce) {
		t.Errorf("a missing file is a *config.Error: %v", err)
	}
}

// A syntax error carries no key from the parser; the loader reads it off
// the line, under the table above it, and falls back to "(file)" when the
// line names none.
func TestASyntaxErrorNamesTheKeyOnItsLine(t *testing.T) {
	t.Parallel()

	for body, want := range map[string]string{
		"listen = \n":                                  "listen",
		minimal + "[storage]\nfsync = group\n":         "storage.fsync",
		minimal + "[[oidc.providers]]\nkey = google\n": "oidc.providers.key",
		"= 3\n":                "(file)",
		minimal + "[storage\n": "(file)",
	} {
		e := loadErr(t, write(t, body), env(nil))
		if !hasKey(e, want) {
			t.Errorf("%q: problems %v, want %q", body, e.Keys(), want)
		}
	}
}

// TestABrokenLineDoesNotQuoteItsSecret: a client_secret on the line the
// parser stops at does not reach the problem text. go-toml's messages can
// quote the file (an invalid escape's character, a number's text), so none
// of them is passed on; the message is the key, the position, and the type
// the key expects.
func TestABrokenLineDoesNotQuoteItsSecret(t *testing.T) {
	t.Parallel()

	const secret = "ketju-sentinel-7f3a"
	head := minimal + "site_url = \"https://forum.example.org\"\n[[oidc.providers]]\nkey = \"a\"\nname = \"A\"\nissuer = \"https://a.example\"\nclient_id = \"c\"\n"
	for _, line := range []string{
		`client_secret = "` + secret + `\q"`,
		`client_secret = "` + secret,
		`client_secret = '` + secret + "\x01'",
		`client_secret = ` + secret,
		`client_secret = 7e3a` + secret,
		`client_secret = 12345` + secret,
		`client_secret = [ "` + secret + `" ]`,
		`client_secret = { v = "` + secret + `" }`,
		`client_secret = "ok" "` + secret + `"`,
	} {
		_, _, err := Load(write(t, head+line+"\n"), env(nil))
		if err == nil {
			t.Errorf("%q loaded", line)
			continue
		}
		msg := err.Error()
		for _, bad := range []string{secret, "U+", "toml:", "'q'", "7e3a"} {
			if strings.Contains(msg, bad) {
				t.Errorf("%q: the problem quotes the file (%q):\n%s", line, bad, msg)
			}
		}
		// An inline table where a string belongs is reported as the unknown
		// key inside it, which names the key and not the value.
		if !strings.Contains(msg, "line 9") && !strings.Contains(msg, "unknown key") {
			t.Errorf("%q: the problem does not give the line:\n%s", line, msg)
		}
	}
}

// A type mismatch names what the key expects rather than quoting the value.
func TestATypeMismatchSaysWhatTheKeyExpects(t *testing.T) {
	t.Parallel()

	for body, want := range map[string]string{
		minimal + "[storage]\nopen_threads = \"many\"\n":  "storage.open_threads: the value is not a whole number",
		minimal + "listen = 8080\n":                       "listen: the value is not a string",
		minimal + "[bootstrap]\nadmin_emails = \"a@b\"\n": "bootstrap.admin_emails: the value is not a list of strings",
	} {
		_, _, err := Load(write(t, body), env(nil))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", body, err, want)
		}
	}
}
