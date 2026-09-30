package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/identity"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// Mentions across handle changes (TKT-01M3NRP3W9, AC6 and AC7): a mention
// names the member it named when it was written, through the kept release
// rows (internal/service/threads mentions.go). Posts here are written
// through the API, so their mentions are recorded as a member's would be.

// renamer is an identity service on the world's clock, as another process
// of the same server would hold one.
func (w *world) renamer() *identity.Service {
	return &identity.Service{DB: w.db, Perm: perm.New(w.db), Now: func() time.Time { return time.UnixMilli(w.clock) }}
}

// renameTo renames handle to to, as an admin, a minute on.
func (w *world) renameTo(handle, to string) {
	w.t.Helper()
	w.clock += 60_000
	admin := domain.Viewer{UserID: w.users["bob"], Role: domain.RoleAdmin, Active: true}
	if _, err := w.renamer().RenameMember(w.t.Context(), admin, "", handle, to); err != nil {
		w.t.Fatalf("renaming %s to %s: %v", handle, to, err)
	}
}

// register makes a new member called handle, a minute on, as registration
// would once the handle is free.
func (w *world) register(name, handle string) {
	w.t.Helper()
	w.clock += 60_000
	w.write(func(q *dbq.Queries) error {
		if err := identity.HandleFree(w.t.Context(), q, handle, w.clock); err != nil {
			return err
		}
		id, err := q.CreateUser(w.t.Context(), dbq.CreateUserParams{Handle: handle, DisplayName: name,
			Status: domain.StatusActive, Role: domain.RoleMember, Kind: domain.KindHuman, CreatedAt: w.clock,
			UpdatedAt: w.clock})
		w.users[name] = id
		return err
	})
}

// say writes a reply as who through the API, a minute on, projects it as
// the server's bus would, and returns its ordinal.
func (w *world) say(id threadid.ID, who, source string) int {
	w.t.Helper()
	res := w.sayUnprojected(id, who, source)
	if _, err := w.proj.ProjectThread(w.t.Context(), id, res.LogLength); err != nil {
		w.t.Fatal(err)
	}
	return res.Post.Ordinal
}

// sayUnprojected is say before the bus has projected the reply.
func (w *world) sayUnprojected(id threadid.ID, who, source string) dto.CreatedPost {
	w.t.Helper()
	w.clock += 60_000
	rec := w.sendAs(w.viewer(who), "POST", postsPath(id), fmt.Sprintf(`{"source":%q}`, source))
	return created[dto.CreatedPost](w.t, rec)
}

// handlesThread is a thread with alice's opening post.
func (w *world) handlesThread() threadid.ID {
	w.t.Helper()
	id, _ := w.thread(1, "Handles")
	w.post(id, "alice", "Who is called what?", 0)
	return id
}

// htmlOf is post n's HTML on the thread page as viewer, through the caches,
// checked against a fresh draw.
func (w *world) htmlOf(id threadid.ID, viewer string, n int) string {
	w.t.Helper()
	w.same(w.session(viewer), "/api/v1/threads/"+id.String())
	for _, p := range page(w.t, w.get(w.session(viewer), "/api/v1/threads/"+id.String())).Posts {
		if p.Ordinal == n {
			return p.HTML
		}
	}
	w.t.Fatalf("no post %d", n)
	return ""
}

// links asserts html links the mention to handle and shows it as @handle.
func links(t *testing.T, what, html, handle string) {
	t.Helper()
	if !strings.Contains(html, `href="/u/`+handle+`"`) || !strings.Contains(html, ">@"+handle+"<") {
		t.Errorf("%s does not link @%s: %s", what, handle, html)
	}
}

// plain asserts html shows @typed as text and links no one.
func plain(t *testing.T, what, html, typed string) {
	t.Helper()
	if strings.Contains(html, "/u/") || !strings.Contains(html, "@"+typed) {
		t.Errorf("%s is not the plain text @%s: %s", what, typed, html)
	}
}

// mentionWorld is a thread whose first reply, bob's, mentions @alice.
func mentionWorld(t *testing.T) (*world, threadid.ID, int) {
	w := newWorld(t)
	id := w.handlesThread()
	n := w.say(id, "bob", "Thanks @alice, that settles it.")
	links(t, "before the rename", w.htmlOf(id, "carol", n), "alice")
	return w, id, n
}

// TestOldMentionsFollowTheRename is AC6's render half: bob's rendering of
// @alice, cached by the render and response caches, is not served after
// alice renames; the next one links /u/alicia and reads @alicia.
func TestOldMentionsFollowTheRename(t *testing.T) {
	w, id, n := mentionWorld(t)
	w.renameTo("alice", "alicia")
	html := w.htmlOf(id, "carol", n)
	links(t, "after the rename", html, "alicia")
	if strings.Contains(html, `/u/alice"`) || strings.Contains(html, ">@alice<") {
		t.Errorf("the old handle is still drawn: %s", html)
	}
}

// TestOldMentionsNeverMoveToANewHolder is AC7: after the reservation ends
// and vic takes alice, bob's old post still links alicia, while a post
// written after vic registered links vic.
func TestOldMentionsNeverMoveToANewHolder(t *testing.T) {
	w, id, n := mentionWorld(t)
	w.renameTo("alice", "alicia")
	links(t, "during the reservation", w.htmlOf(id, "carol", n), "alicia")
	w.clock += domain.HandleReservation.Milliseconds()
	w.register("vic", "alice")
	links(t, "after vic took the handle", w.htmlOf(id, "carol", n), "alicia")
	later := w.say(id, "carol", "Welcome @alice!")
	html := w.htmlOf(id, "carol", later)
	links(t, "a post written after vic registered", html, "alice")
	if strings.Contains(html, "alicia") {
		t.Errorf("the new post names alicia: %s", html)
	}
}

// TestARenameIntoAnOldHandleTakesNoMention: a post mentions @alice and
// @carol; alice renames to alicia, and once alice is free carol renames to
// alice. The post's @alice still names alicia and its @carol names carol,
// shown under her new handle alice; neither moves to the other.
func TestARenameIntoAnOldHandleTakesNoMention(t *testing.T) {
	w := newWorld(t)
	id := w.handlesThread()
	n := w.say(id, "bob", "@alice and @carol both said so.")
	w.renameTo("alice", "alicia")
	w.clock += domain.HandleReservation.Milliseconds()
	w.renameTo("carol", "alice")
	html := w.htmlOf(id, "bob", n)
	if !strings.Contains(html, `href="/u/alicia" class="mention" rel="nofollow">@alicia</a> and <a href="/u/alice" class="mention" rel="nofollow">@alice</a>`) {
		t.Errorf("the mentions moved: %s", html)
	}
}

// TestAMentionWrittenDuringAReservationStaysWithIt is Terva review 1515's
// first finding: a post written while alice redirects to alicia mentions
// @alice and @carol. Once the reservation ends carol renames to alice and
// then away again. The post's @alice still names alicia, whom it named
// when it was written, and never carol, who is in its mentions for her own
// @carol.
func TestAMentionWrittenDuringAReservationStaysWithIt(t *testing.T) {
	w := newWorld(t)
	id := w.handlesThread()
	w.renameTo("alice", "alicia")
	n := w.say(id, "bob", "@alice and @carol both said so.")
	w.clock += domain.HandleReservation.Milliseconds()
	w.renameTo("carol", "alice")
	w.renameTo("alice", "caroline")
	html := w.htmlOf(id, "bob", n)
	if !strings.Contains(html, `href="/u/alicia" class="mention" rel="nofollow">@alicia</a> and <a href="/u/caroline" class="mention" rel="nofollow">@caroline</a>`) {
		t.Errorf("the mentions moved: %s", html)
	}
}

// TestAnAnonymizedMentionStaysTextBesideAnother is Terva review 1515's
// second finding: a post mentions @alice and @carol; alice is anonymized,
// and once her handle is free carol renames to alice. The post's @alice is
// text, never carol, although carol is in its mentions; its @carol links
// carol under her handle now.
func TestAnAnonymizedMentionStaysTextBesideAnother(t *testing.T) {
	w := newWorld(t)
	id := w.handlesThread()
	n := w.say(id, "bob", "@alice and @carol both said so.")
	w.anonymize(w.anonymizer(), "alice")
	// The anonymize route reserves on the real clock, which is ahead of the
	// world's.
	w.clock = max(w.clock, time.Now().UnixMilli()) + domain.HandleReservation.Milliseconds()
	w.renameTo("carol", "alice")
	html := w.htmlOf(id, "bob", n)
	if !strings.HasPrefix(html, `<p>@alice and <a href="/u/alice" class="mention" rel="nofollow">@alice</a>`) {
		t.Errorf("the anonymized mention went to carol: %s", html)
	}
}

// TestAMentionTypedBeforeRegistrationStillLinks is AC7's other half
// (docs/07): a post mentioning @dora before anyone is called that links
// dora once she registers, with no release rows for the handle.
func TestAMentionTypedBeforeRegistrationStillLinks(t *testing.T) {
	w := newWorld(t)
	id := w.handlesThread()
	n := w.say(id, "bob", "Has anyone heard from @dora?")
	plain(t, "before dora registers", w.htmlOf(id, "carol", n), "dora")
	w.register("dora", "dora")
	links(t, "after dora registers", w.htmlOf(id, "carol", n), "dora")
	// And it follows her through a rename, as any mention does, and not to
	// whoever takes dora after her (Terva review 1517).
	w.renameTo("dora", "dorothea")
	links(t, "after dora renames", w.htmlOf(id, "carol", n), "dorothea")
	w.clock += domain.HandleReservation.Milliseconds()
	w.register("vic", "dora")
	links(t, "after vic takes dora", w.htmlOf(id, "carol", n), "dorothea")
}

// TestAMentionOfAReleasedHandleFollowsTheRedirect: during the 90 days a new
// @alice mentions the member who renamed, in the preview and in the post,
// and the post records their id.
func TestAMentionOfAReleasedHandleFollowsTheRedirect(t *testing.T) {
	w := newWorld(t)
	id := w.handlesThread()
	w.renameTo("alice", "alicia")
	rec := w.sendAs(w.viewer("carol"), "POST", "/api/v1/render", `{"source":"hi @alice"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `href=\"/u/alicia\"`) {
		t.Errorf("the preview: %d %s", rec.Code, rec.Body)
	}
	n := w.say(id, "carol", "hi @alice")
	links(t, "the post", w.htmlOf(id, "bob", n), "alicia")
	var mentioned int64
	if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT mentioned_user_id FROM mentions WHERE thread_id = ? AND ordinal = ?`,
			int64(id), n).Scan(&mentioned)
	}); err != nil || mentioned != w.users["alice"] {
		t.Errorf("the post records %d (%v), want alicia's %d", mentioned, err, w.users["alice"])
	}
	// Once the reservation ends, a new @alice mentions nobody.
	w.clock += domain.HandleReservation.Milliseconds()
	plain(t, "a post after the reservation", w.htmlOf(id, "bob", w.say(id, "carol", "hi @alice")), "alice")
}

// TestAReservedMentionLinksBeforeProjection is Terva review 1521: a post
// written during the 90 days links the member who renamed as soon as it is
// read, before the projection has recorded its mentions, and still once it
// has.
func TestAReservedMentionLinksBeforeProjection(t *testing.T) {
	w := newWorld(t)
	id := w.handlesThread()
	w.renameTo("alice", "alicia")
	res := w.sayUnprojected(id, "carol", "hi @alice")
	links(t, "before projection", w.htmlOf(id, "bob", res.Post.Ordinal), "alicia")
	if _, err := w.proj.ProjectThread(t.Context(), id, res.LogLength); err != nil {
		t.Fatal(err)
	}
	links(t, "after projection", w.htmlOf(id, "bob", res.Post.Ordinal), "alicia")
}

// TestAnAnonymizedMentionIsText: after alicia, who renamed from alice, is
// anonymized and vic takes alice once it is free, bob's old @alice is plain
// text, never vic, and never deleted-user-<id>.
func TestAnAnonymizedMentionIsText(t *testing.T) {
	w, id, n := mentionWorld(t)
	w.renameTo("alice", "alicia")
	w.anonymize(w.anonymizer(), "alicia")
	plain(t, "after the anonymization", w.htmlOf(id, "carol", n), "alice")
	w.clock += domain.HandleReservation.Milliseconds()
	w.register("vic", "alice")
	html := w.htmlOf(id, "carol", n)
	plain(t, "after vic took the handle", html, "alice")
	if strings.Contains(html, "deleted-user") {
		t.Errorf("the mention names the placeholder: %s", html)
	}
}

// TestResponseCacheAfterRename is AC6's cache half, as
// TestResponseCacheAfterPromotion: a page cached for each viewer class with
// alice as an author and in a mention is not served once another process
// has renamed her, with no append to the thread; the next page shows
// alicia in the header and the mention, and equals a fresh draw.
func TestResponseCacheAfterRename(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	n := w.say(id, "carol", "Agreed, @alice.")
	path := "/api/v1/threads/" + id.String()
	for _, who := range []string{"", "carol", "bob"} {
		if body := w.same(w.session(who), path); !strings.Contains(body, `"handle":"alice"`) {
			t.Fatalf("%q's page does not name alice to begin with", who)
		}
	}
	w.renameTo("alice", "alicia")
	for _, who := range []string{"", "carol", "bob"} {
		body := w.same(w.session(who), path)
		if strings.Contains(body, `"handle":"alice"`) || strings.Contains(body, `/u/alice\"`) {
			t.Errorf("%q was served alice's old handle after the rename: %s", who, body)
		}
		p := page(t, w.get(w.session(who), path))
		if p.Posts[0].Author.Handle != "alicia" {
			t.Errorf("%q: post 1's author is %+v", who, p.Posts[0].Author)
		}
		for _, post := range p.Posts {
			if post.Ordinal == n {
				links(t, fmt.Sprintf("%q's mention", who), post.HTML, "alicia")
			}
		}
	}
}
