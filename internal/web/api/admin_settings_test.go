package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Tests for the site settings routes (TKT-01M29CXK2H), through the real
// session middleware as the admin world's viewers, and, where a setting's
// effect needs threads, through the thread world.

const adminSettings = "/api/v1/admin/settings"

func settingPath(key string) string { return adminSettings + "/" + key }

// settingsByKey indexes a listing.
func settingsByKey(l dto.SiteSettings) map[string]dto.SiteSetting {
	out := map[string]dto.SiteSetting{}
	for _, s := range l.Settings {
		out[s.Key] = s
	}
	return out
}

// settingRow is the settings row for key as "value by at", or "none".
func (w *adminWorld) settingRow(key string) string {
	w.t.Helper()
	var out string
	if err := w.db.Read(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		row, err := q.GetSetting(w.t.Context(), key)
		if err == sql.ErrNoRows {
			out = "none"
			return nil
		}
		out = fmt.Sprintf("%s by %d at %d", row.ValueJson, row.UpdatedBy.Int64, row.UpdatedAt)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// TestSettingsScreenListsEveryKey is the first criterion's listing: GET
// lists exactly settings.All(), in its order, each key with its doc, kind,
// allowed text and default, and the choices of a key with a closed list.
// The config.toml sections follow, each with a reason and no values.
func TestSettingsScreenListsEveryKey(t *testing.T) {
	w := newAdminWorld(t)
	l := decodeAs[dto.SiteSettings](t, w.call("admin", "GET", adminSettings, ""))
	all := settings.All()
	if len(l.Settings) != len(all) || len(all) != 12 {
		t.Fatalf("%d settings listed, %d in the registry, want 12", len(l.Settings), len(all))
	}
	for i, want := range all {
		got := l.Settings[i]
		if got.Key != want.Name || got.Doc != want.Doc || got.Kind != want.Kind || got.Allowed != want.Allowed ||
			string(got.Default) != want.Default || string(got.Value) != want.Default || !got.IsDefault ||
			got.UpdatedAt != nil || got.UpdatedBy != nil {
			t.Errorf("setting %d: %+v, want %+v at its default and never set", i, got, want)
		}
		wantChoices := want.Choices
		if wantChoices == nil {
			wantChoices = []string{}
		}
		if got.Choices == nil || !slices.Equal(got.Choices, wantChoices) {
			t.Errorf("%s: choices %#v, want %#v", got.Key, got.Choices, wantChoices)
		}
	}
	if got := settingsByKey(l)["registration.mode"].Choices; !slices.Equal(got, []string{"open", "approval"}) {
		t.Errorf("registration.mode offers %q", got)
	}
	if len(l.Config) == 0 {
		t.Fatal("no config.toml sections")
	}
	for _, s := range l.Config {
		if s.Reason == "" || len(s.Keys) == 0 {
			t.Errorf("the section %s has no reason or no keys: %+v", s.Name, s)
		}
	}
	// Named, never shown: the answer holds no config value. The world's
	// server is built with no config at all, so what could leak is a
	// default; the listen default is the one a value would be.
	if rec := w.call("admin", "GET", adminSettings, ""); strings.Contains(rec.Body.String(), "127.0.0.1") {
		t.Errorf("the listing carries a config value: %s", rec.Body)
	}
}

// TestSettingsAreAdminOnly is the fourth criterion: both routes are an
// admin's. A moderator by role and a member who holds moderate through a
// group are 403, as are a member and a pre-user; anonymous is 401. None of
// the refused writes changes the setting or writes an audit row.
func TestSettingsAreAdminOnly(t *testing.T) {
	w := newAdminWorld(t)
	for _, c := range []struct {
		viewer string
		want   int
	}{
		{"moderator", http.StatusForbidden},
		{"scoped", http.StatusForbidden},
		{"member", http.StatusForbidden},
		{"pre-user", http.StatusForbidden},
		{"anonymous", http.StatusUnauthorized},
		{"suspended", http.StatusUnauthorized},
	} {
		if rec := w.call(c.viewer, "GET", adminSettings, ""); rec.Code != c.want {
			t.Errorf("GET as %s: %d, want %d", c.viewer, rec.Code, c.want)
		} else if strings.Contains(rec.Body.String(), "registration.mode") {
			t.Errorf("GET as %s leaked the listing: %s", c.viewer, rec.Body)
		}
		if rec := w.call(c.viewer, "PUT", settingPath("registration.mode"), `{"value":"open"}`); rec.Code != c.want {
			t.Errorf("PUT as %s: %d, want %d", c.viewer, rec.Code, c.want)
		}
	}
	if row := w.settingRow("registration.mode"); row != "none" {
		t.Errorf("after the refused writes registration.mode is %s", row)
	}
	if n := w.auditCount(settings.ActionChanged); n != 0 {
		t.Errorf("the refused writes left %d %s rows", n, settings.ActionChanged)
	}
	if rec := w.call("admin", "PUT", settingPath("registration.mode"), `{"value":"open"}`); rec.Code != http.StatusOK {
		t.Errorf("PUT as admin: %d %s", rec.Code, rec.Body)
	}
}

// TestInvalidSettingRefused is the third criterion: a value the key does not
// take is validation-failed and changes nothing, and the listing's allowed
// text names what it takes. An unknown key is not-found, and a body with no
// value is bad-request.
func TestInvalidSettingRefused(t *testing.T) {
	w := newAdminWorld(t)
	for _, c := range []struct {
		name, key, body, want string
	}{
		{"an out-of-range integer", "flags.auto_hide_threshold", `{"value":0}`, "validation-failed"},
		{"a negative window", "posts.edit_window_seconds", `{"value":-1}`, "validation-failed"},
		{"an unknown choice", "registration.mode", `{"value":"closed"}`, "validation-failed"},
		{"invite, until its phase", "registration.mode", `{"value":"invite"}`, "validation-failed"},
		{"the wrong type", "public_edit_history", `{"value":"yes"}`, "validation-failed"},
		{"a string for an integer", "slow_mode.default_seconds", `{"value":"30"}`, "validation-failed"},
		{"a refill over a week", "agents.write_refill_seconds", `{"value":604801}`, "validation-failed"},
		{"an empty site name", "site_name", `{"value":""}`, "validation-failed"},
		{"null", "site_name", `{"value":null}`, "validation-failed"},
		{"an unknown key", "posts.page_size", `{"value":25}`, "not-found"},
		{"no value", "site_name", `{}`, "bad-request"},
		{"an unknown field", "site_name", `{"value":"X","key":"site_name"}`, "bad-request"},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := w.call("admin", "PUT", settingPath(c.key), c.body)
			if got := problemType(t, rec); got != c.want {
				t.Errorf("%d %s, want %s", rec.Code, rec.Body, c.want)
			}
			if row := w.settingRow(c.key); row != "none" {
				t.Errorf("the refused value was stored: %s", row)
			}
		})
	}
	if n := w.auditCount(settings.ActionChanged); n != 0 {
		t.Errorf("the refusals wrote %d audit rows", n)
	}
	byKey := settingsByKey(decodeAs[dto.SiteSettings](t, w.call("admin", "GET", adminSettings, "")))
	for key, want := range map[string]string{
		"flags.auto_hide_threshold":   "a whole number, 1 or more",
		"registration.mode":           `one of "open", "approval"`,
		"agents.write_refill_seconds": "a whole number from 1 to 604800",
		"site_name":                   "text of 1 to 100 characters",
		"public_edit_history":         "true or false",
	} {
		if got := byKey[key].Allowed; got != want {
			t.Errorf("%s: allowed %q, want %q", key, got, want)
		}
	}
	// The top of the refill's range is accepted.
	if got := decodeAs[dto.SiteSetting](t, w.call("admin", "PUT", settingPath("agents.write_refill_seconds"), `{"value":604800}`)); string(got.Value) != "604800" {
		t.Errorf("604800 answered %s", got.Value)
	}
}

// TestSettingChangeAudited is the seventh criterion's audit half: a change
// writes setting.changed naming the admin, the request's address and the
// old and new values, and a change whose audit insert fails is not made.
func TestSettingChangeAudited(t *testing.T) {
	w := newAdminWorld(t)
	got := decodeAs[dto.SiteSetting](t, w.call("admin", "PUT", settingPath("flags.auto_hide_threshold"), `{"value": 5}`))
	if string(got.Value) != "5" || got.IsDefault {
		t.Errorf("the answer: %+v", got)
	}
	var actor sql.NullInt64
	var typ, id, details, ip string
	var at int64
	if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT actor_id, target_type, target_id, details_json, ip, at
			FROM audit_log WHERE action = ?`, settings.ActionChanged).Scan(&actor, &typ, &id, &details, &ip, &at)
	}); err != nil {
		t.Fatal(err)
	}
	// httptest's requests come from 192.0.2.1, and the admin is user 4.
	if actor.Int64 != 4 || typ != "setting" || id != "flags.auto_hide_threshold" ||
		details != `{"from":3,"to":5}` || ip != "192.0.2.1" || at != w.now.UnixMilli() {
		t.Errorf("audit row: actor %v, target %s/%s, details %s, ip %q, at %d", actor, typ, id, details, ip, at)
	}

	w.exec(`CREATE TRIGGER test_audit_fails BEFORE INSERT ON audit_log
		BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END`)
	if rec := w.call("admin", "PUT", settingPath("flags.auto_hide_threshold"), `{"value":7}`); rec.Code != http.StatusInternalServerError {
		t.Errorf("with a failing audit insert: %d, want 500", rec.Code)
	}
	w.exec(`DROP TRIGGER test_audit_fails`)
	if row := w.settingRow("flags.auto_hide_threshold"); !strings.HasPrefix(row, "5 by 4 ") {
		t.Errorf("after the failed write the setting is %s, want 5 as before", row)
	}
}

// TestSettingLastChanged is the seventh criterion's three rules for who last
// changed a key, one case for each way a key can stand.
func TestSettingLastChanged(t *testing.T) {
	w := newAdminWorld(t)
	w.call("admin", "PUT", settingPath("site_name"), `{"value":"The Supper Club"}`)
	screenAt := w.now.UnixMilli()
	// What `ketju admin setting set` sends (settingSet in cmd/ketju): no
	// actor, "via": "cli", and the time.
	cliAt := w.now.Add(time.Hour)
	if err := settings.SetBy(t.Context(), w.db, "registration.mode", json.RawMessage(`"open"`),
		settings.Change{Details: map[string]string{"via": "cli"}, At: cliAt}); err != nil {
		t.Fatal(err)
	}
	byKey := settingsByKey(decodeAs[dto.SiteSettings](t, w.call("admin", "GET", adminSettings, "")))

	t.Run("set from the screen", func(t *testing.T) {
		s := byKey["site_name"]
		if s.UpdatedBy == nil || s.UpdatedBy.Handle != "admin" || s.UpdatedBy.Role != "admin" {
			t.Errorf("updated_by %+v, want the admin", s.UpdatedBy)
		}
		if s.UpdatedAt == nil || int64(*s.UpdatedAt) != screenAt {
			t.Errorf("updated_at %v, want %d", s.UpdatedAt, screenAt)
		}
		if string(s.Value) != `"The Supper Club"` || s.IsDefault {
			t.Errorf("value %s, is_default %t", s.Value, s.IsDefault)
		}
	})
	t.Run("set from the command line", func(t *testing.T) {
		s := byKey["registration.mode"]
		if s.UpdatedBy != nil {
			t.Errorf("updated_by %+v, want null", s.UpdatedBy)
		}
		if s.UpdatedAt == nil || int64(*s.UpdatedAt) != cliAt.UnixMilli() {
			t.Errorf("updated_at %v, want %d", s.UpdatedAt, cliAt.UnixMilli())
		}
	})
	t.Run("never set", func(t *testing.T) {
		s := byKey["public_edit_history"]
		if s.UpdatedBy != nil || s.UpdatedAt != nil || !s.IsDefault {
			t.Errorf("updated_by %+v, updated_at %v, is_default %t; want null, null, true", s.UpdatedBy, s.UpdatedAt, s.IsDefault)
		}
	})
	// And on the wire: null, not absent and not zero.
	rec := w.call("admin", "GET", adminSettings, "")
	for _, want := range []string{
		`"key":"public_edit_history"`, `"updated_at":null,"updated_by":null`,
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("the listing lacks %s", want)
		}
	}
}

// TestSettingTakesEffectWithoutRestart is the first criterion's other half:
// a PUT changes what the next request in the same server does, with nothing
// restarted and no cache to clear.
func TestSettingTakesEffectWithoutRestart(t *testing.T) {
	t.Run("public_edit_history", func(t *testing.T) {
		w := newWorld(t)
		id, _ := w.seed()
		admin := w.newAdmin()
		if code, _ := w.historyOf("carol", id, 1); code != http.StatusOK {
			t.Fatalf("with the default, carol's history of #1: %d", code)
		}
		ok[dto.SiteSetting](t, w.sendAs(admin, http.MethodPut, settingPath("public_edit_history"), `{"value":false}`))
		if code, _ := w.historyOf("carol", id, 1); code != http.StatusForbidden {
			t.Errorf("after turning it off, carol's history of #1: %d, want 403", code)
		}
		ok[dto.SiteSetting](t, w.sendAs(admin, http.MethodPut, settingPath("public_edit_history"), `{"value":true}`))
		if code, _ := w.historyOf("carol", id, 1); code != http.StatusOK {
			t.Errorf("after turning it back on, carol's history of #1: %d", code)
		}
	})

	t.Run("slow_mode.default_seconds", func(t *testing.T) {
		w := newWorld(t)
		id, _ := w.seed() // a thread with no thread_slow_mode row
		h := w.chain()
		created[dto.CreatedPost](t, replyAs(h, "carol", id, "one"))
		created[dto.CreatedPost](t, replyAs(h, "carol", id, "two"))
		ok[dto.SiteSetting](t, w.sendAs(w.newAdmin(), http.MethodPut, settingPath("slow_mode.default_seconds"), `{"value":30}`))
		w.wantWait(h, "carol", id, "30")
	})

	t.Run("registration.mode", func(t *testing.T) {
		w := newAdminWorld(t)
		canApply := func() bool {
			return decodeAs[dto.ApplicationStatus](t, w.call("pre-user", "GET", "/api/v1/apply/status", "")).CanApply
		}
		if !canApply() {
			t.Fatal("under approval, the applicant cannot apply")
		}
		decodeAs[dto.SiteSetting](t, w.call("admin", "PUT", settingPath("registration.mode"), `{"value":"open"}`))
		if canApply() {
			t.Error("after opening registration, the applicant can still apply")
		}
		decodeAs[dto.SiteSetting](t, w.call("admin", "PUT", settingPath("registration.mode"), `{"value":"approval"}`))
		if !canApply() {
			t.Error("back under approval, the applicant cannot apply")
		}
	})

	t.Run("site_name", func(t *testing.T) {
		w := newMeWorld(t)
		w.exec(`INSERT INTO users (id, handle, display_name, status, role, kind, created_at, updated_at)
			VALUES (6, 'root', 'Site Admin', 'active', 'admin', 'human', 1, 1)`)
		admin := sessiontest.SignIn(t, w.db, 6, w.now)
		title := regexp.MustCompile(`<title>(.*?)</title>`)
		check := func(want string) {
			t.Helper()
			for _, token := range []string{"", admin} {
				if me := decodeMe(t, w.get("/api/v1/me", token).Body.Bytes()); me.SiteName != want {
					t.Errorf("/me (signed in %t) has the site name %q, want %q", token != "", me.SiteName, want)
				}
				if m := title.FindStringSubmatch(w.get("/", token).Body.String()); m == nil || m[1] != want {
					t.Errorf("the shell's title (signed in %t) is %q, want %q", token != "", m, want)
				}
			}
		}
		check("Ketju")
		req := httptest.NewRequest(http.MethodPut, settingPath("site_name"), strings.NewReader(`{"value":"The Supper Club"}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(sessiontest.Cookie(admin))
		rec := httptest.NewRecorder()
		w.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT site_name: %d %s", rec.Code, rec.Body)
		}
		check("The Supper Club")
	})
}
