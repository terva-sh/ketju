package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvironmentOverridesTheFile(t *testing.T) {
	t.Parallel()

	body := `
listen = "127.0.0.1:8080"
data_dir = "/from/file"
[storage]
fsync = "always"
open_threads = 10
`
	c := mustLoad(t, write(t, body), env(map[string]string{
		"KETJU_LISTEN":               "0.0.0.0:9999",
		"KETJU_DATA_DIR":             "/from/env",
		"KETJU_STORAGE_FSYNC":        "group",
		"KETJU_STORAGE_OPEN_THREADS": "77",
	}))

	if c.Listen != "0.0.0.0:9999" {
		t.Errorf("listen = %q; the environment did not win", c.Listen)
	}
	if c.DataDir != "/from/env" {
		t.Errorf("data_dir = %q; the environment did not win", c.DataDir)
	}
	if c.Storage.Fsync != "group" {
		t.Errorf("fsync = %q; the environment did not win", c.Storage.Fsync)
	}
	if c.Storage.OpenThreads != 77 {
		t.Errorf("open_threads = %d; the environment did not win", c.Storage.OpenThreads)
	}
}

// TestFileValueSurvivesAnUnsetVariable is the other half of precedence: the
// environment only wins where it speaks.
func TestFileValueSurvivesAnUnsetVariable(t *testing.T) {
	t.Parallel()

	c := mustLoad(t, write(t, "data_dir = \"/from/file\"\nlisten = \"1.2.3.4:81\""), env(map[string]string{
		"KETJU_STORAGE_FSYNC": "never",
	}))
	if c.Listen != "1.2.3.4:81" {
		t.Errorf("listen = %q, want the file's value", c.Listen)
	}
	if c.Storage.Fsync != "never" {
		t.Errorf("fsync = %q, want the environment's value", c.Storage.Fsync)
	}
}

// TestAnEmptyVariableIsNotAnOverride: an unexpanded ${DATA_DIR} in a compose
// file should not become a config with no data directory.
func TestAnEmptyVariableIsNotAnOverride(t *testing.T) {
	t.Parallel()

	c := mustLoad(t, write(t, "data_dir = \"/from/file\""), env(map[string]string{
		"KETJU_DATA_DIR": "",
	}))
	if c.DataDir != "/from/file" {
		t.Errorf("data_dir = %q; an empty variable was treated as an override", c.DataDir)
	}
}

// TestAnOverrideBeatsTheDefaultForAnAbsentKey: the environment counts as the
// key being present, so a default does not overwrite it afterwards.
func TestAnOverrideBeatsTheDefaultForAnAbsentKey(t *testing.T) {
	t.Parallel()

	c := mustLoad(t, write(t, minimal), env(map[string]string{
		"KETJU_STORAGE_RENDERED_CACHE_MB": "0",
	}))
	if c.Storage.RenderedCacheMB != 0 {
		t.Errorf("rendered_cache_mb = %d; the default overwrote an explicit override of 0", c.Storage.RenderedCacheMB)
	}
}

func TestListOverridesSplitOnCommas(t *testing.T) {
	t.Parallel()

	c := mustLoad(t, write(t, minimal), env(map[string]string{
		"KETJU_BOOTSTRAP_ADMIN_EMAILS": " a@example.org , b@example.org ,",
	}))
	want := []string{"a@example.org", "b@example.org"}
	if len(c.Bootstrap.AdminEmails) != len(want) {
		t.Fatalf("admin_emails = %v, want %v", c.Bootstrap.AdminEmails, want)
	}
	for i := range want {
		if c.Bootstrap.AdminEmails[i] != want[i] {
			t.Errorf("admin_emails[%d] = %q, want %q", i, c.Bootstrap.AdminEmails[i], want[i])
		}
	}
}

func TestNonNumericOverrideIsRefusedNamingTheVariable(t *testing.T) {
	t.Parallel()

	e := loadErr(t, write(t, minimal), env(map[string]string{
		"KETJU_STORAGE_OPEN_THREADS": "many",
	}))
	if !hasKey(e, "storage.open_threads") {
		t.Fatalf("problems %v do not name the key\n%v", e.Keys(), e)
	}
	if !strings.Contains(e.Error(), "KETJU_STORAGE_OPEN_THREADS") {
		t.Errorf("error does not name the variable the operator set:\n%v", e)
	}
}

func TestFileCompanionSuppliesTheValue(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "datadir")
	if err := os.WriteFile(path, []byte("/from/a/file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := mustLoad(t, write(t, minimal), env(map[string]string{
		"KETJU_DATA_DIR_FILE": path,
	}))
	if c.DataDir != "/from/a/file" {
		t.Errorf("data_dir = %q, want the file's contents with the newline trimmed", c.DataDir)
	}
}

// TestBothFormsOfAVariableIsRefused: precedence between KETJU_X and
// KETJU_X_FILE would mean a deployment that sets both gets one of them
// silently, and the one it gets is the one nobody checked.
func TestBothFormsOfAVariableIsRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "datadir")
	if err := os.WriteFile(path, []byte("/from/a/file"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := loadErr(t, write(t, minimal), env(map[string]string{
		"KETJU_DATA_DIR":      "/from/env",
		"KETJU_DATA_DIR_FILE": path,
	}))
	if !hasKey(e, "KETJU_DATA_DIR") {
		t.Errorf("problems %v do not name the variable\n%v", e.Keys(), e)
	}
}

func TestUnreadableCompanionFileIsNamed(t *testing.T) {
	t.Parallel()

	e := loadErr(t, write(t, minimal), env(map[string]string{
		"KETJU_DATA_DIR_FILE": filepath.Join(t.TempDir(), "absent"),
	}))
	if !hasKey(e, "KETJU_DATA_DIR_FILE") {
		t.Errorf("problems %v do not name the companion variable\n%v", e.Keys(), e)
	}
}

// TestProviderOverridesAmendAConfiguredProvider: a secret can arrive from the
// environment, but a provider cannot be conjured from one.
func TestProviderOverridesAmendAConfiguredProvider(t *testing.T) {
	t.Parallel()

	body := minimal + `
site_url = "https://x.example"

[[oidc.providers]]
key = "club-sso"
name = "Club"
issuer = "https://old.example"
client_id = "old-id"
client_secret = "old-secret"
`
	c := mustLoad(t, write(t, body), env(map[string]string{
		"KETJU_OIDC_CLUB_SSO_ISSUER":        "https://new.example",
		"KETJU_OIDC_CLUB_SSO_CLIENT_ID":     "new-id",
		"KETJU_OIDC_CLUB_SSO_CLIENT_SECRET": "new-secret",
	}))
	p, ok := c.Provider("club-sso")
	if !ok {
		t.Fatal("provider missing")
	}
	if p.Issuer != "https://new.example" || p.ClientID != "new-id" {
		t.Errorf("provider = %+v; the environment did not win", p)
	}
	if p.ClientSecret.Value() != "new-secret" {
		t.Error("the client secret override did not take effect")
	}
}

// TestProviderSecretOverrideReplacesTheFileIndirection: a deployment that sets
// the variable should not also have to edit a config file it may not own.
func TestProviderSecretOverrideReplacesTheFileIndirection(t *testing.T) {
	t.Parallel()

	body := minimal + `
site_url = "https://x.example"

[[oidc.providers]]
key = "g"
name = "G"
issuer = "https://idp.example"
client_id = "c"
client_secret_file = "never-read"
`
	c := mustLoad(t, write(t, body), env(map[string]string{
		"KETJU_OIDC_G_CLIENT_SECRET": "from-env",
	}))
	p, _ := c.Provider("g")
	if p.ClientSecret.Value() != "from-env" {
		t.Errorf("secret = %q, want the environment's", p.ClientSecret.Value())
	}
	if p.ClientSecretFile != "" {
		t.Errorf("client_secret_file = %q; the override should have replaced it, not competed with it", p.ClientSecretFile)
	}
}

func TestProviderSecretCanComeFromAFileCompanion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	secret := filepath.Join(dir, "g-secret")
	if err := os.WriteFile(secret, []byte("mounted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := minimal + `
site_url = "https://x.example"

[[oidc.providers]]
key = "g"
name = "G"
issuer = "https://idp.example"
client_id = "c"
client_secret = "in-the-file"
`
	c := mustLoad(t, write(t, body), env(map[string]string{
		"KETJU_OIDC_G_CLIENT_SECRET_FILE": secret,
	}))
	p, _ := c.Provider("g")
	if p.ClientSecret.Value() != "mounted" {
		t.Errorf("secret = %q, want the mounted file's contents", p.ClientSecret.Value())
	}
}

// TestOverridesAreListable keeps the self-description honest: every variable
// the loader reads must appear in the list `ketju config check` prints, or the
// documentation is a guess.
func TestOverridesAreListable(t *testing.T) {
	t.Parallel()

	body := minimal + `
site_url = "https://x.example"

[[oidc.providers]]
key = "g"
name = "G"
issuer = "https://idp.example"
client_id = "c"
client_secret = "s"
`
	c := mustLoad(t, write(t, body), env(nil))
	list := c.Overrides()

	seenEnv := map[string]bool{}
	seenKey := map[string]bool{}
	for _, o := range list {
		if !strings.HasPrefix(o.Env, EnvPrefix) {
			t.Errorf("%q does not start with %q", o.Env, EnvPrefix)
		}
		if seenEnv[o.Env] {
			t.Errorf("%q is listed twice", o.Env)
		}
		if seenKey[o.Key] {
			t.Errorf("key %q is claimed by two variables", o.Key)
		}
		seenEnv[o.Env] = true
		seenKey[o.Key] = true
	}
	for _, want := range []string{
		"KETJU_LISTEN", "KETJU_DATA_DIR", "KETJU_SITE_URL",
		"KETJU_STORAGE_FSYNC", "KETJU_STORAGE_OPEN_THREADS",
		"KETJU_STORAGE_RENDERED_CACHE_MB", "KETJU_STORAGE_RESPONSE_CACHE_MB",
		"KETJU_LIMITS_ATTACHMENT_MB", "KETJU_LIMITS_UNATTACHED_UPLOAD_MB", "KETJU_LIMITS_SSE_CONNECTIONS",
		"KETJU_LIMITS_SSE_PER_USER", "KETJU_LIMITS_SSE_ANONYMOUS",
		"KETJU_BOOTSTRAP_ADMIN_EMAILS", "KETJU_BOOTSTRAP_ADMIN_SUBJECTS",
		"KETJU_OIDC_G_ISSUER", "KETJU_OIDC_G_CLIENT_ID", "KETJU_OIDC_G_CLIENT_SECRET",
	} {
		if !seenEnv[want] {
			t.Errorf("%q is read by the loader but not listed", want)
		}
	}

	// Every whole-config override in the table must be reachable, which is
	// what catches an entry added to the table and never wired to a field.
	if len(overrides) == 0 {
		t.Fatal("no overrides registered")
	}
	for _, o := range overrides {
		if o.apply == nil {
			t.Errorf("%q has no apply function", o.Env)
		}
	}
}

func TestProviderEnvNameReplacesIllegalCharacters(t *testing.T) {
	t.Parallel()

	for key, want := range map[string]string{
		"google":   "KETJU_OIDC_GOOGLE_CLIENT_SECRET",
		"club-sso": "KETJU_OIDC_CLUB_SSO_CLIENT_SECRET",
		"a_b":      "KETJU_OIDC_A_B_CLIENT_SECRET",
	} {
		if got := providerEnv(key, "CLIENT_SECRET"); got != want {
			t.Errorf("providerEnv(%q) = %q, want %q", key, got, want)
		}
	}
}
