package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// forumWorld is newWorld's "general" (public, position 0) with a tree
// around it, created out of position order so that the order in the answer
// is the query's rather than the inserts':
//
//	general        public      root 0
//	  g-secret     restricted  child 0, read granted to crew
//	  g-a          members     child 1
//	  g-b          public      child 2
//	alpha          members     root 1
//	zeta           public      root 2
//	mods           restricted  root 3, read granted to crew
//	  mods-public  public      child 0, under a root only crew may read
//
// carol is in crew; alice is not.
type forumWorld struct {
	*world
	ids map[string]int64
}

func newForumWorld(t *testing.T) *forumWorld {
	t.Helper()
	w := &forumWorld{world: newWorld(t), ids: map[string]int64{"general": 0}}
	w.ids["general"] = w.forum
	create := func(slug, visibility, parent string, position int64) {
		w.write(func(q *dbq.Queries) error {
			p := sql.NullInt64{}
			if parent != "" {
				p = sql.NullInt64{Int64: w.ids[parent], Valid: true}
			}
			id, err := q.CreateForum(t.Context(), dbq.CreateForumParams{ParentID: p, Slug: slug, Name: strings.ToUpper(slug),
				Position: position, Visibility: visibility, CreatedAt: 1, UpdatedAt: 1})
			w.ids[slug] = id
			return err
		})
	}
	create("zeta", domain.VisibilityPublic, "", 2)
	create("mods", domain.VisibilityRestricted, "", 3)
	create("alpha", domain.VisibilityMembers, "", 1)
	create("g-b", domain.VisibilityPublic, "general", 2)
	create("g-a", domain.VisibilityMembers, "general", 1)
	create("g-secret", domain.VisibilityRestricted, "general", 0)
	create("mods-public", domain.VisibilityPublic, "mods", 0)
	w.write(func(q *dbq.Queries) error {
		crew, err := q.CreateGroup(t.Context(), dbq.CreateGroupParams{Name: "Crew", Slug: "crew", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			return err
		}
		if err := q.AddAdminMembership(t.Context(), dbq.AddAdminMembershipParams{GroupID: crew, UserID: w.users["carol"],
			AddedBy: sql.NullInt64{Int64: w.users["bob"], Valid: true}, AddedAt: 1}); err != nil {
			return err
		}
		for _, f := range []string{"mods", "g-secret"} {
			if err := q.GrantPermission(t.Context(), dbq.GrantPermissionParams{ForumID: sql.NullInt64{Int64: w.ids[f], Valid: true},
				GroupID: crew, Permission: string(domain.PermRead), CreatedAt: 1}); err != nil {
				return err
			}
		}
		return nil
	})
	return w
}

// threadIn creates a thread in forum and returns its id.
func (w *forumWorld) threadIn(forum string, seq uint64, title string) threadid.ID {
	w.t.Helper()
	id := threadid.ID(uint64(1_757_500_000_000)<<16 | seq)
	th, err := w.cache.Create(id, uint64(w.ids[forum]), w.clock)
	if err != nil {
		w.t.Fatal(err)
	}
	w.cache.Release(th)
	w.append(id, threadlog.Append{Type: threadlog.TypeThreadCreated, Author: uint64(w.users["alice"]),
		Body: &threadlog.ThreadCreatedBody{Title: title, Slug: strings.ToLower(strings.ReplaceAll(title, " ", "-")),
			ForumID: uint64(w.ids[forum]), Tags: []string{}}})
	return id
}

func (w *forumWorld) tree(handle string) (dto.ForumTree, string) {
	w.t.Helper()
	rec := w.get(w.viewer(handle), "/api/v1/forums")
	if rec.Code != http.StatusOK {
		w.t.Fatalf("GET /forums as %q: %d %s", handle, rec.Code, rec.Body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		w.t.Errorf("Cache-Control %q", cc)
	}
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	var tree dto.ForumTree
	if err := dec.Decode(&tree); err != nil {
		w.t.Fatalf("decoding strictly into dto.ForumTree: %v\n%s", err, rec.Body)
	}
	return tree, rec.Body.String()
}

// shape is the tree as "root[child child]" in order.
func shape(tree dto.ForumTree) string {
	var parts []string
	for _, f := range tree.Forums {
		if f.Children == nil {
			parts = append(parts, f.Slug+"(null children)")
			continue
		}
		var kids []string
		for _, c := range f.Children {
			if c.Children == nil || len(c.Children) != 0 {
				kids = append(kids, c.Slug+"(children not [])")
				continue
			}
			kids = append(kids, c.Slug)
		}
		parts = append(parts, f.Slug+"["+strings.Join(kids, " ")+"]")
	}
	return strings.Join(parts, " ")
}

// TestForumTreeShapeAndVisibility is AC1, AC2 and AC4: one level of nesting in
// position order, and a forum the viewer cannot read absent from the body,
// with its children, rather than present and flagged.
func TestForumTreeShapeAndVisibility(t *testing.T) {
	w := newForumWorld(t)
	for _, c := range []struct {
		viewer, want string
		absent       []string
	}{
		// AC4: public forums only, and not mods-public, whose root is hidden.
		{"", "general[g-b] zeta[]", []string{"g-a", "alpha", "g-secret", "mods"}},
		// AC2: a member without crew sees members forums but no restricted
		// one, nor the public child under one.
		{"alice", "general[g-a g-b] alpha[] zeta[]", []string{"g-secret", "mods"}},
		// With the grant, both restricted forums and the child come back.
		{"carol", "general[g-secret g-a g-b] alpha[] zeta[] mods[mods-public]", nil},
	} {
		tree, body := w.tree(c.viewer)
		if got := shape(tree); got != c.want {
			t.Errorf("viewer %q: tree %s, want %s", c.viewer, got, c.want)
		}
		// Absent from the raw body, not only from the decoded struct: a
		// field the DTO does not know would be dropped by decoding.
		for _, slug := range c.absent {
			if strings.Contains(body, `"`+slug) {
				t.Errorf("viewer %q: the body names %q:\n%s", c.viewer, slug, body)
			}
		}
		for _, f := range tree.Forums {
			if f.Unread != 0 && c.viewer == "" {
				t.Errorf("anonymous: %s unread %d", f.Slug, f.Unread)
			}
		}
	}
}

// TestForumTreeCounts is AC3 through the handler: the unread column counts a
// forum's threads with an unread post, from read positions and
// mark_all_read_at, with mutes not counted; thread_count and last_post come
// from the projection.
func TestForumTreeCounts(t *testing.T) {
	w := newForumWorld(t)
	post := func(id threadid.ID, author string) int64 {
		w.post(id, author, "a post", 0)
		return w.clock
	}
	read := func(id threadid.ID, ordinal int64) {
		w.write(func(q *dbq.Queries) error {
			return q.UpsertReadPosition(t.Context(), dbq.UpsertReadPositionParams{UserID: w.users["alice"],
				ThreadID: int64(id), LastReadOrdinal: ordinal, LastReadAt: 1})
		})
	}

	exec := func(stmt string, args ...any) {
		if err := w.db.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			_, err := tx.ExecContext(t.Context(), stmt, args...)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Before the mark: one thread never opened, one opened and read up to
	// date. Both are read.
	old := w.threadIn("general", 1, "Old news")
	post(old, "bob")
	upToDate := w.threadIn("general", 2, "Up to date")
	post(upToDate, "bob")
	post(upToDate, "carol")
	read(upToDate, 2)
	mark := w.clock
	exec(`UPDATE users SET mark_all_read_at = ? WHERE id = ?`, mark, w.users["alice"])
	// After the mark: never opened (unread), read up to post 1 of 2
	// (unread), read to the end (read), and behind but muted (not counted).
	fresh := w.threadIn("general", 3, "Fresh")
	post(fresh, "bob")
	behind := w.threadIn("general", 4, "Behind")
	post(behind, "bob")
	post(behind, "carol")
	read(behind, 1)
	caughtUp := w.threadIn("general", 5, "Caught up")
	post(caughtUp, "bob")
	read(caughtUp, 1)
	muted := w.threadIn("general", 6, "Muted")
	post(muted, "bob")
	exec(`INSERT INTO watches (user_id, thread_id, level, created_at, updated_at) VALUES (?, ?, 'mute', 1, 1)`,
		w.users["alice"], int64(muted))
	// A thread with no post yet counts nowhere.
	w.threadIn("general", 7, "Empty")
	// A child's threads are the child's: not in general's counts.
	child := w.threadIn("g-b", 8, "In the child")
	post(child, "bob")
	last := w.threadIn("zeta", 9, "Latest anywhere")
	lastAt := post(last, "carol")

	tree, _ := w.tree("alice")
	byslug := map[string]dto.Forum{}
	for _, f := range tree.Forums {
		byslug[f.Slug] = f
		for _, c := range f.Children {
			byslug[c.Slug] = c
		}
	}
	for slug, want := range map[string][2]int{"general": {6, 2}, "g-b": {1, 1}, "zeta": {1, 1}, "alpha": {0, 0}} {
		f := byslug[slug]
		if f.ThreadCount != want[0] || f.Unread != want[1] {
			t.Errorf("%s: thread_count %d unread %d, want %d and %d", slug, f.ThreadCount, f.Unread, want[0], want[1])
		}
	}
	// A child's last post is its own, as a root's is.
	if lp := byslug["g-b"].LastPost; lp == nil || lp.Thread.ID != child.String() {
		t.Errorf("g-b's last post: %+v", lp)
	}
	if byslug["alpha"].LastPost != nil {
		t.Errorf("alpha has no threads but a last post: %+v", byslug["alpha"].LastPost)
	}
	// general's latest activity is Muted's post: muting is the reader's, and
	// the column says where the forum's activity is.
	if lp := byslug["general"].LastPost; lp == nil || lp.Thread.ID != muted.String() || lp.Thread.Title != "Muted" ||
		lp.Author.Handle != "bob" {
		t.Errorf("general's last post: %+v", lp)
	}
	want := dto.ForumLastPost{Thread: dto.ThreadLink{ID: last.String(), Slug: "latest-anywhere", Title: "Latest anywhere"},
		Author: dto.Author{Handle: "carol", DisplayName: "Carol Ruiz", Role: domain.RoleMember, Kind: "human"},
		At:     dto.Millis(lastAt)}
	if lp := byslug["zeta"].LastPost; lp == nil || *lp != want {
		t.Errorf("zeta's last post: %+v, want %+v", lp, want)
	}

	// The same tree to someone with nothing read: every thread with a post.
	tree, _ = w.tree("carol")
	for _, f := range tree.Forums {
		if f.Slug == "general" && f.Unread != 6 {
			t.Errorf("carol's general unread %d, want 6", f.Unread)
		}
	}
	// And to nobody: counts and last posts, no unread.
	tree, _ = w.tree("")
	if i := slices.IndexFunc(tree.Forums, func(f dto.Forum) bool { return f.Slug == "general" }); i < 0 ||
		tree.Forums[i].Unread != 0 || tree.Forums[i].ThreadCount != 6 || tree.Forums[i].LastPost == nil {
		t.Errorf("anonymous general: %+v", tree.Forums)
	}
}

// TestForumTreeEmpty: a site with no forum the viewer may read answers an
// empty array, not null.
func TestForumTreeEmpty(t *testing.T) {
	w := &forumWorld{world: newWorld(t), ids: map[string]int64{}}
	w.hideForum()
	_, body := w.tree("")
	if !strings.Contains(body, `"forums":[]`) {
		t.Errorf("body %s", body)
	}
}
