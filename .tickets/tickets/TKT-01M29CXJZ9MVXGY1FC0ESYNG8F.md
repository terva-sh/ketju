---
schema: 3
id: TKT-01M29CXJZ9MVXGY1FC0ESYNG8F
title: Build the offline hard-redaction tool
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area:storage
  - area:ops
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29BSPE2ZP7X2C736K2KVPBA
  - TKT-01M29C5Y1E782FS135JRVBFRJ6
blocks_on: none
references:
  - ref: design:storage-records
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:30:21Z
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

The CLI-only operation that removes body text from a log for a legal takedown or a GDPR erasure. It is the one thing in the system that is not append-only, which is why it is a command run by an admin with the server stopped rather than a button.

Shape follows the decision from the redaction spike. It rewrites the log, writes an audit_log entry, and leaves the ordinal, the tombstone, and every permalink intact.

The rendered-post cache and the search index both hold copies of the text, so redaction has to clear them too. A body erased from the log and still returned from a cache is a redaction that did not happen.

## Acceptance criteria

- [ ] The post keeps its ordinal and permalink and is served as a tombstone to members, its author and moderators; its history lists the revisions with no content; edit, delete and restore of it are conflict
- [ ] The command holds the data-directory lock for its whole run and refuses beside a running serve, and serve refuses to start while redaction.journal exists (TestRedactRefusesBesideServe, TestServeRefusesWhileAJournalExists)
- [ ] After the run post_meta marks the post deleted and it has no mentions rows, and a projection caught up from the new log equals one rebuilt by ketju reindex (TestRedactedCatchUpEqualsReindex); there is no search index in this build, and clearing it is TKT-01M3NS122F
- [ ] After ketju redact, no byte of any revision's source, mentions, edit reason or attachment list remains in any log that held the post or a copy of it, where the copies are the whole origin component found to a fixed point in both directions, and every frame still passes its CRC (TestRedactBlanksEveryRevision, TestRedactFollowsOriginBothWays, TestCopySetIsTransitive)
- [ ] Blobs only the redacted posts named are deleted and a blob another post still names stays (TestRedactDeletesOnlyUnsharedBlobs); quotes of the post are listed in the command's output and left unedited (TestRedactListsQuotes)
- [ ] Each log is rewritten once, and its new length is its old length plus exactly one 0x80 Redacted frame per target ordinal in it, so the thread's log_length and ETag afterwards exceed every earlier value (TestRedactKeepsEveryFrameLength, TestRedactTwoOrdinalsInOneLog, TestRedactedThreadPageIsNewer)
- [ ] An interruption after any step is finished by rerunning the command: a log already carrying the markers for all its journaled ordinals is skipped wherever those markers sit, one with only some is refused as damage, and the run ends with one Redacted record per target ordinal, one post.redacted row, and no journal (TestRedactResumesAfterEachStep, TestRedactTwoOrdinalsInOneLog)
- [ ] The journal records, before the first swap, each target's attachment hashes read from every Post and Edit frame of it, and all reference drops commit in one transaction with the post.redacted row, so a resumed run drops each reference exactly once and still deletes an unshared blob (TestAttachmentInventoryReadsEveryRevision, TestRedactResumeDropsEachReferenceOnce, TestRedactResumeAfterSwapStillDeletesTheBlob)
- [ ] The post.redacted audit row names the operator, who must be an active admin, the reason, every (thread, ordinal, revisions) cleared, the references dropped and blobs_planned; each blob actually deleted gets its own blob.deleted row in the transaction that deletes it, and no row claims a deletion that did not commit (TestRedactAuditsOnlyCommittedDeletes)
- [ ] A blob file is removed only after the transaction deleting its row and writing its blob.deleted row commits; a failure before that commit leaves the row and the file in place, and a resumed run removes any journaled blob file whose row is gone (TestRedactBlobDeleteFailureKeepsTheFile, TestRedactResumeRemovesAFileWhoseRowIsGone)
- [ ] A pending redaction.journal is resumed before any already-redacted check, so a rerun right after the named post's swap finishes, while naming a redacted post with no journal pending is refused (TestRedactResumeRightAfterTheSwap)
- [ ] ketju redact has two forms, the redaction and ketju redact --root DIR --resume [--dry-run], its usage text lists both, and the serve refusal names the --resume form; any other combination exits 2 (TestRedactUsage)
- [ ] --dry-run writes nothing in either form, and with a journal pending it reports the remaining work instead of resuming (TestRedactDryRunWritesNothing)

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a (TKT-01M3M9RR26, Phase 2 grooming). None of this exists yet: there is no `Redacted` type in the catalogue, no `origin` field, no journal, and no search index.

### Settled in grooming
Maintainer decisions of 2026-09-28, recorded in docs/03 "Hard redaction", docs/10 item 4 and the notes on TKT-01M29BSPE2:
- The format is "Rewrite with a Redacted record". The scope is "Every revision + attachments": the post's original and every revision's source, its mentions, and any attachment only it references (docs/10 item 4).
- The command runs offline, with serve stopped and the data-directory lock held. It writes a new log beside the old one, fsyncs it, renames it over the old one, then fsyncs the directory. It never edits a log in place.
- Every `Post` and `Edit` frame of the post keeps its type, ordinal, revision, timestamp and actor. Its body loses the text, and the frame is re-checksummed, so a CRC failure still always means corruption.
- `Redacted` is `0x80`, in the critical range (claimed in the docs/03 type table), and is appended naming the ordinal, the revisions cleared, the reason and the operator. An older build refuses the log.
- The reader shows a redacted post as a tombstone saying it was removed, and its history lists the revisions with no content.
- The command follows split/merge `origin` links in both directions and redacts every copy, rewriting each log it touches (review 1265).
- A redaction spanning several logs writes a journal to the data directory before touching any log, fsyncs the journal, then fsyncs the directory (reviews 1271 and 1278). `ketju serve` refuses to start while the journal exists. Rerunning the command resumes from it. A log that already carries the `Redacted` record for that ordinal is skipped, so a rerun never appends a second marker. docs/03 says "ends in"; review 1351 showed why that is not enough, below. The journal is deleted only after every swap and the audit row.
- Quotes of the post in other posts are listed for the operator and never edited.
- The action is written to `audit_log`, and the thread's index and projections are brought up to date.
- Types 20 and 21 are advisory, so a log carrying them is read by skipping them (docs/03, decided 2026-09-28).

Settled here as engineering choices, to be written into docs/03 in the same PR:
- **The log never gets shorter.** docs/03 "Caching" makes the log length the thread's version number: monotonic, the ETag, the response-cache key and the live-update offset. The client refuses a page whose `log_length` is lower than the one it holds (`storeThreadPage`, `web/client/src/state/threads.ts:144`), so a tab open across the redaction would keep showing the removed text. An ETag equal to an earlier length would also let a browser revalidate a cached copy that still holds the text. So each rewritten frame keeps its **exact original length**. Its body is the empty body of its type, keeping only `reply_to`, plus a `pad` key: a byte string of zeros (CBOR) or a string of `0` characters (JSON, encoding 2), sized so that the frame length is unchanged. The new log is therefore the old log's length plus the appended `Redacted` frames, one per target ordinal in that log. Every offset in the index, in `post_meta.log_offset` and in `projection_state` stays valid, and the index and projection bring themselves current by replaying only the appended frames. Alternatives that lost:
  - Compacting the frames and padding only the `Redacted` record. The pad would have to cover every byte removed, and a post with many 64 KB revisions exceeds `MaxFrameSize` (1 MiB). It also moves every later offset, which forces a full per-thread re-projection.
  - Compacting with no padding. The log shrinks: catch-up quarantines a log shorter than its projection (`catchup.go:162-169`), and the client refuses the page.
  To make the pad fit every size, the byte-string header is always written in its 5-byte form (`0x5a` followed by a uint32 length). This was checked during grooming: fxamacker/cbor v2.9.3, with the `decMode` options in `records.go`, decodes `{pad: 0x5a 00000002 0000}` into an empty `PostBody` without error. The smallest real body, `{source:"x"}`, is 10 bytes, which is enough for it. The CBOR is built by hand in one function, and a test decodes it with `DecodeBody` and with the frame parser.
- **`reply_to` is kept.** It is the thread's structure, not the post's text, and docs/10 item 4 does not list it among what is removed. Keeping it keeps reply counts and the index `ReplyTo` unchanged. An `Edit`'s `reason` is removed, because it is the author's free text. `Delete` and `Restore` frames are left alone.
- **Index flag bit 6 is `EntryRedacted`.** It uses the spare flag capacity docs/03 names ("the 10 unused bits of the flags field"). `Visible()` excludes it. Applying `Redacted` sets it and clears `EntryHasAttachments`, so an index extended by the one frame equals an index rebuilt from the new log.
- **Settled by Terva review 1345 on PR #139 (three high findings). Each changes docs/03 "Hard redaction", and the implementing PR updates docs/03 to say so.**
  - *The copy set is the whole origin component, found to a fixed point.* Following `origin` one step back and one step forward misses a copy of a copy: if B came from A and C from B, naming A or B would leave C's text. The command builds the origin graph over every post in every log and takes the connected component that holds the named post, following edges in both directions until no new post is added. That covers ancestors, descendants of copies and sibling copies. docs/03 "Copies from split and merge" says "in both directions" and must add "transitively, to a fixed point".
  - *The journal holds the attachment hashes.* After a swap, the post's bodies no longer list their attachments, and a resumed run skips a log that already ends in `Redacted`, so it could not tell which references to drop or which blobs to delete. Planning reads every target's attachment hashes from the logs as they are *before* any rewrite, and the journal records them per target: `targets [{thread, ordinal, attachments []}]`. The journal is fsynced, and its directory entry made durable, before the first swap. From then on the journal, never the logs, is the source of the hashes. docs/03 "Several logs, one redaction" says the journal "lists every (thread, ordinal) to clear" and must add the hashes.
  - *Reference drops are exactly once.* A refcount above zero does not say whether this redaction already dropped its reference, so a resumed run could drop a second one and free a blob another post still needs. Every `DropBlobRef` of the redaction and the audit row are written in **one** `db.Write` transaction, which first checks `AuditExists` inside the same transaction and does nothing if the row is there. The drops and the audit row therefore commit together or not at all, and the audit row is the marker that they happened. Deleting the blob files comes after that commit, per blob, and is idempotent. A blob with no row is skipped, and every delete condition is checked again. docs/03 "Several logs, one redaction" names only the journal and the `Redacted` marker as resume points and must add this one. The alternative that lost was a per-drop progress entry in the journal. A journal write and a database commit cannot be made atomic, so a crash between them would still drop twice or not at all.
- **Settled by Terva review 1351 on PR #139 (head 630564a). Each changes docs/03 "Hard redaction", and the implementing PR updates docs/03 to say so.**
  - *One rewrite per log, and the log is the resume marker.* (high) When the copy set holds several ordinals in one thread, processing them one at a time leaves only the last marker at the end of the log. A resumed run checking "ends in" would then rewrite an earlier ordinal again and append a second marker for it. So the targets are grouped by thread, and each log is rewritten **once**: every target ordinal in it is blanked, and one `Redacted` frame per ordinal is appended, in ordinal order, in the same new file. The swap is atomic, so a log carries either all of its markers or none. On resume, `RedactedOrdinals(log) map[uint32]bool` scans the whole log for `Redacted` records. A log whose journaled ordinals are all marked is done. A log with none is rewritten. A log with some but not all refuses the run as damage, because no atomic swap can leave it that way. The alternative, a per-log "done" state in the journal fsynced after each swap, lost: a crash between the swap and the journal write leaves a swapped log marked not done, so the log check is still needed, and the journal state would be a second record of the same fact with a window where the two disagree. The log is the one thing the swap makes atomic, so it is the marker. docs/03 "Several logs, one redaction" says a log that "ends in the `Redacted` record" is skipped, and must say "carries the `Redacted` records for every ordinal the journal lists for it" and that one log is rewritten once.
  - *The attachment inventory reads every revision.* (high) Planning collected hashes from `Post` records only. In this build an `Edit` has no attachments field (`EditBody`, `internal/store/threadlog/records.go:128-133`; docs/03 "Blob garbage collection": "an edit cannot drop one, because `Edit` has no attachments field"), so no edit can add one today. The finding is still taken, because body maps are open to new fields without a format bump. Planning decodes the `attachments` key of **every** `Post` and `Edit` frame of each target, not just the `Post`, through one function, `attachmentsOf(h, body) []string`, and a test adds an `attachments` key to an `Edit` body fixture. The journal holds the union per target. Reference counting stays per post: refcount counts posts that refer to a blob (docs/03 "Attachments"), so each distinct hash is dropped once per target however many of its revisions list it.
  - *The audit row records what was committed, not what is still to come.* (medium) The `post.redacted` row is written in the transaction that drops the references, before any blob file is deleted. So its details list the references dropped and the blobs *planned* for deletion (`blobs_planned`), never "blobs deleted". Each blob actually deleted gets its own row, `action = "blob.deleted"`, `target_type = "blob"`, `target_id = <hash>`, `details_json` naming the redaction's journal id. It is written in the same `db.Write` that deletes the blob's database row, so the audit row exists exactly when the deletion committed. A blob that turns out to be still referenced gets no row, and the command prints it as kept. Updating the first row after cleanup lost because `audit_log` is append-only by trigger (migration 0006).
- **Settled by Terva review 1360 on PR #139 (head 630a3a3). Both change docs/03 "Hard redaction", and the implementing PR updates docs/03 to say so.**
  - *Blob files are removed only after their row's deletion commits.* (high) A rename to `.gc` inside `db.Write` is not rolled back with the transaction. So the transaction touches only the database: it deletes the blob's row and appends its `blob.deleted` row. No file is moved inside it, and a failed commit leaves the row and the file exactly as they were. After the commit, the command removes `blobs/sha256/xx/<hash>` and any leftover `<hash>.gc`. On resume, every journal hash whose row is gone and whose file (or `.gc`) is still on disk is removed. That is the crash between commit and remove. `BlobGC.collect` moves the file aside inside the transaction because it runs beside live uploads, and bytes must not be removed while a concurrent upload could still commit a row for them. The redaction holds the data-directory lock, and serve refuses to start while the journal exists, so no upload can recreate a row between the commit and the file removal. The shorter sequence is safe here, and it is not shared with `BlobGC`. The alternative that lost was reusing `collect`'s rename-and-undo: it keeps the window the finding names, and relies on an undo that can itself fail.
  - *A pending journal is checked before the "already redacted" refusal.* (medium) After a swap, the named post carries `EntryRedacted`. A rerun that refused such a post before looking for the journal could never finish the pending work. So the command looks for `redaction.journal` first. If it exists and names the same post (or `--resume` is given), the run resumes, and no check on the post's state is made. The refusal of an already-redacted post applies only when no journal is pending.
- **Settled by Terva review 1366 on PR #139 (head a9bfd8c).**
  - *The command has two forms, and `--resume` is one of them.* (medium) `ketju redact --root DIR --operator HANDLE --reason TEXT [--dry-run] THREAD ORDINAL` starts a redaction. `ketju redact --root DIR --resume [--dry-run]` finishes the pending one and takes no other flag or argument, because the operator, the reason and the targets are all in the journal. Any other combination is a usage error (exit 2) that names both forms. Naming the same post as the pending journal also resumes it, which is kept so an operator who simply reruns their command finishes the job. Naming a different post while a journal is pending is refused (exit 2), naming the pending post and `--resume`. The serve refusal and the README tell the operator to run `ketju redact --root DIR --resume`.
  - *A dry run never resumes.* (medium) `--dry-run` is checked before the journal is acted on, and it writes nothing in any form. With a journal pending, `--dry-run` (with `--resume`, or naming the pending post) reads the journal and the logs and prints the remaining work: each log swapped or not, from its markers; whether the reference-and-audit transaction has committed, from `AuditExists`; and each blob's row and file state. It changes nothing and exits 0. `--dry-run` naming a different post with a journal pending is refused as the real run would be. The alternative, refusing every dry run while a journal is pending, lost because inspecting the remaining work is exactly what an operator needs before resuming.
- **The audit row** is `action = "post.redacted"`, `target_type = "post"`, `target_id = "<thread base32>/<ordinal>"` of the post the operator named, with `actor_id` set to the operator. `details_json` holds the reason, the journal id, every (thread, ordinal, revisions) cleared, the references dropped, the blobs planned for deletion (`blobs_planned`), the quotes listed, and `via: "cli"`. A post can be redacted only once, so `AuditExists` on that target and action is the resume marker.
- **The operator** is named with `--operator HANDLE`, which must be an active admin (docs/08: "admin, CLI only"). The reason is required.

### Approach
1. **Catalogue** (`internal/store/threadlog/records.go`): `TypeRedacted RecordType = 0x80`, and `RedactedBody{Ordinal uint32 "ordinal"; Revisions []uint32 "revisions"; Reason string "reason"; Operator uint64 "operator"}`, added to `catalogue` and `typeNames`. Add `PostBody.Origin *PostOrigin` (`origin`, `{thread_id uint64, ordinal uint32}`), the shape docs/03 gives for split and merge. This ticket reads it and TKT-01M29CXJZZ writes it. Update the docs/03 type table row, the frame-flags/index-entry table (bit 6), and "Hard redaction" with the padding and log-length rule above.
2. **Index** (`apply.go`, `flags.go`): a `*RedactedBody` case in `Index.Apply`, with `postFlagEffect(TypeRedacted)` returning set `EntryRedacted`, clear `EntryHasAttachments`. Add `EntryRedacted = 1 << 6` to `EntryFlagsKnown`. `Visible()` excludes `EntryRedacted`.
3. **Reader and service**:
   - `Reader.HiddenEntry` returns the tombstone for a redacted entry, so a moderator never decodes the padded body.
   - `service/threads/view.go` skips the moderator `HiddenEntry` path for redacted posts.
   - `History` (`service/threads/posts.go:137`) lists revisions with empty `source` and `reason` for a moderator. A member sees the same as for a deleted post.
   - `dto.Post` and `dto.PostHistory` gain `redacted bool` (omitempty), with goldens, and docs/07 gains a sentence on it.
   - `service/threads/mutate.go` adds `EntryRedacted` to the `Append.Refuse` set of Edit, Delete and Restore, so each is a `conflict` (`threadlog.ErrPostState`).
4. **Projection** (`internal/events/projection/apply.go`): `applyRedacted` sets `post_meta.deleted = 1` for the ordinal and deletes its `mentions` rows, so catch-up of the one frame equals a reindex of the new log. Add the type to the decisions in `TestEveryRecordTypeHasADecision` (`writer_test.go:261`).
5. **Rewrite** (new `internal/store/threadlog/redact.go`): `RedactLog(src, dst *os.File, targets []RedactedBody) (Rewrite, error)` takes every target ordinal in the log. It walks the source with `ScanFrames`, copies every frame, and for each `Post`/`Edit` of a target ordinal writes `blankBody(h, body, len)` through `AppendFrame`, which recomputes the CRC. It then appends one `Redacted` frame per target, in ordinal order. It refuses a source with any CRC failure or unknown critical record: redaction runs on a sound log or not at all, so fsck first. `Rewrite` reports the revisions cleared, the attachment hashes removed and the origin found. `RedactedOrdinals(log)` answers the resume check by scanning the whole log for `Redacted` records.
6. **Command** (new `cmd/ketju/redact.go`, registered in `main.go`), in two forms: `ketju redact --root DIR --operator HANDLE --reason TEXT [--dry-run] THREAD ORDINAL`, where THREAD is the base32 id, and `ketju redact --root DIR --resume [--dry-run]`. `--resume` with `--operator`, `--reason` or any argument, or the first form missing any of its parts, is a usage error (exit 2). The usage text lists both forms and the exit codes.
   - It takes `lock.Acquire(root, "redact")` for the whole run. A `HeldError` is refused with the same wording and exit code reindex uses (stop ketju serve first).
   - It opens the database with `openDatabase(..., migrate=false)` and refuses when `needDatabase` fails.
   - It first looks for `<root>/redaction.journal`. If one exists and the run is `--resume` or names the journal's post, it resumes (step 7), with no check on the post's state. A dry run instead reports the remaining work and writes nothing (settled above). A different post is refused. `--resume` with no journal is a usage error saying nothing is pending.
   - With no journal pending, it resolves the operator, who must be an active admin, and refuses a post already carrying `EntryRedacted`.
   - Planning:
     - Collect the copy set: one scan of every log under `threads/` (as `jobs.referencedInLogs` walks them) builds the origin edges `(thread, ordinal) -> origin` for every `Post` that has one. The copy set is the connected component that holds the named post, following edges both ways from a work queue until it adds nothing new (`copySet(edges, named) []PostRef` in `cmd/ketju/redact.go`, a pure function).
     - List quotes: every revision of every post in each affected thread whose source quotes the ordinal. Export the quote regex from `internal/render/quote.go` as `render.QuotedOrdinals(source) []uint32`, because quotes are same-thread only.
     - Collect the attachment hashes of every target from **every** `Post` and `Edit` frame of that ordinal (`attachmentsOf`), from the logs as they are before any rewrite. The union per target goes in the journal.
   - `--dry-run` prints the plan and writes nothing, in either form and whether or not a journal is pending.
7. **Journal**: `<root>/redaction.journal`, JSON `{id (ULID), operator_id, reason, created_at, named {thread, ordinal}, targets [{thread, ordinal, attachments []}], quotes [...]}`. It is written to a temp file, fsynced, renamed into place, and the directory fsynced, all before any log is touched. If a journal exists, `ketju redact` resumes it, when run as `--resume` or when named with the same target, and not under `--dry-run`. A different target is refused (exit 2), naming the pending one and `--resume`. Any leftover `<id>.log.redact` temp file is removed on resume.
8. **Per log**, with the journal's targets grouped by thread, in thread-id order:
   - `RedactedOrdinals`: every journaled ordinal of this log is marked, so skip it; none, so rewrite it; some, so refuse as damage (exit 1) and leave the journal in place.
   - To rewrite, write `<id>.log.redact` beside the log, fsync it, rename it over `<id>.log`, and fsync the shard directory. Use one helper, `fsutil.ReplaceFile`, in a new `internal/fsutil` package, with an exported `SyncDir`. Today's `syncDir` is unexported in `internal/store/db/db.go:88`, and the index writers in fsck and `threadcache` do not fsync the directory.
   - Then bring the index current: `threadlog.Open` (which reports `Extended`), then write the `.idx` through `ReplaceFile`.
   - Then bring the projection current with `projection.Writer.ProjectThread(ctx, id, size)` (`writer.go:375`), which projects the appended frames.
9. **References and audit, one transaction.** In one `db.Write`: check `AuditExists`, and if the row is there, do nothing. Otherwise, for each (target, hash) in the journal, run `DropBlobRef` once. That is one reference per post that listed the hash, as refcount counts posts (docs/03 "Attachments"). A row already at zero, or missing, is skipped and noted in the audit details as drift, not driven below zero. Then `AppendAudit` of the `post.redacted` row as settled above, with `blobs_planned` and no claim of deletion.
10. **Blob files**, from the journal's hashes, after that commit: delete a blob only when no log names it (`referencedInLogs`, exported from `internal/jobs`), no avatar names it, and no draft names it. One `db.Write` per blob deletes its row (`DeleteCollectableBlob` with the cutoff set to now, which re-checks refcount zero and no draft or avatar naming it) and appends its `blob.deleted` row. No file is touched inside that transaction. After it commits, remove the blob file and any `<hash>.gc`, then fsync the shard directory. On resume, a journal hash with no row has its file and `.gc` removed if still present, and gets no second `blob.deleted` row. There is no seven-day grace. A redaction is deliberate, and the grace exists for uploads not yet attached. A blob another post still names stays.
11. **Finish**: delete the journal and fsync the directory. Print the quotes list and a reminder that backups and filesystem snapshots still hold the old bytes.
12. **Serve**: a `redaction` step in `serve()` (`cmd/ketju/serve.go`) right after the lock step (`:212-223`) and before the database step. It refuses to start while `<root>/redaction.journal` exists, naming `ketju redact --root DIR --resume`.
13. **Docs**: README gains an operations entry for `ketju redact`. docs/03 is updated as in step 1, and "Hard redaction" gains the three review-1345 rules above: the transitive copy set, the hashes in the journal, and the audit row as the reference-drop marker.

### Tests
- threadlog:
  - `TestRedactKeepsEveryFrameLength` (the new log's length is the old length plus the `Redacted` frames, and every other frame is byte-identical);
  - `TestRedactTwoOrdinalsInOneLog`: a copy set with two ordinals in one thread gives one rewrite with two markers. A resume after that swap skips the log. A log hand-built with only one of the two markers is refused as damage;
  - `TestAttachmentInventoryReadsEveryRevision`: an `Edit` body fixture carrying an `attachments` key contributes its hash to the journal, and a hash listed by both the `Post` and an `Edit` is dropped once;
  - `TestRedactBlobDeleteFailureKeepsTheFile`: a failure injected in the blob transaction before commit leaves the row, the file at its original path, no `.gc`, and no `blob.deleted` row. The rerun deletes it once;
  - `TestRedactResumeRemovesAFileWhoseRowIsGone`: a failure after the commit and before the file is removed. The rerun removes the file and writes no second `blob.deleted` row;
  - `TestRedactResumeRightAfterTheSwap`: a failure right after the named post's log is swapped. A rerun naming the same post resumes and finishes, and is not refused as already redacted. With no journal, naming a redacted post is refused;
  - `TestRedactAuditsOnlyCommittedDeletes`: of two planned blobs, one is still referenced elsewhere. The `post.redacted` row lists both under `blobs_planned`, exactly one `blob.deleted` row exists, and a failure injected before the blob transaction commits leaves no `blob.deleted` row;
  - `TestRedactBlanksEveryRevision` (no byte of any revision's source, mention or reason survives; a grep over the file finds none);
  - `TestRedactedBodiesDecode` (CBOR and JSON, the smallest body, and bodies across the bstr header boundaries);
  - `TestRedactRefusesADamagedLog`;
  - `TestRedactedIndexExtendedEqualsRebuilt`;
  - `TestRedactedIsCritical` (an older catalogue without it refuses the thread). Change `TestEveryCatalogueTypeIsAdvisory` (`unknown_type_test.go:50`) to allow exactly `TypeRedacted`, with a comment citing docs/03.
- projection: `TestRedactedCatchUpEqualsReindex` compares a `dbtest` dump after catch-up of the new log with one after a full reindex.
- service/API:
  - a redacted post is a tombstone for a member, for its author and for a moderator;
  - history lists revisions with no content;
  - edit, delete and restore are `conflict`;
  - goldens for `redacted`;
  - `TestRedactedThreadPageIsNewer`: the page's `log_length` and ETag after the redaction are strictly greater than every earlier one.
- cmd/ketju:
  - `TestRedactRefusesBesideServe` (lock held);
  - `TestServeRefusesWhileAJournalExists`;
  - `TestRedactNeedsAnAdminOperatorAndAReason`;
  - `TestRedactDryRunWritesNothing`: in both forms, with no journal and with a journal pending at each resume point. The data directory's files (logs, indexes, blobs, the journal) and the database's SHA-256 are unchanged, and the pending run's report names the remaining work;
  - `TestRedactUsage`: `--resume` with an operator, reason or argument, `--resume` with no journal, the first form with a part missing, and a different post while a journal is pending, each exit 2 with the message naming both forms or the pending post. `--resume` alone finishes a pending run;
  - `TestRedactFollowsOriginBothWays` (fixture logs with `origin` written through the codec: naming the copy clears the source, and naming the source clears every copy);
  - `TestCopySetIsTransitive`: a chain A to B to C, a tree with two copies of one copy, and a cycle-free diamond from merges. Naming any member returns the whole component, and naming an unrelated post returns only itself;
  - `TestRedactResumeDropsEachReferenceOnce`: a blob named by the redacted post and by two others, refcount 3. Failures are injected after the swap, before the transaction commits, and after it commits. After each rerun, refcount is 2, the blob stays, and the audit row is written once;
  - `TestRedactResumeAfterSwapStillDeletesTheBlob`: a failure after every log is swapped and before the transaction. The rerun reads the hashes from the journal, and the unshared blob is deleted;
  - `TestRedactListsQuotes`;
  - `TestRedactDeletesOnlyUnsharedBlobs`;
  - `TestRedactWritesOneAuditRow`;
  - `TestRedactResumesAfterEachStep`, which injects a failure after the journal, after each rename, before and after the directory fsync, after the projection, after the reference-and-audit transaction, and after each blob file delete. It reruns each case and asserts one `Redacted` record per target ordinal, one `post.redacted` row, one `blob.deleted` row per deleted blob, no journal left, and serve then starts and serves the tombstone;
  - `TestRedactRefusesASecondRedaction`.

### Out of scope
- The search index. There is no FTS table in this build; it arrives with TKT-01M29CRJ3B (phase-3). Clearing it on redaction is TKT-01M3NS122F (Clear the search index in the hard-redaction tool), which depends on both.
- The in-memory render and response caches. They live only in a serve process, and this command runs only while none holds the lock; the tests above prove the restarted server serves the tombstone.
- Writing `origin` on split and merge. TKT-01M29CXJZZ, which now depends on this ticket.
- Thumbnails, which have no name yet (docs/03 "Blob garbage collection"). Upload and attach are TKT-01M29CXK0 (phase-3).
- A thread title. It lives in `ThreadCreated` and `ThreadRenamed` bodies, and docs/03 does not say how it is redacted. That is TKT-01M3NS147C (Decide how a thread title is hard-redacted).
- Backups and filesystem snapshots, which the command reminds the operator of and cannot reach.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:10:10Z

Grooming (TKT-01M3M9RR26), 2026-09-29: the plan settles one point where docs/03 "Hard redaction", as written, would break another part of docs/03.

docs/03 "Caching" makes the log length the thread's version number: monotonic, and used for the ETag, the response-cache key and the live-update offset. Rewriting frames with the bare empty body shrinks the log. When that happens:
- the client refuses the new page as older than the one it holds (`storeThreadPage`, `web/client/src/state/threads.ts:144`), so a tab open across the redaction keeps showing the removed text;
- catch-up quarantines a log shorter than its projection (`catchup.go:162-169`);
- an ETag can repeat an earlier one.

The plan therefore keeps every rewritten frame at its original length, padding the empty body with a zero-filled `pad` key, and the new log is the old one plus the `Redacted` frame. The two alternatives that lost, with reasons, are in the plan. docs/03 "Hard redaction" says each body is "replaced by the empty body of that type", so it must say "padded to the frame's original length" in the same PR. The CBOR decoder's acceptance of the 5-byte bstr header the padding relies on was checked during grooming against fxamacker/cbor v2.9.3.

This ticket also adds `PostBody.origin`, the shape docs/03 gives, and TKT-01M29CXJZZ (Implement split and merge records in the thread log) now depends on this ticket so that it writes the same field.

**agent:claude-code/phase2-groom** at 2026-09-29T05:38:20Z

Terva review 1345 on PR #139 (head e2ce3eb, base 7af2c5a) found three high problems in this plan, all accepted and fixed in it. First, the copy set is now the whole origin component, found to a fixed point, not only one step each way. Second, the journal records every target's attachment hashes before the first swap. Third, all DropBlobRef calls commit in one transaction with the audit row, which is the resume marker, so a reference is dropped exactly once. Each changes docs/03 'Hard redaction', which the implementing PR updates. Criteria 7 to 9 replace the old first and seventh.

**agent:claude-code/phase2-groom** at 2026-09-29T05:56:24Z

Terva review 1351 on PR #139 (head 630564a, base 7af2c5a) marked the three findings of review 1345 resolved and raised three more, all accepted and fixed in the plan. finding-1 (high): each log is now rewritten once, with a marker per target ordinal in it. Resume scans the whole log for markers instead of reading only its last frame, and a log carrying only some of its markers is refused as damage. A per-log 'done' state in the journal lost, because the swap and the journal write cannot be made atomic. finding-2 (high): the attachment inventory reads every Post and Edit frame. Edit has no attachments field today, so this is hardening against a future body field. finding-3 (medium): the post.redacted row records blobs_planned, and each deleted blob gets its own blob.deleted row in the transaction that deletes it. Criteria 6 to 9 replace the old second, fourth, fifth and eighth. The implementing PR updates docs/03 'Hard redaction' for all three.

**agent:claude-code/phase2-groom** at 2026-09-29T06:18:32Z

Terva review 1360 on PR #139 (head 630a3a3, base 7af2c5a) raised two findings, both accepted and fixed in the plan. finding-1 (high): the blob transaction now deletes the row and writes blob.deleted with no file operation. The file is removed only after commit, and a resumed run removes any journaled file whose row is gone. BlobGC.collect's rename-inside-transaction is not reused: it exists for live uploads, which the lock and the journal rule out here. finding-2 (medium): a pending journal is resumed before the already-redacted refusal, which now applies only with no journal pending. Criteria 10 and 11 were added. The implementing PR updates docs/03 'Hard redaction' for both.

**agent:claude-code/phase2-groom** at 2026-09-29T06:32:13Z

Terva review 1366 on PR #139 (head a9bfd8c, base 7af2c5a) raised two medium findings, both accepted and fixed in the plan. finding-1: --dry-run is checked before the journal is acted on. With a journal pending, it reports the remaining work (logs swapped or not, whether the audit transaction committed, the state of each blob) and writes nothing. Refusing every dry run lost, because inspecting the remaining work is what an operator needs before resuming. finding-2: the command has two specified forms, and the second is 'ketju redact --root DIR --resume [--dry-run]', which takes no other flag or argument. Invalid combinations exit 2, and the serve refusal names that form. Criteria 12 and 13 were added.
