// Package sessiontest signs users in for tests in other packages.
//
// The session store mints sessions only through the sign-in flow
// (TKT-01M29C7ZZR), and deliberately exports no other way. A handler test
// still needs a signed-in request, so this writes the row the store would:
// a random 256-bit id in the cookie, its SHA-256 in the table. session's own
// TestSessiontestMatchesTheStore holds the two to the same scheme.
package sessiontest

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// SignIn creates a live user session for userID at now and returns its
// cookie value.
func SignIn(t testing.TB, d *db.DB, userID int64, now time.Time) string {
	t.Helper()
	return insert(t, d, dbq.CreateSessionParams{Kind: "user", UserID: sql.NullInt64{Int64: userID, Valid: true}}, now)
}

// PreUser creates a pre-user session bound to identityID, signed in through
// the provider "github".
func PreUser(t testing.TB, d *db.DB, identityID int64, now time.Time) string {
	t.Helper()
	return insert(t, d, dbq.CreateSessionParams{Kind: "pre_user", IdentityID: sql.NullInt64{Int64: identityID, Valid: true},
		OidcProviderKey: sql.NullString{String: "github", Valid: true}}, now)
}

func insert(t testing.TB, d *db.DB, p dbq.CreateSessionParams, now time.Time) string {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	p.IDHash = sha256.Sum256(raw[:])
	p.CreatedAt, p.LastUsedAt = now.UnixMilli(), now.UnixMilli()
	p.ExpiresAt = now.Add(90 * 24 * time.Hour).UnixMilli()
	if err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error { return q.CreateSession(t.Context(), p) }); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:])
}

// Cookie is the request cookie carrying token.
func Cookie(token string) *http.Cookie {
	return &http.Cookie{Name: "__Host-ketju_session", Value: token}
}
