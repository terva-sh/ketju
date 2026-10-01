package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/applications"
	"github.com/terva-sh/ketju/internal/service/blobs"
	"github.com/terva-sh/ketju/internal/service/blobs/blobstest"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/blob"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
	"github.com/terva-sh/ketju/internal/web"
	blobsweb "github.com/terva-sh/ketju/internal/web/blobs"
)

// Tests for uploading, attaching and serving blobs through the HTTP chain
// (TKT-01M29CXK0K, docs/07 "Attachments").

// uploadCap is the blob world's limits.attachment_mb, in bytes: small, so
// the cap tests send little.
const uploadCap = 64 << 10

// blobWorld is the seeded world with a blob store, and the whole chain in
// front of the API and /blobs/: body limits, rate limiting, CSRF, and a
// test session read from X-Test-Viewer.
type blobWorld struct {
	*world
	blobs *blobs.Service
	data  string
	h     http.Handler
	// roles are the viewers whose role is not member's.
	roles map[string]string
}

func newBlobWorld(t *testing.T) *blobWorld {
	t.Helper()
	w := newWorld(t)
	bw := &blobWorld{world: w, data: t.TempDir(), roles: map[string]string{"bob": domain.RoleModerator}}
	bw.blobs = &blobs.Service{DB: w.db, Root: bw.data, Perm: w.svc.Perm, Threads: w.svc, Max: uploadCap,
		Now: func() time.Time { return time.UnixMilli(w.clock) }}
	w.svc.Attachments = bw.blobs
	api := New(Deps{Threads: w.svc, Users: &users.Service{DB: w.db, Perm: perm.New(w.db)},
		Forums: &forums.Service{DB: w.db, Perm: perm.New(w.db)}, Forms: &formsvc.Service{DB: w.db},
		Applications: &applications.Service{DB: w.db}, Perm: w.svc.Perm, Blobs: bw.blobs})
	session := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(rw, r.WithContext(domain.WithViewer(r.Context(), bw.as(r.Header.Get("X-Test-Viewer")))))
		})
	}
	bw.h = web.New(web.Groups{API: api, Blobs: blobsweb.New(bw.blobs, nil)}, web.Options{
		Session: session, RateLimit: func(h http.Handler) http.Handler { return h },
		Limits: web.Limits{Upload: uploadCap},
	})
	return bw
}

// as is the viewer a test names: "" is anonymous, "pre-user" an applicant,
// "inactive:<handle>" a suspended member, and a handle a member, with the
// role roles gives it.
func (bw *blobWorld) as(who string) domain.Viewer {
	switch {
	case who == "":
		return domain.Anonymous()
	case who == "pre-user":
		return domain.Viewer{PreUser: true, IdentityID: 7, Active: true}
	case strings.HasPrefix(who, "inactive:"):
		v := bw.as(strings.TrimPrefix(who, "inactive:"))
		v.Active = false
		return v
	}
	role := domain.RoleMember
	if r, ok := bw.roles[who]; ok {
		role = r
	}
	return domain.Viewer{UserID: bw.users[who], Role: role, Active: true, SessionKey: "s-" + who}
}

// admin adds dora, an admin.
func (bw *blobWorld) admin() {
	bw.t.Helper()
	bw.newAdmin()
	bw.roles["dora"] = domain.RoleAdmin
}

// group returns a system group's id.
func (bw *blobWorld) group(slug string) int64 {
	bw.t.Helper()
	var id int64
	if err := bw.db.Read(bw.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		g, err := q.GetGroupBySlug(bw.t.Context(), slug)
		id = g.ID
		return err
	}); err != nil {
		bw.t.Fatal(err)
	}
	return id
}

// grantUpload gives the members group upload in forum, or everywhere for
// 0. A forum's own rows for a group replace the group's global ones there
// (perm), so a forum grant carries the global set with it, as an admin's
// grid would.
func (bw *blobWorld) grantUpload(forum int64) {
	bw.t.Helper()
	members := bw.group(domain.GroupMembers)
	changes := []perm.GrantChange{{ForumID: forum, GroupID: members, Permission: domain.PermUpload, Grant: true}}
	if forum != 0 {
		for _, p := range []domain.Permission{domain.PermRead, domain.PermCreateThread, domain.PermReply} {
			changes = append(changes, perm.GrantChange{ForumID: forum, GroupID: members, Permission: p, Grant: true})
		}
	}
	if err := perm.New(bw.db).ApplyGrants(bw.t.Context(), audit.By{}, changes); err != nil {
		bw.t.Fatal(err)
	}
}

func (bw *blobWorld) revokeUpload(forum int64) {
	bw.t.Helper()
	if err := perm.New(bw.db).Revoke(bw.t.Context(), audit.By{}, forum, bw.group(domain.GroupMembers), domain.PermUpload); err != nil {
		bw.t.Fatal(err)
	}
}

// multipartBody is a multipart/form-data body with one part per name in
// parts, in order, each a file part with the bytes given.
func multipartBody(t *testing.T, parts ...struct {
	field, filename, ctype string
	body                   []byte
}) (string, []byte) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for _, p := range parts {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, p.field, p.filename))
		if p.ctype != "" {
			h.Set("Content-Type", p.ctype)
		}
		pw, err := mw.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		pw.Write(p.body)
	}
	mw.Close()
	return mw.FormDataContentType(), b.Bytes()
}

type part = struct {
	field, filename, ctype string
	body                   []byte
}

// uploadTo POSTs one file part to /api/v1/blobs?<target> as who.
func (bw *blobWorld) uploadTo(who, target, filename string, body []byte) *httptest.ResponseRecorder {
	bw.t.Helper()
	ctype, b := multipartBody(bw.t, part{"file", filename, "", body})
	return bw.raw(who, http.MethodPost, "/api/v1/blobs?"+target, ctype, bytes.NewReader(b), int64(len(b)))
}

func (bw *blobWorld) raw(who, method, path, ctype string, body io.Reader, length int64) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, body)
	r.ContentLength = length
	if ctype != "" {
		r.Header.Set("Content-Type", ctype)
	}
	if who != "" {
		r.Header.Set("X-Test-Viewer", who)
	}
	rec := httptest.NewRecorder()
	bw.h.ServeHTTP(rec, r)
	return rec
}

// uploaded uploads to the world's forum and wants a 201.
func (bw *blobWorld) uploaded(who, filename string, body []byte) dto.Attachment {
	bw.t.Helper()
	rec := bw.uploadTo(who, "forum=general", filename, body)
	if rec.Code != http.StatusCreated {
		bw.t.Fatalf("upload as %s: %d %s", who, rec.Code, rec.Body)
	}
	var a dto.Attachment
	dec := json.NewDecoder(rec.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		bw.t.Fatal(err)
	}
	return a
}

// fetch GETs /blobs/<hash> as who, with header pairs.
func (bw *blobWorld) fetch(who, hash string, header ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/blobs/"+hash, nil)
	if who != "" {
		r.Header.Set("X-Test-Viewer", who)
	}
	for i := 0; i+1 < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	bw.h.ServeHTTP(rec, r)
	return rec
}

// replyWith posts a reply to id as who with attachments.
func (bw *blobWorld) replyWith(who string, id threadid.ID, hashes ...string) *httptest.ResponseRecorder {
	bw.t.Helper()
	b, _ := json.Marshal(map[string]any{"source": "files attached", "attachments": hashes})
	return bw.raw(who, http.MethodPost, postsPath(id), "application/json", bytes.NewReader(b), int64(len(b)))
}

// refcount is a blob's refcount, or -1 with no row.
func (bw *blobWorld) refcount(hash string) int64 {
	bw.t.Helper()
	var n int64 = -1
	if err := bw.db.Read(bw.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		b, err := q.GetBlob(bw.t.Context(), hash)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		n = b.Refcount
		return err
	}); err != nil {
		bw.t.Fatal(err)
	}
	return n
}

func (bw *blobWorld) quarantine(hash string) {
	bw.t.Helper()
	bw.write(func(q *dbq.Queries) error {
		_, err := q.SetBlobStatus(bw.t.Context(), dbq.SetBlobStatusParams{Status: "quarantined", Hash: hash})
		return err
	})
}

// staged lists blobs/tmp/.
func (bw *blobWorld) staged() []string {
	entries, err := os.ReadDir(blob.TmpDir(bw.data))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		bw.t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// fieldErrors is a validation-failed body's errors, as "field code".
func fieldErrors(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var p struct {
		Type   string `json:"type"`
		Errors []struct{ Field, Code string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("status %d is not a problem: %s", rec.Code, rec.Body)
	}
	var out []string
	for _, e := range p.Errors {
		out = append(out, e.Field+" "+e.Code)
	}
	return p.Type + ": " + strings.Join(out, ", ")
}

// TestUploadOverHTTP: the route answers 201 with the attachment, sniffs
// rather than believing the part's type, and refuses a body that is not
// one part named file.
func TestUploadOverHTTP(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	img := blobstest.PNG(4, 3, 1)
	ctype, b := multipartBody(t, part{"file", "notes.txt", "text/plain", img})
	rec := bw.raw("carol", http.MethodPost, "/api/v1/blobs?forum=general", ctype, bytes.NewReader(b), int64(len(b)))
	a := created[dto.Attachment](t, rec)
	if a.MIME != "image/png" || a.OriginalName != "notes.txt" || *a.Width != 4 || *a.Height != 3 {
		t.Fatalf("answer = %+v", a)
	}
	html := bw.uploadTo("carol", "forum=general", "x.png", blobstest.Refused["html"])
	if got := fieldErrors(t, html); got != "validation-failed: file type_not_allowed" {
		t.Fatalf("HTML named x.png: %d %s", html.Code, got)
	}
	// The two-part body's file is bytes nothing else uploads, so that a
	// row or file for them is the refused request's (Terva review 1736).
	refused := blobstest.PNG(5, 5, 2)
	for name, body := range map[string][]part{
		"no part":          nil,
		"a part not file":  {{"upload", "a.png", "", img}},
		"two parts":        {{"file", "a.png", "", refused}, {"file", "b.png", "", img}},
		"a trailing field": {{"file", "a.png", "", refused}, {"note", "", "", []byte("hi")}},
	} {
		ctype, b := multipartBody(t, body...)
		rec := bw.raw("carol", http.MethodPost, "/api/v1/blobs?forum=general", ctype, bytes.NewReader(b), int64(len(b)))
		if got := problemCode(t, rec); got != "bad-request" {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	for _, q := range []string{"", "thread=x&forum=general", "forum=", "thread=nope"} {
		ctype, b := multipartBody(t, part{"file", "a.png", "", img})
		rec := bw.raw("carol", http.MethodPost, "/api/v1/blobs?"+q, ctype, bytes.NewReader(b), int64(len(b)))
		if got := problemCode(t, rec); got != "bad-request" {
			t.Errorf("?%s: %d %s", q, rec.Code, rec.Body)
		}
	}
	// Not multipart at all.
	rec = bw.raw("carol", http.MethodPost, "/api/v1/blobs?forum=general", "application/json", strings.NewReader("{}"), 2)
	if got := problemCode(t, rec); got != "bad-request" {
		t.Errorf("a JSON body: %d %s", rec.Code, rec.Body)
	}
	if staged := bw.staged(); len(staged) != 0 {
		t.Fatalf("staged files left: %v", staged)
	}
	hash := sha256.Sum256(refused)
	h := hex.EncodeToString(hash[:])
	if n := bw.refcount(h); n != -1 {
		t.Errorf("a refused two-part upload left a blob row (refcount %d)", n)
	}
	path, err := blob.Path(bw.data, h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused two-part upload left its file: %v", err)
	}
}

// watched is an upload body that reads in small pieces and, before each,
// records the largest staged file, so a test can see how far a refused
// upload grew. It fails the test if read once fail is set.
type watched struct {
	t       *testing.T
	r       io.Reader
	dir     string
	largest int64
	fail    bool
}

func (w *watched) Read(p []byte) (int, error) {
	if w.fail {
		w.t.Error("the body was read")
		return 0, io.EOF
	}
	if entries, err := os.ReadDir(w.dir); err == nil {
		for _, e := range entries {
			if info, err := e.Info(); err == nil && info.Size() > w.largest {
				w.largest = info.Size()
			}
		}
	}
	if len(p) > 1024 {
		p = p[:1024]
	}
	return w.r.Read(p)
}

// TestUploadCapDuringRead: a chunked body whose file is one byte over the
// cap, and one far over it, are each payload-too-large; the staged file
// never grew past the cap and one byte, and is gone afterwards.
func TestUploadCapDuringRead(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	for _, size := range []int{uploadCap + 1, 3 * uploadCap} {
		ctype, b := multipartBody(t, part{"file", "big.txt", "", bytes.Repeat([]byte("a"), size)})
		body := &watched{t: t, r: bytes.NewReader(b), dir: blob.TmpDir(bw.data)}
		rec := bw.raw("carol", http.MethodPost, "/api/v1/blobs?forum=general", ctype, body, -1)
		if got := problemCode(t, rec); got != "payload-too-large" || rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("%d bytes: %d %s", size, rec.Code, rec.Body)
		}
		if body.largest > uploadCap+1 {
			t.Errorf("%d bytes: the staged file reached %d bytes", size, body.largest)
		}
		if staged := bw.staged(); len(staged) != 0 {
			t.Fatalf("%d bytes: staged files left: %v", size, staged)
		}
	}
	// A file of exactly the cap goes through, multipart headers and all.
	a := bw.uploaded("carol", "max.txt", bytes.Repeat([]byte("a"), uploadCap))
	if a.Size != uploadCap {
		t.Fatalf("size %d", a.Size)
	}
}

// TestUploadContentLengthOverCap: an honest Content-Length over the limit
// is refused before a byte of the body is read.
func TestUploadContentLengthOverCap(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	body := &watched{t: t, r: strings.NewReader(""), fail: true}
	rec := bw.raw("carol", http.MethodPost, "/api/v1/blobs?forum=general", "multipart/form-data; boundary=x",
		body, uploadCap+web.UploadOverhead+1)
	if got := problemCode(t, rec); got != "payload-too-large" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

// TestUploadPermission: who may upload, and how everyone else is refused,
// each before the body is read.
func TestUploadPermission(t *testing.T) {
	bw := newBlobWorld(t)
	bw.admin()
	img := blobstest.PNG(2, 2, 3)
	id, _ := bw.seed()
	hidden := bw.forumAt("staff", domain.VisibilityRestricted, false)
	hiddenThread := bw.threadIn(50, hidden, "Staff only", "bob")
	bw.addUsers("active", "gina")
	var uploaders int64
	bw.write(func(q *dbq.Queries) error {
		var err error
		uploaders, err = q.CreateGroup(t.Context(), dbq.CreateGroupParams{Name: "Uploaders", Slug: "uploaders",
			CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	e := perm.New(bw.db)
	if err := e.AddMember(t.Context(), audit.By{}, uploaders, bw.users["gina"]); err != nil {
		t.Fatal(err)
	}
	if err := e.Grant(t.Context(), audit.By{}, bw.forum, uploaders, domain.PermUpload); err != nil {
		t.Fatal(err)
	}

	check := func(who, target, want string) {
		t.Helper()
		ctype, b := multipartBody(t, part{"file", "a.png", "", img})
		body := &watched{t: t, r: bytes.NewReader(b), dir: blob.TmpDir(bw.data), fail: want != "201"}
		rec := bw.raw(who, http.MethodPost, "/api/v1/blobs?"+target, ctype, body, int64(len(b)))
		got := fmt.Sprint(rec.Code)
		if rec.Code != http.StatusCreated {
			got = problemCode(t, rec)
		}
		if got != want {
			t.Errorf("%q to %s: %d %s, want %s", who, target, rec.Code, rec.Body, want)
		}
	}
	thread := "thread=" + id.String()
	check("", "forum=general", "unauthenticated")
	check("", thread, "unauthenticated")
	check("pre-user", "forum=general", "forbidden")
	check("inactive:carol", "forum=general", "unauthenticated")
	check("carol", "forum=general", "forbidden") // no grant
	check("carol", thread, "forbidden")
	check("carol", "forum=staff", "not-found")
	check("carol", "thread="+hiddenThread.String(), "not-found")
	check("carol", "forum=nowhere", "not-found")
	check("gina", "forum=general", "201") // a group grant
	check("gina", thread, "201")
	check("dora", "forum=general", "201") // an admin, with no grant
	check("dora", "forum=staff", "201")

	bw.grantUpload(bw.forum)
	check("carol", "forum=general", "201")
	bw.archive()
	check("carol", "forum=general", "forbidden")
	check("carol", thread, "forbidden")
	check("gina", "forum=general", "forbidden")
	check("dora", "forum=general", "201")
}

// TestTwoPostsTwoRefs: the same bytes, uploaded by two members and
// attached to a post by each, are one blob at refcount 2.
func TestTwoPostsTwoRefs(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	img := blobstest.PNG(3, 3, 2)
	a := bw.uploaded("alice", "a.png", img)
	bw.uploaded("carol", "c.png", img)
	if n := bw.refcount(a.Hash); n != 0 {
		t.Fatalf("refcount %d before any post", n)
	}
	created[dto.CreatedPost](t, bw.replyWith("alice", id, a.Hash))
	bw.clock += 100_000
	created[dto.CreatedPost](t, bw.replyWith("carol", id, a.Hash))
	if n := bw.refcount(a.Hash); n != 2 {
		t.Fatalf("refcount %d, want 2", n)
	}
}

// recordingWriter is an Appender that runs before ahead of each append.
type recordingWriter struct {
	inner  threads.Appender
	before func(a threadlog.Append) error
}

func (r *recordingWriter) Append(th *threadcache.Thread, a threadlog.Append) (threadlog.Result, error) {
	if r.before != nil {
		if err := r.before(a); err != nil {
			return threadlog.Result{}, err
		}
	}
	return r.inner.Append(th, a)
}

// TestAttachRaisesRefcountBeforeAppend: the refcount is up when the Post is
// appended, not after (docs/03 "Blob garbage collection").
func TestAttachRaisesRefcountBeforeAppend(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	a := bw.uploaded("carol", "a.png", blobstest.PNG(2, 2, 11))
	var at int64 = -2
	bw.svc.Writer = &recordingWriter{inner: bw.svc.Writer, before: func(threadlog.Append) error {
		at = bw.refcount(a.Hash)
		return nil
	}}
	created[dto.CreatedPost](t, bw.replyWith("carol", id, a.Hash))
	if at != 1 {
		t.Fatalf("the refcount was %d when the Post was appended, want 1", at)
	}
}

// TestAttachDropsRefsWhenAppendFails: a failed append takes the references
// back, so the blob is unreferenced again, and the log is as it was.
func TestAttachDropsRefsWhenAppendFails(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	a := bw.uploaded("carol", "a.png", blobstest.PNG(2, 2, 12))
	b := bw.uploaded("carol", "b.pdf", blobstest.PDF(12))
	bw.svc.Writer = &recordingWriter{inner: bw.svc.Writer, before: func(threadlog.Append) error {
		return errors.New("disk full")
	}}
	before := bw.logState(id)
	rec := bw.replyWith("carol", id, a.Hash, b.Hash)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	for _, h := range []string{a.Hash, b.Hash} {
		if n := bw.refcount(h); n != 0 {
			t.Errorf("refcount %d after a failed append", n)
		}
	}
	var unreferenced int
	bw.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM blobs WHERE unreferenced_since IS NOT NULL`).Scan(&unreferenced)
	})
	if unreferenced != 2 {
		t.Errorf("%d blobs unreferenced, want 2", unreferenced)
	}
	if after := bw.logState(id); after != before {
		t.Errorf("the log moved: %+v then %+v", before, after)
	}
}

// collecting is an Attachments whose Check passes and then deletes the
// blobs' rows, as a collector pass landing between the check and the
// append would.
type collecting struct {
	*blobs.Service
	bw *blobWorld
}

func (c collecting) Check(ctx context.Context, v domain.Viewer, forumID int64, hashes []string) ([]string, error) {
	out, err := c.Service.Check(ctx, v, forumID, hashes)
	for _, h := range out {
		c.bw.exec(`DELETE FROM blobs WHERE hash = ?`, h)
	}
	return out, err
}

// TestAttachCollectedBlobRefused: a blob collected after the check and
// before the append refuses the attach, and nothing is appended.
func TestAttachCollectedBlobRefused(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	a := bw.uploaded("carol", "a.png", blobstest.PNG(2, 2, 13))
	bw.svc.Attachments = collecting{Service: bw.blobs, bw: bw}
	before := bw.logState(id)
	rec := bw.replyWith("carol", id, a.Hash)
	if got := fieldErrors(t, rec); got != "validation-failed: attachments unknown" {
		t.Fatalf("%d %s", rec.Code, got)
	}
	if after := bw.logState(id); after != before {
		t.Errorf("the log moved: %+v then %+v", before, after)
	}
}

// TestAttachRefusals: someone else's blob, a quarantined one, an unknown
// hash, eleven, and a poster without upload are each refused before
// anything is appended; ten, with repeats, are one each.
func TestAttachRefusals(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	alices := bw.uploaded("alice", "a.png", blobstest.PNG(2, 2, 14))
	q := bw.uploaded("carol", "q.png", blobstest.PNG(2, 2, 15))
	bw.quarantine(q.Hash)
	var mine []string
	for i := range 11 {
		mine = append(mine, bw.uploaded("carol", fmt.Sprintf("%d.txt", i), blobstest.Text(byte(i))).Hash)
	}
	before := bw.logState(id)
	for name, c := range map[string]struct {
		hashes []string
		want   string
	}{
		"TestAttachSomeoneElsesBlobRefused": {[]string{alices.Hash}, "validation-failed: attachments not_yours"},
		"TestAttachQuarantinedRefused":      {[]string{q.Hash}, "validation-failed: attachments blocked"},
		"an unknown hash":                   {[]string{strings.Repeat("e", 64)}, "validation-failed: attachments unknown"},
		"not a hash":                        {[]string{"../../etc/passwd"}, "validation-failed: attachments unknown"},
		"TestAttachLimit":                   {mine, "validation-failed: attachments too_many"},
	} {
		rec := bw.replyWith("carol", id, c.hashes...)
		if got := fieldErrors(t, rec); got != c.want {
			t.Errorf("%s: %d %s, want %s", name, rec.Code, got, c.want)
		}
	}
	if after := bw.logState(id); after != before {
		t.Fatalf("a refusal moved the log: %+v then %+v", before, after)
	}

	// TestAttachNeedsUpload: carol uploaded these with the grant, which is
	// now gone.
	bw.revokeUpload(bw.forum)
	if rec := bw.replyWith("carol", id, mine[0]); problemCode(t, rec) != "forbidden" {
		t.Errorf("without upload: %d %s", rec.Code, rec.Body)
	}
	if err := perm.New(bw.db).Grant(t.Context(), audit.By{}, bw.forum, bw.group(domain.GroupMembers), domain.PermUpload); err != nil {
		t.Fatal(err)
	}

	ten := append(append([]string{}, mine[:10]...), mine[0], mine[3])
	p := created[dto.CreatedPost](t, bw.replyWith("carol", id, ten...))
	if len(p.Post.Attachments) != 10 || p.Post.Attachments[0].Hash != mine[0] || p.Post.Attachments[9].Hash != mine[9] {
		t.Fatalf("ten with repeats: %+v", p.Post.Attachments)
	}
	for _, h := range mine[:10] {
		if n := bw.refcount(h); n != 1 {
			t.Fatalf("refcount %d for a hash sent twice, want 1", n)
		}
	}
}

// TestReplyWithAttachmentIsServableAtOnce: the reply's answer is written
// after its post_attachments rows, so its files load the moment the client
// shows it. The world's Writer feeds no projection: only Reply's own
// projection can have written them.
func TestReplyWithAttachmentIsServableAtOnce(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	a := bw.uploaded("carol", "a.png", blobstest.PNG(2, 2, 16))
	p := created[dto.CreatedPost](t, bw.replyWith("carol", id, a.Hash))
	if len(p.Post.Attachments) != 1 || p.Post.Attachments[0].OriginalName != "a.png" {
		t.Fatalf("the answer's attachments: %+v", p.Post.Attachments)
	}
	for _, who := range []string{"carol", "alice", ""} {
		if rec := bw.fetch(who, a.Hash); rec.Code != http.StatusOK {
			t.Errorf("%q: %d %s", who, rec.Code, rec.Body)
		}
	}
}

// TestCreateThreadWithAttachments: a new thread's first post carries its
// attachments, servable at once, with refcount 1.
func TestCreateThreadWithAttachments(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	a := bw.uploaded("carol", "plan.pdf", blobstest.PDF(17))
	b, _ := json.Marshal(map[string]any{"title": "Plans", "source": "attached", "attachments": []string{a.Hash}})
	rec := bw.raw("carol", http.MethodPost, "/api/v1/forums/general/threads", "application/json", bytes.NewReader(b), int64(len(b)))
	th := created[dto.CreatedThread](t, rec)
	if len(th.Post.Post.Attachments) != 1 || th.Post.Post.Attachments[0].MIME != "application/pdf" {
		t.Fatalf("attachments: %+v", th.Post.Post.Attachments)
	}
	if rec := bw.fetch("alice", a.Hash); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if n := bw.refcount(a.Hash); n != 1 {
		t.Fatalf("refcount %d", n)
	}
	// Someone else's blob is refused as for a reply.
	alices := bw.uploaded("alice", "x.png", blobstest.PNG(2, 2, 18))
	b, _ = json.Marshal(map[string]any{"title": "Mine", "source": "x", "attachments": []string{alices.Hash}})
	rec = bw.raw("carol", http.MethodPost, "/api/v1/forums/general/threads", "application/json", bytes.NewReader(b), int64(len(b)))
	if got := fieldErrors(t, rec); got != "validation-failed: attachments not_yours" {
		t.Fatalf("%d %s", rec.Code, got)
	}
}

// noncePattern is the per-response nonce in the page policy.
var noncePattern = regexp.MustCompile(`nonce-[A-Za-z0-9+/=_-]+`)

// blobAnswer is what a refusal must not differ in from a missing hash's.
func blobAnswer(rec *httptest.ResponseRecorder) string {
	var hs []string
	for _, k := range []string{"Content-Type", "Cache-Control", "Vary", "Content-Security-Policy", "ETag",
		"Content-Disposition", "Cross-Origin-Resource-Policy", "X-Content-Type-Options"} {
		hs = append(hs, k+": "+noncePattern.ReplaceAllString(rec.Header().Get(k), "nonce-N"))
	}
	return fmt.Sprintf("%d\n%s\n%s", rec.Code, strings.Join(hs, "\n"), rec.Body)
}

// TestBlobServedOnlyToReaders is the docs/14 invariant: a blob is served
// only to a viewer who may read, by the thread page's rules, the body of a
// post naming it in its attachments, and every refusal is byte for byte
// the answer to a hash nothing has.
func TestBlobServedOnlyToReaders(t *testing.T) {
	bw := newBlobWorld(t)
	bw.admin()
	bw.grantUpload(0)
	bw.groupModerator(bw.forum)
	bw.addUsers("active", "erin")
	members := bw.forumAt("lounge", domain.VisibilityMembers, false)
	staff := bw.forumAt("staff", domain.VisibilityRestricted, false)

	var seq uint64 = 100
	var seed byte = 100
	// posted is a new thread in forum by author whose second post, by
	// author too, attaches a fresh blob; then is applied to it.
	posted := func(forum int64, author string, then func(id threadid.ID)) string {
		t.Helper()
		seq++
		seed++
		id := bw.threadIn(seq, forum, fmt.Sprintf("Case %d", seq), author)
		a := bw.uploaded(author, "f.png", blobstest.PNG(2, 2, seed))
		created[dto.CreatedPost](t, bw.replyWith(author, id, a.Hash))
		bw.clock += 100_000
		if then != nil {
			then(id)
		}
		return a.Hash
	}
	appendTo := func(id threadid.ID, typ threadlog.RecordType, by string, body any) {
		bw.append(id, threadlog.Append{Type: typ, Author: uint64(bw.users[by]), Ordinal: 2, Body: body})
	}

	visible := posted(bw.forum, "alice", nil)
	membersOnly := posted(members, "alice", nil)
	hiddenForum := posted(staff, "dora", nil)
	authorDeleted := posted(bw.forum, "alice", func(id threadid.ID) {
		appendTo(id, threadlog.TypeDelete, "alice", &threadlog.DeleteBody{})
	})
	modHidden := posted(bw.forum, "alice", func(id threadid.ID) {
		appendTo(id, threadlog.TypeDelete, "bob", &threadlog.DeleteBody{ByModerator: true, Reason: "off topic"})
	})
	review := posted(bw.forum, "alice", func(id threadid.ID) {
		appendTo(id, threadlog.TypeDelete, "bob", &threadlog.DeleteBody{Auto: true})
	})
	var redactedThread threadid.ID
	redacted := posted(bw.forum, "alice", func(id threadid.ID) { redactedThread = id })
	moved := posted(bw.forum, "alice", func(id threadid.ID) {
		bw.append(id, threadlog.Append{Type: threadlog.TypeThreadMoved, Author: uint64(bw.users["dora"]),
			Body: &threadlog.ThreadMovedBody{ForumID: uint64(staff)}})
	})
	suspendedAuthor := posted(bw.forum, "erin", nil)
	bulkHidden := posted(bw.forum, "erin", func(id threadid.ID) {
		appendTo(id, threadlog.TypeDelete, "bob", &threadlog.DeleteBody{ByModerator: true, Suspension: 1})
	})
	bw.exec(`UPDATE users SET status = 'suspended' WHERE handle = 'erin'`)
	unattached := bw.uploaded("alice", "lonely.png", blobstest.PNG(2, 2, 200)).Hash
	quarantined := posted(bw.forum, "alice", nil)
	bw.quarantine(quarantined)
	sourceOnly := bw.uploaded("alice", "src.png", blobstest.PNG(2, 2, 201)).Hash
	srcThread := bw.threadIn(300, bw.forum, "Source only", "alice")
	b, _ := json.Marshal(map[string]any{"source": "![x](/blobs/" + sourceOnly + ")"})
	created[dto.CreatedPost](t, bw.raw("alice", http.MethodPost, postsPath(srcThread), "application/json",
		bytes.NewReader(b), int64(len(b))))

	// The redaction comes last: it restarts the world's stores.
	bw.redactOffline(redactedThread, 2)
	// Even with its row put back, the redacted post opens to nobody.
	bw.exec(`INSERT INTO post_attachments (thread_id, ordinal, hash, position) VALUES (?, 2, ?, 0)`,
		int64(redactedThread), redacted)

	everyone := []string{"", "carol", "alice", "erin", "bob", "gina", "dora", "inactive:carol"}
	cases := []struct {
		name    string
		hash    string
		served  []string
		refused []string
	}{
		{"visible post", visible, []string{"", "carol", "alice", "inactive:carol"}, nil},
		{"members-only forum", membersOnly, []string{"carol", "alice"}, []string{"", "inactive:carol"}},
		{"hidden forum", hiddenForum, []string{"dora"}, []string{"", "carol", "alice", "bob", "gina", "inactive:carol"}},
		{"author-deleted post", authorDeleted, []string{"bob", "gina", "dora"}, []string{"", "carol", "alice"}},
		{"moderator-hidden post", modHidden, []string{"alice", "bob", "gina", "dora"}, []string{"", "carol", "inactive:alice"}},
		{"post hidden pending review", review, []string{"alice", "bob", "gina", "dora"}, []string{"", "carol"}},
		{"redacted post", redacted, nil, everyone},
		{"thread moved to an unreadable forum", moved, []string{"dora"}, []string{"", "carol", "alice", "bob", "gina"}},
		{"suspended author's visible post", suspendedAuthor, []string{"", "carol", "alice"}, nil},
		{"suspended author's post after the bulk hide", bulkHidden, []string{"bob", "gina", "dora"}, []string{"", "carol", "inactive:erin"}},
		{"uploaded, attached to no post", unattached, nil, everyone},
		{"quarantined", quarantined, nil, everyone},
		{"named only by a /blobs URL in a source", sourceOnly, nil, everyone},
		{"malformed hash", strings.ToUpper(visible), nil, everyone},
		{"malformed hash, short", visible[:63], nil, everyone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, who := range c.served {
				if rec := bw.fetch(who, c.hash); rec.Code != http.StatusOK {
					t.Errorf("%q was refused: %d %s", who, rec.Code, rec.Body)
				}
			}
			for _, who := range c.refused {
				got := blobAnswer(bw.fetch(who, c.hash))
				want := blobAnswer(bw.fetch(who, strings.Repeat("0", 64)))
				if got != want {
					t.Errorf("%q's refusal differs from a missing hash's:\n%s\n---\n%s", who, got, want)
				}
				if !strings.HasPrefix(got, "404") {
					t.Errorf("%q: %s", who, got)
				}
			}
		})
	}
}

// TestBlobHeaders: what a served blob carries, and that ETag, Range and
// HEAD work.
func TestBlobHeaders(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	img := blobstest.PNG(3, 2, 30)
	pdf := blobstest.PDF(30)
	a := bw.uploaded("carol", "a.png", img)
	b := bw.uploaded("carol", "b.pdf", pdf)
	created[dto.CreatedPost](t, bw.replyWith("carol", id, a.Hash, b.Hash))

	rec := bw.fetch("alice", a.Hash)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), img) {
		t.Fatalf("%d, %d bytes", rec.Code, rec.Body.Len())
	}
	for k, want := range map[string]string{
		"Content-Type":                 "image/png",
		"Cache-Control":                "private, no-cache",
		"Vary":                         "Cookie",
		"ETag":                         `"` + a.Hash + `"`,
		"X-Content-Type-Options":       "nosniff",
		"Content-Security-Policy":      "default-src 'none'; sandbox",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Content-Disposition":          "inline",
		"Content-Length":               fmt.Sprint(len(img)),
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s: %q, want %q", k, got, want)
		}
	}
	if d := bw.fetch("alice", b.Hash).Header().Get("Content-Disposition"); d != "attachment" {
		t.Errorf("a PDF's disposition: %q", d)
	}
	if rec := bw.fetch("alice", a.Hash, "If-None-Match", `"`+a.Hash+`"`); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Errorf("If-None-Match: %d", rec.Code)
	}
	rec = bw.fetch("alice", a.Hash, "Range", "bytes=0-7")
	if rec.Code != http.StatusPartialContent || !bytes.Equal(rec.Body.Bytes(), img[:8]) {
		t.Errorf("Range: %d %q", rec.Code, rec.Body.Bytes())
	}
	r := httptest.NewRequest(http.MethodHead, "/blobs/"+a.Hash, nil)
	r.Header.Set("X-Test-Viewer", "alice")
	head := httptest.NewRecorder()
	bw.h.ServeHTTP(head, r)
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("ETag") == "" {
		t.Errorf("HEAD: %d, %d bytes", head.Code, head.Body.Len())
	}
	// No other method, and no other path under /blobs/, answers anything
	// but not-found.
	for _, p := range []string{"/blobs/" + a.Hash + "/thumb", "/blobs/", "/blobs/" + a.Hash + "/x"} {
		if rec := bw.fetch("alice", strings.TrimPrefix(p, "/blobs/")); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
}

// TestRevalidationRechecksAccess: a conditional request is authorized
// before any 304, so a reader who has lost access gets not-found, never a
// 304 that would let the browser keep showing the file.
func TestRevalidationRechecksAccess(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(0)
	lounge := bw.forumAt("lounge", domain.VisibilityMembers, false)
	id := bw.threadIn(70, lounge, "Lounge", "alice")
	a := bw.uploaded("alice", "a.png", blobstest.PNG(2, 2, 40))
	created[dto.CreatedPost](t, bw.replyWith("alice", id, a.Hash))
	tag := `"` + a.Hash + `"`
	// What a hash nothing has answers, to the same conditional request. It
	// must be a 404: a refusal that answered 304 would let the browser keep
	// showing the file.
	missing := func(who string) string {
		t.Helper()
		rec := bw.fetch(who, strings.Repeat("0", 64), "If-None-Match", tag)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("a missing hash answers %d", rec.Code)
		}
		return blobAnswer(rec)
	}

	if rec := bw.fetch("carol", a.Hash, "If-None-Match", tag); rec.Code != http.StatusNotModified {
		t.Fatalf("a reader's revalidation: %d", rec.Code)
	}
	// Signed out: the forum is members-only.
	if got := blobAnswer(bw.fetch("", a.Hash, "If-None-Match", tag)); got != missing("") {
		t.Errorf("signed out:\n%s", got)
	}
	// A perm write takes read away from members.
	bw.write(func(q *dbq.Queries) error {
		return q.RevokePermission(t.Context(), dbq.RevokePermissionParams{
			ForumID: sql.NullInt64{}, GroupID: bw.group(domain.GroupMembers), Permission: string(domain.PermRead)})
	})
	perm.New(bw.db).Invalidate()
	if got := blobAnswer(bw.fetch("carol", a.Hash, "If-None-Match", tag)); got != missing("carol") {
		t.Errorf("after read was revoked:\n%s", got)
	}
	bw.write(func(q *dbq.Queries) error {
		return q.GrantPermission(t.Context(), dbq.GrantPermissionParams{
			ForumID: sql.NullInt64{}, GroupID: bw.group(domain.GroupMembers), Permission: string(domain.PermRead), CreatedAt: 1})
	})
	perm.New(bw.db).Invalidate()
	if rec := bw.fetch("carol", a.Hash, "If-None-Match", tag); rec.Code != http.StatusNotModified {
		t.Fatalf("read restored: %d", rec.Code)
	}
	// The post is hidden by a moderator.
	bw.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(bw.users["bob"]), Ordinal: 2,
		Body: &threadlog.DeleteBody{ByModerator: true}})
	if got := blobAnswer(bw.fetch("carol", a.Hash, "If-None-Match", tag)); got != missing("carol") {
		t.Errorf("after the post was hidden:\n%s", got)
	}
	if rec := bw.fetch("alice", a.Hash, "If-None-Match", tag); rec.Code != http.StatusNotModified {
		t.Errorf("the author of a hidden post: %d", rec.Code)
	}
}

// pageAttachments is the attachments of post ordinal on id's first page,
// as who sees it, read twice so the second is the response cache's.
func (bw *blobWorld) pageAttachments(who string, id threadid.ID, ordinal int) []dto.Attachment {
	bw.t.Helper()
	var out []dto.Attachment
	for range 2 {
		p := page(bw.t, bw.get(bw.as(who), "/api/v1/threads/"+id.String()))
		for _, post := range p.Posts {
			if post.Ordinal == ordinal {
				out = post.Attachments
			}
		}
	}
	return out
}

// TestThreadPageAttachmentMeta: a post's attachments carry the blob's size,
// type and dimensions and the name its author uploaded it under, in the
// order posted, and an edit, whose record has no list, keeps them.
func TestThreadPageAttachmentMeta(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	img := bw.uploaded("carol", "photo.png", blobstest.PNG(6, 5, 50))
	doc := bw.uploaded("carol", "doc.pdf", blobstest.PDF(50))
	p := created[dto.CreatedPost](t, bw.replyWith("carol", id, doc.Hash, img.Hash))
	want := []dto.Attachment{doc, img}
	check := func(when string) {
		t.Helper()
		got := bw.pageAttachments("alice", id, int(p.Ordinal))
		gb, _ := json.Marshal(got)
		wb, _ := json.Marshal(want)
		if !bytes.Equal(gb, wb) {
			t.Errorf("%s: %s, want %s", when, gb, wb)
		}
	}
	check("as posted")
	body, _ := json.Marshal(map[string]any{"source": "edited"})
	rec := bw.raw("carol", http.MethodPatch, fmt.Sprintf("%s/%d", postsPath(id), p.Ordinal), "application/json",
		bytes.NewReader(body), int64(len(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body)
	}
	check("after an edit")
	bw.reindex()
	check("after a reindex")
	// An edit takes no attachments.
	body, _ = json.Marshal(map[string]any{"source": "edited", "attachments": []string{img.Hash}})
	rec = bw.raw("carol", http.MethodPatch, fmt.Sprintf("%s/%d", postsPath(id), p.Ordinal), "application/json",
		bytes.NewReader(body), int64(len(body)))
	if problemCode(t, rec) != "bad-request" {
		t.Errorf("an edit with attachments: %d %s", rec.Code, rec.Body)
	}
}

// TestReuploadKeepsFirstName: an author who attached a.png and then
// uploads the same bytes as b.png is answered a.png, and the post still
// reads a.png, from the page and from the response cache.
func TestReuploadKeepsFirstName(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	img := blobstest.PNG(2, 2, 51)
	a := bw.uploaded("carol", "a.png", img)
	p := created[dto.CreatedPost](t, bw.replyWith("carol", id, a.Hash))
	bw.pageAttachments("alice", id, int(p.Ordinal)) // fill the cache
	again := bw.uploaded("carol", "b.png", img)
	if again.OriginalName != "a.png" {
		t.Fatalf("the re-upload answers %q", again.OriginalName)
	}
	if got := bw.pageAttachments("alice", id, int(p.Ordinal)); len(got) != 1 || got[0].OriginalName != "a.png" {
		t.Fatalf("the post reads %+v", got)
	}
}

// TestSecondUploaderNeverSeesFirstName: carol uploads bytes alice posted as
// secret-plan.png, and nothing she is served names it: not her upload's
// answer, and not her own post's attachment.
func TestSecondUploaderNeverSeesFirstName(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	img := blobstest.PNG(2, 2, 52)
	a := bw.uploaded("alice", "secret-plan.png", img)
	created[dto.CreatedPost](t, bw.replyWith("alice", id, a.Hash))
	bw.clock += 100_000
	c := bw.uploaded("carol", "cat.png", img)
	if c.OriginalName != "cat.png" {
		t.Fatalf("carol's answer: %+v", c)
	}
	p := created[dto.CreatedPost](t, bw.replyWith("carol", id, c.Hash))
	if got := bw.pageAttachments("carol", id, int(p.Ordinal)); len(got) != 1 || got[0].OriginalName != "cat.png" {
		t.Fatalf("carol's post: %+v", got)
	}
	// alice's own post still shows her name, to anyone who reads it.
	if got := bw.pageAttachments("carol", id, int(p.Ordinal)-1); len(got) != 1 || got[0].OriginalName != "secret-plan.png" {
		t.Fatalf("alice's post: %+v", got)
	}
}

// TestTombstoneCarriesNoAttachments: a member viewing a removed post that
// had attachments is sent none, while a moderator gets them with the body.
func TestTombstoneCarriesNoAttachments(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	a := bw.uploaded("carol", "a.png", blobstest.PNG(2, 2, 53))
	p := created[dto.CreatedPost](t, bw.replyWith("carol", id, a.Hash))
	bw.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(bw.users["carol"]), Ordinal: p.Ordinal,
		Body: &threadlog.DeleteBody{}})
	for who, want := range map[string]int{"alice": 0, "": 0, "carol": 0, "bob": 1} {
		if got := bw.pageAttachments(who, id, int(p.Ordinal)); len(got) != want {
			t.Errorf("%q sees %d attachments, want %d", who, len(got), want)
		}
	}
}

// TestThreadViewerCanUpload: can_upload is upload in the thread's forum, for
// a member, and false in an archived forum but to an admin.
func TestThreadViewerCanUpload(t *testing.T) {
	bw := newBlobWorld(t)
	bw.admin()
	id, _ := bw.seed()
	can := func(who string) bool {
		return page(t, bw.get(bw.as(who), "/api/v1/threads/"+id.String())).Viewer.CanUpload
	}
	if can("carol") || can("") || !can("dora") {
		t.Fatal("before any grant")
	}
	bw.grantUpload(bw.forum)
	if !can("carol") || can("") {
		t.Fatal("after the grant")
	}
	bw.archive()
	if can("carol") || !can("dora") {
		t.Fatal("in an archived forum")
	}
}

// TestForumTreeCanUpload: GET /forums says per forum whether the viewer may
// upload there.
func TestForumTreeCanUpload(t *testing.T) {
	bw := newBlobWorld(t)
	bw.forumAt("other", domain.VisibilityPublic, false)
	bw.grantUpload(bw.forum)
	tree := func(who string) map[string]bool {
		rec := bw.get(bw.as(who), "/api/v1/forums")
		var tr dto.ForumTree
		if err := json.Unmarshal(rec.Body.Bytes(), &tr); err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, f := range tr.Forums {
			out[f.Slug] = f.CanUpload
		}
		return out
	}
	if got := tree("carol"); !got["general"] || got["other"] {
		t.Fatalf("carol: %v", got)
	}
	if got := tree(""); got["general"] || got["other"] {
		t.Fatalf("anonymous: %v", got)
	}
}
