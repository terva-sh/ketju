package agentcred

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

type world struct {
	t     *testing.T
	db    *db.DB
	perm  *perm.Evaluator
	store *Store
	now   time.Time
	users map[string]int64
}

// newWorld is a database with an admin, a human member, and one agent per
// status.
func newWorld(t *testing.T) *world {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, db: d, perm: perm.New(d), now: time.UnixMilli(1_757_500_000_000), users: map[string]int64{}}
	w.store = &Store{DB: d, Now: func() time.Time { return w.now }}
	for _, u := range []struct{ handle, role, kind, status string }{
		{"root", domain.RoleAdmin, domain.KindHuman, domain.StatusActive},
		{"alice", domain.RoleMember, domain.KindHuman, domain.StatusActive},
		{"scribe", domain.RoleMember, domain.KindAgent, domain.StatusActive},
		{"indexer", domain.RoleMember, domain.KindAgent, domain.StatusActive},
		{"waiting", domain.RoleMember, domain.KindAgent, domain.StatusPending},
		{"benched", domain.RoleMember, domain.KindAgent, domain.StatusSuspended},
		{"retired", domain.RoleMember, domain.KindAgent, domain.StatusDeactivated},
	} {
		w.write(func(q *dbq.Queries) error {
			id, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: u.handle, DisplayName: u.handle,
				Status: u.status, Role: u.role, Kind: u.kind, CreatedAt: 1, UpdatedAt: 1})
			w.users[u.handle] = id
			return err
		})
	}
	return w
}

func (w *world) write(fn func(q *dbq.Queries) error) {
	w.t.Helper()
	if err := w.db.Write(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error { return fn(q) }); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) by() audit.By { return audit.By{Actor: w.users["root"], IP: "192.0.2.1"} }

// issue issues handle a credential, or returns the refusal.
func (w *world) issue(handle string) (Issued, error) {
	var out Issued
	err := w.db.Write(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		out, err = Issue(w.t.Context(), q, w.now, w.by(), w.users[handle])
		return err
	})
	return out, err
}

func (w *world) mustIssue(handle string) string {
	w.t.Helper()
	out, err := w.issue(handle)
	if err != nil {
		w.t.Fatalf("issuing %s a credential: %v", handle, err)
	}
	return out.Token
}

func (w *world) count(query string, args ...any) int {
	w.t.Helper()
	var n int
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), query, args...).Scan(&n)
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

func (w *world) exec(query string) {
	w.t.Helper()
	if err := w.db.Write(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(w.t.Context(), query)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
}

// live reports whether token authenticates.
func (w *world) live(token string) bool {
	w.t.Helper()
	_, err := w.store.Lookup(w.t.Context(), token)
	if err != nil && !errors.Is(err, ErrNoCredential) {
		w.t.Fatal(err)
	}
	return err == nil
}

func TestTokenFormat(t *testing.T) {
	token, h, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, TokenPrefix) || len(token) != len(TokenPrefix)+43 {
		t.Fatalf("token %q is not ketju_agent_ and 43 base64url characters", token)
	}
	if got, ok := hashToken(token); !ok || got != h {
		t.Fatalf("hashToken(token) = %x, %v; want %x", got, ok, h)
	}
	raw := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, tokenBytes))
	for _, bad := range []string{
		"", raw, // no prefix: a session id is not an agent token
		TokenPrefix, TokenPrefix + raw[:42], TokenPrefix + raw + "A",
		TokenPrefix + raw + "=", TokenPrefix + strings.Replace(raw, "B", "+", 1),
		"KETJU_AGENT_" + raw,
	} {
		if _, ok := hashToken(bad); ok {
			t.Errorf("hashToken(%q) accepted it", bad)
		}
	}
}

// TestIssueRefusedUnlessActive: issuing to an agent whose status is not
// active answers conflict and writes no credential, and after an
// unsuspension the agent has no live credential until one is issued afresh.
func TestIssueRefusedUnlessActive(t *testing.T) {
	w := newWorld(t)
	for _, h := range []string{"waiting", "benched", "retired"} {
		_, err := w.issue(h)
		if !errors.Is(err, ErrAgentNotActive) || problem.FromError(err) != problem.CodeConflict {
			t.Errorf("issuing %s: %v, want ErrAgentNotActive as conflict", h, err)
		}
	}
	if _, err := w.issue("alice"); !errors.Is(err, ErrNotAgent) || problem.FromError(err) != problem.CodeConflict {
		t.Errorf("issuing a human: %v, want ErrNotAgent as conflict", err)
	}
	if n := w.count(`SELECT count(*) FROM agent_credentials`); n != 0 {
		t.Fatalf("%d credentials written by refused issues", n)
	}
	if n := w.count(`SELECT count(*) FROM audit_log WHERE action = ?`, ActionIssued); n != 0 {
		t.Fatalf("%d issue rows written by refused issues", n)
	}

	// Suspended while holding one: it is revoked, and unsuspending does not
	// bring it back.
	token := w.mustIssue("scribe")
	root := w.users["root"]
	if err := w.perm.ChangeStatus(t.Context(), w.users["scribe"], perm.StatusChange{By: audit.By{Actor: root},
		Action: perm.ActionSuspended, Reason: "runaway"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.issue("scribe"); !errors.Is(err, ErrAgentNotActive) {
		t.Fatalf("issuing during the suspension: %v", err)
	}
	if err := w.perm.ChangeStatus(t.Context(), w.users["scribe"], perm.StatusChange{By: audit.By{Actor: root},
		Action: perm.ActionUnsuspended}); err != nil {
		t.Fatal(err)
	}
	if w.live(token) {
		t.Fatal("the credential held before the suspension works after it")
	}
	if n := w.count(`SELECT count(*) FROM agent_credentials WHERE user_id = ? AND revoked_at IS NULL`, w.users["scribe"]); n != 0 {
		t.Fatalf("the unsuspended agent has %d live credentials", n)
	}
	if fresh := w.mustIssue("scribe"); !w.live(fresh) {
		t.Fatal("a credential issued after the unsuspension does not work")
	}
}

// TestIssueReplacesTheLiveCredential: the old token is refused at once, and
// the replacement is audited as a revocation with its cause.
func TestIssueReplacesTheLiveCredential(t *testing.T) {
	w := newWorld(t)
	old := w.mustIssue("scribe")
	if !w.live(old) {
		t.Fatal("a fresh credential does not work")
	}
	fresh := w.mustIssue("scribe")
	if w.live(old) {
		t.Error("the replaced token still works")
	}
	if !w.live(fresh) {
		t.Error("the new token does not work")
	}
	if n := w.count(`SELECT count(*) FROM agent_credentials WHERE user_id = ?`, w.users["scribe"]); n != 2 {
		t.Errorf("%d credential rows, want the revoked one kept beside the live one", n)
	}
	if n := w.count(`SELECT count(*) FROM audit_log WHERE action = ? AND json_extract(details_json, '$.cause') = ?
		AND actor_id = ?`, perm.ActionCredentialRevoked, ActionIssued, w.users["root"]); n != 1 {
		t.Errorf("%d revocation rows caused by the replacement, want 1", n)
	}
	if n := w.count(`SELECT count(*) FROM audit_log WHERE action = ? AND json_extract(details_json, '$.replaced') = 1`,
		ActionIssued); n != 1 {
		t.Errorf("%d issue rows marked replaced, want 1", n)
	}
}

// TestCredentialStoredHashed: the token's text and its random bytes appear
// in no column of any table; its SHA-256 does.
func TestCredentialStoredHashed(t *testing.T) {
	w := newWorld(t)
	token := w.mustIssue("scribe")
	h, _ := hashToken(token)
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, TokenPrefix))
	var tables []string
	if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(t.Context(), `SELECT name FROM sqlite_schema WHERE type = 'table'`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				return err
			}
			tables = append(tables, n)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	foundHash := false
	for _, table := range tables {
		if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			rows, err := tx.QueryContext(t.Context(), `SELECT * FROM "`+table+`"`)
			if err != nil {
				return err
			}
			defer rows.Close()
			cols, _ := rows.Columns()
			for rows.Next() {
				vals := make([]any, len(cols))
				ptrs := make([]any, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					return err
				}
				for i, v := range vals {
					var b []byte
					switch x := v.(type) {
					case []byte:
						b = x
					case string:
						b = []byte(x)
					default:
						continue
					}
					if bytes.Contains(b, []byte(token)) || bytes.Contains(b, []byte(strings.TrimPrefix(token, TokenPrefix))) ||
						bytes.Contains(b, raw) {
						t.Errorf("%s.%s holds the token", table, cols[i])
					}
					foundHash = foundHash || bytes.Equal(b, h[:])
				}
			}
			return rows.Err()
		}); err != nil {
			t.Fatal(err)
		}
	}
	if !foundHash {
		t.Error("the token's hash is in no table, so the scan found nothing to find")
	}
}

// TestSuspendRevokesCredential: suspension and deactivation revoke the
// credential in the transaction that changes the status, and both audit
// rows name the actor. A trigger that aborts the status's audit row leaves
// the credential live, so the two commit together or not at all.
func TestSuspendRevokesCredential(t *testing.T) {
	w := newWorld(t)
	root := w.users["root"]
	for _, c := range []struct {
		handle string
		action string
	}{{"scribe", perm.ActionSuspended}, {"indexer", perm.ActionDeactivated}} {
		token := w.mustIssue(c.handle)
		id := w.users[c.handle]
		change := perm.StatusChange{By: audit.By{Actor: root, IP: "192.0.2.1"}, Action: c.action, Reason: "stop"}
		if c.action == perm.ActionDeactivated {
			// Deactivation is a member's own; the agent's admin acts as it.
			change.By.Actor = id
		}

		w.exec(`CREATE TRIGGER refuse BEFORE INSERT ON audit_log WHEN NEW.action = '` + c.action + `'
			BEGIN SELECT RAISE(ABORT, 'refused'); END`)
		if err := w.perm.ChangeStatus(t.Context(), id, change); err == nil {
			t.Fatalf("%s went through with its audit row refused", c.action)
		}
		if !w.live(token) {
			t.Fatalf("%s failed, yet the credential was revoked", c.action)
		}
		w.exec(`DROP TRIGGER refuse`)

		if err := w.perm.ChangeStatus(t.Context(), id, change); err != nil {
			t.Fatal(err)
		}
		if w.live(token) {
			t.Errorf("%s: the credential still works", c.action)
		}
		if n := w.count(`SELECT count(*) FROM audit_log WHERE target_id = ? AND action = ? AND actor_id = ?
			AND json_extract(details_json, '$.cause') = ?`, id, perm.ActionCredentialRevoked, change.By.Actor, c.action); n != 1 {
			t.Errorf("%s: %d revocation rows naming the actor and the cause, want 1", c.action, n)
		}
		if n := w.count(`SELECT count(*) FROM audit_log WHERE target_id = ? AND action = ? AND actor_id = ?`,
			id, c.action, change.By.Actor); n != 1 {
			t.Errorf("%s: %d status rows naming the actor, want 1", c.action, n)
		}
		if n := w.count(`SELECT count(*) FROM agent_credentials WHERE user_id = ? AND revoked_by = ?`, id, change.By.Actor); n != 1 {
			t.Errorf("%s: the credential does not name who revoked it", c.action)
		}
	}
}

// TestAnonymizeRevokesCredential: anonymizing an agent revokes its
// credential with the rest.
func TestAnonymizeRevokesCredential(t *testing.T) {
	w := newWorld(t)
	token := w.mustIssue("scribe")
	if err := w.perm.Anonymize(t.Context(), w.users["scribe"], audit.By{Actor: w.users["root"]}, nil); err != nil {
		t.Fatal(err)
	}
	if w.live(token) {
		t.Error("an anonymized agent's credential still works")
	}
	if n := w.count(`SELECT count(*) FROM audit_log WHERE action = ? AND json_extract(details_json, '$.cause') = ?`,
		perm.ActionCredentialRevoked, perm.ActionAnonymized); n != 1 {
		t.Errorf("%d revocation rows caused by the anonymization, want 1", n)
	}
}

// TestHumanStatusChangeWritesNoRevocation: a human has no credential, so
// suspending one writes no revocation row.
func TestHumanStatusChangeWritesNoRevocation(t *testing.T) {
	w := newWorld(t)
	if err := w.perm.ChangeStatus(t.Context(), w.users["alice"], perm.StatusChange{By: audit.By{Actor: w.users["root"]},
		Action: perm.ActionSuspended, Reason: "x"}); err != nil {
		t.Fatal(err)
	}
	if n := w.count(`SELECT count(*) FROM audit_log WHERE action = ?`, perm.ActionCredentialRevoked); n != 0 {
		t.Errorf("%d revocation rows for a human", n)
	}
}

// TestLookupRefusesAllButALiveActiveAgent: a credential whose account is a
// human, or an agent that is not active, resolves to nothing, whatever
// revoked it or failed to.
func TestLookupRefusesAllButALiveActiveAgent(t *testing.T) {
	w := newWorld(t)
	token := w.mustIssue("scribe")
	c, err := w.store.Lookup(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	if c.UserID != w.users["scribe"] || c.Role != domain.RoleMember || c.Rate != (Rate{Burst: 20, Refill: 30 * time.Second}) {
		t.Fatalf("lookup %+v", c)
	}
	if v := c.Viewer(); !v.Member() || v.UserID != c.UserID || v.PreUser || v.SessionKey == "" ||
		strings.Contains(v.SessionKey, strings.TrimPrefix(token, TokenPrefix)) {
		t.Fatalf("viewer %+v", v)
	}

	for _, status := range []string{domain.StatusPending, domain.StatusSuspended, domain.StatusDeactivated} {
		w.exec(`UPDATE users SET status = '` + status + `' WHERE handle = 'scribe'`)
		if w.live(token) {
			t.Errorf("a %s agent's credential works", status)
		}
	}
	w.exec(`UPDATE users SET status = 'active' WHERE handle = 'scribe'`)
	if !w.live(token) {
		t.Fatal("the credential does not work once active again")
	}
	w.exec(`UPDATE users SET kind = 'human' WHERE handle = 'scribe'`)
	if w.live(token) {
		t.Error("a credential on a human account works")
	}
}

// TestLimitsOverrideTheDefaults: an agent's own burst and refill win over
// the site's, and a changed site default applies on the next lookup.
func TestLimitsOverrideTheDefaults(t *testing.T) {
	w := newWorld(t)
	token := w.mustIssue("scribe")
	w.exec(`INSERT INTO settings (key, value_json, updated_at) VALUES ('agents.write_burst', '5', 1)`)
	c, err := w.store.Lookup(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	if c.Rate != (Rate{Burst: 5, Refill: 30 * time.Second}) {
		t.Errorf("with a site burst of 5: %+v", c.Rate)
	}
	w.exec(`INSERT INTO agents (user_id, bucket_refill_seconds, created_at) VALUES (` +
		itoa(w.users["scribe"]) + `, 2, 1)`)
	if c, err = w.store.Lookup(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	if c.Rate != (Rate{Burst: 5, Refill: 2 * time.Second}) {
		t.Errorf("with the agent's refill of 2 s: %+v", c.Rate)
	}
}

// TestRateIsAlwaysALimit: whatever the stored numbers, the bucket has a
// positive burst and a positive refill, so the limiter never reads it as
// unconfigured (Terva review 1477), and the schema refuses a refill that
// would overflow.
func TestRateIsAlwaysALimit(t *testing.T) {
	for _, c := range []struct{ burst, refill int }{
		{0, 0}, {-5, -5}, {20, 30}, {20, settings.AgentRefillMaxSeconds}, {20, settings.AgentRefillMaxSeconds + 1},
		{1 << 40, 1 << 40}, {int(^uint(0) >> 1), int(^uint(0) >> 1)},
	} {
		r := rate(c.burst, c.refill)
		if r.Burst < 1 || r.Refill <= 0 || r.Refill > time.Duration(settings.AgentRefillMaxSeconds)*time.Second {
			t.Errorf("rate(%d, %d) = %+v", c.burst, c.refill, r)
		}
	}
	w := newWorld(t)
	if err := w.db.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO agents (user_id, bucket_refill_seconds, created_at) VALUES (?, ?, 1)`,
			w.users["scribe"], settings.AgentRefillMaxSeconds+1)
		return err
	}); err == nil {
		t.Error("the schema took a refill past the bound")
	}
}

// TestTouchIsRateLimited: last_used_at moves on first use and then at most
// once per TouchInterval.
func TestTouchIsRateLimited(t *testing.T) {
	w := newWorld(t)
	token := w.mustIssue("scribe")
	touch := func() bool {
		c, err := w.store.Lookup(t.Context(), token)
		if err != nil {
			t.Fatal(err)
		}
		ok, err := w.store.Touch(t.Context(), c)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !touch() {
		t.Fatal("the first use did not touch")
	}
	w.now = w.now.Add(TouchInterval - time.Second)
	if touch() {
		t.Error("touched again inside the interval")
	}
	w.now = w.now.Add(2 * time.Second)
	if !touch() {
		t.Error("did not touch once the interval passed")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
