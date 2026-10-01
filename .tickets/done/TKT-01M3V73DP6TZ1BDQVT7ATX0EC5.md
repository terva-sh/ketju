---
schema: 3
id: TKT-01M3V73DP6TZ1BDQVT7ATX0EC5
title: Keep attachments in reply and new-thread drafts
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:render
  - effort:S
assignees: []
milestone: phase-3
parent: TKT-01M29BKBM7004ZPAA9T81HC939
origin: null
dependencies:
  - TKT-01M29CXK0KHC2QZQ286AG0KKGD
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T07:50:42Z
updated_at: 2026-10-01T11:11:32Z
created_by:
  id: agent:claude-code/phase3-groom-live
  name: ""
updated_by:
  id: agent:claude-code/phase3-v73dp6
  name: ""
extensions: {}
---

## Description

Reply drafts and new-thread drafts keep their source and title on the server so that a draft restored on another device comes back whole (maintainer decision 2026-09-28, TKT-01M3K9XBJA). Once posts can carry attachments (TKT-01M29CXK0K), a draft that drops them comes back incomplete. Worse, nothing then names the uploaded blobs, so blob GC deletes them after seven days.

This ticket adds `attachments` to both draft routes. The run decided it on 2026-10-01 (docs/07 "Drafts"): `PUT` takes the hashes and checks them as an attach does, without adding a reference. `GET` answers them as `Attachment` objects. Blob GC keeps a blob that a draft's attachments name.

Filed by the Phase 3 grooming of live updates and attachments (TKT-01M3V3BVPY). No other ticket owned it, and the Phase 3 deliverables "drafts" and "attachments" (docs/09) need it together.

## Acceptance criteria

- [x] Reply and new-thread drafts save and return their attachments with names and sizes (TestDraftKeepsAttachments)
- [ ] A draft's attachments are checked by the attach rules: another member's blob, a quarantined blob, more than 10, a duplicate, or no upload permission is refused (TestDraftAttachmentChecks)
- [x] Saving a draft adds no blob reference (TestDraftAttachmentAddsNoRef), and a tombstone holds no attachments (TestDraftTombstoneHasNoAttachments)
- [x] Blob GC and ketju redact keep a blob that a draft's attachments name, and GC collects it once the draft is deleted (TestBlobGCKeepsDraftAttachment, TestRedactKeepsDraftAttachedBlob)
- [x] docs/04 drafts, docs/07 Drafts and the Draft and ForumDraft goldens are updated in the same PR

## Implementation plan

Groomed 2026-10-01 for Phase 3 (TKT-01M3V3BVPY), against main at a599b48. Revised at claim on 2026-10-01, against main at 4990dea, after TKT-01M29CXK0K merged in #284 (d253ac0). Read for this plan:
- docs/04 "drafts" and docs/07 "Drafts", with the run decision of 2026-10-01;
- docs/07 "Attachments", and the attach path as #284 built it (`threads.Attachments`, `blobs.Service.Check`, `TestAttachRefusals`);
- docs/03 "Blob garbage collection";
- migration 0018 and the draft queries;
- `DeleteCollectableBlob` in `queries/moderation.sql`, which `ketju redact`'s `deleteBlobs` also calls;
- the plan of TKT-01M29CXK0K (Build the content-addressed blob store and upload path).

### Settled in grooming

- **Drafts carry attachments.** This is the run decision in docs/07 "Drafts" (2026-10-01, pending maintainer review). It follows the maintainer's decision of 2026-09-28 (TKT-01M3K9XBJA) that a draft restored on another device comes back whole.
  - `PUT` on either draft route takes `attachments?`, a list of hashes checked by the attach rules of TKT-01M29CXK0K: at most 10, distinct, each one the viewer uploaded, not quarantined, and `upload` in the forum. It adds no reference, so the refcount and `unreferenced_since` are unchanged.
  - `GET` answers `attachments: [Attachment]`, with the viewer's own upload names.
  - A tombstone holds none.
- **GC keeps them.** `DeleteCollectableBlob` already refuses a blob a draft's source names. It also refuses one a draft's `attachments` names. Redaction calls the same statement, so it keeps them too. The seven-day clock stays the refcount's: an attachment only a draft names is never collected while the draft exists, as a source mention already is not.
- **Authoritative data.** `drafts` is an authoritative table (docs/04), and the column is additive with a default, so a database from before the migration reads as drafts with no attachments.

### What the claim found

- **The interface is `Check`, not `CheckAttach`.** #284 named it `threads.Attachments.Check`, and the threads service already wraps it as `checkAttachments`, which also refuses any attachment when the server has no blob store. Both draft saves call `checkAttachments` and never `AddRefs`.
- **A repeated hash is kept once, not refused.** The attach rule #284 merged answers "the hashes as the Post will record them, each once in the order sent", and `TestAttachRefusals` posts ten with repeats and gets ten. docs/07 "Attachments" says "at most 10 distinct hashes". The groomed test list expected `validation-failed` for a duplicate. Following the attach rule instead is a run decision (pending maintainer review). A draft that refused a list the post would take would fail the composer's autosave on a list it can still post. The duplicate part of the second criterion is therefore met as "kept once", and the box is left unticked with a note.
- **The migration is 0021**, the next after 0020 (TKT-01M29CXK0K). It is checked again against origin/main before the PR opens and before it merges.

### Approach

1. **Migration 0021.**
   - `ALTER TABLE drafts ADD COLUMN attachments TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(attachments) AND json_type(attachments) = 'array')`.
   - Triggers on insert and on update refuse `deleted = 1` with `attachments <> '[]'`, because SQLite cannot add that `CHECK` to the existing table without rebuilding it.
   - docs/04 "drafts" gains the column.
2. **Queries** (`queries/viewer_state.sql`). `UpsertThreadDraft` and `UpsertForumDraft` write `attachments`, `DeleteThreadDraft` and `DeleteForumDraft` write `'[]'`, and `DeleteCollectableBlob` gains `AND NOT EXISTS (SELECT 1 FROM drafts WHERE instr(drafts.attachments, blobs.hash) > 0)`. The hashes are stored as `Check` answers them, valid lower-case hex, so no `lower()` is needed.
3. **Service** (`internal/service/threads` `viewerstate.go`):
   - `DraftRequest` and `ForumDraftRequest` gain `Attachments []string`. `SaveDraft` checks them against the thread's forum, after the reply check, and `SaveForumDraft` against the named forum, after `forumForThread`. Both store the list `checkAttachments` answers as a JSON array.
   - `Draft` and `ForumDraft` (and the saves' answers) draw the stored hashes as `dto.Attachment`, with the metadata helper `attachmentsOf` uses, factored out and keyed on (hash, viewer), so the names are the viewer's own uploads.
4. **Handlers.** The two `PUT` bodies take `attachments`. An absent or null list is none.
5. **DTO.** `Draft` and `ForumDraft` gain `Attachments []Attachment` (`attachments`, never null). The four goldens are regenerated. The hand-written mirror in `web/client/src/api/dto.ts` gains the field because `dto-contract.ts` refuses a golden key the type lacks. A type adds no JavaScript, so `go generate ./internal/web/spa` must leave the dist unchanged, and is run to prove it.
6. **Docs.** docs/07 "Drafts" describes the `PUT` and `GET` shapes and the repeated-hash decision. docs/03 "Blob garbage collection" and docs/04 "drafts" say the collector keeps a blob a draft's attachments name. The form-post table's draft rows are Phase 5, and are noted there as not yet carrying attachments.

### Tests

- `TestDraftKeepsAttachments`: reply and new-thread drafts, saved and read back with names and sizes, through the HTTP chain.
- `TestDraftAttachmentChecks`: another member's blob is `not_yours`, a quarantined one `blocked`, eleven `too_many`, no `upload` is `forbidden`, and a repeated hash is kept once.
- `TestDraftAttachmentAddsNoRef`: the refcount stays 0 and `unreferenced_since` is unchanged.
- `TestDraftTombstoneHasNoAttachments`: the delete empties them, and the triggers refuse a tombstone that has any.
- `TestBlobGCKeepsDraftAttachment`: a blob at zero for eight days that a draft's attachments name is kept. Once the draft is deleted, it is collected.
- `TestRedactKeepsDraftAttachedBlob`: `ketju redact` reports "kept blob … a draft still refers to it".
- The goldens for `Draft` and `ForumDraft` are regenerated, and the diff is read.

### Out of scope

- The composer's use of the field: TKT-01M3V73J8K (Attach files in the composer and show them on posts).
- The non-JS client's draft forms: Phase 5.

## Notes

**agent:claude-code/phase3-groom-live** at 2026-10-01T07:59:50Z

run decision: pending maintainer review. Recorded in docs/07 'Drafts' (2026-10-01): both draft kinds carry attachments, checked by the attach rules but adding no reference. Blob GC and redact keep a blob a draft's attachments name. This follows the maintainer's 2026-09-28 decision (TKT-01M3K9XBJA) that a restored draft comes back whole.

**agent:claude-code/phase3-v73dp6** at 2026-10-01T11:11:03Z

run decision: pending maintainer review. A hash sent twice in a draft's attachments is kept once, at its first place, as a post keeps it. It is not refused as `validation-failed`. Recorded in docs/07 "Drafts" (2026-10-01).

The grooming plan and the second criterion expected a duplicate to be refused. The attach rule TKT-01M29CXK0K merged in #284 does not refuse one. `blobs.Service.Check` answers "each once in the order sent", `TestAttachRefusals` posts ten with repeats and gets ten, and docs/07 "Attachments" says "at most 10 distinct hashes". The description and the settled item both say a draft is checked "as an attach does", so the draft follows the post. Refusing lost: a draft that refused a list the post accepts would fail the composer's autosave for no reason the member could see, and the composer (TKT-01M3V73J8K) sends one list to both.

The second criterion stays unticked for that reason only. Its other four refusals hold on both routes (TestDraftAttachmentChecks): another member's blob is `not_yours`, a quarantined one `blocked`, eleven `too_many`, and no `upload` is `forbidden`. An unknown hash and a non-hash are also refused as `unknown`. The same test pins a repeat as kept once.

## Summary

Both draft routes keep the composer's attachments. Migration 0021 adds `drafts.attachments`, a JSON array that defaults to `'[]'`, and triggers that refuse a tombstone holding any. `PUT /drafts/{thread_id}` and `PUT /forums/{slug}/draft` take `attachments?`. The post path's `checkAttachments` checks them against the thread's forum or the named forum, and no reference is added. `GET` answers them as `Attachment` objects named as the viewer uploaded them. `DeleteCollectableBlob` refuses a blob a draft's attachments name, so blob GC and `ketju redact` keep it while the draft lists it, and GC collects it once the draft is deleted. `dto.Draft` and `dto.ForumDraft` gained `attachments`, which is never null. The goldens and the hand-written `web/client/src/api/dto.ts` mirror gained it too. The dist is unchanged, and the reader stays at 60465 B.

One criterion is unticked. A repeated hash is kept once, as a post keeps it, and is not refused. That is a run decision pending maintainer review, recorded in docs/07 "Drafts" and in the note above. The criterion's four other refusals are tested and hold.

The composer's use of the field is TKT-01M3V73J8K (Attach files in the composer and show them on posts). The Phase 5 draft form routes carry no attachments, as docs/07 now notes.
