package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"mime/multipart"
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

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/agentcred"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/applications"
	"github.com/terva-sh/ketju/internal/service/blobs"
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
	// names are values a case's path holds that a golden's request line
	// shows by name instead, because they are random ("{session}").
	names map[string]string
}

// newServedWorld builds and seeds a served world. Every case gets its own,
// so no case sees another's writes.
func newServedWorld(t *testing.T) *servedWorld {
	t.Helper()
	w := &servedWorld{world: newWorld(t), tokens: map[string]string{}, names: map[string]string{}}
	clock := func() time.Time { return servedNow }
	// One evaluator on the fixed clock, so what perm stamps (a suspension,
	// an audit row, a group member) has a time a golden can pin.
	pm := perm.NewAt(w.db, clock)
	w.svc.Perm = pm
	// A thread created through the API takes its id from the world's
	// clock, so its golden can pin it.
	w.svc.IDs = threadid.NewGeneratorAt(func() time.Time { return time.UnixMilli(w.clock) })
	uploads := &blobs.Service{DB: w.db, Root: t.TempDir(), Perm: pm, Threads: w.svc, Max: uploadCap,
		Now: func() time.Time { return time.UnixMilli(w.clock) }}
	w.svc.Attachments = uploads
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessions := &session.Store{DB: w.db, Now: clock}
	deps := Deps{Threads: w.svc,
		Users: &users.Service{DB: w.db, Perm: pm, Now: clock, QueueCount: w.svc.QueueCount,
			VisibleNotifications: w.svc.CountVisibleNotifications},
		Forums: &forums.Service{DB: w.db, Perm: pm}, Forms: &formsvc.Service{DB: w.db, Now: clock},
		Applications: &applications.Service{DB: w.db, Now: clock}, Perm: pm, Sessions: sessions, Blobs: uploads,
		Providers: []identity.ProviderName{{Key: "github", Name: "GitHub"}, {Key: "club", Name: "The Club"}},
		Logger:    log, Now: clock}
	pass := func(h http.Handler) http.Handler { return h }
	w.h = web.New(web.Groups{API: New(deps)}, web.Options{Logger: log, RateLimit: pass, CSRF: pass,
		Limits:  web.Limits{Upload: uploadCap},
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
//   - dana, an admin;
//   - a session each for alice, bob (a moderator), carol and dana, and a
//     pre-user session for the applicant;
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

	w.write(func(q *dbq.Queries) error {
		id, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "dana", DisplayName: "Dana Admin",
			Status: "active", Role: domain.RoleAdmin, Kind: "human", CreatedAt: 1, UpdatedAt: 1})
		w.users["dana"] = id
		return err
	})
	for _, h := range []string{"alice", "bob", "carol", "dana"} {
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
// "pre-user", or "" for nobody. A body is JSON.
func (w *servedWorld) serve(viewer, method, path, body string) *httptest.ResponseRecorder {
	w.t.Helper()
	ctype := ""
	if body != "" {
		ctype = "application/json"
	}
	return w.send(viewer, method, path, ctype, body)
}

// send is serve with the body's content type given.
func (w *servedWorld) send(viewer, method, path, ctype, body string) *httptest.ResponseRecorder {
	w.t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", ctype)
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
	// ctype is the body's content type when it is not JSON.
	ctype string
}

// at is a case with a fixed path and no body.
func at(viewer, path string) servedCase {
	return servedCase{viewer: viewer, req: func(*servedWorld) (string, string) { return path, "" }}
}

// sending is a case with a fixed path and a JSON body.
func sending(viewer, path, body string) servedCase {
	return servedCase{viewer: viewer, req: func(*servedWorld) (string, string) { return path, body }}
}

// inThreadSending is a case whose path follows the seeded thread's id, with
// a JSON body.
func inThreadSending(viewer, suffix, body string) servedCase {
	return servedCase{viewer: viewer, req: func(w *servedWorld) (string, string) {
		return "/api/v1/threads/" + w.thread.String() + suffix, body
	}}
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

	// The public writes: each runs in its own fresh world, so the golden is
	// the answer to this one write.
	"PUT /api/v1/me/read":        at("carol", "/api/v1/me/read"),
	"PATCH /api/v1/threads/{id}": inThreadSending("alice", "", `{"title":"Why we left Slack, in short"}`),
	"POST /api/v1/render": {viewer: "carol", req: func(w *servedWorld) (string, string) {
		return "/api/v1/render", `{"source":"Agreed with @alice, **twice**.","thread_id":"` + w.thread.String() + `"}`
	}},
	"POST /api/v1/threads/{id}/posts":                   inThreadSending("carol", "/posts", `{"source":"A new reply.","reply_to":1}`),
	"PATCH /api/v1/threads/{id}/posts/{ordinal}":        inThreadSending("carol", "/posts/4", `{"source":"Agreed with @alice on all of it."}`),
	"DELETE /api/v1/threads/{id}/posts/{ordinal}":       inThread("carol", "/posts/4"),
	"POST /api/v1/threads/{id}/posts/{ordinal}/restore": inThread("bob", "/posts/3/restore"),
	"PUT /api/v1/threads/{id}/read":                     inThreadSending("carol", "/read", `{"ordinal":4}`),
	"DELETE /api/v1/threads/{id}/read":                  inThread("carol", "/read"),
	"POST /api/v1/threads/{id}/watch":                   inThreadSending("carol", "/watch", `{"level":"watch"}`),
	"POST /api/v1/threads/{id}/flags":                   inThreadSending("carol", "/flags", `{"ordinal":1,"reason":"spam"}`),
	"POST /api/v1/blobs":                                {viewer: "carol", req: servedUpload, ctype: "multipart/form-data; boundary=" + servedBoundary},
	"PUT /api/v1/drafts/{thread_id}": {viewer: "carol", req: func(w *servedWorld) (string, string) {
		return "/api/v1/drafts/" + w.thread.String(), `{"source":"A whole reply now.","reply_to":1}`
	}},
	"DELETE /api/v1/drafts/{thread_id}":       {viewer: "carol", req: func(w *servedWorld) (string, string) { return "/api/v1/drafts/" + w.thread.String(), "" }},
	"POST /api/v1/forums/{slug}/threads":      sending("carol", "/api/v1/forums/general/threads", `{"title":"A thread from the API","source":"Its first post."}`),
	"PUT /api/v1/forums/{slug}/draft":         sending("carol", "/api/v1/forums/general/draft", `{"title":"A new thread","source":"A whole start now."}`),
	"DELETE /api/v1/forums/{slug}/draft":      at("carol", "/api/v1/forums/general/draft"),
	"POST /api/v1/forums/{slug}/watch":        sending("carol", "/api/v1/forums/general/watch", `{"level":"mute"}`),
	"PUT /api/v1/forums/{slug}/read":          at("carol", "/api/v1/forums/general/read"),
	"POST /api/v1/notifications/read":         sending("alice", "/api/v1/notifications/read", `{"ids":[1]}`),
	"PATCH /api/v1/settings":                  sending("carol", "/api/v1/settings", `{"page_size":40,"theme":"dark"}`),
	"DELETE /api/v1/settings/sessions":        at("carol", "/api/v1/settings/sessions"),
	"DELETE /api/v1/settings/sessions/{id}":   {viewer: "carol", req: servedOtherSession},
	"DELETE /api/v1/settings/identities/{id}": {viewer: "carol", req: servedSecondIdentity},
	"POST /api/v1/settings/deactivate":        at("carol", "/api/v1/settings/deactivate"),
	"PUT /api/v1/settings/handle":             sending("carol", "/api/v1/settings/handle", `{"handle":"caroline"}`),
	"POST /api/v1/apply":                      sending("pre-user", "/api/v1/apply", validAnswers),
	"POST /api/v1/apply/withdraw":             {viewer: "pre-user", req: servedWithdraw},
	// The admin routes, each as the least viewer its gate admits: dana for
	// the admin gate, and bob, a moderator by role, for the staff and
	// moderates gates.
	"POST /api/v1/admin/users/{handle}/suspend":         sending("bob", "/api/v1/admin/users/carol/suspend", `{"reason":"Cooling off.","until":1757700000000}`),
	"POST /api/v1/admin/users/{handle}/unsuspend":       {viewer: "bob", req: then(servedSuspend, "/api/v1/admin/users/carol/unsuspend", `{"restore_posts":true}`)},
	"POST /api/v1/admin/users/{handle}/posts/hide":      {viewer: "bob", req: then(servedSuspend, "/api/v1/admin/users/carol/posts/hide", "")},
	"POST /api/v1/admin/users/{handle}/posts/restore":   {viewer: "bob", req: then(servedSuspend, "/api/v1/admin/users/carol/posts/restore", "")},
	"POST /api/v1/admin/users/{handle}/anonymize":       at("dana", "/api/v1/admin/users/carol/anonymize"),
	"POST /api/v1/admin/users/{handle}/rename":          sending("dana", "/api/v1/admin/users/carol/rename", `{"handle":"carolyn"}`),
	"GET /api/v1/admin/audit":                           {viewer: "dana", req: then(servedSuspend, "/api/v1/admin/audit", "")},
	"GET /api/v1/admin/forums":                          at("dana", "/api/v1/admin/forums"),
	"POST /api/v1/admin/forums":                         sending("dana", "/api/v1/admin/forums", `{"slug":"lounge","name":"Lounge","description":"Off topic.","parent_id":null,"visibility":"members"}`),
	"PATCH /api/v1/admin/forums/{id}":                   {viewer: "dana", req: inForum("general", "", `{"name":"General talk","description":"Anything."}`)},
	"POST /api/v1/admin/forums/{id}/move":               {viewer: "dana", req: inForum("staff", "/move", `{"parent_id":null,"before":0,"after":0}`)},
	"PUT /api/v1/admin/forums/{id}/visibility":          {viewer: "dana", req: inForum("general", "/visibility", `{"visibility":"members"}`)},
	"PUT /api/v1/admin/forums/{id}/archived":            {viewer: "dana", req: inForum("staff", "/archived", `{"archived":true}`)},
	"GET /api/v1/admin/queue":                           {viewer: "bob", req: then(servedFlag, "/api/v1/admin/queue", "")},
	"POST /api/v1/admin/queue/posts/{id}/{ordinal}":     {viewer: "bob", req: servedResolveFlag},
	"GET /api/v1/admin/users":                           at("bob", "/api/v1/admin/users?prefix=c"),
	"GET /api/v1/admin/users/{handle}":                  at("bob", "/api/v1/admin/users/carol"),
	"PUT /api/v1/admin/users/{handle}/role":             sending("dana", "/api/v1/admin/users/carol/role", `{"role":"moderator"}`),
	"GET /api/v1/admin/groups":                          at("dana", "/api/v1/admin/groups"),
	"POST /api/v1/admin/groups":                         sending("dana", "/api/v1/admin/groups", servedGroupBody),
	"GET /api/v1/admin/groups/{id}":                     {viewer: "dana", req: inGroup("", "")},
	"PATCH /api/v1/admin/groups/{id}":                   {viewer: "dana", req: inGroup("", `{"name":"Copy editors","description":"They fix typos."}`)},
	"DELETE /api/v1/admin/groups/{id}":                  {viewer: "dana", req: inGroup("", "")},
	"POST /api/v1/admin/groups/{id}/members":            {viewer: "dana", req: inGroup("/members", `{"handle":"carol"}`)},
	"DELETE /api/v1/admin/groups/{id}/members/{handle}": {viewer: "dana", req: servedRemoveMember},
	"GET /api/v1/admin/permissions":                     at("dana", "/api/v1/admin/permissions"),
	"POST /api/v1/admin/permissions":                    {viewer: "dana", req: servedGrant("/api/v1/admin/permissions", "")},
	"POST /api/v1/admin/permissions/preview":            {viewer: "dana", req: servedGrant("/api/v1/admin/permissions/preview", `"handle":"carol",`)},
	"POST /api/v1/admin/users":                          sending("dana", "/api/v1/admin/users", servedAgentBody),
	"POST /api/v1/admin/users/{handle}/credential":      {viewer: "dana", req: then(servedAgent, "/api/v1/admin/users/helper/credential", "")},
	"DELETE /api/v1/admin/users/{handle}/credential":    {viewer: "dana", req: then(servedCredential, "/api/v1/admin/users/helper/credential", "")},
	"PUT /api/v1/admin/users/{handle}/agent-limits":     {viewer: "dana", req: then(servedAgent, "/api/v1/admin/users/helper/agent-limits", `{"burst":5,"refill_seconds":60}`)},
	"GET /api/v1/admin/applications":                    {viewer: "bob", req: then(servedApplication, "/api/v1/admin/applications", "")},
	"GET /api/v1/admin/applications/{id}":               {viewer: "bob", req: then(servedApplication, "/api/v1/admin/applications/1", "")},
	"POST /api/v1/admin/applications/{id}/approve":      {viewer: "bob", req: then(servedApplication, "/api/v1/admin/applications/1/approve", `{"handle":"newcomer","note":"Welcome."}`)},
	"POST /api/v1/admin/applications/{id}/deny":         {viewer: "bob", req: then(servedApplication, "/api/v1/admin/applications/1/deny", `{"note":"Not this time.","block":false}`)},
	"POST /api/v1/admin/applications/{id}/notes":        {viewer: "bob", req: then(servedApplication, "/api/v1/admin/applications/1/notes", `{"body":"Knows alice."}`)},
	"POST /api/v1/admin/applications/{id}/unblock":      {viewer: "bob", req: then(servedBlocked, "/api/v1/admin/applications/1/unblock", "")},
	"GET /api/v1/admin/forms/{key}":                     at("dana", "/api/v1/admin/forms/registration"),
	"GET /api/v1/admin/forms/{key}/versions/{version}":  at("dana", "/api/v1/admin/forms/registration/versions/1"),
	"POST /api/v1/admin/forms/{key}/versions":           sending("dana", "/api/v1/admin/forms/registration/versions", saveBody(1, draftSchema)),
	"POST /api/v1/admin/forms/{key}/preview":            sending("dana", "/api/v1/admin/forms/registration/preview", previewBody(draftSchema)),
	"GET /api/v1/admin/settings":                        at("dana", "/api/v1/admin/settings"),
	"PUT /api/v1/admin/settings/{key}":                  sending("dana", "/api/v1/admin/settings/registration.mode", `{"value":"open"}`),
	"PUT /api/v1/admin/threads/{id}/slow-mode":          servedModerate("bob", "slow-mode", `{"seconds":30}`),
	"POST /api/v1/admin/threads/{id}/lock":              servedModerate("bob", "lock", `{"reason":"Cooling off."}`),
	"POST /api/v1/admin/threads/{id}/unlock":            {viewer: "bob", req: then(servedLock, "", "")},
	"POST /api/v1/admin/threads/{id}/pin":               {viewer: "bob", req: servedPinOther},
	"POST /api/v1/admin/threads/{id}/unpin":             servedModerate("bob", "unpin", `{"scope":"forum"}`),
	"POST /api/v1/admin/threads/{id}/move":              {viewer: "bob", req: servedMove},
	"PUT /api/v1/admin/threads/{id}/tags":               servedModerate("bob", "tags", `{"tags":["meta","help"]}`),
	"POST /api/v1/admin/threads/{id}/archive":           servedModerate("dana", "archive", `{"reason":"Settled."}`),
}

// then is a case path and body with setup run in the case's world first.
// A path of "" is the seeded thread's unlock route.
func then(setup func(w *servedWorld), path, body string) func(w *servedWorld) (string, string) {
	return func(w *servedWorld) (string, string) {
		setup(w)
		if path == "" {
			path = servedModPath(w, "unlock")
		}
		return path, body
	}
}

// servedSuspend has bob suspend carol, hiding her posts.
func servedSuspend(w *servedWorld) {
	w.mustServe("bob", http.MethodPost, "/api/v1/admin/users/carol/suspend", `{"reason":"Cooling off.","hide_posts":true}`)
}

// servedFlag has carol flag the seeded thread's first post.
func servedFlag(w *servedWorld) {
	w.mustServe("carol", http.MethodPost, "/api/v1/threads/"+w.thread.String()+"/flags", `{"ordinal":1,"reason":"spam"}`)
}

// servedResolveFlag dismisses carol's flag from the queue.
func servedResolveFlag(w *servedWorld) (string, string) {
	servedFlag(w)
	return "/api/v1/admin/queue/posts/" + w.thread.String() + "/1", `{"action":"dismiss","note":"Not spam."}`
}

// servedAgentBody creates the agent helper, with no credential.
const servedAgentBody = `{"kind":"agent","handle":"helper","display_name":"Helper","issue_credential":false}`

// servedAgent has dana create the agent helper.
func servedAgent(w *servedWorld) {
	w.mustServe("dana", http.MethodPost, "/api/v1/admin/users", servedAgentBody)
}

// servedCredential creates helper and issues its credential.
func servedCredential(w *servedWorld) {
	servedAgent(w)
	w.mustServe("dana", http.MethodPost, "/api/v1/admin/users/helper/credential", "")
}

// servedApplication has the applicant apply: application 1.
func servedApplication(w *servedWorld) {
	w.mustServe("pre-user", http.MethodPost, "/api/v1/apply", validAnswers)
}

// servedBlocked is servedApplication, denied with a block.
func servedBlocked(w *servedWorld) {
	servedApplication(w)
	w.mustServe("bob", http.MethodPost, "/api/v1/admin/applications/1/deny", `{"note":"Spam.","block":true}`)
}

// servedLock has bob lock the seeded thread.
func servedLock(w *servedWorld) {
	w.mustServe("bob", http.MethodPost, servedModPath(w, "lock"), "")
}

// servedMove makes a public forum, lounge, and moves the seeded thread
// there: bob moderates both by his role.
func servedMove(w *servedWorld) (string, string) {
	w.forumAt("lounge", domain.VisibilityPublic, false)
	return servedModPath(w, "move"), `{"forum":"lounge"}`
}

// servedPinOther pins the second thread: the seeded one is pinned already.
func servedPinOther(w *servedWorld) (string, string) {
	return "/api/v1/admin/threads/" + w.other.String() + "/pin", `{"scope":"forum","until":null}`
}

// servedGroupBody creates the group the group cases work on.
const servedGroupBody = `{"slug":"editors","name":"Editors","description":"They edit."}`

// inGroup is a case on the group editors, which it has dana create first.
// The id is a counter after the system groups'.
func inGroup(suffix, body string) func(w *servedWorld) (string, string) {
	return func(w *servedWorld) (string, string) {
		w.mustServe("dana", http.MethodPost, "/api/v1/admin/groups", servedGroupBody)
		return fmt.Sprintf("/api/v1/admin/groups/%d%s", servedGroup(w, "editors"), suffix), body
	}
}

// servedRemoveMember adds carol to editors, then removes her.
func servedRemoveMember(w *servedWorld) (string, string) {
	path, _ := inGroup("/members", "")(w)
	w.mustServe("dana", http.MethodPost, path, `{"handle":"carol"}`)
	return path + "/carol", ""
}

// inForum is a case on the forum slug, by its id.
func inForum(slug, suffix, body string) func(w *servedWorld) (string, string) {
	return func(w *servedWorld) (string, string) {
		var id int64
		if err := w.db.Read(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
			f, err := q.GetForumBySlug(w.t.Context(), slug)
			id = f.ID
			return err
		}); err != nil {
			w.t.Fatal(err)
		}
		return fmt.Sprintf("/api/v1/admin/forums/%d%s", id, suffix), body
	}
}

// servedGrant is a grid change giving the members group upload in general,
// as the body of path, with extra keys before changes.
func servedGrant(path, extra string) func(w *servedWorld) (string, string) {
	return func(w *servedWorld) (string, string) {
		return path, fmt.Sprintf(`{%s"changes":[{"forum_id":%d,"group_id":%d,"permission":"upload","grant":true}]}`,
			extra, w.forum, servedGroup(w, domain.GroupMembers))
	}
}

// servedModPath is the seeded thread's moderation route for action.
func servedModPath(w *servedWorld, action string) string {
	return "/api/v1/admin/threads/" + w.thread.String() + "/" + action
}

// servedModerate is a moderation case on the seeded thread.
func servedModerate(viewer, action, body string) servedCase {
	return servedCase{viewer: viewer, req: func(w *servedWorld) (string, string) { return servedModPath(w, action), body }}
}

// servedBoundary is the upload case's multipart boundary, fixed so its
// content type is.
const servedBoundary = "served-golden-boundary"

// servedUpload lets members upload everywhere, as an admin's grid would,
// and uploads a text file to general.
func servedUpload(w *servedWorld) (string, string) {
	members := servedGroup(w, domain.GroupMembers)
	if err := w.svc.Perm.ApplyGrants(w.t.Context(), audit.By{}, []perm.GrantChange{
		{GroupID: members, Permission: domain.PermUpload, Grant: true}}); err != nil {
		w.t.Fatal(err)
	}
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	if err := mw.SetBoundary(servedBoundary); err != nil {
		w.t.Fatal(err)
	}
	fw, err := mw.CreateFormFile("file", "notes.txt")
	if err != nil {
		w.t.Fatal(err)
	}
	fw.Write([]byte("Notes for the thread.\n"))
	mw.Close()
	return "/api/v1/blobs?forum=general", b.String()
}

// servedGroup is a system group's id.
func servedGroup(w *servedWorld, slug string) int64 {
	w.t.Helper()
	var id int64
	if err := w.db.Read(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		g, err := q.GetGroupBySlug(w.t.Context(), slug)
		id = g.ID
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return id
}

// servedOtherSession signs carol in a second time and ends that session
// from the first. Its id is random, so the request line names it.
func servedOtherSession(w *servedWorld) (string, string) {
	tok := sessiontest.SignIn(w.t, w.db, w.users["carol"], servedNow)
	s, err := (&session.Store{DB: w.db, Now: func() time.Time { return servedNow }}).Lookup(w.t.Context(), tok)
	if err != nil {
		w.t.Fatal(err)
	}
	w.names[s.ID()] = "{session}"
	return "/api/v1/settings/sessions/" + s.ID(), ""
}

// servedSecondIdentity links a second sign-in, on the club provider, to
// carol and removes it: removing her only one is refused.
func servedSecondIdentity(w *servedWorld) (string, string) {
	var id int64
	w.write(func(q *dbq.Queries) error {
		var err error
		id, err = q.CreateIdentity(w.t.Context(), dbq.CreateIdentityParams{
			UserID: sql.NullInt64{Int64: w.users["carol"], Valid: true}, ProviderKey: "club", Subject: "carol-club",
			Issuer: "https://club.example", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	return fmt.Sprintf("/api/v1/settings/identities/%d", id), ""
}

// servedWithdraw has the applicant apply, then withdraws the application.
func servedWithdraw(w *servedWorld) (string, string) {
	w.mustServe("pre-user", http.MethodPost, "/api/v1/apply", validAnswers)
	return "/api/v1/apply/withdraw", "{}"
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
	{"POST /api/v1/admin/users/{handle}/credential", "token",
		"the agent's credential, a secret from crypto/rand (agentcred.Issue)"},
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
	ctype := c.ctype
	if ctype == "" && body != "" {
		ctype = "application/json"
	}
	rec := w.send(c.viewer, method, path, ctype, body)
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
	pairs := []string{w.thread.String(), "{thread}", w.other.String(), "{other}"}
	for v, name := range w.names {
		pairs = append(pairs, v, name)
	}
	return strings.NewReplacer(pairs...).Replace(path)
}

// decodeServed is parseServed, failing t on an error.
func decodeServed(t *testing.T, b []byte) any {
	t.Helper()
	v, err := parseServed(b)
	if err != nil {
		t.Fatalf("%v:\n%s", err, b)
	}
	return v
}

// parseServed decodes a JSON body with its numbers kept as written, or nil
// for an empty one. The body must be one JSON value and nothing after it
// but whitespace: a client reads the whole body, so trailing bytes are part
// of what is served (review 2043).
func parseServed(b []byte) (any, error) {
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("the body is not JSON: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("the body has more after its JSON value (%v)", err)
	}
	return v, nil
}

// TestParseServedRefusesTrailingContent: a second value or garbage after
// the body's JSON fails, and trailing whitespace does not.
func TestParseServedRefusesTrailingContent(t *testing.T) {
	for body, ok := range map[string]bool{
		`{"a":1}`:         true,
		"{\"a\":1}\n":     true,
		"":                true,
		`{"a":1}{"b":2}`:  false,
		`{"a":1} garbage`: false,
		`{"a":1}` + "\n]": false,
	} {
		if _, err := parseServed([]byte(body)); (err == nil) != ok {
			t.Errorf("parseServed(%q) = %v, want ok %v", body, err, ok)
		}
	}
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
// routes (as "METHOD /path" patterns) against cases.
func servedCoverage(routes []string, cases map[string]servedCase) []string {
	var out []string
	served := map[string]bool{}
	for _, r := range routes {
		served[r] = true
		if _, ok := cases[r]; !ok {
			out = append(out, fmt.Sprintf("route %q has no served golden: add a servedCases entry for %q in "+
				"internal/web/api/served_test.go (who sends it, and its path and body; seed what it needs in req or "+
				"servedSeed), then run go test ./internal/web/api -run TestServedGoldens -update and commit the "+
				"golden it writes under testdata/golden/served.", r, r))
		}
	}
	for k := range cases {
		if !served[k] {
			out = append(out, fmt.Sprintf("servedCases names %q, which api.Routes does not serve: delete the case "+
				"and its golden", k))
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

// TestEveryRouteHasAServedGolden walks api.Routes: every route has a case,
// and no case names a route that is gone.
func TestEveryRouteHasAServedGolden(t *testing.T) {
	w := newServedWorld(t)
	d := Deps{Threads: w.svc, Users: &users.Service{DB: w.db, Perm: w.svc.Perm}, Forums: &forums.Service{DB: w.db, Perm: w.svc.Perm},
		Forms: &formsvc.Service{DB: w.db}, Applications: &applications.Service{DB: w.db}, Perm: w.svc.Perm}
	routes := patterns(Routes(d))
	if len(routes) < 100 {
		t.Fatalf("api.Routes returned %d routes; the walk is not seeing the API", len(routes))
	}
	for _, p := range servedCoverage(routes, servedCases) {
		t.Error(p)
	}

	// The walk can fail. A route registered through adminRoute in a test
	// world, as a later PR's route would be, with no case: named, with the
	// instruction. A case for a route that is gone: named as stale.
	aw := newAdminWorld(t)
	aw.fixtureRoutes()
	extra := []string{}
	for _, a := range aw.h.adminRoutes {
		extra = append(extra, a.pattern)
	}
	all := append(patterns(aw.h.routes()), extra...)
	cases := maps.Clone(servedCases)
	cases["GET /api/v1/gone"] = at("", "/api/v1/gone")
	got := servedCoverage(all, cases)
	if len(got) != 4 {
		t.Errorf("the walk over a test world with three fixture routes and a stale case found %d problems, want 4:\n%s",
			len(got), strings.Join(got, "\n"))
	}
	for _, want := range []*regexp.Regexp{
		regexp.MustCompile(`^route "POST /api/v1/admin/_test/admin/\{id\}" has no served golden: add a servedCases entry`),
		regexp.MustCompile(`^route "POST /api/v1/admin/_test/moderates/\{id\}" has no served golden`),
		regexp.MustCompile(`^route "POST /api/v1/admin/_test/staff/\{id\}" has no served golden`),
		regexp.MustCompile(`^servedCases names "GET /api/v1/gone"`),
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
