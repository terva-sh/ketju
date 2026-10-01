package threads

import (
	"context"
	"database/sql"
	"errors"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// This file is what the live-update stream (internal/web/sse) asks of the
// thread service: the thread page's own admission, reduced to its verdict,
// and the two numbers a hint is built from (docs/02 "Live updates",
// TKT-01M29CRJ3X). A stream is held to no less than the page: it admits a
// viewer exactly as GET /threads/{id} would, and asks again before every
// write.

// ReadAccess admits v to thread id as the thread page does, and returns the
// forum the thread is in now. The refusals are the page's: not-found for a
// thread that does not exist, has no posts, or is in a forum hidden from v;
// unauthenticated or forbidden for one v may not read; unavailable for a
// quarantined thread. An inactive viewer is served as anonymous.
func (s *Service) ReadAccess(ctx context.Context, v domain.Viewer, id threadid.ID) (int64, error) {
	a, err := s.access(ctx, v, id)
	if err != nil {
		return 0, err
	}
	return a.row.ForumID, nil
}

// ForumAccess admits v to read the forum slug names, as the thread list
// does, and returns its id: not-found for a forum that does not exist or is
// hidden from v, unauthenticated to an anonymous viewer and forbidden to a
// member without read.
func (s *Service) ForumAccess(ctx context.Context, v domain.Viewer, slug string) (int64, error) {
	if !v.Member() {
		v = domain.Anonymous()
	}
	f, err := s.readForum(ctx, v, slug)
	if err != nil {
		return 0, err
	}
	return f.ID, nil
}

// LogLength is thread id's length as a reader's next page would see it:
// the open actor's acknowledged length when the thread is in the cache, and
// the projection's otherwise. A thread with no projection row is 0.
//
// It does not open a thread. Every stream opens with a snapshot of each
// thread it names, and a reconnect storm after a restart would otherwise
// open every one of them from disk at once.
func (s *Service) LogLength(ctx context.Context, id threadid.ID) (int64, error) {
	if th, ok := s.Threads.Peek(id); ok {
		defer s.Threads.Release(th)
		return th.Actor.LogLength(), nil
	}
	p, err := Projections{DB: s.DB}.Projection(ctx, id)
	return p.LogLength, err
}

// Projection is a thread's projection row as the live-update hub reads it:
// the forum the thread is in, and how far its log has been projected.
type Projection struct {
	// Found is false for a thread whose ThreadCreated has not been
	// projected.
	Found     bool
	ForumID   int64
	LogLength int64
}

// Projections reads projection rows for the live-update hub. It is apart
// from Service because the hub subscribes to the event bus before serve has
// built the rest of the service, and it needs nothing but the database.
type Projections struct {
	DB *db.DB
}

// Projection reads thread id's projection row. A thread with no row is not
// an error: it is Found false.
func (p Projections) Projection(ctx context.Context, id threadid.ID) (Projection, error) {
	var out Projection
	err := p.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		row, err := q.GetThread(ctx, int64(id))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out = Projection{Found: true, ForumID: row.ForumID, LogLength: row.LogLengthProjected}
		return nil
	})
	return out, err
}
