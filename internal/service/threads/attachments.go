package threads

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// What the blob store asks of threads (internal/service/blobs,
// TKT-01M29CXK0K, docs/07 "Attachments"), and how a post's attachments are
// drawn.

// ForumOf admits v to thread id as every thread endpoint does (access) and
// answers its forum: the id, and whether it is archived, read from the row
// on this request. An upload aimed at a thread is checked against this
// forum.
func (s *Service) ForumOf(ctx context.Context, v domain.Viewer, id threadid.ID) (forumID int64, archived bool, err error) {
	a, err := s.access(ctx, v, id)
	if err != nil {
		return 0, false, err
	}
	return a.row.ForumID, a.forumArchived, nil
}

// ForumBySlug admits v to the forum slug as the thread list does (readForum)
// and answers it the way ForumOf does.
func (s *Service) ForumBySlug(ctx context.Context, v domain.Viewer, slug string) (forumID int64, archived bool, err error) {
	f, err := s.readForum(ctx, v, slug)
	if err != nil {
		return 0, false, err
	}
	return f.ID, f.Archived != 0, nil
}

// CanSeePostBody reports whether v may read the body of post ordinal of
// thread id now, by the thread page's rules: v is admitted to the thread
// (access, so a hidden or unreadable forum, a thread with no projection row
// and a quarantined thread all say no), and the post is visible, or is a
// removed post revealRemoved opens to v. A moved post has no body here, and
// a redacted one has none for anybody.
//
// The serve path asks it for the posts naming a blob, so a blob and its
// post never disagree (docs/14 "Invariants"). A refusal is false, not an
// error; an error is a failure to decide, which the caller must not read as
// yes.
func (s *Service) CanSeePostBody(ctx context.Context, v domain.Viewer, id threadid.ID, ordinal uint32) (bool, error) {
	return s.CanSeeAnyPostBody(ctx, v, id, []uint32{ordinal})
}

// CanSeeAnyPostBody is CanSeePostBody for several posts of one thread,
// true when v may read any of them. v is admitted to the thread once, and
// the entries are read from one snapshot of the index.
func (s *Service) CanSeeAnyPostBody(ctx context.Context, v domain.Viewer, id threadid.ID, ordinals []uint32) (bool, error) {
	if len(ordinals) == 0 {
		return false, nil
	}
	a, err := s.access(ctx, v, id)
	if err != nil {
		switch problem.FromError(err) {
		case problem.CodeNotFound, problem.CodeUnauthenticated, problem.CodeForbidden, problem.CodeUnavailable:
			return false, nil
		}
		return false, err
	}
	var entries []threadlog.IndexEntry
	err = s.Threads.With(id, func(th *threadcache.Thread) error {
		var eerr error
		th.Actor.Stable(func(int64) {
			n := th.Index.Len()
			for _, o := range ordinals {
				if o == 0 || int(o) > n {
					continue
				}
				e, err := th.Index.Entry(o)
				if err != nil {
					eerr = err
					return
				}
				entries = append(entries, e)
			}
		})
		return eerr
	})
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		switch {
		case e.Flags.Has(threadlog.EntryMoved):
		case e.Flags.Visible(), revealRemoved(a, e):
			return true, nil
		}
	}
	return false, nil
}

// canUpload is the thread page's viewer.can_upload: a member with upload in
// the thread's forum, which is not read-only to them (readOnlyForum, which
// reads the forum's row, so an archive shows at once whatever perm's cache
// holds).
func (s *Service) canUpload(ctx context.Context, a access) (bool, error) {
	if !a.viewer.Member() || readOnlyForum(a) != nil {
		return false, nil
	}
	return s.Perm.Can(ctx, a.viewer, a.row.ForumID, domain.PermUpload)
}

// attachmentsOf answers each post's attachments, parallel to posts, as a
// thread page serves them (docs/07 "Attachments"):
//   - a tombstone carries none, whatever the post had;
//   - a post read from its Post record lists that record's hashes, in order;
//   - an edited post, whose current record is an Edit with no list, takes
//     the hashes from post_attachments, which only its Post and a Redacted
//     record set (docs/03).
//
// Each attachment carries the blob's stored size, type and dimensions, and
// the name the post's author uploaded it under. None of those changes once
// written (blob_uploads is insert-once), so the response cache may keep
// them with the posts. A hash with no blob row, which a hand-made log can
// hold, is still listed, with its hash alone; an author with no upload of
// it gets a name made from the type.
func (s *Service) attachmentsOf(ctx context.Context, id threadid.ID, posts []threadlog.Post) ([][]dto.Attachment, error) {
	out := make([][]dto.Attachment, len(posts))
	hashes := make([][]string, len(posts))
	var fromProjection []int
	for i, p := range posts {
		switch {
		case p.Tombstone:
		case len(p.Attachments) > 0:
			hashes[i] = p.Attachments
		case p.Flags.Has(threadlog.EntryHasAttachments):
			fromProjection = append(fromProjection, i)
		}
	}
	byAuthor := map[uint64]map[string]bool{}
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		for _, i := range fromProjection {
			hs, err := q.PostAttachmentsOf(ctx, dbq.PostAttachmentsOfParams{ThreadID: int64(id), Ordinal: int64(posts[i].Ordinal)})
			if err != nil {
				return err
			}
			hashes[i] = hs
		}
		for i, hs := range hashes {
			if len(hs) == 0 {
				continue
			}
			set := byAuthor[posts[i].Author]
			if set == nil {
				set = map[string]bool{}
				byAuthor[posts[i].Author] = set
			}
			for _, h := range hs {
				set[h] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(byAuthor) == 0 {
		return out, nil
	}

	type key struct {
		author uint64
		hash   string
	}
	meta := map[key]dbq.AttachmentMetaRow{}
	err = s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		for author, set := range byAuthor {
			list := make([]string, 0, len(set))
			for h := range set {
				list = append(list, h)
			}
			rows, err := q.AttachmentMeta(ctx, dbq.AttachmentMetaParams{AuthorID: int64(author), Hashes: list})
			if err != nil {
				return fmt.Errorf("threads: attachment metadata: %w", err)
			}
			for _, r := range rows {
				meta[key{author, r.Hash}] = r
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for i, hs := range hashes {
		for _, h := range hs {
			m, ok := meta[key{posts[i].Author, h}]
			if !ok {
				out[i] = append(out[i], dto.Attachment{Hash: h, OriginalName: "attachment"})
				continue
			}
			a := dto.Attachment{Hash: h, Size: m.Size, MIME: m.Mime, OriginalName: m.OriginalName}
			if a.OriginalName == "" {
				a.OriginalName = "attachment" + extensionOf(m.Mime)
			}
			if m.Width.Valid && m.Height.Valid {
				w, h := int(m.Width.Int64), int(m.Height.Int64)
				a.Width, a.Height = &w, &h
			}
			out[i] = append(out[i], a)
		}
	}
	return out, nil
}

// extensionOf names a file of a stored type, for an attachment whose author
// has no upload row to name it.
func extensionOf(mime string) string {
	base, _, _ := strings.Cut(mime, ";")
	switch strings.TrimSpace(base) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	}
	return ""
}
