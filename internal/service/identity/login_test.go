package identity

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/terva-sh/ketju/internal/auth/oidc"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
)

// cancelling is a session store whose request is cancelled while the old
// session is being ended, as a browser that gives up mid-sign-in does.
type cancelling struct {
	cancel  context.CancelFunc
	revoked []string
}

func (c *cancelling) IssueUser(context.Context, int64, session.Meta) (string, session.Session, error) {
	return "new", session.Session{Kind: session.KindUser}, nil
}

func (c *cancelling) IssueUserWith(context.Context, session.Meta, func(context.Context, *dbq.Queries) (int64, error)) (string, session.Session, error) {
	return "", session.Session{}, errors.New("not used")
}

func (c *cancelling) IssuePreUser(context.Context, int64, session.Meta) (string, session.Session, error) {
	return "", session.Session{}, errors.New("not used")
}

func (c *cancelling) IssuePreUserWith(context.Context, session.Meta, func(context.Context, *dbq.Queries) (int64, error)) (string, session.Session, error) {
	return "", session.Session{}, errors.New("not used")
}

func (c *cancelling) ListForUser(context.Context, int64) ([]session.Info, error) {
	return nil, errors.New("not used")
}

func (c *cancelling) RevokeOwn(context.Context, int64, string) (bool, error) {
	return false, errors.New("not used")
}

func (c *cancelling) RevokeUser(context.Context, int64) error {
	return errors.New("not used")
}

func (c *cancelling) Revoke(ctx context.Context, token string) error {
	if token == "old" {
		c.cancel()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.revoked = append(c.revoked, token)
	return nil
}

// A sign-in cancelled while it replaces a session still ends the session it
// issued, and records nothing (review 513).
func TestACancelledSignInLeavesNoSessionBehind(t *testing.T) {
	d := dbtest.Open(t, db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "alice", DisplayName: "Alice",
			Status: "active", Role: "member", Kind: "human", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			return err
		}
		_, err = q.CreateIdentity(t.Context(), dbq.CreateIdentityParams{UserID: sql.NullInt64{Int64: u, Valid: true},
			ProviderKey: "fake", Subject: "user-1", Issuer: "https://idp.example", CreatedAt: 1, UpdatedAt: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	sessions := &cancelling{cancel: cancel}
	s := &Service{DB: d, Sessions: sessions}
	_, err := s.Login(ctx, &oidc.Identity{Provider: "fake", Subject: "user-1"}, domain.Viewer{}, 0, session.Meta{}, "old")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Login = %v, want the cancellation", err)
	}
	if len(sessions.revoked) != 1 || sessions.revoked[0] != "new" {
		t.Errorf("revoked %v, want the session the failed sign-in issued", sessions.revoked)
	}
	var last sql.NullInt64
	d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), "SELECT last_login_at FROM identities").Scan(&last)
	})
	if last.Valid {
		t.Error("a sign-in that did not happen was recorded")
	}
}
