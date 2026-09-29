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
updated_at: 2026-09-29T05:41:12Z
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

`internal/web/api/admin.go` holds three gates, and every `/api/v1/admin/*` handler starts with one of them:

- `admin(w, r)` admits an active admin.
- `staff(w, r)` admits an active moderator or admin, by role.
- `moderates(w, r)` admits an active member who may moderate somewhere: a moderator or admin by role, or a member who holds `moderate` on at least one forum through a group (docs/04 "permissions"; `perm.ModeratesAny`, a new read). It exists for the queue, which a forum-scoped moderator uses (TKT-01M29CRJ4E), and only the queue uses it. Its handlers still filter what they show by per-forum `moderate`.

Each gate answers `unauthenticated` to anonymous, `forbidden` to a pre-user session, and `forbidden` to any member it does not admit. Routes register through one `adminRoute(mux, pattern, gate, handler)` so the list of admin routes, with each route's gate, is data a test can walk. A handler that needs a finer rule checks it after the gate.

### The audit helper

`internal/audit` is the one way rows reach `audit_log`: `By{Actor int64; IP string; Via string}`, where `Actor` 0 means no account and is written as SQL NULL, never as id 0, and `audit.System(via)` builds one for work nobody's account did (the command line, an expiry job, the flag auto-hide), recording `via` in `details_json`, `Append(ctx, q, at, by, action, target, details)` and target constructors that fix each `target_id` format: `User(id)` and `Forum(id)` and `Group(id)` and `Flag(id)` and `Application(id)` as decimal ids, `Thread(id)` as the base32 thread id, `Post(thread, ordinal)` as `<base32>/<ordinal>`, and `Setting(key)` as the key. `perm.Promote` and `settings.SetBy` move onto it, and `settings.Change` gains `IP`. `web.ClientIP(r)` is exported for the `ip` column, and it reads the address as the per-address limiter does, never `X-Forwarded-For` (docs/08).

### The admin screen scaffold

`web/client/src/screens/admin/AdminRoot.tsx` becomes a layout: a section list and the section itself. Queue is listed when `/me`'s `queue` is not null (TKT-01M29CRJ4E adds it), so a forum-scoped moderator sees it. Applications and Users are listed for moderators and admins by role, and every section for admins. Until `queue` exists, Queue follows the role. Shared pieces live in `screens/admin/common/`: a table, a labelled field, a confirm button, and a send wrapper that turns `conflict`, `validation-failed` and `forbidden` into a sentence. Everything stays under `screens/admin/`, so it stays in the lazy chunk and out of the reader's budget (docs/07 "Preact client").

## Acceptance criteria

- [ ] Every route registered under /api/v1/admin/ answers 401 to an anonymous viewer and 403 to a pre-user and to a plain member, enforced by TestAdminRoutesAreGated over the registered route list
- [ ] internal/audit is the only caller of dbq.AppendAudit outside internal/store/db (TestOneWayIntoAuditLog), and perm.Promote and settings.SetBy write the same rows through it as before
- [ ] docs/04 audit_log lists every target_type and the format of its target_id
- [ ] /admin lists the sections the viewer's role may use, and TestTheAdminBundleIsSeparate and the budget check pass with the reader total unchanged
- [ ] A member granted moderate on one forum through a group passes the moderates gate and neither staff nor admin, and the admin section list shows them Queue when /me's queue is not null (TestModeratesGate, AdminRoot.test.tsx)
- [ ] audit.System(via) writes actor_id NULL with via in the details, and no audit row is ever written with actor id 0 (TestSystemActorIsNull)

## Implementation plan

### Settled in grooming

- Admin routes live under `/api/v1/admin/*` and each is gated on the role docs/08's "Primitives" table names for its action (docs/07 "Moderation and admin").
- A viewer whose status is not active has no role (`session.Session.Viewer`), so the gate needs no status check of its own.
- `audit_log` is append-only by trigger and by `TestAuditLogAppendOnly` (docs/04 "audit_log").
- X-Forwarded-For is not consulted (docs/08 "The transport-level ceiling").
- The admin screens are one lazy chunk that the thread-view budget does not count, and `TestTheAdminBundleIsSeparate` holds it apart (docs/07 "Preact client", TKT-01M29CEE52). The built client in `internal/web/spa/dist` is committed: run `go generate ./internal/web/spa` after any client change.

Terva review 1349 (PR #140, head 90d2f7c) found that `staff` alone would shut out a forum-scoped moderator the queue serves, and that `By` could not express a system actor. Both are fixed above: the `moderates` gate, and `Actor` 0 as NULL with `audit.System`.

Engineering choices made here. A separate ticket rather than "whichever lands first builds it" in seven plans: the parallel run would build it more than once in PRs that conflict. Handles rather than ids in user routes, matching `/users/{handle}`; ids for forums and groups, which have no handle.

### Approach

1. `internal/web/api/admin.go`: `admin`, `staff`, `moderates` and `adminRoute`, as described. `perm.ModeratesAny(ctx, v) (bool, error)` answers through the permission cache: true for a moderator or admin by role, otherwise whether any forum grants the viewer `moderate`. No route of its own besides the gates.
2. `internal/audit`: `By`, `System`, `Target`, `Append`, the constructors, and `audit_test.go`. `Append` writes `By.Actor` 0 as SQL NULL and merges a non-empty `Via` into the details as `"via"`. Refactor `perm.Promote` and `settings.SetBy` onto it, with no change to what they write: their actor 0 already means the command line, and `ketju admin setting set` already records `"via": "cli"`.
3. `internal/web`: export `ClientIP`, used by the limiter and by handlers.
4. docs/04 "audit_log": a table of `target_type` values and `target_id` formats, and the rule that rows go through `internal/audit`.
5. Client: `AdminRoot.tsx` as a layout with the section list, `screens/admin/common/`, and tests. Run `go generate ./internal/web/spa` and commit `dist`. `npm --prefix web/client run budget` must report the same reader total as before.

### Tests

- `TestAdminRoutesAreGated`: walks every route registered through `adminRoute` and asserts 401 for anonymous, 403 for a pre-user and for a member with no grants, and 403 for a moderator on an admin-gated route. A route added later is covered without editing the test.
- `TestModeratesGate`: a member of the `moderators` group granted `moderate` on one forum passes `moderates` and fails `staff` and `admin`. The same member with the grant revoked fails `moderates` on the next request.
- `TestSystemActorIsNull`: `audit.System("expiry")` writes `actor_id` NULL and `"via": "expiry"`, and no row is ever written with actor id 0.
- `TestOneWayIntoAuditLog`: a source scan like `TestMutationsGoThroughPerm`, failing on a call to `AppendAudit` outside `internal/audit` and `internal/store/db`.
- `TestTargetFormats`: each constructor's `target_id`, and a post target round-trips.
- `TestPromoteUnchanged` and the settings tests stay green: same action, target and details.
- `TestClientIPIgnoresForwardedFor`.
- `AdminRoot.test.tsx`: a moderator sees Queue, Applications and Users only. An admin sees every section. A member whose `/me` has a non-null `queue` sees Queue alone. `TestTheAdminBundleIsSeparate` stays green.

### Out of scope

Each section's screen and API, which belong to the seven tickets named in the description. The application review routes are TKT-01M29CP2MG's, which may use the gate and the helper as well.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:41:12Z

Terva review 1349 (PR #140, head 90d2f7c) found two problems, both fixed in the description and plan. The staff gate shut out a forum-scoped moderator the queue serves: a moderates gate is added, and the Queue section follows /me's queue. By could not express a system actor: Actor 0 is written as NULL, and audit.System(via) records the via. The flag auto-hide in the moderation group records the system as its actor this way.
