package main

import (
	"bytes"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/auth/oidc/oidctest"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/lock"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/web/auth"
)

func runSetting(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := admin(t.Context(), &out, &errOut, append([]string{"setting"}, args...))
	return out.String(), errOut.String(), err
}

// settingAudits reads the setting.changed rows for key: actor and details.
func settingAudits(t *testing.T, root, key string) (actors []sql.NullInt64, details []string) {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(root, "db", "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(t.Context(), `SELECT actor_id, details_json FROM audit_log
			WHERE action = 'setting.changed' AND target_type = 'setting' AND target_id = ? ORDER BY id`, key)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a sql.NullInt64
			var dj string
			if err := rows.Scan(&a, &dj); err != nil {
				return err
			}
			actors, details = append(actors, a), append(details, dj)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	return actors, details
}

// set, get and list against a stopped instance's data directory.
func TestAdminSetting(t *testing.T) {
	root := dataDir(t, nil)

	out, _, err := runSetting(t, "get", "--root", root, "registration.mode")
	if err != nil || out != "approval\n" {
		t.Fatalf("get before set = %q, %v", out, err)
	}

	out, _, err = runSetting(t, "set", "--root", root, "registration.mode", "open")
	if err != nil || out != "registration.mode = \"open\"\n" {
		t.Fatalf("set = %q, %v", out, err)
	}
	actors, details := settingAudits(t, root, "registration.mode")
	if len(actors) != 1 || actors[0].Valid || details[0] != `{"from":"approval","to":"open","via":"cli"}` {
		t.Errorf("audit rows: actors %v, details %v", actors, details)
	}

	// A text value takes the words as typed; an integer takes a number.
	for _, kv := range [][3]string{
		{"site_name", "The Friends' Forum", "The Friends' Forum\n"},
		{"slow_mode.default_seconds", "30", "30\n"},
		{"public_edit_history", "false", "false\n"},
	} {
		if _, _, err := runSetting(t, "set", "--root", root, kv[0], kv[1]); err != nil {
			t.Fatalf("set %s %q: %v", kv[0], kv[1], err)
		}
		if out, _, err := runSetting(t, "get", "--root", root, kv[0]); err != nil || out != kv[2] {
			t.Errorf("get %s = %q, %v; want %q", kv[0], out, err, kv[2])
		}
	}

	out, _, err = runSetting(t, "list", "--root", root)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"registration.mode = \"open\"  # set; the default is \"approval\"\n" +
			"  text: one of \"open\", \"approval\"\n" +
			"  How a new visitor becomes a member: \"open\" or \"approval\".\n",
		"site_name = \"The Friends' Forum\"  # set; the default is \"Ketju\"\n",
		"slow_mode.default_seconds = 30  # set; the default is 0\n  integer: a whole number, 0 or more\n",
		"flags.auto_hide_threshold = 3  # default\n  integer: a whole number, 1 or more\n",
		"public_edit_history = false  # set; the default is true\n  boolean: true or false\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, " = "); n != len(settings.All()) {
		t.Errorf("list shows %d keys; there are %d:\n%s", n, len(settings.All()), out)
	}
}

func TestAdminSettingRefuses(t *testing.T) {
	root := dataDir(t, nil)
	empty := t.TempDir()
	cases := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"unknown key", []string{"set", "--root", root, "registration.modes", "open"}, exitNotFound,
			`unknown key: "registration.modes"; the keys are flags.auto_hide_threshold,`},
		{"get unknown key", []string{"get", "--root", root, "nope"}, exitNotFound, "the keys are"},
		{"unknown key, no database", []string{"get", "--root", empty, "nope"}, exitNotFound, "the keys are"},
		{"invite", []string{"set", "--root", root, "registration.mode", "invite"}, exitInvalidValue,
			`it takes one of "open", "approval"`},
		{"JSON quotes", []string{"set", "--root", root, "registration.mode", `"open"`}, exitInvalidValue,
			`it takes one of "open", "approval"`},
		{"not a number", []string{"set", "--root", root, "slow_mode.default_seconds", "thirty"}, exitInvalidValue,
			"it takes a whole number, 0 or more"},
		{"below the least", []string{"set", "--root", root, "flags.auto_hide_threshold", "0"}, exitInvalidValue,
			"it takes a whole number, 1 or more"},
		{"not a boolean", []string{"set", "--root", root, "public_edit_history", "yes"}, exitInvalidValue,
			"it takes true or false"},
		{"empty name", []string{"set", "--root", root, "site_name", ""}, exitInvalidValue,
			"it takes text of 1 to 100 characters"},
		{"set, no database", []string{"set", "--root", empty, "registration.mode", "open"}, exitInternal, "has no database"},
		{"list, no database", []string{"list", "--root", empty}, exitInternal, "has no database"},
		{"no subcommand", nil, exitUsage, "needs set, get or list"},
		{"unknown subcommand", []string{"unset", "site_name"}, exitUsage, `unknown admin setting subcommand "unset"`},
		{"set without a value", []string{"set", "--root", root, "registration.mode"}, exitUsage, "a key and a value"},
		{"set with two values", []string{"set", "--root", root, "site_name", "a", "b"}, exitUsage, "a key and a value"},
		{"get without a key", []string{"get", "--root", root}, exitUsage, "exactly one key"},
		{"list with a key", []string{"list", "--root", root, "site_name"}, exitUsage, "no arguments"},
		{"bad flag", []string{"list", "--nope"}, exitUsage, "nope"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := runSetting(t, c.args...)
			if err == nil || exitCodeFor(err) != c.code || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err %v (exit %d), want exit %d mentioning %q", err, exitCodeFor(err), c.code, c.want)
			}
		})
	}
	// Nothing refused was written, and a mistyped --root is left empty.
	if actors, _ := settingAudits(t, root, "registration.mode"); len(actors) != 0 {
		t.Errorf("refused sets wrote %d audit rows", len(actors))
	}
	if out, _, err := runSetting(t, "list", "--root", root); err != nil || strings.Contains(out, "# set") {
		t.Errorf("after refusals, list = %v:\n%s", err, out)
	}
	if entries, err := os.ReadDir(empty); err != nil || len(entries) != 0 {
		t.Errorf("the empty directory now holds %v (%v)", entries, err)
	}
}

// While serve holds the data directory, set refuses and changes nothing;
// get and list, which only read, still answer.
func TestAdminSettingWhileServeHoldsTheDirectory(t *testing.T) {
	root := dataDir(t, nil)
	lk, err := lock.Acquire(root, "serve")
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Release()

	_, _, err = runSetting(t, "set", "--root", root, "registration.mode", "open")
	var held *lock.HeldError
	if !errors.As(err, &held) || exitCodeFor(err) != exitInternal || !strings.Contains(err.Error(), "stop ketju serve") {
		t.Fatalf("set beside serve = %v (exit %d)", err, exitCodeFor(err))
	}
	if out, _, err := runSetting(t, "get", "--root", root, "registration.mode"); err != nil || out != "approval\n" {
		t.Errorf("get beside serve = %q, %v", out, err)
	}
	if _, _, err := runSetting(t, "list", "--root", root); err != nil {
		t.Errorf("list beside serve = %v", err)
	}
	if actors, _ := settingAudits(t, root, "registration.mode"); len(actors) != 0 {
		t.Errorf("a refused set wrote %d audit rows", len(actors))
	}
}

// The first run as README describes it, with the binary as an operator
// types it: serve once to create the data directory, find registration
// closed, stop serve, open registration, start serve again, and a
// newcomer registers.
func TestAdminSettingOpensRegistration(t *testing.T) {
	idp := oidctest.New(t)
	path, data := writeConfig(t, providerBlock(idp.URL))
	opts := serveOpts{idpClient: idp.Client(), getenv: siteURL}

	ketju := func(args ...string) (int, string) {
		t.Helper()
		cmd := exec.Command(os.Args[0], args...)
		cmd.Env = append(os.Environ(), "KETJU_AS_MAIN=1")
		out, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		switch {
		case errors.As(err, &exit):
			return exit.ExitCode(), string(out)
		case err != nil:
			t.Fatal(err)
		}
		return 0, string(out)
	}

	r := start(t, path, opts)
	// Registration is closed on a fresh install, and set refuses while
	// serve runs.
	if code, out := ketju("admin", "setting", "get", "--root", data, "registration.mode"); code != 0 || out != "approval\n" {
		t.Errorf("get on a fresh install: exit %d, %q", code, out)
	}
	if code, out := ketju("admin", "setting", "set", "--root", data, "registration.mode", "open"); code != exitInternal ||
		!strings.Contains(out, "stop ketju serve") {
		t.Errorf("set beside serve: exit %d:\n%s", code, out)
	}
	if err := r.shutdown(t); err != nil {
		t.Fatal(err)
	}

	if code, out := ketju("admin", "setting", "set", "--root", data, "registration.mode", "invite"); code != 1 ||
		!strings.Contains(out, `one of "open", "approval"`) {
		t.Errorf("set invite: exit %d:\n%s", code, out)
	}
	if code, out := ketju("admin", "setting", "set", "--root", data, "registration.mode", "open"); code != 0 ||
		out != "registration.mode = \"open\"\n" {
		t.Fatalf("set open: exit %d:\n%s", code, out)
	}

	r = start(t, path, opts)
	base := "http://" + r.addr
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	cookie := func(resp *http.Response, name string) *http.Cookie {
		t.Helper()
		for _, ck := range resp.Cookies() {
			if ck.Name == name && ck.Value != "" {
				return ck
			}
		}
		t.Fatalf("no %s cookie in the answer %d", name, resp.StatusCode)
		return nil
	}

	resp, err := c.Get(base + "/auth/fake/start")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	flow := cookie(resp, auth.FlowCookie)
	cb, _ := url.Parse(idp.Authorize(t, resp.Header.Get("Location")))
	req, _ := http.NewRequest("GET", base+cb.RequestURI(), nil)
	req.AddCookie(flow)
	resp, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != auth.RegisterPath {
		t.Fatalf("the callback answered %d to %q, not the handle screen", resp.StatusCode, resp.Header.Get("Location"))
	}
	reg := cookie(resp, auth.RegistrationCookie)

	req, _ = http.NewRequest("POST", base+auth.RegisterPath, strings.NewReader(url.Values{"handle": {"newcomer"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://forum.example")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(reg)
	resp, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("registering answered %d:\n%s", resp.StatusCode, b)
	}
	cookie(resp, session.CookieName)

	if err := r.shutdown(t); err != nil {
		t.Fatal(err)
	}
	if role, _, _, _ := roleIn(t, data, "newcomer"); role != "member" {
		t.Errorf("the newcomer's role is %q", role)
	}
}
