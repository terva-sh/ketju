---
schema: 3
id: TKT-01M29C5Y3E5SKHFR2SHJ8Y60TP
title: Build the server-side session store
type: task
status: in-progress
status_reason: null
priority: urgent
due_on: null
labels:
  - area:identity
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29BSPEFVY4JEZK2MXSTXXB0
  - TKT-01M29BYYH73ZBC9WTS7HN03PKJ
blocks_on: none
references:
  - ref: design:identity-sessions
    path: docs/05-identity-and-accounts.md
claim:
  actor: agent:claude-code/phase1-run-415af6aa
  branch: run/c5y3e-session-store
  worktree: /home/sothr/.t3/worktrees/ketju/t3code-415af6aa
  commit: d68ae9dd4c820c01b56d53c1e88bfe25e32196ba
  session: null
  claimed_at: 2026-09-24T11:35:42Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:17:26Z
updated_at: 2026-09-24T11:37:07Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

Sessions in the database rather than in a signed cookie, so that suspension and sign-out-everywhere take effect on the next request rather than at the next expiry.

The cookie is `__Host-ketju_session`, HttpOnly, Secure, SameSite=Lax, carrying a random 256-bit id whose hash is what the server stores. Idle expiry 30 days, hard expiry 90, and `last_used_at` slides at most once every five minutes so an active reader does not generate a write per request.

The `kind` column distinguishes a user session from the pre-user session that approval mode needs, and middleware has to treat the second as authenticated-but-not-a-member rather than as signed in.

## Acceptance criteria

- [ ] Session ids are stored hashed and a database dump contains nothing usable as a cookie value
- [ ] Idle and hard expiry are both enforced, tested at their boundaries
- [ ] last_used_at writes are rate limited to once per five minutes per session
- [ ] Deleting a user's session rows signs them out on the next request, tested end to end
- [ ] A pre-user session never satisfies a check for a signed-in member

## Implementation plan

Revised at claim (2026-09-24), after reading the code as it stands: `domain.Viewer` and its context helpers, the `sessions` and `users` schema (0002), the existing session queries in `identity.sql`, `web.Options.Session` and the chain order, `logging.ContextWithViewer`, and the redaction keys. `session` and `token` are already redacted.

### Approach
1. **Package `internal/auth/session`.**
   - `Store{DB, Now}`.
   - `CookieName = "__Host-ketju_session"`.
   - `Cookie(token, expires)` builds the cookie: Path=/, HttpOnly, Secure, SameSite=Lax. It is used by C7ZZR when it signs someone in.
2. **Token and hash.**
   - The token is 32 bytes from `crypto/rand`, as unpadded base64url (43 characters).
   - The table stores SHA-256 of the raw bytes. A cookie that does not decode to exactly 32 bytes is no session, without a query.
3. **The expiry rule, in one place: `ExpiredBefore(now) Cutoff{ExpiresAt, LastUsedAt}`.**
   - A row is live when `expires_at > c.ExpiresAt` and `last_used_at > c.LastUsedAt`, where `c.LastUsedAt = now - 30d`.
   - Hard expiry: `expires_at = created_at + 90d`, set at insert.
   - Lookup and the future sweeper (C5Y2S) use the same Cutoff, so they cannot disagree.
4. **`Lookup(ctx, token) (Session, error)` is one query.**
   - A new sqlc query `LookupSession` joins `sessions` to `users` with a LEFT JOIN, because a pre_user row has no user. It matches the hash with both cutoffs bound.
   - It returns the session and the user fields GET /me needs (handle, display_name, avatar, role, kind, status, timezone, locale, prefs_json, mark_all_read_at), so CA99R stays within its budget of 2 queries.
   - A row that does not exist and a row that has expired both give `ErrNoSession`.
5. **`Touch`.**
   - The `TouchSession` query becomes `UPDATE … SET last_used_at=? WHERE id_hash=? AND last_used_at <= ?` (now - 5m). The rate limit therefore holds in SQL even if two requests race.
   - The middleware calls it only when the value it read is due, so most requests write nothing.
   - A failed touch is logged and the request continues. It is a freshness hint, not a gate.
6. **Revoking.**
   - `Revoke(ctx, token)` and `RevokeUser(ctx, userID)` use the existing DeleteSession and DeleteUserSessions.
   - `ListForUser(ctx, userID)` returns id-free `Info` rows (kind, times, ip, user agent, and a `Key` that is the hex hash). A later "sign out this device" can name a row by its Key without ever holding the token.
7. **Creating sessions.** The only constructor is the unexported `insert`. C7ZZR wraps it in the exported sign-in constructors, so until then only tests here can create sessions.
8. **The viewer.**
   - `domain.Viewer` gains `PreUser bool` and `IdentityID int64`. Only this package sets them.
   - A pre_user viewer has UserID 0, so `Member()`, which needs a user and `Active`, is false by construction. A test asserts every predicate over a table of viewers.
   - A user whose `status` is not 'active' resolves with `Active=false`, so a suspended user loses member rights on the next request even before their rows are deleted. This is defence in depth and costs nothing in the one query.
   - `SessionKey` is a short hex prefix of SHA-256(hash): opaque, never the id, and never the stored value either.
   - `session.From(ctx)` exposes the resolved session with its user fields to /me.
9. **`Middleware(store, log)`, in chain slot 6.**
   - With no cookie, a malformed cookie, or no live row, the request is anonymous, never an error. A stale cookie is cleared with Max-Age=-1.
   - A database error in the lookup is a problem 500 rather than a silent sign-out.
   - It sets `domain.WithViewer`, `logging.ContextWithViewer` (the user id only), and the session in the context.
   - The comment on `web.Options.Session` is updated, because the decision has been made.

### Tests
- **AC1:** insert a session, dump every column of every sessions row as text and as blob, and check that none equals the token, its decoded bytes, or any base64 form of either.
- **AC2:** table-driven with a fake clock. The idle boundary at 30d-1ms is live and at 30d is not; the hard boundary at 90d-1ms is live and at 90d is not. A session touched every day past 90d still expires at 90d.
- **AC3:** a counting trigger on `last_used_at` in the test database. 100 requests through the middleware within 5 minutes give exactly one UPDATE (the first request, when the value is due, or none when it was created within 5 minutes). One more request at 5m+1ms after the last write gives a second. A racing touch through the SQL guard is a no-op.
- **AC4:** end to end through `web.New` and httptest with Session set to the middleware. A member's cookie reaches a protected handler, which answers 200. After `RevokeUser`, the same cookie gets 401, the viewer is anonymous, and Set-Cookie clears it.
- **AC5:** a pre_user row resolves to `PreUser` with `Member()` false, and a protected handler answers 401. The table test covers anonymous, member, suspended member, and pre_user.
- **Also:**
  - a malformed cookie (wrong length, bad base64) runs no query, checked with a closed database, which would fail if queried;
  - a database error gives 500;
  - the log line carries the user id and never the token.
- **Mutation checks** over the cutoffs, the touch guard, the length check, the Active derivation, and the clearing of the cookie.

### Out of scope
- Sign-in constructors and cookie issuance: C7ZZR.
- Sweeping expired rows: C5Y2S, which uses `ExpiredBefore`.
- The sessions UI: CCC0X.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T07:27:10Z

From TKT-01M29BYYH7 (Add the users, identities, and sessions schema), 2026-09-24: sessions.id_hash is typed `sqltypes.SessionHash` (`internal/store/db/sqltypes`, a [32]byte with Value and Scan) in every generated query. The queries are CreateSession, GetSession, TouchSession, DeleteSession and DeleteUserSessions. None takes a plaintext id, so hashing the cookie value into a SessionHash is this ticket's job, and it is the only place that conversion should exist. The schema CHECK refuses anything but a 32-byte BLOB.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T08:12:18Z

Requirement from TKT-01M29C5Y2E (permission evaluation), review 383: build domain.Viewer from the users row (role, and status == active) on every request, and never cache role or status in the session. internal/perm treats a Viewer as a per-request snapshot and short-circuits on Role == admin, so a Viewer that outlives a request would carry an admin role past its removal. SessionKey must be opaque, never the raw session id.
