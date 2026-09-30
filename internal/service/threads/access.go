package threads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// access is what every thread endpoint knows once it has let a viewer in.
type access struct {
	viewer      domain.Viewer
	row         dbq.Thread
	class       string
	canModerate bool
	canReply    bool // the permission, before the lock is considered
	// forumArchived is the thread's forum's archived flag, read from its row
	// on this request, never from the decision cache (readOnlyForum).
	forumArchived bool
}

// access admits v to thread id or says why not, the same way for every thread
// endpoint (docs/07 "Error catalogue"): a thread with no projection row or in
// a forum hidden from v is not-found, as if it did not exist; a readable-when-
// signed-in thread is unauthenticated to an anonymous viewer and forbidden to
// a member without read. An inactive viewer is served as anonymous.
func (s *Service) access(ctx context.Context, v domain.Viewer, id threadid.ID) (access, error) {
	if !v.Member() {
		v = domain.Anonymous()
	}
	a := access{viewer: v, class: ClassAnon}
	err := s.DB.Read(ctx, func(dq *dbq.Queries, tx *sql.Tx) error {
		var err error
		if a.row, err = dq.GetThread(ctx, int64(id)); err != nil {
			return err
		}
		forum, err := dq.GetForum(ctx, a.row.ForumID)
		if err != nil {
			return fmt.Errorf("threads: the forum of %s: %w", id, err)
		}
		a.forumArchived = forum.Archived != 0
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		// Not projected is not found. A thread whose ThreadCreated has not
		// reached the projection yet is milliseconds old.
		return a, problem.Coded(problem.CodeNotFound, fmt.Errorf("threads: %s has no projection row", id))
	}
	if err != nil {
		return a, err
	}
	if a.row.PostCount == 0 {
		// ThreadCreated with no first post: a crash between the two
		// appends, or a CreateThread still between them. Nothing to show
		// (settled in grooming for TKT-01M29CA9B9).
		return a, problem.Coded(problem.CodeNotFound, fmt.Errorf("threads: %s has no posts", id))
	}

	switch d, err := s.Perm.Check(ctx, v, a.row.ForumID, domain.PermRead); {
	case err != nil:
		return a, err
	case d == perm.Hidden:
		return a, problem.Coded(problem.CodeNotFound, fmt.Errorf("threads: %s is hidden from this viewer", id))
	case d == perm.Deny && !v.Member():
		return a, problem.Coded(problem.CodeUnauthenticated, fmt.Errorf("threads: %s needs a signed-in reader", id))
	case d == perm.Deny:
		return a, problem.Coded(problem.CodeForbidden, fmt.Errorf("threads: %s is not readable by this viewer", id))
	}
	// After the read check, so a thread hidden from v is still not-found: a
	// 503 would tell a viewer who may not see it that it exists.
	if s.Quarantined != nil && s.Quarantined(id) {
		return a, problem.Coded(problem.CodeUnavailable,
			fmt.Errorf("threads: %s is quarantined: its log is shorter than its projection", id))
	}
	if a.canModerate, err = s.Perm.Can(ctx, v, a.row.ForumID, domain.PermModerate); err != nil {
		return a, err
	}
	if a.canReply, err = s.Perm.Can(ctx, v, a.row.ForumID, domain.PermReply); err != nil {
		return a, err
	}
	switch {
	case a.canModerate:
		a.class = ClassModerator
	case v.Member():
		a.class = ClassMember
	}
	return a, nil
}

// readOnlyForum refuses a post in an archived forum to everyone but an
// admin, moderators included (docs/04 "permissions", TKT-01M3N24RHD). It
// answers conflict, not forbidden, so the refusal says why, as an archived
// thread's does.
//
// perm denies create_thread, reply and upload in an archived forum too, but
// its decisions are cached for up to a minute, and a write by another
// process clears no cache here. This reads the flag access loaded from the
// forum's row on this request, so an archive refuses the next post whatever
// the cache holds: a cached reply Allow can only turn forbidden into
// conflict. It runs after access, so a forum hidden from the viewer is
// still not-found and an anonymous viewer still unauthenticated.
//
// It guards posting only: a reply, a new thread, and an author's edit,
// delete or restore of their own post. Moderation is not posting, and no
// moderation path calls it (maintainer decision 2026-09-29).
func readOnlyForum(a access) error {
	if !a.forumArchived || (a.viewer.Member() && a.viewer.Role == domain.RoleAdmin) {
		return nil
	}
	return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is in an archived forum", threadid.ID(a.row.ID)))
}
