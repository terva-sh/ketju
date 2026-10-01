package session

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// TestRefreshSeesRevocationAndDoesNotTouch: Refresh reads a session again by
// the hash it holds and writes nothing. It sees a change of status, and
// answers ErrNoSession once the session is revoked, by this device or by
// every device, or has gone idle (TKT-01M29CRJ3X). A live-update stream
// relies on it to end when its session does.
func TestRefreshSeesRevocationAndDoesNotTouch(t *testing.T) {
	w := newWorld(t)
	lastUsed := func(s Session) int64 {
		var n int64
		if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT last_used_at FROM sessions WHERE id_hash = ?`, s.hash[:]).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}

	tok := w.signIn(w.member)
	sess, err := w.store.Lookup(t.Context(), tok)
	if err != nil {
		t.Fatal(err)
	}
	before := lastUsed(sess)
	w.clock.add(2 * TouchInterval)
	got, err := w.store.Refresh(t.Context(), sess)
	if err != nil {
		t.Fatalf("a live session did not refresh: %v", err)
	}
	if got.Viewer() != sess.Viewer() || got.User == nil || got.User.ID != w.member {
		t.Errorf("Refresh = %+v, want the session Lookup found", got)
	}
	if after := lastUsed(sess); after != before {
		t.Errorf("Refresh touched the session: last_used_at %d, was %d", after, before)
	}

	// A status change shows at once: the refreshed viewer is not active.
	if err := w.db.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE users SET status = 'suspended' WHERE id = ?`, w.member)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := w.store.Refresh(t.Context(), sess); err != nil || got.Viewer().Active {
		t.Errorf("Refresh after a status change = %+v, %v; want an inactive viewer", got.Viewer(), err)
	}
	if err := w.db.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE users SET status = 'active' WHERE id = ?`, w.member)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name string
		end  func(token string)
	}{
		{"revoked on this device", func(token string) {
			if err := w.store.Revoke(t.Context(), token); err != nil {
				t.Fatal(err)
			}
		}},
		{"revoked everywhere", func(string) {
			if err := w.store.RevokeUser(t.Context(), w.member); err != nil {
				t.Fatal(err)
			}
		}},
		{"gone idle", func(string) { w.clock.add(IdleExpiry + time.Minute) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			tok := w.signIn(w.member)
			sess, err := w.store.Lookup(t.Context(), tok)
			if err != nil {
				t.Fatal(err)
			}
			c.end(tok)
			if _, err := w.store.Refresh(t.Context(), sess); !errors.Is(err, ErrNoSession) {
				t.Errorf("Refresh = %v, want ErrNoSession", err)
			}
		})
	}
}
