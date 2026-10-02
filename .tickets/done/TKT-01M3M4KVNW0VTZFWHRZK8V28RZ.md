---
schema: 3
id: TKT-01M3M4KVNW0VTZFWHRZK8V28RZ
title: Decide what a thread tag may be and who may set one
type: spike
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:api
  - effort:S
  - decision
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T13:52:34Z
updated_at: 2026-10-02T17:16:32Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase4-groom-tags-archive
  name: ""
extensions: {}
---

## Description

TKT-01M3B12ZZ4 (Define thread tag rules so new threads can carry tags) is blocked on the rules themselves. docs/03, docs/04, docs/06, docs/07 and docs/08 name tags but do not say what a tag may be or who may set one. docs/12 "Gaps the design leaves open" says a rule that changes what a member can do goes to a person. The answers also outlive the release, because a ThreadCreated record keeps its tags in the log forever.

Settled already, and not reopened here:
- The log format does not change. ThreadCreated already carries `tags[]` and ThreadTagged (type 16) replaces the set (docs/03). The rules restrict what the service writes, not what the reader accepts.
- An agent working thread is marked by a tag, and a human filters by tag from the forum row (docs/06, docs/10 item 33).
- Retagging an existing thread is a moderator action (docs/08). This decision is about tags set when a thread is created.

### 0. Do tags on create belong in Phase 1?

docs/09 lists "thread tags" under Phase 4, beside slow mode and archive. The rest of Phase 1 works without them: the create endpoint refuses a non-empty `tags` today, and a thread list shows whatever tags a thread has.

Options:
- (a) Phase 1: accept tags on create under the rules below.
- (b) Phase 4: keep the refusal, and move TKT-01M3B12ZZ4 to Phase 4 with retag and tag filtering.

### 1. What characters may a tag contain, and what about case?

`thread_tags` is keyed on the exact string, so the answer also decides what counts as the same tag for a later filter. The e2e seed's tags (`garden`, `long-read`, and others) fit option (a).

Options:
- (a) Lowercase ASCII words joined by single hyphens, `[a-z0-9]+(-[a-z0-9]+)*`. Uppercase input is folded to lowercase.
- (b) The same pattern, but uppercase is refused rather than folded.
- (c) Any printable Unicode text, trimmed. Case is kept for display and ignored when comparing.

### 2. How long may a tag be, and how many may a thread have?

The e2e seed gives one thread six tags, to test a 390 px row. A limit under six makes that seed an example of a thread the API could not create. The seed writes the log directly, so it would still work.

Options:
- (a) 1–32 characters, at most 5 tags.
- (b) 1–24 characters, at most 8 tags.
- (c) Other limits, stated.

### 3. Free text, or a vocabulary each forum keeps?

Options:
- (a) Free text. Any tag that passes question 1 is accepted.
- (b) A list per forum that moderators curate. Only a tag on the list is accepted. This needs a table, a migration and an admin surface, none of which exist.
- (c) Free text now. A moderator corrects a thread with a retag (docs/08), and a curated list can come later.

### 4. Who may set tags when creating a thread?

Options:
- (a) Anyone who may create a thread in the forum (`create_thread`).
- (b) Moderators only. A member's thread starts untagged, so an agent working thread needs a moderator to mark it.
- (c) Anyone with `create_thread`, except for reserved tags such as `agent`, which only a moderator or an agent account may set. The reserved list would need a home, either in docs or in configuration.

### Smaller points to confirm with the above

- A request that repeats a tag, or repeats one after case folding: refuse it with `validation-failed`, or keep one copy?
- The log keeps the order the author gave. The projection and the API list tags sorted (`ORDER BY tag`). Is sorted the order a member should see?

### Done when

The answers are written into docs/04 (beside `thread_tags`) or docs/08, and into docs/07's `POST /forums/{slug}/threads` row. TKT-01M3B12ZZ4's Implementation plan carries them under `### Settled in grooming`, and the ticket moves back to ready.

## Acceptance criteria

- [x] Whether tags on create are Phase 1 or Phase 4 is decided
- [x] Tag syntax, case handling and limits are decided and written into docs/04 or docs/08
- [x] Whether tags are free text or a per-forum vocabulary, and who may set them on create, is decided and written into docs
- [x] docs/07's POST /forums/{slug}/threads row states the rules
- [x] TKT-01M3B12ZZ4's settled items carry the answers

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T15:01:16Z

Maintainer decision, 2026-09-28: tags on create are deferred to Phase 4 ("Defer to Phase 4"). docs/09 lists thread tags under Phase 4 beside retag and tag filtering. Until then the create endpoint keeps refusing a non-empty `tags`. This ticket and TKT-01M3M4KVNW (Decide what a thread tag may be and who may set one) move to the phase-4 milestone. The rule questions in TKT-01M3M4KVNW stay open, to be decided with the rest of the tag feature.

**agent:claude-code/phase4-run-orchestrator** at 2026-10-02T15:42:09Z

Maintainer decision, 2026-10-02. Asked each question as a choice, the maintainer answered:
- **Question 0, Phase 1 or Phase 4:** moot. Tags are built in the Phase 4 run (docs/15).
- **Question 1, characters and case:** (a). A tag is lowercase ASCII words joined by single hyphens, `[a-z0-9]+(-[a-z0-9]+)*`, and uppercase input is folded to lowercase.
- **Question 2, limits:** (b). A tag is 1 to 24 characters, and a thread has at most 8 tags. The e2e seed's six-tag thread stays creatable through the API.
- **Question 3, vocabulary:** (c). Tags are free text now, and a moderator corrects a thread with a retag (docs/08). A curated list may come later.
- **Question 4, who sets tags at creation:** (c). Anyone with `create_thread` may set them, except reserved tags such as `agent`, which only a moderator or an agent account may set. The reserved list lives in docs/08.

The smaller points were not put to the maintainer. They are proposed as run decisions pending maintainer review:
- A request that repeats a tag, before or after case folding, keeps one copy and is not refused. This matches how a post keeps a repeated attachment hash once.
- Members see tags in sorted order, as the projection and the API already list them.

What remains of this spike is the "Done when" work: writing the answers into docs/04, docs/07 and docs/08, and into TKT-01M3B12ZZ4's plan. That work belongs to the Phase 4 tags grooming lane.

## Summary

The maintainer answered every question on 2026-10-02, and the answers are written into the design documents on branch groom/p4-tags-archive:
- docs/04 `thread_tags` gives the syntax `[a-z0-9]+(-[a-z0-9]+)*`, with uppercase folded to lowercase. A tag is 1 to 24 characters and a thread carries at most 8. Tags are free text, and a moderator corrects them with a retag.
- docs/08 "Thread tags" says who sets tags: anyone with `create_thread`, except reserved tags, which only a moderator or an agent account may set. The reserved list is `agent`, between `reserved-tags` markers that a test will read.
- docs/07's `POST /forums/{slug}/threads` row states the rules and the refusals.
- Question 0 (Phase 1 or Phase 4) was settled for Phase 4 on 2026-09-28.

Each is marked "Decided by the maintainer (2026-10-02)".

The two smaller points are run decisions pending maintainer review, in docs/04 `thread_tags`: a repeated tag keeps one copy, and members see tags sorted. The lane added three more:
- "Moderator" means `moderate` in the forum, by role or group, for reserved tags and for retags.
- Only ASCII is folded.
- A refused tag is reported as a field error, with the new codes `invalid_tag` and `reserved_tag`.

TKT-01M3B12ZZ4 (Define thread tag rules so new threads can carry tags) carries the answers under `### Settled in grooming` and is back in ready.
