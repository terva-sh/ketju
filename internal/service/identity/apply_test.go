package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// applying is registry with registration.mode set to approval.
func applying(t *testing.T) (*Service, *session.Store) {
	t.Helper()
	s, d := registry(t)
	if err := settings.Set(t.Context(), d, settings.RegistrationMode.Name, json.RawMessage(`"approval"`), 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	return s, &session.Store{DB: d}
}

func TestBeginApplicationWritesAnApplicant(t *testing.T) {
	s, store := applying(t)
	res, err := s.BeginApplication(t.Context(), claims("sub-1"), session.Meta{Provider: "fake"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Applying || res.Token == "" || res.Session.Kind != session.KindPreUser {
		t.Fatalf("BeginApplication = %+v", res)
	}
	sess, err := store.Lookup(t.Context(), res.Token)
	if err != nil || sess.Kind != session.KindPreUser || sess.User != nil {
		t.Fatalf("the token names %+v (%v), want a pre-user session", sess, err)
	}
	if u, i, n := count(t, s.DB); u != 0 || i != 1 || n != 1 {
		t.Errorf("%d users, %d identities, %d sessions; want 0, 1, 1", u, i, n)
	}
}

// The identity an application writes records the claims the IdP sent, which
// the application form prefills from (TKT-01M29CP2KZ), and the email.
func TestBeginApplicationRecordsTheClaims(t *testing.T) {
	s, _ := applying(t)
	c := claims("sub-1")
	c.RawClaims = json.RawMessage(`{"sub":"sub-1","name":"Someone sub-1","preferred_username":"some1"}`)
	if _, err := s.BeginApplication(t.Context(), c, session.Meta{Provider: "fake"}, ""); err != nil {
		t.Fatal(err)
	}
	var ident dbq.Identity
	if err := s.DB.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		ident, err = q.GetIdentityBySubject(t.Context(), dbq.GetIdentityBySubjectParams{ProviderKey: "fake", Subject: "sub-1"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if ident.RawClaimsJson.String != string(c.RawClaims) || ident.Email.String != "sub-1@example.org" || ident.EmailVerified != 1 {
		t.Errorf("the identity recorded claims %q, email %q (%d)", ident.RawClaimsJson.String, ident.Email.String, ident.EmailVerified)
	}
	// The claim set never rides in the registration cookie.
	if b, _ := json.Marshal(c); strings.Contains(string(b), "preferred_username") {
		t.Errorf("Claims marshals the raw claim set: %s", b)
	}
}

// BeginApplication refuses, and writes nothing, unless the mode is approval
// and the identity is new.
func TestBeginApplicationRefuses(t *testing.T) {
	t.Run("registration open", func(t *testing.T) {
		s, _ := registry(t)
		if _, err := s.BeginApplication(t.Context(), claims("sub-1"), session.Meta{}, ""); !errors.Is(err, ErrRegistrationClosed) {
			t.Fatalf("err = %v, want ErrRegistrationClosed", err)
		}
		if u, i, n := count(t, s.DB); u+i+n != 0 {
			t.Errorf("wrote %d users, %d identities, %d sessions", u, i, n)
		}
	})
	t.Run("the identity exists", func(t *testing.T) {
		s, _ := applying(t)
		if _, err := s.BeginApplication(t.Context(), claims("sub-1"), session.Meta{}, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := s.BeginApplication(t.Context(), claims("sub-1"), session.Meta{}, ""); !errors.Is(err, ErrAlreadyRegistered) {
			t.Fatalf("err = %v, want ErrAlreadyRegistered", err)
		}
		if u, i, n := count(t, s.DB); u != 0 || i != 1 || n != 1 {
			t.Errorf("%d users, %d identities, %d sessions; want only the first application's", u, i, n)
		}
	})
}

// opening is a session store on which an admin opens registration between
// BeginApplication's start and its transaction.
type opening struct {
	*session.Store
	t *testing.T
}

func (o opening) IssuePreUserWith(ctx context.Context, meta session.Meta, create func(context.Context, *dbq.Queries) (int64, error)) (string, session.Session, error) {
	if err := settings.Set(ctx, o.DB, settings.RegistrationMode.Name, json.RawMessage(`"open"`), 0, time.Now()); err != nil {
		o.t.Fatal(err)
	}
	return o.Store.IssuePreUserWith(ctx, meta, create)
}

// The mode is read in the transaction that writes the identity (review
// 1145): registration opened just before it is seen, and nothing is written.
func TestRegistrationOpenedJustBeforeTheApplication(t *testing.T) {
	s, store := applying(t)
	s.Sessions = opening{Store: store, t: t}
	if _, err := s.BeginApplication(t.Context(), claims("sub-1"), session.Meta{}, ""); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("err = %v, want ErrRegistrationClosed", err)
	}
	if u, i, n := count(t, s.DB); u+i+n != 0 {
		t.Errorf("wrote %d users, %d identities, %d sessions", u, i, n)
	}
}
