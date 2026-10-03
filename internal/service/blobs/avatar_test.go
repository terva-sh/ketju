package blobs_test

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"slices"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/blobs"
	"github.com/terva-sh/ketju/internal/service/blobs/blobstest"
	"github.com/terva-sh/ketju/internal/store/blob"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

func (s *store) setAvatar(who string, b []byte) (string, error) {
	s.t.Helper()
	return s.svc.SetAvatar(s.t.Context(), s.viewer(who), bytes.NewReader(b))
}

func (s *store) avatarOf(who string) string {
	s.t.Helper()
	var h string
	s.read(func(q *dbq.Queries) error {
		u, err := q.GetUser(s.t.Context(), s.users[who])
		h = u.AvatarBlob.String
		return err
	})
	return h
}

func (s *store) unattached(who string) int64 {
	s.t.Helper()
	var n int64
	s.read(func(q *dbq.Queries) error {
		var err error
		n, err = q.UnattachedBytes(s.t.Context(), nullID(s.users[who]))
		return err
	})
	return n
}

// TestSetAvatar: an avatar is stored re-encoded, as a 256 px square blob
// no post names, with no upload row, so its owner cannot attach it; the
// account names it; and nothing is left staged.
func TestSetAvatar(t *testing.T) {
	s := newStore(t)
	h, err := s.setAvatar("alice", blobstest.PNG(600, 400, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.avatarOf("alice"); got != h {
		t.Fatalf("alice's avatar is %q, want %q", got, h)
	}
	if n := s.count(`SELECT count(*) FROM blobs WHERE hash = ? AND mime = 'image/png' AND width = 256 AND height = 256
		AND refcount = 0 AND uploaded_by = ?`, h, s.users["alice"]); n != 1 {
		t.Fatal("the blob row is not a 256 px PNG of alice's")
	}
	if n := s.count(`SELECT count(*) FROM blob_uploads WHERE hash = ?`, h); n != 0 {
		t.Fatalf("%d upload rows: an avatar would be attachable", n)
	}
	if files := s.files(); !slices.Equal(files, []string{h}) || len(s.staged()) != 0 {
		t.Fatalf("files %v, staged %v", files, s.staged())
	}
	path, err := blob.Path(s.root, h)
	if err != nil {
		t.Fatal(err)
	}
	img := decodePNGFile(t, path)
	if b := img.Bounds(); b.Dx() != blob.AvatarEdge || b.Dy() != blob.AvatarEdge {
		t.Fatalf("the stored avatar is %v", b)
	}
	if _, err := s.svc.Check(t.Context(), s.viewer("alice"), forumID(s), []string{h}); fieldCode(err) != "attachments not_yours" {
		t.Fatalf("attaching her avatar: %v", err)
	}

	if err := s.svc.ClearAvatar(t.Context(), s.viewer("alice")); err != nil {
		t.Fatal(err)
	}
	if got := s.avatarOf("alice"); got != "" {
		t.Fatalf("a cleared avatar is %q", got)
	}
}

// TestSetAvatarRefusals: a GIF, text, an image too large to decode, a file
// over 1 MB and bytes identical to a quarantined avatar are refused, the
// account keeps the avatar it had, and nothing is left staged.
func TestSetAvatarRefusals(t *testing.T) {
	s := newStore(t)
	had, err := s.setAvatar("alice", blobstest.JPEG(50, 50, 2))
	if err != nil {
		t.Fatal(err)
	}
	quarantined := blobstest.PNG(70, 70, 3)
	qh, err := s.setAvatar("carol", quarantined)
	if err != nil {
		t.Fatal(err)
	}
	s.write(func(q *dbq.Queries) error {
		_, err := q.SetBlobStatus(t.Context(), dbq.SetBlobStatusParams{Status: "quarantined", Hash: qh})
		return err
	})
	big := append(blobstest.PNG(4, 4, 4), make([]byte, blob.AvatarMaxBytes)...)
	for name, c := range map[string]struct {
		data []byte
		want func(error) bool
	}{
		"a GIF":        {blobstest.GIF(9, 9, 5), func(err error) bool { return fieldCode(err) == "file type_not_allowed" }},
		"text":         {blobstest.Text(6), func(err error) bool { return fieldCode(err) == "file type_not_allowed" }},
		"a PNG bomb":   {pngBomb(), func(err error) bool { return fieldCode(err) == "file image_too_large" }},
		"over 1 MB":    {big, func(err error) bool { return problem.FromError(err) == problem.CodePayloadTooLarge }},
		"quarantined":  {quarantined, func(err error) bool { return fieldCode(err) == "file blocked" }},
		"not an image": {[]byte("\x89PNG\r\n\x1a\nnot really"), func(err error) bool { return fieldCode(err) == "file type_not_allowed" }},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := s.setAvatar("alice", c.data); !c.want(err) {
				t.Fatalf("err = %v", err)
			}
			if got := s.avatarOf("alice"); got != had {
				t.Fatalf("alice's avatar changed to %q", got)
			}
			if staged := s.staged(); len(staged) != 0 {
				t.Fatalf("staged files left: %v", staged)
			}
		})
	}
	// Exactly the cap is read; it is refused for what it is, not its size.
	exact := append(blobstest.PNG(4, 4, 7), make([]byte, 0)...)
	exact = append(exact, make([]byte, blob.AvatarMaxBytes-len(exact))...)
	if _, err := s.setAvatar("alice", exact); problem.FromError(err) == problem.CodePayloadTooLarge {
		t.Fatalf("a file of exactly the cap was too large: %v", err)
	}
}

// TestAvatarServedOnlyToMembers is the maintainer's rule (docs/05, an
// exception to docs/07 "Attachments"), and security: a blob named only as
// an avatar is served at /avatars/ to signed-in members, its owner and
// staff among them, and to nobody signed out, a pre-user, or a suspended
// account. A replaced or cleared avatar is served to no one, an attachment
// is never served as an avatar, and /blobs/ never serves an avatar no post
// names.
func TestAvatarServedOnlyToMembers(t *testing.T) {
	s := newStore(t)
	h, err := s.setAvatar("alice", blobstest.PNG(40, 40, 8))
	if err != nil {
		t.Fatal(err)
	}
	moderator := s.viewer("carol")
	moderator.Role = domain.RoleModerator
	suspended := s.viewer("carol")
	suspended.Active = false
	for name, v := range map[string]domain.Viewer{
		"the owner": s.viewer("alice"), "another member": s.viewer("carol"), "a moderator": moderator,
	} {
		if _, err := s.svc.AuthorizeAvatar(t.Context(), v, h); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, v := range map[string]domain.Viewer{
		"anonymous":   domain.Anonymous(),
		"a pre-user":  {PreUser: true, IdentityID: 7, Active: true},
		"a suspended": suspended,
	} {
		if _, err := s.svc.AuthorizeAvatar(t.Context(), v, h); !errors.Is(err, blobs.ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, who := range []string{"alice", "carol"} {
		if _, err := s.svc.Authorize(t.Context(), s.viewer(who), h); !errors.Is(err, blobs.ErrNotFound) {
			t.Errorf("/blobs/ served the avatar to %s: %v", who, err)
		}
	}
	for _, bad := range []string{"", "x", "../" + h, h[:63], h + "0"} {
		if _, err := s.svc.AuthorizeAvatar(t.Context(), s.viewer("carol"), bad); !errors.Is(err, blobs.ErrNotFound) {
			t.Errorf("%q: %v", bad, err)
		}
	}

	// An attachment, posted or not, is not an avatar.
	att, err := s.upload("carol", "a.png", blobstest.PNG(3, 3, 9))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.svc.AddRefs(t.Context(), []string{att}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.svc.AuthorizeAvatar(t.Context(), s.viewer("carol"), att); !errors.Is(err, blobs.ErrNotFound) {
		t.Errorf("an attachment was served as an avatar: %v", err)
	}

	// A replaced avatar is gone at once, whoever kept its hash.
	if _, err := s.setAvatar("alice", blobstest.PNG(40, 40, 10)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.svc.AuthorizeAvatar(t.Context(), s.viewer("carol"), h); !errors.Is(err, blobs.ErrNotFound) {
		t.Errorf("a replaced avatar is served: %v", err)
	}
	now := s.avatarOf("alice")
	if err := s.svc.ClearAvatar(t.Context(), s.viewer("alice")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.svc.AuthorizeAvatar(t.Context(), s.viewer("alice"), now); !errors.Is(err, blobs.ErrNotFound) {
		t.Errorf("a cleared avatar is served: %v", err)
	}
}

// TestAvatarAndTheUploadBound: the avatar an account names does not count
// against limits.unattached_upload_mb; one it replaced does; new bytes are
// refused when that would take the account over; bytes already stored, and
// a clear, never are.
func TestAvatarAndTheUploadBound(t *testing.T) {
	s := newStore(t)
	s.svc.Budget = 1
	first := blobstest.PNG(30, 30, 11)
	h, err := s.setAvatar("alice", first)
	if err != nil {
		t.Fatalf("an avatar at an empty bound: %v", err)
	}
	if n := s.unattached("alice"); n != 0 {
		t.Fatalf("the avatar counts %d bytes", n)
	}
	if _, err := s.setAvatar("alice", blobstest.PNG(30, 30, 12)); fieldCode(err) != "file upload_budget" {
		t.Fatalf("a new avatar that leaves the old one over the bound: %v", err)
	}
	if got := s.avatarOf("alice"); got != h || len(s.staged()) != 0 {
		t.Fatalf("the refusal changed the avatar to %q or left %v staged", got, s.staged())
	}
	if err := s.svc.ClearAvatar(t.Context(), s.viewer("alice")); err != nil {
		t.Fatalf("a clear over the bound: %v", err)
	}
	if n := s.unattached("alice"); n == 0 {
		t.Fatal("a cleared avatar does not count")
	}
	if got, err := s.setAvatar("alice", first); err != nil || got != h {
		t.Fatalf("setting stored bytes again over the bound: %q %v", got, err)
	}

	// Under a bound with room, replacing passes and the old one counts.
	s.svc.Budget = 1 << 20
	if _, err := s.setAvatar("alice", blobstest.PNG(30, 30, 13)); err != nil {
		t.Fatal(err)
	}
	var size int64
	s.read(func(q *dbq.Queries) error {
		b, err := q.GetBlob(t.Context(), h)
		size = b.Size
		return err
	})
	if n := s.unattached("alice"); n != size {
		t.Fatalf("unattached = %d, want the replaced avatar's %d", n, size)
	}
}

// TestUnattachedBytesUsesItsIndex: the bound's sum reads one account's
// unattached blobs through blobs_uploader_unreferenced, and asks whether an
// avatar names each through users_avatar_blob, never scanning either table.
func TestUnattachedBytesUsesItsIndex(t *testing.T) {
	s := newStore(t)
	plan := s.plan(`SELECT CAST(coalesce(sum(b.size), 0) AS INTEGER) FROM blobs b
WHERE b.uploaded_by = ? AND b.refcount = 0
  AND NOT EXISTS (SELECT 1 FROM users u WHERE u.avatar_blob = b.hash)`, s.users["alice"])
	wantPlan(t, plan, "SEARCH b USING INDEX blobs_uploader_unreferenced (uploaded_by=?)",
		"SEARCH u USING COVERING INDEX users_avatar_blob (avatar_blob=?)")
}

// TestAvatarForServingUsesItsIndex: the serving check seeks the blob by
// its key and the account by users_avatar_blob.
func TestAvatarForServingUsesItsIndex(t *testing.T) {
	s := newStore(t)
	plan := s.plan(`SELECT b.* FROM blobs b
WHERE b.hash = ? AND b.status = 'ok'
  AND EXISTS (SELECT 1 FROM users u WHERE u.avatar_blob = ? AND u.anonymized = 0)`, "x", "x")
	wantPlan(t, plan, "SEARCH b USING PRIMARY KEY (hash=?)",
		"SEARCH u EXISTS USING INDEX users_avatar_blob (avatar_blob=?)")
}

func decodePNGFile(t *testing.T, path string) image.Image {
	t.Helper()
	b := readFile(t, path)
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}
