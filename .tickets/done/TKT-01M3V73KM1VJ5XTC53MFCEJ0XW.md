---
schema: 3
id: TKT-01M3V73KM1VJ5XTC53MFCEJ0XW
title: Bound how much one member may upload
type: task
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area:render
  - effort:S
  - area:api
  - area:client
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:360
    path: null
claim: null
archive: null
created_at: 2026-10-01T07:50:48Z
updated_at: 2026-10-03T11:33:46Z
created_by:
  id: agent:claude-code/phase3-groom-live
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-v73km1
  name: ""
extensions: {}
---

## Description

`limits.attachment_mb` bounds one upload, and the per-address write limiter bounds how often a client may send one. Nothing bounds the total a member uploads. A member granted `upload` can fill the disk with files they never attach until the collector deletes them seven days later (docs/03 "Blob garbage collection").

`upload` is granted by an admin and is not seeded for members (docs/07 "Attachments", decided by the run 2026-10-01), so the exposure is limited to people an admin chose. A per-member byte budget over a rolling window, or a cap on unattached bytes per member, would close it.

Found while grooming TKT-01M29CXK0K (Build the content-addressed blob store and upload path). No Phase 3 exit criterion needs it, so it has no milestone (docs/14 "Scope").

## Acceptance criteria

- [x] An upload of new bytes that would take an account's unattached bytes over limits.unattached_upload_mb is validation-failed with file upload_budget and leaves no file or row; bytes already stored are never refused for it (TestUploadBudget, TestUploadBudgetRace)
- [x] limits.unattached_upload_mb defaults to 100, reads KETJU_LIMITS_UNATTACHED_UPLOAD_MB, and ketju config check refuses a value below limits.attachment_mb (validate tests; docs/02 and docs/07 rows)
- [x] The query uses a partial index on blobs (uploaded_by) where refcount = 0, in a migration with the next free number, and a test reads its plan
- [x] The composer shows the upload_budget words (Composer.test.tsx), and e2e/attachments.spec.ts passes at 390 px and 1440 px with one axe scan per test
- [x] The dist from go generate ./internal/web/spa is committed, npm run budget keeps the reader within 120000 B gzip, and the PR states the reader's growth
- [x] Through the HTTP handler, an account at the bound is refused new bytes and gets 201 for bytes the server already holds (TestUploadBudgetOverHTTP)

## Implementation plan

Groomed 2026-10-02 for the Phase 4 run (docs/15), against main at ed82f15.

### Settled in grooming

- **The bound.** Decided by the run, pending maintainer review, in docs/07 "Attachments" (2026-10-02), with the alternatives that lost:
  - **What is bounded:** the bytes an account has stored that no post names. That is the sum of `blobs.size` over blobs whose `uploaded_by` is the account and whose `refcount` is 0. It includes blobs a draft holds, which blob GC keeps for as long as a draft names them (`DeleteCollectableBlob`, migration 0021), and blobs waiting out their seven days (docs/03 "Blob garbage collection").
  - **The limit:** `limits.unattached_upload_mb` (`KETJU_LIMITS_UNATTACHED_UPLOAD_MB`), 100 MB by default. `ketju config check` refuses a value below `limits.attachment_mb`.
  - **When it is asked:** once, after the file is read and hashed, in the write transaction that stores new bytes. A refusal before the body is read lost (Terva review 1945): the hash is unknown then, so it would refuse stored bytes too, and the read it saves is already bounded by `limits.attachment_mb` and the per-address write limiter.
  - **The refusal:** `validation-failed` with `{field: "file", code: "upload_budget"}`. The staged file is removed.
  - **Bytes already stored** are never refused for this, because they use no new disk.
  - **Everyone** is bound by it, admins and agents included.
- **Not a security-posture question.** The bound narrows what an account granted `upload` may do, and changes no one's access to anything. docs/14's upload invariant ("content-addressed and size-limited") is kept and tightened.
- **The member sees it** in the composer, in the words of attaching (`uploadFailure`, `state/files.ts`).

### Approach

1. **Config.**
   - `config.Limits` gains `UnattachedUploadMB int toml:"unattached_upload_mb"`, defaulted to `DefaultUnattachedUploadMB = 100` in the defaults block, read from `KETJU_LIMITS_UNATTACHED_UPLOAD_MB` in `env.go`.
   - `validate.go` refuses a value below `AttachmentMB` and above a sanity ceiling, `maxUnattachedUploadMB = 1 << 20`, with the wording style of the other limits.
   - `testdata/full.toml` gains the key.
   - docs/02's limits list and docs/07's "Request limits" table gain the row at implementation.
2. **Query and index.**
   - Migration: take the next free number, 0025 on main at ed82f15 and checked again at implementation. It adds `CREATE INDEX blobs_uploader_unreferenced ON blobs (uploaded_by) WHERE refcount = 0;`.
   - `queries/blobs.sql` gains `UnattachedBytes :one`, `SELECT coalesce(sum(size), 0) FROM blobs WHERE uploaded_by = ? AND refcount = 0`. Regenerate with sqlc.
   - A test in `internal/service/blobs` reads `EXPLAIN QUERY PLAN` for `UnattachedBytes` and holds that it uses the index, as `TestPlansSeek` (`internal/store/db/views`) does for the thread list.
3. **`blobs.Service`.**
   - It gains `Budget int64` (bytes; zero means the default), wired from config in `cmd/ketju/serve.go` where `Max` is.
   - `MayUpload` is unchanged. It runs before the body is read, when the hash is not yet known.
   - `Upload`'s write transaction asks `UnattachedBytes` in the branch that creates a new blob (`sql.ErrNoRows` from `GetBlob`), and nowhere else. It refuses when the total plus `n` is over the budget. The write connection serializes uploads, so two at once cannot both pass.
   - The known-bytes branch (`RefreshBlob`) is not asked, so stored bytes are never refused for the bound.
   - `CodeUploadBudget = "upload_budget"` joins the codes in `blobs.go`.
   - The refusal is a field error on `file` (`invalid("file", CodeUploadBudget)`), so the deferred cleanup removes the staged file, as it does for `blocked`.
4. **The error catalogue.** docs/07's `POST /blobs` row and its "Attachments" section name the code.
5. **Client.** `uploadFailure` in `state/files.ts` gains a case: `upload_budget` reads "You have too many files waiting to be posted. Post them, or wait: files you remove are cleared within 7 days."
6. **Build.** Run `go generate ./internal/web/spa` and `npm run budget`.

**Reader bundle.** One string in the reader's `state/files.ts`. Estimated at +80 B gzip. Measure it and state it in the PR.

### Tests

- **Go (`internal/service/blobs`).**
  - `TestUploadBudget`:
    - with a 3 MB budget, three 1 MB uploads pass and a fourth is `upload_budget`, and no blob file or row is left behind;
    - attaching one to a post frees its bytes, and the next upload passes;
    - a draft's blob still counts;
    - re-uploading stored bytes passes at the bound;
    - another account's unattached bytes do not count.
  - `TestUploadBudgetRace`: two uploads that each fit alone, but not together, run at once. Exactly one is stored, and the refused one's staged file is gone. Run it under `-race`.
  - **Uploads are content-addressed and served only to readers of a referencing post.** The existing `TestBlobServedOnlyToReaders` and `TestRevalidationRechecksAccess` stay green. The bound adds no serving path.
- **Go (`internal/config`).** `validate_test.go`: below `attachment_mb` and above the ceiling are refused; the default and the env override are read.
- **Go (`internal/web/api/blobs_test.go`).** `TestUploadBudgetOverHTTP`, through the handler, so `MayUpload` is on the path (Terva review 1945):
  - an account at the bound that uploads new bytes gets `validation-failed` with `{field: "file", code: "upload_budget"}`;
  - the same account re-uploading bytes the server holds, its own and another account's, gets 201;
  - an account under the bound gets 201.
- **Vitest** (`composer/Composer.test.tsx`). The table of refusal words (`['type.png', () => field('type_not_allowed'), ...]`) gains an `upload_budget` row.
- **e2e.** The Vitest row covers the words, and the Go tests cover the bound. An e2e test would need a low `limits.unattached_upload_mb` in the config `cmd/ketju/e2e.go` writes (`[limits]`), which every other upload in the run would then share. So:
  - `e2e/attachments.spec.ts` runs unchanged at 390 px and 1440 px with its axe scans, and proves the default bound does not refuse ordinary use;
  - no e2e step reaches the bound.

### Out of scope

- A per-day upload rate. The per-address write limiter already bounds request rate.
- Showing a member how much of the bound they have used.
- Avatars. When TKT-01M3YSQ4CR (Let a member set an avatar) is decided, a blob an `avatar_blob` names should not count against the bound. That ticket's plan carries this.

## Notes

**agent:claude-code/phase4-groom-client-account** at 2026-10-02T17:21:27Z

run decision: pending maintainer review. Bound the bytes an account has stored that no post names (blobs.uploaded_by, refcount 0, drafts included) by limits.unattached_upload_mb, 100 MB by default. It is asked before the body is read and again in the write that stores new bytes, and refused as validation-failed with file upload_budget. Bytes already stored are never refused. Admins and agents are bound too. Recorded with the alternatives in docs/07 'Attachments' (2026-10-02). The ticket now has the phase-4 milestone (docs/15 scope), so the description's 'no milestone' line is out of date.

**agent:claude-code/phase4-groom-client-account** at 2026-10-02T17:33:37Z

Terva review 1945 on PR #360 (head 1359e58, base ed82f15), medium, 'Pre-read budget rejection blocks the stored-byte exemption'. Fixed. The bound is now asked once, after the file is read and hashed, in the write that stores new bytes. The refusal before the read is dropped, because it could not tell stored bytes from new ones. The read it saved is already bounded by limits.attachment_mb and the per-address write limiter. docs/07 'Attachments' records this as an alternative that lost. The plan replaces TestUploadBudgetRefusedBeforeRead with TestUploadBudgetOverHTTP, which goes through the handler so MayUpload is on the path. This supersedes the run-decision note's 'asked before the body is read and again in the write'.

**agent:claude-code/phase4-run-v73km1** at 2026-10-03T04:18:22Z

Phase A done on run/v73km1-upload-bound at 4e64f39 (source only; the dist is not committed, criterion 5 waits for Phase B).

### What landed
- Migration 0027_blobs_uploader_unreferenced (0027 was the next free number on origin/main at 2f2f914d and again at fetch before this note; recheck at push). UnattachedBytes in queries/blobs.sql; TestUnattachedBytesUsesItsIndex reads EXPLAIN QUERY PLAN and wants `SEARCH blobs USING INDEX blobs_uploader_unreferenced (uploaded_by=?)`.
- blobs.Service.Budget (bytes; zero is config.DefaultUnattachedUploadMB), wired in cmd/ketju/serve.go beside Max. Upload asks UnattachedBytes only in the GetBlob ErrNoRows branch and refuses with invalid("file", CodeUploadBudget) when used > budget-n. The RefreshBlob branch is never asked.
- config: Limits.UnattachedUploadMB, KETJU_LIMITS_UNATTACHED_UPLOAD_MB, validate refuses below attachment_mb and above 1<<20. testdata/full.toml and cmd/ketju/testdata/config-check.golden updated.
- Client: uploadFailure's upload_budget case; Composer.test.tsx refusal table row (the removal count at the end of that test now reads refusals.length - 1 rather than a literal 4).
- docs/02 Blob GC row, docs/07 POST /blobs row, Request limits table row, and the Attachments decision.

### One departure from the plan, and why
The plan says the key defaults to 100. It defaults to max(100, limits.attachment_mb) when the key is absent. With a fixed 100, validate's "below attachment_mb" rule would refuse every installation that already runs with attachment_mb above 100 and never set the new key, so the upgrade would stop it starting. Alternatives that lost: dropping the below-attachment_mb rule (the criterion asks for it, and a bound under one full-size file is a misconfiguration); clamping the budget inside blobs.Service (an explicit, invalid value would then be silently replaced rather than refused). An explicit value below attachment_mb is still refused, from the file or the environment (TestUnattachedUploadDefaultsAndEnv, TestInvalidValuesAreRefusedByName). docs/07 records the alternative that lost.

### Tests run
- go vet ./...; go test -p 4 ./internal/... ./tools/... green; go test ./cmd/ketju alone green (39 s); go test -race ./internal/service/blobs green (TestUploadBudgetRace runs ten rounds).
- tsc --noEmit; vitest 92 files, 2616 tests green.
- e2e/attachments.spec.ts at phone (390 px) and desktop (1440 px): 10 passed, including setup. Its two UI tests each scan with axe; the third, "a stranger cannot fetch the blob", is request-only and renders no page, so it has no scan, as before this ticket.

### Bundle
go generate ./internal/web/spa then npm run budget: reader 81492 B before, 81554 B after (+62 B gzip), within 120000 B. The generated dist was restored, not committed.

**agent:claude-code/phase4-run-v73km1** at 2026-10-03T11:33:46Z

Phase B on run/v73km1-upload-bound, rebased onto origin/main b4b70d6d.

- Migration renumbered to 0028_blobs_uploader_unreferenced: 0027 went to thread_continuations (#397) while this ticket waited. docs/07 names 0028. Rechecked at push: 0027 is still main's newest.
- GET /api/v1/admin/settings served golden (#390) gains limits.unattached_upload_mb, since that response lists every restart-only key. Rewritten with -update; no route was added, so no new served case.
- Dist regenerated and committed. npm run budget: reader 91435 B on main, 91501 B here (+66 B gzip), within 120000 B.
- Tests: go vet; go test -race -p 4 ./internal/... ./tools/... and ./cmd/ketju green; tsc; vitest 100 files, 2812 tests; e2e/attachments.spec.ts at 390 px and 1440 px, 10 passed.

## Summary

Each account's stored-but-unposted bytes are now bounded by limits.unattached_upload_mb (KETJU_LIMITS_UNATTACHED_UPLOAD_MB). When the key is absent it defaults to max(100, limits.attachment_mb), and ketju config check refuses an explicit value below attachment_mb or above 1<<20. blobs.Service.Upload checks the bound once, inside the write that stores new bytes, after hashing. Going over it is validation-failed {file, upload_budget}, and the staged file is removed. Re-uploading stored bytes is never refused. The sum is read through the partial index blobs_uploader_unreferenced (migration 0028), and a test reads that plan. The composer shows the refusal in plain words. docs/02 and docs/07 carry the rows and the decision, including the default departure and the alternatives that lost. Reader +66 B gzip (91501 B).
