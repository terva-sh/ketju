---
schema: 3
id: TKT-01M29C5Y3E5SKHFR2SHJ8Y60TP
title: Build the server-side session store
type: task
status: ready
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
claim: null
archive: null
created_at: 2026-09-11T23:17:26Z
updated_at: 2026-09-24T08:12:18Z
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

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. Create package `internal/auth/session`. It owns `Session`, `Kind` (`user`, `pre_user`), `Store`, `Viewer`, and the context helpers. The design is hand-rolled, as decided in docs/05 "Sessions and CSRF" and TKT-01M29BSPEF.
2. Token: 32 bytes from `crypto/rand`, base64url in the `__Host-ketju_session` cookie (Path=/, HttpOnly, Secure, SameSite=Lax). Store SHA-256(token) only. A single hash is enough because the token already has 256 bits of entropy, so it needs no salt or KDF.
3. `Store.Lookup(ctx, token)` is one query: `sessions` joined to `users` (LEFT JOIN, because a pre_user row has no user), matched by hash, with `expires_at > now` and `last_used_at > now-30d`. It returns a `Viewer` that already carries the user fields GET /me needs (TKT-01M29CA99R depends on this to stay within 2 queries).
4. Hard expiry: set `expires_at = created_at + 90d` at insert. Idle expiry: compare `last_used_at` against 30d at lookup. The Store takes a `now func() time.Time` so tests can control the clock.
5. `Store.Touch` runs `UPDATE … SET last_used_at=? WHERE id=? AND last_used_at < now-5m`. Its only caller is the middleware, and only when the value it read is older than 5 minutes. This keeps active readers at zero writes on most requests.
6. `Store.Revoke(id)`, `Store.RevokeUser(userID)`, and `Store.ListForUser(userID)`. TKT-01M29CCC0X and the lifecycle work in TKT-01M29CKRFS consume these.
7. The row insert is an unexported `insert`. TKT-01M29C7ZZR wraps it in the only exported constructors. Until then, only tests in this package call it.
8. `session.Middleware(store)` plugs into `web.Options.Session` at chain slot 6 in internal/web/server.go. A missing or invalid cookie produces an anonymous Viewer, never an error. The middleware logs the viewer id and never the token; add `session` and `token` to the redacting handler's names in internal/logging if they are missing.
9. `Viewer` methods: `Anonymous()`, `PreUser()`, and `Member()`. `Member()` is true only for kind=user with a non-null user whose `status='active'`. A pre_user session never satisfies it.

### Files
New: internal/auth/session/{session.go,store.go,middleware.go,viewer.go}+tests. Touched: internal/web/server.go (only the doc comment on `Session`). Queries are sqlc (docs/04, decided 2026-09-24): `LookupSessionByHash` (a join to users), `TouchSession`, `DeleteSession`, `DeleteSessionsForUser`, `ListSessionsForUser`, and `InsertSession`, all in a sessions .sql file in the data lane's query directory. The generated package path is `internal/store/db` or `internal/store/db`; the interfaces file covers the question.

### Tests
- AC1: a test inserts a session, dumps every column of `sessions`, and checks that no value equals the cookie token or its base64 form.
- AC2: table-driven with a fake clock at idle 30d±1ms and hard 90d±1ms, plus a session kept alive past 90d by touches that still expires.
- AC3: 100 lookups inside 5 minutes produce exactly one UPDATE, counted by a query-counting driver wrapper or by reading `last_used_at` back. A lookup at 5m+1ms produces a second one.
- AC4: end to end through `web.New` and httptest. Sign in, `RevokeUser`, and the next request's Viewer is anonymous; a protected handler returns 401.
- AC5: a pre_user row resolves to `PreUser()` with `Member()` false. A table test covers every Viewer predicate.
- Run everything under `-race`.

### Design
Reread docs/05 "Login flow" step 4 and "Sessions and CSRF", docs/04 "sessions", and the docs/02 chain order.

### Interactions
Consumes the `sessions`/`users` schema from TKT-01M29BYYH7 and the db handle from TKT-01M29BW9Q1. Defines `session.Viewer`, which TKT-01M29C5Y2E (perm) and every API ticket consume. TKT-01M29C5Y2S (sweeper) deletes expired rows with the same expiry predicate, so export it as `session.ExpiredBefore(now)`.
Open: the join on `users.status` is defense in depth beyond "suspending deletes sessions" in docs/05. It is free inside the one query, but the docs do not require it.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T07:27:10Z

From TKT-01M29BYYH7 (Add the users, identities, and sessions schema), 2026-09-24: sessions.id_hash is typed `sqltypes.SessionHash` (`internal/store/db/sqltypes`, a [32]byte with Value and Scan) in every generated query. The queries are CreateSession, GetSession, TouchSession, DeleteSession and DeleteUserSessions. None takes a plaintext id, so hashing the cookie value into a SessionHash is this ticket's job, and it is the only place that conversion should exist. The schema CHECK refuses anything but a 32-byte BLOB.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T08:12:18Z

Requirement from TKT-01M29C5Y2E (permission evaluation), review 383: build domain.Viewer from the users row (role, and status == active) on every request, and never cache role or status in the session. internal/perm treats a Viewer as a per-request snapshot and short-circuits on Role == admin, so a Viewer that outlives a request would carry an admin role past its removal. SessionKey must be opaque, never the raw session id.
