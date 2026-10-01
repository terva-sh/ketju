---
schema: 3
id: TKT-01M3V73DP6TZ1BDQVT7ATX0EC5
title: Keep attachments in reply and new-thread drafts
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase3-v73dp6
  branch: run/v73dp6-draft-attachments
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-ad5e25f968c2050ea
  commit: 4990deaa56b419c5b8b3294e0f3df9abc0af451c
  session: null
  claimed_at: 2026-10-01T10:53:08Z
  expires_at: null
archive: null
created_at: 2026-10-01T07:50:42Z
updated_at: 2026-10-01T10:53:10Z
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

- [ ] Reply and new-thread drafts save and return their attachments with names and sizes (TestDraftKeepsAttachments)
- [ ] A draft's attachments are checked by the attach rules: another member's blob, a quarantined blob, more than 10, a duplicate, or no upload permission is refused (TestDraftAttachmentChecks)
- [ ] Saving a draft adds no blob reference (TestDraftAttachmentAddsNoRef), and a tombstone holds no attachments (TestDraftTombstoneHasNoAttachments)
- [ ] Blob GC and ketju redact keep a blob that a draft's attachments name, and GC collects it once the draft is deleted (TestBlobGCKeepsDraftAttachment, TestRedactKeepsDraftAttachedBlob)
- [ ] docs/04 drafts, docs/07 Drafts and the Draft and ForumDraft goldens are updated in the same PR

## Implementation plan

Groomed 2026-10-01 for Phase 3 (TKT-01M3V3BVPY), against main at a599b48. Read for this plan:
- docs/04 "drafts" and docs/07 "Drafts", with the run decision of 2026-10-01;
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

### Approach

1. **Migration** (the next free number after TKT-01M29CXK0K's, checked at implementation).
   - `ALTER TABLE drafts ADD COLUMN attachments TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(attachments) AND json_type(attachments) = 'array')`.
   - A trigger refuses `deleted = 1` with `attachments <> '[]'`, because SQLite cannot add that `CHECK` to the existing table without rebuilding it.
   - docs/04 "drafts" gains the column.
2. **Queries** (`queries/viewer_state.sql`). `UpsertThreadDraft` and `UpsertForumDraft` write `attachments`, `DeleteThreadDraft` and `DeleteForumDraft` write `'[]'`, and `DeleteCollectableBlob` gains `AND NOT EXISTS (SELECT 1 FROM drafts WHERE instr(drafts.attachments, blobs.hash) > 0)`.
3. **Service** (`internal/service/threads` `viewerstate.go`, the draft methods):
   - `SaveDraft` and `SaveForumDraft` take `Attachments []string` and call `s.Attachments.CheckAttach`, the interface TKT-01M29CXK0K adds to avoid an import cycle, without `AddRefs`. The forum is the thread's forum for a reply draft and the named forum for a new-thread draft.
   - Attachments without `upload` are `forbidden`, and an empty list needs nothing.
   - The DTOs are built with the same metadata helper as posts, keyed on (hash, viewer).
4. **DTO.** `Draft` and `ForumDraft` gain `Attachments []Attachment` (`attachments`, never null). The goldens are regenerated.
5. **Docs.** docs/07 "Drafts" describes the `PUT` and `GET` shapes. The form-post table's draft rows are Phase 5, and are noted there as not yet carrying attachments.

### Tests

- `TestDraftKeepsAttachments`: reply and new-thread drafts, saved and read back with names and sizes.
- `TestDraftAttachmentChecks`: another member's blob is `not_yours`, a quarantined one `blocked`, eleven `too_many`, a duplicate `validation-failed`, and no `upload` is `forbidden`.
- `TestDraftAttachmentAddsNoRef`: the refcount stays 0 and `unreferenced_since` is unchanged.
- `TestDraftTombstoneHasNoAttachments`: the delete empties them, and the trigger refuses a tombstone that has any.
- `TestBlobGCKeepsDraftAttachment`: a blob at zero for eight days that a draft's attachments name is kept. Once the draft is deleted, it is collected.
- `TestRedactKeepsDraftAttachedBlob`: `ketju redact` reports "kept blob … a draft still refers to it".
- The goldens for `Draft` and `ForumDraft` are regenerated, and the diff is read.

### Out of scope

- The composer's use of the field: TKT-01M3V73J8K (Attach files in the composer and show them on posts).
- The non-JS client's draft forms: Phase 5.

## Notes

**agent:claude-code/phase3-groom-live** at 2026-10-01T07:59:50Z

run decision: pending maintainer review. Recorded in docs/07 'Drafts' (2026-10-01): both draft kinds carry attachments, checked by the attach rules but adding no reference. Blob GC and redact keep a blob a draft's attachments name. This follows the maintainer's 2026-09-28 decision (TKT-01M3K9XBJA) that a restored draft comes back whole.
