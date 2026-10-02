---
schema: 3
id: TKT-01M3B12ZZ403NEPFH3DASEW1D8
title: Define thread tag rules so new threads can carry tags
type: task
status: ready
status_reason: null
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
updated_at: 2026-10-02T17:16:02Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase4-groom-tags-archive
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

- [ ] docs/04 `thread_tags`, docs/08 "Thread tags" and the docs/07 `POST /forums/{slug}/threads` row state the syntax, the limits, who may set tags and the reserved list, and match the built behaviour
- [ ] POST /forums/{slug}/threads folds, trims, de-duplicates and sorts tags, writes them into ThreadCreated, and the header, the thread list and a reindexed database agree (TestCreateThreadWithTags, TestCreateThreadTagsSurviveReindex)
- [ ] A tag that is not a string, does not match, is over 24 characters, or a ninth distinct tag is validation-failed with the documented field and code, and nothing is written (TestCreateThreadTagRefusals)
- [ ] A reserved tag is accepted from a role moderator, a group-granted moderator of the forum and an agent account, and refused with reserved_tag to anyone else, a moderator of another forum included (TestReservedTagOnCreate)
- [ ] The six seed tags are creatable through the API, and domain.ReservedTags matches docs/08 (TestCreateThreadSixSeedTagsCreatable, TestReservedTagsDocumented)

## Implementation plan

### Settled in grooming

- **Phase.** Tags on create are Phase 4 (maintainer, 2026-09-28: "Defer to Phase 4"). Built in the Phase 4 run (docs/15).
- **Syntax and case.** A tag is `[a-z0-9]+(-[a-z0-9]+)*`, and uppercase input is folded to lowercase (maintainer, 2026-10-02, TKT-01M3M4KVNW question 1 (a); docs/04 `thread_tags`).
- **Limits.** A tag is 1 to 24 characters, and a thread carries at most 8 (maintainer, 2026-10-02, question 2 (b)). The e2e seed's six-tag thread (`cmd/ketju/e2e.go` `seedLongTags`) stays creatable through the API.
- **Vocabulary.** Free text. A moderator corrects a thread with a retag (docs/08). A curated list may come later (maintainer, 2026-10-02, question 3 (c)).
- **Who sets tags on create.** Anyone with `create_thread` in the forum, except reserved tags, which only a moderator or an agent account may set. The reserved list lives in docs/08 "Thread tags" and is `agent` (maintainer, 2026-10-02, question 4 (c)).
- **Repeats keep one copy, and tags are sorted.** A tag named twice, before or after folding, is kept once and not refused. The record holds the tags sorted, which is the order members see (docs/04 `thread_tags`, run decisions pending maintainer review).
- **"Moderator" and "agent account".** "Moderator" means `moderate` in the forum, by role or through a group. "Agent account" means `users.kind = 'agent'`, the account `authorFlags` marks machine-authored (docs/08 "Thread tags", run decision pending maintainer review).
- **Trimming, folding and refusals.** Surrounding whitespace is trimmed, and only ASCII A–Z is folded, so the Kelvin sign (U+212A), which `strings.ToLower` turns into `k`, is refused. The refusals are as docs/07's `POST /forums/{slug}/threads` row states: `validation-failed` with `{field: "tags.N", code}`. The codes are `wrong_type`, `invalid_tag`, `too_long` and `reserved_tag`, and `{field: "tags", code: "too_many"}` covers more than 8 distinct tags (run decision pending maintainer review).
- **No log format change.** `threadlog.ThreadCreatedBody.Tags` already exists, and the projection's `replaceTags` (internal/events/projection/apply.go) writes `thread_tags` from it. These rules bind what the service writes, not what a reader accepts.

### Approach

1. **`domain` tag rules.** Add `internal/domain/tags.go`, beside `ValidateGroupSlug`:
   - `MaxTagLen = 24` and `MaxTags = 8`.
   - `ReservedTags = []string{"agent"}`.
   - `NormalizeTag(s string) (string, code string)`: trim with `strings.TrimSpace`, fold bytes `A`–`Z` only, then check the length (1 to 24 bytes, which is characters, since the result is ASCII) and the pattern. The pattern is checked by a byte loop or a compiled `regexp`, anchored. Answer `invalid_tag` for an empty tag or a pattern miss, and `too_long` past 24. Check the length on the folded string, but refuse anything over 24 bytes before the fold, so a long input is not copied.
   - `NormalizeTags(in []string) ([]string, []forms.FieldError)`: normalize each tag, recording an error with field `tags.N` against its index in the request, drop repeats, and sort. Past 8 distinct tags, add `{tags, too_many}`. Collect every error rather than stopping at the first, as forms validation does. Decide in code review whether `domain` may import `forms` for `FieldError`. If it may not, put the function in `internal/service/threads/tags.go` and keep only the constants and `NormalizeTag` in `domain`.
   - `IsReservedTag(t string) bool`.
2. **Codes.** Add `CodeInvalidTag = "invalid_tag"` and `CodeReservedTag = "reserved_tag"` to `internal/forms/schema.go` and to `forms.Codes`. Add two rows to docs/07's `form-codes` table, which `TestCodesAndTypesAreDocumented` holds to `forms.Codes`. Extend docs/07's sentence "A search names `q` ..." with what create names (`tags.N` and `tags`).
3. **API decode.** In `internal/web/api/create.go`, make `threadRequest.Tags` a `[]json.RawMessage`. Turn each element into a string, or into `{tags.N, wrong_type}` for one that is not a JSON string, as the watch route turns `level`. A `tags` that is not a list is `{tags, wrong_type}`. Pass the strings in `threads.CreateThreadRequest.Tags`. The body bound stays as it is.
4. **Service.** In `CreateThread` (internal/service/threads/create.go), replace the `len(req.Tags) > 0` refusal with `checkTags`. It runs after `checkTitle` and before `checkPost`, and returns the normalized list or a `forms.Invalid` (`problem` maps it to `validation-failed` with `errors`). When any normalized tag is reserved:
   - Ask `s.Perm.Can(ctx, v, forum.ID, domain.PermModerate)`.
   - Call `authorFlags(ctx, v)`, which is already called later, and is moved earlier so it is read once.
   - If the viewer neither moderates the forum nor is an agent account, refuse with `{tags.N, reserved_tag}` for each reserved tag.

   Write the normalized, sorted tags into `ThreadCreatedBody.Tags` in place of `[]string{}`. Delete the comment on `CreateThreadRequest.Tags` and write the rule there instead. All refusals still come before `Threads.Create`, so a refused request leaves no files.
5. **Docs.** The docs/07 row already states the rules (written in grooming). Check the row against the built behaviour, and add "(TKT-01M3B12ZZ4)" wherever the refusal order is described.
6. **e2e.** Add a case to `web/client/e2e` only if TKT-01M3YSQPZX (the client) needs one. This ticket is server only.

### Tests

- `TestNormalizeTag` (internal/domain), table-driven:
  - `garden` passes. `Garden` gives `garden`, and ` long-read ` gives `long-read`.
  - `a` and a 24-character tag pass. A 25-character tag is `too_long`.
  - `-a`, `a-`, `a--b`, `a_b`, `a b`, `ä` and `""` are `invalid_tag`.
  - `"K"` (the Kelvin sign) is `invalid_tag` and never `k`.
  - A 10 KB string is refused without being folded.
- `TestNormalizeTagsDedupesAndSorts`: `["b","A","a","B"]` gives `["a","b"]`. Nine distinct tags give `too_many`, and eight tags with repeats among nine entries pass.
- `TestReservedTagsDocumented`: docs/08 "Thread tags"'s reserved list and `domain.ReservedTags` name the same tags, read between the `reserved-tags:begin` and `reserved-tags:end` markers, as `TestActionsDocumented` reads docs/04.
- `TestCreateThreadWithTags` (internal/web/api/create_test.go):
  - A member creates a thread with `["Garden","long-read","garden"]`.
  - The 201 header's `tags` is `["garden","long-read"]`.
  - `GET /threads/{id}` and the forum's thread list agree.
  - The log's `ThreadCreated` body holds the same list (`threadlog` walk).
- `TestCreateThreadSixSeedTagsCreatable`: the six tags `seedLongTags` writes are accepted through the API.
- `TestCreateThreadTagRefusals`, table-driven, each asserting the `errors` member and that no thread file exists afterwards (as `TestCreateThreadRefusals` does):
  - A non-string element is `wrong_type`.
  - `tags` given as an object is `wrong_type` on `tags`.
  - An invalid tag, a long tag and nine tags each give their code.
  - A mix of errors lists every error, in index order.
- `TestReservedTagOnCreate`. `agent` is:
  - refused with `reserved_tag` for a plain member;
  - accepted for a role moderator, for a member holding `moderate` in that forum through a group (`dbtest.Open` with a group grant), and for an agent account;
  - refused for a member who moderates a different forum only.

  A moderator-by-group whose grant is revoked is refused on the next request, which shows that the check reads `perm` and not the session.
- `TestCreateThreadTagsSurviveReindex`: create a tagged thread, run reindex (`projection` rebuild from the logs), and the thread's tags are unchanged.
- The existing `TestCreateThreadRefusals` case "tags are refused" is replaced by the cases above, not deleted silently.
- Invariants. No route is added, so `adminRouteGates` and the session chokepoint are unchanged. `TestOneDoorIntoSessions` passes unchanged. The response-cache key is untouched, because tags are in the header, and the header is read from the projection at the log length the key already names.

### Out of scope

- Retagging, the tag filter on a forum's list, and the audit row for a retag: TKT-01M29CYSCP (Implement thread tags).
- The Tags field on the new thread screen, chips and filtering in the client: TKT-01M3YSQPZX (Show, filter and set thread tags in the client).
- A curated tag list per forum: no ticket. The maintainer left it for later.
- Keeping agent threads out of a human's unread list by tag (docs/06 "Live updates" mentions it): no ticket, and not a docs/09 deliverable.

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

**agent:claude-code/phase4-groom-tags-archive** at 2026-10-02T17:16:02Z

run decision: pending maintainer review. Phase 4 grooming (tags and archive lane) settled these beside the maintainer's tag answers. Each is written into a design document on branch groom/p4-tags-archive:
- A tag named twice, before or after folding, keeps one copy. Tags are written sorted and members see them sorted (docs/04 `thread_tags`).
- "Moderator" for a reserved tag means `moderate` in the forum, by role or through a group. An agent account is `users.kind = 'agent'` (docs/08 "Thread tags").
- Only ASCII is folded and surrounding whitespace is trimmed. A refused tag is `validation-failed` with `{field: "tags.N", code}`, using the new codes `invalid_tag` and `reserved_tag`. A reserved tag is a field error rather than `forbidden` (docs/07 `POST /forums/{slug}/threads`).
