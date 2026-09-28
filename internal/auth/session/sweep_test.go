package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/web"
)

// sessionRows counts the rows in sessions, expired or not.
func sessionRows(t *testing.T, w *world) int {
	t.Helper()
	var n int
	if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), "SELECT count(*) FROM sessions").Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

// registered issues a session the way registration does, through
// IssueUserWith, for a user and identity the callback writes.
func (w *world) registered(handle string) string {
	w.t.Helper()
	tok, _, err := w.store.IssueUserWith(w.t.Context(), Meta{Provider: "github"},
		func(ctx context.Context, q *dbq.Queries) (int64, error) {
			uid, err := q.CreateUser(ctx, dbq.CreateUserParams{Handle: handle, DisplayName: handle,
				Status: "active", Role: "member", Kind: "human", CreatedAt: 1, UpdatedAt: 1})
			if err != nil {
				return 0, err
			}
			return q.CreateIdentity(ctx, dbq.CreateIdentityParams{UserID: sql.NullInt64{Int64: uid, Valid: true},
				ProviderKey: "github", Subject: "sub-" + handle, Issuer: "https://github.com", CreatedAt: 1, UpdatedAt: 1})
		})
	if err != nil {
		w.t.Fatal(err)
	}
	return tok
}

// live reports whether Lookup accepts tok right now.
func (w *world) live(tok string) bool {
	w.t.Helper()
	_, err := w.store.Lookup(w.t.Context(), tok)
	if err != nil && !errors.Is(err, ErrNoSession) {
		w.t.Fatal(err)
	}
	return err == nil
}

// TestSweepDeletesExpiredOnly is AC1's first half: the sweep deletes
// sessions of both kinds at the idle boundary and not a millisecond before
// it, and after every sweep a row exists exactly when Lookup accepts it.
func TestSweepDeletesExpiredOnly(t *testing.T) {
	w := newWorld(t)
	sweep := func(want int64) {
		t.Helper()
		n, err := w.store.Sweep(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Fatalf("Sweep removed %d, want %d", n, want)
		}
	}

	userA, preA := w.signIn(w.member), w.preUser()
	w.clock.add(24 * time.Hour)
	userB := w.signIn(w.member)
	w.clock.add(IdleExpiry - 24*time.Hour) // A at exactly 30 d idle, B at 29 d
	fresh := w.registered("newcomer")      // issued now through IssueUserWith

	sweep(2)
	for name, c := range map[string]struct {
		tok  string
		live bool
	}{
		"user at 30 d idle": {userA, false}, "pre_user at 30 d idle": {preA, false},
		"user at 29 d idle": {userB, true}, "registered just now": {fresh, true},
	} {
		if got := w.live(c.tok); got != c.live {
			t.Errorf("%s: live=%v, want %v", name, got, c.live)
		}
	}
	if n := sessionRows(t, w); n != 2 {
		t.Fatalf("%d rows left, want the 2 live sessions", n)
	}

	// B one millisecond short of the idle limit: kept.
	w.clock.add(24*time.Hour - time.Millisecond)
	sweep(0)
	if !w.live(userB) {
		t.Fatal("a session a millisecond short of idle expiry was swept")
	}
	w.clock.add(time.Millisecond)
	sweep(1)
	if w.live(userB) || sessionRows(t, w) != 1 {
		t.Fatal("B at 30 d idle was not swept, or something else was")
	}
}

// TestSweepHonoursTheHardExpiry: a session used every day is never idle, and
// is swept at ninety days from creation and not a millisecond before.
func TestSweepHonoursTheHardExpiry(t *testing.T) {
	w := newWorld(t)
	tok := w.signIn(w.member)
	for day := 1; day < 90; day++ {
		w.clock.add(24 * time.Hour)
		s, err := w.store.Lookup(t.Context(), tok)
		if err != nil {
			t.Fatalf("day %d: %v", day, err)
		}
		if _, err := w.store.Touch(t.Context(), s); err != nil {
			t.Fatal(err)
		}
		if n, err := w.store.Sweep(t.Context()); err != nil || n != 0 {
			t.Fatalf("day %d: a busy session swept (n=%d, err=%v)", day, n, err)
		}
	}
	w.clock.add(24*time.Hour - time.Millisecond)
	if n, _ := w.store.Sweep(t.Context()); n != 0 {
		t.Fatal("swept at 90 d - 1 ms")
	}
	w.clock.add(time.Millisecond)
	if n, err := w.store.Sweep(t.Context()); err != nil || n != 1 {
		t.Fatalf("at 90 d the sweep removed %d (%v), want 1", n, err)
	}
}

// TestSweepDoesNotAffectAResolvedRequest is AC1's second half: a request
// that resolved its session keeps its viewer when the sweep deletes the row
// partway through it. Only the next request is anonymous.
func TestSweepDoesNotAffectAResolvedRequest(t *testing.T) {
	w := newWorld(t)
	tok := w.signIn(w.member)
	// A day short of idle expiry, so the request resolves a live session.
	w.clock.add(IdleExpiry - 24*time.Hour)

	var swept int64
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/private", func(rw http.ResponseWriter, r *http.Request) {
		// Mid-request: the session expires and the sweeper deletes it. The
		// middleware has just touched it, so it is the hard limit that
		// passes here, not the idle one.
		w.clock.add(HardExpiry)
		n, err := w.store.Sweep(r.Context())
		if err != nil {
			t.Error(err)
		}
		swept = n
		v := domain.ViewerFrom(r.Context())
		s, ok := From(r.Context())
		if !v.Member() || !ok || s.User == nil || s.User.ID != w.member {
			rw.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprintf(rw, "hello %d", v.UserID)
	})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	pass := func(h http.Handler) http.Handler { return h }
	h := web.New(web.Groups{API: api}, web.Options{Logger: log, Session: Middleware(w.store, log),
		RateLimit: pass, CSRF: pass})

	rec := get(h, tok)
	if rec.Code != http.StatusOK || rec.Body.String() != fmt.Sprintf("hello %d", w.member) {
		t.Fatalf("the in-flight request = %d %q, want the member it resolved", rec.Code, rec.Body.String())
	}
	if swept != 1 {
		t.Fatalf("the sweep inside the request removed %d, want 1", swept)
	}
	rec = get(h, tok)
	if rec.Code != http.StatusUnauthorized || !cleared(rec) {
		t.Fatalf("the next request = %d (cleared=%v), want anonymous with the cookie cleared", rec.Code, cleared(rec))
	}
}
