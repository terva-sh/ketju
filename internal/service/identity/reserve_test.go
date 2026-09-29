package identity

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// TestAnAnonymizedHandleIsReserved is AC9 (TKT-01M29CKRFS, Terva review
// 1448): anonymizing reserves the old handle for 90 days, in any case, with
// a release row that names no user, so nobody else can take it and the
// links an old mention of it would give them. Registration answers "taken".
// Once the 90 days are over the handle is free, and it goes to a new
// account, never the anonymized one.
func TestAnAnonymizedHandleIsReserved(t *testing.T) {
	s, d := registry(t)
	if _, err := s.Register(t.Context(), claims("sub-1"), "alice", session.Meta{Provider: "fake"}, ""); err != nil {
		t.Fatal(err)
	}
	old := accountOf(t, d, "sub-1")
	if err := s.Perm.Anonymize(t.Context(), old, audit.System("test"), nil); err != nil {
		t.Fatal(err)
	}

	var user sql.NullInt64
	var released, until int64
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT user_id, released_at, reserved_until FROM handle_releases
			WHERE handle = 'alice'`).Scan(&user, &released, &until)
	}); err != nil {
		t.Fatal(err)
	}
	if user.Valid || until-released != domain.HandleReservation.Milliseconds() {
		t.Errorf("the release names user %v and reserves for %v", user, time.Duration(until-released)*time.Millisecond)
	}

	// The shared check every allocation path asks (Terva review 1450).
	for at, want := range map[int64]error{until - 1: ErrHandleTaken, until: nil} {
		if err := d.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
			return HandleFree(t.Context(), q, "alice", at)
		}); !errors.Is(err, want) || (want == nil && err != nil) {
			t.Errorf("HandleFree(alice) at %d = %v, want %v", at, err, want)
		}
	}

	for _, h := range []string{"alice", "ALICE"} {
		if _, err := s.Register(t.Context(), claims("sub-2"), h, session.Meta{Provider: "fake"}, ""); !errors.Is(err, ErrHandleTaken) {
			t.Errorf("registering %q inside the reservation: %v", h, err)
		}
	}

	s.Now = func() time.Time { return time.UnixMilli(until) }
	if _, err := s.Register(t.Context(), claims("sub-2"), "alice", session.Meta{Provider: "fake"}, ""); err != nil {
		t.Fatalf("registering alice once the reservation is over: %v", err)
	}
	if accountOf(t, d, "sub-2") == old {
		t.Error("the handle went back to the anonymized account")
	}
}

// accountOf is the user id the identity at the fake provider for subject
// belongs to.
func accountOf(t *testing.T, d *db.DB, subject string) int64 {
	t.Helper()
	var id int64
	if err := d.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		i, err := q.GetIdentityBySubject(t.Context(), dbq.GetIdentityBySubjectParams{ProviderKey: "fake", Subject: subject})
		id = i.UserID.Int64
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id
}
