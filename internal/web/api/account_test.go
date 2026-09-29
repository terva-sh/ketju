package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// The account lifecycle routes (TKT-01M29CKRFS).

// TestEachTransitionSignsOutAtOnce is AC1 through the real session
// middleware: after a deactivation, a suspension and an anonymization, the
// account's old cookie is anonymous on its next request, and a member
// endpoint refuses it as unauthenticated. Each writes its one audit row.
func TestEachTransitionSignsOutAtOnce(t *testing.T) {
	for _, c := range []struct {
		name, as, method, path, body, action string
	}{
		{"deactivate", "member", "POST", "/api/v1/settings/deactivate", "", perm.ActionDeactivated},
		{"suspend", "moderator", "POST", "/api/v1/admin/users/member/suspend", `{"reason":"spam","until":null}`,
			perm.ActionSuspended},
		{"anonymize", "admin", "POST", "/api/v1/admin/users/member/anonymize", "", perm.ActionAnonymized},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newAdminWorld(t)
			if me := w.meOf("member"); me.User == nil || me.User.Handle != "member" {
				t.Fatalf("before: %+v", me.User)
			}
			before := w.auditCount(c.action)
			rec := w.call(c.as, c.method, c.path, c.body)
			if rec.Code != http.StatusNoContent || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%s: %d %s", c.path, rec.Code, rec.Body)
			}
			if c.as == "member" {
				cleared := false
				for _, ck := range rec.Result().Cookies() {
					cleared = cleared || (ck.Name == session.CookieName && ck.MaxAge < 0)
				}
				if !cleared {
					t.Error("deactivating did not clear the session cookie")
				}
			}
			if n := w.auditCount(c.action) - before; n != 1 {
				t.Errorf("%d %s rows, want 1", n, c.action)
			}
			if me := w.meOf("member"); me.User != nil {
				t.Errorf("the old cookie is still %+v", me.User)
			}
			if rec := w.call("member", "GET", "/api/v1/users?prefix=mo", ""); rec.Code != http.StatusUnauthorized {
				t.Errorf("a member endpoint answered the old cookie %d", rec.Code)
			}
			var n int
			if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
				return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM sessions WHERE user_id = 1`).Scan(&n)
			}); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Errorf("%d sessions left", n)
			}
		})
	}
}

// meOf is GET /me's answer to viewer in the admin world.
func (w *adminWorld) meOf(viewer string) dto.Me {
	w.t.Helper()
	rec := w.call(viewer, "GET", "/api/v1/me", "")
	if rec.Code != http.StatusOK {
		w.t.Fatalf("/me answered %d", rec.Code)
	}
	return decodeMe(w.t, rec.Body.Bytes())
}

// The routes' refusals, as problems: a moderator on a moderator, an admin on
// themselves, a bad reason, unsuspending an active account, and the last
// active admin deactivating.
func TestLifecycleRefusals(t *testing.T) {
	w := newAdminWorld(t)
	for _, c := range []struct {
		as, path, body string
		status         int
		problem        string
	}{
		{"moderator", "/api/v1/admin/users/moderator/suspend", `{"reason":"x"}`, 403, "forbidden"},
		{"moderator", "/api/v1/admin/users/admin/suspend", `{"reason":"x"}`, 403, "forbidden"},
		{"admin", "/api/v1/admin/users/admin/suspend", `{"reason":"x"}`, 403, "forbidden"},
		{"admin", "/api/v1/admin/users/admin/anonymize", "", 403, "forbidden"},
		{"moderator", "/api/v1/admin/users/member/anonymize", "", 403, "forbidden"},
		{"moderator", "/api/v1/admin/users/member/suspend", `{"reason":""}`, 422, "validation-failed"},
		{"moderator", "/api/v1/admin/users/member/suspend", `{"reason":"x","until":1}`, 422, "validation-failed"},
		{"moderator", "/api/v1/admin/users/member/suspend", `{"reason":"x","why":"y"}`, 400, "bad-request"},
		{"moderator", "/api/v1/admin/users/nobody/suspend", `{"reason":"x"}`, 404, "not-found"},
		{"moderator", "/api/v1/admin/users/member/unsuspend", "", 409, "conflict"},
		{"admin", "/api/v1/settings/deactivate", "", 409, "conflict"},
		{"anonymous", "/api/v1/settings/deactivate", "", 401, "unauthenticated"},
		{"pre-user", "/api/v1/settings/deactivate", "", 403, "forbidden"},
	} {
		rec := w.call(c.as, "POST", c.path, c.body)
		if rec.Code != c.status || problemType(t, rec) != c.problem {
			t.Errorf("%s %s: %d %s, want %d %s", c.as, c.path, rec.Code, rec.Body, c.status, c.problem)
		}
	}
	if role, status := w.role(4), w.statusOf(4); role != domain.RoleAdmin || status != domain.StatusActive {
		t.Errorf("after the refusals the admin is %s, %s", role, status)
	}
}

func (w *adminWorld) statusOf(id int64) string {
	w.t.Helper()
	var s string
	if err := w.db.Read(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUser(w.t.Context(), id)
		s = u.Status
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return s
}

// A suspension through the route stores the reason and end the member will
// be shown, and unsuspending clears them.
func TestSuspendThroughTheAPI(t *testing.T) {
	w := newAdminWorld(t)
	until := w.now.Add(48 * 60 * 60 * 1e9).UnixMilli()
	if rec := w.call("moderator", "POST", "/api/v1/admin/users/member/suspend",
		fmt.Sprintf(`{"reason":"  Posting spam  ","until":%d}`, until)); rec.Code != http.StatusNoContent {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var reason string
	var end sql.NullInt64
	if err := w.db.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUser(t.Context(), 1)
		reason, end = u.StatusReason, u.StatusUntil
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if reason != "Posting spam" || end.Int64 != until {
		t.Errorf("stored %q until %v", reason, end)
	}
	if rec := w.call("moderator", "POST", "/api/v1/admin/users/member/unsuspend", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if s := w.statusOf(1); s != domain.StatusActive {
		t.Errorf("member is %s", s)
	}
}

// anonymizer is an admin in the thread world, as a viewer.
func (w *world) anonymizer() domain.Viewer {
	w.t.Helper()
	var id int64
	w.write(func(q *dbq.Queries) error {
		var err error
		id, err = q.CreateUser(w.t.Context(), dbq.CreateUserParams{Handle: "root", DisplayName: "Root",
			Status: domain.StatusActive, Role: domain.RoleAdmin, Kind: domain.KindHuman, CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	w.users["root"] = id
	return domain.Viewer{UserID: id, Role: domain.RoleAdmin, Active: true, SessionKey: "s-root"}
}

// anonymize anonymizes handle through the route, as root.
func (w *world) anonymize(root domain.Viewer, handle string) {
	w.t.Helper()
	if rec := w.sendAs(root, "POST", "/api/v1/admin/users/"+handle+"/anonymize", ""); rec.Code != http.StatusNoContent {
		w.t.Fatalf("anonymizing %s: %d %s", handle, rec.Code, rec.Body)
	}
}

// logSums is the SHA-256 of every file under the threads directory.
func (w *world) logSums() map[string][32]byte {
	w.t.Helper()
	out := map[string][32]byte{}
	if err := filepath.WalkDir(w.root, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		out[path] = sha256.Sum256(b)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// TestAnonymizeLeavesTheLogsAlone is AC4's log half: anonymizing touches
// the database only. Every thread log is byte for byte what it was, post
// bodies included.
func TestAnonymizeLeavesTheLogsAlone(t *testing.T) {
	w := newWorld(t)
	w.seed()
	root := w.anonymizer()
	w.cache.Close()
	before := w.logSums()
	if len(before) == 0 {
		t.Fatal("no thread log files to compare")
	}
	w.anonymize(root, "alice")
	after := w.logSums()
	if len(after) != len(before) {
		t.Fatalf("%d files, want %d", len(after), len(before))
	}
	for path, sum := range before {
		if after[path] != sum {
			t.Errorf("%s changed", path)
		}
	}
}

// TestAnonymizedAttributionEverywhere is AC5: after alice is anonymized,
// every surface that drew her draws deleted-user-<id> with no display name
// or avatar, from users.anonymized: the thread page's post headers, the
// quoted post's header, the thread list's author and last poster, the forum
// tree's last post, her profile and her history. Carol's mention of @alice
// no longer links anyone: the body is not edited, so what carol typed is
// shown as the text it is. Her old handle names nobody, and no suggestion
// offers her.
func TestAnonymizedAttributionEverywhere(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.post(id, "carol", "> [quote=#1]\n> The bill\n\nQuoting her.", 0)
	w.post(id, "alice", "My last word.", 0)
	w.exec(`UPDATE users SET avatar_blob = 'alicesavatar', bio_source = 'Alice here.' WHERE id = ?`, w.users["alice"])
	root := w.anonymizer()
	carol := w.viewer("carol")
	path := "/api/v1/threads/" + id.String()
	if body := w.get(carol, path).Body.String(); !strings.Contains(body, "/u/alice") {
		t.Fatalf("before: the mention does not link alice: %s", body)
	}

	w.anonymize(root, "alice")
	placeholder := fmt.Sprintf("deleted-user-%d", w.users["alice"])
	want := dto.Author{Handle: placeholder, Role: domain.RoleMember, Kind: domain.KindHuman}
	named := func(what, body string) {
		t.Helper()
		for _, old := range []string{"Alice Nakamura", "alicesavatar", "Alice here", `"handle":"alice"`, "/u/alice"} {
			if strings.Contains(body, old) {
				t.Errorf("%s still carries %q: %s", what, old, body)
			}
		}
		if !strings.Contains(body, placeholder) {
			t.Errorf("%s does not name %s: %s", what, placeholder, body)
		}
	}

	p := page(t, w.get(carol, path))
	named("the thread page", w.get(carol, path).Body.String())
	for _, post := range p.Posts {
		if post.Ordinal == 1 || post.Ordinal == 6 {
			if post.Author != want {
				t.Errorf("post %d's header is %+v", post.Ordinal, post.Author)
			}
		}
		if post.Ordinal == 4 && (!strings.Contains(post.HTML, "@alice") || strings.Contains(post.HTML, "/u/")) {
			t.Errorf("the mention renders as %s", post.HTML)
		}
		if post.Ordinal == 5 && (!strings.Contains(post.HTML, "Post #1") || strings.Contains(post.HTML, "Alice")) {
			t.Errorf("the quote names her: %s", post.HTML)
		}
	}
	named("the thread list", w.get(carol, "/api/v1/forums/general/threads").Body.String())
	named("the forum tree", w.get(carol, "/api/v1/forums").Body.String())

	prof := w.get(carol, "/api/v1/users/"+placeholder)
	named("the profile", prof.Body.String())
	var profile dto.Profile
	if err := json.Unmarshal(prof.Body.Bytes(), &profile); err != nil || profile.User != want || profile.BioHTML != "" {
		t.Errorf("the profile is %+v (%v)", profile, err)
	}
	named("the history", w.get(carol, "/api/v1/users/"+placeholder+"/posts").Body.String())
	if rec := w.get(carol, "/api/v1/users/alice"); rec.Code != http.StatusNotFound {
		t.Errorf("the old handle answered %d", rec.Code)
	}
	for _, prefix := range []string{"del", "ali"} {
		var s dto.UserSuggestions
		if err := json.Unmarshal(w.get(carol, "/api/v1/users?prefix="+prefix).Body.Bytes(), &s); err != nil || len(s.Users) != 0 {
			t.Errorf("?prefix=%s suggests %+v (%v)", prefix, s.Users, err)
		}
	}
}

// TestResponseCacheAfterAnonymize is AC5's cache half, as
// TestResponseCacheAfterPromotion: a page cached with alice's name is not
// served once another process has anonymized her, with no append to the
// thread, for any viewer class, and the cached page is the fresh one.
func TestResponseCacheAfterAnonymize(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	path := "/api/v1/threads/" + id.String()
	for _, who := range []string{"", "carol", "bob"} {
		if body := w.same(w.session(who), path); !strings.Contains(body, "Alice Nakamura") {
			t.Fatalf("%q's page does not name alice to begin with", who)
		}
	}
	other := perm.New(w.db) // another process's Evaluator
	if err := other.Anonymize(t.Context(), w.users["alice"], audit.System("test"), nil); err != nil {
		t.Fatal(err)
	}
	for _, who := range []string{"", "carol", "bob"} {
		if body := w.same(w.session(who), path); strings.Contains(body, "Alice Nakamura") {
			t.Errorf("%q was served alice's name after the anonymization: %s", who, body)
		}
	}
}

// TestEverySurfaceReadsTheColumn: every author block is drawn through
// member.Author, which reads users.anonymized. A row flagged anonymized that
// still holds its old name and avatar, as one caught halfway would, shows
// neither on the thread page, the thread list or the forum tree, where
// deleted-user-9 wrote the last post.
func TestEverySurfaceReadsTheColumn(t *testing.T) {
	w := newProfileWorld(t)
	general := w.posts["general"].thread
	for _, path := range []string{"/api/v1/threads/" + general.String(), "/api/v1/forums/general/threads",
		"/api/v1/forums"} {
		body := w.get(w.as("member"), path).Body.String()
		if !strings.Contains(body, "deleted-user-9") {
			t.Errorf("%s does not draw deleted-user-9: %s", path, body)
		}
		for _, old := range []string{"Old Name", "a1b2c3old"} {
			if strings.Contains(body, old) {
				t.Errorf("%s carries %q: %s", path, old, body)
			}
		}
	}
}
