package auditlog

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

var admin = domain.Viewer{UserID: 1, Role: domain.RoleAdmin, Active: true, SessionKey: "s1"}

// world is a database with ann (1, admin), ben (2, moderator) and an
// anonymized account (3); two forums; a thread with a flag on its fourth
// post; and whatever rows each test appends.
type world struct {
	t              *testing.T
	db             *db.DB
	svc            *Service
	general, money int64
	thread         threadid.ID
	flag           int64
	moderators     int64
	at             int64
}

func newWorld(t *testing.T) *world {
	t.Helper()
	d := dbtest.Open(t, db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	w := &world{t: t, db: d, svc: &Service{DB: d}, thread: threadid.ID(0x1234_5678_9abc), at: 1_757_600_000_000}
	if err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		for _, stmt := range []string{
			`INSERT INTO users (id, handle, display_name, status, role, kind, prefs_json, created_at, updated_at)
				VALUES (1, 'ann', 'Ann', 'active', 'admin', 'human', '{}', 1, 1)`,
			`INSERT INTO users (id, handle, display_name, status, role, kind, prefs_json, created_at, updated_at)
				VALUES (2, 'ben', 'Ben', 'active', 'moderator', 'human', '{}', 1, 1)`,
			`INSERT INTO users (id, handle, display_name, status, role, kind, prefs_json, created_at, updated_at)
				VALUES (3, 'deleted-user-3', '', 'active', 'member', 'human', '{}', 1, 1)`,
			`UPDATE users SET anonymized = 1, status = 'deactivated' WHERE id = 3`,
		} {
			if _, err := tx.ExecContext(t.Context(), stmt); err != nil {
				return err
			}
		}
		var err error
		if w.general, err = q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "general", Name: "General",
			Visibility: domain.VisibilityPublic, CreatedAt: 1, UpdatedAt: 1}); err != nil {
			return err
		}
		if w.money, err = q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "money", Name: "Money",
			Visibility: domain.VisibilityPublic, CreatedAt: 1, UpdatedAt: 1}); err != nil {
			return err
		}
		if _, err := tx.ExecContext(t.Context(), `INSERT INTO threads (id, forum_id, slug, title, author_id,
			created_at, post_count, last_post_at, last_post_author_id, last_post_ordinal, log_length_projected)
			VALUES (?, ?, 'the-dinner-bill', 'The dinner bill', 2, 1, 5, 1, 2, 5, 0)`, int64(w.thread), w.money); err != nil {
			return err
		}
		if err := tx.QueryRowContext(t.Context(), `INSERT INTO flags (thread_id, ordinal, reporter_id, reason,
			created_at) VALUES (?, 4, 2, 'spam', 1) RETURNING id`, int64(w.thread)).Scan(&w.flag); err != nil {
			return err
		}
		g, err := q.GetGroupBySlug(t.Context(), domain.GroupModerators)
		w.moderators = g.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return w
}

// append writes one row through audit.Append, a millisecond after the last,
// and returns its id.
func (w *world) append(by audit.By, action string, target audit.Target, details map[string]any) int64 {
	w.t.Helper()
	w.at++
	var id int64
	if err := w.db.Write(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		id, err = audit.Append(w.t.Context(), q, time.UnixMilli(w.at), by, action, target, details)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return id
}

func (w *world) query(in Query) dto.AuditPage {
	w.t.Helper()
	page, err := w.svc.Query(w.t.Context(), admin, in)
	if err != nil {
		w.t.Fatalf("%+v: %v", in, err)
	}
	return page
}

func ids(p dto.AuditPage) []int64 {
	out := []int64{}
	for _, e := range p.Entries {
		out = append(out, e.ID)
	}
	return out
}

// TestAuditTargetsLink is the third criterion's server half: every target
// type that has a page links to it, labelled as it reads now; a blob and an
// identity have none; an absent user reads as deleted-user-<id>; and a
// thread.moved row carries both forums in related, the one it left first.
func TestAuditTargetsLink(t *testing.T) {
	w := newWorld(t)
	by := audit.By{Actor: 1, IP: "203.0.113.7"}
	hash := strings.Repeat("ab", 32)
	tpath := "/t/" + w.thread.String() + "/the-dinner-bill"
	gone := threadid.ID(0x9999)
	cases := []struct {
		target      audit.Target
		label, href string
	}{
		{audit.User(2), "ben", "/u/ben"},
		{audit.User(3), "deleted-user-3", "/u/deleted-user-3"},
		{audit.User(99), "deleted-user-99", ""},
		{audit.Thread(w.thread), "The dinner bill", tpath},
		{audit.Thread(gone), gone.String(), ""},
		{audit.Post(w.thread, 4), "The dinner bill #4", tpath + "/4"},
		{audit.Forum(w.general), "General", "/f/general"},
		{audit.Forum(999), "999", ""},
		{audit.Setting("registration.mode"), "registration.mode", "/admin/settings"},
		{audit.Flag(w.flag), "flag " + strconv.FormatInt(w.flag, 10) + " on The dinner bill #4", tpath + "/4"},
		{audit.Flag(999), "999", ""},
		{audit.Application(5), "application 5", "/admin/applications?id=5"},
		{audit.Group(w.moderators), "Moderators", "/admin/users?tab=groups"},
		{audit.Group(999), "999", ""},
		{audit.Permission(0, w.moderators, "moderate"), "global:" + strconv.FormatInt(w.moderators, 10) + ":moderate",
			"/admin/users?tab=permissions"},
		{audit.Form("registration"), "registration", "/admin/forms"},
		{audit.Identity(1), "identity 1", ""},
		{audit.Blob(hash), hash[:12], ""},
	}
	want := map[int64]int{}
	for i, c := range cases {
		want[w.append(by, "user.suspended", c.target, nil)] = i
	}
	moved := w.append(audit.System("thread-log"), "thread.moved", audit.Thread(w.thread),
		map[string]any{"thread": w.thread.String(), "log_offset": 64, "from_forum": w.general, "to_forum": w.money})
	movedAway := w.append(audit.System("thread-log"), "thread.moved", audit.Thread(w.thread),
		map[string]any{"thread": w.thread.String(), "log_offset": 128, "from_forum": w.money, "to_forum": 999})

	page := w.query(Query{})
	if len(page.Entries) != len(cases)+2 {
		t.Fatalf("%d entries, want %d", len(page.Entries), len(cases)+2)
	}
	for _, e := range page.Entries {
		i, ok := want[e.ID]
		if !ok {
			continue
		}
		c := cases[i]
		if e.Target.Type != c.target.Type || e.Target.ID != c.target.ID || e.Target.Label != c.label ||
			e.Target.Href != c.href {
			t.Errorf("%s %s: %+v, want label %q and href %q", c.target.Type, c.target.ID, e.Target, c.label, c.href)
		}
		if e.Actor == nil || e.Actor.Handle != "ann" || e.IP != "203.0.113.7" {
			t.Errorf("%s %s: actor %+v, ip %q", c.target.Type, c.target.ID, e.Actor, e.IP)
		}
		if e.Related == nil || len(e.Related) != 0 {
			t.Errorf("%s %s: related %#v, want empty and not null", c.target.Type, c.target.ID, e.Related)
		}
	}
	byID := map[int64]dto.AuditEntry{}
	for _, e := range page.Entries {
		byID[e.ID] = e
	}
	m := byID[moved]
	if m.Actor != nil || m.Target.Href != tpath {
		t.Errorf("the move: actor %+v, target %+v", m.Actor, m.Target)
	}
	if wantRel := []dto.AuditTarget{
		{Type: "forum", ID: strconv.FormatInt(w.general, 10), Label: "General", Href: "/f/general"},
		{Type: "forum", ID: strconv.FormatInt(w.money, 10), Label: "Money", Href: "/f/money"},
	}; !slices.Equal(m.Related, wantRel) {
		t.Errorf("the move's related is %+v, want %+v", m.Related, wantRel)
	}
	var d map[string]any
	if err := json.Unmarshal(m.Details, &d); err != nil || d["via"] != "thread-log" || d["from_forum"] != float64(w.general) {
		t.Errorf("the move's details are %s: %v", m.Details, err)
	}
	if rel := byID[movedAway].Related; len(rel) != 2 || rel[1] != (dto.AuditTarget{Type: "forum", ID: "999", Label: "999"}) {
		t.Errorf("a move to a forum that is gone: related %+v", rel)
	}
}

// TestAuditFilters is the first criterion through the service: each
// parameter reaches the view, the actor by handle, and the answer carries
// the whole action list whatever the filter.
func TestAuditFilters(t *testing.T) {
	w := newWorld(t)
	suspended := w.append(audit.By{Actor: 2}, "user.suspended", audit.User(1), nil)
	expired := w.append(audit.System("expiry"), "user.unsuspended", audit.User(1), nil)
	locked := w.append(audit.By{Actor: 1}, "thread.locked", audit.Thread(w.thread), nil)
	changed := w.append(audit.By{Actor: 1}, "setting.changed", audit.Setting("registration.mode"), nil)
	for _, c := range []struct {
		name string
		in   Query
		want []int64
	}{
		{"none", Query{}, []int64{changed, locked, expired, suspended}},
		{"actor by handle", Query{Actor: "ben"}, []int64{suspended}},
		{"actor in another case", Query{Actor: "BEN"}, []int64{suspended}},
		{"an actor nobody is", Query{Actor: "nobody"}, []int64{}},
		{"system", Query{System: "1"}, []int64{expired}},
		{"action", Query{Action: "thread.locked"}, []int64{locked}},
		{"action prefix", Query{Action: "user."}, []int64{expired, suspended}},
		{"target type", Query{TargetType: "user"}, []int64{expired, suspended}},
		{"target", Query{TargetType: "thread", TargetID: w.thread.String()}, []int64{locked}},
		{"from", Query{From: strconv.FormatInt(w.at-1, 10)}, []int64{changed, locked}},
		{"to", Query{To: strconv.FormatInt(w.at-1, 10)}, []int64{expired, suspended}},
		{"combined", Query{Actor: "ann", Action: "thread.", TargetType: "thread"}, []int64{locked}},
	} {
		t.Run(c.name, func(t *testing.T) {
			page := w.query(c.in)
			if got := ids(page); !slices.Equal(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
			if !slices.Equal(page.Actions, audit.Actions) {
				t.Errorf("actions %v, want audit.Actions", page.Actions)
			}
			if page.NextCursor != "" {
				t.Errorf("next_cursor %q on the only page", page.NextCursor)
			}
		})
	}
}

// TestAuditPagesByCursor: a full page carries a cursor, following it
// reaches every row once, newest first, and the last page carries none.
func TestAuditPagesByCursor(t *testing.T) {
	w := newWorld(t)
	var want []int64
	for i := 0; i < PageSize+7; i++ {
		want = append(want, w.append(audit.By{Actor: 1}, "user.promoted", audit.User(2), nil))
	}
	slices.Reverse(want)
	var got []int64
	cursor := ""
	for pages := 0; ; pages++ {
		page := w.query(Query{Cursor: cursor})
		got = append(got, ids(page)...)
		if page.NextCursor == "" {
			if pages != 1 {
				t.Errorf("%d pages, want 2", pages+1)
			}
			break
		}
		if len(page.Entries) != PageSize {
			t.Errorf("a page with a next cursor has %d entries, want %d", len(page.Entries), PageSize)
		}
		cursor = page.NextCursor
	}
	if !slices.Equal(got, want) {
		t.Errorf("paged %v, want %v", got, want)
	}
}

// TestAuditCursorIsOpaque: a cursor the service did not issue, a target_id
// with no target_type, a target_type that is not one, a time that is not
// Unix ms, and a system flag that is not 1 or is set beside an actor, are
// each bad-request, and nothing is read.
func TestAuditCursorIsOpaque(t *testing.T) {
	w := newWorld(t)
	w.append(audit.By{Actor: 1}, "user.promoted", audit.User(2), nil)
	for _, in := range []Query{
		{Cursor: "not base64!"},
		{Cursor: "YWJj"}, // "abc"
		{Cursor: "MA"},   // "0"
		{Cursor: "LTE"},  // "-1"
		{Cursor: "MDEy"}, // "012", not the form issued
		{Cursor: "NDIK"}, // "42\n"
		{Cursor: "NDI="}, // "42", padded
		{TargetID: "2"},
		{TargetType: "users"},
		{TargetType: "User"},
		{From: "yesterday"},
		{To: "-5"},
		{From: "1.5"},
		{System: "true"},
		{System: "1", Actor: "ann"},
	} {
		_, err := w.svc.Query(t.Context(), admin, in)
		if code := problem.FromError(err); code != problem.CodeBadRequest {
			t.Errorf("%+v: %v, want bad-request", in, err)
		}
	}
	if got := ids(w.query(Query{Cursor: encodeCursor(2)})); len(got) != 1 {
		t.Errorf("an issued cursor: %v", got)
	}
}

// TestAuditQueryAsksAgain: the service refuses anyone but an active admin
// before it reads anything, so a caller that forgot the admin gate is still
// refused; the refusal carries the action list no more than the rows.
func TestAuditQueryAsksAgain(t *testing.T) {
	w := newWorld(t)
	w.append(audit.By{Actor: 1}, "user.promoted", audit.User(2), nil)
	for _, c := range []struct {
		name string
		v    domain.Viewer
		code problem.Code
	}{
		{"anonymous", domain.Anonymous(), problem.CodeUnauthenticated},
		{"an inactive admin", domain.Viewer{UserID: 1, Role: domain.RoleAdmin}, problem.CodeUnauthenticated},
		{"a pre-user", domain.Viewer{PreUser: true, IdentityID: 1}, problem.CodeForbidden},
		{"a member", domain.Viewer{UserID: 3, Role: domain.RoleMember, Active: true}, problem.CodeForbidden},
		{"a moderator", domain.Viewer{UserID: 2, Role: domain.RoleModerator, Active: true}, problem.CodeForbidden},
	} {
		t.Run(c.name, func(t *testing.T) {
			page, err := w.svc.Query(t.Context(), c.v, Query{})
			if code := problem.FromError(err); code != c.code {
				t.Errorf("%v, want %s", err, c.code)
			}
			if len(page.Entries) != 0 {
				t.Errorf("refused, and still answered %d entries", len(page.Entries))
			}
		})
	}
	if _, err := w.svc.Query(t.Context(), admin, Query{}); err != nil {
		t.Errorf("the admin: %v", err)
	}
}
