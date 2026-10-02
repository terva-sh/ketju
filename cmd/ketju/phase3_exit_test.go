package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/oidc/oidctest"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/store/blob"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/views"
)

// Phase 3's exit (TKT-01M3V3CFRE, docs/09 and docs/14): "daily-driver
// quality for an active community, on a phone as much as on a desktop".
// These tests prove docs/14's invariants through the real serve, as the
// binary assembles it: search, notifications, lenses, live events and blobs
// never reach a viewer who could not read the post at that moment. Each
// feature ticket proves its own invariant in its package; these prove the
// assembled server keeps them.
//
// Each test starts its own serve and builds the same world (newP3World). A
// test's expected answer comes from the viewer's effective permission, read
// from what the thread page serves that viewer, and never from a blanket
// "nothing":
//
//   - pat, a plain member, is neither the posts' author nor a moderator.
//     They get nothing for a removed post, a redacted one, one hidden
//     pending review, and a thread moved to a forum they cannot read; and
//     no more than the thread page shows them for sam's posts once sam is
//     suspended.
//   - gail moderates the restricted forum vault through a group grant
//     (docs/08 "Decided 2026-09-30"), and gets exactly what vault's thread
//     page serves her.
//   - otto moderates general through a group grant and not vault, and gets
//     pat's answer in vault.
//   - ada, an admin, moderates everything, and redacts.
//
// Five people sign in on one serve, which is the burst of ten /auth/*
// requests one address has (docs/08).

var (
	p3Pat  = seeded{subject: "pat-sub", handle: "pat", role: domain.RoleMember}
	p3Gail = seeded{subject: "gail-sub", handle: "gail", role: domain.RoleMember}
	p3Otto = seeded{subject: "otto-sub", handle: "otto", role: domain.RoleMember}
)

// p3World is one serve with the world every Phase 3 exit test reads.
type p3World struct {
	t    *testing.T
	path string
	data string
	idp  *oidctest.IdP
	r    *running

	admin, author, member, keeper, warden *actor

	// g is a thread in general, m one started in general and moved to
	// vault, and v one in vault.
	g, m, v string
	// blobs are the attachments, by name: b2 to b6 on g's posts, bm on m's
	// #2, and bv, bvr and bvh on v's #2, #3 and #4.
	blobs map[string]string
	// lastG is g's log length after its last post.
	lastG int64
}

// p3Word is a word one post or title holds, and whether it is the post's
// text now: obsidian is #1's first revision, which its edit replaced.
type p3Word struct {
	thread  func(w *p3World) string
	ordinal int
	current bool
}

// p3Words are the words the world writes, each in exactly one place.
var p3Words = map[string]p3Word{
	"feldspar":      {func(w *p3World) string { return w.g }, 1, true},
	"obsidian":      {func(w *p3World) string { return w.g }, 1, false},
	"removedword":   {func(w *p3World) string { return w.g }, 2, true},
	"hiddenword":    {func(w *p3World) string { return w.g }, 3, true},
	"redactedword":  {func(w *p3World) string { return w.g }, 4, true},
	"suspendedword": {func(w *p3World) string { return w.g }, 5, true},
	"plainword":     {func(w *p3World) string { return w.g }, 6, true},
	"movedtitle":    {func(w *p3World) string { return w.m }, 0, true},
	"movedword":     {func(w *p3World) string { return w.m }, 1, true},
	"vaulttitle":    {func(w *p3World) string { return w.v }, 0, true},
	"vaultword":     {func(w *p3World) string { return w.v }, 2, true},
	"vaultremoved":  {func(w *p3World) string { return w.v }, 3, true},
	"vaulthidden":   {func(w *p3World) string { return w.v }, 4, true},
}

// seedVault writes, beside seedPeople's general, the restricted forum vault;
// the group keepers, granted everything in vault alone, with gail in it; the
// group wardens, granted everything in general alone, with otto in it; and
// upload for every member everywhere they may post, which no migration
// grants (docs/07 "Attachments").
func seedVault(t *testing.T, data string) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, db.Options{Path: filepath.Join(data, "db", "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	now := time.Now().UnixMilli()
	var vault, general, keepers, wardens, members, gail, otto int64
	if err := d.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		if vault, err = q.CreateForum(ctx, dbq.CreateForumParams{Slug: "vault", Name: "Vault",
			Description: "Keepers only", Visibility: domain.VisibilityRestricted, Position: 1,
			CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		f, err := q.GetForumBySlug(ctx, "general")
		if err != nil {
			return err
		}
		general = f.ID
		if keepers, err = q.CreateGroup(ctx, dbq.CreateGroupParams{Name: "Keepers", Slug: "keepers",
			CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		if wardens, err = q.CreateGroup(ctx, dbq.CreateGroupParams{Name: "Wardens", Slug: "wardens",
			CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		g, err := q.GetGroupBySlug(ctx, domain.GroupMembers)
		if err != nil {
			return err
		}
		members = g.ID
		for h, id := range map[string]*int64{"gail": &gail, "otto": &otto} {
			u, err := q.GetUserByHandle(ctx, h)
			if err != nil {
				return err
			}
			*id = u.ID
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	e := perm.New(d)
	by := audit.System("phase 3 exit seed")
	for _, p := range domain.AllPermissions {
		if err := e.Grant(ctx, by, vault, keepers, p); err != nil {
			t.Fatal(err)
		}
		if err := e.Grant(ctx, by, general, wardens, p); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Grant(ctx, by, 0, members, domain.PermUpload); err != nil {
		t.Fatal(err)
	}
	if err := e.AddMember(ctx, by, keepers, gail); err != nil {
		t.Fatal(err)
	}
	if err := e.AddMember(ctx, by, wardens, otto); err != nil {
		t.Fatal(err)
	}
}

// pngOf is a 24 by 16 PNG with tag after its IEND, which a decoder ignores,
// so each attachment is a blob of its own.
func pngOf(t *testing.T, tag string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 24, 16))
	for x := range 24 {
		for y := range 16 {
			img.Set(x, y, color.RGBA{uint8(x * 10), uint8(y * 15), 120, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	b.WriteString(tag)
	return b.Bytes()
}

// send sends req with a's cookies, and keeps those the answer sets.
func (a *actor) send(t *testing.T, req *http.Request) answer {
	t.Helper()
	for _, c := range a.cookies {
		req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	resp, err := noRedirects.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	a.keep(resp.Cookies())
	a.bodies = append(a.bodies, string(b))
	return answer{status: resp.StatusCode, header: resp.Header, cookies: resp.Cookies(), body: b}
}

// upload sends body as a PNG named name to POST /api/v1/blobs for thread.
func (a *actor) upload(t *testing.T, thread, name string, body []byte) answer {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, name))
	h.Set("Content-Type", "image/png")
	part, err := mw.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(body)
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, "http://"+a.r.addr+"/api/v1/blobs?thread="+thread, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return a.send(t, req)
}

// attach uploads a PNG of its own to thread and answers its hash.
func (a *actor) attach(t *testing.T, thread, tag string) string {
	t.Helper()
	res := a.upload(t, thread, tag+".png", pngOf(t, tag))
	var out struct {
		Hash string `json:"hash"`
	}
	if res.status == http.StatusCreated {
		res.decode(t, &out)
	}
	if res.status != http.StatusCreated || out.Hash == "" {
		t.Fatalf("%s: uploading %s answered %d:\n%s", a.name, tag, res.status, res)
	}
	return out.Hash
}

// post replies source to thread as a, with attachments, and answers its
// ordinal and the log length after it.
func (a *actor) post(t *testing.T, thread, source string, attachments ...string) (int, int64) {
	t.Helper()
	body := map[string]any{"source": source}
	if len(attachments) > 0 {
		body["attachments"] = attachments
	}
	res := a.do(t, http.MethodPost, "/api/v1/threads/"+thread+"/posts", body)
	if res.status != http.StatusCreated {
		t.Fatalf("%s: posting to %s answered %d:\n%s", a.name, thread, res.status, res)
	}
	return created(t, res)
}

// start starts a thread in forum as a and answers its id.
func (a *actor) start(t *testing.T, forum, title, source string) string {
	t.Helper()
	res := a.do(t, http.MethodPost, "/api/v1/forums/"+forum+"/threads",
		map[string]any{"title": title, "source": source, "tags": []string{}})
	if res.status != http.StatusCreated {
		t.Fatalf("%s: starting a thread in %s answered %d:\n%s", a.name, forum, res.status, res)
	}
	var out struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	res.decode(t, &out)
	return out.Thread.ID
}

// must sends method path body as a and fails unless it answers want.
func (a *actor) must(t *testing.T, want int, method, path string, body any) answer {
	t.Helper()
	res := a.do(t, method, path, body)
	if res.status != want {
		t.Fatalf("%s: %s %s answered %d, want %d:\n%s", a.name, method, path, res.status, want, res)
	}
	return res
}

// newP3World starts serve, signs the five people in, and writes the world:
//
//	g, in general, by sam: #1 "obsidian" edited to "feldspar"; #2 to #5
//	  mention pat, each with an image, and are then removed by otto (#2),
//	  hidden pending review by pat's flag (#3), redacted (#4) and left
//	  standing while sam is suspended (#5); #6, by ada, mentions pat, gail
//	  and otto, with an image.
//	m, in general, by sam: #1 mentions pat, #2 has an image; pat and gail
//	  watch it; ada moves it to vault; #3, by ada, follows the move.
//	v, in vault, by ada: #2 mentions gail, pat and otto, with an image; #3
//	  mentions gail and is removed by gail; #4 is hidden pending review by
//	  gail's flag. #3 and #4 have images.
//
// Then sam is suspended, serve is stopped, g's #4 is redacted, and serve is
// started again on the same data, with every session still standing.
func newP3World(t *testing.T) *p3World {
	t.Helper()
	w := &p3World{t: t, blobs: map[string]string{}}
	w.idp = oidctest.New(t)
	w.idp.Users = []string{ada.subject, sam.subject, p3Pat.subject, p3Gail.subject, p3Otto.subject}
	w.path, w.data = writeConfig(t, providerBlock(w.idp.URL)+"[limits]\nattachment_mb = 1\n")
	seedPeople(t, w.data, w.idp.URL, ada, sam, p3Pat, p3Gail, p3Otto)
	seedVault(t, w.data)
	w.r = start(t, w.path, serveOpts{idpClient: w.idp.Client(), getenv: siteURL})
	s := &exitServer{r: w.r, idp: w.idp}
	w.admin, w.author, w.member, w.keeper, w.warden = s.member(t, ada), s.member(t, sam), s.member(t, p3Pat),
		s.member(t, p3Gail), s.member(t, p3Otto)

	// One flag hides a post pending review (docs/08 "Flags").
	w.admin.must(t, http.StatusOK, http.MethodPut, "/api/v1/admin/settings/flags.auto_hide_threshold", map[string]any{"value": 1})

	w.g = w.author.start(t, "general", "Rock club notes", "Opening words about obsidian.")
	w.author.must(t, http.StatusOK, http.MethodPatch, "/api/v1/threads/"+w.g+"/posts/1",
		map[string]any{"source": "Opening words about feldspar."})
	for i, word := range []string{"removedword", "hiddenword", "redactedword", "suspendedword"} {
		name := fmt.Sprintf("b%d", i+2)
		w.blobs[name] = w.author.attach(t, w.g, name)
		w.author.post(t, w.g, "@pat, "+word+" here.", w.blobs[name])
	}

	w.m = w.author.start(t, "general", "Trip plans movedtitle", "@pat, movedword for the trip.")
	w.blobs["bm"] = w.author.attach(t, w.m, "bm")
	w.author.post(t, w.m, "The map for the trip.", w.blobs["bm"])
	for _, a := range []*actor{w.member, w.keeper} {
		a.must(t, http.StatusOK, http.MethodPost, "/api/v1/threads/"+w.m+"/watch", map[string]any{"level": "watch"})
	}

	w.v = w.admin.start(t, "vault", "Keepers notes vaulttitle", "The keepers' thread.")
	for _, c := range []struct{ name, source string }{
		{"bv", "@gail @pat @otto, vaultword."},
		{"bvr", "@gail, vaultremoved."},
		{"bvh", "Something vaulthidden."},
	} {
		w.blobs[c.name] = w.admin.attach(t, w.v, c.name)
		w.admin.post(t, w.v, c.source, w.blobs[c.name])
	}

	// The moderation: otto removes g's #2, as general's warden; pat's flag
	// hides #3; gail removes v's #3 and her flag hides #4, as vault's keeper.
	w.warden.must(t, http.StatusOK, http.MethodDelete, "/api/v1/threads/"+w.g+"/posts/2", map[string]any{"reason": "Off topic."})
	w.member.must(t, http.StatusOK, http.MethodPost, "/api/v1/threads/"+w.g+"/flags", map[string]any{"ordinal": 3, "reason": "spam"})
	w.keeper.must(t, http.StatusOK, http.MethodDelete, "/api/v1/threads/"+w.v+"/posts/3", map[string]any{"reason": "Kept back."})
	w.keeper.must(t, http.StatusOK, http.MethodPost, "/api/v1/threads/"+w.v+"/flags", map[string]any{"ordinal": 4, "reason": "spam"})

	w.admin.must(t, http.StatusOK, http.MethodPost, "/api/v1/admin/threads/"+w.m+"/move", map[string]any{"forum": "vault"})
	w.admin.post(t, w.m, "After the move.")
	w.blobs["b6"] = w.admin.attach(t, w.g, "b6")
	_, w.lastG = w.admin.post(t, w.g, "@pat @gail @otto, plainword.", w.blobs["b6"])

	w.admin.must(t, http.StatusNoContent, http.MethodPost, "/api/v1/admin/users/sam/suspend", map[string]any{"reason": "A week off."})

	// Redaction rewrites the log, so serve is stopped for it (docs/03 "Hard
	// redaction").
	if err := w.r.shutdown(t); err != nil {
		t.Fatalf("stopping serve for the redaction: %v\n%s", err, w.r.log)
	}
	var out, errOut bytes.Buffer
	if err := redactMain(t.Context(), &out, &errOut, []string{"--root", w.data, "--operator", "ada",
		"--reason", "court order 17", w.g, "4"}, redactOpts{}); err != nil {
		t.Fatalf("redacting g #4: %v\n%s%s", err, &out, &errOut)
	}
	w.r = start(t, w.path, serveOpts{idpClient: w.idp.Client(), getenv: siteURL})
	for _, a := range []*actor{w.admin, w.author, w.member, w.keeper, w.warden} {
		a.r = w.r
	}
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(w.r.log.String(), "search catch-up finished") {
		if time.Now().After(deadline) {
			t.Fatalf("the search catch-up never finished:\n%s", w.r.log)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The world is as described: the redaction holds, and every removal is
	// the kind it should be, as the admin's thread page shows them.
	want := map[string]map[int]string{
		w.g: {2: "moderator", 3: "review", 4: "redacted"},
		w.v: {3: "moderator", 4: "review"},
	}
	for thread, posts := range want {
		_, page := w.page(w.admin, thread)
		for ordinal, kind := range posts {
			p, ok := page[ordinal]
			got := ""
			switch {
			case !ok:
			case p.Redacted:
				got = "redacted"
			case p.Removal != nil:
				got = *p.Removal
			}
			if got != kind {
				t.Fatalf("thread %s #%d is %q to the admin, want %q: %+v", thread, ordinal, got, kind, p)
			}
		}
	}
	return w
}

// p3Post is what these tests read of a post.
type p3Post struct {
	Ordinal int `json:"ordinal"`
	Author  struct {
		Handle string `json:"handle"`
	} `json:"author"`
	HTML        string  `json:"html"`
	Deleted     bool    `json:"deleted"`
	Removal     *string `json:"removal"`
	Redacted    bool    `json:"redacted"`
	Attachments []struct {
		Hash string `json:"hash"`
	} `json:"attachments"`
}

// asItself is whether the post is shown as itself: not removed, moved or
// redacted, with its body.
func (p p3Post) asItself() bool { return !p.Deleted && p.HTML != "" }

// hashes is the post's attachments' hashes.
func (p p3Post) hashes() []string {
	var out []string
	for _, a := range p.Attachments {
		out = append(out, a.Hash)
	}
	return out
}

// page is thread's first page as a reads it: its status, and its posts by
// ordinal. Every thread here fits on one page.
func (w *p3World) page(a *actor, thread string) (int, map[int]p3Post) {
	w.t.Helper()
	return w.lensPage(a, thread, "")
}

// lensPage is page through lens, "" for none.
func (w *p3World) lensPage(a *actor, thread, lens string) (int, map[int]p3Post) {
	w.t.Helper()
	path := "/api/v1/threads/" + thread + "?page=1"
	if lens != "" {
		path += "&lens=" + lens
	}
	res := a.do(w.t, http.MethodGet, path, nil)
	if res.status != http.StatusOK {
		return res.status, nil
	}
	var page struct {
		Posts []p3Post `json:"posts"`
	}
	res.decode(w.t, &page)
	out := map[int]p3Post{}
	for _, p := range page.Posts {
		out[p.Ordinal] = p
	}
	return res.status, out
}

// title is the thread's title as a's thread page serves it, "" when the page
// is refused.
func (w *p3World) title(a *actor, thread string) string {
	w.t.Helper()
	res := a.do(w.t, http.MethodGet, "/api/v1/threads/"+thread+"?page=1", nil)
	if res.status != http.StatusOK {
		return ""
	}
	var page struct {
		Thread struct {
			Title string `json:"title"`
		} `json:"thread"`
	}
	res.decode(w.t, &page)
	return page.Thread.Title
}

// shownAsItself is whether a's thread page shows thread's post ordinal as
// itself, or for ordinal 0 whether it serves the thread at all.
func (w *p3World) shownAsItself(a *actor, thread string, ordinal int) bool {
	w.t.Helper()
	if ordinal == 0 {
		return w.title(a, thread) != ""
	}
	_, page := w.page(a, thread)
	p, ok := page[ordinal]
	return ok && p.asItself()
}

// viewers are the people who still hold a session, with an anonymous reader.
func (w *p3World) viewers() []*actor {
	return []*actor{w.member, w.warden, w.keeper, w.admin, newActor(w.r, "anonymous")}
}

// indexHits is how many rows the search index holds for word in any forum,
// read past every permission, so a test can tell a filtered answer from an
// index that never had the word.
func (w *p3World) indexHits(word string) int {
	w.t.Helper()
	var n int
	d, err := db.Open(w.t.Context(), db.Options{Path: filepath.Join(w.data, "db", "ketju.db")})
	if err != nil {
		w.t.Fatal(err)
	}
	defer d.Close()
	if err := d.Read(w.t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		forums, err := q.ListForums(w.t.Context())
		if err != nil {
			return err
		}
		var ids []int64
		for _, f := range forums {
			ids = append(ids, f.ID)
		}
		rows, err := views.Search(w.t.Context(), tx, views.SearchParams{
			Match: views.MatchAll([]string{word}), Forums: ids, Limit: 25})
		n = len(rows)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// p3Result is what these tests read of a search result.
type p3Result struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
	Ordinal int `json:"ordinal"`
	Author  struct {
		Handle string `json:"handle"`
	} `json:"author"`
	Snippet []struct {
		Text  string `json:"text"`
		Match bool   `json:"match"`
	} `json:"snippet"`
}

func (r p3Result) key() string { return fmt.Sprintf("%s#%d", r.Thread.ID, r.Ordinal) }

// search is a's results for q.
func (w *p3World) search(a *actor, q string) []p3Result {
	w.t.Helper()
	res := a.do(w.t, http.MethodGet, "/api/v1/search?q="+q, nil)
	if res.status != http.StatusOK {
		w.t.Fatalf("%s: searching %q answered %d:\n%s", a.name, q, res.status, res)
	}
	var out struct {
		Results []p3Result `json:"results"`
	}
	res.decode(w.t, &out)
	return out.Results
}

// A search finds a post only for a viewer whose thread page shows it as
// itself, in a forum they may read, at the text it has now; and no snippet
// carries a redacted body or a superseded revision, whoever asks.
func TestPhase3ExitSearchNeverWidensAccess(t *testing.T) {
	w := newP3World(t)

	// The index holds the moved thread and the restricted forum's posts, so
	// an empty answer for them below is the served filter's doing. A post
	// removed, hidden or redacted is not Visible, and the indexer drops its
	// row (internal/events/search decisions), so the redaction took its
	// words out with the rest.
	for word, n := range map[string]int{
		"movedword": 1, "movedtitle": 1, "vaultword": 1, "vaulttitle": 1,
		"removedword": 0, "hiddenword": 0, "redactedword": 0, "vaultremoved": 0, "vaulthidden": 0,
	} {
		if got := w.indexHits(word); got != n {
			t.Errorf("the index holds %d rows for %q, want %d", got, word, n)
		}
	}

	// Suspension does not by itself remove posts: the page decides what
	// sam's post is to pat, and the search follows it.
	_, page := w.page(w.member, w.g)
	t.Logf("g #5 by the suspended sam, as pat's thread page serves it: shown as itself %t, author @%s",
		page[5].asItself(), page[5].Author.Handle)

	for _, v := range w.viewers() {
		for word, at := range p3Words {
			thread := at.thread(w)
			var want []string
			if at.current && w.shownAsItself(v, thread, at.ordinal) {
				want = []string{fmt.Sprintf("%s#%d", thread, at.ordinal)}
			}
			results := w.search(v, word)
			var got []string
			for _, r := range results {
				got = append(got, r.key())
				for _, part := range r.Snippet {
					for _, gone := range []string{"obsidian", "redactedword"} {
						if strings.Contains(strings.ToLower(part.Text), gone) {
							t.Errorf("%s: a snippet for %q carries %q: %+v", v.name, word, gone, r.Snippet)
						}
					}
				}
				if r.Ordinal > 0 {
					_, page := w.page(v, r.Thread.ID)
					if p := page[r.Ordinal]; p.Author.Handle != r.Author.Handle {
						t.Errorf("%s: %q's result names @%s, the thread page @%s", v.name, word, r.Author.Handle, p.Author.Handle)
					}
				}
				if !slices.ContainsFunc(r.Snippet, func(p struct {
					Text  string `json:"text"`
					Match bool   `json:"match"`
				}) bool {
					return p.Match && strings.EqualFold(p.Text, word)
				}) {
					t.Errorf("%s: %q's snippet does not mark the word: %+v", v.name, word, r.Snippet)
				}
			}
			if !slices.Equal(got, want) {
				t.Errorf("%s: searching %q found %v, want %v, what the thread page shows them", v.name, word, got, want)
			}
		}
	}

	// The expected answers above are the thread page's; these pin what
	// that page is, so a page that widened would not pass with it.
	for _, c := range []struct {
		who   *actor
		word  string
		found bool
	}{
		{w.member, "feldspar", true}, {w.member, "plainword", true},
		{w.member, "vaultword", false}, {w.member, "vaulttitle", false},
		{w.member, "movedword", false}, {w.member, "movedtitle", false},
		{w.member, "removedword", false}, {w.member, "hiddenword", false},
		{w.warden, "vaultword", false}, {w.warden, "movedword", false},
		{w.keeper, "vaultword", true}, {w.keeper, "vaulttitle", true}, {w.keeper, "movedword", true},
		{w.admin, "vaultword", true},
	} {
		if found := len(w.search(c.who, c.word)) > 0; found != c.found {
			t.Errorf("%s: searching %q found something: %t, want %t", c.who.name, c.word, found, c.found)
		}
	}
	for _, v := range w.viewers() {
		for _, word := range []string{"obsidian", "redactedword", "removedword", "hiddenword", "vaultremoved", "vaulthidden"} {
			if n := len(w.search(v, word)); n != 0 {
				t.Errorf("%s: searching %q found %d results, want none", v.name, word, n)
			}
		}
	}
}

// p3Note is what these tests read of a notification.
type p3Note struct {
	Kind   string `json:"kind"`
	Thread *struct {
		ID string `json:"id"`
	} `json:"thread"`
	Ordinal *int `json:"ordinal"`
	Read    bool `json:"read"`
}

func (n p3Note) key() string {
	if n.Thread == nil || n.Ordinal == nil {
		return n.Kind
	}
	return fmt.Sprintf("%s %s#%d", n.Kind, n.Thread.ID, *n.Ordinal)
}

// notes is a's notification list, sorted.
func (w *p3World) notes(a *actor) []string {
	w.t.Helper()
	res := a.do(w.t, http.MethodGet, "/api/v1/notifications", nil)
	if res.status != http.StatusOK {
		w.t.Fatalf("%s: the notifications answered %d:\n%s", a.name, res.status, res)
	}
	var out struct {
		Notifications []p3Note `json:"notifications"`
	}
	res.decode(w.t, &out)
	var keys []string
	for _, n := range out.Notifications {
		keys = append(keys, n.key())
	}
	sort.Strings(keys)
	return keys
}

// A mention or a watch is listed and counted only for a recipient whose
// thread page shows the post as itself when it is served.
func TestPhase3ExitNotificationsNeverWidenAccess(t *testing.T) {
	w := newP3World(t)

	// Everything each person could be told of, by kind: the mentions of
	// pat on g and m and in vault, of gail and otto in vault and on g, and
	// the watched m's post after its move.
	type cause struct {
		kind    string
		thread  string
		ordinal int
	}
	causes := map[*actor][]cause{
		w.member: {{"mention", w.g, 2}, {"mention", w.g, 3}, {"mention", w.g, 4}, {"mention", w.g, 5},
			{"mention", w.g, 6}, {"mention", w.m, 1}, {"mention", w.v, 2}, {"thread_you_watch", w.m, 3}},
		w.keeper: {{"mention", w.v, 2}, {"mention", w.v, 3}, {"mention", w.g, 6}, {"thread_you_watch", w.m, 3}},
		w.warden: {{"mention", w.v, 2}, {"mention", w.g, 6}},
	}
	want := map[*actor][]string{}
	for a, cs := range causes {
		for _, c := range cs {
			if w.shownAsItself(a, c.thread, c.ordinal) {
				want[a] = append(want[a], fmt.Sprintf("%s %s#%d", c.kind, c.thread, c.ordinal))
			}
		}
		sort.Strings(want[a])
	}
	// Generation follows the bus, so wait for each list to fill.
	deadline := time.Now().Add(20 * time.Second)
	for a, keys := range want {
		for !slices.Equal(w.notes(a), keys) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if got := w.notes(a); !slices.Equal(got, keys) {
			t.Errorf("%s is listed %v, want %v, what their thread pages show", a.name, got, keys)
		}
		if m := a.me(t); m.Notifications != len(keys) {
			t.Errorf("%s's badge counts %d, want the list's %d", a.name, m.Notifications, len(keys))
		}
	}

	// What those pages are: pat hears of the mention ada left in plain
	// view, and of none in a removed, hidden, redacted or moved post, nor in
	// vault; gail hears of her mention in vault and of the watched thread
	// that moved in, and not of the post she removed; otto, who does not
	// keep vault, hears nothing from it.
	has := func(a *actor, key string) bool { return slices.Contains(w.notes(a), key) }
	for _, c := range []struct {
		who  *actor
		key  string
		want bool
	}{
		{w.member, "mention " + w.g + "#6", true},
		{w.member, "mention " + w.g + "#2", false}, {w.member, "mention " + w.g + "#3", false},
		{w.member, "mention " + w.g + "#4", false}, {w.member, "mention " + w.m + "#1", false},
		{w.member, "mention " + w.v + "#2", false}, {w.member, "thread_you_watch " + w.m + "#3", false},
		{w.keeper, "mention " + w.v + "#2", true}, {w.keeper, "thread_you_watch " + w.m + "#3", true},
		{w.keeper, "mention " + w.v + "#3", false},
		{w.warden, "mention " + w.v + "#2", false}, {w.warden, "mention " + w.g + "#6", true},
	} {
		if got := has(c.who, c.key); got != c.want {
			t.Errorf("%s is listed %q: %t, want %t", c.who.name, c.key, got, c.want)
		}
	}
	// Nothing about a post they may not read reached them in any answer.
	for _, a := range []*actor{w.member, w.warden} {
		for _, b := range a.bodies {
			if strings.Contains(b, "vaulttitle") || strings.Contains(b, "movedtitle") {
				t.Errorf("an answer to %s names a thread in vault:\n%s", a.name, b)
			}
		}
	}
}

// The mentions lens and the moderator lens follow the thread page's rule:
// each post a lens serves is the post the canonical page serves the same
// viewer, the moderator lens is refused to a viewer who may not moderate the
// forum, and it serves vault's keeper its tombstones with their reasons.
func TestPhase3ExitLensesNeverWidenAccess(t *testing.T) {
	w := newP3World(t)

	// The mentions projection follows the bus: wait for ada's mention of
	// pat, the last post written to g.
	deadline := time.Now().Add(20 * time.Second)
	for {
		_, lens := w.lensPage(w.member, w.g, "mentions")
		if _, ok := lens[6]; ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pat's mentions lens never reached g #6: %v", lens)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// A lens post is the canonical page's post, for the same viewer.
	same := func(a *actor, thread, lens string) map[int]p3Post {
		t.Helper()
		status, got := w.lensPage(a, thread, lens)
		pageStatus, page := w.page(a, thread)
		if status != pageStatus && status == http.StatusOK {
			t.Errorf("%s: the %s lens of %s answered 200, the page %d", a.name, lens, thread, pageStatus)
		}
		for o, p := range got {
			if q, ok := page[o]; !ok || p.HTML != q.HTML || p.Deleted != q.Deleted || p.Redacted != q.Redacted ||
				!slices.Equal(p.hashes(), q.hashes()) {
				t.Errorf("%s: the %s lens of %s serves #%d as %+v, the page as %+v", a.name, lens, thread, o, p, q)
			}
		}
		return got
	}
	ordinals := func(m map[int]p3Post) []int {
		var out []int
		for o := range m {
			out = append(out, o)
		}
		sort.Ints(out)
		return out
	}

	// Mentions: pat's on g are the posts that mention them and the page
	// shows as themselves; the removed, hidden and redacted ones are left
	// out. vault and the moved thread are hidden from pat altogether.
	want := []int{6}
	if w.shownAsItself(w.member, w.g, 5) {
		want = []int{5, 6}
	}
	if got := ordinals(same(w.member, w.g, "mentions")); !slices.Equal(got, want) {
		t.Errorf("pat's mentions lens on g is %v, want %v", got, want)
	}
	if got := ordinals(same(w.keeper, w.v, "mentions")); !slices.Equal(got, []int{2}) {
		t.Errorf("gail's mentions lens on vault is %v, want [2]: her removed #3 is left out", got)
	}
	for _, a := range []*actor{w.member, w.warden} {
		for _, thread := range []string{w.v, w.m} {
			for _, lens := range []string{"mentions", "mod"} {
				if status, _ := w.lensPage(a, thread, lens); status != http.StatusNotFound {
					t.Errorf("%s: the %s lens of a vault thread answered %d, want 404", a.name, lens, status)
				}
			}
		}
	}

	// Moderator: refused to pat on g and to gail, who does not keep
	// general; served to otto on g and to gail on vault as each one's page
	// draws the removed posts, bodies included where that page has them.
	for _, c := range []struct {
		who    *actor
		thread string
	}{{w.member, w.g}, {w.keeper, w.g}} {
		if status, _ := w.lensPage(c.who, c.thread, "mod"); status != http.StatusForbidden {
			t.Errorf("%s: the moderator lens of %s answered %d, want 403", c.who.name, c.thread, status)
		}
	}
	for _, c := range []struct {
		who     *actor
		thread  string
		removal map[int]string
		bodies  []int
	}{
		{w.warden, w.g, map[int]string{2: "moderator", 3: "review", 4: "redacted"}, []int{2, 3}},
		{w.admin, w.g, map[int]string{2: "moderator", 3: "review", 4: "redacted"}, []int{2, 3}},
		{w.keeper, w.v, map[int]string{3: "moderator", 4: "review"}, []int{3, 4}},
	} {
		got := same(c.who, c.thread, "mod")
		var gotOrd, wantOrd []int
		for o := range c.removal {
			wantOrd = append(wantOrd, o)
		}
		sort.Ints(wantOrd)
		gotOrd = ordinals(got)
		if !slices.Equal(gotOrd, wantOrd) {
			t.Errorf("%s: the moderator lens of %s holds %v, want %v", c.who.name, c.thread, gotOrd, wantOrd)
			continue
		}
		for o, kind := range c.removal {
			p := got[o]
			reason := ""
			if p.Removal != nil {
				reason = *p.Removal
			}
			if kind == "redacted" {
				if !p.Redacted || p.HTML != "" || len(p.Attachments) != 0 {
					t.Errorf("%s: the moderator lens serves the redacted #%d as %+v", c.who.name, o, p)
				}
				continue
			}
			if reason != kind {
				t.Errorf("%s: the moderator lens gives #%d the reason %q, want %q", c.who.name, o, reason, kind)
			}
			if slices.Contains(c.bodies, o) && p.HTML == "" {
				t.Errorf("%s: the moderator lens serves #%d without its body", c.who.name, o)
			}
		}
	}
	// No lens anyone was served carries the redacted body.
	for _, a := range w.viewers() {
		for _, b := range a.bodies {
			if strings.Contains(b, "redactedword") {
				t.Errorf("an answer to %s carries the redacted body:\n%s", a.name, b)
			}
		}
	}
}

// sseEvent is one event a stream sent.
type sseEvent struct{ name, data string }

// p3Stream is one GET /events, read as it arrives.
type p3Stream struct {
	who    string
	status int
	events chan sseEvent // closed when the stream ends
	body   io.Closer
}

// stream opens GET /events?query as a. A refused stream has no events.
func (a *actor) stream(t *testing.T, query string) *p3Stream {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+a.r.addr+"/events?"+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range a.cookies {
		req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	s := &p3Stream{who: a.name + " " + query, status: resp.StatusCode, events: make(chan sseEvent, 64), body: resp.Body}
	t.Cleanup(func() { resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		close(s.events)
		return s
	}
	go func() {
		defer close(s.events)
		sc := bufio.NewScanner(resp.Body)
		var ev sseEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if ev.name != "" {
					s.events <- ev
				}
				ev = sseEvent{}
			case strings.HasPrefix(line, "event: "):
				ev.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				ev.data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return s
}

// hint is a thread event's payload, which must carry nothing else.
type hint struct {
	ThreadID  string `json:"thread_id"`
	LogLength int64  `json:"log_length"`
}

func (s *p3Stream) parse(t *testing.T, ev sseEvent) hint {
	t.Helper()
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(ev.data), &keys); err != nil {
		t.Fatalf("%s: a thread event's data %q: %v", s.who, ev.data, err)
	}
	if len(keys) != 2 || keys["thread_id"] == nil || keys["log_length"] == nil {
		t.Errorf("%s: a hint carries more than the thread and its length: %s", s.who, ev.data)
	}
	var h hint
	json.Unmarshal([]byte(ev.data), &h)
	return h
}

// until reads s until a hint for thread at length n or later, and fails on
// any hint forbid matches before it.
func (s *p3Stream) until(t *testing.T, thread string, n int64, forbid func(hint) bool) {
	t.Helper()
	timeout := time.After(15 * time.Second)
	for {
		select {
		case ev, ok := <-s.events:
			if !ok {
				t.Fatalf("%s: the stream ended before a hint for %s at %d", s.who, thread, n)
			}
			if ev.name != "thread" {
				continue
			}
			h := s.parse(t, ev)
			if forbid != nil && forbid(h) {
				t.Errorf("%s: sent a hint it may not have: %+v", s.who, h)
			}
			if h.ThreadID == thread && h.LogLength >= n {
				return
			}
		case <-timeout:
			t.Fatalf("%s: no hint for %s at %d", s.who, thread, n)
		}
	}
}

// ends reads s until it ends, and fails on any hint forbid matches.
func (s *p3Stream) ends(t *testing.T, forbid func(hint) bool) {
	t.Helper()
	timeout := time.After(15 * time.Second)
	for {
		select {
		case ev, ok := <-s.events:
			if !ok {
				return
			}
			if ev.name == "thread" {
				if h := s.parse(t, ev); forbid(h) {
					t.Errorf("%s: sent a hint it may not have: %+v", s.who, h)
				}
			}
		case <-timeout:
			t.Fatalf("%s: the stream did not end", s.who)
		}
	}
}

// ready waits for the stream's ready event.
func (s *p3Stream) ready(t *testing.T) {
	t.Helper()
	if s.status != http.StatusOK {
		t.Fatalf("%s: the stream answered %d", s.who, s.status)
	}
	timeout := time.After(15 * time.Second)
	for {
		select {
		case ev, ok := <-s.events:
			if !ok {
				t.Fatalf("%s: the stream ended before ready", s.who)
			}
			if ev.name == "ready" {
				return
			}
		case <-timeout:
			t.Fatalf("%s: no ready", s.who)
		}
	}
}

// A live-update stream is admitted only where the thread page would admit
// its viewer, sends a hint only for a thread they may read, carrying no more
// than the thread and its length, and stops when the viewer's session ends
// or the thread leaves what they may read.
func TestPhase3ExitLiveNeverWidensAccess(t *testing.T) {
	w := newP3World(t)

	// Admission is the thread page's.
	anon := newActor(w.r, "anonymous")
	for _, c := range []struct {
		who   *actor
		query string
	}{
		{w.member, "threads=" + w.v}, {w.member, "threads=" + w.m}, {w.member, "forums=vault"},
		{w.warden, "threads=" + w.v}, {w.warden, "forums=vault"}, {anon, "threads=" + w.v},
	} {
		status, _ := w.page(c.who, w.v)
		if strings.HasPrefix(c.query, "threads="+w.m) {
			status, _ = w.page(c.who, w.m)
		}
		s := c.who.stream(t, c.query)
		if s.status == http.StatusOK || status == http.StatusOK {
			t.Errorf("%s: a stream of %s answered %d and the page %d; want both refused", c.who.name, c.query, s.status, status)
		}
	}

	patG, patF := w.member.stream(t, "threads="+w.g), w.member.stream(t, "forums=general")
	gailV, gailF := w.keeper.stream(t, "threads="+w.v), w.keeper.stream(t, "forums=vault")
	for _, s := range []*p3Stream{patG, patF, gailV, gailF} {
		s.ready(t)
	}
	notVault := func(h hint) bool { return h.ThreadID == w.v || h.ThreadID == w.m }

	// A post in vault reaches its keeper, on both her streams, and not pat:
	// pat's next hint is g's, and nothing from vault comes before it.
	_, vLen := w.admin.post(t, w.v, "Live in the vault.")
	gailV.until(t, w.v, vLen, nil)
	gailF.until(t, w.v, vLen, nil)
	_, gLen := w.admin.post(t, w.g, "Live in general.")
	patG.until(t, w.g, gLen, notVault)
	patF.until(t, w.g, gLen, notVault)

	// A thread pat is reading moves to vault: pat's stream of it ends with
	// nothing past what they could read, their forum stream hears nothing
	// of it, and gail's does.
	x := w.admin.start(t, "general", "Leaving soon", "This one moves.")
	patX := w.member.stream(t, "threads="+x)
	patX.ready(t)
	_, page := w.page(w.member, x)
	if len(page) != 1 {
		t.Fatalf("pat reads %d posts of the thread before the move", len(page))
	}
	before, _ := threadPage(t, w.member, x)
	patF.until(t, x, before, nil)
	w.admin.must(t, http.StatusOK, http.MethodPost, "/api/v1/admin/threads/"+x+"/move", map[string]any{"forum": "vault"})
	_, xLen := w.admin.post(t, x, "Now in the vault.")
	gailF.until(t, x, xLen, nil)
	moved := func(h hint) bool { return h.ThreadID == x && h.LogLength > before }
	patX.ends(t, moved)
	_, gLen = w.admin.post(t, w.g, "Still in general.")
	patF.until(t, w.g, gLen, func(h hint) bool { return notVault(h) || moved(h) })

	// pat signs out everywhere: both their streams end at the next post,
	// before they are sent it.
	w.member.must(t, http.StatusNoContent, http.MethodDelete, "/api/v1/settings/sessions", nil)
	_, gLen = w.admin.post(t, w.g, "After pat left.")
	after := func(h hint) bool { return h.LogLength >= gLen || notVault(h) || moved(h) }
	patG.ends(t, after)
	patF.ends(t, after)
}

// blobStatus is GET /blobs/h, or its thumbnail, as a.
func (w *p3World) blobStatus(a *actor, h string, thumb bool) int {
	w.t.Helper()
	path := "/blobs/" + h
	if thumb {
		path += "/thumb"
	}
	return a.do(w.t, http.MethodGet, path, nil).status
}

// A blob and its thumbnail are served only to a viewer whose thread page
// shows a post naming it with its body; a blob is named by its content; and
// an upload over limits.attachment_mb is refused and stores nothing.
func TestPhase3ExitBlobsFollowTheirPosts(t *testing.T) {
	w := newP3World(t)

	threads := []string{w.g, w.m, w.v}
	for _, v := range w.viewers() {
		// What the thread pages show v, by hash.
		shown := map[string]bool{}
		for _, thread := range threads {
			_, page := w.page(v, thread)
			for _, p := range page {
				if p.HTML != "" {
					for _, h := range p.hashes() {
						shown[h] = true
					}
				}
			}
		}
		for name, h := range w.blobs {
			want := http.StatusNotFound
			if shown[h] {
				want = http.StatusOK
			}
			if got := w.blobStatus(v, h, false); got != want {
				t.Errorf("%s: blob %s answered %d, want %d", v.name, name, got, want)
			}
			// A thumbnail is made after the upload; wait for one that is due.
			got := w.blobStatus(v, h, true)
			for deadline := time.Now().Add(10 * time.Second); got != want && want == http.StatusOK && time.Now().Before(deadline); {
				time.Sleep(50 * time.Millisecond)
				got = w.blobStatus(v, h, true)
			}
			if got != want {
				t.Errorf("%s: blob %s's thumbnail answered %d, want %d", v.name, name, got, want)
			}
		}
	}

	// What those pages are.
	for _, c := range []struct {
		who  *actor
		ok   []string
		gone []string
	}{
		{w.member, []string{"b6"}, []string{"b2", "b3", "b4", "bm", "bv", "bvr", "bvh"}},
		{w.warden, []string{"b2", "b3", "b6"}, []string{"b4", "bm", "bv", "bvr", "bvh"}},
		{w.keeper, []string{"bm", "bv", "bvr", "bvh", "b6"}, []string{"b2", "b3", "b4"}},
		{w.admin, []string{"b2", "b3", "bm", "bv", "bvr", "bvh", "b6"}, []string{"b4"}},
	} {
		for _, name := range c.ok {
			if got := w.blobStatus(c.who, w.blobs[name], false); got != http.StatusOK {
				t.Errorf("%s: blob %s answered %d, want 200", c.who.name, name, got)
			}
		}
		for _, name := range c.gone {
			if got := w.blobStatus(c.who, w.blobs[name], false); got != http.StatusNotFound {
				t.Errorf("%s: blob %s answered %d, want 404", c.who.name, name, got)
			}
		}
	}

	// Content-addressed: the hash is the bytes' SHA-256, and the same bytes
	// again are the same blob.
	body := pngOf(t, "bv")
	sum := sha256.Sum256(body)
	if want := hex.EncodeToString(sum[:]); w.blobs["bv"] != want {
		t.Errorf("blob bv is %s, want its SHA-256 %s", w.blobs["bv"], want)
	}
	if again := w.admin.attach(t, w.v, "bv"); again != w.blobs["bv"] {
		t.Errorf("the same bytes uploaded again are %s, want %s", again, w.blobs["bv"])
	}

	// One byte over the configured 1 MB is refused, and nothing is stored.
	big := make([]byte, 1<<20+1)
	copy(big, body)
	res := w.admin.upload(t, w.g, "big.png", big)
	if res.status != http.StatusRequestEntityTooLarge {
		t.Errorf("an upload over limits.attachment_mb answered %d, want 413:\n%s", res.status, res)
	}
	// A 404 cannot tell a missing blob from a stored one no post names
	// (Terva review on PR #351), so the store itself is read: the blobs
	// row, the file, and the staging directory. A blob that was stored
	// shows up in all of the first two, so the check can see storage.
	sum = sha256.Sum256(big)
	if row, file := w.stored(w.blobs["bv"]); !row || !file {
		t.Fatalf("blob bv, which was stored, reads as row %t, file %t", row, file)
	}
	if row, file := w.stored(hex.EncodeToString(sum[:])); row || file {
		t.Errorf("the refused upload was stored: row %t, file %t", row, file)
	}
	staged, err := os.ReadDir(filepath.Join(w.data, blob.Dir, blob.Tmp))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(staged) != 0 {
		t.Errorf("the refused upload left %d files staged", len(staged))
	}
}

// stored is whether the store holds blob h: a row in blobs, and its file.
func (w *p3World) stored(h string) (row, file bool) {
	w.t.Helper()
	d, err := db.Open(w.t.Context(), db.Options{Path: filepath.Join(w.data, "db", "ketju.db")})
	if err != nil {
		w.t.Fatal(err)
	}
	defer d.Close()
	if err := d.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		var n int
		err := tx.QueryRowContext(w.t.Context(), `SELECT count(*) FROM blobs WHERE hash = ?`, h).Scan(&n)
		row = n > 0
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	p, err := blob.Path(w.data, h)
	if err != nil {
		w.t.Fatal(err)
	}
	_, err = os.Stat(p)
	if err != nil && !os.IsNotExist(err) {
		w.t.Fatal(err)
	}
	return row, err == nil
}
