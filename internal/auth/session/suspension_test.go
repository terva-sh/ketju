package session

import (
	"context"
	"errors"
	"testing"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// TestChokepointRefusesSuspended is AC5 of TKT-01M29CP2QK: a suspension
// made through perm, as the moderator tools make it, ends the member's
// session at once, and neither issuer gives them another: not IssueUser,
// not IssueUserWith, and not IssueUserWith whose create suspends the
// account inside the inserting transaction, since the status is read after
// create. Lifted, the member signs in again. TestOneDoorIntoSessions keeps
// the issuers the only way in, with its lists unchanged by this ticket.
func TestChokepointRefusesSuspended(t *testing.T) {
	w := newWorld(t)
	ident := w.identityFor("sam", "active")
	tok, _, err := w.store.IssueUser(t.Context(), ident, meta)
	if err != nil {
		t.Fatal(err)
	}
	signedIn, err := w.store.Lookup(t.Context(), tok)
	if err != nil {
		t.Fatal(err)
	}
	uid := signedIn.User.ID
	p := perm.New(w.db)
	suspend := perm.StatusChange{By: audit.By{Actor: w.member}, Action: perm.ActionSuspended, Reason: "spam"}
	if err := p.ChangeStatus(t.Context(), uid, suspend); err != nil {
		t.Fatal(err)
	}
	if _, err := w.store.Lookup(t.Context(), tok); !errors.Is(err, ErrNoSession) {
		t.Errorf("the session a suspension ended still looks up: %v", err)
	}

	refused := func(what string, err error) {
		t.Helper()
		var na *NotActiveError
		if !errors.As(err, &na) || na.Status != "suspended" {
			t.Errorf("%s: %v, want NotActiveError naming suspended", what, err)
		}
	}
	_, _, err = w.store.IssueUser(t.Context(), ident, meta)
	refused("IssueUser", err)
	_, _, err = w.store.IssueUserWith(t.Context(), meta, func(context.Context, *dbq.Queries) (int64, error) { return ident, nil })
	refused("IssueUserWith", err)
	if n := w.sessions(); n != 0 {
		t.Fatalf("refusals left %d session rows", n)
	}

	unsuspend := perm.StatusChange{By: audit.System("expiry"), Action: perm.ActionUnsuspended}
	if err := p.ChangeStatus(t.Context(), uid, unsuspend); err != nil {
		t.Fatal(err)
	}
	_, _, err = w.store.IssueUserWith(t.Context(), meta, func(ctx context.Context, q *dbq.Queries) (int64, error) {
		return ident, p.ChangeStatusIn(ctx, q, uid, suspend)
	})
	refused("IssueUserWith suspending inside its transaction", err)
	if n := w.sessions(); n != 0 {
		t.Fatalf("a suspension inside the transaction left %d session rows", n)
	}

	// The refusal rolled the create func's suspension back with it; the
	// account is active and signs in.
	if _, _, err := w.store.IssueUser(t.Context(), ident, meta); err != nil {
		t.Errorf("after the suspension was lifted: %v", err)
	}
}
