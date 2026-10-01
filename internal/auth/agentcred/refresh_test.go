package agentcred

import (
	"errors"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// TestRefreshSeesRevocationAndDoesNotTouch: Refresh reads a credential again
// by the hash it holds, writes nothing, and answers ErrNoCredential once the
// credential is revoked or its agent is suspended (TKT-01M29CRJ3X). A
// live-update stream relies on it to end when its credential does.
func TestRefreshSeesRevocationAndDoesNotTouch(t *testing.T) {
	w := newWorld(t)
	lastUsed := func(id int64) int {
		return w.count(`SELECT coalesce(last_used_at, 0) FROM agent_credentials WHERE id = ?`, id)
	}

	token := w.mustIssue("scribe")
	c, err := w.store.Lookup(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	// Touched once, as the request that opened a stream touches it, so a
	// second write would show as a later last_used_at.
	if ok, err := w.store.Touch(t.Context(), c); err != nil || !ok {
		t.Fatalf("Touch = %v, %v", ok, err)
	}
	before := lastUsed(c.ID)
	w.now = w.now.Add(2 * TouchInterval)
	got, err := w.store.Refresh(t.Context(), c)
	if err != nil {
		t.Fatalf("a live credential did not refresh: %v", err)
	}
	if got.ID != c.ID || got.UserID != c.UserID || got.Viewer() != c.Viewer() {
		t.Errorf("Refresh = %+v, want the credential Lookup found, %+v", got, c)
	}
	if after := lastUsed(c.ID); after != before {
		t.Errorf("Refresh touched the credential: last_used_at %d, was %d", after, before)
	}

	// An admin's revocation.
	w.write(func(q *dbq.Queries) error {
		_, err := perm.RevokeAgentCredentialIn(t.Context(), q, w.now, w.by(), w.users["scribe"], "admin")
		return err
	})
	if _, err := w.store.Refresh(t.Context(), c); !errors.Is(err, ErrNoCredential) {
		t.Errorf("Refresh of a revoked credential = %v, want ErrNoCredential", err)
	}

	// A suspension, which revokes too.
	token = w.mustIssue("indexer")
	c, err = w.store.Lookup(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.perm.ChangeStatus(t.Context(), w.users["indexer"], perm.StatusChange{
		By: audit.By{Actor: w.users["root"], IP: "192.0.2.1"}, Action: perm.ActionSuspended, Reason: "stop",
		Until: w.now.Add(time.Hour).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.store.Refresh(t.Context(), c); !errors.Is(err, ErrNoCredential) {
		t.Errorf("Refresh after a suspension = %v, want ErrNoCredential", err)
	}
}
