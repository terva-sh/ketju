---
schema: 3
id: TKT-01M3B12ZZ403NEPFH3DASEW1D8
title: Define thread tag rules so new threads can carry tags
type: task
status: blocked
status_reason: Tag syntax, limits, vocabulary, who may set tags, and whether tags on create are Phase 1 are undecided; see TKT-01M3M4KVNW
priority: normal
due_on: null
labels:
  - area:api
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies:
  - TKT-01M3M4KVNW0VTZFWHRZK8V28RZ
blocks_on: none
references:
  - ref: pr:111
    path: null
claim: null
archive: null
created_at: 2026-09-25T00:57:46Z
updated_at: 2026-09-28T15:01:16Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

`POST /forums/{slug}/threads` (TKT-01M29CA9B9) refuses any tag with `validation-failed`. Nothing documents what a tag may be, and a ThreadCreated record keeps its tags in the log forever. docs/03 names tags in ThreadCreated and ThreadTagged, and docs/06 relies on a tag to mark agent threads, but neither says:

- the characters a tag may contain, and its case
- its length, and how many tags a thread may have
- whether tags are free text or chosen from a vocabulary each forum keeps
- who may set tags when a thread is created

Decide these in docs/04 or docs/08. Then accept tags in `CreateThread` (internal/service/threads/create.go) and remove the refusal.

## Acceptance criteria

- [ ] docs name the tag syntax, limits and who may set them
- [ ] POST /forums/{slug}/threads accepts tags that follow the rules and refuses others with validation-failed

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T13:52:45Z

Blocked at planning, before any code. The design does not settle the tag rules, and each open rule changes what a member can do or what a log keeps forever. docs/12 "Gaps the design leaves open" puts those with a person. TKT-01M3M4KVNW (Decide what a thread tag may be and who may set one) lists the questions with options:

0. docs/09 puts "thread tags" in Phase 4, so it is not clear that tags on create belong in Phase 1 at all.
1. The characters a tag may contain, and whether case is folded, refused or kept.
2. Its length, and how many tags a thread may have.
3. Free text, or a vocabulary each forum keeps.
4. Who may set tags on create: anyone with `create_thread`, moderators only, or anyone except for reserved tags such as `agent`.

Facts found while reading, for whoever picks this up:
- No log format change is needed. `threadlog.ThreadCreatedBody` and `ThreadTaggedBody` already carry `Tags []string` (internal/store/threadlog/records.go), and the projection replaces `thread_tags` wholesale from either record (internal/events/projection/apply.go, `replaceTags`). The rules only restrict what `CreateThread` accepts.
- The seeded tags come from the e2e build only. `cmd/ketju/e2e.go` writes ThreadCreated records straight to the log with `seedLongTags` (six tags, including `long-read`), bypassing the service, so the refusal never sees them. `web/client/e2e/thread-list.spec.ts` asserts all six render in a 390 px row.
- `thread_tags` is `PRIMARY KEY (thread_id, tag)` with no collation, and the projection inserts with `INSERT OR IGNORE`. The answer on case therefore decides whether `Garden` and `garden` are one tag to a later filter.
- Reads already work: the thread page header and the thread list both return tags sorted (`ORDER BY tag`), and the client renders them as a list in the row.
- The implementation, once decided, is small: a `checkTags` in internal/service/threads/create.go beside `checkTitle`, table-driven tests, removing the refusal and the comment on `CreateThreadRequest.Tags`, passing the checked tags into `ThreadCreatedBody`, and updating the docs/07 row. A per-forum vocabulary (option 3b) would add a migration and an admin surface as well.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T13:52:49Z

in-progress to blocked: Tag syntax, limits, vocabulary, who may set tags, and whether tags on create are Phase 1 are undecided; see TKT-01M3M4KVNW

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T13:59:38Z

Review 1160 (run 73cddc78-67db-4814-8410-7b61c6a0a61d, Actions run #639) on PR #111, head 4ce6afe596943324c1de166366158ede7085cd88, base 4ea1a5ddf3017b12b741c60475a564f868d31b40: one low finding, 'Correct the recorded status transition'. Declined: the note is accurate. The ticket went ready to in-progress (claim, then status, both in commit eabb9a1) and then in-progress to blocked, and git ticket wrote the transition note itself. The diff shows only the net change from ready to blocked because both transitions land in the same PR. The note is left as written.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T15:01:16Z

Maintainer decision, 2026-09-28: tags on create are deferred to Phase 4 ("Defer to Phase 4"). docs/09 lists thread tags under Phase 4 beside retag and tag filtering. Until then the create endpoint keeps refusing a non-empty `tags`. This ticket and TKT-01M3M4KVNW (Decide what a thread tag may be and who may set one) move to the phase-4 milestone. The rule questions in TKT-01M3M4KVNW stay open, to be decided with the rest of the tag feature.
