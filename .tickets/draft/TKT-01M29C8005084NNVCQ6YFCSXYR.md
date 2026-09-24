---
schema: 3
id: TKT-01M29C8005084NNVCQ6YFCSXYR
title: Implement open-mode registration with handle selection
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29C5Y3E5SKHFR2SHJ8Y60TP
  - TKT-01M29BYYH73ZBC9WTS7HN03PKJ
  - TKT-01M29C7ZZR3FJMZ7P7VA0CWNJ2
blocks_on: none
references:
  - ref: design:identity-registration
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:18:33Z
updated_at: 2026-09-24T05:21:03Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

The `mode = "open"` path: a one-field screen asking for a handle, prefilled from `preferred_username` or the email local part, checked for uniqueness, then a user with status active, an identity, and a session.

The handle is chosen here rather than taken from the IdP, which is the decision that lets a user keep their identity when an organization changes providers. Uniqueness is case-insensitive, and the check needs to happen at the database rather than only in the form, because two people can submit the same handle in the same second.

## Acceptance criteria

- [ ] A handle is validated for character set, length, and case-insensitive uniqueness before the user is created
- [ ] A collision between two concurrent registrations produces one user and one clear error, not two users
- [ ] Reserved handles such as admin and deleted-user prefixes are refused
- [ ] The user, the identity, and the session are created atomically or not at all

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. On the callback's signed-out not-found branch with `mode="open"`, or with approval mode on an `auto_approve` provider, seal the verified claims (provider key, sub, issuer, email, email_verified, name, preferred_username) into a short-lived `__Host-ketju_reg` cookie with the TKT-01M29C7ZZ9 sealer. Then redirect to the handle screen. This keeps "an abandoned login writes nothing".
2. Prefill = `preferred_username`, else the email local part, normalised to the handle charset. It is offered and never reserved.
3. `identity.ValidateHandle(h)`:
   - Charset is the one the mention parser accepts (`isHandleByte` in internal/render/mention.go: `[A-Za-z0-9_.-]`), so every handle can be @-mentioned.
   - Length is 1..N, with N at most `render.MaxHandle` (64).
   - It refuses a reserved list (`admin`, `deleted-user-*`, route words such as `me`, `new`, `settings`, `apply`, `auth`, `api`) compared case-insensitively.
4. `identity.Register(ctx, claims, handle)` runs one `BEGIN IMMEDIATE` transaction: insert `users` (status active, role member, kind human), insert `identities`, `session.IssueUser` from TKT-01M29C7ZZR, run the bootstrap hook from TKT-01M29C800G, then commit. The inserts are sqlc queries (`InsertUser`, `InsertIdentity`). Uniqueness is the schema's case-insensitive unique index from TKT-01M29BYYH7. Map the constraint error to a handle-taken result, never to the constraint text (docs/07 "No error body ever carries the underlying error text").
5. The endpoint accepts JSON `POST /api/v1/register {handle}`, and a `GET` returns the prefill. The reg cookie is cleared on success.

### Files
New: internal/service/identity/register.go, handle.go, internal/web/api/register.go, and a DTO pair in internal/dto (`RegisterForm{prefill}` and request `{handle}`) with goldens. Reuses the sealer, `session.IssueUser`, and `problem.Coded(CodeConflict|CodeValidationFailed, …)`.

### Tests
- AC1: table-driven `ValidateHandle` (charset, 0/1/64/65 length, `Alice` against an existing `alice`).
- AC2: two goroutines register the same handle in different case against a real SQLite file under `-race`. Exactly one user row; the loser gets `conflict`.
- AC3: `admin`, `Admin`, and `deleted-user-7` are refused with `validation-failed`.
- AC4: fault injection makes the session insert fail after the user insert, and the test asserts zero users, identities, and sessions. Use a hook in the service or a closed transaction.
- Golden JSON for the new DTOs.

### Design
Reread docs/05 "Registration path / mode = open" and the handle rationale in docs/04 "users".

### Interactions
Consumes TKT-01M29C7ZZ9 (claims hand-off), TKT-01M29C7ZZR, and TKT-01M29BYYH7. `mode` lives in the `settings` table (TKT-01M29BYYJ0), so this also consumes a settings reader.
Open: no route or DTO for handle selection exists in docs/07. Add them to docs/07 in the same PR. Also open: the handle charset, length, and reserved list are not documented.
Open: how the claims survive between callback and submit is not documented; the cookie above is the lean.
Open: TKT-01M29BSPEX may skip this screen for auto_approve providers.

### Settled in grooming (2026-09-24)

- The handle rule is in docs/04. Reserve handles that collide with top-level routes: api, auth, admin, assets, blobs, events, healthz, me, new.
- The verified claims travel from the callback to the handle submit in the sealed `__Host-ketju_reg` cookie.
- Define the route and shape in docs/07 in this PR.
