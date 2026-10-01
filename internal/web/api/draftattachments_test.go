package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/service/blobs/blobstest"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Tests for the attachments a draft keeps (TKT-01M3V73DP6, docs/07
// "Drafts", decided by the run 2026-10-01): checked as a post's are, adding
// no reference, and gone from a tombstone.

// putJSON PUTs body, marshalled, to path as who.
func (bw *blobWorld) putJSON(who, path string, body map[string]any) *httptest.ResponseRecorder {
	bw.t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		bw.t.Fatal(err)
	}
	return bw.raw(who, http.MethodPut, path, "application/json", bytes.NewReader(b), int64(len(b)))
}

// decodeOK decodes a 200 strictly into T.
func decodeOK[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body)
	}
	dec := json.NewDecoder(rec.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decoding %T: %v", v, err)
	}
	return v
}

// names lists attachments as "name size", in order.
func names(as []dto.Attachment) string {
	var out []string
	for _, a := range as {
		out = append(out, fmt.Sprintf("%s %d", a.OriginalName, a.Size))
	}
	return strings.Join(out, ", ")
}

// blobState is a blob's refcount and unreferenced_since.
func (bw *blobWorld) blobState(h string) (int64, sql.NullInt64) {
	bw.t.Helper()
	var b dbq.Blob
	if err := bw.db.Read(bw.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		b, err = q.GetBlob(bw.t.Context(), h)
		return err
	}); err != nil {
		bw.t.Fatal(err)
	}
	return b.Refcount, b.UnreferencedSince
}

// storedAttachments is the drafts.attachments column of who's every row,
// tombstones included, in id order.
func (bw *blobWorld) storedAttachments(who string) []string {
	bw.t.Helper()
	var out []string
	if err := bw.db.Read(bw.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(bw.t.Context(), `SELECT deleted, attachments FROM drafts WHERE user_id = ? ORDER BY id`, bw.users[who])
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var deleted int
			var a string
			if err := rows.Scan(&deleted, &a); err != nil {
				return err
			}
			out = append(out, fmt.Sprintf("%d %s", deleted, a))
		}
		return rows.Err()
	}); err != nil {
		bw.t.Fatal(err)
	}
	return out
}

// TestDraftKeepsAttachments is AC1: a reply draft and a new-thread draft
// each save their attachments and come back, from a GET as from the PUT,
// in the order sent and with the names and sizes the viewer uploaded them
// under. A save without attachments clears them.
func TestDraftKeepsAttachments(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	png := bw.uploaded("carol", "plan.png", blobstest.PNG(3, 2, 40))
	pdf := bw.uploaded("carol", "minutes.pdf", blobstest.PDF(41))
	want := fmt.Sprintf("minutes.pdf %d, plan.png %d", pdf.Size, png.Size)

	saved := decodeOK[dto.Draft](t, bw.putJSON("carol", draftPath(id),
		map[string]any{"source": "see attached", "reply_to": 1, "attachments": []string{pdf.Hash, png.Hash}}))
	got := decodeOK[dto.Draft](t, bw.raw("carol", http.MethodGet, draftPath(id), "", nil, 0))
	for name, d := range map[string]dto.Draft{"PUT": saved, "GET": got} {
		if names(d.Attachments) != want {
			t.Errorf("reply draft, %s: %q, want %q", name, names(d.Attachments), want)
		}
		if a := d.Attachments[1]; a.MIME != "image/png" || a.Width == nil || *a.Width != 3 || a.Hash != png.Hash {
			t.Errorf("reply draft, %s: the image is %+v", name, a)
		}
	}

	path := forumDraftPath("general")
	saved2 := decodeOK[dto.ForumDraft](t, bw.putJSON("carol", path,
		map[string]any{"title": "Plans", "source": "", "attachments": []string{png.Hash}}))
	got2 := decodeOK[dto.ForumDraft](t, bw.raw("carol", http.MethodGet, path, "", nil, 0))
	for name, d := range map[string]dto.ForumDraft{"PUT": saved2, "GET": got2} {
		if want := fmt.Sprintf("plan.png %d", png.Size); names(d.Attachments) != want {
			t.Errorf("new-thread draft, %s: %q, want %q", name, names(d.Attachments), want)
		}
	}

	// A save without the field, or with null, replaces the list with none.
	for _, body := range []map[string]any{{"source": "no files"}, {"source": "no files", "attachments": nil}} {
		decodeOK[dto.Draft](t, bw.putJSON("carol", draftPath(id), map[string]any{"source": "x", "attachments": []string{pdf.Hash}}))
		d := decodeOK[dto.Draft](t, bw.putJSON("carol", draftPath(id), body))
		if d.Attachments == nil || len(d.Attachments) != 0 {
			t.Errorf("%v: attachments %#v, want []", body, d.Attachments)
		}
	}
	if rec := bw.raw("carol", http.MethodGet, draftPath(id), "", nil, 0); !strings.Contains(rec.Body.String(), `"attachments":[]`) {
		t.Errorf("an emptied draft does not say attachments: []: %s", rec.Body)
	}
}

// TestDraftAttachmentChecks is AC2: a draft's attachments are held to a
// post's attach rules on both routes. Another member's blob, a quarantined
// one, an unknown hash, eleven, and a viewer without upload are each
// refused, and the stored draft is left as it was. A hash sent twice is
// kept once, as a post keeps it (run decision 2026-10-01).
func TestDraftAttachmentChecks(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	alices := bw.uploaded("alice", "a.png", blobstest.PNG(2, 2, 42))
	q := bw.uploaded("carol", "q.png", blobstest.PNG(2, 2, 43))
	bw.quarantine(q.Hash)
	var mine []string
	for i := range 11 {
		mine = append(mine, bw.uploaded("carol", fmt.Sprintf("%d.txt", i), blobstest.Text(byte(100+i))).Hash)
	}
	routes := map[string]func(hashes ...string) *httptest.ResponseRecorder{
		"reply draft": func(hashes ...string) *httptest.ResponseRecorder {
			return bw.putJSON("carol", draftPath(id), map[string]any{"source": "s", "attachments": hashes})
		},
		"new-thread draft": func(hashes ...string) *httptest.ResponseRecorder {
			return bw.putJSON("carol", forumDraftPath("general"), map[string]any{"source": "s", "attachments": hashes})
		},
	}
	for route, put := range routes {
		if rec := put(mine[0]); rec.Code != http.StatusOK {
			t.Fatalf("%s: the first save: %d %s", route, rec.Code, rec.Body)
		}
	}
	before := bw.storedAttachments("carol")
	for route, put := range routes {
		for name, c := range map[string]struct {
			hashes []string
			want   string
		}{
			"another member's blob": {[]string{alices.Hash}, "validation-failed: attachments not_yours"},
			"a quarantined blob":    {[]string{q.Hash}, "validation-failed: attachments blocked"},
			"an unknown hash":       {[]string{strings.Repeat("e", 64)}, "validation-failed: attachments unknown"},
			"not a hash":            {[]string{"../../etc/passwd"}, "validation-failed: attachments unknown"},
			"eleven":                {mine, "validation-failed: attachments too_many"},
		} {
			if got := fieldErrors(t, put(c.hashes...)); got != c.want {
				t.Errorf("%s, %s: %s, want %s", route, name, got, c.want)
			}
		}
	}
	if after := bw.storedAttachments("carol"); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("a refused save changed the drafts: %v then %v", before, after)
	}

	// Ten distinct with repeats are the ten, each once, in the order first
	// sent, as for a post.
	for route, put := range routes {
		ten := append(append([]string{}, mine[:10]...), mine[0], mine[3])
		rec := put(ten...)
		var hashes []string
		var body struct{ Attachments []dto.Attachment }
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil {
			t.Fatalf("%s, ten with repeats: %d %s", route, rec.Code, rec.Body)
		}
		for _, a := range body.Attachments {
			hashes = append(hashes, a.Hash)
		}
		if fmt.Sprint(hashes) != fmt.Sprint(mine[:10]) {
			t.Errorf("%s, ten with repeats: %v, want %v", route, hashes, mine[:10])
		}
	}

	// Without upload, any attachment is forbidden, and none is still fine.
	bw.revokeUpload(bw.forum)
	for route, put := range routes {
		if rec := put(mine[0]); problemCode(t, rec) != "forbidden" {
			t.Errorf("%s without upload: %d %s", route, rec.Code, rec.Body)
		}
		if rec := put(); rec.Code != http.StatusOK {
			t.Errorf("%s without upload or attachments: %d %s", route, rec.Code, rec.Body)
		}
	}
}

// TestDraftAttachmentAddsNoRef is AC3's first half: saving a draft, and
// saving it again later, leaves the blob's refcount at 0 and its
// unreferenced_since where the upload set it, so the draft does not pass
// for a post.
func TestDraftAttachmentAddsNoRef(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	a := bw.uploaded("carol", "a.txt", blobstest.Text(44))
	n0, since0 := bw.blobState(a.Hash)
	if n0 != 0 || !since0.Valid {
		t.Fatalf("after upload: refcount %d, unreferenced_since %v", n0, since0)
	}
	for i := range 2 {
		bw.clock += 60_000
		if rec := bw.putJSON("carol", draftPath(id), map[string]any{"source": "s", "attachments": []string{a.Hash}}); rec.Code != http.StatusOK {
			t.Fatalf("save %d: %d %s", i, rec.Code, rec.Body)
		}
		if rec := bw.putJSON("carol", forumDraftPath("general"), map[string]any{"source": "s", "attachments": []string{a.Hash}}); rec.Code != http.StatusOK {
			t.Fatalf("forum save %d: %d %s", i, rec.Code, rec.Body)
		}
		if n, since := bw.blobState(a.Hash); n != 0 || since != since0 {
			t.Fatalf("after save %d: refcount %d, unreferenced_since %v; want 0 and %v", i, n, since, since0)
		}
	}
}

// TestDraftTombstoneHasNoAttachments is AC3's second half: deleting a draft
// of either kind empties its attachments with its text, a GET then answers
// 204, and a draft started again on the tombstone carries only what that
// save sends. The triggers that refuse a tombstone holding attachments are
// tested in internal/store/db.
func TestDraftTombstoneHasNoAttachments(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	a := bw.uploaded("carol", "a.txt", blobstest.Text(45))
	for _, path := range []string{draftPath(id), forumDraftPath("general")} {
		if rec := bw.putJSON("carol", path, map[string]any{"source": "s", "attachments": []string{a.Hash}}); rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		if rec := bw.raw("carol", http.MethodDelete, path, "", nil, 0); rec.Code != http.StatusNoContent {
			t.Fatalf("%s: delete %d %s", path, rec.Code, rec.Body)
		}
		if rec := bw.raw("carol", http.MethodGet, path, "", nil, 0); rec.Code != http.StatusNoContent {
			t.Errorf("%s: a tombstone reads %d %s", path, rec.Code, rec.Body)
		}
	}
	if got := fmt.Sprint(bw.storedAttachments("carol")); got != "[1 [] 1 []]" {
		t.Fatalf("the tombstones hold %s, want two with []", got)
	}
	d := decodeOK[dto.Draft](t, bw.putJSON("carol", draftPath(id), map[string]any{"source": "again"}))
	if len(d.Attachments) != 0 {
		t.Errorf("a draft started again on a tombstone has %v", d.Attachments)
	}
}

// TestDraftCollectedBlobRefused is Terva review 1749's first finding: a
// blob the collector deletes after the attach check and before the draft's
// write refuses the save, on both routes, and leaves the draft as it was.
// Otherwise the draft would keep a hash with no bytes, which no reference
// and no later pass would ever notice. The recheck runs in the draft's own
// write transaction, which is ordered against the collector's delete.
func TestDraftCollectedBlobRefused(t *testing.T) {
	bw := newBlobWorld(t)
	bw.grantUpload(bw.forum)
	id, _ := bw.seed()
	kept := bw.uploaded("carol", "kept.txt", blobstest.Text(46))
	for _, path := range []string{draftPath(id), forumDraftPath("general")} {
		if rec := bw.putJSON("carol", path, map[string]any{"source": "s", "attachments": []string{kept.Hash}}); rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
	before := bw.storedAttachments("carol")
	bw.svc.Attachments = collecting{Service: bw.blobs, bw: bw}
	for i, path := range []string{draftPath(id), forumDraftPath("general")} {
		gone := bw.uploaded("carol", fmt.Sprintf("gone-%d.txt", i), blobstest.Text(byte(47+i)))
		rec := bw.putJSON("carol", path, map[string]any{"source": "s", "attachments": []string{gone.Hash}})
		if got := fieldErrors(t, rec); got != "validation-failed: attachments unknown" {
			t.Errorf("%s: %d %s", path, rec.Code, got)
		}
	}
	if after := bw.storedAttachments("carol"); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("a refused save changed the drafts: %v then %v", before, after)
	}
}
