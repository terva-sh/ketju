package api

import (
	"bytes"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/agentcred"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/service/account"
	"github.com/terva-sh/ketju/internal/service/blobs/blobstest"
	"github.com/terva-sh/ketju/internal/store/blob"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	blobsweb "github.com/terva-sh/ketju/internal/web/blobs"
)

// Avatars through the chain (TKT-01M3YSQ4CR, docs/05 "Display name and
// bio"): set and cleared on PUT and DELETE /settings/avatar, served at
// GET /avatars/{hash} to signed-in members only, and cleared by staff.

// putAvatar PUTs parts to /settings/avatar as token.
func (w *settingsWorld) putAvatar(token string, parts ...part) *httptest.ResponseRecorder {
	w.t.Helper()
	ctype, b := multipartBody(w.t, parts...)
	r := httptest.NewRequest(http.MethodPut, "/api/v1/settings/avatar", bytes.NewReader(b))
	r.Header.Set("Content-Type", ctype)
	if token != "" {
		r.AddCookie(sessiontest.Cookie(token))
	}
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, r)
	return rec
}

// agentToken is a credential for scribe, the world's agent.
func (w *settingsWorld) agentToken() string {
	w.t.Helper()
	var tok string
	w.write(func(q *dbq.Queries) error {
		if err := q.CreateAgent(w.t.Context(), dbq.CreateAgentParams{UserID: w.users["scribe"], CreatedAt: 1}); err != nil {
			return err
		}
		out, err := agentcred.Issue(w.t.Context(), q, w.now, audit.By{}, w.users["scribe"])
		tok = "bearer:" + out.Token
		return err
	})
	return tok
}

// preUserToken is a pre-user session: an applicant with no account.
func (w *settingsWorld) preUserToken() string {
	w.t.Helper()
	var applicant int64
	w.write(func(q *dbq.Queries) error {
		var err error
		applicant, err = q.CreateIdentity(w.t.Context(), dbq.CreateIdentityParams{ProviderKey: "github", Subject: "applicant",
			Issuer: "https://github.example", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	return sessiontest.PreUser(w.t, w.db, applicant, w.now)
}

// TestAvatarOverHTTP: a member sets their avatar, which the profile, /me
// and their author block then carry, replaces it and removes it; what is
// not a PNG or JPEG, over 1 MB, or not one part named file is refused, and
// a refusal changes nothing.
func TestAvatarOverHTTP(t *testing.T) {
	w := newSettingsWorld(t)
	alice := w.signIn("alice", "github", firefox)
	bob := w.signIn("bob", "github", firefox)

	rec := w.putAvatar(alice, part{"file", "me.png", "image/png", blobstest.PNG(500, 300, 1)})
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body)
	}
	got := decodeAs[dto.ProfileSettings](t, rec)
	if !blob.ValidHash(got.Avatar) {
		t.Fatalf("the answer's avatar is %q", got.Avatar)
	}
	h := got.Avatar
	if p := decodeAs[dto.ProfileSettings](t, w.do(alice, http.MethodGet, "/api/v1/settings/profile", "")); p.Avatar != h {
		t.Errorf("GET /settings/profile: %+v", p)
	}
	if me := decodeMe(t, w.do(alice, http.MethodGet, "/api/v1/me", "").Body.Bytes()); me.User == nil || me.User.Avatar != h {
		t.Errorf("/me: %+v", me.User)
	}
	if p := decodeAs[dto.Profile](t, w.do(bob, http.MethodGet, "/api/v1/users/alice", "")); p.User.Avatar != h {
		t.Errorf("alice's author block: %+v", p.User)
	}

	for name, c := range map[string]struct {
		parts  []part
		status int
		want   string
	}{
		"a GIF":      {[]part{{"file", "a.gif", "", blobstest.GIF(9, 9, 2)}}, 422, `"field":"file","code":"type_not_allowed"`},
		"a PDF":      {[]part{{"file", "a.png", "image/png", blobstest.PDF(3)}}, 422, `"field":"file","code":"type_not_allowed"`},
		"over 1 MB":  {[]part{{"file", "a.png", "", append(blobstest.PNG(4, 4, 4), make([]byte, blob.AvatarMaxBytes)...)}}, 413, "payload-too-large"},
		"no file":    {nil, 400, "bad-request"},
		"misnamed":   {[]part{{"avatar", "a.png", "", blobstest.PNG(4, 4, 5)}}, 400, "bad-request"},
		"a trailer":  {[]part{{"file", "a.png", "", blobstest.PNG(4, 4, 6)}, {"note", "", "", []byte("x")}}, 400, "bad-request"},
		"two images": {[]part{{"file", "a.png", "", blobstest.PNG(4, 4, 7)}, {"file", "b.png", "", blobstest.PNG(4, 4, 8)}}, 400, "bad-request"},
	} {
		rec := w.putAvatar(alice, c.parts...)
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if p := decodeAs[dto.ProfileSettings](t, w.do(alice, http.MethodGet, "/api/v1/settings/profile", "")); p.Avatar != h {
		t.Errorf("a refusal changed the avatar to %q", p.Avatar)
	}
	if staged := stagedIn(t, w.data); len(staged) != 0 {
		t.Errorf("staged files left: %v", staged)
	}
	// A JSON body is not a file.
	if rec := w.do(alice, http.MethodPut, "/api/v1/settings/avatar", `{"avatar":"`+h+`"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a JSON body: %d %s", rec.Code, rec.Body)
	}

	rec = w.putAvatar(alice, part{"file", "me.jpg", "", blobstest.JPEG(300, 300, 9)})
	next := decodeAs[dto.ProfileSettings](t, rec).Avatar
	if next == h || !blob.ValidHash(next) {
		t.Fatalf("replacing answered %q", next)
	}
	if rec := w.fetchAvatar(bob, h); rec.Code != http.StatusNotFound {
		t.Errorf("the replaced avatar answered %d", rec.Code)
	}

	rec = w.do(alice, http.MethodDelete, "/api/v1/settings/avatar", "")
	if p := decodeAs[dto.ProfileSettings](t, rec); p.Avatar != "" {
		t.Fatalf("DELETE answered %+v", p)
	}
	if rec := w.fetchAvatar(alice, next); rec.Code != http.StatusNotFound {
		t.Errorf("the removed avatar answered %d", rec.Code)
	}
	if me := decodeMe(t, w.do(alice, http.MethodGet, "/api/v1/me", "").Body.Bytes()); me.User.Avatar != "" {
		t.Errorf("/me after removing: %+v", me.User)
	}
}

func (w *settingsWorld) fetchAvatar(token, hash string) *httptest.ResponseRecorder {
	w.t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/avatars/"+hash, nil)
	if bearer, ok := strings.CutPrefix(token, "bearer:"); ok {
		r.Header.Set("Authorization", "Bearer "+bearer)
	} else if token != "" {
		r.AddCookie(sessiontest.Cookie(token))
	}
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, r)
	return rec
}

// TestAvatarFetchOverHTTP is the maintainer's rule through the chain, and
// security: GET /avatars/{hash} answers a signed-in member, an agent by its
// credential included, with the re-encoded image and an attachment's
// headers, and answers a signed-out reader and a pre-user not-found, in
// the same bytes as for a hash nothing has. /blobs/ does not serve it.
func TestAvatarFetchOverHTTP(t *testing.T) {
	w := newSettingsWorld(t)
	alice := w.signIn("alice", "github", firefox)
	bob := w.signIn("bob", "github", firefox)
	agent := w.agentToken()
	pre := w.preUserToken()
	h := decodeAs[dto.ProfileSettings](t, w.putAvatar(alice, part{"file", "me.png", "", blobstest.PNG(64, 64, 1)})).Avatar

	for name, tok := range map[string]string{"the owner": alice, "another member": bob, "an agent": agent} {
		rec := w.fetchAvatar(tok, h)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
			continue
		}
		hd := rec.Header()
		for k, want := range map[string]string{
			"Content-Type": "image/png", "Cache-Control": "private, no-cache", "Vary": "Cookie",
			"X-Content-Type-Options": "nosniff", "Content-Security-Policy": blobsweb.Policy,
			"Cross-Origin-Resource-Policy": "same-origin", "Content-Disposition": "inline", "ETag": `"` + h + `"`,
		} {
			if got := hd.Get(k); got != want {
				t.Errorf("%s: %s = %q, want %q", name, k, got, want)
			}
		}
		img, err := io.ReadAll(rec.Body)
		if err != nil || !bytes.HasPrefix(img, []byte("\x89PNG")) {
			t.Errorf("%s: the body is not a PNG", name)
		}
	}

	nothing := w.fetchAvatar("", strings.Repeat("0", 64))
	if nothing.Code != http.StatusNotFound {
		t.Fatalf("a hash nothing has: %d", nothing.Code)
	}
	// Signed out: not-found, in the bytes of a hash nothing has, and the
	// same when revalidating with the bytes a browser kept.
	if rec := w.fetchAvatar("", h); rec.Code != http.StatusNotFound || rec.Body.String() != nothing.Body.String() {
		t.Errorf("signed out: %d %s", rec.Code, rec.Body)
	}
	r := httptest.NewRequest(http.MethodGet, "/avatars/"+h, nil)
	r.Header.Set("If-None-Match", `"`+h+`"`)
	rr := httptest.NewRecorder()
	w.h.ServeHTTP(rr, r)
	if rr.Code != http.StatusNotFound {
		t.Errorf("signed out revalidating: %d", rr.Code)
	}
	// A pre-user is turned back to their application by the pre-user
	// gate before the route is reached (web.PreUserGate), and the service
	// refuses them too (TestAvatarServedOnlyToMembers).
	if rec := w.fetchAvatar(pre, h); rec.Code == http.StatusOK || bytes.Contains(rec.Body.Bytes(), []byte("\x89PNG")) {
		t.Errorf("a pre-user: %d", rec.Code)
	}
	for name, tok := range map[string]string{"the owner": alice, "another member": bob} {
		r := httptest.NewRequest(http.MethodGet, "/blobs/"+h, nil)
		r.AddCookie(sessiontest.Cookie(tok))
		rec := httptest.NewRecorder()
		w.h.ServeHTTP(rec, r)
		if rec.Code != http.StatusNotFound {
			t.Errorf("/blobs/ served the avatar to %s: %d", name, rec.Code)
		}
	}
	for _, p := range []string{"/avatars/", "/avatars/" + h + "/x", "/avatars/" + strings.ToUpper(h)} {
		r := httptest.NewRequest(http.MethodGet, p, nil)
		r.AddCookie(sessiontest.Cookie(bob))
		rec := httptest.NewRecorder()
		w.h.ServeHTTP(rec, r)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
}

// stagedIn lists blobs/tmp/ under root.
func stagedIn(t *testing.T, root string) []string {
	t.Helper()
	bw := &blobWorld{world: &world{t: t}, data: root}
	return bw.staged()
}

// TestStaffClearAnAvatar: a moderator clears a member's avatar, with a
// user.avatar_cleared row; a moderator may not clear staff's, an admin may
// not clear their own here, a member may not clear anyone's, and an account
// with no avatar is conflict.
func TestStaffClearAnAvatar(t *testing.T) {
	w := newAdminWorld(t)
	const h = "c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2"
	set := func(id int64) {
		t.Helper()
		if err := w.db.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			_, err := tx.ExecContext(t.Context(), `UPDATE users SET avatar_blob = ? WHERE id = ?`, h, id)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	avatar := func(id int64) string {
		t.Helper()
		var a sql.NullString
		if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT avatar_blob FROM users WHERE id = ?`, id).Scan(&a)
		}); err != nil {
			t.Fatal(err)
		}
		return a.String
	}
	for _, id := range []int64{1, 3, 4} {
		set(id)
	}
	for _, c := range []struct {
		as, handle string
		status     int
	}{
		{"member", "member", 403},
		{"anonymous", "member", 401},
		{"pre-user", "member", 403},
		{"moderator", "moderator", 403},
		{"moderator", "admin", 403},
		{"admin", "admin", 403},
		{"moderator", "nobody", 404},
	} {
		if rec := w.call(c.as, http.MethodDelete, "/api/v1/admin/users/"+c.handle+"/avatar", ""); rec.Code != c.status {
			t.Errorf("%s clearing %s: %d %s", c.as, c.handle, rec.Code, rec.Body)
		}
	}
	if avatar(1) != h || avatar(3) != h || avatar(4) != h {
		t.Fatal("a refusal cleared an avatar")
	}
	before := w.auditCount(account.ActionAvatarCleared)
	rec := w.call("moderator", http.MethodDelete, "/api/v1/admin/users/member/avatar", "")
	if rec.Code != http.StatusNoContent || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("a moderator clearing a member's: %d %s", rec.Code, rec.Body)
	}
	if avatar(1) != "" {
		t.Error("the member's avatar is still set")
	}
	if n := w.auditCount(account.ActionAvatarCleared) - before; n != 1 {
		t.Errorf("%d audit rows, want 1", n)
	}
	if rec := w.call("moderator", http.MethodDelete, "/api/v1/admin/users/member/avatar", ""); rec.Code != http.StatusConflict {
		t.Errorf("clearing again: %d", rec.Code)
	}
	if rec := w.call("admin", http.MethodDelete, "/api/v1/admin/users/moderator/avatar", ""); rec.Code != http.StatusNoContent || avatar(3) != "" {
		t.Errorf("an admin clearing a moderator's: %d", rec.Code)
	}
}
