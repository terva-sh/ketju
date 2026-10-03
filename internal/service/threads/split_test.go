package threads

import (
	"database/sql"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
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

// Tests for splitting posts into a new thread (TKT-01M29CYSAM): Service.Split
// around movePosts (move_test.go), and what the two threads say about each
// other, drawn per viewer (movedraw.go). They run in a move world.

// splitAs splits ordinals of source into a new thread in forum, as who.
func (w *moveWorld) splitAs(who string, source threadid.ID, forum, title string, ords ...uint32) (dto.MovedPosts, error) {
	w.clock += 1000
	return w.svc.Split(w.t.Context(), w.as(who), source, ords, forum, title)
}

// mustSplit is splitAs, which must succeed; it answers the new thread.
func (w *moveWorld) mustSplit(who string, source threadid.ID, forum, title string, ords ...uint32) threadid.ID {
	w.t.Helper()
	res, err := w.splitAs(who, source, forum, title, ords...)
	if err != nil {
		w.t.Fatalf("%s's split of %v into %s: %v", who, ords, forum, err)
	}
	id, err := threadid.Parse(res.Thread.ID)
	if err != nil {
		w.t.Fatal(err)
	}
	return id
}

// viewerOf is who as a viewer, "" being anonymous.
func (w *moveWorld) viewerOf(who string) domain.Viewer {
	if who == "" {
		return domain.Anonymous()
	}
	return w.as(who)
}

// page is thread id's first page of 50 as who reads it.
func (w *moveWorld) page(who string, id threadid.ID) Result {
	w.t.Helper()
	res, err := w.svc.View(w.t.Context(), w.viewerOf(who), id, Query{PageSize: 50}, "")
	if err != nil {
		w.t.Fatalf("%s reading %s: %v", who, id, err)
	}
	return res
}

// threadsOnDisk counts the threads the database and the data directory
// hold, to prove a refusal opened none.
func (w *moveWorld) threadsOnDisk() (rows, files int) {
	w.t.Helper()
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), `SELECT COUNT(*) FROM threads`).Scan(&rows)
	}); err != nil {
		w.t.Fatal(err)
	}
	if err := filepath.WalkDir(w.root, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files++
		}
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return rows, files
}

// leaks fails if body, marshalled, names any of words.
func leaks(t *testing.T, who string, body any, words ...string) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range words {
		if strings.Contains(string(b), s) {
			t.Errorf("%s is served %q:\n%s", who, s, b)
		}
	}
}

// TestSplitAcrossForumsNeedsModerateInBoth: a split into another forum
// needs moderate in both, by role or through a group, and a refusal writes
// nothing: the source log is unchanged and no thread is opened.
func TestSplitAcrossForumsNeedsModerateInBoth(t *testing.T) {
	w := newMoveWorld(t)
	cases := []struct {
		who  string
		want problem.Code
	}{
		{"mod", ""},                     // moderator by role
		{"bmod", ""},                    // moderate in both through a group
		{"fmod", problem.CodeForbidden}, // moderate in the source only
		{"omod", problem.CodeForbidden}, // moderate in the target only
		{"rep1", problem.CodeForbidden}, // a member, whom the route's gate refuses first
		{"admin", ""},                   // an admin
	}
	for _, c := range cases {
		o := w.postIn(w.id, "author", "to split by "+c.who, 0)
		was := w.lengths(w.id)
		rows, files := w.threadsOnDisk()
		res, err := w.splitAs(c.who, w.id, "other", "Split by "+c.who, o)
		if codeOf(err) != c.want {
			t.Errorf("%s splitting into other: %v, want %q", c.who, err, c.want)
			continue
		}
		if c.want == "" {
			if res.Source.ID != w.id.String() || res.Thread.Forum.Slug != "other" || res.Thread.Title != "Split by "+c.who {
				t.Errorf("%s's split answered %+v", c.who, res)
			}
			continue
		}
		if !slices.Equal(w.lengths(w.id), was) {
			t.Errorf("%s's refused split wrote to the source", c.who)
		}
		if r, f := w.threadsOnDisk(); r != rows || f != files {
			t.Errorf("%s's refused split opened a thread: %d rows and %d files, were %d and %d", c.who, r, f, rows, files)
		}
	}
}

// TestSplitNoticeRevealsTargetOnlyToReaders: a viewer of both forums gets
// moved_to and the notice's thread; a reader of the source alone, member,
// moderator of the source or anonymous, gets moved and the count with
// both null, and nothing that names the target, on the page and the post.
func TestSplitNoticeRevealsTargetOnlyToReaders(t *testing.T) {
	w := newMoveWorld(t)
	a := w.postIn(w.id, "author", "first to move", 0)
	b := w.postIn(w.id, "author", "second to move", 0)
	target := w.mustSplit("admin", w.id, "staff", "Staff side thread", a, b)
	for _, who := range []string{"admin", "rep1", "fmod", "mod", ""} {
		reads := who == "admin"
		page := w.page(who, w.id)
		pa, pb := page.Body.Posts[a-1], page.Body.Posts[b-1]
		for _, p := range []dto.Post{pa, pb} {
			if !p.Moved || !p.Deleted || p.Removal != nil || p.HTML != "" {
				t.Errorf("%s reads moved post %d as %+v", who, p.Ordinal, p)
			}
		}
		if pb.MoveNotice != nil {
			t.Errorf("%s reads a notice on the move's second post: %+v", who, pb.MoveNotice)
		}
		if pa.MoveNotice == nil || pa.MoveNotice.Count != 2 {
			t.Fatalf("%s reads the move's notice as %+v", who, pa.MoveNotice)
		}
		if !reads {
			if pa.MovedTo != nil || pb.MovedTo != nil || pa.MoveNotice.Thread != nil || pa.MoveNotice.Ordinal != nil {
				t.Errorf("%s, who may not read the target, is told where it is: %+v %+v %+v", who, pa.MovedTo, pb.MovedTo, pa.MoveNotice)
			}
			leaks(t, who, page.Body, target.String(), "Staff side thread", "staff-side-thread")
			p, err := w.svc.Post(t.Context(), w.viewerOf(who), w.id, a, "")
			if err != nil {
				t.Fatal(err)
			}
			if p.Body.MovedTo != nil || p.Body.MoveNotice == nil || p.Body.MoveNotice.Thread != nil {
				t.Errorf("%s reads the moved post alone as %+v", who, p.Body)
			}
			leaks(t, who, p.Body, target.String(), "Staff side thread")
			continue
		}
		want := dto.ThreadLink{ID: target.String(), Slug: "staff-side-thread", Title: "Staff side thread"}
		if pa.MovedTo == nil || pa.MovedTo.Thread != want || pa.MovedTo.Ordinal == nil || *pa.MovedTo.Ordinal != 1 {
			t.Errorf("%s reads the first moved post's moved_to as %+v", who, pa.MovedTo)
		}
		if pb.MovedTo == nil || pb.MovedTo.Ordinal == nil || *pb.MovedTo.Ordinal != 2 {
			t.Errorf("%s reads the second moved post's moved_to as %+v", who, pb.MovedTo)
		}
		if n := pa.MoveNotice; n.Thread == nil || *n.Thread != want || n.Ordinal == nil || *n.Ordinal != 1 {
			t.Errorf("%s reads the notice as %+v", who, n)
		}
	}
}

// TestSplitFromRevealsSourceOnlyToReaders: the split thread's header names
// the thread it came from to a reader of that thread, and to anyone else
// says only that it was split, since a title is content.
func TestSplitFromRevealsSourceOnlyToReaders(t *testing.T) {
	w := newMoveWorld(t)
	o := w.postIn(w.staffID, "author", "out of staff", 0)
	res, err := w.splitAs("admin", w.staffID, "general", "Now in general", o)
	if err != nil {
		t.Fatal(err)
	}
	want := dto.ThreadLink{ID: w.staffID.String(), Slug: "thread-3", Title: "Thread 3"}
	if sf := res.Thread.SplitFrom; sf == nil || sf.Thread == nil || *sf.Thread != want {
		t.Errorf("the split answered the new thread's split_from as %+v", sf)
	}
	if res.Source.SplitFrom != nil {
		t.Errorf("the source's header says it was split from %+v", res.Source.SplitFrom)
	}
	target, _ := threadid.Parse(res.Thread.ID)
	for _, who := range []string{"admin", "rep1", "fmod", ""} {
		page := w.page(who, target)
		sf := page.Body.Thread.SplitFrom
		if sf == nil {
			t.Errorf("%s is not told the thread was split", who)
			continue
		}
		if who == "admin" {
			if sf.Thread == nil || *sf.Thread != want {
				t.Errorf("the admin reads split_from as %+v", sf.Thread)
			}
			continue
		}
		if sf.Thread != nil {
			t.Errorf("%s, who may not read staff, reads split_from as %+v", who, sf.Thread)
		}
		leaks(t, who, page.Body, w.staffID.String(), "Thread 3", "thread-3")
	}
	// The moderation answers draw the header the same way.
	h, err := w.svc.Lock(t.Context(), w.as("fmod"), target, "")
	if err != nil {
		t.Fatal(err)
	}
	if h.SplitFrom == nil || h.SplitFrom.Thread != nil {
		t.Errorf("fmod's lock answered split_from %+v", h.SplitFrom)
	}
	// A thread no split opened has none.
	if sf := w.page("admin", w.otherID).Body.Thread.SplitFrom; sf != nil {
		t.Errorf("an ordinary thread reads as split from %+v", sf)
	}
}

// TestMovedToNamesCopyOrdinal: moved_to.ordinal is the copy's own ordinal
// in the target, never the source's, and is null while the copy is not
// written; the notice's ordinal is the move's first copy.
func TestMovedToNamesCopyOrdinal(t *testing.T) {
	w := newMoveWorld(t)
	for i := 2; i <= 30; i++ {
		w.postIn(w.id, "author", "post", 0)
	}
	w.mustSplit("mod", w.id, "other", "Twenty and thirty", 20, 30)
	posts := w.page("rep1", w.id).Body.Posts
	for _, c := range []struct{ source, copy int }{{20, 1}, {30, 2}} {
		m := posts[c.source-1].MovedTo
		if m == nil || m.Ordinal == nil || *m.Ordinal != c.copy {
			t.Errorf("post %d's moved_to is %+v, want the copy at %d", c.source, m, c.copy)
		}
	}

	// A merge into other whose run stopped after its PostsMergedIn and the
	// first copy: the second copy is not written yet.
	cw, _ := crashed(t, 3)
	if _, err := cw.proj.ProjectThread(t.Context(), cw.otherID, cw.logOf(cw.otherID)); err != nil {
		t.Fatal(err)
	}
	check := func(want3 *int) {
		t.Helper()
		posts := cw.page("rep1", cw.id).Body.Posts
		p2, p3 := posts[1], posts[2]
		if p2.MovedTo == nil || p2.MovedTo.Thread.ID != cw.otherID.String() || p2.MovedTo.Ordinal == nil || *p2.MovedTo.Ordinal != 2 {
			t.Errorf("post 2's moved_to is %+v, want other's 2", p2.MovedTo)
		}
		if n := p2.MoveNotice; n == nil || n.Count != 2 || n.Ordinal == nil || *n.Ordinal != 2 {
			t.Errorf("the notice is %+v, want 2 posts from other's 2", n)
		}
		if p3.MovedTo == nil || p3.MovedTo.Thread.ID != cw.otherID.String() {
			t.Fatalf("post 3's moved_to is %+v", p3.MovedTo)
		}
		if got := p3.MovedTo.Ordinal; (got == nil) != (want3 == nil) || (got != nil && *got != *want3) {
			t.Errorf("post 3's moved_to.ordinal is %v, want %v", got, want3)
		}
	}
	check(nil)
	cw.finish(1)
	three := 3
	check(&three)
}

// TestMovedToNotInCachedPosts: two members of one viewer class who differ
// only in reading the target are served different bodies and ETags, the
// second from the cache, and the cache holds neither's moved_to.
func TestMovedToNotInCachedPosts(t *testing.T) {
	w := newMoveWorld(t)
	w.svc.Responses = respcache.New(1 << 20)
	var readers int64
	w.write(func(q *dbq.Queries) error {
		var err error
		readers, err = q.CreateGroup(t.Context(), dbq.CreateGroupParams{Name: "Staff readers", Slug: "staff-readers", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	if err := w.svc.Perm.AddMember(t.Context(), audit.By{}, readers, w.users["rep2"]); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Perm.Grant(t.Context(), audit.By{}, w.staff, readers, domain.PermRead); err != nil {
		t.Fatal(err)
	}
	o := w.postIn(w.id, "author", "for staff", 0)
	target := w.mustSplit("admin", w.id, "staff", "For staff", o)

	before := w.svc.Responses.Counters()
	blind := w.page("rep1", w.id)
	mid := w.svc.Responses.Counters()
	sees := w.page("rep2", w.id)
	after := w.svc.Responses.Counters()
	if mid.Misses != before.Misses+1 || after.Hits != mid.Hits+1 {
		t.Errorf("the second member's page was not served from the cache: %+v, %+v, %+v", before, mid, after)
	}
	if blind.Body.Posts[o-1].MovedTo != nil {
		t.Errorf("rep1 reads moved_to %+v", blind.Body.Posts[o-1].MovedTo)
	}
	if m := sees.Body.Posts[o-1].MovedTo; m == nil || m.Thread.ID != target.String() {
		t.Errorf("rep2 reads moved_to %+v", m)
	}
	if blind.ETag == sees.ETag {
		t.Error("the two members' pages share an ETag")
	}
	// The cache answered rep2 and still answers rep1 as before.
	again := w.page("rep1", w.id)
	if again.Body.Posts[o-1].MovedTo != nil || again.ETag != blind.ETag {
		t.Errorf("rep1's page after rep2's: moved_to %+v, ETag %s, was %s", again.Body.Posts[o-1].MovedTo, again.ETag, blind.ETag)
	}
	// And the page rep2 had is not revalidated for rep1.
	if res, err := w.svc.View(t.Context(), w.as("rep1"), w.id, Query{PageSize: 50}, sees.ETag); err != nil || res.NotModified {
		t.Errorf("rep1 sending rep2's ETag: %+v, %v", res.NotModified, err)
	}
}

// TestSplitRefusesArchived: a split out of an archived thread is conflict,
// to moderators and admins alike, and writes nothing.
func TestSplitRefusesArchived(t *testing.T) {
	w := newMoveWorld(t)
	o := w.postIn(w.id, "author", "stays", 0)
	if _, err := w.svc.Archive(t.Context(), w.as("admin"), w.id, "done"); err != nil {
		t.Fatal(err)
	}
	for _, who := range []string{"mod", "fmod", "admin"} {
		was := w.lengths(w.id)
		rows, files := w.threadsOnDisk()
		if _, err := w.splitAs(who, w.id, "general", "Out of the archive", o); codeOf(err) != problem.CodeConflict {
			t.Errorf("%s splitting an archived thread: %v, want conflict", who, err)
		}
		if r, f := w.threadsOnDisk(); !slices.Equal(w.lengths(w.id), was) || r != rows || f != files {
			t.Errorf("%s's refused split wrote something", who)
		}
	}
}

// TestSplitIntoArchivedForumAdminOnly: a split into an archived forum is
// conflict to a moderator and allowed to an admin.
func TestSplitIntoArchivedForumAdminOnly(t *testing.T) {
	w := newMoveWorld(t)
	if err := w.svc.Perm.SetForumArchived(t.Context(), audit.By{}, w.other, true); err != nil {
		t.Fatal(err)
	}
	o := w.postIn(w.id, "author", "to the archive", 0)
	for _, who := range []string{"bmod", "mod"} {
		was := w.lengths(w.id)
		rows, files := w.threadsOnDisk()
		if _, err := w.splitAs(who, w.id, "other", "Into the archive", o); codeOf(err) != problem.CodeConflict {
			t.Errorf("%s splitting into an archived forum: %v, want conflict", who, err)
		}
		if r, f := w.threadsOnDisk(); !slices.Equal(w.lengths(w.id), was) || r != rows || f != files {
			t.Errorf("%s's refused split wrote something", who)
		}
	}
	if _, err := w.splitAs("admin", w.id, "other", "Into the archive", o); err != nil {
		t.Errorf("the admin's split into an archived forum: %v", err)
	}
}

// TestSplitKeepsSourceOrdinals is criterion 1: the source keeps every
// ordinal, the moved ones served moved, with the notice at the first.
func TestSplitKeepsSourceOrdinals(t *testing.T) {
	w := newMoveWorld(t)
	for i := 2; i <= 5; i++ {
		w.postIn(w.id, "author", "post", 0)
	}
	w.mustSplit("mod", w.id, "general", "Two and four", 2, 4)
	page := w.page("rep1", w.id)
	if page.Body.Thread.PostCount != 5 || len(page.Body.Posts) != 5 {
		t.Fatalf("the source holds %d posts, %d served, want 5", page.Body.Thread.PostCount, len(page.Body.Posts))
	}
	for i, p := range page.Body.Posts {
		moved := p.Ordinal == 2 || p.Ordinal == 4
		if p.Ordinal != i+1 || p.Moved != moved || p.Deleted != moved {
			t.Errorf("source post %d: %+v", i+1, p)
		}
		if notice := p.Ordinal == 2; (p.MoveNotice != nil) != notice {
			t.Errorf("post %d's notice is %+v", p.Ordinal, p.MoveNotice)
		}
	}
}

// TestSplitThreadKeepsAuthorsAndTimes is criterion 2: the copies keep
// their authors and times, the new thread is started by the moderator who
// split it, and links back to the source.
func TestSplitThreadKeepsAuthorsAndTimes(t *testing.T) {
	w := newMoveWorld(t)
	a := w.postIn(w.id, "author", "by the author", 0)
	b := w.postIn(w.id, "rep2", "by rep2", a)
	src := w.page("admin", w.id).Body.Posts
	target := w.mustSplit("mod", w.id, "general", "Kept", a, b)
	page := w.page("rep1", target)
	if page.Body.Thread.Author.Handle != "mod" {
		t.Errorf("the split thread's starter is %+v, want the moderator", page.Body.Thread.Author)
	}
	if sf := page.Body.Thread.SplitFrom; sf == nil || sf.Thread == nil || sf.Thread.ID != w.id.String() {
		t.Errorf("the split thread's split_from is %+v", sf)
	}
	got := page.Body.Posts
	if len(got) != 2 {
		t.Fatalf("the split thread holds %d posts", len(got))
	}
	for i, o := range []uint32{a, b} {
		want := src[o-1]
		if got[i].Author != want.Author || got[i].CreatedAt != want.CreatedAt || got[i].HTML != want.HTML {
			t.Errorf("copy %d is %+v, want the original %+v", i+1, got[i], want)
		}
	}
	if r := got[1].ReplyTo; r == nil || *r != 1 {
		t.Errorf("the copied reply answers %v, want 1", r)
	}
}

// TestSplitReindexMatches is criterion 3: after a reindex from the logs
// alone, both threads read as they did, to a reader of both and a reader
// of the source alone.
func TestSplitReindexMatches(t *testing.T) {
	w := newMoveWorld(t)
	a := w.postIn(w.id, "author", "one", 0)
	b := w.postIn(w.id, "rep2", "two", a)
	target := w.mustSplit("admin", w.id, "staff", "Reindexed", a, b)
	type read struct {
		who string
		id  threadid.ID
	}
	reads := []read{{"admin", w.id}, {"admin", target}, {"rep1", w.id}}
	before := map[read]string{}
	for _, r := range reads {
		b, _ := json.Marshal(w.page(r.who, r.id).Body)
		before[r] = string(b)
	}
	if _, err := w.proj.Reindex(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	for _, r := range reads {
		b, _ := json.Marshal(w.page(r.who, r.id).Body)
		if string(b) != before[r] {
			t.Errorf("%s's page of %s differs after a reindex:\n%s\n---\n%s", r.who, r.id, before[r], b)
		}
	}
}

// TestSplitValidation: what a split refuses before writing anything.
func TestSplitValidation(t *testing.T) {
	w := newMoveWorld(t)
	removed := w.postIn(w.id, "author", "deleted", 0)
	if _, err := w.svc.Delete(t.Context(), w.as("author"), w.id, removed, ""); err != nil {
		t.Fatal(err)
	}
	pending := w.postIn(w.id, "author", "pending review", 0)
	w.hide(pending)
	moved := w.postIn(w.id, "author", "moved already", 0)
	w.mustSplit("mod", w.id, "general", "Already", moved)
	shown := w.postIn(w.id, "author", "shown", 0)
	tooMany := make([]uint32, threadlog.MaxMoveOrdinals+1)
	for i := range tooMany {
		tooMany[i] = uint32(i + 1)
	}
	cases := []struct {
		name  string
		ords  []uint32
		title string
		want  problem.Code
	}{
		{"empty", nil, "Fine", problem.CodeValidationFailed},
		{"over the bound", tooMany, "Fine", problem.CodeValidationFailed},
		{"twice", []uint32{shown, shown}, "Fine", problem.CodeValidationFailed},
		{"removed", []uint32{removed}, "Fine", problem.CodeConflict},
		{"pending review", []uint32{pending}, "Fine", problem.CodeConflict},
		{"moved", []uint32{moved}, "Fine", problem.CodeConflict},
		{"unknown", []uint32{999}, "Fine", problem.CodeNotFound},
		{"no title", []uint32{shown}, "  ", problem.CodeValidationFailed},
		{"title too long", []uint32{shown}, strings.Repeat("x", MaxTitle+1), problem.CodeValidationFailed},
	}
	for _, c := range cases {
		was := w.lengths(w.id)
		rows, files := w.threadsOnDisk()
		if _, err := w.splitAs("mod", w.id, "general", c.title, c.ords...); codeOf(err) != c.want {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
		if r, f := w.threadsOnDisk(); !slices.Equal(w.lengths(w.id), was) || r != rows || f != files {
			t.Errorf("%s: the refused split wrote something", c.name)
		}
	}
	if _, err := w.splitAs("mod", w.id, "nowhere", "Fine", shown); codeOf(err) != problem.CodeNotFound {
		t.Errorf("a split into no forum: %v, want not-found", err)
	}
}

// TestMovedPostQueueServesNoBody: a flagged post split where its source's
// moderators cannot read stays in their queue as moved, with no body and
// nothing naming the target; the admin, who reads it, is told where.
func TestMovedPostQueueServesNoBody(t *testing.T) {
	w := newMoveWorld(t)
	o := w.postIn(w.id, "author", "SECRET-FLAGGED-TEXT", 0)
	w.mustFlag("rep1", o)
	target := w.mustSplit("admin", w.id, "staff", "Flagged elsewhere", o)
	for _, who := range []string{"fmod", "mod", "admin"} {
		q, err := w.svc.Queue(t.Context(), w.as(who), "")
		if err != nil {
			t.Fatal(err)
		}
		leaks(t, who, q, "SECRET-FLAGGED-TEXT")
		var p *dto.Post
		for _, it := range q.Items {
			if it.Flag != nil && it.Flag.Thread.ID == w.id.String() && it.Flag.Ordinal == int(o) {
				p = it.Flag.Post
			}
		}
		if p == nil {
			t.Fatalf("%s's queue does not list the flagged post", who)
		}
		if !p.Moved || p.HTML != "" {
			t.Errorf("%s's queue serves the moved post as %+v", who, p)
		}
		if who == "admin" {
			if p.MovedTo == nil || p.MovedTo.Thread.ID != target.String() {
				t.Errorf("the admin's queue serves moved_to %+v", p.MovedTo)
			}
			continue
		}
		if p.MovedTo != nil {
			t.Errorf("%s's queue serves moved_to %+v", who, p.MovedTo)
		}
		leaks(t, who, q, target.String(), "Flagged elsewhere")
	}
}
