---
schema: 3
id: TKT-01M29CXK0KHC2QZQ286AG0KKGD
title: Build the content-addressed blob store and upload path
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:render
  - effort:L
assignees: []
milestone: phase-3
parent: TKT-01M29BKBM7004ZPAA9T81HC939
origin: null
dependencies:
  - TKT-01M29BYYJ0WJ3C7RRZQMDRP5ZB
  - TKT-01M29C5Y3SAD25GYYEMHYPDF24
blocks_on: none
references:
  - ref: design:storage-attachments
    path: docs/03-thread-storage.md
  - ref: pr:280
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:30:21Z
updated_at: 2026-10-01T10:25:24Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-run-cxk0k
  name: ""
extensions: {}
---

## Description

`POST /api/v1/blobs` as multipart, writing to `blobs/sha256/xx/<hash>`, deduplicated by content hash, with a `blobs` row carrying size, MIME, uploader, refcount, and status. Serving is at `/blobs/{hash}` outside the API with immutable cache headers.

Attachments are referenced from the post body by hash, filename, size, and MIME type. They never enter the log itself.

MIME is decided by sniffing against an allowlist, not by what the upload claims. A file named `.png` that is something else is the oldest trick in the book.

## Acceptance criteria

- [x] The same bytes uploaded by two members are one file and one blobs row with two blob_uploads rows, and attaching them to two posts makes the refcount 2 (TestUploadDedupOneFileOneRow, TestTwoPostsTwoRefs)
- [x] The type is sniffed from the bytes, never the claim: only image/png, image/jpeg and image/gif that pass their DecodeConfig, application/pdf and text/plain are stored, and anything else is validation-failed type_not_allowed (TestUploadSniffsNotClaims, TestUploadRefusesOutsideAllowlist, TestUploadImageMustDecodeConfig)
- [x] Upload needs a member with upload in the named forum: anonymous 401, pre-user 403, no grant 403, hidden forum 404, archived forum 403 except an admin (TestUploadPermission)
- [x] A blob is served only to a viewer who may read the body of a post naming it, by the thread page's rules; every case in TestBlobServedOnlyToReaders passes, including deleted, moderator-hidden, hidden-pending-review, redacted, moved-to-an-unreadable-forum, suspended-author, group-grant moderator, unattached, quarantined and source-URL-only, and every refusal is identical to a missing hash
- [x] A post attaches at most 10 blobs, each one the poster uploaded and not quarantined; AddBlobRef runs before the append, a collected blob is refused, and a failed append drops the references again (the TestAttach* tests)
- [x] A reply with attachments is projected before it answers, so its blobs are servable at once (TestReplyWithAttachmentIsServableAtOnce)
- [x] A quarantined blob is never served, and its bytes cannot be uploaded or attached again (TestBlobServedOnlyToReaders quarantined row, TestUploadOfQuarantinedBytesBlocked, TestAttachQuarantinedRefused)
- [x] post_attachments is a projection that reindex rebuilds identically and a Redacted record clears (TestPostAttachmentsProjected, TestRedactedDropsPostAttachments, TestReindexRebuildsPostAttachments)
- [x] The thread page's viewer and the forum tree carry can_upload, and the goldens and docs/04 and docs/07 are updated in the same PR (TestThreadViewerCanUpload, TestForumTreeCanUpload)
- [x] GET /blobs/{hash} answers with Cache-Control private, no-cache, the hash as ETag with 304 and Range support, nosniff, the sandbox CSP, CORP same-origin, and inline or attachment disposition (TestBlobHeaders), and a conditional request is authorized before any 304, so a reader who lost access or signed out gets not-found (TestRevalidationRechecksAccess)
- [x] A post's attachment shows the name its author first uploaded it under and a later upload of the same bytes renames nothing, a second uploader never sees the first one's name, and a tombstone carries no attachments (TestReuploadKeepsFirstName, TestSecondUploaderNeverSeesFirstName, TestTombstoneCarriesNoAttachments)
- [x] The size cap limits.attachment_mb is enforced during the read, and an honest Content-Length over it is refused before a byte is read; every refused or failed upload removes its staged file at once, and GC removes staged files over an hour old (TestUploadCapDuringRead, TestUploadContentLengthOverCap, TestRefusedUploadLeavesNoStagedFile, TestCollectorSweepsStaleStagedFiles)
- [x] A blob file with no row is never served, an upload whose transaction fails after placing its file removes it, and the collector removes any orphan over an hour old (TestUploadCommitFailureLeavesNoOrphan, TestCollectorRemovesOrphanBlobFiles)

## Implementation plan

Groomed 2026-10-01 for Phase 3 (TKT-01M3V3BVPY), against main at a599b48. Read for this plan:
- docs/03 "On-disk layout", "Attachments", "Blob garbage collection" and "Hard deletion";
- docs/04 "blobs", "permissions", "drafts" and "Projection tables";
- docs/07 endpoint table, "Thread response shape", "Security headers and limits", "Request limits" and "CSRF posture";
- docs/14;
- `internal/store/blob/blob.go`;
- migration 0006 (`blobs`) and `queries/moderation.sql` (`CreateBlob`, `BlobForServing`, `AddBlobRef`, `DropBlobRef`, `DeleteCollectableBlob`);
- `internal/jobs/blobgc.go`, and `cmd/ketju/redact.go` (`deleteBlobs`, `blob.deleted`, #192);
- `internal/events/projection/apply.go`;
- in `internal/service/threads`: `create.go` (`checkPost`, `Reply`, `CreateThread`, `Project`), `view.go` (`revealRemoved`, `removal`, attachment assembly) and `access.go`;
- `internal/dto/dto.go` (`Attachment`, `ThreadViewer`, `Forum`);
- `internal/web/middleware.go` (`UploadPath`, `limitBody`) and `internal/web/csrf.go` (`needsJSON` exempts the upload route);
- `internal/config` (`limits.attachment_mb`, default 10, ceiling 512);
- migration 0003's permission seed.

### Settled in grooming

- **The on-disk layout is fixed** by docs/03 "On-disk layout" and "Attachments" (`blobs/sha256/ab/<hash>`), and implemented in `internal/store/blob` (`Dir`, `Algorithm`, `Path`, `ValidHash`). Blob GC and `ketju redact` already use it. This ticket adds no format.
  - The staging directory `blobs/tmp/` and the thumbnail name `<hash>.thumb` are recorded in docs/03 as a run decision (2026-10-01, pending maintainer review). Both hold only what can be rebuilt, so neither is docs/14 class 2.
  - The log already carries `attachments` on `Post` (`threadlog.PostBody.Attachments`, a list of hashes), so no record type or field is added (docs/13 "The frozen log format").
- **Upload, type, attach, serve and names** are the run decision in docs/07 "Attachments" (2026-10-01, pending maintainer review):
  - `POST /api/v1/blobs?thread=` or `?forum=` takes one `file` part, and needs a member with `upload` in that forum.
  - The type is sniffed by `http.DetectContentType`. The allowlist is png, jpeg and gif, each of which must also pass `DecodeConfig`, plus pdf and text/plain. Anything else is `validation-failed`, `type_not_allowed`, and quarantined bytes are `blocked`.
  - The answer is 201 `Attachment`, identical whether or not the bytes were known. Its `original_name` is the name the viewer first uploaded these bytes under.
  - A post attaches at most 10 blobs, each one the poster uploaded.
  - A blob is served only to a viewer who may read the body of a post that names it, by the thread page's rules. Everyone else gets `not-found`, the uploader included.
  - A post's attachment shows the name its author first uploaded it under. A `blob_uploads` row is written once and never updated, so a later upload of the same bytes renames nothing already posted (Terva review 1696).
  - Blob responses are `Cache-Control: private, no-cache` with the hash as `ETag`, so every use revalidates and `Authorize` runs before any 304. A year's `immutable` lost (Terva review 1696), because a browser would keep showing the file after its reader lost access. docs/03's "immutable cache headers" sentence now points at this.
  - No migration grants `upload` to members.
- **The upload and attach ordering** is docs/03's contract with the collector:
  - `AddBlobRef` comes before the append, and an attach whose `AddBlobRef` changes no row is refused;
  - an upload writes its file and inserts or refreshes its row inside one `db.Write`.
- **The size cap** is `limits.attachment_mb` (docs/07 "Request limits", `cfg.Limits.AttachmentMB` passed to `web.Limits.Upload` in `serve.go`). It is enforced while the bytes are read, never after they have been buffered.
- **Removed posts.** `revealRemoved` in `view.go` already says who sees a removed post's body:
  - for a moderator's hide or a post hidden pending review, a moderator of the forum, including one by group grant, and the post's author;
  - for an author's delete, moderators only;
  - for a redacted post, nobody.

  Serving reuses that function, so a blob and its post never disagree.
- **The response cache.** Attachment metadata for a (hash, author) pair never changes once written: `blobs` keeps its size, type and dimensions, and the `blob_uploads` row is insert-once. So the cached `posts` may carry it without a new key part. The `can_upload` flags live in the viewer block and the forum tree, and neither is cached (docs/07 "The response cache").
- **No new audit action.** An upload is not a moderation act, and `blob.deleted` stays the redaction's and nothing else's.

### Approach

1. **Migration** (the next free number: 0020 at grooming, checked again at implementation).
   - `blob_uploads (hash TEXT NOT NULL REFERENCES blobs(hash) ON DELETE CASCADE, user_id INTEGER NOT NULL REFERENCES users(id), original_name TEXT NOT NULL, uploaded_at INTEGER NOT NULL, PRIMARY KEY (hash, user_id)) WITHOUT ROWID`. It is authoritative, and `foreign_keys` is on in `db.go`.
   - `ALTER TABLE blobs ADD COLUMN width INTEGER` and `height INTEGER`, null for anything that is not an image.
   - `post_attachments (thread_id INTEGER NOT NULL, ordinal INTEGER NOT NULL, hash TEXT NOT NULL, PRIMARY KEY (thread_id, ordinal, hash))` with `INDEX (hash, thread_id, ordinal)`. It is a projection: it carries the PROJECTION marker and joins `db.ProjectionTables`.
   - docs/04 gains `blob_uploads` under the authoritative tables, the two `blobs` columns, and `post_attachments` under the projection tables.
2. **Queries** (`queries/blobs.sql`, new):
   - `GetBlob` (any status, for the upload path);
   - `CreateBlob` with width and height;
   - `RefreshBlob`, which sets `unreferenced_since = now` while `refcount = 0`, so GC waits seven days from the latest upload;
   - `InsertBlobUpload`, which is `INSERT … ON CONFLICT (hash, user_id) DO NOTHING`, so the first name stands;
   - `BlobUploadedBy(hash, user_id)`, which answers the stored name;
   - `AttachmentMeta`, one batched read joining `blobs` to `blob_uploads` on (hash, author) for a page's posts;
   - `PostAttachmentRefs(hash, after_thread, after_ordinal, limit)`, keyset-paged;
   - `InsertPostAttachment` and `DeletePostAttachments(thread_id, ordinal)`.

   `BlobForServing` stays the serve path's only blob lookup (docs/04).
3. **Projection** (`internal/events/projection/apply.go`):
   - `applyPost` inserts a `post_attachments` row for each hash in `b.Attachments`.
   - `applyRedacted` deletes the ordinal's rows beside `DeleteMentions`.
   - Reindex rebuilds the table like the other projections.
4. **`internal/store/blob`**:
   - `TmpDir`, and `Stage(root) (*os.File, error)` for a random file in `blobs/tmp/`;
   - `SweepTmp(root, olderThan)`. `serve` calls it with 0 under the lock before it listens, and `BlobGC.Run` calls it with one hour, for a crash mid-upload;
   - `Commit(tmp, root, hash) (placed bool, err error)`, which renames into place and syncs the directory, and reports whether it placed the file. When a file is already there, it removes the staged one instead, which is safe because the bytes are identical.
   - `Orphans(root, olderThan)`, which lists blob files under `blobs/sha256/*/` that are older than `olderThan`. Names ending in `.gc` or `.thumb` are left out.
5. **`internal/service/blobs`** (new). `Service{DB, Root, Perm, Threads, Clock, Log}`.
   - **`Upload(ctx, v, target, name, r, max)`**:
     1. An anonymous viewer is refused `unauthenticated`. The target resolves to a forum, either through `threads.ReadAccess` or through the forum slug. Then `perm.Check(upload)`: Hidden is `not-found`, Deny is `forbidden`. `perm` already denies an archived forum to everyone but admins.
     2. Stage the part, with a `defer` that removes the staged file unless step 6 committed it. So every refusal and failure removes it at once: too large, a type off the allowlist, a failed `DecodeConfig`, `blocked`, a database error, or a client that went away mid-body (Terva review 1703). Copy the part into the staged file through `io.LimitReader(r, max+1)` while hashing with SHA-256. More than `max` bytes is `payload-too-large`.
     3. Sniff the first 512 bytes and check the allowlist. For an image, call `png`, `jpeg` or `gif`'s own `DecodeConfig` on the staged file, as the sniffed type says, never `image.DecodeConfig`. That gives the width and height, or the upload is refused.
     4. Clean the name: base name only, no control characters or path separators, at most 255 bytes of UTF-8, and `file` if nothing is left.
     5. `fsync` the staged file.
     6. In one `db.Write`, read the blob with `GetBlob`. A quarantined blob is `blocked`. A missing one is created with `CreateBlob`, and an existing one gets `RefreshBlob`. Then `InsertBlobUpload(hash, v.UserID, name, now)`, `BlobUploadedBy` for the name that stands, and `blob.Commit`. If the transaction fails after `Commit` placed the file, a second `db.Write` removes the file when `BlobExists` is still false. If that also fails, the collector's orphan sweep finishes the job (docs/03, after Terva review 1708). The orphan is never served in the meantime, because serving needs the row.
     7. Answer `Attachment{hash, size, mime, the stored name, width, height}`.
   - **`Authorize(ctx, v, hash)`**: `BlobForServing`, then the references a page of 100 at a time, memoised per thread. It allows the first reference for which `threads.CanSeePostBody` says yes. Every refusal, a malformed hash included, is the same `not-found`.
   - **`CheckAttach(ctx, v, forumID, hashes)`** and **`AddRefs(ctx, hashes)`**, for the post paths:
     - at most 10 distinct valid hashes;
     - the poster has `upload` in the forum;
     - each hash has a `blob_uploads` row for the poster and status `ok`.

     A failure is `validation-failed`, with `{field: "attachments", code}` where `code` is `not_yours`, `blocked`, `too_many` or `unknown`, except that a missing `upload` permission is `forbidden`. `AddRefs` runs `AddBlobRef` for each hash in one `db.Write`, and a row that does not change is `unknown`, because the blob has been collected.
6. **`internal/service/threads`**:
   - **No import cycle.** `blobs` imports `threads`, for `ReadAccess` and `CanSeePostBody`, and never the reverse. `threads.Service` gains a field `Attachments`, whose interface type `threads` defines with `CheckAttach`, `AddRefs`, `DropRefs` and `Meta`. `*blobs.Service` satisfies it, and `serve.go` wires it in.
   - **`CanSeePostBody(ctx, v, id, ordinal) (bool, error)`** runs `access`; refused means false. It takes the entry from a `Stable` snapshot of the index. The answer is true for a visible entry that is not moved, and otherwise `revealRemoved(a, e)`.
   - **Posting with attachments.** `checkPost` no longer refuses attachments. `Reply` and `CreateThread` call `CheckAttach` with the other refusals, before anything is appended, and call `AddRefs` just before `Writer.Append`. If the append fails, `DropBlobRef` runs for each hash, and a failure there is logged at error with the hashes. `postBody` records the hashes in the order sent. `CreateThreadRequest` gains `Attachments`.
   - **Projection before the answer.** `Reply` with attachments calls `s.Project` before it answers, as `CreateThread` already does, so the new post's images load at once.
   - **Attachment metadata.** `view.go` fills `dto.Attachment` from `AttachmentMeta`. The name falls back to `attachment` plus an extension from the type when the author has no `blob_uploads` row. A tombstone keeps `attachments: []`, because `HiddenEntry` alone reads them.
   - **`ThreadViewer.CanUpload`** is `perm.Can(upload)` on the thread's forum, and false for an anonymous viewer.
7. **`internal/service/forums`.** `Forum.CanUpload` in the forum tree comes from one `perm.CanEach(v, ids, upload)` per request, and is false for an anonymous viewer. The new-thread screen reads it.
8. **`internal/dto`.** `Attachment` gains `Width` and `Height` as `*int` (null). `ThreadViewer` gains `CanUpload` and `Forum` gains `CanUpload`. The goldens are regenerated.
9. **HTTP.**
   - **`internal/web/api/blobs.go`**: `POST /api/v1/blobs`. It reads the parts with `r.MultipartReader()`, never `ParseMultipartForm`. Exactly one part named `file` is allowed, and any other part is `bad-request`. Answer 201.
   - **Body limit.** `web.Limits` gets an upload overhead allowance of 64 KiB for multipart headers, so the file part itself may be the full `attachment_mb`. The part is capped at exactly the limit in step 5.
   - **`internal/web/blobs`** (new): `GET` and `HEAD /blobs/{hash}`, through `Authorize` and then `http.ServeContent` on the file, so `Range` and `If-None-Match` work. It sets the headers docs/07 "Attachments" lists before writing, so the `sandbox` CSP replaces the page policy that `securityHeaders` set on the way in.
   - **Wiring.** `serve.go` sets `Groups.Blobs` and calls `blob.SweepTmp` at startup.
10. **Orphans.** `BlobGC.Run` gains a step beside `recoverTrash`. For each file from `blob.Orphans(root, time.Hour)`, it removes the file inside a `db.Write` when `BlobExists` is false.
11. **Blob deletion.** `BlobGC.collect` and `redactor.deleteBlobs` delete the blob row, and the `ON DELETE CASCADE` removes its `blob_uploads` rows in the same transaction. A test holds this rather than trusting the pragma.
12. **Docs.**
    - docs/07: the `POST /blobs` and `GET /blobs/{hash}` rows. The `POST /threads/{id}/posts` row, where "any attachment (nothing uploads yet)" is replaced by the attach rules. The `POST /forums/{slug}/threads` row gains `attachments`. The thread page's `viewer` gains `can_upload`.
    - docs/04: the tables in step 1.

### Tests

- **Upload type.**
  - `TestUploadSniffsNotClaims`: a PNG sent as `notes.txt` with `Content-Type: text/plain` is stored as `image/png`, and an HTML file sent as `x.png` is `type_not_allowed`.
  - `TestUploadRefusesOutsideAllowlist`: SVG, HTML, WebP, ZIP and an ELF binary.
  - `TestUploadImageMustDecodeConfig`: PNG magic followed by garbage.
- **Upload size.**
  - `TestUploadCapDuringRead`: a chunked body one byte over the cap is 413, and the staged file never grew past cap + 1 bytes and is gone afterwards.
  - `TestUploadContentLengthOverCap`: refused before a byte is read.
- **Content addressing.**
  - `TestUploadDedupOneFileOneRow`: two members upload the same bytes, giving one file, one `blobs` row at refcount 0, and two `blob_uploads` rows.
  - `TestTwoPostsTwoRefs`: attaching the bytes to two posts makes the refcount 2.
  - `TestUploadAnswerSameForKnownBytes`: the same status and shape for bytes another member sent first, with `original_name` the name this viewer sent.
  - `TestReuploadKeepsFirstName`: an author attaches `a.png`, then uploads the same bytes as `b.png`. The answer says `a.png`, and the posted attachment still reads `a.png`, both live and from the response cache.
- **Who may upload.** `TestUploadPermission`, a table:
  - anonymous is 401;
  - a pre-user is 403, from the gate;
  - a member without `upload` is 403;
  - a hidden forum is 404;
  - a member in an archived forum is 403;
  - an admin succeeds;
  - a group grant succeeds.
- **Upload edge cases.**
  - `TestUploadOfQuarantinedBytesBlocked`.
  - `TestUploadNameSanitized`.
  - `TestUploadRacesCollector`: an upload during `collect` leaves the file present exactly when the row is.
  - `TestServeSweepsStagedUploads`.
  - `TestRefusedUploadLeavesNoStagedFile`, a table: over the cap, a type off the allowlist, a failed `DecodeConfig`, quarantined bytes, an injected `db.Write` failure, and a client that disconnects mid-body. Each leaves `blobs/tmp/` empty.
  - `TestCollectorSweepsStaleStagedFiles`: a staged file two hours old is removed and one a minute old is kept.
  - `TestUploadCommitFailureLeavesNoOrphan`: a `db.Write` failure injected after `Commit` placed the file leaves no blob file and no row. When the cleanup write is also failed, the file is not served, and the collector's next pass removes it.
  - `TestCollectorRemovesOrphanBlobFiles`: an hour-old blob file with no row is removed. A fresh one is kept, and so is a file whose row exists.
- **Attach.**
  - `TestAttachRaisesRefcountBeforeAppend`;
  - `TestAttachCollectedBlobRefused`;
  - `TestAttachSomeoneElsesBlobRefused`;
  - `TestAttachQuarantinedRefused`;
  - `TestAttachLimit`;
  - `TestAttachNeedsUpload`;
  - `TestAttachDropsRefsWhenAppendFails`;
  - `TestReplyWithAttachmentIsServableAtOnce`;
  - `TestCreateThreadWithAttachments`.
- **Serving: the docs/14 invariant.** `TestBlobServedOnlyToReaders` is one table, run against `GET /blobs/{hash}`. Every refusal is byte-identical to the answer for a hash that does not exist.

  | Case | Served to | Refused to |
  |---|---|---|
  | visible post | a member; an anonymous viewer on a public forum | an anonymous viewer on a members-only forum; anyone in a hidden forum |
  | author-deleted post | a moderator | a member; the author |
  | moderator-hidden post | the author; a moderator; a moderator by group grant | a member |
  | post hidden pending review | the author; a moderator; a moderator by group grant | a member |
  | redacted post | | everyone, admin included |
  | thread moved to a forum the viewer cannot read | | that viewer |
  | suspended author's visible post | a member | |
  | suspended author's post after the bulk hide | a moderator | a member |
  | the viewer is suspended | as an anonymous viewer would be | |
  | uploaded, attached to no post | | everyone, the uploader included |
  | quarantined | | everyone |
  | named only by a `/blobs/<hash>` URL in a post's source | | everyone |
  | malformed hash | | everyone |
- **Headers.** `TestBlobHeaders`: `private, no-cache`, `Vary: Cookie`, the hash as `ETag` with a 304 on `If-None-Match`, a 206 for `Range`, `nosniff`, `default-src 'none'; sandbox`, `Cross-Origin-Resource-Policy: same-origin`, `inline` for an image and `attachment` for a PDF.
- **Revalidation.** `TestRevalidationRechecksAccess`: a reader's `If-None-Match` gets 304. After the post is hidden, the same conditional request from that member is the 404 a missing hash gets, never a 304. The same holds after sign-out, and after a `perm` write removes `read`.
- **Projection.**
  - `TestPostAttachmentsProjected`;
  - `TestRedactedDropsPostAttachments`;
  - `TestReindexRebuildsPostAttachments`, through the existing reindex-equals-live harness;
  - `TestProjectionTablesAreMarked`, updated.
- **DTO.**
  - `TestThreadPageAttachmentMeta`;
  - `TestSecondUploaderNeverSeesFirstName`;
  - `TestTombstoneCarriesNoAttachments`, for a member viewing a removed post that had attachments;
  - `TestThreadViewerCanUpload`;
  - `TestForumTreeCanUpload`;
  - the goldens regenerated with `go test ./internal/dto ./internal/web/api -update`, and the diff read.
- **Deletion.** `TestCollectorRemovesBlobUploads` and `TestRedactRemovesBlobUploads`.
- **docs/13 invariants.**
  - `chokepoint_test.go` passes unedited.
  - `TestRespCacheKeyUnchanged`, or the existing key test, passes with no new key part.
  - No record type is added, and `PostBody` is unchanged.
  - No audit action is added.

### Out of scope

- Thumbnails and `/blobs/{hash}/thumb`: TKT-01M29CYS9W (Generate image thumbnails for attachments).
- Attachments in drafts, and the drafts half of the GC guard: TKT-01M3V73DP6 (Keep attachments in reply and new-thread drafts).
- The composer's Attach control and the post's attachment card: TKT-01M3V73J8K (Attach files in the composer and show them on posts).
- A per-member upload budget: TKT-01M3V73KM1 (Bound how much one member may upload), with no milestone.
- Avatars. Nothing uploads one in Phase 3, and serving an avatar blob needs its own access rule, which would be a docs/14 class 1 question when it comes.
- A moderator control to quarantine a blob. No ticket owns one, and nothing in Phase 3 needs it. The tests set the status with `SetBlobStatus`.
- Backing up `blobs/`: TKT-01M29CXK35, in Phase 4.


### Revised at claim (2026-10-01, against main at a15d1b2)

Everything above stands, `### Settled in grooming` unchanged. Reading the code at claim adds:
- **An edited post's attachments come from `post_attachments`.** An index entry points at the current revision, and an `Edit` body has no `attachments`, so `threadlog.Reader` returns none for an edited post. `view.go` takes the hashes from the `Post` record when it read one, and otherwise, for an entry carrying `EntryHasAttachments`, from `post_attachments` for that (thread, ordinal). Serving already reads `post_attachments`, so the page and the serve path agree. The response cache key already names `log_length_projected`, so a page drawn before the projection caught up is not reused after it.
- **`api.Deps.Blobs` is optional.** Fifteen call sites build `api.Deps`, all tests but `serve.go`. A nil `Blobs` answers `POST /api/v1/blobs` with `not-implemented`, as `web.Groups` does for an absent group, and a nil `threads.Service.Attachments` refuses any attachment as today. `serve.go` wires both.
- **The upload body limit** is `limits.attachment_mb` plus a 64 KiB multipart allowance in `web.Limits.forPath`. An honest `Content-Length` over that is refused before a byte is read, and the `file` part is capped at exactly `limits.attachment_mb` while it is read.
- **The migration number** is checked against origin/main at push time.

## Notes

**agent:claude-code/phase3-groom-live** at 2026-10-01T07:59:44Z

run decision: pending maintainer review. Recorded in docs/07 'Attachments' and docs/03 'Blob garbage collection' (2026-10-01):
- Upload needs upload in a named forum (?thread= or ?forum=). No migration grants upload to members.
- The type is sniffed by http.DetectContentType. The allowlist is png, jpeg and gif (each passing its stdlib DecodeConfig), pdf and text/plain. SVG, HTML, WebP and archives are refused.
- The answer is the same for known and new bytes.
- A post attaches at most 10 blobs, and only blobs the poster uploaded (new blob_uploads table).
- A blob is served only to a reader of a post body naming it in attachments. There is no uploader exception, and a /blobs URL in a post's source is not a reference.
- A post shows the name its author uploaded under.
- Uploads are staged in blobs/tmp/.
The on-disk layout itself is fixed by docs/03 'On-disk layout' and internal/store/blob, so no open decision was needed. Serving an unposted blob to its uploader would widen the docs/14 invariant (class 1), so it was not taken.

**agent:claude-code/phase3-groom-live** at 2026-10-01T08:13:52Z

run decision: pending maintainer review. Amends note 1 after Terva review 1696 on PR #271. Blob and thumbnail responses are Cache-Control: private, no-cache with the hash as ETag, and Authorize runs before any 304. The year-long immutable caching docs/03 once named would let a browser keep showing a file after access was lost. A blob_uploads row is written once, so a re-upload of the same bytes under another name renames nothing already posted, and the upload answer carries the viewer's first name. docs/07 'Attachments' and docs/03 'Attachments' are updated.

**agent:claude-code/phase3-run-cxk0k** at 2026-10-01T09:56:27Z

Implementation notes (agent:claude-code/phase3-run-cxk0k, 2026-10-01). Where the build differs from the plan's Approach, and why:
- **post_attachments has a `position` column.** An edited post's current record is an Edit with no attachments, so the page reads its list from the projection; position keeps the order the Post gave. It is a projection column, rebuilt by reindex, so no format question.
- **threads.Attachments is Check, AddRefs and DropRefs, without Meta.** The thread service reads attachment metadata itself (`AttachmentMeta`), so a thread page with no blob store still draws its attachments. A nil `Attachments` refuses any attachment as before.
- **api.Deps.Blobs is optional** and a nil one answers POST /api/v1/blobs not-implemented: fifteen test sites build Deps. serve.go wires it.
- **The upload refuses an archived forum from the forum's row** as well as through perm, so an archive refuses at once whatever perm's one-minute cache holds, as readOnlyForum does for posts. viewer.can_upload and the forum tree's can_upload read it the same way.
- **The blob_uploads cascade** is named as the second exception in TestNoCascadingDeletes, with the reason.
- **Forum-level grants replace a group's global ones in that forum** (perm). Granting members `upload` in one forum through the admin grid therefore needs read, create_thread and reply granted there too. That is the grid's existing rule and not this ticket's to change; the tests grant the full set.

run decision: pending maintainer review. Recorded in docs/03 "Blob garbage collection" (2026-10-01): the collector keeps a blob file with no row when a thread log names its hash, and removes no orphan while a pass is held. No failed upload is on a post, so such a file means a database restored from a backup older than blobs/, and its bytes have no other copy. The alternative, removing every hour-old orphan as review 1708's text read, lost because it would delete posted files after such a restore. This only narrows what is deleted.

**agent:claude-code/phase3-run-cxk0k** at 2026-10-01T10:14:44Z

Terva review 1732 (run f475757d, head f452a30, base f3538e6), both fixed. high: Commit now syncs every directory from the shard up to the data directory before the rename (and the shard after it), always, so a new shard's entry survives a power cut; TestCommitSyncsNewShardInItsParent records the syncs through a syncDir seam. Always rather than only when MkdirAll created something, because a shard a crashed process created was never synced either; four fsyncs per new blob are cheap. medium: the collector checks Hold before sweeping stale staged files, so a held pass removes nothing; the orphan test now also keeps a stale staged file while held. Both mutations (sweep before hold, parent syncs dropped) fail the tests. docs/03 says both.

**agent:claude-code/phase3-run-cxk0k** at 2026-10-01T10:20:29Z

Terva review 1735 (run 79b82405, head 8707cce, base f3538e6): both 1732 findings resolved; one new medium, fixed. A client pausing mid-body past the collector's hour had its staged file swept, and the upload then failed at Commit (the server sets only ReadHeaderTimeout, so nothing bounds a body read). blob.Stage now records the staged file's base name in an in-process set until blob.Unstage, which Upload's deferred cleanup calls after removing or committing the file; SweepTmp skips held names at any age. One process holds the data directory, so an in-memory set is all of them. Considered instead: a per-read deadline on the upload body (indirect, and couples the collector's hour to a handler timeout) and touching the file's mtime while waiting (no event to touch on during a pause). Tests: TestSweepTmpSkipsHeldFiles, TestSweepSparesAPausedUpload (io.Pipe upload paused, file aged two hours, swept, resumed, succeeds). Removing the skip fails both.

**agent:claude-code/phase3-run-cxk0k** at 2026-10-01T10:25:24Z

Terva review 1736 (run bab42c5b, head 101facb, base 3890b38): 1735's finding resolved; one new medium, fixed. A file part followed by a second part answered bad-request after the file was committed with its blob_uploads row, so the uploader could attach the blob of a refused request. The handler now hands Upload the part through onlyPart, which at the part's end calls NextPart and fails the read (partError: bad-request, or payload-too-large for a MaxBytesError) unless the body ends; Upload fails on the read error before it syncs or commits, and its deferred cleanup removes the staged file. Considered: undoing the upload after the fact, which would mean deleting a blob_uploads row and possibly a blob row and file that a concurrent identical upload may share, and is a second write that can fail. TestUploadOverHTTP now uses bytes no other upload carries for the two-part and trailing-field cases, and asserts no blob row and no file for them; against the old handler it fails on both. docs/07 says so.

## Summary

Landed the blob store, the upload path and the serve route, server-side, with all thirteen criteria met. Migration 0020 adds blob_uploads (insert-once, cascading from blobs), blobs.width and height, and the post_attachments projection (with a position column) in db.ProjectionTables. internal/store/blob stages uploads in blobs/tmp/ and commits them into place. internal/service/blobs holds MayUpload and Upload (type sniffed against the allowlist and DecodeConfig, the cap held while the bytes are read, every refusal removing its staged file, an orphan removed when the transaction fails), Check, AddRefs and DropRefs for posting, and Authorize, which asks threads.CanSeeAnyPostBody about each post naming the blob. Reply and CreateThread take attachments, raising refs before the append and dropping them when it fails, and a reply with attachments is projected before it answers. The page reads attachment metadata, taking an edited post's list from post_attachments. viewer.can_upload and the forum tree's can_upload are new. POST /api/v1/blobs streams multipart, and GET and HEAD /blobs/{hash} (internal/web/blobs) answer private, no-cache with access checked before any 304, plus ETag, Range, sandbox CSP, CORP and disposition. The upload body limit is the cap plus 64 KiB. The collector sweeps stale staged files and hour-old orphans no row and no log names, and never while held. Client dto.ts changed in its types only, and the dist is byte-identical. docs/03, 04 and 07 are updated. Thumbnails (TKT-01M29CYS9W), the composer (TKT-01M3V73J8K), drafts (TKT-01M3V73DP6) and the upload budget (TKT-01M3V73KM1) stay out of scope.
