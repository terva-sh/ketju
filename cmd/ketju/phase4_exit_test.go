package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/oidc/oidctest"
	"github.com/terva-sh/ketju/internal/backup"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// Phase 4's exit (TKT-01M3YMG0NX, docs/09 and docs/15). docs/09 gives the
// criterion as "v1.0 tag", which is the maintainer's to push; these tests
// prove the work behind it through the real serve, as the binary assembles
// it, the way the TestPhase3Exit tests do. They hold docs/15 "Invariants,
// added":
//
//   - moving posts follows docs/08's thread-move rule and nothing else
//     changes who can read: a cross-forum split or merge needs moderate in
//     both forums; a moved post is served under the target forum's rules
//     and by no other path; a notice names the other thread only to a
//     viewer who may read it;
//   - archived means read-only for everyone, admins included;
//   - a snapshot ketju backup makes passes ketju fsck after restore;
//
// and the deliverables beside them: tags under their rules, and /metrics
// only on its own listener.
//
// The world (newP4World) is three forums: general, public; and vault and
// annex, restricted. Who holds what, all through groups but ada's:
//
//   - ada, an admin by role, and mona, a moderator by role;
//   - kim, who holds every permission in vault, annex and general: the
//     moderator by group grant in both forums of a move;
//   - gail, who holds every permission in vault alone: she moderates and
//     reads the source of a vault-to-annex move and cannot read the target;
//   - otto, who holds every permission in annex and read in vault: he
//     moderates the target alone and can still read the source;
//   - pat, who reads annex alone: the viewer of the target alone;
//   - sam, a plain member, who starts threads in general.
//
// Each expected answer comes from the viewer's effective permission, read
// from what the thread page serves that viewer, never from a blanket
// "nothing". Each test starts one serve and signs at most five people in,
// the burst of /auth/* one address has (docs/08). An agent acts through its
// bearer credential, which spends no /auth/*.

var p4Kim = seeded{subject: "kim-sub", handle: "kim", role: domain.RoleMember}

// p4People is everyone the world seeds.
var p4People = []seeded{ada, mona, sam, p4Kim, p3Gail, p3Otto, p3Pat}

// seedP4 writes, beside seedPeople's general, the restricted forums vault
// and annex, and the groups that give kim, gail, otto and pat what the world
// says, with upload for every member everywhere they may post, which no
// migration grants (docs/07 "Attachments").
func seedP4(t *testing.T, data string) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, db.Options{Path: filepath.Join(data, "db", "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	now := time.Now().UnixMilli()
	forums := map[string]int64{}
	groups := map[string]int64{}
	users := map[string]int64{}
	if err := d.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		for i, slug := range []string{"vault", "annex"} {
			id, err := q.CreateForum(ctx, dbq.CreateForumParams{Slug: slug, Name: strings.ToUpper(slug[:1]) + slug[1:],
				Description: "Restricted", Visibility: domain.VisibilityRestricted, Position: int64(i + 1),
				CreatedAt: now, UpdatedAt: now})
			if err != nil {
				return err
			}
			forums[slug] = id
		}
		f, err := q.GetForumBySlug(ctx, "general")
		if err != nil {
			return err
		}
		forums["general"] = f.ID
		for _, slug := range []string{"both", "keepers", "annex-mods", "vault-readers", "annex-readers"} {
			id, err := q.CreateGroup(ctx, dbq.CreateGroupParams{Name: slug, Slug: slug, CreatedAt: now, UpdatedAt: now})
			if err != nil {
				return err
			}
			groups[slug] = id
		}
		g, err := q.GetGroupBySlug(ctx, domain.GroupMembers)
		if err != nil {
			return err
		}
		groups["members"] = g.ID
		for _, h := range []string{"kim", "gail", "otto", "pat"} {
			u, err := q.GetUserByHandle(ctx, h)
			if err != nil {
				return err
			}
			users[h] = u.ID
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	e := perm.New(d)
	by := audit.System("phase 4 exit seed")
	grant := func(forum, group string, ps ...domain.Permission) {
		for _, p := range ps {
			if err := e.Grant(ctx, by, forums[forum], groups[group], p); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, f := range []string{"vault", "annex", "general"} {
		grant(f, "both", domain.AllPermissions...)
	}
	grant("vault", "keepers", domain.AllPermissions...)
	grant("annex", "annex-mods", domain.AllPermissions...)
	grant("vault", "vault-readers", domain.PermRead)
	grant("annex", "annex-readers", domain.PermRead)
	if err := e.Grant(ctx, by, 0, groups["members"], domain.PermUpload); err != nil {
		t.Fatal(err)
	}
	for group, who := range map[string][]string{
		"both": {"kim"}, "keepers": {"gail"}, "annex-mods": {"otto"}, "vault-readers": {"otto"}, "annex-readers": {"pat"},
	} {
		for _, h := range who {
			if err := e.AddMember(ctx, by, groups[group], users[h]); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// p4World is one serve over the Phase 4 world, with its metrics listener.
type p4World struct {
	t                 *testing.T
	path, data, maddr string
	idp               *oidctest.IdP
	r                 *running
	as                map[string]*actor
	// addrs is how many loopback addresses from has handed out.
	addrs int
}

// newP4World seeds the world and starts serve with metrics_listen on a
// port of its own, then signs people in. forBackup puts the data directory
// where its control socket path fits, as `ketju backup` needs.
func newP4World(t *testing.T, forBackup bool, people ...seeded) *p4World {
	t.Helper()
	if len(people) > 5 {
		t.Fatalf("%d sign-ins on one serve; /auth/* allows five", len(people))
	}
	w := &p4World{t: t, as: map[string]*actor{}}
	w.idp = oidctest.New(t)
	for _, p := range p4People {
		w.idp.Users = append(w.idp.Users, p.subject)
	}
	config := writeConfig
	if forBackup {
		config = backupConfig
	}
	w.path, w.data = config(t, providerBlock(w.idp.URL))
	seedPeople(t, w.data, w.idp.URL, p4People...)
	seedP4(t, w.data)
	w.maddr = freePort(t)
	w.serve()
	for _, p := range people {
		a := w.from(p.handle)
		res := signIn(t, a, w.idp, p.subject)
		if res.status != http.StatusSeeOther || res.header.Get("Location") != "/" || a.session() == "" {
			t.Fatalf("%s: signing in answered %d to %q:\n%s", p.handle, res.status, res.header.Get("Location"), res)
		}
		if m := a.me(t); m.User == nil || m.User.Handle != p.handle {
			t.Fatalf("%s: /me after signing in is %+v", p.handle, m)
		}
		w.as[p.handle] = a
	}
	return w
}

// from is a new actor whose requests come from a loopback address of
// their own, 127.0.0.11 and up, as each person's would. The write ceiling
// is per address (docs/08 "The transport-level ceiling"), and the archive
// test alone makes more writes than one address's burst of sixty.
func (w *p4World) from(name string) *actor {
	w.t.Helper()
	w.addrs++
	d := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, byte(10+w.addrs))}}
	tr := &http.Transport{DialContext: d.DialContext}
	w.t.Cleanup(tr.CloseIdleConnections)
	a := newActor(w.r, name)
	a.client = &http.Client{Transport: tr, CheckRedirect: noRedirects.CheckRedirect}
	return a
}

// serve starts serve on the world's config, and points every actor at it.
func (w *p4World) serve() {
	w.t.Helper()
	ready := make(chan string, 1)
	w.r = start(w.t, w.path, serveOpts{idpClient: w.idp.Client(), getenv: withMetrics(w.maddr),
		metricsReady: func(a string) { ready <- a }})
	w.maddr = <-ready
	for _, a := range w.as {
		a.r = w.r
	}
}

// restart stops serve and starts it again on the same data directory.
func (w *p4World) restart() {
	w.t.Helper()
	if err := w.r.shutdown(w.t); err != nil {
		w.t.Fatalf("stopping serve: %v\n%s", err, w.r.log)
	}
	w.serve()
}

// agent is an agent account ada provisions with a credential, as an actor
// that sends it as a bearer token.
func (w *p4World) agent(handle string) *actor {
	w.t.Helper()
	res := w.as["ada"].must(w.t, http.StatusCreated, http.MethodPost, "/api/v1/admin/users",
		map[string]any{"kind": domain.KindAgent, "handle": handle, "display_name": "Agent " + handle, "issue_credential": true})
	var made struct {
		Credential *struct {
			Token string `json:"token"`
		} `json:"credential"`
	}
	res.decode(w.t, &made)
	if made.Credential == nil || made.Credential.Token == "" {
		w.t.Fatalf("provisioning %s issued no credential:\n%s", handle, res)
	}
	a := w.from(handle)
	a.bearer = made.Credential.Token
	return a
}

// view is a's first page of thread: the answer, and the page when it is 200.
// Every thread here fits on one page.
func (w *p4World) view(a *actor, thread string) (answer, dto.ThreadPage) {
	w.t.Helper()
	res := a.do(w.t, http.MethodGet, "/api/v1/threads/"+thread+"?page=1", nil)
	var p dto.ThreadPage
	if res.status == http.StatusOK {
		res.decode(w.t, &p)
	}
	return res, p
}

// mustView is view, failing unless the page is served.
func (w *p4World) mustView(a *actor, thread string) dto.ThreadPage {
	w.t.Helper()
	res, p := w.view(a, thread)
	if res.status != http.StatusOK {
		w.t.Fatalf("%s: thread %s answered %d:\n%s", a.name, thread, res.status, res)
	}
	return p
}

// postAt is ordinal o on p, failing when the page lacks it.
func postAt(t *testing.T, p dto.ThreadPage, o int) dto.Post {
	t.Helper()
	for _, q := range p.Posts {
		if q.Ordinal == o {
			return q
		}
	}
	t.Fatalf("thread %s has no #%d on its page", p.Thread.ID, o)
	return dto.Post{}
}

// logOf is thread's log, read from the data directory.
func (w *p4World) logOf(thread string) []byte {
	w.t.Helper()
	id, err := threadid.Parse(thread)
	if err != nil {
		w.t.Fatal(err)
	}
	b, err := os.ReadFile(id.LogPath(w.data))
	if err != nil {
		w.t.Fatal(err)
	}
	return b
}

// threadFiles is how many thread logs the data directory holds.
func (w *p4World) threadFiles() int {
	w.t.Helper()
	sizes, _, err := threadid.WalkLogs(w.data)
	if err != nil {
		w.t.Fatal(err)
	}
	return len(sizes)
}

// codeOf is a problem answer's code, "" for an answer that is not one.
func codeOf(res answer) string {
	var p struct {
		Type string `json:"type"`
	}
	if res.status < 300 || json.Unmarshal(res.body, &p) != nil {
		return ""
	}
	return p.Type
}

// split moves ordinals of source into a new thread titled title in forum,
// as a.
func (a *actor) split(t *testing.T, source string, ordinals []int, forum, title string) answer {
	t.Helper()
	return a.do(t, http.MethodPost, "/api/v1/admin/threads/"+source+"/split",
		map[string]any{"ordinals": ordinals, "title": title, "forum": forum})
}

// merge moves ordinals of source to the end of into, as a.
func (a *actor) merge(t *testing.T, source string, ordinals []int, into string) answer {
	t.Helper()
	return a.do(t, http.MethodPost, "/api/v1/admin/threads/"+source+"/merge",
		map[string]any{"ordinals": ordinals, "into": into})
}

// continueAs starts a thread in forum as a reply as a new thread to
// from's post ordinal, as a.
func (a *actor) continueAs(t *testing.T, forum, title, source, from string, ordinal int) answer {
	t.Helper()
	return a.do(t, http.MethodPost, "/api/v1/forums/"+forum+"/threads", map[string]any{"title": title, "source": source,
		"tags": []string{}, "from": map[string]any{"thread": from, "ordinal": ordinal}})
}

// startTagged starts a thread in forum with tags as a, and answers it.
func (a *actor) startTagged(t *testing.T, forum, title string, tags []string) answer {
	t.Helper()
	return a.do(t, http.MethodPost, "/api/v1/forums/"+forum+"/threads",
		map[string]any{"title": title, "source": "Opening words.", "tags": tags})
}

// threadIDOf is the id of the thread a 201 or a move's answer names under key.
func threadIDOf(t *testing.T, res answer, key string) string {
	t.Helper()
	var out map[string]struct {
		ID string `json:"id"`
	}
	res.decode(t, &out)
	if out[key].ID == "" {
		t.Fatalf("the answer names no %s:\n%s", key, res)
	}
	return out[key].ID
}

// quoting is source quoting line from ordinal, as the composer writes a
// quote (docs/06 "Post anatomy").
func quoting(ordinal int, line, after string) string {
	return fmt.Sprintf("> [quote=#%d]\n> %s\n\n%s", ordinal, line, after)
}

// TestPhase4ExitMovesNeedModerateInBoth: a cross-forum split or merge goes
// through only for an actor who holds moderate in both forums, by role or
// through a group (docs/15, docs/08's thread-move rule). ada, an admin by
// role, and kim, by group grant in vault and annex, split vault into annex
// and merge annex into vault. gail, who moderates the source of the split
// and the target of the merge and cannot read annex, is refused, and so is
// otto, who moderates the split's target and the merge's source and reads
// vault; pat, who moderates nothing, is refused at the gate. Every refusal
// leaves both logs byte-identical and creates no thread file. gail splits
// within vault through her grant.
func TestPhase4ExitMovesNeedModerateInBoth(t *testing.T) {
	w := newP4World(t, false, ada, p4Kim, p3Gail, p3Otto, p3Pat)
	admin, kim, gail, otto, pat := w.as["ada"], w.as["kim"], w.as["gail"], w.as["otto"], w.as["pat"]
	v := admin.start(t, "vault", "Vault ledger", "The vault's first post.")
	a := admin.start(t, "annex", "Annex ledger", "The annex's first post.")
	for i := 2; i <= 6; i++ {
		admin.post(t, v, fmt.Sprintf("Vault post %d.", i))
		admin.post(t, a, fmt.Sprintf("Annex post %d.", i))
	}

	for _, c := range []struct {
		name string
		send func() answer
		want string
	}{
		// gail cannot read annex, so it is not-found to her: a move never
		// tells a forum hidden from the actor apart from one that does not
		// exist (TKT-01M29CYSBA).
		{"gail splits vault into annex, moderating the source alone", func() answer { return gail.split(t, v, []int{2}, "annex", "Away") }, "not-found"},
		{"gail merges annex into vault, moderating the target alone", func() answer { return gail.merge(t, a, []int{2}, v) }, "not-found"},
		{"otto splits vault into annex, moderating the target alone", func() answer { return otto.split(t, v, []int{2}, "annex", "Away") }, "forbidden"},
		{"otto merges annex into vault, moderating the source alone", func() answer { return otto.merge(t, a, []int{2}, v) }, "forbidden"},
		{"otto splits annex into vault, moderating the source alone", func() answer { return otto.split(t, a, []int{2}, "vault", "Away") }, "forbidden"},
		{"pat splits, at the gate", func() answer { return pat.split(t, v, []int{2}, "annex", "Away") }, "forbidden"},
		{"pat merges, at the gate", func() answer { return pat.merge(t, a, []int{2}, v) }, "forbidden"},
	} {
		lv, la, n := w.logOf(v), w.logOf(a), w.threadFiles()
		res := c.send()
		if got := codeOf(res); got != c.want {
			t.Errorf("%s: answered %d %q, want %q:\n%s", c.name, res.status, got, c.want, res)
		}
		if !bytes.Equal(w.logOf(v), lv) || !bytes.Equal(w.logOf(a), la) {
			t.Errorf("%s: a refused move changed a log", c.name)
		}
		if got := w.threadFiles(); got != n {
			t.Errorf("%s: a refused move left %d thread files, were %d", c.name, got, n)
		}
	}

	moved := func(who *actor, thread string, o int) {
		t.Helper()
		if p := postAt(t, w.mustView(who, thread), o); !p.Moved || !p.Deleted || p.HTML != "" {
			t.Errorf("%s: %s #%d after the move is %+v, want moved", who.name, thread, o, p)
		}
	}
	for _, c := range []struct {
		who     *actor
		ordinal int
	}{{admin, 2}, {kim, 3}} {
		res := c.who.split(t, v, []int{c.ordinal}, "annex", "Split by "+c.who.name)
		if res.status != http.StatusOK {
			t.Fatalf("%s's split of vault into annex answered %d:\n%s", c.who.name, res.status, res)
		}
		var out dto.MovedPosts
		res.decode(t, &out)
		if out.Thread.Forum.Slug != "annex" || out.Thread.PostCount != 1 {
			t.Errorf("%s's split made %+v", c.who.name, out.Thread)
		}
		moved(c.who, v, c.ordinal)

		res = c.who.merge(t, a, []int{c.ordinal}, v)
		if res.status != http.StatusOK {
			t.Fatalf("%s's merge of annex into vault answered %d:\n%s", c.who.name, res.status, res)
		}
		moved(c.who, a, c.ordinal)
	}
	res := gail.split(t, v, []int{4}, "vault", "Kept in the vault")
	if res.status != http.StatusOK {
		t.Fatalf("gail's split within vault answered %d:\n%s", res.status, res)
	}
	moved(gail, v, 4)
}

// p4Leaks are what no answer to a viewer who may not read annex may carry
// once vault's posts moved there.
func p4Leaks(t *testing.T, who *actor, words ...string) {
	t.Helper()
	for _, b := range who.bodies {
		for _, word := range words {
			if word != "" && strings.Contains(b, word) {
				t.Errorf("an answer to %s carries %q:\n%.600s", who.name, word, b)
			}
		}
	}
}

// noteKeys is a's notifications as kind thread#ordinal, sorted.
func noteKeys(t *testing.T, a *actor) []string {
	t.Helper()
	res := a.must(t, http.StatusOK, http.MethodGet, "/api/v1/notifications", nil)
	var out struct {
		Notifications []p3Note `json:"notifications"`
	}
	res.decode(t, &out)
	var keys []string
	for _, n := range out.Notifications {
		keys = append(keys, n.key())
	}
	slices.Sort(keys)
	return keys
}

// foundBy is a's search results for q, as thread#ordinal.
func foundBy(t *testing.T, a *actor, q string) []string {
	t.Helper()
	res := a.must(t, http.StatusOK, http.MethodGet, "/api/v1/search?q="+q, nil)
	var out struct {
		Results []p3Result `json:"results"`
	}
	res.decode(t, &out)
	var keys []string
	for _, r := range out.Results {
		keys = append(keys, r.key())
	}
	return keys
}

// mustJSON is v as JSON.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// eventually polls cond until it holds or 20 s pass.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s never happened", what)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// TestPhase4ExitMovedPostsFollowTheTargetForum: kim splits two vault posts
// into annex. #2 mentions gail, who was notified before the move, carries
// an attachment and quotes #1; #3 is flagged by gail and quoted by #4,
// which stays. Afterwards no path serves a moved post's body or blob to
// gail, who reads vault alone, or to an anonymous reader: the vault page and
// post, its replies and history, the moderator lens and the queue's flag
// entry, search, notifications, the author's post list, the blob, a live
// stream of vault or of annex, #4's quote, and #1's quoted_by. pat, who
// reads annex alone, reads the copies there under the original author and
// times, finds them by search, and is served the blob.
func TestPhase4ExitMovedPostsFollowTheTargetForum(t *testing.T) {
	w := newP4World(t, false, ada, p4Kim, p3Gail, p3Pat)
	admin, kim, gail, pat := w.as["ada"], w.as["kim"], w.as["gail"], w.as["pat"]
	anon := newActor(w.r, "anonymous")

	v := admin.start(t, "vault", "Vault minutes", "The minutes open here.")
	blob := admin.attach(t, v, "movedblob")
	admin.post(t, v, quoting(1, "The minutes open here.", "@gail, movedalpha is the plan."), blob)
	admin.post(t, v, "movedbeta for the record.")
	gail.must(t, http.StatusOK, http.MethodPost, "/api/v1/threads/"+v+"/flags", map[string]any{"ordinal": 3, "reason": "spam"})
	admin.post(t, v, quoting(3, "movedbeta for the record.", "This one stays."))

	mention := "mention " + v + "#2"
	eventually(t, "gail's mention", func() bool { return slices.Contains(noteKeys(t, gail), mention) })
	before := w.mustView(kim, v)
	for o, want := range map[int][]int{1: {2}, 3: {4}} {
		if got := postAt(t, before, o).QuotedBy; !slices.Equal(got, want) {
			t.Fatalf("before the move, #%d is quoted by %v, want %v", o, got, want)
		}
	}
	if p := postAt(t, w.mustView(gail, v), 4); !strings.Contains(p.HTML, "movedbeta") {
		t.Fatalf("before the move, #4's quote does not show #3's text to gail: %s", p.HTML)
	}

	// Streams, opened before the move: gail's of vault and of the thread,
	// and pat's of annex. gail's of annex and every anonymous one are
	// refused, as the pages are.
	gailF, gailV, patF := gail.stream(t, "forums=vault"), gail.stream(t, "threads="+v), pat.stream(t, "forums=annex")
	for _, s := range []*p3Stream{gailF, gailV, patF} {
		s.ready(t)
	}
	for _, s := range []*p3Stream{gail.stream(t, "forums=annex"), anon.stream(t, "threads="+v), anon.stream(t, "forums=vault")} {
		if s.status == http.StatusOK {
			t.Errorf("%s: the stream was admitted", s.who)
		}
	}

	res := kim.split(t, v, []int{2, 3}, "annex", "Annex minutes")
	if res.status != http.StatusOK {
		t.Fatalf("kim's split answered %d:\n%s", res.status, res)
	}
	target := threadIDOf(t, res, "thread")
	gail.bodies, anon.bodies = nil, nil

	// pat reads the copies, as annex's page serves them, under the
	// original authors and times; finds them; and is served the blob.
	eventually(t, "pat's search finding the copies", func() bool {
		return slices.Equal(foundBy(t, pat, "movedalpha"), []string{target + "#1"}) &&
			slices.Equal(foundBy(t, pat, "movedbeta"), []string{target + "#2"})
	})
	copies, kims := w.mustView(pat, target), w.mustView(kim, target)
	for i, o := range []int{2, 3} {
		was, got, k := postAt(t, before, o), postAt(t, copies, i+1), postAt(t, kims, i+1)
		if got.Author.Handle != "ada" || got.CreatedAt != was.CreatedAt || got.HTML == "" || got.Deleted {
			t.Errorf("pat reads copy #%d as %+v; want ada's, written at %d, shown as itself", i+1, got, was.CreatedAt)
		}
		if gj, kj := mustJSON(t, got), mustJSON(t, k); gj != kj {
			t.Errorf("pat and kim are served copy #%d differently:\n%+v\n%+v", i+1, got, k)
		}
	}
	if a := postAt(t, copies, 1).Attachments; len(a) != 1 || a[0].Hash != blob {
		t.Errorf("the first copy carries %+v, want the blob", a)
	}
	if got := pat.do(t, http.MethodGet, "/blobs/"+blob, nil).status; got != http.StatusOK {
		t.Errorf("pat: the moved blob answered %d, want 200", got)
	}
	patF.until(t, target, copies.Thread.LogLength, nil)

	// gail and anonymous: every path.
	page := w.mustView(gail, v)
	for _, o := range []int{2, 3} {
		if p := postAt(t, page, o); !p.Moved || p.HTML != "" || len(p.Attachments) != 0 || p.MovedTo != nil {
			t.Errorf("gail reads vault #%d as %+v; want moved, with no body, blob or target", o, p)
		}
	}
	if p := postAt(t, page, 4); strings.Contains(p.HTML, "movedbeta") || p.HTML == "" {
		t.Errorf("gail reads #4's quote of the moved #3 as %s; want a tombstone", p.HTML)
	}
	if got := postAt(t, page, 1).QuotedBy; len(got) != 0 {
		t.Errorf("gail reads #1 as quoted by %v; a moved quoting post fills none", got)
	}
	if got := postAt(t, w.mustView(kim, v), 1).QuotedBy; len(got) != 0 {
		t.Errorf("kim reads #1 as quoted by %v; a moved quoting post fills none", got)
	}
	for _, path := range []string{"/posts/2", "/posts/3", "/posts/2/replies", "/posts/3/replies",
		"/posts/2/history", "/posts/3/history", "?page=1&lens=mod", "?page=1&lens=mentions"} {
		gail.do(t, http.MethodGet, "/api/v1/threads/"+v+path, nil)
		if st := anon.do(t, http.MethodGet, "/api/v1/threads/"+v+path, nil).status; st == http.StatusOK {
			t.Errorf("anonymous: %s answered 200", path)
		}
	}
	gail.must(t, http.StatusOK, http.MethodGet, "/api/v1/admin/queue", nil)
	gail.must(t, http.StatusOK, http.MethodGet, "/api/v1/users/ada/posts", nil)
	anon.do(t, http.MethodGet, "/api/v1/users/ada/posts", nil)
	for _, a := range []*actor{gail, anon} {
		for _, word := range []string{"movedalpha", "movedbeta"} {
			if got := foundBy(t, a, word); len(got) != 0 {
				t.Errorf("%s: searching %q found %v", a.name, word, got)
			}
		}
		if got := a.do(t, http.MethodGet, "/blobs/"+blob, nil).status; got != http.StatusNotFound {
			t.Errorf("%s: the moved blob answered %d, want 404", a.name, got)
		}
		if got := a.do(t, http.MethodGet, "/blobs/"+blob+"/thumb", nil).status; got != http.StatusNotFound {
			t.Errorf("%s: the moved blob's thumbnail answered %d, want 404", a.name, got)
		}
		if st, _ := w.view(a, target); st.status == http.StatusOK {
			t.Errorf("%s: annex's copy answered 200", a.name)
		}
	}
	for _, k := range noteKeys(t, gail) {
		if k == mention || strings.Contains(k, target) {
			t.Errorf("gail is still told of the moved post: %q", k)
		}
	}

	// A post after the move reaches gail's streams, and nothing of annex
	// comes before it.
	_, n := admin.post(t, v, "After the move.")
	toAnnex := func(h hint) bool { return h.ThreadID == target }
	gailF.until(t, v, n, toAnnex)
	gailV.until(t, v, n, toAnnex)

	for _, a := range []*actor{gail, anon} {
		p4Leaks(t, a, "movedalpha", "movedbeta", "Annex minutes", target)
	}
}

// linkID is l's thread id, "" for none.
func linkID(l *dto.ThreadLink) string {
	if l == nil {
		return ""
	}
	return l.ID
}

// cacheHits is the response cache's hits on the metrics listener.
func (w *p4World) cacheHits() int {
	w.t.Helper()
	code, body := get(w.t, "http://"+w.maddr+"/metrics")
	if code != http.StatusOK {
		w.t.Fatalf("the metrics answered %d", code)
	}
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), `ketju_cache_hits_total{cache="response"} `); ok {
			n, err := strconv.Atoi(v)
			if err != nil {
				w.t.Fatal(err)
			}
			return n
		}
	}
	w.t.Fatalf("the metrics hold no response cache hits:\n%s", body)
	return 0
}

// TestPhase4ExitNoticesNameOnlyWhatTheViewerMayRead: kim splits vault posts
// into annex, merges vault posts into an annex thread and annex posts into
// vault, and replies as a new thread from a vault post into annex. kim and
// ada are shown every link. gail, who reads vault alone, sees the moved
// notices and merged-from notice with no thread, moved_to null and no
// "continued in" note; pat, who reads annex alone, sees "split from",
// "merged from" and the moved notice with no thread, and no "continued
// from" header. gail and kim share a viewer class and are served different
// bodies and ETags, and the second request for each is a response cache hit
// that answers the same, so nothing per viewer is cached.
func TestPhase4ExitNoticesNameOnlyWhatTheViewerMayRead(t *testing.T) {
	w := newP4World(t, false, ada, p4Kim, p3Gail, p3Pat)
	admin, kim, gail, pat := w.as["ada"], w.as["kim"], w.as["gail"], w.as["pat"]
	v := admin.start(t, "vault", "Vault notices", "Vault, first.")
	a := admin.start(t, "annex", "Annex notices", "Annex, first.")
	for i := 2; i <= 6; i++ {
		admin.post(t, v, fmt.Sprintf("Vault %d.", i))
	}
	for i := 2; i <= 3; i++ {
		admin.post(t, a, fmt.Sprintf("Annex %d.", i))
	}

	res := kim.split(t, v, []int{2, 3}, "annex", "Split to annex")
	if res.status != http.StatusOK {
		t.Fatalf("kim's split answered %d:\n%s", res.status, res)
	}
	s := threadIDOf(t, res, "thread")
	// v #4 and #5 go to a's end, as #4 and #5 there; a's #2 and #3 then go
	// to v's end, as #7 and #8.
	kim.must(t, http.StatusOK, http.MethodPost, "/api/v1/admin/threads/"+v+"/merge", map[string]any{"ordinals": []int{4, 5}, "into": a})
	res = kim.must(t, http.StatusOK, http.MethodPost, "/api/v1/admin/threads/"+a+"/merge", map[string]any{"ordinals": []int{2, 3}, "into": v})
	var merged dto.MergedPosts
	res.decode(t, &merged)
	intoV := merged.FirstCopy
	res = kim.continueAs(t, "annex", "Continued in annex", "Picking this up.", v, 6)
	if res.status != http.StatusCreated {
		t.Fatalf("kim's reply as a new thread answered %d:\n%s", res.status, res)
	}
	c := threadIDOf(t, res, "thread")
	intoA := 4

	type notice struct {
		count  int
		thread string
	}
	moveNotice := func(p dto.Post) *notice {
		if p.MoveNotice == nil {
			return nil
		}
		return &notice{p.MoveNotice.Count, linkID(p.MoveNotice.Thread)}
	}
	mergeNotice := func(p dto.Post) *notice {
		if p.MergeNotice == nil {
			return nil
		}
		return &notice{p.MergeNotice.Count, linkID(p.MergeNotice.Thread)}
	}
	movedTo := func(p dto.Post) string {
		if p.MovedTo == nil {
			return ""
		}
		return p.MovedTo.Thread.ID
	}
	continuedIn := func(p dto.Post) []string {
		var out []string
		for _, l := range p.ContinuedIn {
			out = append(out, l.ID)
		}
		return out
	}

	// What vault shows: the split's and the merge's notices and moved_to,
	// the merged-in notice, and #6's continued-in note.
	for _, c2 := range []struct {
		who  *actor
		full bool
	}{{kim, true}, {admin, true}, {gail, false}} {
		page := w.mustView(c2.who, v)
		want := func(id string) string {
			if c2.full {
				return id
			}
			return ""
		}
		for _, x := range []struct {
			o      int
			target string
		}{{2, s}, {4, a}} {
			p := postAt(t, page, x.o)
			if got := moveNotice(p); got == nil || *got != (notice{2, want(x.target)}) {
				t.Errorf("%s: vault #%d's moved notice is %+v, want 2 to %q", c2.who.name, x.o, got, want(x.target))
			}
			if got := movedTo(p); got != want(x.target) {
				t.Errorf("%s: vault #%d's moved_to is %q, want %q", c2.who.name, x.o, got, want(x.target))
			}
		}
		if got := mergeNotice(postAt(t, page, intoV)); got == nil || *got != (notice{2, want(a)}) {
			t.Errorf("%s: vault #%d's merged-from notice is %+v, want 2 from %q", c2.who.name, intoV, got, want(a))
		}
		if got := continuedIn(postAt(t, page, 6)); !slices.Equal(got, slices.DeleteFunc([]string{want(c)}, func(s string) bool { return s == "" })) {
			t.Errorf("%s: vault #6 is continued in %v", c2.who.name, got)
		}
	}

	// What annex shows: the split thread's "split from", the merged-in
	// notice, the moved notice of a's #2, and the continued thread's
	// header.
	for _, c2 := range []struct {
		who  *actor
		full bool
	}{{kim, true}, {admin, true}, {pat, false}} {
		want := func(id string) string {
			if c2.full {
				return id
			}
			return ""
		}
		if sf := w.mustView(c2.who, s).Thread.SplitFrom; sf == nil || linkID(sf.Thread) != want(v) {
			t.Errorf("%s: the split thread's split_from is %+v, want %q", c2.who.name, sf, want(v))
		}
		page := w.mustView(c2.who, a)
		if got := mergeNotice(postAt(t, page, intoA)); got == nil || *got != (notice{2, want(v)}) {
			t.Errorf("%s: annex #%d's merged-from notice is %+v, want 2 from %q", c2.who.name, intoA, got, want(v))
		}
		p := postAt(t, page, 2)
		if got := moveNotice(p); got == nil || *got != (notice{2, want(v)}) {
			t.Errorf("%s: annex #2's moved notice is %+v, want 2 to %q", c2.who.name, got, want(v))
		}
		if got := movedTo(p); got != want(v) {
			t.Errorf("%s: annex #2's moved_to is %q, want %q", c2.who.name, got, want(v))
		}
		cf := w.mustView(c2.who, c).Thread.ContinuedFrom
		switch {
		case c2.full && (cf == nil || cf.Thread.ID != v || cf.Ordinal != 6):
			t.Errorf("%s: the continued thread says it continues %+v, want vault #6", c2.who.name, cf)
		case !c2.full && cf != nil:
			t.Errorf("%s: the continued thread names what it continues: %+v", c2.who.name, cf)
		}
	}
	for _, x := range []string{s, a, c} {
		if res, _ := w.view(gail, x); res.status == http.StatusOK {
			t.Errorf("gail: annex thread %s answered 200", x)
		}
	}
	if res, _ := w.view(pat, v); res.status == http.StatusOK {
		t.Errorf("pat: vault answered 200")
	}

	// gail and kim share a viewer class: their bodies and ETags differ, and
	// each second request is a cache hit answering what the first did.
	first := map[string]answer{}
	for _, who := range []*actor{gail, kim} {
		first[who.name], _ = w.view(who, v)
	}
	if bytes.Equal(first["gail"].body, first["kim"].body) {
		t.Errorf("gail and kim are served the same vault page")
	}
	if eg, ek := first["gail"].header.Get("ETag"), first["kim"].header.Get("ETag"); eg == "" || eg == ek {
		t.Errorf("gail's ETag %q, kim's %q; want two", eg, ek)
	}
	for _, who := range []*actor{gail, kim} {
		hits := w.cacheHits()
		again, _ := w.view(who, v)
		if got := w.cacheHits(); got <= hits {
			t.Errorf("%s: the second request was not a response cache hit (%d, then %d)", who.name, hits, got)
		}
		if !bytes.Equal(again.body, first[who.name].body) || again.header.Get("ETag") != first[who.name].header.Get("ETag") {
			t.Errorf("%s: the cached answer differs from the first", who.name)
		}
	}
	p4Leaks(t, gail, "Split to annex", "Annex notices", "Continued in annex", s, a, c)
	p4Leaks(t, pat, "Vault notices", v)
}

// p4Act is one write an actor might try on a thread.
type p4Act struct {
	name, method string
	path         func(who string) string
	body         func(who string) any
	// mods is whether moderators (ada, mona and kim) may make it on a live
	// thread, and everyone whether every actor may.
	mods, everyone bool
	// also names an actor who may make it on a live thread besides those.
	also string
}

// TestPhase4ExitArchivedThreadIsReadOnlyToEveryone: ada archives a thread
// in general through the API. Then sam, its author; pat, a member; mona, a
// moderator by role; kim, a moderator by group grant; ada; and an agent by
// its credential each try every write they could make on a live thread.
// Each is refused with conflict, or forbidden where it already was, and the
// log does not change. A reply as a new thread from one of its posts goes
// through and writes only the new thread. The thread stays readable,
// watchable and in its forum's list, and after a restart on the same data
// directory a reply is still conflict (docs/15, TKT-01M3YSQQ5X: v1 has no
// unarchive).
func TestPhase4ExitArchivedThreadIsReadOnlyToEveryone(t *testing.T) {
	w := newP4World(t, false, ada, mona, sam, p3Pat, p4Kim)
	scribe := w.agent("scribe")
	// Room for every write in the table: an agent's own bucket starts
	// smaller (docs/08 "Agents").
	w.as["ada"].must(t, http.StatusOK, http.MethodPut, "/api/v1/admin/users/scribe/agent-limits",
		map[string]any{"burst": 60, "refill_seconds": 1})
	as := map[string]*actor{"sam": w.as["sam"], "pat": w.as["pat"], "mona": w.as["mona"], "kim": w.as["kim"],
		"ada": w.as["ada"], "scribe": scribe}
	order := []string{"sam", "pat", "mona", "kim", "ada", "scribe"}
	admin, author, member := as["ada"], as["sam"], as["pat"]

	// g holds a post of each actor's and one each deleted, pat's post mona
	// hid, and mona's post pat flagged. o is a live thread to merge from.
	g := author.start(t, "general", "Club history", "Sam opens the history.")
	own, gone := map[string]int{"sam": 1}, map[string]int{}
	for _, who := range order {
		if who != "sam" {
			own[who], _ = as[who].post(t, g, who+"'s own post.")
		}
		gone[who], _ = as[who].post(t, g, who+"'s post, deleted.")
		as[who].must(t, http.StatusOK, http.MethodDelete, fmt.Sprintf("/api/v1/threads/%s/posts/%d", g, gone[who]), nil)
	}
	hidden, _ := member.post(t, g, "A post mona hides.")
	as["mona"].must(t, http.StatusOK, http.MethodDelete, fmt.Sprintf("/api/v1/threads/%s/posts/%d", g, hidden), map[string]any{"reason": "Tone."})
	flagged := own["mona"]
	member.must(t, http.StatusOK, http.MethodPost, "/api/v1/threads/"+g+"/flags", map[string]any{"ordinal": flagged, "reason": "spam"})
	o := author.start(t, "general", "A live thread", "Live, first.")
	author.post(t, o, "Live, second.")

	res := admin.must(t, http.StatusOK, http.MethodPost, "/api/v1/admin/threads/"+g+"/archive", map[string]any{"reason": "Closed history."})
	var head dto.ThreadHeader
	res.decode(t, &head)
	if !head.Archived {
		t.Fatalf("the archive answered %+v", head)
	}

	post := func(n int) string { return fmt.Sprintf("/api/v1/threads/%s/posts/%d", g, n) }
	modRoute := func(action string) func(string) string {
		return func(string) string { return "/api/v1/admin/threads/" + g + "/" + action }
	}
	fixed := func(v any) func(string) any { return func(string) any { return v } }
	acts := []p4Act{
		{name: "reply", method: http.MethodPost, path: func(string) string { return "/api/v1/threads/" + g + "/posts" }, body: fixed(map[string]any{"source": "Still here."}), everyone: true},
		{name: "own edit", method: http.MethodPatch, path: func(who string) string { return post(own[who]) }, body: fixed(map[string]any{"source": "Changed."}), everyone: true},
		{name: "own delete", method: http.MethodDelete, path: func(who string) string { return post(own[who]) }, body: fixed(nil), everyone: true},
		{name: "own restore", method: http.MethodPost, path: func(who string) string { return post(gone[who]) + "/restore" }, body: fixed(nil), everyone: true},
		{name: "edit of sam's post", method: http.MethodPatch, path: func(string) string { return post(1) }, body: fixed(map[string]any{"source": "Moderated.", "reason": "Tone."}), mods: true, also: "sam"},
		{name: "hide of sam's post", method: http.MethodDelete, path: func(string) string { return post(1) }, body: fixed(map[string]any{"reason": "Tone."}), mods: true, also: "sam"},
		{name: "restore of the hidden post", method: http.MethodPost, path: func(string) string { return post(hidden) + "/restore" }, body: fixed(nil), mods: true},
		{name: "flag", method: http.MethodPost, path: func(string) string { return "/api/v1/threads/" + g + "/flags" }, body: fixed(map[string]any{"ordinal": 1, "reason": "spam"}), mods: true, also: "pat"},
		{name: "queue resolve", method: http.MethodPost, path: func(string) string { return fmt.Sprintf("/api/v1/admin/queue/posts/%s/%d", g, flagged) }, body: fixed(map[string]any{"action": "dismiss"}), mods: true},
		{name: "lock", method: http.MethodPost, path: modRoute("lock"), body: fixed(nil), mods: true},
		{name: "pin", method: http.MethodPost, path: modRoute("pin"), body: fixed(map[string]any{"scope": "forum"}), mods: true},
		{name: "move", method: http.MethodPost, path: modRoute("move"), body: fixed(map[string]any{"forum": "annex"}), mods: true},
		{name: "rename", method: http.MethodPatch, path: func(string) string { return "/api/v1/threads/" + g }, body: fixed(map[string]any{"title": "A new title"}), mods: true, also: "sam"},
		{name: "slow mode", method: http.MethodPut, path: modRoute("slow-mode"), body: fixed(map[string]any{"seconds": 30}), mods: true},
		{name: "retag", method: http.MethodPut, path: modRoute("tags"), body: fixed(map[string]any{"tags": []string{"history"}}), mods: true},
		{name: "split out of it", method: http.MethodPost, path: modRoute("split"), body: func(who string) any {
			return map[string]any{"ordinals": []int{own[who]}, "title": "Out of the archive", "forum": "general"}
		}, mods: true},
		{name: "merge out of it", method: http.MethodPost, path: modRoute("merge"), body: func(who string) any {
			return map[string]any{"ordinals": []int{own[who]}, "into": o}
		}, mods: true},
		{name: "merge into it", method: http.MethodPost, path: func(string) string { return "/api/v1/admin/threads/" + o + "/merge" }, body: fixed(map[string]any{"ordinals": []int{2}, "into": g}), mods: true},
		{name: "archive again", method: http.MethodPost, path: modRoute("archive"), body: fixed(nil), also: "ada"},
	}
	mods := []string{"ada", "mona", "kim"}
	before, live := w.logOf(g), w.logOf(o)
	conflicts := 0
	for _, act := range acts {
		for _, who := range order {
			may := act.everyone || (act.mods && slices.Contains(mods, who)) || act.also == who
			res := as[who].do(t, act.method, act.path(who), act.body(who))
			got := codeOf(res)
			switch {
			case got == "conflict":
				conflicts++
			case !may && got == "forbidden":
			default:
				t.Errorf("%s's %s on the archived thread answered %d %q, want conflict:\n%s", who, act.name, res.status, got, res)
			}
			if !bytes.Equal(w.logOf(g), before) {
				t.Fatalf("%s's %s changed the archived thread's log", who, act.name)
			}
		}
	}
	if !bytes.Equal(w.logOf(o), live) {
		t.Errorf("a refused merge changed the live thread's log")
	}
	t.Logf("%d of %d writes answered conflict", conflicts, len(acts)*len(order))

	// A reply as a new thread from one of its posts is a new thread's log
	// alone (docs/06 "Moderator flow for drift", run decision pending
	// maintainer review).
	n := w.threadFiles()
	res = member.continueAs(t, "general", "Picking up the history", "Where we left off.", g, 1)
	if res.status != http.StatusCreated {
		t.Errorf("a reply as a new thread from the archived thread answered %d:\n%s", res.status, res)
	}
	if !bytes.Equal(w.logOf(g), before) || w.threadFiles() != n+1 {
		t.Errorf("a reply as a new thread changed the archived log, or wrote no thread of its own")
	}

	// Readable, watchable and in its list, with the flag.
	for _, who := range order {
		page := w.mustView(as[who], g)
		if !page.Thread.Archived || page.Viewer.CanReply || page.Viewer.CanUpload {
			t.Errorf("%s reads the archived thread as archived %t, can reply %t, can upload %t", who,
				page.Thread.Archived, page.Viewer.CanReply, page.Viewer.CanUpload)
		}
	}
	member.must(t, http.StatusOK, http.MethodPost, "/api/v1/threads/"+g+"/watch", map[string]any{"level": "watch"})
	type row struct {
		ID       string `json:"id"`
		Archived bool   `json:"archived"`
	}
	var list struct {
		Threads []row `json:"threads"`
	}
	member.must(t, http.StatusOK, http.MethodGet, "/api/v1/forums/general/threads", nil).decode(t, &list)
	if !slices.Contains(list.Threads, row{g, true}) {
		t.Errorf("general's list does not hold the archived thread with its flag: %+v", list.Threads)
	}

	// After a restart, the actor's seal is read back: still conflict.
	w.restart()
	scribe.r = w.r
	for _, who := range []string{"pat", "ada", "scribe"} {
		res := as[who].do(t, http.MethodPost, "/api/v1/threads/"+g+"/posts", map[string]any{"source": "After the restart."})
		if got := codeOf(res); got != "conflict" {
			t.Errorf("%s's reply after a restart answered %d %q, want conflict", who, res.status, got)
		}
	}
	if !bytes.Equal(w.logOf(g), before) {
		t.Errorf("a reply after the restart changed the archived thread's log")
	}
}

// fieldCodes is a validation-failed answer's field errors, "field code".
func fieldCodes(t *testing.T, res answer) []string {
	t.Helper()
	var p struct {
		Errors []struct{ Field, Code string }
	}
	res.decode(t, &p)
	var out []string
	for _, e := range p.Errors {
		out = append(out, e.Field+" "+e.Code)
	}
	return out
}

// listIDs is a forum list's answer to a: its status, its problem code, and
// the ids it lists.
func listIDs(t *testing.T, a *actor, path string) (int, string, []string) {
	t.Helper()
	res := a.do(t, http.MethodGet, path, nil)
	if res.status != http.StatusOK {
		return res.status, codeOf(res), nil
	}
	var list struct {
		Threads []struct {
			ID string `json:"id"`
		} `json:"threads"`
	}
	res.decode(t, &list)
	var ids []string
	for _, r := range list.Threads {
		ids = append(ids, r.ID)
	}
	return res.status, "", ids
}

// TestPhase4ExitTagsFollowTheirRules: tags are folded to lowercase, kept
// once and served sorted; a member's reserved tag is refused and an
// agent's is accepted; a moderator by group grant retags, and the author and
// a moderator of another forum may not; an archived thread refuses a
// moderator's retag; and ?tag= answers a forum the viewer cannot read as the
// unfiltered list does, and lists no global pin from it (docs/15 "Decided
// by the maintainer", docs/07, docs/08).
func TestPhase4ExitTagsFollowTheirRules(t *testing.T) {
	w := newP4World(t, false, ada, sam, p3Pat, p4Kim, p3Gail)
	admin, author, member, kim, gail := w.as["ada"], w.as["sam"], w.as["pat"], w.as["kim"], w.as["gail"]
	scribe := w.agent("scribe")
	tagsOf := func(res answer) []string {
		var out struct {
			Thread dto.ThreadHeader `json:"thread"`
		}
		res.decode(t, &out)
		return out.Thread.Tags
	}

	res := author.startTagged(t, "general", "Garden notes", []string{"Garden", "long-read", "garden"})
	if res.status != http.StatusCreated {
		t.Fatalf("starting a tagged thread answered %d:\n%s", res.status, res)
	}
	g := threadIDOf(t, res, "thread")
	if got := tagsOf(res); !slices.Equal(got, []string{"garden", "long-read"}) {
		t.Errorf("the thread is tagged %q, want [garden long-read]", got)
	}
	if got := w.mustView(member, g).Thread.Tags; !slices.Equal(got, []string{"garden", "long-read"}) {
		t.Errorf("pat reads the tags %q", got)
	}

	n := w.threadFiles()
	res = member.startTagged(t, "general", "Not an agent", []string{"agent"})
	if got := fieldCodes(t, res); codeOf(res) != "validation-failed" || !slices.Equal(got, []string{"tags.0 reserved_tag"}) {
		t.Errorf("a member's reserved tag answered %d %v:\n%s", res.status, got, res)
	}
	if w.threadFiles() != n {
		t.Errorf("a refused thread was written")
	}
	res = scribe.startTagged(t, "general", "Nightly digest", []string{"agent", "digest"})
	if res.status != http.StatusCreated || !slices.Equal(tagsOf(res), []string{"agent", "digest"}) {
		t.Errorf("the agent's reserved tag answered %d:\n%s", res.status, res)
	}

	retag := func(a *actor, thread string, tags ...string) answer {
		return a.do(t, http.MethodPut, "/api/v1/admin/threads/"+thread+"/tags", map[string]any{"tags": tags})
	}
	before := w.logOf(g)
	for _, a := range []*actor{author, gail} {
		if res := retag(a, g, "weeds"); codeOf(res) != "forbidden" {
			t.Errorf("%s's retag answered %d:\n%s", a.name, res.status, res)
		}
	}
	if !bytes.Equal(w.logOf(g), before) {
		t.Errorf("a refused retag changed the log")
	}
	if res := retag(kim, g, "garden"); res.status != http.StatusOK {
		t.Errorf("kim's retag answered %d:\n%s", res.status, res)
	}
	if got := w.mustView(member, g).Thread.Tags; !slices.Equal(got, []string{"garden"}) {
		t.Errorf("after kim's retag pat reads %q, want [garden]", got)
	}

	z := admin.start(t, "general", "Archived garden", "Done.")
	admin.must(t, http.StatusOK, http.MethodPost, "/api/v1/admin/threads/"+z+"/archive", nil)
	if res := retag(kim, z, "garden"); codeOf(res) != "conflict" {
		t.Errorf("a retag of an archived thread answered %d:\n%s", res.status, res)
	}

	// A vault thread tagged garden and pinned everywhere: pat, who cannot
	// read vault, is answered its filtered list as its unfiltered one, and
	// general's filtered list holds sam's thread and not the pin.
	res = admin.startTagged(t, "vault", "Vault garden", []string{"garden"})
	if res.status != http.StatusCreated {
		t.Fatalf("starting the vault thread answered %d:\n%s", res.status, res)
	}
	secret := threadIDOf(t, res, "thread")
	admin.must(t, http.StatusOK, http.MethodPost, "/api/v1/admin/threads/"+secret+"/pin", map[string]any{"scope": "global"})
	for _, who := range []*actor{member, newActor(w.r, "anonymous")} {
		ps, pc, _ := listIDs(t, who, "/api/v1/forums/vault/threads")
		fs, fc, _ := listIDs(t, who, "/api/v1/forums/vault/threads?tag=garden")
		if ps == http.StatusOK || ps != fs || pc != fc {
			t.Errorf("%s: vault's list answered %d %q, filtered %d %q", who.name, ps, pc, fs, fc)
		}
		_, _, ids := listIDs(t, who, "/api/v1/forums/general/threads?tag=garden")
		if !slices.Equal(ids, []string{g}) {
			t.Errorf("%s: general's list filtered by garden holds %v, want only %s", who.name, ids, g)
		}
	}
	if _, _, ids := listIDs(t, kim, "/api/v1/forums/general/threads?tag=garden"); !slices.Contains(ids, secret) {
		t.Errorf("kim, who reads vault, is not listed its global pin: %v", ids)
	}
	p4Leaks(t, member, "Vault garden", secret)
}

// TestPhase4ExitMetricsServeOnlyOnTheirOwnListener: after a split, a merge,
// an archive and a retag, the main listener answers /metrics with 404; the
// metrics listener serves docs/02's families, answers 404 for anything
// else, and no label carries a thread id, a handle, a forum slug or a path,
// so the new routes appear as their patterns. A serve with metrics_listen
// unset opens no second listener.
func TestPhase4ExitMetricsServeOnlyOnTheirOwnListener(t *testing.T) {
	w := newP4World(t, false, ada, p4Kim)
	admin, kim := w.as["ada"], w.as["kim"]
	v := admin.start(t, "vault", "Metered vault", "First.")
	a := admin.start(t, "annex", "Metered annex", "First.")
	admin.post(t, v, "Second.")
	admin.post(t, v, "Third.")
	admin.post(t, a, "Second.")
	s := threadIDOf(t, kim.must(t, http.StatusOK, http.MethodPost, "/api/v1/admin/threads/"+v+"/split",
		map[string]any{"ordinals": []int{2}, "title": "Metered split", "forum": "annex"}), "thread")
	kim.must(t, http.StatusOK, http.MethodPost, "/api/v1/admin/threads/"+a+"/merge", map[string]any{"ordinals": []int{2}, "into": v})
	kim.must(t, http.StatusOK, http.MethodPut, "/api/v1/admin/threads/"+s+"/tags", map[string]any{"tags": []string{"metered"}})
	admin.must(t, http.StatusOK, http.MethodPost, "/api/v1/admin/threads/"+a+"/archive", nil)
	w.mustView(kim, v)

	if code, body := get(t, "http://"+w.r.addr+"/metrics"); code != http.StatusNotFound || strings.Contains(body, "ketju_") {
		t.Errorf("the main listener answered /metrics %d:\n%s", code, body)
	}
	if code, _ := get(t, "http://"+w.maddr+"/api/v1/forums"); code != http.StatusNotFound {
		t.Errorf("the metrics listener answered /api/v1/forums %d, want 404", code)
	}
	code, body := get(t, "http://"+w.maddr+"/metrics")
	if code != http.StatusOK {
		t.Fatalf("the metrics listener answered %d", code)
	}
	for _, family := range []string{
		"ketju_http_request_duration_seconds", "ketju_threadlog_fsync_duration_seconds", "ketju_projection_lag_bytes",
		"ketju_threads_quarantined", "ketju_cache_hits_total", "ketju_cache_misses_total", "ketju_cache_evictions_total",
		"ketju_cache_stale_total", "ketju_thread_cache_opens_total", "ketju_thread_cache_evictions_total",
		"ketju_thread_cache_open_threads", "ketju_events_queued", "ketju_events_dropped_total", "ketju_sse_streams",
		"ketju_sse_refused_total", "ketju_sse_overflowed_total", "ketju_sse_lagged_total", "ketju_goroutines", "ketju_heap_bytes",
	} {
		if !strings.Contains(body, "# TYPE "+family+" ") {
			t.Errorf("the scrape lacks %s", family)
		}
	}
	for _, route := range []string{"/api/v1/admin/threads/{id}/split", "/api/v1/admin/threads/{id}/merge",
		"/api/v1/admin/threads/{id}/tags", "/api/v1/admin/threads/{id}/archive", "/api/v1/threads/{id}"} {
		if !strings.Contains(body, `route="`+route+`"`) {
			t.Errorf("the scrape names no route %q", route)
		}
	}
	forbidden := []string{v, a, s, "ada", "kim", "vault", "annex", "general", "Metered", "?"}
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := sc.Text()
		i, j := strings.IndexByte(line, '{'), strings.LastIndexByte(line, '}')
		if strings.HasPrefix(line, "#") || i < 0 || j < i {
			continue
		}
		for _, word := range forbidden {
			if strings.Contains(line[i:j], word) {
				t.Errorf("a label carries %q: %s", word, line)
			}
		}
	}

	// Off when unset: no listener, and the main one still answers 404.
	path, _ := writeConfig(t, "")
	r := start(t, path, serveOpts{})
	if code, body := get(t, "http://"+r.addr+"/metrics"); code != http.StatusNotFound || strings.Contains(body, "ketju_") {
		t.Errorf("GET /metrics with metrics_listen unset answered %d:\n%s", code, body)
	}
	if err := r.shutdown(t); err != nil {
		t.Fatal(err)
	}
	if steps := r.steps(); slices.Contains(steps, "metrics") || strings.Contains(r.log.String(), "serving metrics") {
		t.Errorf("an unset metrics_listen started a listener: %v", steps)
	}
}

// pages is each viewer's answer for each thread, status and body, by
// viewer and thread.
func (w *p4World) pages(viewers []*actor, threads []string) map[string]string {
	w.t.Helper()
	out := map[string]string{}
	for _, a := range viewers {
		for _, th := range threads {
			res, _ := w.view(a, th)
			out[a.name+" "+th] = fmt.Sprintf("%d %s", res.status, res.body)
		}
	}
	return out
}

// TestPhase4ExitBackupRestoresAndPassesFsck: the data directory holds every
// Phase 4 write (a cross-forum split and merge, a reply as a new thread,
// tags and a retag, an archived thread and a quoting post). While a writer
// adds 40 replies to a thread in general, ketju backup writes DEST. DEST,
// copied into an empty directory with the source's config.toml and
// secrets/ as the README restores it, passes ketju fsck with exit 0; a
// serve on the copy gives kim, gail and pat the same pages as the source
// for every thread but the one being written, whose log is a prefix of the
// source's; and ketju reindex --projections --search leaves the same pages.
func TestPhase4ExitBackupRestoresAndPassesFsck(t *testing.T) {
	w := newP4World(t, true, ada, p4Kim, p3Gail, p3Pat)
	admin, kim, gail, pat := w.as["ada"], w.as["kim"], w.as["gail"], w.as["pat"]

	v := admin.start(t, "vault", "Backed-up vault", "Vault, first.")
	a := admin.start(t, "annex", "Backed-up annex", "Annex, first.")
	for i := 2; i <= 5; i++ {
		admin.post(t, v, fmt.Sprintf("Vault %d.", i))
	}
	admin.post(t, a, "Annex 2.")
	s := threadIDOf(t, kim.must(t, http.StatusOK, http.MethodPost, "/api/v1/admin/threads/"+v+"/split",
		map[string]any{"ordinals": []int{2}, "title": "Backed-up split", "forum": "annex"}), "thread")
	kim.must(t, http.StatusOK, http.MethodPost, "/api/v1/admin/threads/"+a+"/merge", map[string]any{"ordinals": []int{2}, "into": v})
	res := kim.continueAs(t, "annex", "Backed-up continuation", "Onward.", v, 3)
	if res.status != http.StatusCreated {
		t.Fatalf("the reply as a new thread answered %d:\n%s", res.status, res)
	}
	c := threadIDOf(t, res, "thread")
	kim.post(t, v, quoting(4, "Vault 4.", "Quoting the fourth."))
	res = kim.startTagged(t, "general", "Backed-up tags", []string{"garden", "long-read"})
	if res.status != http.StatusCreated {
		t.Fatalf("the tagged thread answered %d:\n%s", res.status, res)
	}
	tagged := threadIDOf(t, res, "thread")
	kim.must(t, http.StatusOK, http.MethodPut, "/api/v1/admin/threads/"+tagged+"/tags", map[string]any{"tags": []string{"garden"}})
	z := admin.start(t, "general", "Backed-up archive", "Closed.")
	admin.must(t, http.StatusOK, http.MethodPost, "/api/v1/admin/threads/"+z+"/archive", map[string]any{"reason": "History."})
	busy := kim.start(t, "general", "Busy thread", "Replies arrive during the backup.")
	threads := []string{v, a, s, c, tagged, z}
	viewers := []*actor{kim, gail, pat}

	// The writer: 40 replies, through the backup. The backup is held after
	// serve answers its snapshot until three more replies have landed, so
	// the logs it copies have grown past what the snapshot projected.
	var wrote atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		for wrote.Load() < 40 {
			res := kim.do(t, http.MethodPost, "/api/v1/threads/"+busy+"/posts", map[string]any{"source": fmt.Sprintf("Reply %d.", wrote.Load())})
			switch res.status {
			case http.StatusCreated:
				wrote.Add(1)
			case http.StatusTooManyRequests:
				time.Sleep(50 * time.Millisecond)
			default:
				t.Errorf("a reply during the backup answered %d:\n%s", res.status, res)
				return
			}
		}
	}()
	waitUntil(t, func() bool { return wrote.Load() >= 5 })
	ask := func(ctx context.Context, sock string) (backup.Answer, func(), error) {
		ans, release, err := backup.Ask(ctx, sock)
		if err == nil {
			at := wrote.Load()
			waitUntil(t, func() bool { return wrote.Load() >= min(at+3, 40) })
		}
		return ans, release, err
	}
	dest := filepath.Join(t.TempDir(), "backup")
	var out, errOut bytes.Buffer
	if err := backupMain(t.Context(), &out, &errOut, []string{"--root", w.data, "--json", dest}, backupOpts{ask: ask}); err != nil {
		t.Fatalf("ketju backup: %v (exit %d)\n%s", err, exitCodeFor(err), &errOut)
	}
	<-done
	var sum backupSummary
	if err := json.Unmarshal(out.Bytes(), &sum); err != nil {
		t.Fatalf("the report is not JSON: %v\n%s", err, &out)
	}
	t.Logf("ketju backup: %+v", sum)
	if sum.Mode != "online" || wrote.Load() != 40 {
		t.Errorf("the backup ran %q beside %d replies, want online beside 40", sum.Mode, wrote.Load())
	}
	// Each log is copied at the length the snapshot projected it to, so
	// the copy has nothing to catch up and no drift, though the source's
	// busy log had grown past it.
	if sum.CaughtUp != 0 || sum.Drift != 0 {
		t.Errorf("the copy caught up %d thread(s), with drift %d; want the logs copied at the snapshot's lengths", sum.CaughtUp, sum.Drift)
	}
	want := w.pages(viewers, threads)
	source := w.logOf(busy)

	// Restore as the README says, beside the source's config and secrets.
	got := restore(t, dest)
	mustFsck(t, got)
	if err := os.CopyFS(filepath.Join(got, "secrets"), os.DirFS(filepath.Join(w.data, "secrets"))); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.ReadFile(w.path)
	if err != nil {
		t.Fatal(err)
	}
	conf := strings.Replace(string(cfg), fmt.Sprintf("data_dir = %q", w.data), fmt.Sprintf("data_dir = %q", got), 1)
	conf = strings.Replace(conf, fmt.Sprintf("listen = %q", w.r.addr), fmt.Sprintf("listen = %q", freePort(t)), 1)
	copyPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(copyPath, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	cw := &p4World{t: t, path: copyPath, data: got, maddr: freePort(t), idp: w.idp, as: map[string]*actor{}}
	for _, a := range viewers {
		cw.as[a.name] = a
	}
	compare := func(when string) {
		t.Helper()
		pages := cw.pages(viewers, threads)
		for k, page := range want {
			if pages[k] != page {
				t.Errorf("%s, %s: the copy serves\n%.800s\nthe source served\n%.800s", when, k, pages[k], page)
			}
		}
		b := cw.logOf(busy)
		if len(b) == 0 || !bytes.HasPrefix(source, b) {
			t.Errorf("%s: the busy thread's log (%d bytes) is not a prefix of the source's (%d)", when, len(b), len(source))
		}
	}
	cw.serve()
	compare("restored")
	if err := cw.r.shutdown(t); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	var errBuf bytes.Buffer
	if err := reindexMain(t.Context(), &out, &errBuf, []string{"--root", got, "--projections", "--search"}, reindexOpts{}); err != nil {
		t.Fatalf("ketju reindex on the copy: %v\n%s%s", err, &out, &errBuf)
	}
	cw.serve()
	compare("reindexed")
}
