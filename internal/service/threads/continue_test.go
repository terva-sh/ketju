package threads

import (
	"database/sql"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/respcache"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// Tests for reply as a new thread (TKT-01M29CYSC0, docs/06 "Reply as a new
// thread"): CreateThread with a from, the ThreadCreated's continues key it
// writes, and the "continued in" note and "continued from" header drawn per
// viewer (movedraw.go). They run in a move world (move_test.go): general and
// other are public, and staff is the admin's alone until staffReader lets
// rep2 read it.

// continueAs starts a thread in forum, as who, continuing post ordinal of
// from, with source as its first post.
func (w *moveWorld) continueAs(who, forum string, from threadid.ID, ordinal uint32, source string) (dto.CreatedThread, error) {
	w.clock += 1000
	return w.svc.CreateThread(w.t.Context(), w.viewerOf(who), forum, CreateThreadRequest{
		Title: "Continued by " + who, Source: source,
		From: &ContinueFrom{Thread: from.String(), Ordinal: ordinal},
	})
}

// mustContinue is continueAs, which must succeed; it answers the new thread.
func (w *moveWorld) mustContinue(who, forum string, from threadid.ID, ordinal uint32) threadid.ID {
	w.t.Helper()
	res, err := w.continueAs(who, forum, from, ordinal, "Picking this up on its own.")
	if err != nil {
		w.t.Fatalf("%s continuing %s/%d in %s: %v", who, from, ordinal, forum, err)
	}
	id, err := threadid.Parse(res.Thread.ID)
	if err != nil {
		w.t.Fatal(err)
	}
	return id
}

// continuations is every thread_continuations row, as "thread<-source/ordinal".
func (w *moveWorld) continuations() []string {
	w.t.Helper()
	var out []string
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(w.t.Context(), `SELECT thread_id, source_thread_id, source_ordinal FROM thread_continuations ORDER BY thread_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, src, o int64
			if err := rows.Scan(&id, &src, &o); err != nil {
				return err
			}
			out = append(out, threadid.ID(id).String()+"<-"+threadid.ID(src).String()+"/"+itoa(o))
		}
		return rows.Err()
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// TestReplyAsNewThreadNeedsCreateThread is criterion 2: any member who may
// start a thread in the forum may continue a post there, no moderator
// needed; one who may not is forbidden, and anonymous is refused; a refusal
// opens no thread.
func TestReplyAsNewThreadNeedsCreateThread(t *testing.T) {
	w := newMoveWorld(t)
	o := w.postIn(w.id, "author", "worth its own thread", 0)
	res, err := w.continueAs("rep1", "other", w.id, o, "On its own now.")
	if err != nil {
		t.Fatalf("a member continuing into other: %v", err)
	}
	if res.Thread.Forum.Slug != "other" || res.Thread.Author.Handle != "rep1" {
		t.Errorf("the new thread is %+v", res.Thread)
	}
	if cf := res.Thread.ContinuedFrom; cf == nil || cf.Thread.ID != w.id.String() || cf.Ordinal != int(o) {
		t.Errorf("the new thread's continued_from is %+v", cf)
	}

	// Members keep read and reply in other, and lose create_thread there.
	var members int64
	w.write(func(q *dbq.Queries) error {
		g, err := q.GetGroupBySlug(t.Context(), domain.GroupMembers)
		members = g.ID
		return err
	})
	if err := w.svc.Perm.ApplyGrants(t.Context(), audit.By{}, []perm.GrantChange{
		{ForumID: w.other, GroupID: members, Permission: domain.PermRead, Grant: true},
		{ForumID: w.other, GroupID: members, Permission: domain.PermReply, Grant: true},
	}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		who  string
		want problem.Code
	}{{"rep1", problem.CodeForbidden}, {"", problem.CodeUnauthenticated}} {
		rows, files := w.threadsOnDisk()
		if _, err := w.continueAs(c.who, "other", w.id, o, "Again."); codeOf(err) != c.want {
			t.Errorf("%q continuing into other without create_thread: %v, want %q", c.who, err, c.want)
		}
		if r, f := w.threadsOnDisk(); r != rows || f != files {
			t.Errorf("%q's refused continuation opened a thread", c.who)
		}
	}
	if page := w.page("rep1", w.id); !page.Body.Viewer.CanCreateThread {
		t.Error("rep1 is not offered reply as a new thread in general")
	}
	if page := w.page("rep1", w.otherID); page.Body.Viewer.CanCreateThread {
		t.Error("rep1 is offered reply as a new thread in other, where they may not start one")
	}
	if page := w.page("", w.id); page.Body.Viewer.CanCreateThread {
		t.Error("an anonymous viewer is offered reply as a new thread")
	}
}

// TestReplyAsNewThreadRefusesUnreadableOrRemovedSource: a source the member
// may not read, one that does not exist, and a post that is not shown as
// itself (deleted, hidden pending review, moved) or that the thread does not
// hold are all not-found, and a malformed thread id is validation-failed.
// Nothing is written, to any log. A redacted source is the API's test
// (TestReplyAsNewThreadRefusesRedactedSource), which can rewrite a log.
func TestReplyAsNewThreadRefusesUnreadableOrRemovedSource(t *testing.T) {
	w := newMoveWorld(t)
	deleted := w.postIn(w.id, "author", "deleted", 0)
	if _, err := w.svc.Delete(t.Context(), w.as("author"), w.id, deleted, ""); err != nil {
		t.Fatal(err)
	}
	pending := w.postIn(w.id, "author", "pending review", 0)
	w.hide(pending)
	moved := w.postIn(w.id, "author", "moved", 0)
	w.mustSplit("mod", w.id, "general", "Moved away", moved)
	secret := w.postIn(w.staffID, "author", "staff only", 0)
	nowhere := threadid.ID(uint64(1_757_500_000_000)<<16 | 999)
	for _, c := range []struct {
		name    string
		thread  string
		ordinal uint32
		want    problem.Code
	}{
		{"a thread rep1 may not read", w.staffID.String(), secret, problem.CodeNotFound},
		{"no such thread", nowhere.String(), 1, problem.CodeNotFound},
		{"a deleted post", w.id.String(), deleted, problem.CodeNotFound},
		{"a post pending review", w.id.String(), pending, problem.CodeNotFound},
		{"a moved post", w.id.String(), moved, problem.CodeNotFound},
		{"no such post", w.id.String(), 999, problem.CodeNotFound},
		{"ordinal 0", w.id.String(), 0, problem.CodeNotFound},
		{"a malformed thread", "not a thread", 1, problem.CodeValidationFailed},
	} {
		was := w.lengths(w.id, w.staffID)
		rows, files := w.threadsOnDisk()
		_, err := w.svc.CreateThread(t.Context(), w.as("rep1"), "general", CreateThreadRequest{Title: "Onwards",
			Source: "text", From: &ContinueFrom{Thread: c.thread, Ordinal: c.ordinal}})
		if codeOf(err) != c.want {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
		if r, f := w.threadsOnDisk(); !slices.Equal(w.lengths(w.id, w.staffID), was) || r != rows || f != files {
			t.Errorf("%s: the refusal wrote something", c.name)
		}
	}
	// The admin reads staff, so the same post is theirs to continue.
	if _, err := w.continueAs("admin", "general", w.staffID, secret, "From staff."); err != nil {
		t.Errorf("the admin continuing a staff post: %v", err)
	}
}

// TestReplyAsNewThreadWritesOneLog is criterion 7's second half: the source
// log is byte-for-byte as it was, a locked or archived source's included
// (docs/06's run decision of 2026-10-02), and the link is the new thread's
// ThreadCreated's continues, projected into thread_continuations.
func TestReplyAsNewThreadWritesOneLog(t *testing.T) {
	w := newMoveWorld(t)
	o := w.postIn(w.id, "author", "the start of something", 0)
	var made []string
	for _, step := range []string{"open", "locked", "archived"} {
		switch step {
		case "locked":
			if _, err := w.svc.Lock(t.Context(), w.as("mod"), w.id, "cooling off"); err != nil {
				t.Fatal(err)
			}
		case "archived":
			if _, err := w.svc.Archive(t.Context(), w.as("admin"), w.id, "done"); err != nil {
				t.Fatal(err)
			}
		}
		was := w.logOf(w.id)
		id := w.mustContinue("rep1", "other", w.id, o)
		if n := w.logOf(w.id); n != was {
			t.Errorf("continuing a post of the %s source wrote %d bytes to it", step, n-was)
		}
		made = append(made, id.String()+"<-"+w.id.String()+"/"+itoa(int64(o)))
	}
	slices.Sort(made)
	if got := w.continuations(); !slices.Equal(got, made) {
		t.Errorf("thread_continuations holds %v, want %v", got, made)
	}
	// Every continuation reads under the post, oldest first.
	got := w.page("rep1", w.id).Body.Posts[o-1].ContinuedIn
	if len(got) != 3 || got[0].Title != "Continued by rep1" {
		t.Errorf("the post's continued_in is %+v", got)
	}
}

// TestContinuedNoteRevealsOnlyToReaders is criterion 3, both ways round,
// with three viewers: rep2 reads general and staff, rep1 general alone, and
// an anonymous viewer general alone. A post of general continued in staff
// shows the note to rep2 and the admin alone; a post of staff continued in
// general gives the new thread a "continued from" header for rep2 alone,
// and rep1 reads the thread and nothing that names staff.
func TestContinuedNoteRevealsOnlyToReaders(t *testing.T) {
	w := newMoveWorld(t)
	w.staffReader()
	general, staff := w.linkOf(w.id), w.linkOf(w.staffID)
	p := w.postIn(w.id, "author", "picked up in staff", 0)
	inStaff := w.mustContinue("admin", "staff", w.id, p)
	inStaffLink := w.linkOf(inStaff)
	for _, who := range []string{"admin", "rep2", "rep1", "fmod", ""} {
		reads := who == "admin" || who == "rep2"
		page := w.page(who, w.id)
		alone, err := w.svc.Post(t.Context(), w.viewerOf(who), w.id, p, "")
		if err != nil {
			t.Fatal(err)
		}
		for where, got := range map[string][]dto.ThreadLink{"page": page.Body.Posts[p-1].ContinuedIn, "post": alone.Body.ContinuedIn} {
			if reads && (len(got) != 1 || got[0] != inStaffLink) {
				t.Errorf("%s reads the %s's continued_in as %+v, want the staff thread", who, where, got)
			}
			if !reads && len(got) != 0 {
				t.Errorf("%s, who may not read staff, reads the %s's continued_in as %+v", who, where, got)
			}
		}
		if !reads {
			leaks(t, who, page.Body, inStaff.String(), inStaffLink.Title)
			leaks(t, who, alone.Body, inStaff.String(), inStaffLink.Title)
			continue
		}
		cf := w.page(who, inStaff).Body.Thread.ContinuedFrom
		if cf == nil || cf.Thread != general || cf.Ordinal != int(p) {
			t.Errorf("%s reads the staff thread's continued_from as %+v", who, cf)
		}
	}

	q := w.postIn(w.staffID, "author", "STAFF-WORDS", 0)
	inGeneral := w.mustContinue("admin", "general", w.staffID, q)
	for _, who := range []string{"rep2", "rep1", ""} {
		page := w.page(who, inGeneral)
		cf := page.Body.Thread.ContinuedFrom
		if who == "rep2" {
			if cf == nil || cf.Thread != staff || cf.Ordinal != int(q) {
				t.Errorf("rep2 reads continued_from as %+v", cf)
			}
			if got := w.page("rep2", w.staffID).Body.Posts[q-1].ContinuedIn; len(got) != 1 || got[0].ID != inGeneral.String() {
				t.Errorf("rep2 reads the staff post's continued_in as %+v", got)
			}
			continue
		}
		if cf != nil {
			t.Errorf("%s, who may not read staff, reads continued_from as %+v", who, cf)
		}
		leaks(t, who, page.Body, w.staffID.String(), staff.Title, staff.Slug)
		if len(page.Body.Posts) == 0 {
			t.Errorf("%s cannot read the new thread's posts", who)
		}
	}
	// The moderation answers draw the header the same way.
	h, err := w.svc.Lock(t.Context(), w.as("fmod"), inGeneral, "")
	if err != nil {
		t.Fatal(err)
	}
	if h.ContinuedFrom != nil {
		t.Errorf("fmod's lock answered continued_from %+v", h.ContinuedFrom)
	}
}

// TestContinuedNotInCachedPosts: two members of one viewer class who differ
// only in reading the new thread are served different bodies and ETags, the
// second from the cache, and the cache holds neither's continued_in.
func TestContinuedNotInCachedPosts(t *testing.T) {
	w := newMoveWorld(t)
	w.svc.Responses = respcache.New(1 << 20)
	w.staffReader()
	p := w.postIn(w.id, "author", "for staff", 0)
	inStaff := w.mustContinue("admin", "staff", w.id, p)

	before := w.svc.Responses.Counters()
	blind := w.page("rep1", w.id)
	mid := w.svc.Responses.Counters()
	sees := w.page("rep2", w.id)
	after := w.svc.Responses.Counters()
	if mid.Misses != before.Misses+1 || after.Hits != mid.Hits+1 {
		t.Errorf("the second member's page was not served from the cache: %+v, %+v, %+v", before, mid, after)
	}
	if got := blind.Body.Posts[p-1].ContinuedIn; len(got) != 0 {
		t.Errorf("rep1 reads continued_in %+v", got)
	}
	if got := sees.Body.Posts[p-1].ContinuedIn; len(got) != 1 || got[0].ID != inStaff.String() {
		t.Errorf("rep2 reads continued_in %+v", got)
	}
	if blind.ETag == sees.ETag {
		t.Error("the two members' pages share an ETag")
	}
	again := w.page("rep1", w.id)
	if got := again.Body.Posts[p-1].ContinuedIn; len(got) != 0 || again.ETag != blind.ETag {
		t.Errorf("rep1's page after rep2's: continued_in %+v, ETag %s, was %s", got, again.ETag, blind.ETag)
	}
	if res, err := w.svc.View(t.Context(), w.as("rep1"), w.id, Query{PageSize: 50}, sees.ETag); err != nil || res.NotModified {
		t.Errorf("rep1 sending rep2's ETag: %+v, %v", res.NotModified, err)
	}
}

// TestContinuedNoteSurvivesReindex is criterion 7's first half: the note
// and the header are rebuilt from the new thread's log alone, so after a
// reindex both threads read as they did.
func TestContinuedNoteSurvivesReindex(t *testing.T) {
	w := newMoveWorld(t)
	w.staffReader()
	p := w.postIn(w.id, "author", "one", 0)
	inStaff := w.mustContinue("admin", "staff", w.id, p)
	inOther := w.mustContinue("rep1", "other", w.id, p)
	type read struct {
		who string
		id  threadid.ID
	}
	reads := []read{{"rep2", w.id}, {"rep1", w.id}, {"rep2", inStaff}, {"rep1", inOther}}
	before := map[read]string{}
	for _, r := range reads {
		b, _ := json.Marshal(w.page(r.who, r.id).Body)
		before[r] = string(b)
	}
	rows := w.continuations()
	if len(rows) != 2 {
		t.Fatalf("thread_continuations holds %v", rows)
	}
	if _, err := w.proj.Reindex(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if got := w.continuations(); !slices.Equal(got, rows) {
		t.Errorf("after a reindex thread_continuations holds %v, was %v", got, rows)
	}
	for _, r := range reads {
		b, _ := json.Marshal(w.page(r.who, r.id).Body)
		if string(b) != before[r] {
			t.Errorf("%s's page of %s differs after a reindex:\n%s\n---\n%s", r.who, r.id, before[r], b)
		}
	}
	if got := w.page("rep2", w.id).Body.Posts[p-1].ContinuedIn; len(got) != 2 {
		t.Errorf("rep2 reads continued_in %+v after the reindex, want both threads", got)
	}
}

// TestPrefilledQuoteIsMemberText is criterion 6: the quote the composer
// pre-fills is the member's own text, an ordinary blockquote, so it renders
// to every reader of the new thread and stays when the source post is
// removed; it carries no [quote=#N], which would name a post of the new
// thread.
func TestPrefilledQuoteIsMemberText(t *testing.T) {
	w := newMoveWorld(t)
	p := w.postIn(w.id, "author", "the words being continued", 0)
	source := "@author wrote:\n\n> the words being continued\n\nAnd here is where it goes next."
	res, err := w.continueAs("rep1", "general", w.id, p, source)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := threadid.Parse(res.Thread.ID)
	if _, err := w.svc.Delete(t.Context(), w.as("mod"), w.id, p, "off topic"); err != nil {
		t.Fatal(err)
	}
	for _, who := range []string{"rep1", "rep2", ""} {
		first := w.page(who, id).Body.Posts[0]
		if !strings.Contains(first.HTML, "<blockquote>") || !strings.Contains(first.HTML, "the words being continued") {
			t.Errorf("%s reads the new thread's first post as %q", who, first.HTML)
		}
		if strings.Contains(first.HTML, "quote=") || strings.Contains(first.HTML, `class="quote`) {
			t.Errorf("%s reads a quote attribution in %q", who, first.HTML)
		}
	}
	if src := w.page("rep1", w.id).Body.Posts[p-1]; src.HTML != "" {
		t.Errorf("the removed source post still serves %q", src.HTML)
	}
}
