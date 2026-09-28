package api

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// profileWorld is the world with the forums, accounts and posts every
// viewer class is tested against (docs/07 GET /users/{handle}, decided in
// TKT-01M3KJJNDC):
//
//   - general is public, lounge is members-only, and staff is restricted,
//     readable by the book group, which carol is in, and by admins;
//   - alice wrote a post in each, and one more in general that she deleted;
//   - sam is suspended, dee deactivated and penny pending; sam and dee each
//     wrote a post in general before their status changed;
//   - erin is a plain member, dana an admin, bob (the world's) a moderator;
//   - deleted-user-9 is an anonymized account whose row still holds its old
//     name, avatar and bio, as a row caught halfway would.
type profileWorld struct {
	*world
	lounge, staff int64
	// posts are alice's, by name: general, gone (deleted), lounge, staff.
	posts map[string]postAt
}

type postAt struct {
	thread  threadid.ID
	ordinal int
}

func newProfileWorld(t *testing.T) *profileWorld {
	w := &profileWorld{world: newWorld(t), posts: map[string]postAt{}}
	w.addUsers(domain.StatusActive, "erin")
	w.addUsers(domain.StatusSuspended, "sam")
	w.addUsers(domain.StatusDeactivated, "dee")
	w.addUsers(domain.StatusPending, "penny")
	w.addUsers(domain.StatusActive, "deleted-user-9")
	w.write(func(q *dbq.Queries) error {
		id, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "dana", DisplayName: "Dana Admin",
			Status: domain.StatusActive, Role: domain.RoleAdmin, Kind: "human", CreatedAt: 1, UpdatedAt: 1})
		w.users["dana"] = id
		return err
	})
	w.exec(`UPDATE users SET bio_source = ?, created_at = ? WHERE id = ?`,
		"Runs the *book club*.\n\n> [quote=#3]\n> quoted", 1_757_000_000_000, w.users["alice"])
	w.exec(`UPDATE users SET display_name = 'Old Name', avatar_blob = 'a1b2c3old', bio_source = 'I am Old Name.' WHERE id = ?`,
		w.users["deleted-user-9"])

	w.write(func(q *dbq.Queries) error {
		var err error
		if w.lounge, err = q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "lounge", Name: "Lounge",
			Visibility: domain.VisibilityMembers, CreatedAt: 1, UpdatedAt: 1}); err != nil {
			return err
		}
		if w.staff, err = q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "staff", Name: "Staff",
			Visibility: domain.VisibilityRestricted, CreatedAt: 1, UpdatedAt: 1}); err != nil {
			return err
		}
		g, err := q.CreateGroup(t.Context(), dbq.CreateGroupParams{Name: "Book", Slug: "book", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			return err
		}
		if err := q.AddAdminMembership(t.Context(), dbq.AddAdminMembershipParams{GroupID: g, UserID: w.users["carol"], AddedAt: 1}); err != nil {
			return err
		}
		return q.GrantPermission(t.Context(), dbq.GrantPermissionParams{
			ForumID: sql.NullInt64{Int64: w.staff, Valid: true}, GroupID: g, Permission: string(domain.PermRead), CreatedAt: 1})
	})

	general := w.threadIn(1, w.forum, "General chat", "alice")
	w.posts["general"] = postAt{general, 1}
	lounge := w.threadIn(2, w.lounge, "Lounge chat", "alice")
	w.posts["lounge"] = postAt{lounge, 1}
	staff := w.threadIn(3, w.staff, "Staff chat", "alice")
	w.posts["staff"] = postAt{staff, 1}
	w.post(general, "alice", "I take this back.", 0)
	w.posts["gone"] = postAt{general, 2}
	w.del(general, 2, "alice")
	w.post(general, "sam", "Sam was here.", 0)
	w.post(general, "dee", "Dee was here.", 0)
	w.post(general, "deleted-user-9", "Signed by nobody now.", 0)
	return w
}

// exec runs one statement outside the generated queries, for columns no
// write path sets yet (bio_source) and for the halfway anonymized row.
func (w *world) exec(stmt string, args ...any) {
	w.t.Helper()
	if err := w.db.Write(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(w.t.Context(), stmt, args...)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
}

// threadIn creates a thread in forum whose first post is author's, and
// returns its id.
func (w *world) threadIn(seq uint64, forum int64, title, author string) threadid.ID {
	w.t.Helper()
	id := threadid.ID(uint64(1_757_500_000_000)<<16 | seq)
	th, err := w.cache.Create(id, uint64(forum), w.clock)
	if err != nil {
		w.t.Fatal(err)
	}
	w.cache.Release(th)
	w.append(id, threadlog.Append{Type: threadlog.TypeThreadCreated, Author: uint64(w.users[author]),
		Body: &threadlog.ThreadCreatedBody{Title: title, Slug: strings.ToLower(strings.ReplaceAll(title, " ", "-")),
			ForumID: uint64(forum)}})
	w.post(id, author, title+", the first post.", 0)
	return id
}

// del appends author's Delete of ordinal, and projects it.
func (w *world) del(id threadid.ID, ordinal uint32, author string) {
	w.t.Helper()
	w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users[author]), Ordinal: ordinal,
		Body: &threadlog.DeleteBody{}})
}

// as is the viewer each class is tested as.
func (w *profileWorld) as(class string) domain.Viewer {
	switch class {
	case "anon":
		return domain.Anonymous()
	case "pre-user":
		return domain.Viewer{PreUser: true, IdentityID: 7, Active: true}
	case "author":
		return w.viewer("alice")
	case "member":
		return w.viewer("erin")
	case "grantee":
		return w.viewer("carol")
	case "moderator":
		return w.viewer("bob")
	case "admin":
		return domain.Viewer{UserID: w.users["dana"], Role: domain.RoleAdmin, Active: true, SessionKey: "s-dana"}
	case "inactive":
		v := w.viewer("sam")
		v.Active = false
		return v
	}
	w.t.Fatalf("no viewer class %q", class)
	return domain.Viewer{}
}

var viewerClasses = []string{"anon", "pre-user", "inactive", "author", "member", "grantee", "moderator", "admin"}

// TestProfileWhoMaySee is the decision's first and third answers, for every
// viewer class against every account status: a stranger is unauthenticated
// and a pre-user forbidden, whatever the handle; a pending account is
// not-found except to moderators and admins; suspended and deactivated
// accounts are served; the status is shown to staff only.
func TestProfileWhoMaySee(t *testing.T) {
	w := newProfileWorld(t)
	for _, class := range viewerClasses {
		for _, handle := range []string{"alice", "sam", "dee", "penny", "nobody-here"} {
			for _, path := range []string{"/api/v1/users/" + handle, "/api/v1/users/" + handle + "/posts"} {
				rec := w.get(w.as(class), path)
				staff := class == "moderator" || class == "admin"
				var want problem.Code
				switch {
				case class == "anon" || class == "inactive":
					want = problem.CodeUnauthenticated
				case class == "pre-user":
					want = problem.CodeForbidden
				case handle == "nobody-here", handle == "penny" && !staff:
					want = problem.CodeNotFound
				}
				if want != "" {
					if got := problemCode(t, rec); got != string(want) {
						t.Errorf("%s %s: %d %s, want %s", class, path, rec.Code, got, want)
					}
					continue
				}
				if rec.Code != http.StatusOK {
					t.Errorf("%s %s: %d %s", class, path, rec.Code, rec.Body)
					continue
				}
				if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
					t.Errorf("%s %s: Cache-Control %q", class, path, cc)
				}
				if strings.HasSuffix(path, "/posts") {
					decode[dto.UserPosts](t, rec)
					continue
				}
				p := decode[dto.Profile](t, rec)
				if p.User.Handle != handle {
					t.Errorf("%s %s: handle %q", class, path, p.User.Handle)
				}
				switch {
				case staff && (p.Status == nil || *p.Status != statusOf(handle)):
					t.Errorf("%s %s: status %v, want %s", class, path, p.Status, statusOf(handle))
				case !staff && p.Status != nil:
					t.Errorf("%s %s: status %q shown to a non-staff viewer", class, path, *p.Status)
				}
			}
		}
	}
	// A stranger's malformed cursor is refused as a stranger's.
	if got := problemCode(t, w.get(domain.Anonymous(), "/api/v1/users/alice/posts?cursor=!!!")); got != string(problem.CodeUnauthenticated) {
		t.Errorf("anon with a bad cursor: %s", got)
	}
}

func statusOf(handle string) string {
	switch handle {
	case "sam":
		return domain.StatusSuspended
	case "dee":
		return domain.StatusDeactivated
	case "penny":
		return domain.StatusPending
	}
	return domain.StatusActive
}

// TestProfileFields: a profile is the author block, the rendered bio, the
// joined date and the count. A handle typed with capitals finds its account
// and shows it as stored. A quote in a bio has no thread to point into, so
// it is not a link.
func TestProfileFields(t *testing.T) {
	w := newProfileWorld(t)
	p := decode[dto.Profile](t, w.get(w.as("member"), "/api/v1/users/ALICE"))
	want := dto.Author{Handle: "alice", DisplayName: "Alice Nakamura", Role: domain.RoleMember, Kind: "human"}
	if p.User != want || p.JoinedAt != 1_757_000_000_000 {
		t.Errorf("profile %+v", p)
	}
	if !strings.Contains(p.BioHTML, "<em>book club</em>") || strings.Contains(p.BioHTML, "#post-3") {
		t.Errorf("bio %q", p.BioHTML)
	}
}

// TestPostCountByViewer is the decision's second answer: the count is the
// account's non-deleted posts in forums this viewer can read. It varies by
// viewer, leaks nothing about the staff forum to those who cannot read it,
// and never counts the deleted post, even for the viewers whose history
// shows it. For a member it is the length of the history.
func TestPostCountByViewer(t *testing.T) {
	w := newProfileWorld(t)
	for class, want := range map[string]int{"author": 2, "member": 2, "moderator": 2, "grantee": 3, "admin": 3} {
		p := decode[dto.Profile](t, w.get(w.as(class), "/api/v1/users/alice"))
		if p.PostCount != want {
			t.Errorf("%s: post count %d, want %d", class, p.PostCount, want)
		}
	}
	h := decode[dto.UserPosts](t, w.get(w.as("member"), "/api/v1/users/alice/posts"))
	if p := decode[dto.Profile](t, w.get(w.as("member"), "/api/v1/users/alice")); p.PostCount != len(h.Posts) {
		t.Errorf("a member's count %d is not their history's %d posts", p.PostCount, len(h.Posts))
	}
}

// history is who's view of alice's posts, as names from w.posts, newest
// first, with each post's deleted mark and whether it carries a body.
func (w *profileWorld) history(class string) []string {
	w.t.Helper()
	h := decode[dto.UserPosts](w.t, w.get(w.as(class), "/api/v1/users/alice/posts"))
	var out []string
	for _, up := range h.Posts {
		name := "?"
		for n, at := range w.posts {
			if at.thread.String() == up.Thread.ID && at.ordinal == up.Post.Ordinal {
				name = n
			}
		}
		if up.Post.Deleted {
			name += "(deleted"
			if up.Post.HTML != "" {
				name += ", body"
			}
			name += ")"
		}
		out = append(out, name)
	}
	return out
}

// TestHistoryByViewer is AC1 and AC3 with the decision's fourth answer: a
// history spans forums and leaves out what the viewer cannot read; a
// deleted post is left out for other members, shown marked deleted to its
// author with no body, as the thread page shows it to them, and shown
// marked deleted with its body to moderators and admins.
func TestHistoryByViewer(t *testing.T) {
	w := newProfileWorld(t)
	for class, want := range map[string][]string{
		"member":    {"lounge", "general"},
		"author":    {"gone(deleted)", "lounge", "general"},
		"grantee":   {"staff", "lounge", "general"},
		"moderator": {"gone(deleted, body)", "lounge", "general"},
		"admin":     {"gone(deleted, body)", "staff", "lounge", "general"},
	} {
		if got := w.history(class); !slices.Equal(got, want) {
			t.Errorf("%s sees %v, want %v", class, got, want)
		}
	}
	// Each post links to its thread and forum.
	h := decode[dto.UserPosts](t, w.get(w.as("grantee"), "/api/v1/users/alice/posts"))
	if got := h.Posts[0]; got.Thread != (dto.ThreadLink{ID: w.posts["staff"].thread.String(), Slug: "staff-chat", Title: "Staff chat"}) ||
		got.Forum != (dto.ForumRef{Slug: "staff", Name: "Staff"}) || got.Post.HTML != "<p>Staff chat, the first post.</p>\n" {
		t.Errorf("first post %+v", got)
	}
	if h.NextCursor != "" {
		t.Errorf("a short history has a cursor %q", h.NextCursor)
	}
}

// TestMemberPagesAreFull: the query leaves out what a member may not see,
// rather than the service dropping it from a page already cut to size, so
// a member's page is full while there is more to show. alice's newest post
// is the deleted one; a member's first page of one is her lounge post.
func TestMemberPagesAreFull(t *testing.T) {
	w := newProfileWorld(t)
	w.svc.UserPostsPage = 1
	h := decode[dto.UserPosts](t, w.get(w.as("member"), "/api/v1/users/alice/posts"))
	if len(h.Posts) != 1 || h.Posts[0].Thread.Slug != "lounge-chat" || h.NextCursor == "" {
		t.Errorf("first page of one: %+v", h)
	}
}

// unprojected appends a record to a thread's log without projecting it: the
// moment between an append and the projection catching up, held open.
func (w *world) unprojected(id threadid.ID, a threadlog.Append) {
	w.t.Helper()
	if a.TimeMS == 0 {
		a.TimeMS = w.clock + 1
	}
	if err := w.cache.With(id, func(th *threadcache.Thread) error {
		_, err := th.Actor.Append(a)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
}

// count is class's post count for alice.
func (w *profileWorld) count(class string) int {
	w.t.Helper()
	return decode[dto.Profile](w.t, w.get(w.as(class), "/api/v1/users/alice")).PostCount
}

// TestHistoryTrustsTheLogOverTheProjection: a post deleted in the log that
// post_meta has not heard of yet is kept from a member and shown to its
// author marked deleted, and the count agrees with the history: it counts
// that thread from its index, since its projection trails its log.
func TestHistoryTrustsTheLogOverTheProjection(t *testing.T) {
	w := newProfileWorld(t)
	at := w.posts["lounge"]
	w.unprojected(at.thread, threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["alice"]),
		Ordinal: uint32(at.ordinal), Body: &threadlog.DeleteBody{}})
	if got, want := w.history("member"), []string{"general"}; !slices.Equal(got, want) {
		t.Errorf("member sees %v, want %v", got, want)
	}
	if got, want := w.history("author"), []string{"gone(deleted)", "lounge(deleted)", "general"}; !slices.Equal(got, want) {
		t.Errorf("author sees %v, want %v", got, want)
	}
	for _, class := range []string{"member", "author", "moderator"} {
		if got := w.count(class); got != 1 {
			t.Errorf("%s: post count %d, want 1", class, got)
		}
	}
}

// TestRestoreBeforeTheProjection is the other direction (review 1162): a
// post restored in the log while post_meta still says deleted is on a
// member's history, and counted.
func TestRestoreBeforeTheProjection(t *testing.T) {
	w := newProfileWorld(t)
	at := w.posts["gone"]
	w.unprojected(at.thread, threadlog.Append{Type: threadlog.TypeRestore, Author: uint64(w.users["alice"]),
		Ordinal: uint32(at.ordinal), Body: &threadlog.RestoreBody{}})
	want := []string{"gone", "lounge", "general"}
	if got := w.history("member"); !slices.Equal(got, want) {
		t.Errorf("member sees %v, want %v", got, want)
	}
	if got := w.count("member"); got != len(want) {
		t.Errorf("member: post count %d, want %d", got, len(want))
	}
}

// TestPagesFillPastHiddenPosts (review 1162): a page is filled from further
// posts when the log hides some the projection offered, and a history that
// ends has no cursor. With a page of one and lounge deleted in the log only,
// a member's first page is general, the last. With a page of two, the page
// that reaches the end has no cursor, though a deleted post came first.
func TestPagesFillPastHiddenPosts(t *testing.T) {
	w := newProfileWorld(t)
	w.svc.UserPostsPage = 2
	h := decode[dto.UserPosts](t, w.get(w.as("member"), "/api/v1/users/alice/posts"))
	if len(h.Posts) != 2 || h.NextCursor != "" {
		t.Errorf("page of two: %d posts, cursor %q", len(h.Posts), h.NextCursor)
	}

	w.svc.UserPostsPage = 1
	at := w.posts["lounge"]
	w.unprojected(at.thread, threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["alice"]),
		Ordinal: uint32(at.ordinal), Body: &threadlog.DeleteBody{}})
	h = decode[dto.UserPosts](t, w.get(w.as("member"), "/api/v1/users/alice/posts"))
	if len(h.Posts) != 1 || h.Posts[0].Thread.Slug != "general-chat" || h.NextCursor != "" {
		t.Errorf("first page of one: %+v", h)
	}
}

// TestCountTakesTheProjectionWhenCaughtUp holds the premise of the count's
// fast path: once a thread is projected, its log's size on disk is exactly
// how far the projection says it has got, so a caught-up thread is counted
// from post_meta and not opened.
func TestCountTakesTheProjectionWhenCaughtUp(t *testing.T) {
	w := newProfileWorld(t)
	for name, at := range w.posts {
		size, err := w.cache.LogSize(at.thread)
		if err != nil {
			t.Fatal(err)
		}
		var row dbq.Thread
		w.write(func(q *dbq.Queries) error {
			var err error
			row, err = q.GetThread(t.Context(), int64(at.thread))
			return err
		})
		if size != row.LogLengthProjected {
			t.Errorf("%s: log is %d bytes, projected to %d", name, size, row.LogLengthProjected)
		}
	}
}

// TestSuspendedAndDeactivatedHistories: their posts are kept with the handle
// shown (docs/05), so a member sees them.
func TestSuspendedAndDeactivatedHistories(t *testing.T) {
	w := newProfileWorld(t)
	for _, h := range []string{"sam", "dee"} {
		got := decode[dto.UserPosts](t, w.get(w.as("member"), "/api/v1/users/"+h+"/posts"))
		if len(got.Posts) != 1 || got.Posts[0].Post.Author.Handle != h {
			t.Errorf("%s: %+v", h, got.Posts)
		}
	}
}

// TestAnonymizedProfile is AC2: the placeholder handle and no prior identity,
// in the profile and in the history, for every viewer class that is served,
// even though the row still holds the old name, avatar and bio. The raw
// bodies are searched, so no field anywhere can carry them.
func TestAnonymizedProfile(t *testing.T) {
	w := newProfileWorld(t)
	for _, class := range []string{"author", "member", "moderator", "admin"} {
		for _, path := range []string{"/api/v1/users/deleted-user-9", "/api/v1/users/deleted-user-9/posts"} {
			rec := w.get(w.as(class), path)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s %s: %d %s", class, path, rec.Code, rec.Body)
			}
			for _, old := range []string{"Old Name", "a1b2c3old"} {
				if strings.Contains(rec.Body.String(), old) {
					t.Errorf("%s %s: the response carries %q: %s", class, path, old, rec.Body)
				}
			}
		}
		p := decode[dto.Profile](t, w.get(w.as(class), "/api/v1/users/deleted-user-9"))
		if want := (dto.Author{Handle: "deleted-user-9", Role: domain.RoleMember, Kind: "human"}); p.User != want || p.BioHTML != "" {
			t.Errorf("%s: %+v", class, p)
		}
		h := decode[dto.UserPosts](t, w.get(w.as(class), "/api/v1/users/deleted-user-9/posts"))
		if len(h.Posts) != 1 || h.Posts[0].Post.Author != p.User {
			t.Errorf("%s: history %+v", class, h.Posts)
		}
	}
}

// TestHistoryPaging is AC4: paging is a keyset on created_at, with the
// thread and ordinal breaking ties. Three posts written in one millisecond,
// paged two at a time, come back exactly once each, newest thread and
// ordinal first; a post written between pages is newer than all of them, so
// it is above the cursor and appears on no later page; the last page has no
// cursor.
func TestHistoryPaging(t *testing.T) {
	w := newProfileWorld(t)
	w.svc.UserPostsPage = 2
	a, b := w.threadIn(10, w.forum, "Tie one", "carol"), w.threadIn(11, w.forum, "Tie two", "bob")
	same := w.clock + 1_000_000
	for _, id := range []threadid.ID{a, b, a} {
		w.append(id, threadlog.Append{Type: threadlog.TypePost, Author: uint64(w.users["erin"]),
			Body: &threadlog.PostBody{Source: "tied"}, TimeMS: same})
	}
	w.post(a, "erin", "older than the ties", 0)
	olderAt := w.clock

	var seen []string
	cursor := ""
	for page := 0; ; page++ {
		path := "/api/v1/users/erin/posts"
		if cursor != "" {
			path += "?cursor=" + cursor
		}
		h := decode[dto.UserPosts](t, w.get(w.as("member"), path))
		if len(h.Posts) > 2 {
			t.Fatalf("page %d has %d posts", page, len(h.Posts))
		}
		for _, p := range h.Posts {
			seen = append(seen, fmt.Sprintf("%s#%d@%d", p.Thread.Slug, p.Post.Ordinal, p.Post.CreatedAt))
		}
		if page == 0 {
			w.append(b, threadlog.Append{Type: threadlog.TypePost, Author: uint64(w.users["erin"]),
				Body: &threadlog.PostBody{Source: "written while paging"}, TimeMS: same + 1})
		}
		if h.NextCursor == "" {
			break
		}
		cursor = h.NextCursor
		if page > 5 {
			t.Fatal("paging does not end")
		}
	}
	want := []string{
		fmt.Sprintf("tie-two#2@%d", same),
		fmt.Sprintf("tie-one#3@%d", same),
		fmt.Sprintf("tie-one#2@%d", same),
		fmt.Sprintf("tie-one#4@%d", olderAt),
	}
	if !slices.Equal(seen, want) {
		t.Errorf("paged through %v, want %v", seen, want)
	}
	// And a fresh first page starts with the post written while paging.
	h := decode[dto.UserPosts](t, w.get(w.as("member"), "/api/v1/users/erin/posts"))
	if got := h.Posts[0]; got.Thread.Slug != "tie-two" || got.Post.Ordinal != 3 {
		t.Errorf("first page opens with %s#%d", got.Thread.Slug, got.Post.Ordinal)
	}
}

// TestHistoryCursorRefusals: a cursor is a position, and one that is not
// exactly what the server writes is bad-request.
func TestHistoryCursorRefusals(t *testing.T) {
	w := newProfileWorld(t)
	bad := []string{"!!!", base64.RawURLEncoding.EncodeToString([]byte("1|1|1")) + "="}
	for _, raw := range []string{"x", "1|2", "1|2|0", "1|0|1", "1|-1|1", "+5|1|1", "05|1|1", "1|1|1|1", "a|1|1", "1|1|1 "} {
		bad = append(bad, base64.RawURLEncoding.EncodeToString([]byte(raw)))
	}
	for _, c := range bad {
		if got := problemCode(t, w.get(w.as("member"), "/api/v1/users/alice/posts?cursor="+c)); got != string(problem.CodeBadRequest) {
			t.Errorf("cursor %q: %s", c, got)
		}
	}
}
