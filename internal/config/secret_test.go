package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// theSecret is a value distinctive enough that finding it anywhere in an
// output string is unambiguous.
const theSecret = "s3cr3t-tulipaani-9c1f"

// TestASecretSurvivesNoRouteToOutput is the third acceptance criterion, and it
// is written as a sweep rather than as one assertion because the ways a string
// reaches a log file are many and each one was closed deliberately.
//
// Every entry here is a real route: %v and %s are what a handler writes, %+v
// and %#v are what a debug line writes, JSON is the effective-config dump, and
// slog is every structured log line in the server.
func TestASecretSurvivesNoRouteToOutput(t *testing.T) {
	t.Parallel()

	s := NewSecret(theSecret)
	renders := map[string]string{
		"%v":       fmt.Sprintf("%v", s),
		"%s":       fmt.Sprintf("%s", s),
		"%q":       fmt.Sprintf("%q", s),
		"%+v":      fmt.Sprintf("%+v", s),
		"%#v":      fmt.Sprintf("%#v", s),
		"String()": s.String(),
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	renders["json"] = string(b)

	txt, err := s.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}
	renders["text"] = string(txt)

	var log bytes.Buffer
	slog.New(slog.NewTextHandler(&log, nil)).Info("check", "secret", s)
	renders["slog"] = log.String()

	for what, got := range renders {
		if strings.Contains(got, theSecret) {
			t.Errorf("%s leaked the secret: %s", what, got)
		}
		if !strings.Contains(got, Redacted) && what != "text" {
			t.Errorf("%s = %s, which does not say it was redacted", what, got)
		}
	}

	if s.Value() != theSecret {
		t.Error("Value() does not return the secret, so nothing could authenticate")
	}
}

// TestASecretInsideAStructIsStillRedacted covers the case that actually
// happens: nobody logs a Secret, they log the config that contains one.
func TestASecretInsideAStructIsStillRedacted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	secretPath := filepath.Join(dir, "client-secret")
	if err := os.WriteFile(secretPath, []byte(theSecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	body := `
data_dir = "` + dir + `"
site_url = "https://x.example"

[[oidc.providers]]
key = "g"
name = "G"
issuer = "https://idp.example"
client_id = "c"
client_secret_file = "client-secret"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	c := mustLoad(t, path, env(nil))
	p, _ := c.Provider("g")
	if p.ClientSecret.Value() != theSecret {
		t.Fatalf("the secret was not loaded from its file; got %q", p.ClientSecret.Value())
	}

	effective, err := c.Effective()
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}

	var log bytes.Buffer
	slog.New(slog.NewTextHandler(&log, nil)).Info("effective config", "config", c)

	jsonBytes, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("json.Marshal(config): %v", err)
	}

	renders := map[string]string{
		"Effective()":  effective,
		"%v":           fmt.Sprintf("%v", c),
		"%+v":          fmt.Sprintf("%+v", c),
		"%#v":          fmt.Sprintf("%#v", *c),
		"slog":         log.String(),
		"json":         string(jsonBytes),
		"%v provider":  fmt.Sprintf("%v", p),
		"%+v provider": fmt.Sprintf("%+v", p),
	}
	for what, got := range renders {
		if strings.Contains(got, theSecret) {
			t.Errorf("%s leaked the secret:\n%s", what, got)
		}
	}
	if !strings.Contains(effective, Redacted) {
		t.Errorf("the effective config does not show that a secret was redacted:\n%s", effective)
	}
}

// TestALoadFailureDoesNotQuoteASecret: an error message is a log line, and a
// failure that happens after the secret is read must not carry it.
func TestALoadFailureDoesNotQuoteASecret(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	secretPath := filepath.Join(dir, "client-secret")
	if err := os.WriteFile(secretPath, []byte(theSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	// Valid enough to reach the secret, invalid enough to fail afterwards.
	body := `
data_dir = "` + dir + `"
site_url = "https://x.example"

[[oidc.providers]]
key = "g"
name = "G"
issuer = "http://plaintext.example"
client_id = "c"
client_secret_file = "client-secret"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	e := loadErr(t, path, env(nil))
	if strings.Contains(e.Error(), theSecret) {
		t.Errorf("the failure message carries the secret:\n%v", e)
	}
}

func TestSecretFileTrimsOneTrailingNewline(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for name, contents := range map[string]string{
		"plain": theSecret,
		"lf":    theSecret + "\n",
		"crlf":  theSecret + "\r\n",
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := readSecretFile(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if s.Value() != theSecret {
			t.Errorf("%s: got %q, want %q", name, s.Value(), theSecret)
		}
	}
}

func TestEmptySecretFileIsRefused(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSecretFile(path); err == nil {
		t.Error("an empty secret file was accepted; the provider would fail at login with a wrong-secret error instead")
	}
}

// TestAWritableSecretFileWarnsRatherThanRefuses: bind mounts and projected
// volumes land with modes the application does not control, so a forum that
// will not boot because of a file mode helps nobody — but 0666 on a client
// secret is a real finding and has to be said out loud.
func TestAWritableSecretFileWarnsRatherThanRefuses(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	secretPath := filepath.Join(dir, "client-secret")
	if err := os.WriteFile(secretPath, []byte(theSecret), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(secretPath, 0o666); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	body := `
data_dir = "` + dir + `"
site_url = "https://x.example"

[[oidc.providers]]
key = "g"
name = "G"
issuer = "https://idp.example"
client_id = "c"
client_secret_file = "client-secret"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	c, warnings, err := Load(path, env(nil))
	if err != nil {
		t.Fatalf("a world-writable secret file stopped startup: %v", err)
	}
	if c == nil {
		t.Fatal("no config returned")
	}
	if len(warnings) == 0 {
		t.Fatal("a world-writable secret file produced no warning")
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "client-secret") {
		t.Errorf("the warning does not name the file:\n%s", joined)
	}
	if strings.Contains(joined, theSecret) {
		t.Errorf("the warning carries the secret:\n%s", joined)
	}
}

func TestZeroSecretRendersEmptyRatherThanRedacted(t *testing.T) {
	t.Parallel()

	var s Secret
	if !s.IsZero() {
		t.Error("the zero Secret does not report itself as zero")
	}
	if s.String() != "" {
		t.Errorf("an unset secret renders as %q; it should be empty, so that "+
			"the effective config does not claim a secret is configured", s.String())
	}
}
