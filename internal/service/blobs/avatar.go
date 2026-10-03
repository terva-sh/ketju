package blobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/blob"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Avatars (docs/05 "Display name and bio", decided by the maintainer
// 2026-10-02; TKT-01M3YSQ4CR).
//
// A member sets their own avatar from a PNG or JPEG of at most
// blob.AvatarMaxBytes. The server re-encodes it to a blob.AvatarEdge square
// and stores only that, as a blob no post names: it has no blob_uploads
// row, so it is never the member's to attach, and the upload bound does not
// count it while an account's avatar_blob names it. GET /avatars/{hash}
// serves it to signed-in members only (AuthorizeAvatar), an exception to
// the attachment rule, which Authorize keeps for /blobs/.

// CodeImageTooLarge refuses an image whose header declares more than the
// decoder's bounds (blob.MaxSide, blob.MaxDecodeBytes).
const CodeImageTooLarge = "image_too_large"

// SetAvatar makes the image r carries v's avatar and answers its hash. The
// file is read to blob.AvatarMaxBytes, and more is payload-too-large. A
// type other than PNG or JPEG is validation-failed {file,
// type_not_allowed}, a header over the decoder's bounds {file,
// image_too_large}, and bytes identical to a quarantined blob {file,
// blocked}. New bytes are {file, upload_budget} when the avatar they
// replace, which then counts as unattached, would take v over Budget.
//
// The image is decoded under the thumbnails' semaphore, so one decode runs
// in the process at a time. The blob row, the account's avatar_blob and the
// file's move into place are one database write, as an upload's are.
func (s *Service) SetAvatar(ctx context.Context, v domain.Viewer, r io.Reader) (string, error) {
	if err := member.Require(v, "set an avatar"); err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(r, blob.AvatarMaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("blobs: reading an avatar: %w", err)
	}
	if len(data) > blob.AvatarMaxBytes {
		return "", problem.Coded(problem.CodePayloadTooLarge, fmt.Errorf("blobs: an avatar is over %d bytes", blob.AvatarMaxBytes))
	}
	mime := http.DetectContentType(data)
	if !blob.AvatarType(mime) {
		return "", invalid("file", CodeTypeNotAllowed)
	}
	out, err := s.encodeAvatar(ctx, data, mime)
	switch {
	case errors.Is(err, blob.ErrTooLarge):
		return "", invalid("file", CodeImageTooLarge)
	case errors.Is(err, blob.ErrDecode), errors.Is(err, blob.ErrNotImage):
		return "", invalid("file", CodeTypeNotAllowed)
	case err != nil:
		return "", err
	}
	sum := sha256.Sum256(out)
	hash := hex.EncodeToString(sum[:])

	f, err := blob.Stage(s.Root)
	if err != nil {
		return "", err
	}
	staged := f.Name()
	committed := false
	defer func() {
		f.Close()
		if !committed {
			if rerr := os.Remove(staged); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
				s.log().Error("blobs: removing a staged avatar", "path", staged, "err", rerr)
			}
		}
		blob.Unstage(staged)
	}()
	if _, err := f.Write(out); err != nil {
		return "", fmt.Errorf("blobs: writing an avatar: %w", err)
	}
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("blobs: syncing an avatar: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("blobs: closing an avatar: %w", err)
	}

	now := s.now().UnixMilli()
	owner := sql.NullInt64{Int64: v.UserID, Valid: true}
	var placed bool
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		b, err := q.GetBlob(ctx, hash)
		fresh := errors.Is(err, sql.ErrNoRows)
		switch {
		case fresh:
			if err := q.CreateAvatarBlob(ctx, dbq.CreateAvatarBlobParams{
				Hash: hash, Size: int64(len(out)), Mime: mime, UploadedBy: owner, UploadedAt: now,
				Edge: sql.NullInt64{Int64: blob.AvatarEdge, Valid: true},
			}); err != nil {
				return err
			}
		case err != nil:
			return err
		case b.Status != "ok":
			return invalid("file", CodeBlocked)
		}
		if _, err := q.SetUserAvatar(ctx, dbq.SetUserAvatarParams{
			Hash: sql.NullString{String: hash, Valid: true}, UpdatedAt: now, ID: v.UserID,
		}); err != nil {
			return err
		}
		// The new avatar is named, so it does not count; the avatar it
		// replaced does, if that was v's and nothing else names it, and so
		// does every avatar v cleared earlier. New bytes are refused
		// whenever v's unattached total is then over the bound, whether
		// this change raised it or v was over already (Terva review 2063:
		// clearing before each upload kept the total level, and a check on
		// the rise alone let that repeat without end). So replacing or
		// clearing and setting again and again is bounded as uploading is.
		// Stored bytes are never refused, as for an upload.
		if fresh {
			after, err := q.UnattachedBytes(ctx, owner)
			if err != nil {
				return err
			}
			if after > s.budget() {
				return invalid("file", CodeUploadBudget)
			}
		}
		placed, err = blob.Commit(staged, s.Root, hash)
		if err != nil {
			return err
		}
		committed = true
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", problem.Coded(problem.CodeUnauthenticated, fmt.Errorf("blobs: no account %d to set an avatar on", v.UserID))
	}
	if err != nil {
		if placed {
			s.removeOrphan(hash)
		}
		return "", err
	}
	return hash, nil
}

// encodeAvatar is blob.Avatar, waiting its turn for the one decode the
// process runs at a time (thumb.go).
func (s *Service) encodeAvatar(ctx context.Context, data []byte, mime string) ([]byte, error) {
	st := s.thumbState()
	select {
	case st.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-st.sem }()
	return blob.Avatar(bytes.NewReader(data), mime)
}

// ClearAvatar removes v's own avatar. Its blob is then named by nothing
// and the collector takes it after seven days; until then it counts
// against v's bound, and stops being served at once. Clearing is never
// refused for the bound.
func (s *Service) ClearAvatar(ctx context.Context, v domain.Viewer) error {
	if err := member.Require(v, "remove an avatar"); err != nil {
		return err
	}
	err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		_, err := q.SetUserAvatar(ctx, dbq.SetUserAvatarParams{UpdatedAt: s.now().UnixMilli(), ID: v.UserID})
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return problem.Coded(problem.CodeUnauthenticated, fmt.Errorf("blobs: no account %d to clear an avatar on", v.UserID))
	}
	return err
}

// AuthorizeAvatar answers the blob for hash if v may fetch it as an avatar
// now, and ErrNotFound otherwise. v must be a signed-in, active member:
// an anonymous reader, a pre-user and a suspended account are refused
// (docs/05). The blob must be the current avatar of an account that is not
// anonymized, so a replaced or cleared avatar is refused to everyone, and
// an attachment is never served here.
func (s *Service) AuthorizeAvatar(ctx context.Context, v domain.Viewer, hash string) (dbq.Blob, error) {
	var b dbq.Blob
	if !v.Member() || !blob.ValidHash(hash) {
		return b, ErrNotFound
	}
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		b, err = q.AvatarForServing(ctx, hash)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return dbq.Blob{}, ErrNotFound
	}
	if err != nil {
		return dbq.Blob{}, err
	}
	return b, nil
}
