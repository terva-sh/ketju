package threads

import (
	"database/sql"
	"encoding/json"
	"slices"
	"testing"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/respcache"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for merging posts into an existing thread (TKT-01M29CYSBA):
// Service.Merge around movePosts (move_test.go), and the notices the two
// threads carry, drawn per viewer (movedraw.go). They run in a move world
// (move_test.go); the route and the notifier are tested in
// internal/web/api (merge_test.go).

// mergeAs merges ordinals of source into target, as who.
func (w *moveWorld) mergeAs(who string, source, target threadid.ID, ords ...uint32) (dto.MergedPosts, error) {
	w.clock += 1000
	return w.svc.Merge(w.t.Context(), w.as(who), source, ords, target.String())
}

// mustMerge is mergeAs, which must succeed.
func (w *moveWorld) mustMerge(who string, source, target threadid.ID, ords ...uint32) dto.MergedPosts {
	w.t.Helper()
	res, err := w.mergeAs(who, source, target, ords...)
	if err != nil {
		w.t.Fatalf("%s's merge of %v into %s: %v", who, ords, target, err)
	}
	return res
}

// linkOf is thread id's link, as the admin reads it.
func (w *moveWorld) linkOf(id threadid.ID) dto.ThreadLink {
	w.t.Helper()
	h := w.page("admin", id).Body.Thread
	return dto.ThreadLink{ID: h.ID, Slug: h.Slug, Title: h.Title}
}

// staffReader grants rep2 read in the staff forum through a group, so rep2
// reads general, other and staff, and rep1 general and other alone.
func (w *moveWorld) staffReader() {
	w.t.Helper()
	var readers int64
	w.write(func(q *dbq.Queries) error {
		var err error
		readers, err = q.CreateGroup(w.t.Context(), dbq.CreateGroupParams{Name: "Staff readers", Slug: "staff-readers", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	if err := w.svc.Perm.AddMember(w.t.Context(), audit.By{}, readers, w.users["rep2"]); err != nil {
		w.t.Fatal(err)
	}
	if err := w.svc.Perm.Grant(w.t.Context(), audit.By{}, w.staff, readers, domain.PermRead); err != nil {
		w.t.Fatal(err)
	}
}

// TestMergeAcrossForumsNeedsModerateInBoth is criterion 5: a merge into
// another forum needs moderate in both, by role or through a group; a target
// the actor cannot read is not-found, never forbidden, so a merge does not
// confirm that a hidden thread exists; and a refusal writes nothing to
// either thread.
func TestMergeAcrossForumsNeedsModerateInBoth(t *testing.T) {
	w := newMoveWorld(t)
	cases := []struct {
		who    string
		target threadid.ID
		want   problem.Code
	}{
		{"mod", w.otherID, ""},                     // moderator by role
		{"bmod", w.otherID, ""},                    // moderate in both through a group
		{"fmod", w.otherID, problem.CodeForbidden}, // moderate in the source only
		{"omod", w.otherID, problem.CodeForbidden}, // moderate in the target only
		{"rep1", w.otherID, problem.CodeForbidden}, // a member, whom the route's gate refuses first
		{"admin", w.otherID, ""},                   // an admin
		{"fmod", w.staffID, problem.CodeNotFound},  // a target fmod cannot read
		{"mod", w.staffID, problem.CodeNotFound},   // nor a moderator by role
		{"admin", w.staffID, ""},                   // which the admin reads
	}
	for _, c := range cases {
		o := w.postIn(w.id, "author", "to merge by "+c.who, 0)
		was := w.lengths(w.id, c.target)
		res, err := w.mergeAs(c.who, w.id, c.target, o)
		if codeOf(err) != c.want {
			t.Errorf("%s merging into %s: %v, want %q", c.who, c.target, err, c.want)
			continue
		}
		if c.want == "" {
			if res.Source.ID != w.id.String() || res.Target.ID != c.target.String() {
				t.Errorf("%s's merge answered %+v", c.who, res)
			}
			continue
		}
		if !slices.Equal(w.lengths(w.id, c.target), was) {
			t.Errorf("%s's refused merge into %s wrote to a log", c.who, c.target)
		}
	}
}

// TestMergeValidation: what a merge refuses before writing anything: a
// target that is not a thread id, the source itself, a thread that does not
// exist, and the shape of the posts, which movePosts holds as for a split.
func TestMergeValidation(t *testing.T) {
	w := newMoveWorld(t)
	o := w.postIn(w.id, "author", "stays", 0)
	nowhere := threadid.ID(uint64(1_757_500_000_000)<<16 | 999)
	cases := []struct {
		name string
		into string
		ords []uint32
		want problem.Code
	}{
		{"a malformed target", "not a thread", []uint32{o}, problem.CodeValidationFailed},
		{"an empty target", "", []uint32{o}, problem.CodeValidationFailed},
		{"the source itself", w.id.String(), []uint32{o}, problem.CodeValidationFailed},
		{"no such thread", nowhere.String(), []uint32{o}, problem.CodeNotFound},
		{"no posts", w.otherID.String(), nil, problem.CodeValidationFailed},
		{"twice", w.otherID.String(), []uint32{o, o}, problem.CodeValidationFailed},
		{"unknown", w.otherID.String(), []uint32{999}, problem.CodeNotFound},
	}
	for _, c := range cases {
		was := w.lengths(w.id, w.otherID)
		rows, files := w.threadsOnDisk()
		if _, err := w.svc.Merge(t.Context(), w.as("mod"), w.id, c.ords, c.into); codeOf(err) != c.want {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
		if r, f := w.threadsOnDisk(); !slices.Equal(w.lengths(w.id, w.otherID), was) || r != rows || f != files {
			t.Errorf("%s: the refused merge wrote something", c.name)
		}
	}
	if _, err := w.svc.Merge(t.Context(), domain.Anonymous(), w.id, []uint32{o}, w.otherID.String()); err == nil {
		t.Error("an anonymous merge was allowed")
	}
}

// TestMergeNoticesRevealOnlyToReaders is half of criterion 6, both ways
// round. A merge from general into staff: a reader of both (the admin,
// rep2) is told where the posts went and, in staff, where they came from; a
// reader of general alone (a member, moderators of general, anonymous) gets
// the count with no thread and cannot open staff. A merge from staff into
// general: a reader of general alone reads the copies and the count, and
// nothing that names staff.
func TestMergeNoticesRevealOnlyToReaders(t *testing.T) {
	w := newMoveWorld(t)
	w.staffReader()
	general, staff := w.linkOf(w.id), w.linkOf(w.staffID)
	a := w.postIn(w.id, "author", "first to staff", 0)
	b := w.postIn(w.id, "author", "second to staff", 0)
	res := w.mustMerge("admin", w.id, w.staffID, a, b)
	first := res.Target.PostCount - 1
	for _, who := range []string{"admin", "rep2", "rep1", "fmod", "mod", ""} {
		reads := who == "admin" || who == "rep2"
		page := w.page(who, w.id)
		pa, pb := page.Body.Posts[a-1], page.Body.Posts[b-1]
		if pa.MoveNotice == nil || pa.MoveNotice.Count != 2 || pb.MoveNotice != nil {
			t.Fatalf("%s reads the source notices as %+v and %+v", who, pa.MoveNotice, pb.MoveNotice)
		}
		if !reads {
			if pa.MovedTo != nil || pb.MovedTo != nil || pa.MoveNotice.Thread != nil || pa.MoveNotice.Ordinal != nil {
				t.Errorf("%s, who may not read staff, is told where the posts went: %+v %+v", who, pa.MovedTo, pa.MoveNotice)
			}
			leaks(t, who, page.Body, w.staffID.String(), staff.Title, staff.Slug)
			if _, err := w.svc.View(t.Context(), w.viewerOf(who), w.staffID, Query{PageSize: 50}, ""); err == nil {
				t.Errorf("%s opened staff", who)
			}
			continue
		}
		if n := pa.MoveNotice; n.Thread == nil || *n.Thread != staff || n.Ordinal == nil || *n.Ordinal != first {
			t.Errorf("%s reads the source notice as %+v, want staff at %d", who, n, first)
		}
		copies := w.page(who, w.staffID).Body.Posts
		c1, c2 := copies[first-1], copies[first]
		if n := c1.MergeNotice; n == nil || n.Count != 2 || n.Thread == nil || *n.Thread != general || n.Ordinal == nil || *n.Ordinal != int(a) {
			t.Errorf("%s reads the merge notice as %+v, want general at %d", who, n, a)
		}
		if c2.MergeNotice != nil {
			t.Errorf("%s reads a merge notice on the second copy: %+v", who, c2.MergeNotice)
		}
	}

	s := w.postIn(w.staffID, "author", "STAFF-WORDS-TO-GENERAL", 0)
	into := w.mustMerge("admin", w.staffID, w.id, s).Target.PostCount
	for _, who := range []string{"rep1", "rep2", ""} {
		page := w.page(who, w.id)
		c := page.Body.Posts[into-1]
		if c.HTML == "" || c.MergeNotice == nil || c.MergeNotice.Count != 1 {
			t.Fatalf("%s reads the copy from staff as %+v", who, c)
		}
		if who == "rep2" {
			if n := c.MergeNotice; n.Thread == nil || *n.Thread != staff || n.Ordinal == nil || *n.Ordinal != int(s) {
				t.Errorf("rep2 reads the merge notice as %+v", n)
			}
			continue
		}
		if c.MergeNotice.Thread != nil || c.MergeNotice.Ordinal != nil {
			t.Errorf("%s, who may not read staff, is told where the copy came from: %+v", who, c.MergeNotice)
		}
		leaks(t, who, page.Body, w.staffID.String(), staff.Title, staff.Slug)
		p, err := w.svc.Post(t.Context(), w.viewerOf(who), w.id, uint32(into), "")
		if err != nil {
			t.Fatal(err)
		}
		if p.Body.MergeNotice == nil || p.Body.MergeNotice.Thread != nil {
			t.Errorf("%s reads the copy alone as %+v", who, p.Body)
		}
		leaks(t, who, p.Body, w.staffID.String(), staff.Title)
	}
}

// TestMergeNoticeNotInCachedPosts is the other half of criterion 6: two
// members who differ only in reading the source are served different
// bodies and ETags for the target's page, the second from the cache, and
// the cache holds neither's notice thread.
func TestMergeNoticeNotInCachedPosts(t *testing.T) {
	w := newMoveWorld(t)
	w.svc.Responses = respcache.New(1 << 20)
	w.staffReader()
	s := w.postIn(w.staffID, "author", "from staff", 0)
	into := w.mustMerge("admin", w.staffID, w.id, s).Target.PostCount

	before := w.svc.Responses.Counters()
	blind := w.page("rep1", w.id)
	mid := w.svc.Responses.Counters()
	sees := w.page("rep2", w.id)
	after := w.svc.Responses.Counters()
	if mid.Misses != before.Misses+1 || after.Hits != mid.Hits+1 {
		t.Errorf("the second member's page was not served from the cache: %+v, %+v, %+v", before, mid, after)
	}
	if n := blind.Body.Posts[into-1].MergeNotice; n == nil || n.Thread != nil {
		t.Errorf("rep1 reads the merge notice %+v", n)
	}
	if n := sees.Body.Posts[into-1].MergeNotice; n == nil || n.Thread == nil || n.Thread.ID != w.staffID.String() {
		t.Errorf("rep2 reads the merge notice %+v", n)
	}
	if blind.ETag == sees.ETag {
		t.Error("the two members' pages share an ETag")
	}
	again := w.page("rep1", w.id)
	if n := again.Body.Posts[into-1].MergeNotice; n == nil || n.Thread != nil || again.ETag != blind.ETag {
		t.Errorf("rep1's page after rep2's: notice %+v, ETag %s, was %s", n, again.ETag, blind.ETag)
	}
	if res, err := w.svc.View(t.Context(), w.as("rep1"), w.id, Query{PageSize: 50}, sees.ETag); err != nil || res.NotModified {
		t.Errorf("rep1 sending rep2's ETag: %+v, %v", res.NotModified, err)
	}
}

// TestMergeRefusesArchived is criterion 7: a merge into an archived thread
// or out of one is conflict, to moderators and admins alike, and writes
// nothing to either thread.
func TestMergeRefusesArchived(t *testing.T) {
	for _, side := range []string{"target", "source"} {
		w := newMoveWorld(t)
		o := w.postIn(w.id, "author", "stays", 0)
		archived := w.otherID
		if side == "source" {
			archived = w.id
		}
		if _, err := w.svc.Archive(t.Context(), w.as("admin"), archived, "done"); err != nil {
			t.Fatal(err)
		}
		for _, who := range []string{"mod", "bmod", "admin"} {
			was := w.lengths(w.id, w.otherID)
			if _, err := w.mergeAs(who, w.id, w.otherID, o); codeOf(err) != problem.CodeConflict {
				t.Errorf("%s merging with an archived %s: %v, want conflict", who, side, err)
			}
			if !slices.Equal(w.lengths(w.id, w.otherID), was) {
				t.Errorf("%s's refused merge with an archived %s wrote something", who, side)
			}
		}
	}
}

// TestMergeIntoArchivedForumAdminOnly: a merge into a thread of an archived
// forum is conflict to a moderator and allowed to an admin.
func TestMergeIntoArchivedForumAdminOnly(t *testing.T) {
	w := newMoveWorld(t)
	if err := w.svc.Perm.SetForumArchived(t.Context(), audit.By{}, w.other, true); err != nil {
		t.Fatal(err)
	}
	o := w.postIn(w.id, "author", "to the archive", 0)
	for _, who := range []string{"bmod", "mod"} {
		was := w.lengths(w.id, w.otherID)
		if _, err := w.mergeAs(who, w.id, w.otherID, o); codeOf(err) != problem.CodeConflict {
			t.Errorf("%s merging into an archived forum: %v, want conflict", who, err)
		}
		if !slices.Equal(w.lengths(w.id, w.otherID), was) {
			t.Errorf("%s's refused merge wrote something", who)
		}
	}
	if _, err := w.mergeAs("admin", w.id, w.otherID, o); err != nil {
		t.Errorf("the admin's merge into an archived forum: %v", err)
	}
}

// TestMergeAppendsAtEndInSourceOrder is the ordering half of criterion 4,
// docs/06's run decision: the copies land after every post of the target,
// older than its latest though they are, in their source order whatever
// order the request named them in, the first carrying the merge notice.
func TestMergeAppendsAtEndInSourceOrder(t *testing.T) {
	w := newMoveWorld(t)
	a := w.postIn(w.id, "author", "a, oldest", 0)
	b := w.postIn(w.id, "rep1", "b", 0)
	c := w.postIn(w.id, "rep2", "c, newest in general", 0)
	w.clock += 1_000_000
	w.postIn(w.otherID, "rep1", "newer than all of general", 0)
	src := w.page("rep1", w.id).Body.Posts
	res := w.mustMerge("mod", w.id, w.otherID, c, a, b)
	if res.Target.PostCount != 5 {
		t.Fatalf("the target holds %d posts, want 5", res.Target.PostCount)
	}
	got := w.page("rep1", w.otherID).Body.Posts
	if len(got) != 5 {
		t.Fatalf("the target serves %d posts", len(got))
	}
	for i, o := range []uint32{a, b, c} {
		p := got[2+i]
		if p.Ordinal != 3+i || p.HTML != src[o-1].HTML {
			t.Errorf("target post %d is %q, want the copy of general's %d %q", p.Ordinal, p.HTML, o, src[o-1].HTML)
		}
		if p.CreatedAt >= got[1].CreatedAt {
			t.Errorf("copy %d is dated %d, after the target's latest post at %d", p.Ordinal, p.CreatedAt, got[1].CreatedAt)
		}
		if notice := i == 0; (p.MergeNotice != nil) != notice {
			t.Errorf("copy %d's merge notice is %+v", p.Ordinal, p.MergeNotice)
		}
	}
	if n := got[2].MergeNotice; n == nil || n.Count != 3 || n.Ordinal == nil || *n.Ordinal != int(a) {
		t.Errorf("the merge notice is %+v, want 3 posts from general's %d", n, a)
	}
}

// TestMergeKeepsAuthorsTimesAndOrigin is criterion 1: each copy keeps its
// original's author, time and text, and its Post record names the original.
func TestMergeKeepsAuthorsTimesAndOrigin(t *testing.T) {
	w := newMoveWorld(t)
	a := w.postIn(w.id, "author", "by the author", 0)
	b := w.postIn(w.id, "rep2", "by rep2", 0)
	src := w.page("admin", w.id).Body.Posts
	w.clock += 1_000_000
	res := w.mustMerge("mod", w.id, w.otherID, a, b)
	got := w.page("rep1", w.otherID).Body.Posts
	for i, o := range []uint32{a, b} {
		copyAt := res.Target.PostCount - 1 + i
		c, want := got[copyAt-1], src[o-1]
		if c.Author != want.Author || c.CreatedAt != want.CreatedAt || c.HTML != want.HTML {
			t.Errorf("copy %d is %+v, want the original %+v", copyAt, c, want)
		}
		chain := w.chainOf(w.otherID, uint32(copyAt))
		if len(chain) == 0 || chain[0].Post == nil || chain[0].Post.Origin == nil ||
			*chain[0].Post.Origin != (threadlog.PostOrigin{ThreadID: uint64(w.id), Ordinal: o}) {
			t.Errorf("copy %d's Post names its origin as %+v, want %s/%d", copyAt, chain, w.id, o)
		}
	}
}

// TestMergeNoticesBothWays is criterion 2: to a reader of both, the
// source's first moved post names the target and the first copy, each
// moved post its own copy, and the target's first copy names the source
// and the first moved post, on the page and the post alone.
func TestMergeNoticesBothWays(t *testing.T) {
	w := newMoveWorld(t)
	general, other := w.linkOf(w.id), w.linkOf(w.otherID)
	a := w.postIn(w.id, "author", "one", 0)
	b := w.postIn(w.id, "author", "two", 0)
	res := w.mustMerge("mod", w.id, w.otherID, a, b)
	if res.Source.ID != general.ID || res.Target.ID != other.ID {
		t.Errorf("the merge answered %+v", res)
	}
	first := res.Target.PostCount - 1
	posts := w.page("rep1", w.id).Body.Posts
	pa, pb := posts[a-1], posts[b-1]
	if n := pa.MoveNotice; n == nil || n.Count != 2 || n.Thread == nil || *n.Thread != other || n.Ordinal == nil || *n.Ordinal != first {
		t.Errorf("the source notice is %+v, want 2 posts to other at %d", n, first)
	}
	for i, p := range []dto.Post{pa, pb} {
		if m := p.MovedTo; m == nil || m.Thread != other || m.Ordinal == nil || *m.Ordinal != first+i {
			t.Errorf("post %d's moved_to is %+v, want other's %d", p.Ordinal, m, first+i)
		}
	}
	want := dto.MoveNotice{Count: 2, Thread: &general, Ordinal: new(int(a))}
	c := w.page("rep1", w.otherID).Body.Posts[first-1]
	alone, err := w.svc.Post(t.Context(), w.as("rep1"), w.otherID, uint32(first), "")
	if err != nil {
		t.Fatal(err)
	}
	for where, n := range map[string]*dto.MoveNotice{"page": c.MergeNotice, "post": alone.Body.MergeNotice} {
		got, _ := json.Marshal(n)
		exp, _ := json.Marshal(want)
		if string(got) != string(exp) {
			t.Errorf("the %s's merge notice is %s, want %s", where, got, exp)
		}
	}
}

// TestMergeReplyToInsideSetResolves is criterion 3: a copied reply to a
// post merged with it answers that post's copy, and one to a post left
// behind answers nothing.
func TestMergeReplyToInsideSetResolves(t *testing.T) {
	w := newMoveWorld(t)
	a := w.postIn(w.id, "author", "asked", 0)
	b := w.postIn(w.id, "rep1", "answered", a)
	c := w.postIn(w.id, "rep2", "to the opener", 1)
	res := w.mustMerge("mod", w.id, w.otherID, a, b, c)
	first := res.Target.PostCount - 2
	got := w.page("rep1", w.otherID).Body.Posts
	if r := got[first-1].ReplyTo; r != nil {
		t.Errorf("the copy of a top-level post answers %d", *r)
	}
	if r := got[first].ReplyTo; r == nil || *r != first {
		t.Errorf("the copied reply answers %v, want the copy at %d", r, first)
	}
	if r := got[first+1].ReplyTo; r != nil {
		t.Errorf("the copy of a reply to a post left behind answers %d", *r)
	}
}

// TestMergeBackRestoresReading: reversal is the opposite merge. Merging the
// copies back makes copies of the copies at the end of the source, which
// read as the originals did and name the intermediate copies as their
// origin; the original ordinals stay moved and point at the first copies,
// which are moved in turn and point back.
func TestMergeBackRestoresReading(t *testing.T) {
	w := newMoveWorld(t)
	a := w.postIn(w.id, "author", "asked", 0)
	b := w.postIn(w.id, "rep1", "answered", a)
	w.postIn(w.id, "rep2", "stays", 0)
	orig := w.page("rep1", w.id).Body.Posts
	there := w.mustMerge("mod", w.id, w.otherID, a, b).Target.PostCount - 1
	back := w.mustMerge("mod", w.otherID, w.id, uint32(there), uint32(there+1)).Target.PostCount - 1

	posts := w.page("rep1", w.id).Body.Posts
	for i, o := range []uint32{a, b} {
		p, c := posts[o-1], posts[back-1+i]
		if !p.Moved || p.MovedTo == nil || p.MovedTo.Thread.ID != w.otherID.String() || *p.MovedTo.Ordinal != there+i {
			t.Errorf("original %d reads %+v, want moved to other's %d", o, p, there+i)
		}
		if c.Moved || c.HTML != orig[o-1].HTML || c.Author != orig[o-1].Author || c.CreatedAt != orig[o-1].CreatedAt {
			t.Errorf("the copy back at %d reads %+v, want the original %+v", back+i, c, orig[o-1])
		}
		chain := w.chainOf(w.id, uint32(back+i))
		if len(chain) == 0 || chain[0].Post == nil || chain[0].Post.Origin == nil ||
			*chain[0].Post.Origin != (threadlog.PostOrigin{ThreadID: uint64(w.otherID), Ordinal: uint32(there + i)}) {
			t.Errorf("the copy back at %d names its origin as %+v", back+i, chain)
		}
	}
	if r := posts[back].ReplyTo; r == nil || *r != back {
		t.Errorf("the reply copied back answers %v, want %d", r, back)
	}
	if n := posts[back-1].MergeNotice; n == nil || n.Thread == nil || n.Thread.ID != w.otherID.String() || *n.Ordinal != there {
		t.Errorf("the merge back's notice is %+v", n)
	}
	mid := w.page("rep1", w.otherID).Body.Posts
	for i := range 2 {
		p := mid[there-1+i]
		if !p.Moved || p.MovedTo == nil || p.MovedTo.Thread.ID != w.id.String() || *p.MovedTo.Ordinal != back+i {
			t.Errorf("other's copy %d reads %+v, want moved back to general's %d", there+i, p, back+i)
		}
	}
}

// TestMergeDoesNotBumpThread is the bump half of criterion 4: merging a
// post newer than every post of the target leaves the target's threads row
// naming the same last post, author and time, which is what every thread
// list sorts and draws by, so the thread does not rise in its forum's list.
func TestMergeDoesNotBumpThread(t *testing.T) {
	w := newMoveWorld(t)
	w.postIn(w.otherID, "rep1", "the target's latest", 0)
	row := func() dbq.Thread {
		t.Helper()
		w.page("rep1", w.otherID) // projects the target
		var r dbq.Thread
		if err := w.db.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
			var err error
			r, err = q.GetThread(t.Context(), int64(w.otherID))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return r
	}
	before := row()
	w.clock += 1_000_000
	o := w.postIn(w.id, "rep2", "newer than the whole target", 0)
	res := w.mustMerge("mod", w.id, w.otherID, o)
	after := row()
	if after.LastPostAt != before.LastPostAt || after.LastPostAuthorID != before.LastPostAuthorID ||
		after.LastPostOrdinal != before.LastPostOrdinal {
		t.Errorf("the target's last post is %d by %d at %d, was %d by %d at %d", after.LastPostOrdinal,
			after.LastPostAuthorID, after.LastPostAt, before.LastPostOrdinal, before.LastPostAuthorID, before.LastPostAt)
	}
	if after.PostCount != before.PostCount+1 || res.Target.PostCount != int(before.PostCount)+1 {
		t.Errorf("the target counts %d posts, answered %d, want %d", after.PostCount, res.Target.PostCount, before.PostCount+1)
	}
}
