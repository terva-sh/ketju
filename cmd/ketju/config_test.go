package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var updateConfigGolden = flag.Bool("update", false, "rewrite testdata/config-check.golden")

// sentinel stands in for every secret in these tests. It must appear in no
// output of config check, on any exit.
const sentinel = "ketju-sentinel-7f3a"

// checkDir writes body as config.toml in a fresh directory, with files
// beside it, and returns the directory and the config's path.
func checkDir(t *testing.T, body string, files map[string]string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

func runCheck(getenv map[string]string, args ...string) (string, string, error) {
	var out, errOut bytes.Buffer
	err := configMain(&out, &errOut, append([]string{"check"}, args...),
		func(k string) string { return getenv[k] })
	return out.String(), errOut.String(), err
}

// TestConfigCheckFailsNamingKey is AC1: a config with a problem exits
// non-zero, and the message names the key, in text and in JSON.
func TestConfigCheckFailsNamingKey(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name, body string
		env        map[string]string
		key        string
	}{
		{"bad fsync", "data_dir = \"d\"\n[storage]\nfsync = \"gruop\"\n", nil, "storage.fsync"},
		{"missing data_dir", "listen = \":8080\"\n", nil, "data_dir"},
		{"a moved key", "data_dir = \"d\"\nsite_name = \"Ours\"\n", nil, "site_name"},
		{"a runtime setting", "data_dir = \"d\"\n[registration]\nmode = \"open\"\n", nil, "registration"},
		{"a runtime setting, dotted", "data_dir = \"d\"\nposts.edit_window_seconds = 60\n", nil, "posts.edit_window_seconds"},
		{"an unknown key", "data_dir = \"d\"\nlissten = \":1\"\n", nil, "lissten"},
		{"a bad response cache", "data_dir = \"d\"\n[storage]\nresponse_cache_mb = -1\n", nil, "storage.response_cache_mb"},
		{"a bad admin subject", "data_dir = \"d\"\n[bootstrap]\nadmin_subjects = [\"bare\"]\n", nil, "bootstrap.admin_subjects"},
		{"a bad admin email", "data_dir = \"d\"\n[bootstrap]\nadmin_emails = [\"nobody\"]\n", nil, "bootstrap.admin_emails"},
		{"a bad variable", "data_dir = \"d\"\n", map[string]string{"KETJU_STORAGE_OPEN_THREADS": "many"}, "storage.open_threads"},
		{"malformed TOML", "listen = \":1\"\ndata_dir = \n", nil, "data_dir"},
		{"malformed TOML in a table", "data_dir = \"d\"\n[storage]\nfsync = group\n", nil, "storage.fsync"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, path := checkDir(t, c.body, nil)

			out, _, err := runCheck(c.env, "--config", path)
			if code := exitCodeFor(err); code != exitInvalidConfig {
				t.Fatalf("exit %d (%v), want %d", code, err, exitInvalidConfig)
			}
			if !strings.Contains(err.Error(), c.key+":") {
				t.Errorf("the error does not name %q:\n%v", c.key, err)
			}
			if out != "" {
				t.Errorf("an invalid config printed an effective configuration:\n%s", out)
			}

			out, _, err = runCheck(c.env, "--config", path, "--json")
			if code := exitCodeFor(err); code != exitInvalidConfig {
				t.Fatalf("--json: exit %d, want %d", code, exitInvalidConfig)
			}
			var rep checkReport
			if err := json.Unmarshal([]byte(out), &rep); err != nil {
				t.Fatalf("--json output is not JSON: %v\n%s", err, out)
			}
			found := false
			for _, p := range rep.Problems {
				found = found || p.Key == c.key
			}
			if rep.Valid || !found {
				t.Errorf("--json: valid %v, problems %+v; want invalid naming %q", rep.Valid, rep.Problems, c.key)
			}
		})
	}
}

// TestConfigCheckExitCodes covers the codes other than 1: a valid config
// (warnings included) is 0, usage is 2, and a file that is not there is 4.
func TestConfigCheckExitCodes(t *testing.T) {
	t.Parallel()

	_, valid := checkDir(t, "data_dir = \"d\"\n[storage]\nfsync = \"never\"\n", nil)
	out, _, err := runCheck(nil, "--config", valid)
	if err != nil {
		t.Errorf("a valid config with a warning: %v", err)
	}
	if !strings.Contains(out, "# warnings\n#   storage.fsync:") {
		t.Errorf("the fsync warning is not printed:\n%s", out)
	}

	missing := filepath.Join(t.TempDir(), "nope.toml")
	if _, _, err := runCheck(nil, "--config", missing); exitCodeFor(err) != exitInternal {
		t.Errorf("a missing file: exit %d (%v), want %d", exitCodeFor(err), err, exitInternal)
	}
	out, _, err = runCheck(nil, "--config", missing, "--json")
	var rep checkReport
	if jerr := json.Unmarshal([]byte(out), &rep); jerr != nil || rep.Valid || !strings.Contains(rep.Error, "nope.toml") {
		t.Errorf("a missing file in --json: %+v (%v)", rep, jerr)
	}
	if exitCodeFor(err) != exitInternal {
		t.Errorf("a missing file in --json: exit %d, want %d", exitCodeFor(err), exitInternal)
	}

	for _, args := range [][]string{{"check", "extra"}, {"check", "--nope"}, {"frob"}, {}} {
		var o, e bytes.Buffer
		if err := configMain(&o, &e, args, func(string) string { return "" }); exitCodeFor(err) != exitUsage {
			t.Errorf("config %q: exit %d (%v), want %d", args, exitCodeFor(err), err, exitUsage)
		}
	}

	// The config path comes from KETJU_CONFIG when --config is absent, and
	// the report says so, and lists the variable as set.
	out, _, err = runCheck(map[string]string{"KETJU_CONFIG": valid})
	if err != nil {
		t.Errorf("KETJU_CONFIG: %v", err)
	}
	for _, want := range []string{"# the path was chosen by KETJU_CONFIG\n", "# * KETJU_CONFIG "} {
		if !strings.Contains(out, want) {
			t.Errorf("KETJU_CONFIG: the report lacks %q:\n%s", want, out)
		}
	}
	out, _, _ = runCheck(map[string]string{"KETJU_CONFIG": valid}, "--json")
	rep = checkReport{}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.ChosenBy != "KETJU_CONFIG" || len(rep.Overrides) == 0 ||
		rep.Overrides[0] != (checkOverride{Env: "KETJU_CONFIG", Key: "(the config file's path)", Set: true}) {
		t.Errorf("--json with KETJU_CONFIG: chosen_by %q, overrides %+v", rep.ChosenBy, rep.Overrides)
	}
}

// layered is a config whose keys come from every layer: the file, a
// variable, a variable's _FILE companion, a provider's client_secret_file,
// and the defaults.
const layered = `listen = "0.0.0.0:9000"
data_dir = "data"

[storage]
fsync = "always"

[bootstrap]
admin_subjects = ["club:8f2c61d4"]

[[oidc.providers]]
key = "club"
name = "Club SSO"
issuer = "https://auth.club.example"
client_id = "ketju"
client_secret_file = "club-secret"
groups_claim = "groups"
group_map = { "forum-mods" = "moderators" }
`

func layeredEnv(dir string) map[string]string {
	return map[string]string{
		"KETJU_LOG_LEVEL":     "debug",
		"KETJU_SITE_URL_FILE": filepath.Join(dir, "site-url"),
	}
}

var layeredFiles = map[string]string{
	"club-secret": sentinel + "\n",
	"site-url":    "https://forum.example.org\n",
}

// TestConfigCheckShowsLayers is AC2: the effective output merges the file,
// the environment and the defaults, and names the layer of each key. The
// whole text report is a golden file, because its shape is what an
// operator reads and pastes; go test -run TestConfigCheckShowsLayers
// -update rewrites it.
func TestConfigCheckShowsLayers(t *testing.T) {
	t.Parallel()

	dir, path := checkDir(t, layered, layeredFiles)
	out, errOut, err := runCheck(layeredEnv(dir), "--config", path)
	if err != nil {
		t.Fatalf("config check: %v\n%s", err, errOut)
	}

	for key, src := range map[string]string{
		"listen":                            "file",
		"data_dir":                          "file",
		"storage.fsync":                     "file",
		"log.level":                         "env KETJU_LOG_LEVEL",
		"site_url":                          "env-file KETJU_SITE_URL_FILE",
		"oidc.providers.club.client_secret": "secret-file " + filepath.Join(dir, "club-secret"),
		"storage.response_cache_mb":         "default",
		"log.format":                        "default",
		"bootstrap.admin_emails":            "default",
		"bootstrap.admin_subjects":          "file",
	} {
		line := lineFor(out, key+" = ")
		if !strings.HasSuffix(line, "# "+src) {
			t.Errorf("%s: line %q, want source %q", key, line, src)
		}
	}
	if line := lineFor(out, "data_dir = "); !strings.Contains(line, filepath.Join(dir, "data")) {
		t.Errorf("a relative data_dir is not shown resolved against the config file: %q", line)
	}
	if !strings.Contains(out, "# * KETJU_LOG_LEVEL") || !strings.Contains(out, "# * KETJU_SITE_URL ") {
		t.Errorf("the variables set here are not marked:\n%s", out)
	}
	if line := lineFor(out, "#   registration.mode = "); line == "" {
		t.Errorf("the runtime settings are not listed:\n%s", out)
	}

	// The golden run gives data_dir as an absolute path, so that no
	// temporary directory, whose name varies in length, sits in the
	// aligned value column. The paths left are in comments, after it.
	gdir, gpath := checkDir(t, strings.Replace(layered, `data_dir = "data"`, `data_dir = "/srv/ketju"`, 1), layeredFiles)
	out, errOut, err = runCheck(layeredEnv(gdir), "--config", gpath)
	if err != nil {
		t.Fatalf("config check: %v\n%s", err, errOut)
	}
	got := strings.ReplaceAll(out, gdir, "$DIR")
	golden := filepath.Join("testdata", "config-check.golden")
	if *updateConfigGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("reading the golden file (run with -update to create it): %v", err)
	}
	if got != string(want) {
		t.Errorf("config check output differs from %s (rerun with -update if the change is intended):\n%s", golden, got)
	}

	// --json carries the same keys and sources.
	out, _, err = runCheck(layeredEnv(dir), "--config", path, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var rep checkReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{}
	for _, k := range rep.Keys {
		sources[k.Key] = k.Source + " " + k.From
	}
	for key, want := range map[string]string{
		"listen":     "file ",
		"log.level":  "env KETJU_LOG_LEVEL",
		"site_url":   "env-file KETJU_SITE_URL_FILE",
		"log.format": "default ",
	} {
		if sources[key] != want {
			t.Errorf("--json %s: source %q, want %q", key, sources[key], want)
		}
	}
	if !rep.Valid || len(rep.Settings) == 0 || len(rep.Overrides) == 0 {
		t.Errorf("--json report is missing a section: valid %v, %d settings, %d overrides",
			rep.Valid, len(rep.Settings), len(rep.Overrides))
	}
}

func lineFor(out, prefix string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return ""
}

// TestConfigCheckNeverPrintsSecret is AC3. The sentinel is a provider
// secret given inline, through client_secret_file, through the
// environment, and through a _FILE companion, and it appears in no output
// on stdout or stderr, as text or JSON, on success or on failure.
func TestConfigCheckNeverPrintsSecret(t *testing.T) {
	t.Parallel()

	provider := func(key, secretLine string) string {
		return "\n[[oidc.providers]]\nkey = \"" + key + "\"\nname = \"N\"\nissuer = \"https://" + key +
			".example\"\nclient_id = \"c\"\n" + secretLine + "\n"
	}
	head := "data_dir = \"d\"\nsite_url = \"https://forum.example.org\"\n"
	cases := []struct {
		name  string
		body  string
		files map[string]string
		env   func(dir string) map[string]string
	}{
		{name: "inline", body: head + provider("a", `client_secret = "`+sentinel+`"`)},
		{name: "client_secret_file", body: head + provider("a", `client_secret_file = "s"`),
			files: map[string]string{"s": sentinel + "\n"}},
		{name: "variable", body: head + provider("a", `client_secret_file = "absent"`),
			env: func(string) map[string]string { return map[string]string{"KETJU_OIDC_A_CLIENT_SECRET": sentinel} }},
		{name: "variable file", body: head + provider("a", `client_secret = "x"`),
			files: map[string]string{"s": sentinel + "\n"},
			env: func(dir string) map[string]string {
				return map[string]string{"KETJU_OIDC_A_CLIENT_SECRET_FILE": filepath.Join(dir, "s")}
			}},
		// The same, beside a problem, so the failure path is covered too.
		{name: "inline, invalid", body: head + "listen = \"nope\"\n" + provider("a", `client_secret = "`+sentinel+`"`)},
		{name: "file, invalid", body: head + "listen = \"nope\"\n" + provider("a", `client_secret_file = "s"`),
			files: map[string]string{"s": sentinel + "\n"}},
		// A line the parser stops at: its message is not passed on, because
		// it can quote the line (TestABrokenLineDoesNotQuoteItsSecret).
		{name: "broken line", body: head + provider("a", `client_secret = "`+sentinel+`\q"`)},
		{name: "both set", body: head + provider("a", `client_secret = "`+sentinel+`"`+"\n"+`client_secret_file = "s"`),
			files: map[string]string{"s": sentinel + "\n"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir, path := checkDir(t, c.body, c.files)
			var env map[string]string
			if c.env != nil {
				env = c.env(dir)
			}
			for _, args := range [][]string{{"--config", path}, {"--config", path, "--json"}} {
				out, errOut, err := runCheck(env, args...)
				all := out + errOut
				if err != nil {
					all += err.Error()
				}
				if strings.Contains(all, sentinel) {
					t.Errorf("%v printed the secret:\n%s", args, all)
				}
				if err == nil && !strings.Contains(out, "[redacted]") {
					t.Errorf("%v: a set secret is not shown as redacted:\n%s", args, out)
				}
			}
		})
	}
}

// TestConfigCheckNeverReadsASecret is the static half of AC3: redaction is
// by the Secret type, and the one way to defeat it is to call Value, which
// the command's source must never do.
func TestConfigCheckNeverReadsASecret(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(src, []byte(".Value()")) {
		t.Error("config.go calls .Value(); config check must print secrets only through their own marshalling")
	}
}

// TestConfigCheckAsTheBinary runs `ketju config check` through the real
// main, as a deployment script would, and checks the exit codes it sees.
func TestConfigCheckAsTheBinary(t *testing.T) {
	t.Parallel()

	dir, valid := checkDir(t, layered, layeredFiles)
	_, invalid := checkDir(t, "data_dir = \"d\"\n[storage]\nfsync = \"gruop\"\n", nil)
	for _, c := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"config", "check", "--config", valid}, 0, "is valid"},
		{[]string{"config", "check", "--config", invalid}, 1, "storage.fsync:"},
		{[]string{"config", "check", "--bogus"}, 2, "usage: ketju config check"},
		{[]string{"config", "check", "--config", filepath.Join(dir, "nope.toml")}, 4, "does not exist"},
	} {
		cmd := exec.Command(os.Args[0], c.args...)
		cmd.Env = append(os.Environ(), "KETJU_AS_MAIN=1")
		for k, v := range layeredEnv(dir) {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		out, err := cmd.CombinedOutput()
		code := 0
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		if code != c.code || !strings.Contains(string(out), c.want) {
			t.Errorf("ketju %q: exit %d, want %d, and output lacking %q:\n%s", c.args, code, c.code, c.want, out)
		}
		if strings.Contains(string(out), sentinel) {
			t.Errorf("ketju %q printed the secret:\n%s", c.args, out)
		}
	}
}
