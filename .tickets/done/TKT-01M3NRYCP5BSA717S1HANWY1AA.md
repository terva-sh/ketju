---
schema: 3
id: TKT-01M3NRYCP5BSA717S1HANWY1AA
title: Add the admin API gate, audit helper and admin screen scaffold
type: task
status: done
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
references:
  - ref: pr:145
    path: null
claim: null
archive: null
created_at: 2026-09-29T05:07:05Z
updated_at: 2026-09-29T14:45:30Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-nrycp5
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

`internal/audit` is the one way rows reach `audit_log`: `By{Actor int64; IP string; Via string}`, where `Actor` 0 means no account and is written as SQL NULL, never as id 0, and `audit.System(via)` builds one for work nobody's account did (the command line, an expiry job, the flag auto-hide), recording `via` in `details_json`, `Append(ctx, q, at, by, action, target, details)` and target constructors that fix each `target_id` format: `User(id)` and `Forum(id)` and `Group(id)` and `Flag(id)` and `Application(id)` as decimal ids, `Thread(id)` as the base32 thread id, `Post(thread, ordinal)` as `<base32>/<ordinal>`, and `Setting(key)` as the key. `perm.Promote` and `settings.SetBy` move onto it, and `settings.Change` gains `IP`. The projection writer is a caller like any other. The moderation plans on PR #138 (TKT-01M29CKRGS step 1) write the audit row for a log-recorded moderation action inside the transaction that projects the record, from `internal/events/projection/audit.go`, through `audit.Append`. That row carries the frame's timestamp as `at`, the frame's actor in `By.Actor` (0 written as NULL), and an empty `IP`, because the log carries no address. `Append` therefore takes `at` from its caller rather than reading the clock, and accepts an empty `IP`. `web.ClientIP(r)` is exported for the `ip` column, and it reads the address as the per-address limiter does, never `X-Forwarded-For` (docs/08).

### The admin screen scaffold

`web/client/src/screens/admin/AdminRoot.tsx` becomes a layout: a section list and the section itself. Queue is listed when `/me`'s `queue` is not null (TKT-01M29CRJ4E adds it), so a forum-scoped moderator sees it. Applications and Users are listed for moderators and admins by role, and every section for admins. Until `queue` exists, Queue follows the role. Shared pieces live in `screens/admin/common/`: a table, a labelled field, a confirm button, and a send wrapper that turns `conflict`, `validation-failed` and `forbidden` into a sentence. Everything stays under `screens/admin/`, so it stays in the lazy chunk and out of the reader's budget (docs/07 "Preact client").

## Acceptance criteria

- [x] Every route registered under /api/v1/admin/ answers 401 to an anonymous viewer and 403 to a pre-user and to a plain member, enforced by TestAdminRoutesAreGated over the registered route list
- [x] internal/audit is the only caller of dbq.AppendAudit outside internal/store/db (TestOneWayIntoAuditLog), and perm.Promote and settings.SetBy write the same rows through it as before
- [x] docs/04 audit_log lists every target_type and the format of its target_id
- [ ] /admin lists the sections the viewer's role may use, and TestTheAdminBundleIsSeparate and the budget check pass with the reader total unchanged
- [x] A member granted moderate on one forum through a group passes the moderates gate and neither staff nor admin, and the admin section list shows them Queue when /me's queue is not null (TestModeratesGate, AdminRoot.test.tsx)
- [x] audit.System(via) writes actor_id NULL with via in the details, and no audit row is ever written with actor id 0 (TestSystemActorIsNull)

## Implementation plan

### Settled in grooming

- Admin routes live under `/api/v1/admin/*` and each is gated on the role docs/08's "Primitives" table names for its action (docs/07 "Moderation and admin").
- A viewer whose status is not active has no role (`session.Session.Viewer`), so the gate needs no status check of its own.
- `audit_log` is append-only by trigger and by `TestAuditLogAppendOnly` (docs/04 "audit_log").
- X-Forwarded-For is not consulted (docs/08 "The transport-level ceiling").
- The admin screens are one lazy chunk that the thread-view budget does not count, and `TestTheAdminBundleIsSeparate` holds it apart (docs/07 "Preact client", TKT-01M29CEE52). The built client in `internal/web/spa/dist` is committed: run `go generate ./internal/web/spa` after any client change.

Terva review 1349 (PR #140, head 90d2f7c) found that `staff` alone would shut out a forum-scoped moderator the queue serves, and that `By` could not express a system actor. Both are fixed above: the `moderates` gate, and `Actor` 0 as NULL with `audit.System`.

Engineering choices made here. A separate ticket rather than "whichever lands first builds it" in seven plans: the parallel run would build it more than once in PRs that conflict. Handles rather than ids in user routes, matching `/users/{handle}`; ids for forums and groups, which have no handle.

### Approach (revised at claim, 2026-09-29, against main 31f4a79)

1. `internal/web/api/admin.go`. The gates are methods on the handlers: `h.admin(w, r)`, `h.staff(w, r)` and `h.moderates(w, r)`, each returning whether it admitted the viewer and writing the refusal otherwise. Refusals follow `member.Require` (unauthenticated with no member session, an inactive account included; forbidden to a pre-user), then forbidden to a member the gate does not admit. `adminGate`, `staffGate` and `moderatesGate` name them for registration. `h.adminRoute(mux, pattern, gate, handler)` wraps the handler in its gate, records `{pattern, gate}` in `h.adminRoutes`, and panics on a pattern with no method, a path outside `/api/v1/admin/`, or no gate. `h.registerAdmin(mux)` is where each section's ticket adds its routes; it registers none yet. `api.Deps` gains a required `Perm *perm.Evaluator`, which `ketju serve` already has. `build(d)` is `New` returning the handlers too, for the walk.
2. `perm.ModeratesAny(ctx, v) (bool, error)`: false for a viewer who is not a member, true for a moderator or admin by role, otherwise whether `CanEach(v, every forum, moderate)` allows any. The answer is cached in the viewer's cache entry beside the forum decisions, under the same generation and TTL, so a warm call costs no statement and a revoke clears it.
3. `internal/audit`: `By{Actor, IP, Via}`, `System(via)`, `Target{Type, ID}`, the constructors `User`, `Forum`, `Group`, `Flag`, `Application`, `Thread`, `Post`, `Setting`, `ParsePost`, `Types`, and `Append(ctx, q *dbq.Queries, at time.Time, by, action, target, details map[string]any) (int64, error)`. `Append` writes `Actor` 0 as NULL, refuses a negative actor and an empty action, writes a non-empty `Via` as `"via"` over any `"via"` in details, and returns the row id (the queue's `audit_id` needs it). `perm.Promote` and `settings.SetBy` call it with no change to what they write; `settings.Change` gains `IP`, and `settings.ActionChanged` names its action.
4. `internal/web`: `clientIP` becomes `ClientIP`, unchanged.
5. docs/04 "audit_log": the rule that rows go through `internal/audit`, what `actor_id`, `via` and `ip` hold, and a marked table of `target_type` and `target_id` formats that `TestTargetTypesDocumented` holds to `audit.Types`.
6. Client: `screens/admin/sections.ts` is the section list and who may use each. Queue follows `/me`'s `queue` when `/me` carries it (a number or null) and the role until then; Applications and Users need moderator or admin by role; the rest need admin. `AdminRoot.tsx` renders the offered sections as a nav, marks the current one, and says so when the viewer may not use the section at the address or has none. `screens/admin/common/`: `Table`, `Field`, `ConfirmButton`, and `send`, which turns `conflict`, `validation-failed` and `forbidden` into a sentence. `admin.css` is lazy with the chunk. Run `go generate ./internal/web/spa` and commit `dist`.

### Tests

- `TestAdminRoutesAreGated`: registers one fixture route per gate through `adminRoute`, then walks `h.adminRoutes` through the real session middleware: 401 for anonymous and a suspended admin, 403 for a pre-user, a plain member, and any member the route's gate does not admit (a moderator on an admin route, a forum-scoped moderator on staff and admin routes), and the fixture handler's 204 for those it admits. A route a section adds later is walked without editing the test, and the test fails if any gate has no route.
- `TestModeratesGate`: a `moderators`-group member granted `moderate` on one forum passes `moderates` and fails `staff` and `admin`; after `Revoke`, fails `moderates` on the next request.
- `TestAdminRouteRefusesAPathOutsideAdmin` and `TestAdminPathsGoThroughAdminRoute` (a source scan of the package for a direct `Handle`/`HandleFunc` of an admin path).
- `TestModeratesAny`, `TestModeratesAnyFollowsGrants`, `TestModeratesAnyIsCached` in `internal/perm`.
- `TestSystemActorIsNull`, `TestAppendTakesTheCallersTime`, `TestViaIsTheActors`, `TestTargetFormats`, `TestTargetTypesDocumented`, `TestOneWayIntoAuditLog` in `internal/audit`.
- `TestPromoteUnchanged` compares Promote's rows column by column; the existing settings and CLI tests pin SetBy's; `TestSetByRecordsTheAddress` covers `IP`.
- `TestClientIPIgnoresForwardedFor`.
- `AdminRoot.test.tsx` (moderator, admin, member with a queue, member with a null queue, plain member, current section, a section the viewer may not use) and `common/common.test.tsx`. `TestTheAdminBundleIsSeparate` stays green.

### Out of scope

Each section's screen and API, which belong to the seven tickets named in the description. The application review routes are TKT-01M29CP2MG's, which may use the gate and the helper as well.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:41:12Z

Terva review 1349 (PR #140, head 90d2f7c) found two problems, both fixed in the description and plan. The staff gate shut out a forum-scoped moderator the queue serves: a moderates gate is added, and the Queue section follows /me's queue. By could not express a system actor: Actor 0 is written as NULL, and audit.System(via) records the via. The flag auto-hide in the moderation group records the system as its actor this way.

**agent:claude-code/phase2-run-nrycp5** at 2026-09-29T07:41:53Z

Build notes, and why AC4 is unticked.

AC4 is unticked. /admin lists the sections by role, TestTheAdminBundleIsSeparate passes, and the budget check passes, but the reader total is not unchanged. It moved from 38171 B to 38287 B gzip (+116 B of the 60000 B budget). None of the admin code is in the entry. The section list has to read the viewer, and the title goes to the phone top bar, so the lazy chunk imports `viewer` (state/viewer.ts) and `useShellContext` (state/shell.ts). The entry chunk must then export those two bindings. The minifier renames identifiers around the new exports, and gzip absorbs the difference as +116 B. Without the shell-context import it is still +110 B, so the viewer import alone costs almost all of it. Any admin screen that reads /me pays this once. No design that keeps the entry byte-identical lets the section list follow /me: every other route to the viewer goes through reader code (routes.tsx passing props), which costs more. A maintainer can reword the criterion to "within budget, with no admin module in the entry", or accept the 116 B.

Gaps settled as internals (docs/12 "Gaps the design leaves open"):
- `audit.Append` takes `at time.Time`. The column is milliseconds, and the projection writer passes `time.UnixMilli(frame.TimestampMS)`. Append returns the row id, because the queue plan (TKT-01M29CRJ4E) stores it in `flags.audit_id`.
- A non-empty `By.Via` is written as `"via"` and replaces a `"via"` key in details. What did the action is the actor's to say. Promote and SetBy pass no Via and keep their `"via"` in details, so their rows are unchanged (TestPromoteUnchanged, TestSetByDetails, cmd/ketju admin tests).
- `api.Deps.Perm` is required, like the services. `ketju serve` passes the process's one evaluator.
- `perm.ModeratesAny` caches its answer in the viewer's cache entry, beside the forum decisions, under the same generation and TTL. A warm call costs no statement, which the queue plan's `/me` count relies on. A grant or revoke clears it. A new forum is seen within TTL, since creating a forum clears nothing. That matters only for a global grant made before any forum existed.
- The gates wrap the handler inside `adminRoute`, rather than each handler calling its gate first, so a route cannot forget its gate. Gate names (`adminGate`, `staffGate`, `moderatesGate`) are data on the route list.
- Client: `/me`'s `queue` is read through a local type in screens/admin/sections.ts until TKT-01M29CRJ4E adds it to dto.Me. `undefined` means /me does not carry it yet, so the role decides.

Mutations tried, each caught: ModeratesAny not caching (TestModeratesAnyIsCached); Append writing actor 0 as a value (TestSystemActorIsNull and two others); staff admitting any member (TestAdminRoutesAreGated, TestModeratesGate); moderates by role only (TestModeratesGate); a direct AppendAudit in settings (TestOneWayIntoAuditLog); ClientIP reading X-Forwarded-For (TestClientIPIgnoresForwardedFor); Promote adding a details key (TestPromoteUnchanged); the Queue section ignoring /me's queue (AdminRoot.test.tsx).

Checks run before push: gofmt, go vet, go test -race ./..., client tsc, vitest (958 tests), go generate ./internal/web/spa, budget, and Playwright on port 5181 (53 passed, 9 skipped).

**agent:claude-code/phase2-run-nrycp5** at 2026-09-29T14:45:30Z

Terva review 1374 (PR #145, head 819e596, base 31f4a79, run 875). Both medium findings are fixed.

- Finding 1, "Pass the computed timestamp to the settings audit row": fixed. SetBy has no fallback time: its `at` is `c.At.UnixMilli()`, so the row already carried the same millisecond, and TestSetByDetails pins `at`. The audit call now passes `time.UnixMilli(at)`, the value the settings row gets, so the two cannot drift.
- Finding 2, "Handle a rejected confirmation action without dismissing it": fixed. A rejected `onConfirm` no longer closes ConfirmButton. The rejection is caught and shown beside the buttons in `send`'s words. An abort is ignored. An `onConfirm` that shows its own failure resolves to false, which also keeps the button open. Only success closes it. Tests: "stays open and says why when the action is refused" and "stays open when the action reports its own failure with false".

CI on 819e596 failed at "built client is current", which is not a code fault. The committed dist came from a measurement build I ran with the shell-context import removed, and I restored the source without rebuilding. The dist is regenerated from the current source in this commit.

## Summary

The admin API gates (admin, staff, moderates) and adminRoute are in internal/web/api/admin.go. Every /api/v1/admin/* route registers through h.adminRoute in h.registerAdmin, and TestAdminRoutesAreGated walks that list. perm.ModeratesAny answers from the viewer's permission cache. internal/audit is the one writer of audit_log: By, System, Target and its constructors, and Append. perm.Promote and settings.SetBy write through it with unchanged rows, settings.Change gained IP, and web.ClientIP is exported. docs/04 documents the target formats. The admin screen is a layout with a role-filtered section list, and screens/admin/common holds Table, Field, ConfirmButton and send. AC4 is unticked: the reader bundle grew 116 B gzip (38171 to 38287 of 60000), because the lazy chunk imports the viewer signal from the entry. The build note says why.
