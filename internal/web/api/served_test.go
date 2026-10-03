package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/agentcred"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/service/applications"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/identity"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/web"
)

// Served goldens (TKT-01M3YME9G0, TKT-01M29BKBMC criterion 2): what every
// route api.Routes returns actually serves, through the real session
// middleware, recorded under testdata/golden/served. A handler that changes
// its status, its caching headers or the shape of its body fails
// TestServedGoldens; a route with no case fails TestEveryRouteHasAServedGolden.
//
// Regenerate with
//
//	go test ./internal/web/api -run TestServedGoldens -update
//
// and read the diff: every change in a golden is a change in what clients
// get.

// servedNow is the clock of the sessions, the users service and the
// applications service: after every seeded record, so every seeded session
// is live.
var servedNow = time.UnixMilli(1_757_600_000_000)

// servedWorld is the world every served case runs in: the thread world,
// seeded as servedSeed describes, served through web.New with the session
// and agent-credential middleware serve uses.
type servedWorld struct {
	*world
	h http.Handler
	// tokens is each viewer's session cookie, by handle; "pre-user" is an
	// applicant's.
	tokens map[string]string
	// thread is the seeded thread (world.seed), other an unopened second
	// thread in the same forum.
	thread, other threadid.ID
}

// newServedWorld builds and seeds a served world. Every case gets its own,
// so no case sees another's writes.
func newServedWorld(t *testing.T) *servedWorld {
	t.Helper()
	w := &servedWorld{world: newWorld(t), tokens: map[string]string{}}
	clock := func() time.Time { return servedNow }
	pm := w.svc.Perm
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessions := &session.Store{DB: w.db, Now: clock}
	deps := Deps{Threads: w.svc,
		Users: &users.Service{DB: w.db, Perm: pm, Now: clock, QueueCount: w.svc.QueueCount,
			VisibleNotifications: w.svc.CountVisibleNotifications},
		Forums: &forums.Service{DB: w.db, Perm: pm}, Forms: &formsvc.Service{DB: w.db},
		Applications: &applications.Service{DB: w.db, Now: clock}, Perm: pm, Sessions: sessions,
		Providers: []identity.ProviderName{{Key: "github", Name: "GitHub"}, {Key: "club", Name: "The Club"}},
		Logger:    log, Now: clock}
	pass := func(h http.Handler) http.Handler { return h }
	w.h = web.New(web.Groups{API: New(deps)}, web.Options{Logger: log, RateLimit: pass, CSRF: pass,
		Session: agentcred.Middleware(&agentcred.Store{DB: w.db, Now: clock}, session.Middleware(sessions, log), log)})
	w.servedSeed()
	return w
}

// servedSeed is what the cases read:
//
//   - the general forum, and a restricted staff forum with a thread in it
//     that carol cannot see;
//   - world.seed's thread: an edit, an agent's reply, a deleted post, a
//     mention of alice, a pin, two tags, carol's read position and forum
//     watch, and bob's mute;
//   - a second thread in general nobody has opened, which carol watches;
//   - carol's reply draft and forum draft;
//   - alice's two notifications, of scribe's reply and carol's mention;
//   - a GitHub identity for carol, and an applicant's identity;
//   - a session each for alice, bob and carol, and a pre-user session for
//     the applicant;
//   - the search index, caught up.
func (w *servedWorld) servedSeed() {
	t := w.t
	w.thread, _ = w.seed()
	w.other, _ = w.thread2()

	staff := w.forumAt("staff", domain.VisibilityRestricted, false)
	general := w.forum
	w.forum = staff
	hidden, _ := w.world.thread(3, "Staff only")
	w.post(hidden, "bob", "Nothing for members here.", 0)
	w.forum = general

	var applicant int64
	w.write(func(q *dbq.Queries) error {
		if _, err := q.CreateIdentity(t.Context(), dbq.CreateIdentityParams{
			UserID: sql.NullInt64{Int64: w.users["carol"], Valid: true}, ProviderKey: "github", Subject: "carol-github",
			Issuer: "https://github.example", Email: sql.NullString{String: "carol@github.example", Valid: true},
			CreatedAt: 1, UpdatedAt: 1}); err != nil {
			return err
		}
		var err error
		applicant, err = q.CreateIdentity(t.Context(), dbq.CreateIdentityParams{ProviderKey: "github",
			Subject: "applicant", Issuer: "https://github.example", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	if err := w.db.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO notifications (user_id, kind, thread_id, ordinal, at, read_at)
			VALUES (?, 'reply_to_you', ?, 2, ?, NULL), (?, 'mention', ?, 4, ?, NULL)`,
			w.users["alice"], int64(w.thread), servedNow.UnixMilli()-2000,
			w.users["alice"], int64(w.thread), servedNow.UnixMilli()-1000)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	for _, h := range []string{"alice", "bob", "carol"} {
		w.tokens[h] = sessiontest.SignIn(t, w.db, w.users[h], servedNow)
	}
	w.tokens["pre-user"] = sessiontest.PreUser(t, w.db, applicant, servedNow)

	w.mustServe("carol", http.MethodPut, "/api/v1/drafts/"+w.thread.String(), `{"source":"Half a reply.","reply_to":1}`)
	w.mustServe("carol", http.MethodPut, "/api/v1/forums/general/draft", `{"title":"A new thread","source":"Half a start."}`)
	w.mustServe("carol", http.MethodPost, "/api/v1/threads/"+w.other.String()+"/watch", `{"level":"watch"}`)
	w.index()
}

// thread2 is a second thread in general, by alice, that nobody has opened.
func (w *servedWorld) thread2() (threadid.ID, int64) {
	w.t.Helper()
	id, _ := w.world.thread(2, "A thread nobody opened")
	return id, w.post(id, "alice", "Still unread by everyone.", 0)
}

// serve sends one request as viewer: a handle with a seeded session,
// "pre-user", or "" for nobody.
func (w *servedWorld) serve(viewer, method, path, body string) *httptest.ResponseRecorder {
	w.t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r.RemoteAddr = "192.0.2.10:4000"
	if viewer != "" {
		tok, ok := w.tokens[viewer]
		if !ok {
			w.t.Fatalf("no seeded session for %q", viewer)
		}
		r.AddCookie(sessiontest.Cookie(tok))
	}
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, r)
	return rec
}

// mustServe is serve for seeding: anything but a 2xx fails the test.
func (w *servedWorld) mustServe(viewer, method, path, body string) {
	w.t.Helper()
	if rec := w.serve(viewer, method, path, body); rec.Code/100 != 2 {
		w.t.Fatalf("seeding %s %s as %s: %d %s", method, path, viewer, rec.Code, rec.Body)
	}
}

// servedCase is one route's canonical request, its success case: who sends
// it (a handle servedSeed signed in, "pre-user", or "" for nobody), and the
// path and body. req runs in the case's own fresh world, so it may also seed
// what only this case needs, through w.mustServe or w.write.
type servedCase struct {
	viewer string
	req    func(w *servedWorld) (path, body string)
}

// at is a case with a fixed path and no body.
func at(viewer, path string) servedCase {
	return servedCase{viewer: viewer, req: func(*servedWorld) (string, string) { return path, "" }}
}

// inThread is a case whose path follows the seeded thread's id.
func inThread(viewer, suffix string) servedCase {
	return servedCase{viewer: viewer, req: func(w *servedWorld) (string, string) {
		return "/api/v1/threads/" + w.thread.String() + suffix, ""
	}}
}

// servedCases is every route's case, by the pattern api.Routes names it
// ("GET /api/v1/me"). A route added to the API adds its case here, in the
// same PR, and its golden with -update.
var servedCases = map[string]servedCase{
	"GET /api/v1/me":                                   at("carol", "/api/v1/me"),
	"GET /api/v1/unread":                               at("carol", "/api/v1/unread"),
	"GET /api/v1/new":                                  at("carol", "/api/v1/new"),
	"GET /api/v1/participated":                         at("carol", "/api/v1/participated"),
	"GET /api/v1/watching":                             at("carol", "/api/v1/watching"),
	"GET /api/v1/forums":                               at("carol", "/api/v1/forums"),
	"GET /api/v1/threads/{id}":                         inThread("carol", ""),
	"GET /api/v1/threads/{id}/posts/{ordinal}":         inThread("carol", "/posts/1"),
	"GET /api/v1/threads/{id}/posts/{ordinal}/replies": inThread("carol", "/posts/1/replies"),
	"GET /api/v1/threads/{id}/posts/{ordinal}/history": inThread("carol", "/posts/1/history"),
	"GET /api/v1/threads/{id}/posts/{ordinal}/source":  inThread("alice", "/posts/1/source"),
	"GET /api/v1/drafts/{thread_id}":                   {viewer: "carol", req: func(w *servedWorld) (string, string) { return "/api/v1/drafts/" + w.thread.String(), "" }},
	"GET /api/v1/forums/{slug}/threads":                at("carol", "/api/v1/forums/general/threads"),
	"GET /api/v1/forums/{slug}/draft":                  at("carol", "/api/v1/forums/general/draft"),
	"GET /api/v1/users":                                at("carol", "/api/v1/users?prefix=a"),
	"GET /api/v1/users/{handle}":                       at("carol", "/api/v1/users/alice"),
	"GET /api/v1/users/{handle}/posts":                 at("carol", "/api/v1/users/alice/posts"),
	"GET /api/v1/search":                               at("carol", "/api/v1/search?q=bill"),
	"GET /api/v1/notifications":                        at("alice", "/api/v1/notifications"),
	"GET /api/v1/settings":                             at("carol", "/api/v1/settings"),
	"GET /api/v1/settings/sessions":                    at("carol", "/api/v1/settings/sessions"),
	"GET /api/v1/settings/identities":                  at("carol", "/api/v1/settings/identities"),
	"GET /api/v1/settings/handle":                      at("carol", "/api/v1/settings/handle"),
	"GET /api/v1/apply/form":                           at("pre-user", "/api/v1/apply/form"),
	"GET /api/v1/apply/status":                         at("pre-user", "/api/v1/apply/status"),
}

// pending is the routes that have no served case yet. It exists only so the
// work could merge in three PRs that each fit a review (docs/12 "The merge
// gate"), and it was frozen in the first of them: it only ever shrinks.
//
// Do not add to it. A route added to the API adds a servedCases entry
// instead, and the last PR of TKT-01M3YME9G0 deletes this list.
var pending = []string{
	// The public writes (the second PR).
	"PUT /api/v1/me/read",
	"PATCH /api/v1/threads/{id}",
	"POST /api/v1/render",
	"POST /api/v1/threads/{id}/posts",
	"PATCH /api/v1/threads/{id}/posts/{ordinal}",
	"DELETE /api/v1/threads/{id}/posts/{ordinal}",
	"POST /api/v1/threads/{id}/posts/{ordinal}/restore",
	"PUT /api/v1/threads/{id}/read",
	"DELETE /api/v1/threads/{id}/read",
	"POST /api/v1/threads/{id}/watch",
	"POST /api/v1/threads/{id}/flags",
	"POST /api/v1/blobs",
	"PUT /api/v1/drafts/{thread_id}",
	"DELETE /api/v1/drafts/{thread_id}",
	"POST /api/v1/forums/{slug}/threads",
	"PUT /api/v1/forums/{slug}/draft",
	"DELETE /api/v1/forums/{slug}/draft",
	"POST /api/v1/forums/{slug}/watch",
	"PUT /api/v1/forums/{slug}/read",
	"POST /api/v1/notifications/read",
	"PATCH /api/v1/settings",
	"DELETE /api/v1/settings/sessions",
	"DELETE /api/v1/settings/sessions/{id}",
	"DELETE /api/v1/settings/identities/{id}",
	"POST /api/v1/settings/deactivate",
	"PUT /api/v1/settings/handle",
	"POST /api/v1/apply",
	"POST /api/v1/apply/withdraw",
	// The admin routes (the third PR).
	"POST /api/v1/admin/users/{handle}/suspend",
	"POST /api/v1/admin/users/{handle}/unsuspend",
	"POST /api/v1/admin/users/{handle}/posts/hide",
	"POST /api/v1/admin/users/{handle}/posts/restore",
	"POST /api/v1/admin/users/{handle}/anonymize",
	"POST /api/v1/admin/users/{handle}/rename",
	"GET /api/v1/admin/audit",
	"GET /api/v1/admin/forums",
	"POST /api/v1/admin/forums",
	"PATCH /api/v1/admin/forums/{id}",
	"POST /api/v1/admin/forums/{id}/move",
	"PUT /api/v1/admin/forums/{id}/visibility",
	"PUT /api/v1/admin/forums/{id}/archived",
	"GET /api/v1/admin/queue",
	"POST /api/v1/admin/queue/posts/{id}/{ordinal}",
	"GET /api/v1/admin/users",
	"GET /api/v1/admin/users/{handle}",
	"PUT /api/v1/admin/users/{handle}/role",
	"GET /api/v1/admin/groups",
	"POST /api/v1/admin/groups",
	"GET /api/v1/admin/groups/{id}",
	"PATCH /api/v1/admin/groups/{id}",
	"DELETE /api/v1/admin/groups/{id}",
	"POST /api/v1/admin/groups/{id}/members",
	"DELETE /api/v1/admin/groups/{id}/members/{handle}",
	"GET /api/v1/admin/permissions",
	"POST /api/v1/admin/permissions",
	"POST /api/v1/admin/permissions/preview",
	"POST /api/v1/admin/users",
	"POST /api/v1/admin/users/{handle}/credential",
	"DELETE /api/v1/admin/users/{handle}/credential",
	"PUT /api/v1/admin/users/{handle}/agent-limits",
	"GET /api/v1/admin/applications",
	"GET /api/v1/admin/applications/{id}",
	"POST /api/v1/admin/applications/{id}/approve",
	"POST /api/v1/admin/applications/{id}/deny",
	"POST /api/v1/admin/applications/{id}/notes",
	"POST /api/v1/admin/applications/{id}/unblock",
	"GET /api/v1/admin/forms/{key}",
	"GET /api/v1/admin/forms/{key}/versions/{version}",
	"POST /api/v1/admin/forms/{key}/versions",
	"POST /api/v1/admin/forms/{key}/preview",
	"GET /api/v1/admin/settings",
	"PUT /api/v1/admin/settings/{key}",
	"PUT /api/v1/admin/threads/{id}/slow-mode",
	"POST /api/v1/admin/threads/{id}/lock",
	"POST /api/v1/admin/threads/{id}/unlock",
	"POST /api/v1/admin/threads/{id}/pin",
	"POST /api/v1/admin/threads/{id}/unpin",
	"POST /api/v1/admin/threads/{id}/move",
	"PUT /api/v1/admin/threads/{id}/tags",
	"POST /api/v1/admin/threads/{id}/archive",
}

// volatileMask is one value a golden cannot pin because it is random by
// design: route is a servedCases key, path a JSON path in its body
// ("sessions[].id": the id of every element of sessions).
type volatileMask struct {
	route, path string
	// why names the source of the randomness.
	why string
}

// volatile is every masked value. A row may name only a value that is
// random by design; a time or a counter is fixed by the world's clock
// instead. TestTheVolatileTableNamesOnlyRandomValues holds every row to
// that: it serves the route in two fresh worlds and wants the value to
// differ.
var volatile = []volatileMask{
	{"GET /api/v1/settings/sessions", "sessions[].id",
		"derived from the session's random token (session.Session.ID)"},
}

// servedGoldenDir holds one golden per route.
var servedGoldenDir = filepath.Join("testdata", "golden", "served")

// servedGoldenName is the golden's file name for a route pattern: the
// method, then the path with each '/', '{' and '}' as '_'.
func servedGoldenName(pattern string) string {
	return strings.NewReplacer(" ", "_", "/", "_", "{", "_", "}", "_").Replace(pattern) + ".json"
}

// servedHeaders are the response headers a golden records when present.
var servedHeaders = []string{"Allow", "Cache-Control", "Content-Type", "ETag", "Location"}

// servedRecord is a golden's content.
type servedRecord struct {
	Request string            `json:"request"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    any               `json:"body"`
}

// runServed serves pattern's case in a fresh world, and returns the record
// with the volatile values masked, and the body unmasked.
func runServed(t *testing.T, pattern string) (servedRecord, any) {
	t.Helper()
	c, ok := servedCases[pattern]
	if !ok {
		t.Fatalf("no served case for %q", pattern)
	}
	method, _, _ := strings.Cut(pattern, " ")
	w := newServedWorld(t)
	path, body := c.req(w)
	rec := w.serve(c.viewer, method, path, body)
	out := servedRecord{Request: method + " " + w.stable(path), Status: rec.Code, Headers: map[string]string{}}
	for _, h := range servedHeaders {
		if v := rec.Header().Get(h); v != "" {
			out.Headers[h] = v
		}
	}
	raw := decodeServed(t, rec.Body.Bytes())
	masked := decodeServed(t, rec.Body.Bytes())
	for _, m := range volatile {
		if m.route == pattern {
			maskPath(masked, strings.Split(m.path, "."), func(any) any { return "<random>" })
		}
	}
	out.Body = masked
	return out, raw
}

// stable is path with the seeded thread ids named, so a golden's request
// line reads the same however the ids are spelled.
func (w *servedWorld) stable(path string) string {
	return strings.NewReplacer(w.thread.String(), "{thread}", w.other.String(), "{other}").Replace(path)
}

// decodeServed decodes a JSON body with its numbers kept as written, or
// nil for an empty one.
func decodeServed(t *testing.T, b []byte) any {
	t.Helper()
	if len(bytes.TrimSpace(b)) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("the body is not JSON: %v\n%s", err, b)
	}
	return v
}

// maskPath applies fn to every value at path under v, and reports how many
// it found. "name[]" steps into every element of the array under name.
func maskPath(v any, path []string, fn func(any) any) int {
	if len(path) == 0 {
		return 0
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return 0
	}
	key, each := strings.CutSuffix(path[0], "[]")
	next, ok := obj[key]
	if !ok {
		return 0
	}
	switch {
	case each:
		arr, ok := next.([]any)
		if !ok {
			return 0
		}
		n := 0
		for i, el := range arr {
			if len(path) == 1 {
				arr[i] = fn(el)
				n++
				continue
			}
			n += maskPath(el, path[1:], fn)
		}
		return n
	case len(path) == 1:
		obj[key] = fn(next)
		return 1
	default:
		return maskPath(next, path[1:], fn)
	}
}

// encodeServed is a record as its golden holds it: indented, with HTML
// characters as written, and a final newline.
func encodeServed(t *testing.T, r servedRecord) []byte {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestServedGoldens serves every case and compares it with its golden, and
// fails on a golden no case writes.
func TestServedGoldens(t *testing.T) {
	keys := slices.Sorted(func(yield func(string) bool) {
		for k := range servedCases {
			if !yield(k) {
				return
			}
		}
	})
	for _, pattern := range keys {
		t.Run(pattern, func(t *testing.T) {
			rec, _ := runServed(t, pattern)
			got := encodeServed(t, rec)
			path := filepath.Join(servedGoldenDir, servedGoldenName(pattern))
			if *update {
				if err := os.MkdirAll(servedGoldenDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run go test ./internal/web/api -run TestServedGoldens -update to write it)", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s serves differently from %s. If the change is meant, rewrite the golden with -update "+
					"and say so in the PR:\n%s", pattern, path, got)
			}
		})
	}

	files, err := os.ReadDir(servedGoldenDir)
	if err != nil {
		t.Fatal(err)
	}
	named := map[string]bool{}
	for k := range servedCases {
		named[servedGoldenName(k)] = true
	}
	for _, f := range files {
		if !named[f.Name()] {
			t.Errorf("%s is a golden no served case writes: delete it", filepath.Join(servedGoldenDir, f.Name()))
		}
	}
}

// servedCoverage is what TestEveryRouteHasAServedGolden finds wrong with
// routes (as "METHOD /path" patterns) against cases and pending.
func servedCoverage(routes []string, cases map[string]servedCase, pending []string) []string {
	var out []string
	served := map[string]bool{}
	for _, r := range routes {
		served[r] = true
		_, hasCase := cases[r]
		isPending := slices.Contains(pending, r)
		switch {
		case !hasCase && !isPending:
			out = append(out, fmt.Sprintf("route %q has no served golden: add a servedCases entry for %q in "+
				"internal/web/api/served_test.go (who sends it, and its path and body; seed what it needs in req or "+
				"servedSeed), then run "+
				"go test ./internal/web/api -run TestServedGoldens -update and commit the golden it writes "+
				"under testdata/golden/served. Do not add it to pending, which only shrinks.", r, r))
		case hasCase && isPending:
			out = append(out, fmt.Sprintf("route %q has a served case and is in pending: delete it from pending", r))
		}
	}
	for k := range cases {
		if !served[k] {
			out = append(out, fmt.Sprintf("servedCases names %q, which api.Routes does not serve: delete the case "+
				"and its golden", k))
		}
	}
	for _, p := range pending {
		if !served[p] {
			out = append(out, fmt.Sprintf("pending names %q, which api.Routes does not serve: delete it from pending", p))
		}
	}
	sort.Strings(out)
	return out
}

// patterns is routes as the "METHOD /path" patterns servedCases is keyed by.
func patterns(routes []web.Route) []string {
	out := make([]string, 0, len(routes))
	for _, r := range routes {
		out = append(out, r.Method+" "+r.Path)
	}
	return out
}

// TestEveryRouteHasAServedGolden walks api.Routes: every route has a case
// or, until the last PR of TKT-01M3YME9G0, a pending entry, and nothing
// names a route that is gone.
func TestEveryRouteHasAServedGolden(t *testing.T) {
	w := newServedWorld(t)
	d := Deps{Threads: w.svc, Users: &users.Service{DB: w.db, Perm: w.svc.Perm}, Forums: &forums.Service{DB: w.db, Perm: w.svc.Perm},
		Forms: &formsvc.Service{DB: w.db}, Applications: &applications.Service{DB: w.db}, Perm: w.svc.Perm}
	routes := patterns(Routes(d))
	if len(routes) < 100 {
		t.Fatalf("api.Routes returned %d routes; the walk is not seeing the API", len(routes))
	}
	for _, p := range servedCoverage(routes, servedCases, pending) {
		t.Error(p)
	}
	seen := map[string]bool{}
	for _, p := range pending {
		if seen[p] {
			t.Errorf("pending names %q twice", p)
		}
		seen[p] = true
	}

	// The walk can fail. A route registered through adminRoute in a test
	// world, as a later PR's route would be, with no case: named, with the
	// instruction. A case and a pending entry for routes that are gone:
	// named as stale. A route in both: named.
	aw := newAdminWorld(t)
	aw.fixtureRoutes()
	extra := []string{}
	for _, a := range aw.h.adminRoutes {
		extra = append(extra, a.pattern)
	}
	all := append(patterns(aw.h.routes()), extra...)
	cases := map[string]servedCase{"GET /api/v1/gone": at("", "/api/v1/gone"), "GET /api/v1/me": at("", "/api/v1/me")}
	got := servedCoverage(all, cases, append(slices.Clone(pending), "GET /api/v1/vanished", "GET /api/v1/me"))
	for _, want := range []*regexp.Regexp{
		regexp.MustCompile(`^route "POST /api/v1/admin/_test/admin/\{id\}" has no served golden: add a servedCases entry`),
		regexp.MustCompile(`^route "POST /api/v1/admin/_test/moderates/\{id\}" has no served golden`),
		regexp.MustCompile(`^route "POST /api/v1/admin/_test/staff/\{id\}" has no served golden`),
		regexp.MustCompile(`^servedCases names "GET /api/v1/gone"`),
		regexp.MustCompile(`^pending names "GET /api/v1/vanished"`),
		regexp.MustCompile(`^route "GET /api/v1/me" has a served case and is in pending`),
	} {
		if !slices.ContainsFunc(got, want.MatchString) {
			t.Errorf("the walk over a test world with fixture routes found no %s; it found:\n%s", want,
				strings.Join(got, "\n"))
		}
	}
}

// TestTheVolatileTableNamesOnlyRandomValues: every mask names a route with
// a case and a path its response has, and the value there differs between
// two fresh worlds, so it is random by design rather than a time or a
// counter the clock should fix.
func TestTheVolatileTableNamesOnlyRandomValues(t *testing.T) {
	for _, m := range volatile {
		t.Run(m.route+" "+m.path, func(t *testing.T) {
			if m.why == "" {
				t.Error("the row gives no reason")
			}
			if _, ok := servedCases[m.route]; !ok {
				t.Fatalf("%q has no served case", m.route)
			}
			var values [2][]string
			for i := range values {
				_, raw := runServed(t, m.route)
				if n := maskPath(raw, strings.Split(m.path, "."), func(v any) any {
					values[i] = append(values[i], fmt.Sprint(v))
					return v
				}); n == 0 {
					t.Fatalf("the response has nothing at %s", m.path)
				}
			}
			if slices.Equal(values[0], values[1]) {
				t.Errorf("%s is %v in two fresh worlds: it is not random, so fix it rather than mask it", m.path, values[0])
			}
		})
	}
}
