---
schema: 3
id: TKT-01M29CP2N0KR7F7PFGCWHQTQCH
title: Notify moderators and applicants about applications
type: task
status: ready
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
updated_at: 2026-09-29T07:18:12Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
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
- [ ] No email is sent or required: a test fails on any net/smtp import under internal/ or cmd/, and no mail setting is added
- [ ] Closing an application (approve, deny or withdraw) never writes read_at; GET /me's notifications counts a new_application row only while its application is pending, so the badge drops while the row stays unread, tested for all three closings and for other kinds

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
- **Closing an application never touches `read_at`; the unread count leaves out `new_application` rows whose application is no longer pending** (review 1364). `read_at` means that a recipient read the notification. Deciding or withdrawing an application is not that, so writing `read_at` then would record a read that never happened. The count in `MeCounts`, which `GET /me`'s `notifications` and so the Alerts badge read, counts a `new_application` row only while its application is pending. Two alternatives lost. Marking the rows read at decision time, as the first plan did, falsifies the read state. Leaving the count alone would let the badge count decided applications forever, because nothing can mark them read until Phase 3's list exists. Deleting the rows lost too, because the Phase 3 list should still show that an application arrived.
- **The rows are written by `internal/service/notify`** (docs/02 lists `service/notify/`), as functions taking a `*dbq.Queries`, so each caller runs them in its own transaction. TKT-01M29CRJ2T's post generator joins the same package later.

### Approach

1. `internal/store/db/queries/notifications.sql`, regenerated into dbq:
   - `NotifyStaffOfApplication`: `INSERT INTO notifications (user_id, kind, application_id, at) SELECT id, 'new_application', ?, ? FROM users WHERE status = 'active' AND role IN ('moderator', 'admin')`, one statement.
   - `NotifyApplicationDecided`: one row with kind application_decided, the user_id, application_id and actor_id.
   - `MeCounts` (in `viewer_state.sql`): its `notifications` subquery becomes a count of unread rows `LEFT JOIN applications AS a ON a.id = notifications.application_id`, keeping a row when `notifications.kind <> 'new_application' OR a.status = 'pending'`. The `notifications_application` index from TKT-01M29CKRDQ and the primary key of `applications` keep the join cheap. `mecounts_plan_test.go` is extended so the plan still uses `notifications_user` and reads no table in full.
2. `internal/service/notify/applications.go`: `NewApplication(ctx, q, appID, now)`, and `ApplicationDecided(ctx, q, userID, appID, actorID, now)`. There is no settle function: closing an application writes nothing to `notifications`.
3. The calls, added inside the existing transactions:
   - `applications.Submit` (TKT-01M29CP2KZ) calls `NewApplication`.
   - `applications.Approve` (TKT-01M29CP2MG) calls `ApplicationDecided` for the new user, with the moderator as actor.
   - `applications.Deny` and `applications.Withdraw` write no notification.
4. No client change is needed for the badge. The Alerts badge already reads `GET /me`'s `notifications`. The admin screen's in-app-only statement is TKT-01M3NRX2NN's.
5. docs/05 step 3: recipients by role, and the rows are written in the submitting transaction. docs/04 "notifications": the `application_id` use, and that a `new_application` row counts as unread only while its application is pending, with `read_at` left alone.

### Tests

- `internal/service/notify/applications_test.go` and `internal/service/applications`:
  - `TestEveryStaffMemberIsNotified`: the users are an admin, a moderator, a member, a suspended moderator, an agent moderator, and a `moderators`-group member without the role. A submission writes exactly one `new_application` row each for the admin, the moderator and the agent moderator, all naming the application, and no row for anyone else.
  - `TestNotificationFailureFailsTheSubmission`: a test trigger refusing the insert leaves no application row.
  - `TestClosedApplicationLeavesTheCount`: after approve, deny, or withdraw, every recipient's `new_application` row still has read_at NULL, and a moderator's `GET /me` `notifications` drops back by one. Another pending application still counts. A `reply_to_you` row with no application still counts. A row the moderator marked read stays read.
  - `TestApprovedApplicantIsNotified`: one `application_decided` row for the new user, with actor set to the moderator and the application_id. After signing in, the user's `GET /me` `notifications` is 1.
  - `TestDeniedApplicantSeesTheStatus`: `GET /apply/status` answers denied with decided_at, and no `application_decided` row exists.
- `internal/web/api/me_test.go`: `TestNewApplicationCountsInMe` shows a moderator's count rises by one per submission.
- `TestNoMailIsSent`: a scan of non-test Go under `internal/` and `cmd/` fails on an import of `net/smtp`.

### Out of scope

- The notification list, marking read, and the Alerts screen: TKT-01M29CTY86 (Phase 3).
- The post notification kinds: TKT-01M29CRJ2T (Phase 3).
- The moderator queue's own count badge: TKT-01M29CRJ4.
- Email: docs/10 item 12, a later phase.
