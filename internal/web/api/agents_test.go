package api

import (
	"bytes"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/agentcred"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/applications"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
	"github.com/terva-sh/ketju/internal/web"
)

// Agents on the API (TKT-01M29P9RSW), through the real chain: the bearer
// middleware in front of the real session middleware, the real CSRF
// middleware, and the per-agent bucket on a clock the test moves.

// agentWorld is the thread world served as serve serves it.
type agentWorld struct {
	*world
	now     time.Time
	perm    *perm.Evaluator
	h       http.Handler
	logs    *bytes.Buffer
	cookies map[string]string
}

func newAgentWorld(t *testing.T) *agentWorld {
	t.Helper()
	w := &agentWorld{world: newWorld(t), now: time.UnixMilli(1_757_500_000_000), cookies: map[string]string{},
		logs: &bytes.Buffer{}}
	w.perm = w.svc.Perm
	clock := func() time.Time { return w.now }
	api := New(Deps{Threads: w.svc, Users: &users.Service{DB: w.db, Perm: w.perm, Now: clock},
		Forums: &forums.Service{DB: w.db, Perm: w.perm}, Forms: &formsvc.Service{DB: w.db},
		Applications: &applications.Service{DB: w.db}, Perm: w.perm, Now: clock})
	sessions := &session.Store{DB: w.db, Now: clock}
	creds := &agentcred.Store{DB: w.db, Now: clock}
	log := slog.New(slog.NewJSONHandler(w.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	pass := func(h http.Handler) http.Handler { return h }
	w.h = web.New(web.Groups{API: api}, web.Options{Logger: log, RateLimit: pass,
		Session: agentcred.Middleware(creds, session.Middleware(sessions, log), log)})
	for _, u := range []string{"alice", "carol", "bob"} {
		w.cookies[u] = sessiontest.SignIn(t, w.db, w.users[u], w.now)
	}
	return w
}

// issue gives the agent handle a credential, as an admin would.
func (w *agentWorld) issue(handle string) string {
	w.t.Helper()
	var token string
	if err := w.db.Write(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		out, err := agentcred.Issue(w.t.Context(), q, w.now, audit.By{}, w.users[handle])
		token = out.Token
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return token
}

// bearer sends a request with Authorization: Bearer token, and JSON when
// body is not empty.
func (w *agentWorld) bearer(token, method, path, body string) *httptest.ResponseRecorder {
	w.t.Helper()
	return w.do(method, path, body, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) })
}

// cookie sends a request as a member signed in with a session cookie.
func (w *agentWorld) cookie(who, method, path, body string) *httptest.ResponseRecorder {
	w.t.Helper()
	return w.do(method, path, body, func(r *http.Request) { r.AddCookie(sessiontest.Cookie(w.cookies[who])) })
}

func (w *agentWorld) do(method, path, body string, auth func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	auth(r)
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, r)
	return rec
}

func (w *agentWorld) count(query string, args ...any) int {
	w.t.Helper()
	var n int
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), query, args...).Scan(&n)
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

func (w *agentWorld) exec(query string, args ...any) {
	w.t.Helper()
	if err := w.db.Write(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(w.t.Context(), query, args...)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
}

// flags are a post's frame flags, read from the log, and its index
// entry's flags.
type flags struct {
	frame threadlog.FrameFlags
	entry threadlog.EntryFlags
}

func (w *agentWorld) frameAndEntry(id threadid.ID, ordinal uint32) flags {
	w.t.Helper()
	var out flags
	f, err := os.Open(id.LogPath(w.root))
	if err != nil {
		w.t.Fatal(err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		w.t.Fatal(err)
	}
	found := false
	if err := threadlog.ScanFrames(f, fi.Size(), func(r threadlog.ScanRecord) bool {
		if r.Err == nil && r.Header.Type == threadlog.TypePost && r.Header.Ordinal == ordinal {
			out.frame, found = r.Header.Flags, true
		}
		return true
	}); err != nil {
		w.t.Fatal(err)
	}
	if !found {
		w.t.Fatalf("no post frame for ordinal %d", ordinal)
	}
	if err := w.cache.With(id, func(th *threadcache.Thread) error {
		e, err := th.Index.Entry(ordinal)
		out.entry = e.Flags
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// posts is how many posts the thread's index holds.
func (w *agentWorld) posts(id threadid.ID) int {
	w.t.Helper()
	return w.logState(id).posts
}

func reply(src string) string { return fmt.Sprintf(`{"source":%q}`, src) }

// TestAgentCredentialMintsNoSession: a bearer request that writes a post
// leaves the sessions table as it was and sets no cookie, even when it
// also carries a stale cookie the session middleware would have cleared.
func TestAgentCredentialMintsNoSession(t *testing.T) {
	w := newAgentWorld(t)
	id, _ := w.seed()
	token := w.issue("scribe")
	before := w.count(`SELECT count(*) FROM sessions`)
	rec := w.do(http.MethodPost, postsPath(id), reply("From the agent."), func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+token)
		r.AddCookie(&http.Cookie{Name: session.CookieName, Value: "stale"})
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("a bearer reply: %d %s", rec.Code, rec.Body)
	}
	if got := w.count(`SELECT count(*) FROM sessions`); got != before {
		t.Errorf("sessions went from %d to %d", before, got)
	}
	if c := rec.Header().Values("Set-Cookie"); len(c) != 0 {
		t.Errorf("a bearer request set cookies %q", c)
	}
	if strings.Contains(w.logs.String(), strings.TrimPrefix(token, agentcred.TokenPrefix)) {
		t.Error("the token reached the log")
	}
}

// TestAgentCredentialRequiresActiveAgent: a pending, suspended or
// deactivated agent, a revoked credential, a credential row pointed at a
// human, and a malformed header are all 401, and nothing is written.
func TestAgentCredentialRequiresActiveAgent(t *testing.T) {
	w := newAgentWorld(t)
	id, _ := w.seed()
	token := w.issue("scribe")
	posts := w.posts(id)
	refused := func(what string, rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Code != http.StatusUnauthorized || problemType(t, rec) != "unauthenticated" {
			t.Errorf("%s: %d %s, want 401 unauthenticated", what, rec.Code, rec.Body)
		}
		if !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "Bearer") {
			t.Errorf("%s: WWW-Authenticate %q", what, rec.Header().Get("WWW-Authenticate"))
		}
		if len(rec.Header().Values("Set-Cookie")) != 0 {
			t.Errorf("%s set a cookie", what)
		}
	}
	for _, status := range []string{domain.StatusPending, domain.StatusSuspended, domain.StatusDeactivated} {
		w.exec(`UPDATE users SET status = ? WHERE id = ?`, status, w.users["scribe"])
		refused("a "+status+" agent", w.bearer(token, http.MethodPost, postsPath(id), reply("no")))
		refused("a "+status+" agent's read", w.bearer(token, http.MethodGet, "/api/v1/threads/"+id.String(), ""))
	}
	w.exec(`UPDATE users SET status = 'active' WHERE id = ?`, w.users["scribe"])
	w.exec(`UPDATE agent_credentials SET user_id = ? WHERE user_id = ?`, w.users["alice"], w.users["scribe"])
	refused("a credential on a human", w.bearer(token, http.MethodPost, postsPath(id), reply("no")))
	w.exec(`UPDATE agent_credentials SET user_id = ?, revoked_at = 1 WHERE user_id = ?`, w.users["scribe"], w.users["alice"])
	refused("a revoked credential", w.bearer(token, http.MethodPost, postsPath(id), reply("no")))

	fresh := w.issue("scribe")
	for _, h := range []string{"", "Bearer", "Bearer ", "Basic " + fresh, "bearer" + fresh, "Bearer " + fresh + " x",
		"Bearer " + strings.TrimPrefix(fresh, agentcred.TokenPrefix), "Bearer " + w.cookies["alice"]} {
		refused(fmt.Sprintf("Authorization %q", h), w.do(http.MethodPost, postsPath(id), reply("no"), func(r *http.Request) {
			r.Header.Set("Authorization", h)
			// A valid session beside a bad header does not rescue it: the
			// header decides.
			r.AddCookie(sessiontest.Cookie(w.cookies["alice"]))
		}))
	}
	refused("two Authorization headers", w.do(http.MethodPost, postsPath(id), reply("no"), func(r *http.Request) {
		r.Header.Add("Authorization", "Bearer "+fresh)
		r.Header.Add("Authorization", "Bearer "+fresh)
	}))
	if got := w.posts(id); got != posts {
		t.Fatalf("refused requests appended: %d posts, was %d", got, posts)
	}
	// The scheme is case-insensitive.
	if rec := w.do(http.MethodPost, postsPath(id), reply("yes"), func(r *http.Request) {
		r.Header.Set("Authorization", "bearer "+fresh)
	}); rec.Code != http.StatusCreated {
		t.Errorf("a live credential: %d %s", rec.Code, rec.Body)
	}
}

// TestBearerWriteIsMachineAuthored: a bearer reply's frame carries the
// machine-authored flag, its index entry the mirroring bit, and the thread
// response shows machine_authored and author.kind agent.
func TestBearerWriteIsMachineAuthored(t *testing.T) {
	w := newAgentWorld(t)
	id, _ := w.seed()
	token := w.issue("scribe")
	res := created[dto.CreatedPost](t, w.bearer(token, http.MethodPost, postsPath(id), reply("Machine words.")))
	if !res.Post.MachineAuthored || res.Post.Author.Kind != domain.KindAgent {
		t.Errorf("the created post: machine_authored %v, author %+v", res.Post.MachineAuthored, res.Post.Author)
	}
	st := w.frameAndEntry(id, res.Ordinal)
	if st.frame&threadlog.FrameFlagMachineAuthored == 0 {
		t.Errorf("frame flags %b lack the machine-authored bit", st.frame)
	}
	if st.entry&threadlog.EntryMachineAuthored == 0 {
		t.Errorf("index flags %b lack the machine-authored bit", st.entry)
	}
	page := decode[dto.ThreadPage](t, w.bearer(token, http.MethodGet, "/api/v1/threads/"+id.String()+"?ordinal="+
		strconv.Itoa(int(res.Ordinal)), ""))
	found := false
	for _, p := range page.Posts {
		if uint32(p.Ordinal) == res.Ordinal {
			found = true
			if !p.MachineAuthored || p.Author.Kind != domain.KindAgent {
				t.Errorf("the thread page shows %+v", p)
			}
		}
	}
	if !found {
		t.Fatalf("the reply is not on its page")
	}
}

// TestClientCannotSetMachineAuthored: a human's reply that claims to be
// machine-authored is bad-request, and nothing is appended.
func TestClientCannotSetMachineAuthored(t *testing.T) {
	w := newAgentWorld(t)
	id, _ := w.seed()
	posts := w.posts(id)
	for _, body := range []string{`{"source":"human","machine_authored":true}`, `{"source":"human","flags":1}`} {
		rec := w.cookie("carol", http.MethodPost, postsPath(id), body)
		if rec.Code != http.StatusBadRequest || problemType(t, rec) != "bad-request" {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
	if got := w.posts(id); got != posts {
		t.Fatalf("%d posts, was %d", got, posts)
	}
	res := created[dto.CreatedPost](t, w.cookie("carol", http.MethodPost, postsPath(id), reply("human")))
	if res.Post.MachineAuthored {
		t.Error("a human's reply is machine-authored")
	}
}

// TestAgentBucket: twenty writes pass and the twenty-first is 429 with
// Retry-After, appending nothing; a refill admits one more; a per-agent
// override and a changed site default apply on the next request; the read
// position and drafts are not charged; a member with a cookie never is.
func TestAgentBucket(t *testing.T) {
	w := newAgentWorld(t)
	id, _ := w.seed()
	token := w.issue("scribe")
	path := postsPath(id)
	for i := range 20 {
		if rec := w.bearer(token, http.MethodPost, path, reply(fmt.Sprintf("post %d", i))); rec.Code != http.StatusCreated {
			t.Fatalf("write %d: %d %s", i+1, rec.Code, rec.Body)
		}
	}
	posts := w.posts(id)
	rec := w.bearer(token, http.MethodPost, path, reply("one too many"))
	if rec.Code != http.StatusTooManyRequests || problemType(t, rec) != "rate-limited" {
		t.Fatalf("write 21: %d %s", rec.Code, rec.Body)
	}
	if ra, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || ra < 30 || ra > 31 {
		t.Errorf("Retry-After %q, want about 30", rec.Header().Get("Retry-After"))
	}
	if got := w.posts(id); got != posts {
		t.Fatalf("a refused write appended: %d posts, was %d", got, posts)
	}

	// Not charged: reading, the read position, drafts; and a member.
	for _, r := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/threads/" + id.String(), ""},
		{http.MethodPut, "/api/v1/threads/" + id.String() + "/read", `{"ordinal":1}`},
		{http.MethodDelete, "/api/v1/threads/" + id.String() + "/read", ""},
		{http.MethodPut, "/api/v1/drafts/" + id.String(), `{"source":"thinking","reply_to":null}`},
		{http.MethodDelete, "/api/v1/drafts/" + id.String(), ""},
	} {
		if rec := w.bearer(token, r.method, r.path, r.body); rec.Code/100 != 2 {
			t.Errorf("%s %s with an empty bucket: %d %s", r.method, r.path, rec.Code, rec.Body)
		}
	}
	for i := range 25 {
		if rec := w.cookie("carol", http.MethodPost, path, reply(fmt.Sprintf("human %d", i))); rec.Code != http.StatusCreated {
			t.Fatalf("a member's write %d: %d %s", i+1, rec.Code, rec.Body)
		}
	}

	// One refill admits one write.
	w.now = w.now.Add(30 * time.Second)
	if rec := w.bearer(token, http.MethodPost, path, reply("refilled")); rec.Code != http.StatusCreated {
		t.Fatalf("after a refill: %d %s", rec.Code, rec.Body)
	}
	if rec := w.bearer(token, http.MethodPost, path, reply("empty again")); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("after spending the refill: %d", rec.Code)
	}

	// The agent's own refill of one second applies on the next request.
	w.exec(`INSERT INTO agents (user_id, bucket_refill_seconds, created_at) VALUES (?, 1, 1)`, w.users["scribe"])
	w.now = w.now.Add(time.Second)
	if rec := w.bearer(token, http.MethodPost, path, reply("overridden")); rec.Code != http.StatusCreated {
		t.Fatalf("with a 1 s override: %d %s", rec.Code, rec.Body)
	}

	// A site burst of 1 caps the bucket at once, however long it rested.
	w.exec(`UPDATE agents SET bucket_refill_seconds = NULL WHERE user_id = ?`, w.users["scribe"])
	w.exec(`INSERT INTO settings (key, value_json, updated_at) VALUES ('agents.write_burst', '1', 1)`)
	w.now = w.now.Add(time.Hour)
	if rec := w.bearer(token, http.MethodPost, path, reply("one")); rec.Code != http.StatusCreated {
		t.Fatalf("with a site burst of 1: %d %s", rec.Code, rec.Body)
	}
	if rec := w.bearer(token, http.MethodPost, path, reply("two")); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("a second write with a site burst of 1: %d", rec.Code)
	}

	// The bucket is the agent's: a new credential does not refill it.
	fresh := w.issue("scribe")
	if rec := w.bearer(fresh, http.MethodPost, path, reply("rotated")); rec.Code != http.StatusTooManyRequests {
		t.Errorf("a rotated credential refilled the bucket: %d", rec.Code)
	}
}

// TestAgentReadPositionOnlyExplicit: an agent reading a thread leaves its
// read position alone; PUT /threads/{id}/read sets it.
func TestAgentReadPositionOnlyExplicit(t *testing.T) {
	w := newAgentWorld(t)
	id, _ := w.seed()
	token := w.issue("scribe")
	positions := func() int {
		return w.count(`SELECT count(*) FROM read_positions WHERE user_id = ?`, w.users["scribe"])
	}
	for _, p := range []string{"/api/v1/threads/" + id.String(), postPathN(id, 1), postPathN(id, 1) + "/replies"} {
		if rec := w.bearer(token, http.MethodGet, p, ""); rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", p, rec.Code, rec.Body)
		}
	}
	if n := positions(); n != 0 {
		t.Fatalf("reading set %d read positions", n)
	}
	if rec := w.bearer(token, http.MethodPut, "/api/v1/threads/"+id.String()+"/read", `{"ordinal":2}`); rec.Code/100 != 2 {
		t.Fatalf("PUT read: %d %s", rec.Code, rec.Body)
	}
	if n := w.count(`SELECT count(*) FROM read_positions WHERE user_id = ? AND last_read_ordinal = 2`, w.users["scribe"]); n != 1 {
		t.Errorf("PUT read left %d positions at 2", n)
	}
}

// TestAgentViewerSharesMemberClass: an agent member's thread page is the
// member class's cached page, so no viewer class is added and the response
// cache's key needs nothing new.
func TestAgentViewerSharesMemberClass(t *testing.T) {
	w := newAgentWorld(t)
	id, _ := w.seed()
	token := w.issue("scribe")
	path := "/api/v1/threads/" + id.String()
	if rec := w.cookie("carol", http.MethodGet, path, ""); rec.Code != http.StatusOK {
		t.Fatalf("a member's read: %d", rec.Code)
	}
	before := w.svc.Responses.Stats()
	if rec := w.bearer(token, http.MethodGet, path, ""); rec.Code != http.StatusOK {
		t.Fatalf("an agent's read: %d", rec.Code)
	}
	after := w.svc.Responses.Stats()
	if after.Hits != before.Hits+1 || after.Inserts != before.Inserts {
		t.Errorf("the agent's page was not the member's cached page: before %+v, after %+v", before, after)
	}
}

// TestAgentRolesUnchanged: an agent promoted to moderator through the
// audited role change reads as a moderator does: the text of a post a
// moderator deleted, which a member is not shown. Promoted to admin, it
// passes the admin gate.
func TestAgentRolesUnchanged(t *testing.T) {
	w := newAgentWorld(t)
	id, _ := w.seed()
	token := w.issue("scribe")
	const deleted = "a moderator still needs to read"
	path := "/api/v1/threads/" + id.String()
	if rec := w.bearer(token, http.MethodGet, path, ""); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), deleted) {
		t.Fatalf("a member agent's read: %d, deleted text shown %v", rec.Code, strings.Contains(rec.Body.String(), deleted))
	}
	if err := w.perm.ChangeRole(t.Context(), audit.By{Actor: w.users["bob"]}, w.users["scribe"], domain.RoleModerator); err != nil {
		t.Fatal(err)
	}
	if n := w.count(`SELECT count(*) FROM audit_log WHERE action = 'user.role_changed' AND target_id = ?`,
		strconv.FormatInt(w.users["scribe"], 10)); n != 1 {
		t.Errorf("%d role change rows", n)
	}
	if rec := w.bearer(token, http.MethodGet, path, ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), deleted) {
		t.Fatalf("a moderator agent's read: %d, deleted text shown %v", rec.Code, strings.Contains(rec.Body.String(), deleted))
	}

	// An admin agent passes the admin gate, as any admin does.
	if rec := w.bearer(token, http.MethodGet, "/api/v1/admin/groups", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("a moderator agent read the groups: %d", rec.Code)
	}
	if err := w.perm.ChangeRole(t.Context(), audit.By{Actor: w.users["bob"]}, w.users["scribe"], domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if n := w.count(`SELECT count(*) FROM audit_log WHERE action = 'user.promoted' AND target_id = ?`,
		strconv.FormatInt(w.users["scribe"], 10)); n != 1 {
		t.Errorf("%d promotion rows", n)
	}
	if rec := w.bearer(token, http.MethodGet, "/api/v1/admin/groups", ""); rec.Code != http.StatusOK {
		t.Fatalf("an admin agent's read of the groups: %d %s", rec.Code, rec.Body)
	}
}
