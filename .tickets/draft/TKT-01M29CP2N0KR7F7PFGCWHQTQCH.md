---
schema: 3
id: TKT-01M29CP2N0KR7F7PFGCWHQTQCH
title: Notify moderators and applicants about applications
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:identity
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BKBM38VTYY4Y91018GTH6
origin: null
dependencies:
  - TKT-01M29BYYHQ7PTYNYA3VBBD4PDB
  - TKT-01M29CKRDQHB528ZW1FFBN4SA1
  - TKT-01M29CP2KZPR0880MGFV3CPXR4
  - TKT-01M29CP2MGC6FD35PJHR2Y2WNA
blocks_on: none
references:
  - ref: design:approval-mode
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-29T05:08:42Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

The `new_application` notification to everyone in moderators and admins, and `application_decided` to the applicant.

In-app only. docs/10 keeps SMTP out of v1, which means an applicant learns the decision by returning to the status page, and the notification is what a moderator sees when they next open the site. That is a real limitation worth stating in the admin UI rather than discovering.

## Acceptance criteria

- [ ] Submitting an application writes one new_application row naming it for each active user with role moderator or admin, in the submission's transaction, and no row for anyone else (tested against a member, a suspended moderator and a moderators-group member without the role)
- [ ] The applicant's decision shows on GET /apply/status for both outcomes, and an approved applicant also gets one application_decided row as their new user, written in the approval's transaction
- [ ] A new_application row counts in each recipient's GET /me notifications, so the Alerts badge shows it in both layouts; the queue's own badge is TKT-01M29CRJ4's
- [ ] When an application is approved, denied or withdrawn, its new_application rows are marked read in the same transaction
- [ ] No email is sent or required: a test fails on any net/smtp import under internal/ or cmd/, and no mail setting is added

## Implementation plan

Groomed 2026-09-29 for the Phase 2 autonomous run (TKT-01M3M9RR26), against main at 7af2c5a.

What exists there:
- `notifications` exists (migration 0005) with the kinds `new_application` and `application_decided`. TKT-01M29CKRDQ adds `application_id`.
- Nothing writes a notification yet. The post notification generator is Phase 3's TKT-01M29CRJ2T, and the list screen is Phase 3's TKT-01M29CTY86.
- `GET /me`'s `notifications` counts unread rows (`MeCounts` in `internal/store/db/queries/viewer_state.sql`), and the shell's Alerts badge shows it in both layouts (TKT-01M29CEE4K).

### Settled in grooming

- **`new_application` goes to the moderators and admins, and `application_decided` goes to the applicant** (docs/05 step 3, docs/04 "notifications").
- **In-app only, with no email** (docs/10 item 12, this ticket's description). docs/05 step 3's "optionally email later" stays later.
- **An applicant learns the decision by returning to the status page** (this ticket's description). The status page is TKT-01M29CP2KZ's `GET /apply/status`, which reads the application row, so it shows both outcomes.
- **Recipients are active users with role `moderator` or `admin`.** docs/05 step 3 writes "everyone in `moderators`+`admins`", which names the system groups, while review is gated on the role (docs/08 "Primitives", docs/07 "Moderation and admin"), and docs/04 says the role "implies nothing about the `moderators` group". A member of the `moderators` group without the role cannot open `/admin/applications`, so notifying them sends them to a 403. Settled on the role, the predicate that gates the work. docs/05 step 3 is corrected in this ticket's PR.
- **The rows are written in the transaction that causes them**: the submission, the decision, or the withdrawal. The event bus lost because it carries post appends, whose catch-up replays thread logs. A database-only event lost between commit and publish would never be replayed.

Engineering decisions taken here, with the alternative that lost:
- **A denied applicant gets no `notifications` row.** They have no `users` row, and `notifications.user_id` is NOT NULL, so the status page is their whole notification. An approved applicant gets an `application_decided` row as their new user, which they see in Alerts after signing in.
- **When an application leaves pending, its `new_application` rows are marked read** in the same transaction. Until Phase 3's list exists, nothing else can clear them, and the Alerts badge would count decided applications forever. Deleting them lost, because the Phase 3 list should still show that an application arrived.
- **The rows are written by `internal/service/notify`** (docs/02 lists `service/notify/`), as functions taking a `*dbq.Queries`, so each caller runs them in its own transaction. TKT-01M29CRJ2T's post generator joins the same package later.

### Approach

1. `internal/store/db/queries/notifications.sql`, regenerated into dbq:
   - `NotifyStaffOfApplication`: `INSERT INTO notifications (user_id, kind, application_id, at) SELECT id, 'new_application', ?, ? FROM users WHERE status = 'active' AND role IN ('moderator', 'admin')`, one statement.
   - `NotifyApplicationDecided`: one row with kind application_decided, the user_id, application_id and actor_id.
   - `SettleApplicationNotifications`: UPDATE … SET read_at = ? WHERE application_id = ? AND kind = 'new_application' AND read_at IS NULL.
2. `internal/service/notify/applications.go`: `NewApplication(ctx, q, appID, now)`, `ApplicationDecided(ctx, q, userID, appID, actorID, now)` and `SettleApplication(ctx, q, appID, now)`.
3. The calls, added inside the existing transactions:
   - `applications.Submit` (TKT-01M29CP2KZ) calls `NewApplication`.
   - `applications.Approve` (TKT-01M29CP2MG) calls `ApplicationDecided` for the new user with the moderator as actor, then `SettleApplication`.
   - `applications.Deny` and `applications.Withdraw` call `SettleApplication`.
4. No client change is needed for the badge. The Alerts badge already reads `GET /me`'s `notifications`. The admin screen's in-app-only statement is TKT-01M3NRX2NN's.
5. docs/05 step 3: recipients by role, and the rows are written in the submitting transaction. docs/04 "notifications": the `application_id` use, and the settle rule.

### Tests

- `internal/service/notify/applications_test.go` and `internal/service/applications`:
  - `TestEveryStaffMemberIsNotified`: the users are an admin, a moderator, a member, a suspended moderator, an agent moderator, and a `moderators`-group member without the role. A submission writes exactly one `new_application` row each for the admin, the moderator and the agent moderator, all naming the application, and no row for anyone else.
  - `TestNotificationFailureFailsTheSubmission`: a test trigger refusing the insert leaves no application row.
  - `TestDecisionSettlesNewApplication`: after approve, deny, or withdraw, every recipient's row has read_at set, and a moderator's `GET /me` `notifications` drops back.
  - `TestApprovedApplicantIsNotified`: one `application_decided` row for the new user, with actor set to the moderator and the application_id. After signing in, the user's `GET /me` `notifications` is 1.
  - `TestDeniedApplicantSeesTheStatus`: `GET /apply/status` answers denied with decided_at, and no `application_decided` row exists.
- `internal/web/api/me_test.go`: `TestNewApplicationCountsInMe` shows a moderator's count rises by one per submission.
- `TestNoMailIsSent`: a scan of non-test Go under `internal/` and `cmd/` fails on an import of `net/smtp`.

### Out of scope

- The notification list, marking read, and the Alerts screen: TKT-01M29CTY86 (Phase 3).
- The post notification kinds: TKT-01M29CRJ2T (Phase 3).
- The moderator queue's own count badge: TKT-01M29CRJ4.
- Email: docs/10 item 12, a later phase.
