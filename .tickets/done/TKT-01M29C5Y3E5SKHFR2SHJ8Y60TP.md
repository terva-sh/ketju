---
schema: 3
id: TKT-01M29C5Y3E5SKHFR2SHJ8Y60TP
title: Build the server-side session store
type: task
status: done
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
  - ref: pr:61
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:17:26Z
updated_at: 2026-09-24T11:47:52Z
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

- [x] Session ids are stored hashed and a database dump contains nothing usable as a cookie value
- [x] Idle and hard expiry are both enforced, tested at their boundaries
- [x] last_used_at writes are rate limited to once per five minutes per session
- [x] Deleting a user's session rows signs them out on the next request, tested end to end
- [x] A pre-user session never satisfies a check for a signed-in member

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

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T11:42:19Z

Built and verified.

### What landed
- **`internal/auth/session`:**
  - `Store` provides Lookup, Touch, Revoke, RevokeUser and ListForUser, plus the unexported `insert`, which C7ZZR wraps.
  - `ExpiredBefore(now) Cutoff` is the single expiry rule, and `Cutoff.Live` mirrors the SQL.
  - `Cookie(token, expires)` builds the `__Host-ketju_session` cookie (Secure, HttpOnly, SameSite=Lax, Path=/).
  - `From(ctx)` exposes the resolved session and its user to /me.
  - `Middleware(store, log)` plugs into `web.Options.Session`.
- **Queries:**
  - `LookupSession` is one statement: the session, alive by both cutoffs, LEFT JOINed to the user fields GET /me needs.
  - `TouchSession` now repeats the due check in its WHERE clause and returns the affected rows.
  - `ListUserSessions`.
- **`domain.Viewer`** gains `PreUser` and `IdentityID`. `Member()` is unchanged: a pre-user viewer has UserID 0, so it is false by construction.

### Decisions (alternatives considered)
- **The token is 32 bytes as unpadded base64url, decoded strictly.**
  - The stored value is SHA-256 of the raw bytes, with no salt or KDF: 256 bits cannot be enumerated.
  - A strict decoder refuses the three other spellings of the last character, which a lax decoder maps to the same bytes. One session should have one cookie value.
  - Anything that does not decode to exactly 32 bytes is anonymous without a query, so a flood of junk cookies costs nothing.
- **The rate limit is enforced twice.** The Go pre-check means most requests open no write transaction. The SQL guard means two racing requests that both read a due value write once between them.
- **A database error during lookup is a 500, not an anonymous request.** The alternative, degrading to anonymous, would sign everyone out silently during an outage, and that reads as a sign-in bug.
- **A failed touch is logged and the request proceeds.** last_used_at is a freshness hint, not a gate.
- **Status is read through the join.** A suspended user whose rows have not yet been deleted resolves with Active=false, so they lose member rights on the next request anyway. The ticket noted this as defence in depth, beyond docs/05's "suspending deletes sessions"; it costs nothing in the one query.
- **SessionKey (the per-session cache key) is hex of SHA-256 over the stored hash, truncated to 12 bytes.** It is neither the cookie nor the row key, so a cache key that leaks into a log or dump names nothing usable.
- **A stale cookie is cleared (Max-Age=-1)** when it names no live session.
- **ListForUser returns an `Info.Key` (the same derived key)** for a later "sign out this device", plus `Live` for rows that have expired but not yet been swept.

### Verification
- **AC1:** after a user and a pre-user session are created, every cell of every row is read back as text and as blob. None contains, or is contained in, the token, its raw bytes, the standard or padded base64 of those bytes, or their hex. The row is found by sha256(raw).
- **AC2:**
  - Idle expiry: live at 30d-1ms; not live at 30d or at 30d+1ms.
  - Hard expiry: a session touched daily for 89 days is live at 90d-1ms and gone at 90d.
  - `Cutoff.Live` agrees with the SQL at both boundaries.
- **AC3:**
  - A counting trigger on `last_used_at` shows 10 requests on a fresh session write nothing.
  - 100 requests across 198 s, starting when the touch is due, write once. A request at 5m-1ms after that write does not write; one at 5m does.
  - Two racing touches of the same due session write once.
  - A touch that is not due never reaches the database (checked with a closed database).
- **AC4:** end to end through `web.New`'s real chain. A member's cookie reaches a protected route and gets 200. After `RevokeUser`, both of their sessions get 401 on the next request, with the cookie cleared. `Revoke` ends one device and not another.
- **AC5:** a table covering anonymous, member, suspended member and pre-user checks Member, PreUser, UserID, Role and IdentityID, and what the protected route answers. The pre-user viewer is not a member and gets 401.
- **Also:**
  - six malformed cookies, including the non-canonical spelling, are anonymous against a closed database;
  - a well-formed one against the closed database gets 500;
  - the request log carries `"viewer":<id>` and not the token.
- **Mutation:** 28 mutants, 27 killed. They covered both cutoffs in Go and in SQL, the hard-expiry offset, the touch guard in SQL and in Go, the length check, strict decoding, Active, PreUser, the derived key, cookie clearing, the 500 path, the log viewer, Revoke, Live, and Touch's result. The one survivor is `s.Kind == KindUser && s.User != nil` reduced to `s.User != nil`. It is equivalent, because the schema's CHECK rules out a pre_user row with a user, and it is kept as a guard.
- The full CI set passes locally: gofmt, build, vet, sqlc stable, and `go test -race ./...`.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T11:47:52Z

Review 407 (1 high, 1 medium; both fixed).
(1) High, a suspended admin keeping the admin role on the viewer. perm.Check already turns any non-member into an anonymous viewer before its admin shortcut, so no permission check passed. But domain.Viewer relied on every consumer ignoring the role of an inactive user. Session.Viewer now sets Role only when the user is Active, so a suspended viewer carries no role that anything could misread. The test world's suspended user is now an admin. The predicate table checks that its Role is empty, and runs every viewer through perm.New(db).Check on a members-only forum: only the active member is allowed.
(2) Medium, a touch reviving an idle-expired session. TouchSession checked only that the touch was due, so a lookup just before the idle boundary followed by a touch just after it would have moved last_used_at on a dead row. The UPDATE now also requires the row to be alive by the LookupSession cutoffs, computed at the time of the write. TestTouchDoesNotRevive covers the idle boundary (lookup at 30d-1ms, touch at 30d+1ms) and the hard one (touched daily, lookup at 90d-1ms, touch at 90d+1ms): no write, and the session stays gone.
Mutants, all 5 killed: the role set regardless of Active; each expiry condition dropped from the SQL; each cutoff passed as 0.

## Summary

internal/auth/session is the server-side session store. The cookie is __Host-ketju_session, carrying 32 random bytes; the table holds only their SHA-256. Resolution is one query that returns the live session and the user fields /me needs. Idle (30d) and hard (90d) expiry are one Cutoff shared with the future sweeper. last_used_at slides at most once per 5 minutes, enforced in Go and in the UPDATE itself. Revocation is a DELETE and takes effect on the next request. The middleware plugs into web.Options.Session and sets domain.Viewer, which gains PreUser and IdentityID; a pre-user session is never Member().
