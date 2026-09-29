package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/agentcred"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// The admin routes that provision agents (TKT-01M29P9RSW), as the admin
// world's viewers.

// authenticates reports whether token is a live credential.
func (w *adminWorld) authenticates(token string) bool {
	w.t.Helper()
	_, err := (&agentcred.Store{DB: w.db, Now: func() time.Time { return w.now }}).Lookup(w.t.Context(), token)
	if err != nil && !errors.Is(err, agentcred.ErrNoCredential) {
		w.t.Fatal(err)
	}
	return err == nil
}

func (w *adminWorld) createAgent(handle string, issue bool) dto.CreatedAgent {
	w.t.Helper()
	body := `{"kind":"agent","handle":"` + handle + `","display_name":"The ` + handle + `","issue_credential":false}`
	if issue {
		body = strings.Replace(body, `"issue_credential":false`, `"issue_credential":true`, 1)
	}
	rec := w.call("admin", "POST", "/api/v1/admin/users", body)
	if rec.Code != http.StatusCreated {
		w.t.Fatalf("creating %s: %d %s", handle, rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/api/v1/admin/users/"+handle {
		w.t.Errorf("Location %q", loc)
	}
	return decodeAs[dto.CreatedAgent](w.t, rec)
}

// TestCreateAgentInOneFlow: an admin creates an agent and its credential in
// one request. The account is an active member agent with no identity, so
// nothing can sign it in (TestAgentHasNoIdentity), and the token
// authenticates.
func TestCreateAgentInOneFlow(t *testing.T) {
	w := newAdminWorld(t)
	res := w.createAgent("scribe-bot", true)
	u := res.User.User
	if u.User.Handle != "scribe-bot" || u.User.DisplayName != "The scribe-bot" || u.User.Kind != "agent" ||
		u.User.Role != "member" || u.Status != "active" {
		t.Fatalf("the account %+v", u)
	}
	if res.Credential == nil || !strings.HasPrefix(res.Credential.Token, agentcred.TokenPrefix) {
		t.Fatalf("the credential %+v", res.Credential)
	}
	a := res.User.Agent
	if a == nil || a.Credential == nil || a.Credential.LastUsedAt != nil || a.Burst != nil || a.RefillSeconds != nil ||
		a.DefaultBurst != 20 || a.DefaultRefillSeconds != 30 {
		t.Fatalf("the agent %+v", a)
	}
	if !w.authenticates(res.Credential.Token) {
		t.Error("the token does not authenticate")
	}
	if n := w.countRows(`SELECT count(*) FROM identities WHERE user_id = ?`, u.ID); n != 0 {
		t.Errorf("%d identities for the agent", n)
	}
	if n := w.countRows(`SELECT count(*) FROM agents WHERE user_id = ? AND created_by = 4`, u.ID); n != 1 {
		t.Error("no agents row naming the admin")
	}
	for _, action := range []string{"user.created", agentcred.ActionIssued} {
		if n := w.countRows(`SELECT count(*) FROM audit_log WHERE action = ? AND actor_id = 4 AND target_id = ?`,
			action, u.ID); n != 1 {
			t.Errorf("%d %s rows naming the admin", n, action)
		}
	}
	detail := w.call("admin", "GET", "/api/v1/admin/users/scribe-bot", "").Body.String()
	if !strings.Contains(detail, `"credential":{`) {
		t.Error("the detail does not show the live credential")
	}
	if strings.Contains(detail, res.Credential.Token) {
		t.Error("the detail shows the token again")
	}
	if d := decodeAs[dto.AdminUserDetail](t, w.call("moderator", "GET", "/api/v1/admin/users/scribe-bot", "")); d.Agent != nil {
		t.Errorf("a moderator sees the agent's credential: %+v", d.Agent)
	}
	if d := decodeAs[dto.AdminUserDetail](t, w.call("admin", "GET", "/api/v1/admin/users/member", "")); d.Agent != nil {
		t.Errorf("a human has agent details: %+v", d.Agent)
	}

	// Without a credential, the agent has none until one is issued.
	bare := w.createAgent("indexer", false)
	if bare.Credential != nil || bare.User.Agent == nil || bare.User.Agent.Credential != nil {
		t.Fatalf("an agent created without a credential: %+v", bare)
	}
	rec := w.call("admin", "POST", "/api/v1/admin/users/indexer/credential", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("issuing: %d %s", rec.Code, rec.Body)
	}
	if tok := decodeAs[dto.IssuedCredential](t, rec); !w.authenticates(tok.Token) {
		t.Error("the issued token does not authenticate")
	}
}

// TestCreateAgentRefusals: a human cannot be created here, and the handle
// follows registration's rules.
func TestCreateAgentRefusals(t *testing.T) {
	w := newAdminWorld(t)
	w.exec(`INSERT INTO handle_releases (handle, released_at, reserved_until) VALUES ('gone', 1, ?)`,
		w.now.Add(time.Hour).UnixMilli())
	for _, c := range []struct{ body, want string }{
		{`{"kind":"human","handle":"somebody","display_name":"","issue_credential":false}`, "validation-failed"},
		{`{"handle":"somebody","display_name":"","issue_credential":false}`, "validation-failed"},
		{`{"kind":"agent","handle":"member","display_name":"","issue_credential":false}`, "conflict"},
		{`{"kind":"agent","handle":"MEMBER","display_name":"","issue_credential":false}`, "conflict"},
		{`{"kind":"agent","handle":"gone","display_name":"","issue_credential":false}`, "conflict"},
		{`{"kind":"agent","handle":"admin","display_name":"","issue_credential":false}`, "validation-failed"},
		{`{"kind":"agent","handle":"deleted-user-9","display_name":"","issue_credential":false}`, "validation-failed"},
		{`{"kind":"agent","handle":"no","display_name":"","issue_credential":false}`, "validation-failed"},
		{`{"kind":"agent","handle":"ok-bot","display_name":"a\u0007b","issue_credential":false}`, "validation-failed"},
		{`{"kind":"agent","handle":"ok-bot","display_name":"x","issue_credential":false,"role":"admin"}`, "bad-request"},
		{`{"kind":"agent","handle":"ok-bot","display_name":"x","machine_authored":true}`, "bad-request"},
	} {
		rec := w.call("admin", "POST", "/api/v1/admin/users", c.body)
		if rec.Code/100 == 2 || problemType(t, rec) != c.want {
			t.Errorf("%s: %d %s, want %s", c.body, rec.Code, rec.Body, c.want)
		}
	}
	if n := w.countRows(`SELECT count(*) FROM users WHERE kind = 'agent'`); n != 0 {
		t.Errorf("%d agents created by refused requests", n)
	}
	// A display name left empty is the handle.
	rec := w.call("admin", "POST", "/api/v1/admin/users", `{"kind":"agent","handle":"plain-bot","display_name":"  ","issue_credential":false}`)
	if res := decodeAs[dto.CreatedAgent](t, rec); res.User.User.User.DisplayName != "plain-bot" {
		t.Errorf("an empty display name became %q", res.User.User.User.DisplayName)
	}
}

// TestAgentCreationAuditedInOneTransaction: the users row, the agents row,
// the credential and the audit rows are written together or not at all.
func TestAgentCreationAuditedInOneTransaction(t *testing.T) {
	for _, refused := range []string{"user.created", agentcred.ActionIssued} {
		t.Run(refused, func(t *testing.T) {
			w := newAdminWorld(t)
			w.exec(`CREATE TRIGGER refuse BEFORE INSERT ON audit_log WHEN NEW.action = '` + refused + `'
				BEGIN SELECT RAISE(ABORT, 'refused'); END`)
			rec := w.call("admin", "POST", "/api/v1/admin/users",
				`{"kind":"agent","handle":"scribe-bot","display_name":"","issue_credential":true}`)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
			for _, q := range []string{`SELECT count(*) FROM users WHERE handle = 'scribe-bot'`,
				`SELECT count(*) FROM agents`, `SELECT count(*) FROM agent_credentials`,
				`SELECT count(*) FROM audit_log WHERE action IN ('user.created', 'agent.credential_issued')`} {
				if n := w.countRows(q); n != 0 {
					t.Errorf("%s: %d", q, n)
				}
			}
		})
	}
}

// TestRevokeStopsOnlyThatAgent: revoking one agent's credential leaves the
// other's working and the revoked agent's account as it was.
func TestRevokeStopsOnlyThatAgent(t *testing.T) {
	w := newAdminWorld(t)
	a := w.createAgent("alpha-bot", true)
	b := w.createAgent("beta-bot", true)
	account := func() dbq.User {
		var u dbq.User
		if err := w.db.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
			var err error
			u, err = q.GetUser(t.Context(), a.User.User.ID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return u
	}
	before := account()
	rec := w.call("admin", "DELETE", "/api/v1/admin/users/alpha-bot/credential", "")
	if rec.Code != http.StatusNoContent || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("revoking: %d %s", rec.Code, rec.Body)
	}
	if w.authenticates(a.Credential.Token) {
		t.Error("the revoked token works")
	}
	if !w.authenticates(b.Credential.Token) {
		t.Error("the other agent's token stopped working")
	}
	if after := account(); after != before {
		t.Errorf("revoking changed the account: %+v, was %+v", after, before)
	}
	if n := w.countRows(`SELECT count(*) FROM audit_log WHERE action = ? AND actor_id = 4 AND target_id = ?
		AND json_extract(details_json, '$.cause') = 'admin'`, perm.ActionCredentialRevoked, a.User.User.ID); n != 1 {
		t.Errorf("%d revocation rows naming the admin", n)
	}
	if rec := w.call("admin", "DELETE", "/api/v1/admin/users/alpha-bot/credential", ""); rec.Code != http.StatusNotFound {
		t.Errorf("revoking again: %d", rec.Code)
	}
	if rec := w.call("admin", "DELETE", "/api/v1/admin/users/member/credential", ""); problemType(t, rec) != "conflict" {
		t.Errorf("revoking a human's credential: %d %s", rec.Code, rec.Body)
	}
}

// TestIssueThroughTheAPIRefusedUnlessActive: an admin cannot issue a
// credential to a suspended agent or to a human, and nothing is written.
func TestIssueThroughTheAPIRefusedUnlessActive(t *testing.T) {
	w := newAdminWorld(t)
	w.createAgent("scribe-bot", false)
	if rec := w.call("moderator", "POST", "/api/v1/admin/users/scribe-bot/suspend", `{"reason":"loop","until":null}`); rec.Code != http.StatusNoContent {
		t.Fatalf("suspending: %d %s", rec.Code, rec.Body)
	}
	for h, want := range map[string]string{"scribe-bot": "conflict", "member": "conflict", "nobody-here": "not-found"} {
		if rec := w.call("admin", "POST", "/api/v1/admin/users/"+h+"/credential", ""); problemType(t, rec) != want {
			t.Errorf("issuing to %s: %d %s, want %s", h, rec.Code, rec.Body, want)
		}
	}
	if n := w.countRows(`SELECT count(*) FROM agent_credentials`); n != 0 {
		t.Errorf("%d credentials written", n)
	}
}

// TestAgentLimits: an admin sets an agent's own burst and refill, or puts
// either back to the site default.
func TestAgentLimits(t *testing.T) {
	w := newAdminWorld(t)
	w.createAgent("scribe-bot", false)
	path := "/api/v1/admin/users/scribe-bot/agent-limits"
	d := decodeAs[dto.AdminUserDetail](t, w.call("admin", "PUT", path, `{"burst":40,"refill_seconds":null}`))
	if d.Agent == nil || d.Agent.Burst == nil || *d.Agent.Burst != 40 || d.Agent.RefillSeconds != nil {
		t.Fatalf("after setting a burst: %+v", d.Agent)
	}
	if n := w.countRows(`SELECT count(*) FROM audit_log WHERE action = 'agent.limits_changed' AND actor_id = 4
		AND json_extract(details_json, '$.burst') = 40 AND json_extract(details_json, '$.refill_seconds') IS NULL`); n != 1 {
		t.Errorf("%d limit rows", n)
	}
	d = decodeAs[dto.AdminUserDetail](t, w.call("admin", "PUT", path, `{"burst":null,"refill_seconds":5}`))
	if d.Agent.Burst != nil || d.Agent.RefillSeconds == nil || *d.Agent.RefillSeconds != 5 {
		t.Fatalf("after setting a refill: %+v", d.Agent)
	}
	for _, c := range []struct{ body, want string }{
		{`{"burst":0,"refill_seconds":null}`, "validation-failed"},
		{`{"burst":null,"refill_seconds":-1}`, "validation-failed"},
		// Past a week the refill would overflow into no limit (review 1477).
		{`{"burst":null,"refill_seconds":604801}`, "validation-failed"},
		{`{"burst":null,"refill_seconds":9223372036854775807}`, "validation-failed"},
		{`{"burst":null}`, "bad-request"},
		{`{"burst":1.5,"refill_seconds":null}`, "bad-request"},
		{`{"burst":"2","refill_seconds":null}`, "bad-request"},
	} {
		if rec := w.call("admin", "PUT", path, c.body); problemType(t, rec) != c.want {
			t.Errorf("%s: %d %s, want %s", c.body, rec.Code, rec.Body, c.want)
		}
	}
	if rec := w.call("admin", "PUT", "/api/v1/admin/users/member/agent-limits", `{"burst":1,"refill_seconds":1}`); problemType(t, rec) != "conflict" {
		t.Errorf("limits on a human: %d %s", rec.Code, rec.Body)
	}
}
