---
schema: 3
id: TKT-01M29CXJZ9MVXGY1FC0ESYNG8F
title: Build the offline hard-redaction tool
type: task
status: draft
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
updated_at: 2026-09-29T05:10:10Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

The CLI-only operation that removes body text from a log for a legal takedown or a GDPR erasure. It is the one thing in the system that is not append-only, which is why it is a command run by an admin with the server stopped rather than a button.

Shape follows the decision from the redaction spike. It rewrites the log, writes an audit_log entry, and leaves the ordinal, the tombstone, and every permalink intact.

The rendered-post cache and the search index both hold copies of the text, so redaction has to clear them too. A body erased from the log and still returned from a cache is a redaction that did not happen.

## Acceptance criteria

- [ ] After ketju redact, no byte of any revision's source, mentions, edit reason or attachment list for the post remains in any log that held it (the named post and every copy found through origin, in both directions), and every frame still passes its CRC (TestRedactBlanksEveryRevision, TestRedactFollowsOriginBothWays)
- [ ] The post keeps its ordinal and permalink and is served as a tombstone to members, its author and moderators; its history lists the revisions with no content; edit, delete and restore of it are conflict
- [ ] Each rewritten log is its old length plus exactly the appended 0x80 Redacted frame, so the thread's log_length and ETag afterwards exceed every earlier value (TestRedactKeepsEveryFrameLength, TestRedactedThreadPageIsNewer)
- [ ] The command holds the data-directory lock for its whole run and refuses beside a running serve, and serve refuses to start while redaction.journal exists (TestRedactRefusesBesideServe, TestServeRefusesWhileAJournalExists)
- [ ] An interruption after any step is finished by rerunning the command, leaving one Redacted record per log, one audit row and no journal (TestRedactResumesAfterEachStep)
- [ ] One audit_log row with action post.redacted names the operator, who must be an active admin, the reason, and every (thread, ordinal, revisions) cleared
- [ ] Blobs only the redacted post named are deleted and a blob another post still names stays (TestRedactDeletesOnlyUnsharedBlobs); quotes of the post are listed in the command's output and left unedited (TestRedactListsQuotes)
- [ ] After the run post_meta marks the post deleted and it has no mentions rows, and a projection caught up from the new log equals one rebuilt by ketju reindex (TestRedactedCatchUpEqualsReindex); there is no search index in this build, and clearing it is TKT-01M3NS122F

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
- A redaction spanning several logs writes a journal to the data directory before touching any log, fsyncs the journal, then fsyncs the directory (reviews 1271 and 1278). `ketju serve` refuses to start while the journal exists. Rerunning the command resumes from it. A log that already ends in the `Redacted` record for that ordinal is skipped, so a rerun never appends a second marker. The journal is deleted only after every swap and the audit row.
- Quotes of the post in other posts are listed for the operator and never edited.
- The action is written to `audit_log`, and the thread's index and projections are brought up to date.
- Types 20 and 21 are advisory, so a log carrying them is read by skipping them (docs/03, decided 2026-09-28).

Settled here as engineering choices, to be written into docs/03 in the same PR:
- **The log never gets shorter.** docs/03 "Caching" makes the log length the thread's version number: monotonic, the ETag, the response-cache key and the live-update offset. The client refuses a page whose `log_length` is lower than the one it holds (`storeThreadPage`, `web/client/src/state/threads.ts:144`), so a tab open across the redaction would keep showing the removed text. An ETag equal to an earlier length would also let a browser revalidate a cached copy that still holds the text. So each rewritten frame keeps its **exact original length**. Its body is the empty body of its type, keeping only `reply_to`, plus a `pad` key: a byte string of zeros (CBOR) or a string of `0` characters (JSON, encoding 2), sized so that the frame length is unchanged. The new log is therefore the old log's length plus the one appended `Redacted` frame. Every offset in the index, in `post_meta.log_offset` and in `projection_state` stays valid, and the index and projection bring themselves current by replaying one frame. Alternatives that lost:
  - Compacting the frames and padding only the `Redacted` record. The pad would have to cover every byte removed, and a post with many 64 KB revisions exceeds `MaxFrameSize` (1 MiB). It also moves every later offset, which forces a full per-thread re-projection.
  - Compacting with no padding. The log shrinks: catch-up quarantines a log shorter than its projection (`catchup.go:162-169`), and the client refuses the page.
  To make the pad fit every size, the byte-string header is always written in its 5-byte form (`0x5a` followed by a uint32 length). This was checked during grooming: fxamacker/cbor v2.9.3, with the `decMode` options in `records.go`, decodes `{pad: 0x5a 00000002 0000}` into an empty `PostBody` without error. The smallest real body, `{source:"x"}`, is 10 bytes, which is enough for it. The CBOR is built by hand in one function, and a test decodes it with `DecodeBody` and with the frame parser.
- **`reply_to` is kept.** It is the thread's structure, not the post's text, and docs/10 item 4 does not list it among what is removed. Keeping it keeps reply counts and the index `ReplyTo` unchanged. An `Edit`'s `reason` is removed, because it is the author's free text. `Delete` and `Restore` frames are left alone.
- **Index flag bit 6 is `EntryRedacted`.** It uses the spare flag capacity docs/03 names ("the 10 unused bits of the flags field"). `Visible()` excludes it. Applying `Redacted` sets it and clears `EntryHasAttachments`, so an index extended by the one frame equals an index rebuilt from the new log.
- **The audit row** is `action = "post.redacted"`, `target_type = "post"`, `target_id = "<thread base32>/<ordinal>"` of the post the operator named, with `actor_id` set to the operator. `details_json` holds the reason, the journal id, every (thread, ordinal, revisions) cleared, the blobs deleted, the quotes listed, and `via: "cli"`. A post can be redacted only once, so `AuditExists` on that target and action is the resume marker.
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
5. **Rewrite** (new `internal/store/threadlog/redact.go`): `RedactLog(src, dst *os.File, ordinal uint32, r RedactedBody) (Rewrite, error)` walks the source with `ScanFrames`, copies every frame, and for each `Post`/`Edit` of the ordinal writes `blankBody(h, body, len)` through `AppendFrame`, which recomputes the CRC. It then appends the `Redacted` frame. It refuses a source with any CRC failure or unknown critical record: redaction runs on a sound log or not at all, so fsck first. `Rewrite` reports the revisions cleared, the attachment hashes removed and the origin found. `EndsWithRedacted(log, ordinal)` answers the resume check by reading the final frame.
6. **Command** (new `cmd/ketju/redact.go`, registered in `main.go`): `ketju redact --root DIR --operator HANDLE --reason TEXT [--dry-run] THREAD ORDINAL`, where THREAD is the base32 id.
   - It takes `lock.Acquire(root, "redact")` for the whole run. A `HeldError` is refused with the same wording and exit code reindex uses (stop ketju serve first).
   - It opens the database with `openDatabase(..., migrate=false)` and refuses when `needDatabase` fails.
   - It resolves the operator, who must be an active admin, and refuses a post already carrying `EntryRedacted`.
   - Planning:
     - Collect the copy set: follow `origin` from the named post back to its source, and find every post in any log whose `origin` names the source. The second is a scan of every log under `threads/`, as `jobs.referencedInLogs` does.
     - List quotes: every revision of every post in each affected thread whose source quotes the ordinal. Export the quote regex from `internal/render/quote.go` as `render.QuotedOrdinals(source) []uint32`, because quotes are same-thread only.
     - Collect the attachment hashes.
   - `--dry-run` prints the plan and writes nothing.
7. **Journal**: `<root>/redaction.journal`, JSON `{id (ULID), operator_id, reason, created_at, named {thread, ordinal}, targets [{thread, ordinal}], quotes [...]}`. It is written to a temp file, fsynced, renamed into place, and the directory fsynced, all before any log is touched. If a journal exists, `ketju redact` resumes it: with `--resume`, or when named with the same target. A different target is refused (exit 2), naming the pending one. Any leftover `<id>.log.redact` temp file is removed on resume.
8. **Per target**, in journal order:
   - If `EndsWithRedacted`, skip the rewrite.
   - Otherwise write `<id>.log.redact` beside the log, fsync it, rename it over `<id>.log`, and fsync the shard directory. Use one helper, `fsutil.ReplaceFile`, in a new `internal/fsutil` package, with an exported `SyncDir`. Today's `syncDir` is unexported in `internal/store/db/db.go:88`, and the index writers in fsck and `threadcache` do not fsync the directory.
   - Then bring the index current: `threadlog.Open` (which reports `Extended`), then write the `.idx` through `ReplaceFile`.
   - Then bring the projection current with `projection.Writer.ProjectThread(ctx, id, size)` (`writer.go:375`), which projects the one appended frame.
9. **Blobs**: for each hash removed, run `DropBlobRef` once per redacted post that listed it, when the row's refcount is above zero. Then delete the blob only when no log names it (`referencedInLogs`, exported from `internal/jobs`), no avatar names it, and no draft names it. The delete uses the same row-delete, `.gc` rename and remove-after-commit sequence as `BlobGC.collect`, factored into a shared helper and without the seven-day grace. A redaction is deliberate, and the grace exists for uploads not yet attached. A blob another post still names stays.
10. **Audit and finish**: `AppendAudit` as settled above, skipped when `AuditExists` already holds it. Then delete the journal and fsync the directory. Print the quotes list and a reminder that backups and filesystem snapshots still hold the old bytes.
11. **Serve**: a `redaction` step in `serve()` (`cmd/ketju/serve.go`) right after the lock step (`:212-223`) and before the database step. It refuses to start while `<root>/redaction.journal` exists, naming `ketju redact --resume`.
12. **Docs**: README gains an operations entry for `ketju redact`. docs/03 is updated as in step 1.

### Tests
- threadlog:
  - `TestRedactKeepsEveryFrameLength` (the new log's length is the old length plus the `Redacted` frame, and every other frame is byte-identical);
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
  - `TestRedactDryRunWritesNothing`;
  - `TestRedactFollowsOriginBothWays` (fixture logs with `origin` written through the codec: naming the copy clears the source, and naming the source clears every copy);
  - `TestRedactListsQuotes`;
  - `TestRedactDeletesOnlyUnsharedBlobs`;
  - `TestRedactWritesOneAuditRow`;
  - `TestRedactResumesAfterEachStep`, which injects a failure after the journal, after each rename, before and after the directory fsync, after the projection, and after the audit row. It reruns each case and asserts one `Redacted` record per log, one audit row, no journal left, and serve then starts and serves the tombstone;
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
