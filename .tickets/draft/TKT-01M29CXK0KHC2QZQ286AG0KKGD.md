---
schema: 3
id: TKT-01M29CXK0KHC2QZQ286AG0KKGD
title: Build the content-addressed blob store and upload path
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-11T23:30:21Z
updated_at: 2026-10-01T07:59:44Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-groom-live
  name: ""
extensions: {}
---

## Description

`POST /api/v1/blobs` as multipart, writing to `blobs/sha256/xx/<hash>`, deduplicated by content hash, with a `blobs` row carrying size, MIME, uploader, refcount, and status. Serving is at `/blobs/{hash}` outside the API with immutable cache headers.

Attachments are referenced from the post body by hash, filename, size, and MIME type. They never enter the log itself.

MIME is decided by sniffing against an allowlist, not by what the upload claims. A file named `.png` that is something else is the oldest trick in the book.

## Acceptance criteria

- [ ] The same bytes uploaded by two members are one file and one blobs row with two blob_uploads rows, and attaching them to two posts makes the refcount 2 (TestUploadDedupOneFileOneRow, TestTwoPostsTwoRefs)
- [ ] The type is sniffed from the bytes, never the claim: only image/png, image/jpeg and image/gif that pass their DecodeConfig, application/pdf and text/plain are stored, and anything else is validation-failed type_not_allowed (TestUploadSniffsNotClaims, TestUploadRefusesOutsideAllowlist, TestUploadImageMustDecodeConfig)
- [ ] The size cap limits.attachment_mb is enforced during the read: a chunked body over it is 413 with no staged file left, and an honest Content-Length over it is refused before a byte is read (TestUploadCapDuringRead, TestUploadContentLengthOverCap)
- [ ] Upload needs a member with upload in the named forum: anonymous 401, pre-user 403, no grant 403, hidden forum 404, archived forum 403 except an admin (TestUploadPermission)
- [ ] A blob is served only to a viewer who may read the body of a post naming it, by the thread page's rules; every case in TestBlobServedOnlyToReaders passes, including deleted, moderator-hidden, hidden-pending-review, redacted, moved-to-an-unreadable-forum, suspended-author, group-grant moderator, unattached, quarantined and source-URL-only, and every refusal is identical to a missing hash
- [ ] A post attaches at most 10 blobs, each one the poster uploaded and not quarantined; AddBlobRef runs before the append, a collected blob is refused, and a failed append drops the references again (the TestAttach* tests)
- [ ] A reply with attachments is projected before it answers, so its blobs are servable at once (TestReplyWithAttachmentIsServableAtOnce)
- [ ] GET /blobs/{hash} answers with private immutable caching, the hash as ETag with 304 and Range support, nosniff, the sandbox CSP, CORP same-origin, and inline or attachment disposition (TestBlobHeaders)
- [ ] A quarantined blob is never served, and its bytes cannot be uploaded or attached again (TestBlobServedOnlyToReaders quarantined row, TestUploadOfQuarantinedBytesBlocked, TestAttachQuarantinedRefused)
- [ ] post_attachments is a projection that reindex rebuilds identically and a Redacted record clears (TestPostAttachmentsProjected, TestRedactedDropsPostAttachments, TestReindexRebuildsPostAttachments)
- [ ] A post's attachment shows the name its author uploaded it under, a second uploader never sees the first one's name, and a tombstone carries no attachments (TestSecondUploaderNeverSeesFirstName, TestTombstoneCarriesNoAttachments)
- [ ] The thread page's viewer and the forum tree carry can_upload, and the goldens and docs/04 and docs/07 are updated in the same PR (TestThreadViewerCanUpload, TestForumTreeCanUpload)

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
  - The answer is 201 `Attachment`, identical whether or not the bytes were known.
  - A post attaches at most 10 blobs, each one the poster uploaded.
  - A blob is served only to a viewer who may read the body of a post that names it, by the thread page's rules. Everyone else gets `not-found`, the uploader included.
  - A post's attachment shows the name its author uploaded it under.
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
- **The response cache.** Attachment metadata for a (hash, author) pair never changes once written, so the cached `posts` may carry it without a new key part. The `can_upload` flags live in the viewer block and the forum tree, and neither is cached (docs/07 "The response cache").
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
   - `UpsertBlobUpload`;
   - `BlobUploadedBy(hash, user_id)`;
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
   - `SweepTmp(root)`, which `serve` calls under the lock before it listens;
   - `Commit(tmp, root, hash)`, which renames into place and syncs the directory. When a file is already there, it removes the staged one instead, which is safe because the bytes are identical.
5. **`internal/service/blobs`** (new). `Service{DB, Root, Perm, Threads, Clock, Log}`.
   - **`Upload(ctx, v, target, name, r, max)`**:
     1. An anonymous viewer is refused `unauthenticated`. The target resolves to a forum, either through `threads.ReadAccess` or through the forum slug. Then `perm.Check(upload)`: Hidden is `not-found`, Deny is `forbidden`. `perm` already denies an archived forum to everyone but admins.
     2. Copy the part into the staged file through `io.LimitReader(r, max+1)` while hashing with SHA-256. More than `max` bytes is `payload-too-large`, and the staged file is removed.
     3. Sniff the first 512 bytes and check the allowlist. For an image, call `png`, `jpeg` or `gif`'s own `DecodeConfig` on the staged file, as the sniffed type says, never `image.DecodeConfig`. That gives the width and height, or the upload is refused.
     4. Clean the name: base name only, no control characters or path separators, at most 255 bytes of UTF-8, and `file` if nothing is left.
     5. `fsync` the staged file.
     6. In one `db.Write`, read the blob with `GetBlob`. A quarantined blob is `blocked`. A missing one is created with `CreateBlob`, and an existing one gets `RefreshBlob`. Then `UpsertBlobUpload(hash, v.UserID, name, now)` and `blob.Commit`.
     7. Answer `Attachment{hash, size, mime, name as sent, width, height}`.
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
10. **Blob deletion.** `BlobGC.collect` and `redactor.deleteBlobs` delete the blob row, and the `ON DELETE CASCADE` removes its `blob_uploads` rows in the same transaction. A test holds this rather than trusting the pragma.
11. **Docs.**
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
  - `TestUploadAnswerSameForKnownBytes`: the same status and shape, with `original_name` echoing this upload's name.
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
- **Headers.** `TestBlobHeaders`: `private, max-age=31536000, immutable`, `Vary: Cookie`, the hash as `ETag` with a 304 on `If-None-Match`, a 206 for `Range`, `nosniff`, `default-src 'none'; sandbox`, `Cross-Origin-Resource-Policy: same-origin`, `inline` for an image and `attachment` for a PDF.
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
