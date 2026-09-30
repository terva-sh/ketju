package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/auth/oidc/oidctest"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
)

// Phase 2's exit (TKT-01M3NTSCMN, docs/09 and docs/13): "a club can open
// registration to strangers safely, and an admin can give an agent an
// account and a credential". Each test drives the real serve, as the binary
// assembles it, through its listener: the middleware order, the pre-user
// gate, the admin gates, the bearer middleware and the session chokepoint
// together. People sign in through the fake OpenID provider, which signs
// its tokens as a real one does. Registration is in approval mode, the
// default a fresh install starts in.
//
// Each test starts its own serve, so each runs alone under -run, and each
// stays within the burst of sign-ins /auth/* allows one address.

// exitSubjects are everyone the provider will sign in: the seeded staff
// and member, and the strangers, who have no account until approved.
var exitSubjects = []string{"mona-sub", "ada-sub", "sam-sub", "stranger-sub", "stranger2-sub", "stranger3-sub"}

// The seeded people: mona moderates, ada administers, and sam is a member.
var (
	mona = seeded{subject: "mona-sub", handle: "mona", role: domain.RoleModerator}
	ada  = seeded{subject: "ada-sub", handle: "ada", role: domain.RoleAdmin}
	sam  = seeded{subject: "sam-sub", handle: "sam", role: domain.RoleMember}
)

// exitServer is one serve for one exit test.
type exitServer struct {
	r   *running
	idp *oidctest.IdP
}

func startExit(t *testing.T, people ...seeded) *exitServer {
	t.Helper()
	idp := oidctest.New(t)
	idp.Users = exitSubjects
	path, data := writeConfig(t, providerBlock(idp.URL))
	seedPeople(t, data, idp.URL, people...)
	r := start(t, path, serveOpts{idpClient: idp.Client(), getenv: siteURL})
	return &exitServer{r: r, idp: idp}
}

// signIn signs a in as subject.
func (s *exitServer) signIn(t *testing.T, a *actor, subject string) answer {
	t.Helper()
	return signIn(t, a, s.idp, subject)
}

// member signs a new actor in as p, who has an account, and checks that
// the answer is a member session.
func (s *exitServer) member(t *testing.T, p seeded) *actor {
	t.Helper()
	a := newActor(s.r, p.handle)
	res := s.signIn(t, a, p.subject)
	if res.status != http.StatusSeeOther || res.header.Get("Location") != "/" || a.session() == "" {
		t.Fatalf("%s: signing in answered %d to %q, session %t:\n%s", p.handle, res.status, res.header.Get("Location"), a.session() != "", res)
	}
	if m := a.me(t); m.User == nil || m.User.Handle != p.handle {
		t.Fatalf("%s: /me after signing in is %+v", p.handle, m)
	}
	return a
}

// applicant signs a new actor in as subject, who has no account, and checks
// that the answer is a pre-user session on its way to the form.
func (s *exitServer) applicant(t *testing.T, subject string) *actor {
	t.Helper()
	a := newActor(s.r, subject)
	res := s.signIn(t, a, subject)
	if res.status != http.StatusSeeOther || res.header.Get("Location") != "/apply" || a.session() == "" {
		t.Fatalf("%s: signing in answered %d to %q, session %t:\n%s", subject, res.status, res.header.Get("Location"), a.session() != "", res)
	}
	if m := a.me(t); m.User != nil || m.PreUser == nil || m.PreUser.Provider != e2eProviderKey {
		t.Fatalf("%s: /me for a stranger is %+v, want no user and a pre-user at %q", subject, m, e2eProviderKey)
	}
	return a
}

// newThread is a thread a starts in general, by id.
func newThread(t *testing.T, a *actor) string {
	t.Helper()
	res := a.do(t, http.MethodPost, "/api/v1/forums/general/threads",
		map[string]any{"title": "Welcome, new members", "source": "Say hello here.", "tags": []string{}})
	if res.status != http.StatusCreated {
		t.Fatalf("%s: starting a thread answered %d:\n%s", a.name, res.status, res)
	}
	var out struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	res.decode(t, &out)
	return out.Thread.ID
}

// applicationStatus is what the tests read of an ApplicationStatus.
type applicationStatus struct {
	Status string  `json:"status"`
	SignIn *string `json:"sign_in"`
}

// apply reads the registration form and submits it, answered, as a.
func apply(t *testing.T, a *actor, name string) {
	t.Helper()
	res := a.do(t, http.MethodGet, "/api/v1/apply/form", nil)
	if res.status != http.StatusOK {
		t.Fatalf("%s: the form answered %d:\n%s", a.name, res.status, res)
	}
	var form struct {
		Form struct {
			Version int `json:"version"`
		} `json:"form"`
	}
	res.decode(t, &form)
	res = a.do(t, http.MethodPost, "/api/v1/apply", map[string]any{"version": form.Form.Version,
		"answers": map[string]any{"name": name, "who": "Sam from the reading group invited me.", "rules": true}})
	var st applicationStatus
	if res.status == http.StatusCreated {
		res.decode(t, &st)
	}
	if res.status != http.StatusCreated || st.Status != "pending" {
		t.Fatalf("%s: applying answered %d:\n%s", a.name, res.status, res)
	}
}

// statusOf is a's GET /api/v1/apply/status, which must answer 200.
func statusOf(t *testing.T, a *actor) applicationStatus {
	t.Helper()
	res := a.do(t, http.MethodGet, "/api/v1/apply/status", nil)
	if res.status != http.StatusOK {
		t.Fatalf("%s: the status answered %d:\n%s", a.name, res.status, res)
	}
	var st applicationStatus
	res.decode(t, &st)
	return st
}

// queuedApplication is the one application the moderator queue lists for
// a, by id: each test's server has exactly one pending when it asks.
func queuedApplication(t *testing.T, a *actor) int64 {
	t.Helper()
	res := a.do(t, http.MethodGet, "/api/v1/admin/queue", nil)
	if res.status != http.StatusOK {
		t.Fatalf("%s: the queue answered %d:\n%s", a.name, res.status, res)
	}
	var q struct {
		Items []struct {
			Kind        string `json:"kind"`
			Application *struct {
				ID     int64  `json:"id"`
				Status string `json:"status"`
			} `json:"application"`
		} `json:"items"`
	}
	res.decode(t, &q)
	var ids []int64
	for _, it := range q.Items {
		if it.Kind == "application" && it.Application != nil && it.Application.Status == "pending" {
			ids = append(ids, it.Application.ID)
		}
	}
	if len(ids) != 1 {
		t.Fatalf("%s: the queue lists pending applications %v, want exactly one:\n%s", a.name, ids, res)
	}
	return ids[0]
}

// threadPost is what the tests read of a post on a thread page.
type threadPost struct {
	Ordinal int `json:"ordinal"`
	Author  struct {
		Handle string `json:"handle"`
		Kind   string `json:"kind"`
	} `json:"author"`
	HTML            string `json:"html"`
	MachineAuthored bool   `json:"machine_authored"`
}

// threadPage is a's first page of thread id: its log length and its posts.
func threadPage(t *testing.T, a *actor, id string) (int64, []threadPost) {
	t.Helper()
	res := a.do(t, http.MethodGet, "/api/v1/threads/"+id, nil)
	if res.status != http.StatusOK {
		t.Fatalf("%s: the thread answered %d:\n%s", a.name, res.status, res)
	}
	var page struct {
		Thread struct {
			LogLength int64 `json:"log_length"`
		} `json:"thread"`
		Posts []threadPost `json:"posts"`
	}
	res.decode(t, &page)
	return page.Thread.LogLength, page.Posts
}

// reply posts source to thread id as a, and answers the answer.
func reply(t *testing.T, a *actor, id, source string) answer {
	t.Helper()
	return a.do(t, http.MethodPost, "/api/v1/threads/"+id+"/posts", map[string]any{"source": source})
}

// created is the ordinal and log length a 201 from reply names.
func created(t *testing.T, res answer) (int, int64) {
	t.Helper()
	var out struct {
		Ordinal   int   `json:"ordinal"`
		LogLength int64 `json:"log_length"`
	}
	res.decode(t, &out)
	return out.Ordinal, out.LogLength
}

// A stranger signs in and gets a pre-user session, which reaches the
// application flow and nothing a member may; a moderator approves them;
// and their next sign-in is a member session that posts.
func TestPhase2ExitApprovalRegistration(t *testing.T) {
	s := startExit(t, mona)
	moderator := s.member(t, mona)
	thread := newThread(t, moderator)

	stranger := s.applicant(t, "stranger-sub")
	preUser := stranger.session()

	// The sample of member and admin routes, each refused. The pre-user
	// gate's own test walks every route (TKT-01M29CKRG9); this is the same
	// rule through the assembled server.
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/v1/forums/general/threads", map[string]any{"title": "Hello", "source": "Hello", "tags": []string{}}},
		{http.MethodPost, "/api/v1/threads/" + thread + "/posts", map[string]any{"source": "Hello"}},
		{http.MethodGet, "/api/v1/threads/" + thread, nil},
		{http.MethodGet, "/api/v1/forums", nil},
		{http.MethodGet, "/api/v1/users?prefix=m", nil},
		{http.MethodPut, "/api/v1/drafts/" + thread, map[string]any{"source": "Hello"}},
		{http.MethodGet, "/api/v1/admin/applications", nil},
		{http.MethodGet, "/api/v1/admin/queue", nil},
	} {
		if res := stranger.do(t, c.method, c.path, c.body); res.status != http.StatusForbidden {
			t.Errorf("%s %s as a pre-user answered %d, want 403:\n%s", c.method, c.path, res.status, res)
		}
	}
	// A page is answered with the status page, where an applicant belongs.
	if res := stranger.do(t, http.MethodGet, "/f/general", nil); res.status != http.StatusSeeOther ||
		res.header.Get("Location") != "/apply/status" {
		t.Errorf("GET /f/general as a pre-user answered %d to %q, want 303 to /apply/status", res.status, res.header.Get("Location"))
	}

	// The application flow is reached: the form, submitting it, its status.
	apply(t, stranger, "Stranger Danger")
	if st := statusOf(t, stranger); st.Status != "pending" {
		t.Fatalf("the status after applying is %q, want pending", st.Status)
	}

	// The moderator finds it in the queue, reads it, and approves it. The
	// fake provider sends no preferred_username or email, so the server
	// suggests no handle, and the moderator writes one, as the review
	// screen lets them.
	id := queuedApplication(t, moderator)
	res := moderator.do(t, http.MethodGet, fmt.Sprintf("/api/v1/admin/applications/%d", id), nil)
	if res.status != http.StatusOK || !strings.Contains(res.String(), "Stranger Danger") {
		t.Fatalf("the application answered %d:\n%s", res.status, res)
	}
	const handle = "newcomer"
	res = moderator.do(t, http.MethodPost, fmt.Sprintf("/api/v1/admin/applications/%d/approve", id), map[string]any{"handle": handle})
	var decided struct {
		Status string `json:"status"`
	}
	res.decode(t, &decided)
	if res.status != http.StatusOK || decided.Status != "approved" {
		t.Fatalf("approving answered %d:\n%s", res.status, res)
	}

	// Approval issues no session. The pre-user cookie still has no user:
	// it has retired, so it is signed out, and the status route refuses it
	// as it refuses anyone signed out (maintainer decision 2026-09-29,
	// TKT-01M3Q2V25E).
	retired := holding(s.r, "the pre-user cookie after approval", preUser)
	if m := retired.me(t); m.User != nil || m.PreUser != nil {
		t.Errorf("/me for the pre-user cookie after approval is %+v, want signed out", m)
	}
	if res := holding(s.r, "the pre-user cookie after approval", preUser).do(t, http.MethodGet, "/api/v1/apply/status", nil); res.status != http.StatusUnauthorized {
		t.Errorf("the status for the pre-user cookie after approval answered %d, want 401:\n%s", res.status, res)
	}

	// The next sign-in is a member session, through the session
	// chokepoint's status check.
	res = s.signIn(t, stranger, "stranger-sub")
	if res.status != http.StatusSeeOther || res.header.Get("Location") != "/" {
		t.Fatalf("signing in after approval answered %d to %q:\n%s", res.status, res.header.Get("Location"), res)
	}
	if stranger.session() == "" || stranger.session() == preUser {
		t.Fatal("signing in after approval set no new session")
	}
	m := stranger.me(t)
	if m.User == nil || m.User.Handle != handle || m.User.Role != domain.RoleMember || m.PreUser != nil {
		t.Fatalf("/me for the new member is %+v, want @%s, a member", m, handle)
	}
	// The decision waits for them there (TKT-01M29CP2N0).
	if m.Notifications != 1 {
		t.Errorf("the new member has %d notifications, want the approval's 1", m.Notifications)
	}
	res = reply(t, stranger, thread, "Hello from the newest member.")
	if res.status != http.StatusCreated {
		t.Fatalf("the new member's post answered %d:\n%s", res.status, res)
	}
	ordinal, _ := created(t, res)
	_, posts := threadPage(t, moderator, thread)
	found := false
	for _, p := range posts {
		if p.Ordinal == ordinal {
			found = p.Author.Handle == handle && strings.Contains(p.HTML, "Hello from the newest member.")
		}
	}
	if !found {
		t.Errorf("the thread does not show post #%d by @%s: %+v", ordinal, handle, posts)
	}

	// The old pre-user cookie is refused everywhere a member goes.
	old := holding(s.r, "the old pre-user cookie", preUser)
	if res := reply(t, old, thread, "Still here?"); res.status != http.StatusUnauthorized {
		t.Errorf("a post with the old pre-user cookie answered %d, want 401:\n%s", res.status, res)
	}
	if m := old.me(t); m.User != nil || m.PreUser != nil {
		t.Errorf("/me with the old pre-user cookie is %+v, want signed out", m)
	}
}

// A denied applicant never gets a member session and never sees the
// moderator's note; a deny with block closes the form to them.
func TestPhase2ExitDeniedApplicant(t *testing.T) {
	s := startExit(t, mona)
	moderator := s.member(t, mona)

	const note = "note-7f3a"
	stranger := s.applicant(t, "stranger2-sub")
	apply(t, stranger, "Second Stranger")
	res := moderator.do(t, http.MethodPost, fmt.Sprintf("/api/v1/admin/applications/%d/deny", queuedApplication(t, moderator)),
		map[string]any{"note": note, "block": false})
	if res.status != http.StatusOK {
		t.Fatalf("denying answered %d:\n%s", res.status, res)
	}
	if st := statusOf(t, stranger); st.Status != "denied" || st.SignIn != nil {
		t.Errorf("the status after a denial is %+v, want denied with no sign-in", st)
	}

	// Signing in again is an applicant's session again, back at the status
	// page, and never a member's.
	res = s.signIn(t, stranger, "stranger2-sub")
	if res.status != http.StatusSeeOther || res.header.Get("Location") != "/apply/status" || stranger.session() == "" {
		t.Fatalf("signing in after a denial answered %d to %q:\n%s", res.status, res.header.Get("Location"), res)
	}
	if m := stranger.me(t); m.User != nil || m.PreUser == nil {
		t.Errorf("/me after a denial is %+v, want a pre-user and no user", m)
	}
	if st := statusOf(t, stranger); st.Status != "denied" {
		t.Errorf("the status after signing in again is %q, want denied", st.Status)
	}
	for _, path := range []string{"/api/v1/forums", "/api/v1/users?prefix=m", "/api/v1/admin/applications"} {
		if res := stranger.do(t, http.MethodGet, path, nil); res.status != http.StatusForbidden {
			t.Errorf("GET %s after a denial answered %d, want 403", path, res.status)
		}
	}
	stranger.do(t, http.MethodGet, "/api/v1/apply/form", nil)
	for i, b := range stranger.bodies {
		if strings.Contains(b, note) {
			t.Errorf("answer %d to the denied applicant carries the moderator's note:\n%s", i, b)
		}
	}

	// A deny with block: the form is closed to that identity, in the
	// session it has and in the next.
	blocked := s.applicant(t, "stranger3-sub")
	apply(t, blocked, "Third Stranger")
	res = moderator.do(t, http.MethodPost, fmt.Sprintf("/api/v1/admin/applications/%d/deny", queuedApplication(t, moderator)),
		map[string]any{"note": "note-b10c", "block": true})
	if res.status != http.StatusOK {
		t.Fatalf("denying with block answered %d:\n%s", res.status, res)
	}
	if res := blocked.do(t, http.MethodGet, "/api/v1/apply/form", nil); res.status != http.StatusForbidden {
		t.Errorf("the form after a deny with block answered %d, want 403:\n%s", res.status, res)
	}
	s.signIn(t, blocked, "stranger3-sub")
	if m := blocked.me(t); m.User != nil {
		t.Errorf("/me for a blocked applicant is %+v, want no user", m)
	}
	if res := blocked.do(t, http.MethodGet, "/api/v1/apply/form", nil); res.status != http.StatusForbidden {
		t.Errorf("the form after signing in blocked answered %d, want 403:\n%s", res.status, res)
	}
}

// A suspended member loses their session at once and cannot sign in, and
// signs in again once the suspension is lifted.
func TestPhase2ExitSuspendedMember(t *testing.T) {
	s := startExit(t, mona, sam)
	moderator := s.member(t, mona)
	member := s.member(t, sam)
	thread := newThread(t, moderator)
	if res := reply(t, member, thread, "Before the suspension."); res.status != http.StatusCreated {
		t.Fatalf("the member's post answered %d:\n%s", res.status, res)
	}

	const reason = "reason-3c9d: a week to cool off"
	cookie := member.session()
	if res := moderator.do(t, http.MethodPost, "/api/v1/admin/users/sam/suspend", map[string]any{"reason": reason}); res.status != http.StatusNoContent {
		t.Fatalf("suspending answered %d:\n%s", res.status, res)
	}

	// The cookie loses access on the next request.
	if res := reply(t, holding(s.r, "the suspended member's cookie", cookie), thread, "After the suspension."); res.status != http.StatusUnauthorized {
		t.Errorf("a post with the suspended member's cookie answered %d, want 401:\n%s", res.status, res)
	}
	if m := member.me(t); m.User != nil {
		t.Errorf("/me with the suspended member's cookie is %+v, want signed out", m)
	}

	// Signing in shows the account as unavailable, with the reason, and
	// sets no session.
	res := s.signIn(t, member, sam.subject)
	if res.status != http.StatusForbidden || !strings.Contains(res.String(), "Account unavailable") ||
		!strings.Contains(res.String(), "Reason: "+reason) {
		t.Errorf("signing in suspended answered %d:\n%s", res.status, res)
	}
	for _, c := range res.cookies {
		if c.Name == session.CookieName && c.Value != "" {
			t.Errorf("signing in suspended set a session cookie")
		}
	}
	if member.session() != "" {
		t.Error("the suspended member holds a session after signing in")
	}

	// Lifted, they sign in again as themselves.
	if res := moderator.do(t, http.MethodPost, "/api/v1/admin/users/sam/unsuspend", nil); res.status != http.StatusNoContent {
		t.Fatalf("unsuspending answered %d:\n%s", res.status, res)
	}
	back := s.member(t, sam)
	if res := reply(t, back, thread, "Back again."); res.status != http.StatusCreated {
		t.Errorf("the member's post after the suspension answered %d:\n%s", res.status, res)
	}
}

// An admin provisions an agent with a credential; the agent posts with its
// bearer token, machine-authored; and once the credential is revoked the
// agent is refused and writes nothing.
func TestPhase2ExitAgentCredential(t *testing.T) {
	s := startExit(t, ada)
	admin := s.member(t, ada)
	thread := newThread(t, admin)

	res := admin.do(t, http.MethodPost, "/api/v1/admin/users",
		map[string]any{"kind": domain.KindAgent, "handle": "helperbot", "display_name": "Helper Bot", "issue_credential": true})
	var made struct {
		Credential *struct {
			Token string `json:"token"`
		} `json:"credential"`
	}
	if res.status == http.StatusCreated {
		res.decode(t, &made)
	}
	if res.status != http.StatusCreated || made.Credential == nil || made.Credential.Token == "" {
		t.Fatalf("provisioning the agent answered %d:\n%s", res.status, res)
	}

	agent := newActor(s.r, "helperbot")
	agent.bearer = made.Credential.Token
	res = reply(t, agent, thread, "Posted by the agent.")
	if res.status != http.StatusCreated {
		t.Fatalf("the agent's post answered %d:\n%s", res.status, res)
	}
	if sc := res.header.Values("Set-Cookie"); len(sc) != 0 {
		t.Errorf("the agent's post set cookies %q; a credential makes no session", sc)
	}
	ordinal, _ := created(t, res)
	length, posts := threadPage(t, admin, thread)
	found := false
	for _, p := range posts {
		if p.Ordinal == ordinal {
			found = true
			if !p.MachineAuthored || p.Author.Kind != domain.KindAgent || p.Author.Handle != "helperbot" {
				t.Errorf("the agent's post is served as %+v, want machine_authored and author.kind agent", p)
			}
		}
	}
	if !found {
		t.Fatalf("the thread does not show the agent's post #%d: %+v", ordinal, posts)
	}

	if res := admin.do(t, http.MethodDelete, "/api/v1/admin/users/helperbot/credential", nil); res.status != http.StatusNoContent {
		t.Fatalf("revoking the credential answered %d:\n%s", res.status, res)
	}
	res = reply(t, agent, thread, "Posted by the agent.")
	if res.status != http.StatusUnauthorized {
		t.Errorf("the agent's post after revocation answered %d, want 401:\n%s", res.status, res)
	}
	if sc := res.header.Values("Set-Cookie"); len(sc) != 0 {
		t.Errorf("the refused agent was set cookies %q", sc)
	}
	if after, _ := threadPage(t, admin, thread); after != length {
		t.Errorf("the thread's log_length moved from %d to %d after the refused post", length, after)
	}
}
