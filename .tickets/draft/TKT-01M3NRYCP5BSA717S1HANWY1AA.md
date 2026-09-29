---
schema: 3
id: TKT-01M3NRYCP5BSA717S1HANWY1AA
title: Add the admin API gate, audit helper and admin screen scaffold
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:moderation
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T05:07:05Z
updated_at: 2026-09-29T05:07:05Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

Seven Phase 2 tickets build admin and moderator surfaces: the audit view (TKT-01M29CP2R4), the moderator queue (TKT-01M29CRJ4E), forum management (TKT-01M29CXK19), users, groups and permissions (TKT-01M29CXK1X), site settings (TKT-01M29CXK2H), suspension (TKT-01M29CP2QK) and agent accounts (TKT-01M29P9RSW). Each needs the same three things first, and an autonomous run working them in parallel would otherwise build each one several times in conflicting PRs. Found while grooming them (TKT-01M3M9RR26).

### The admin API gate

`internal/web/api/admin.go` holds two helpers every `/api/v1/admin/*` handler starts with: `staff(w, r)`, which admits an active moderator or admin, and `admin(w, r)`, which admits an active admin. Anonymous is `unauthenticated`, a pre-user session is `forbidden`, and any other member is `forbidden`. Routes register through one `adminRoute(mux, pattern, gate, handler)` so the list of admin routes is data a test can walk. A handler that needs a finer rule, such as per-forum `moderate`, checks it after the gate.

### The audit helper

`internal/audit` is the one way rows reach `audit_log`: `By{Actor int64; IP string}`, `Append(ctx, q, at, by, action, target, details)` and target constructors that fix each `target_id` format: `User(id)` and `Forum(id)` and `Group(id)` and `Flag(id)` and `Application(id)` as decimal ids, `Thread(id)` as the base32 thread id, `Post(thread, ordinal)` as `<base32>/<ordinal>`, and `Setting(key)` as the key. `perm.Promote` and `settings.SetBy` move onto it, and `settings.Change` gains `IP`. `web.ClientIP(r)` is exported for the `ip` column, and it reads the address as the per-address limiter does, never `X-Forwarded-For` (docs/08).

### The admin screen scaffold

`web/client/src/screens/admin/AdminRoot.tsx` becomes a layout: a section list filtered by role (moderators see Queue, Applications and Users; admins see every section) and the section itself. Shared pieces live in `screens/admin/common/`: a table, a labelled field, a confirm button, and a send wrapper that turns `conflict`, `validation-failed` and `forbidden` into a sentence. Everything stays under `screens/admin/`, so it stays in the lazy chunk and out of the reader's budget (docs/07 "Preact client").

## Acceptance criteria

- [ ] Every route registered under /api/v1/admin/ answers 401 to an anonymous viewer and 403 to a pre-user and to a plain member, enforced by TestAdminRoutesAreGated over the registered route list
- [ ] internal/audit is the only caller of dbq.AppendAudit outside internal/store/db (TestOneWayIntoAuditLog), and perm.Promote and settings.SetBy write the same rows through it as before
- [ ] docs/04 audit_log lists every target_type and the format of its target_id
- [ ] /admin lists the sections the viewer's role may use, and TestTheAdminBundleIsSeparate and the budget check pass with the reader total unchanged

## Implementation plan

### Settled in grooming

- Admin routes live under `/api/v1/admin/*` and each is gated on the role docs/08's "Primitives" table names for its action (docs/07 "Moderation and admin").
- A viewer whose status is not active has no role (`session.Session.Viewer`), so the gate needs no status check of its own.
- `audit_log` is append-only by trigger and by `TestAuditLogAppendOnly` (docs/04 "audit_log").
- X-Forwarded-For is not consulted (docs/08 "The transport-level ceiling").
- The admin screens are one lazy chunk that the thread-view budget does not count, and `TestTheAdminBundleIsSeparate` holds it apart (docs/07 "Preact client", TKT-01M29CEE52). The built client in `internal/web/spa/dist` is committed: run `go generate ./internal/web/spa` after any client change.

Engineering choices made here. A separate ticket rather than "whichever lands first builds it" in seven plans: the parallel run would build it more than once in PRs that conflict. Handles rather than ids in user routes, matching `/users/{handle}`; ids for forums and groups, which have no handle.

### Approach

1. `internal/web/api/admin.go`: `staff`, `admin`, and `adminRoute`, as described. No route of its own besides the gate.
2. `internal/audit`: `By`, `Target`, `Append`, the constructors, and `audit_test.go`. Refactor `perm.Promote` and `settings.SetBy` onto it, with no change to what they write.
3. `internal/web`: export `ClientIP`, used by the limiter and by handlers.
4. docs/04 "audit_log": a table of `target_type` values and `target_id` formats, and the rule that rows go through `internal/audit`.
5. Client: `AdminRoot.tsx` as a layout with the section list, `screens/admin/common/`, and tests. Run `go generate ./internal/web/spa` and commit `dist`. `npm --prefix web/client run budget` must report the same reader total as before.

### Tests

- `TestAdminRoutesAreGated`: walks every route registered through `adminRoute` and asserts 401 for anonymous, 403 for a pre-user and for a member, and 403 for a moderator on an admin-gated route. A route added later is covered without editing the test.
- `TestOneWayIntoAuditLog`: a source scan like `TestMutationsGoThroughPerm`, failing on a call to `AppendAudit` outside `internal/audit` and `internal/store/db`.
- `TestTargetFormats`: each constructor's `target_id`, and a post target round-trips.
- `TestPromoteUnchanged` and the settings tests stay green: same action, target and details.
- `TestClientIPIgnoresForwardedFor`.
- `AdminRoot.test.tsx`: a moderator sees Queue, Applications and Users only; an admin sees every section. `TestTheAdminBundleIsSeparate` stays green.

### Out of scope

Each section's screen and API, which belong to the seven tickets named in the description. The application review routes are TKT-01M29CP2MG's, which may use the gate and the helper as well.
