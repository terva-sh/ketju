---
schema: 3
id: TKT-01M29CYSCPQJ5ZZHCFDGV1S4TF
title: Implement thread tags
type: task
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area:moderation
  - effort:S
assignees: []
milestone: phase-4
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CKRH92JGZ1WY58JXC4T2G
  - TKT-01M3B12ZZ403NEPFH3DASEW1D8
blocks_on: none
references:
  - ref: design:storage-records
    path: docs/03-thread-storage.md
  - ref: pr:361
    path: null
  - ref: pr:372
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:31:00Z
updated_at: 2026-10-02T20:30:46Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase4-run-cyscp
  name: ""
extensions: {}
---

## Description

Tags on threads, set at creation and changed by moderators, shown as chips in thread lists and filterable.

The `ThreadTagged` record replaces the whole set rather than adding or removing one, which keeps the log record self-contained and makes a rebuild trivial. That shape is already in the format and this ticket honors it rather than working around it.

Whether members can tag their own threads is the open call here.

## Acceptance criteria

- [x] PUT /admin/threads/{id}/tags, behind the moderates gate with a row in adminRouteGates, writes one ThreadTagged replacing the whole set, and the header, the list and a reindex agree (TestRetag, TestRetagWritesFullReplacement)
- [x] A retag is refused in docs/07 order, writing nothing: a member or the author without moderate, a moderator of another forum, an archived thread to moderators and admins, an invalid tag, and the set the thread already has (TestRetagRefusals, TestRetagDecidesAfterAMove)
- [x] A group-granted moderator may retag until the grant is revoked, and a retag in an archived forum is allowed (TestRetagByGroupModerator, TestRetagInArchivedForum)
- [x] Every retag writes thread.retagged with no tag text, listed in audit.Actions and docs/04, and moves the response cache key (TestRetagAudited, TestRetagMovesTheResponseCacheKey)
- [x] GET /forums/{slug}/threads?tag= lists only threads carrying the folded tag, pins included, with the tag bound into the cursor, and answers a hidden or unreadable forum as the unfiltered list does (TestThreadListTagFilter, TestTagFilterNeverWidensAccess, TestPlansSeek)
- [x] The vocabulary is free text as docs/04 thread_tags decides, and docs/07 documents the retag route and the tag filter

## Implementation plan

### Settled in grooming

- **Rules.** A tag is what docs/04 `thread_tags` says, checked by the code TKT-01M3B12ZZ4 adds (`domain.NormalizeTag`, `NormalizeTags`), which this ticket reuses and does not copy. The rules are free text, at most 8 tags per thread, folded, de-duplicated and sorted (maintainer, 2026-10-02; run decisions pending maintainer review). This answers the description's "open call": members tag their own threads on create, and only a moderator retags (docs/08 "Thread tags").
- **Who retags.** Anyone holding `moderate` in the thread's forum, by role or through a group, behind the `moderates` gate. A retag may set or remove a reserved tag. The thread's author may not retag (docs/08 "Thread tags", docs/07 "Moderation and admin", run decision pending maintainer review).
- **Record.** A retag writes one `ThreadTagged` (16) whose `tags[]` replaces the whole set (docs/03). The log format does not change, and the projection's `applyTagged` and `replaceTags` already apply it.
- **Archived.** Retagging is refused on an archived thread and allowed in an archived forum, as locking is (docs/08 "Thread tags"; docs/07 "Locking and pinning threads").
- **Filter.** One tag at a time, `GET /forums/{slug}/threads?tag=TAG`. A filtered list shows only threads carrying the tag, pins included (docs/06 "Thread list views", run decision pending maintainer review).
- **Audit.** Every retag is a moderation action, and docs/08's opening says every moderation action appends to `audit_log`. The projection writes `thread.retagged` against the thread, as it writes `thread.moved`. The row carries no tag text, following the rename row's precedent (internal/events/projection/audit.go `ActionThreadRenamed`, maintainer decision 2026-09-29 on TKT-01M3NS147C): the log is the record of what the tags were, and `audit_log` is append-only. Alternatives: `{"from", "to"}` tag lists lost because they copy member-written text into a table nothing can redact; no row at all lost to docs/08's "every action ... appends to `audit_log`".
- **Vocabulary.** Free, not constrained by a setting (maintainer, 2026-10-02, question 3 (c)). The original criterion on the vocabulary is answered by docs/04, not by a setting.
- **The client is a separate ticket.** Chips, the filtered list, the new thread field and the moderator's panel are TKT-01M3YSQPZX (Show, filter and set thread tags in the client). They were split out so this server work does not queue for the committed dist (docs/15 "Order").

### Revised at claim

The approach below stands as groomed, with these refinements found while
building it:

- `tags: null` in a retag body is `bad-request`, as an absent `tags` is,
  because a retag must name the new set; `[]` is how every tag is removed.
- The cursor gains its sixth field only when the list is filtered, so an
  unfiltered cursor is byte-for-byte what it was and old cursors still
  decode.
- `TestRenameAndMoveWriteNoTags` (TKT-01M29CKRH9) asserted that no tag route
  is served. It now allows exactly the retag route; its record-type check,
  that a rename or move writes no `ThreadTagged`, is unchanged.
- The mid-decision hook in `rename_test.go` (`moveMidDecision`) gains a
  retag case, which is how `TestRetagDecidesAfterAMove` lands a move between
  the decision and the append.

### Approach

1. **Service: `Retag`.** Add `func (s *Service) Retag(ctx, v, id, tags []string) (dto.ThreadHeader, error)` to `internal/service/threads/moderate.go`, built on `s.moderate` (the lock/pin path, which decides through `appendDecided` at a log length, so a move landing in between makes it decide again). Inside `decide`:
   - `moderate` already refuses forbidden without `a.canModerate`, and conflict on `th.Archived()`.
   - `NormalizeTags(tags)` refuses with `validation-failed` and its `errors`. No reserved check: the retagger moderates the forum.
   - The normalized list equal to the thread's current tags (`q.ThreadTags`, already sorted) is conflict, so the log keeps no record that changes nothing. Read it in the same `access` read, or with one query beside it.
   - Append `threadlog.Append{Type: TypeThreadTagged, Body: &ThreadTaggedBody{Tags: normalized}}`. An empty list is a valid retag that removes every tag. Because the struct has `omitempty`, an empty list encodes as an absent key, which decodes to an empty set, and `replaceTags` clears the rows.
2. **Route.** Register `PUT /api/v1/admin/threads/{id}/tags` with `{tags}` in `internal/web/api/moderate.go`, using `h.adminRoute(..., moderatesGate, h.retagThread)`:
   - The body is decoded strictly and bounded at 4 KiB, as lock's is. A body without `tags` is `bad-request`. `tags` elements are decoded as TKT-01M3B12ZZ4 decodes them (`wrong_type` for a non-string).
   - It answers 200 with `ThreadHeader`, `no-store`, and is charged to the per-address write limiter as the other thread routes are.
   - Add the row `"PUT /api/v1/admin/threads/{id}/tags": "moderates"` to `adminRouteGates` (internal/web/api/admin_test.go). `TestThreadModerationRoutesUseTheModeratesGate` counts the routes on the gate, so its expected count rises by one.
   - Add a "Retagging" table and refusal list to docs/07 under "Renaming and moving threads", in lock's refusal order.
3. **Audit.** In `internal/events/projection/audit.go` `auditFor`, add `case *threadlog.ThreadTaggedBody` returning `ActionThreadRetagged` (`"thread.retagged"`) with `map[string]any{}`. `ThreadCreated` has its own body type, so tags set on create write no row. Update the "never" comment, which names `ThreadTagged`. Add `"thread.retagged"` to `audit.Actions` and a row to docs/04's audit table (`TestActionsDocumented`, `TestActionsCoverTheWriters`).
4. **Filter.**
   - Add `Tag string` to `threads.ListQuery`, read from `q.Get("tag")` in `internal/web/api/forums.go`.
   - In `List`, when `Tag` is set, run it through `NormalizeTag`. One that cannot be a tag is `bad-request`, as an unknown `sort` is.
   - Add `Tag string` to `views.ListParams` and a `tag` argument to `views.PinnedThreads`. When it is set, both queries add `AND EXISTS (SELECT 1 FROM thread_tags g WHERE g.thread_id = t.id AND g.tag = ?)`. That probes `thread_tags`'s primary key `(thread_id, tag)` per row while the existing forum indexes drive the scan, so no migration is needed.
   - Bind the tag into the cursor: `cursor.encode` adds a sixth field, and `decodeCursor` refuses a cursor made for another tag (`errBadCursor`). The cursor stays opaque, and the pinned-ids rule is unchanged.
   - Update docs/07's `GET /forums/{slug}/threads` row.
5. **Docs.** Add `PUT /admin/threads/{id}/tags` to the "Moderation and admin" prose, and add the retag action to docs/08's Primitives sentence on the forum grant, citing "Thread tags".

### Tests

- `TestRetag` (internal/web/api, a new `retag_test.go`, fixtures by `dbtest.Open`):
  - A role moderator sets `["Garden","agent"]`. The answer's `tags` is `["agent","garden"]`.
  - The log ends in one `ThreadTagged` with that body.
  - `GET /threads/{id}` and the forum list agree.
  - Setting `[]` clears the tags, and a reindex leaves them cleared.
- `TestRetagWritesFullReplacement`: retagging `[a,b]` then `[b,c]` leaves exactly `[b,c]` in `thread_tags`, and the second record's body is `[b,c]`, not a delta. This is the original criterion on a full replacement record.
- `TestRetagRefusals`, in docs/07's order, each asserting that the log length is unchanged and no audit row is written:
  - anonymous is `unauthenticated`;
  - a member, and the thread's own author without `moderate`, are `forbidden`;
  - a body without `tags` is `bad-request`;
  - a thread hidden from the viewer is `not-found`;
  - a moderator of another forum only is `forbidden`;
  - an archived thread is `conflict` to a moderator and to an admin;
  - an invalid tag is `validation-failed` with `errors`;
  - the set the thread already has, in another order or case, is `conflict`.
- `TestRetagByGroupModerator`: a member holding `moderate` in the forum through a group may retag. Once the grant is revoked, the next request is `forbidden`, which shows that the decision is `perm`'s and not the session's.
- `TestRetagInArchivedForum`: a moderator may retag a thread in an archived forum.
- `TestRetagDecidesAfterAMove`: a move into a forum the moderator does not moderate lands between the decision and the append (the mid-decision hook `TestAMoveLandingMidDecisionIsJudged` uses, internal/web/api/rename_test.go), and the retag is `forbidden`, not appended.
- `TestRetagAudited`: a retag writes one `thread.retagged` row with the moderator as actor and details carrying no tag text. A `ThreadCreated` with tags writes none.
- `TestRetagMovesTheResponseCacheKey`: the thread page's cached response before a retag is not served after it, because the log length is in the key.
- `TestThreadListTagFilter` (internal/web/api):
  - `?tag=garden` lists only the forum's threads carrying `garden`, and `?tag=Garden` gives the same list.
  - Pins without the tag are left out, and pins with it lead.
  - A malformed tag is `bad-request`.
  - Paging through 30 tagged threads gives each once, and a cursor from `?tag=a` used with `?tag=b` is `bad-request`.
- `TestTagFilterNeverWidensAccess`:
  - `?tag=` on a hidden forum is `not-found`, and on a members-only forum it is `unauthenticated` to an anonymous viewer, as the unfiltered list answers.
  - A global pin carrying the tag from a forum the viewer cannot read is not listed.
- `TestPlansSeek` (internal/store/db/views): add the filtered list and the filtered pins. The plan uses the forum index and `thread_tags`'s primary key, with no full scan of `thread_tags`.
- `TestKeysetSurvivesBumps`: add a filtered variant.
- Invariants:
  - The session chokepoint is unchanged (`TestOneDoorIntoSessions`).
  - `adminRouteGates` has the new row (`TestAdminRoutesAreGated`).
  - No role or status change.
  - The log format is unchanged: no new type, and `TestEveryRecordTypeHasADecision` passes as is.

### Out of scope

- The tag rules on create: TKT-01M3B12ZZ4 (Define thread tag rules so new threads can carry tags), which this ticket depends on.
- Chips, the filtered list screen, the Tags field and the "Edit tags" panel, together with the original criteria on chips and on the phone hit area: TKT-01M3YSQPZX (Show, filter and set thread tags in the client).
- A per-forum tag vocabulary, filtering several tags at once, cross-forum tag lists, and searching by tag: no ticket. The maintainer left the vocabulary for later, and the others are not docs/09 deliverables.
- Keeping agent threads out of a human's unread list by tag (docs/06 "Live updates"): no ticket.

## Notes

**agent:claude-code/phase4-groom-tags-archive** at 2026-10-02T17:18:01Z

run decision: pending maintainer review. Phase 4 grooming (tags and archive lane) settled these. Each is written into a design document on branch groom/p4-tags-archive:
- A retag follows the forum grant: anyone holding `moderate` in the thread's forum, by role or group. It is behind the `moderates` gate, and the author may not retag (docs/08 "Thread tags", docs/07 "Moderation and admin").
- The filter takes one tag at a time, `/f/{slug}?tag=TAG`. A filtered list shows only threads carrying the tag, pins included (docs/06 "Thread list views").

Grooming split the client half, the chips, the filtered list screen, the Tags field and "Edit tags", into TKT-01M3YSQPZX (Show, filter and set thread tags in the client), so that this server work does not queue for the committed dist. The original criteria on chips and on the phone hit area moved there.

## Summary

Landed in one PR, server side only. The client half is TKT-01M3YSQPZX
(Show, filter and set thread tags in the client).

- `PUT /api/v1/admin/threads/{id}/tags` `{tags}` sits behind the
  `moderates` gate and has its row in `adminRouteGates`. `threads.Retag`
  decides through `s.moderate`, on the log at a length, so a move landing
  between the decision and the append makes it decide again. It normalizes
  with `NormalizeTags` and refuses the set the thread already has as
  `conflict`. It appends one `ThreadTagged` holding the whole new set, and
  `[]` clears every tag. `tags` absent or null is `bad-request`, and a
  non-string tag is `wrong_type`, as on create.
- The projection audits every retag as `thread.retagged` with `{}`. That
  action is in `audit.Actions` and in docs/04. Tags set on create write no
  audit row.
- `GET /forums/{slug}/threads?tag=` takes one tag, folded by
  `domain.NormalizeTag`. A malformed tag is `bad-request`. Both the list
  query and the pins query add an `EXISTS` probe on `thread_tags`'s primary
  key, and `TestPlansSeek` checks that the plan uses it. The cursor carries
  the tag as a sixth field, only when the list is filtered, and a cursor
  made for another tag is refused.
- docs/07 has a new "Retagging threads" section and the `?tag=` row. docs/08
  says the forum grant covers a retag.
- `TestRenameAndMoveWriteNoTags` now allows exactly the retag route.

Run decisions are the groomed ones, pending maintainer review: a retag
follows the forum grant, and the filtered list shows only tagged pins.
