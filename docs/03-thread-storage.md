# 03 — Thread Storage

The distinguishing design decision. Each thread is an append-only log file on
disk plus a small sidecar index of fixed-width entries. The database never
holds post bodies.

## Goals

- O(1) seek to any post by ordinal without scanning.
- Appends are cheap, sequential, and crash-safe.
- Edit history and soft deletion fall out of the format for free.
- A thread is fully reconstructible from its log alone; the index and every
  database row about it are derived data.
- Reads scale with the OS page cache; hot paths never touch the database.
- Files are friendly to `rsync`, `zfs send`, and object-storage backup.

## Non-goals

- Multi-node replication. Single writer process, one data directory.
- Storing reactions, read markers, or anything high-churn in the log.
  Those belong in the database (see 04).

## Format freeze

**The thread log format is frozen at version 1 as of 2026-09-15**
(TKT-01M29C5Y1E). Everything in this document from here down is a contract.

What frozen means: a change to the file header, the record frame, the index
entry layout, or the meaning of an existing record type is a format version
bump with a migration path, not an edit. Adding a field to a record *body* is
not a bump — bodies are self-describing CBOR maps and that is why. Claiming a
new record type number is not a bump either, provided it follows the rules in
"Claiming a new record type" below.

The freeze rests on evidence rather than on confidence. It was gated on the
property tests (TKT-01M29C2WT7), the crash tests (TKT-01M29C2WTJ), the frame
fuzzer (TKT-01M29C2WTV) and the benchmark (TKT-01M29C2WV6), all of which
landed before this was signed. A format frozen before the numbers are in is a
format frozen on hope.

### Reserved space, audited before signing

| Structure | Reserved | Enough for segment rollover? |
|---|---|---|
| File header (64 B) | 4 B at 12, 20 B at 40 | Yes, ample |
| Frame fixed part (32 B) | no spare bytes; 15 spare flag bits | Not needed — see below |
| Index entry (40 B) | 4 B at 36, 9 flag bits (10 until auto-hidden took bit 6, TKT-01M29CP2PJ) | Yes: a uint32 segment number |
| Index header (32 B) | **none** | Not needed — see below |

Segment rollover is the scenario the reserved bytes exist for: if a log ever
approaches the tens of GB it becomes `.log.0001`, `.log.0002`, and an index
entry's offset needs to say which segment it lives in. The 4 reserved bytes at
entry offset 36 hold that segment number, and 2^32 segments at any sane segment
size is more address space than the rest of the design could ever use.

The frame fixed part has no spare bytes at all, and does not need any. A frame
does not record its own position; the index does. Rollover changes where frames
are found, not what they contain.

The index header having no reserved bytes is deliberate and worth stating
plainly, because it otherwise reads as an oversight. **The index is derived and
disposable.** Changing its layout costs a magic bump from `THRDIDX1` to
`THRDIDX2` and one rebuild per thread, which `ketju fsck --rebuild` already
does at roughly 100 MB in 178 ms cold. Reserving space in a file you can
regenerate is paying for insurance you will never claim.

### `MaxFrameSize` at 1 MiB

Settled here rather than left as an implementation detail, because a reader
that refuses a frame the writer was willing to produce is a compatibility
break, which makes this a format parameter.

1 MiB stands. The margin, though, is not what it was assumed to be when the
number was chosen. The reasoning then was "docs/07 caps a post body at 64 KB,
so this is roughly sixteen times the largest legitimate frame". That is true of
`Post` and false of the format as a whole, because every record type shares
this one bound.

The binding case is `PostsSplitOut`, which carries `ordinals[]`. Measured
against CBOR integer widths:

| Case | Body size | Headroom |
|---|---|---|
| Post: 64 KB source, 1000 attachments, 1000 mentions | ~146 KB | 7.2x |
| Split naming all 50 000 posts, ordinals under 65536 | ~150 KB | 7.0x |
| Split naming all 50 000 posts, ordinals above 65536 | ~250 KB | 4.2x |

So the true margin is 4 to 7 times, not 16, and the constraint comes from split
rather than from posts. That is still comfortable against the 50 000-post
largest-plausible thread in "Sizing", so the bound holds.

The ceiling it implies, for whoever implements split and merge in Phase 4: a
single `PostsSplitOut` record can name at most about **349 000** ordinals below
65536, or about **209 000** above it. A split larger than that has to be
carried by more than one record. Writing that down now is cheaper than
discovering it from a refused frame later.

### Reserved record type numbers

`20` `PostsSplitOut` and `21` `PostsMergedIn` are **held, not free**. They
appear in the catalogue with names and no body type, they are what
`RecordType.Reserved` reports, and they sit in the **advisory** range, so a
reader meeting one skips it rather than refusing the thread. Skipping a split
or merge leaves a stale render (the moved posts still show in the source
thread), not a disclosure, and that is the advisory class's test
(`TestEveryCatalogueTypeIsAdvisory` in `internal/store/threadlog`). The index
fold and the projection writer both skip them, so the two folds of one log
agree. Their bodies are specified in the table under "Record types" and will
be implemented in Phase 4. Decided 2026-09-28 (TKT-01M39973KG): the first
version of this paragraph said a reader refuses them. The code skipped them
deliberately from the start, and the document was the one that was wrong.

Every other number is unassigned rather than reserved: `6`–`9`, `17`–`19`,
`22`–`29`, and `31`–`0x7F` in the advisory range, and `0x81`–`0xFF` in the
critical range, where `0x80` is claimed for `Redacted` ("Hard redaction").
`0x00` is never assigned. Unassigned numbers are claimed by the
procedure in "Claiming a new record type"; held numbers are not available at
all.

### Bumping the version, when it comes to that

The procedure, written down before it is needed because the moment it is
needed is a bad moment to design it:

1. Raise the format version in the file header and in `THRDLOG1`'s successor
   magic. A reader refusing an unknown version is the point; it must not guess.
2. `ketju migrate-logs --from DIR --to DIR` reads every log under the old root
   and writes a new tree under a new root. It never edits in place. An
   append-only log rewritten in place during a migration is an append-only log
   that can be left half-converted by a power cut.
3. Verify the new tree with `ketju fsck --root NEW`, which walks every log and
   every checksum independently of whatever the migration thought it did.
4. Stop the server, swap the roots by rename, start it. The old tree stays on
   disk until somebody deletes it deliberately.
5. Indexes are not migrated. They are rebuilt, because they are derived.

The swap being a rename, and the old tree surviving it, is what makes the
rollback a rename too.

## On-disk layout

```
data/
  threads/
    3f/
      7a/
        3f7a9c21e0000001.log     # append-only record log
        3f7a9c21e0000001.idx     # fixed-width offset index (derived)
  blobs/
    sha256/
      ab/
        abcd…ef                  # attachments, content-addressed
  db/
    ketju.db                     # embedded database (see 04)
  config.toml
```

Thread ids are 64-bit, time-sortable: a 48-bit millisecond timestamp in the
high bits and a 16-bit per-process sequence in the low bits. The path shards on
the first two bytes of the hex form so no directory exceeds a few thousand
entries; the second of those bytes turns over about every 50 days and the first
about every 35 years, so a directory fills at the rate the board is used.

URLs use a shorter base32 form plus a slug. The id above is
`/t/3yymw47g00001/why-we-left-slack`. The encoding is Crockford base32,
thirteen lowercase characters — thirteen because 64 bits needs ceil(64/5), and
Crockford because it drops I, L, O and U so an id can be read off a screen and
typed back. Parsing accepts uppercase and maps the lookalikes; it refuses
anything else rather than coercing it, since a coerced id is a valid id for
somebody else's thread. Implemented in `internal/store/threadid`, decided in
TKT-01M29BW9PJ.

## Log file format

All multi-byte integers are little-endian. The format is single-node by design
(see Non-goals), so cross-endian portability buys nothing real, and
little-endian is native on every target this runs on. Decided in
TKT-01M29BW9PT.

### File header (64 bytes, written once)

| Offset | Size | Field |
|-------:|-----:|-------|
| 0 | 8 | Magic `THRDLOG1` |
| 8 | 2 | Format version (1) |
| 10 | 2 | Header length (64) |
| 12 | 4 | Reserved |
| 16 | 8 | Thread id |
| 24 | 8 | Created at (unix ms) |
| 32 | 8 | Forum id at creation (informational; moves are recorded as events) |
| 40 | 20 | Reserved |
| 60 | 4 | CRC32C of bytes 0–59 |

The magic names the format, not the product. A thread log is a thread log
whatever the binary above it is called, so renaming the project never has to
touch a byte on disk, before or after the freeze. Branding in a file signature
buys nothing and costs a format version bump on the day the name changes.

### Record frame

Every record after the header is one frame:

| Offset | Size | Field |
|-------:|-----:|-------|
| 0 | 4 | Frame length `L` (header + body, excludes this field and the trailing CRC) |
| 4 | 1 | Record type |
| 5 | 1 | Body encoding (1 = CBOR, 2 = JSON; both supported, CBOR is default) |
| 6 | 2 | Flags (bit 0 = machine-authored; bits 1–15 reserved) |
| 8 | 4 | Post ordinal this record concerns (0 for thread-level events) |
| 12 | 4 | Revision number (1 for a new post, incremented by each edit) |
| 16 | 8 | Timestamp (unix ms) |
| 24 | 8 | Actor user id |
| 32 | `L-28` | Body (type-specific, CBOR-encoded map) |
| 4+L | 4 | CRC32C over bytes 0 … 4+L−1 |

So `L = 28 + len(body)`, the body occupies bytes `[32, 4+L)`, and a whole frame
occupies `4 + L + 4` bytes on disk. `L` excludes the length field, which is why
the body is `L-28` and not `L-32`.

Rationale for the split: the fixed 32-byte header lets the reindexer and the
index builder walk a log without decoding bodies. The body is a self-describing
map so we can add fields without a format bump. CBOR over JSON for the body
because bodies are written once and read many times, and the pure-Go CBOR
libraries are fast and dependency-light; JSON remains a supported encoding for
debugging and for `ketju dump`.

Measured in Phase 0 (TKT-01M29BSPD, harness in `spikes/format-decisions/`):
decoding a 2 KB body costs 963 ns under CBOR against 7982 ns under
`encoding/json`, with one allocation instead of seven, and the gap widens to
12.5x at 8 KB. The size argument that usually travels with "binary is smaller"
does not survive contact with the data. A body is one large UTF-8 string plus a
handful of small fields, and JSON costs only 2 to 5 percent more bytes. Decode
speed is the entire case for CBOR, and it is enough on its own.

### Frame flags

| Bit | Meaning | Constant |
|----:|---------|----------|
| 0 | Machine-authored: the actor was an agent account when this record was written | `FrameFlagMachineAuthored` |
| 1–15 | Reserved, written zero | `FrameFlagsReserved` |

The constants are in `internal/store/threadlog/flags.go` and they are what code
uses. This table is the wire format and stays authoritative for what is on
disk, but no caller should ever write the number: the assignment used to live
only here, while the server setting the bit, the index builder mirroring it and
the lens reading it back all had to agree on a literal `1` with nothing holding
them together. `TKT-01M2BXKF8F` closed that.

Note that machine-authored is bit **0** in a frame and bit **5** in an index
entry. The same fact has two numbers, which is why crossing between them goes
through `FrameFlags.ToEntryFlags` rather than by hand.

A reader preserves bits it does not know. `ParseFrame` returns the field
verbatim and `AppendFrame` writes back what it was given, so a log written by a
newer build stays readable by an older one. `TestFrameUnknownFlagBitsRoundTrip`
holds that property. Nothing in the parser interprets a flag — naming the bits
added names, not validation, because refusing an unknown bit would destroy that
compatibility.

The server sets bit 0 from `users.kind` (04) when it appends the record. A
client never supplies it. The bit is not redundant with the author's account:
`kind` is current state and can change, while the log records what was true when
the post was written, and no projection can reconstruct that afterwards. It also
lets a lens count or filter machine-authored posts from the index alone.

Anything richer about provenance, which model and which version, or a reference
to the conversation and tool calls that produced the text, belongs in the body
rather than here. Bodies are self-describing maps, so those fields can arrive at
any time without a format bump. Only what a lens must see *without* decoding a
body has to be settled before the format freeze, and that is one bit.

### Record types

Every number below but `Redacted` (`0x80`, critical) is in the advisory
range (`0x01`–`0x7F`); see
"Unrecognised record types" for what that means and how a new number is
claimed. The constants are in `internal/store/threadlog/records.go` and code
uses the name, never the literal.

| Type | Name | Body fields | Effect on projection |
|-----:|------|-------------|----------------------|
| 1 | `ThreadCreated` | title, slug, forum_id, tags[] | thread row |
| 2 | `Post` | source (markdown), reply_to (ordinal, optional), mentions[], attachments[], origin (`{thread_id, ordinal}`, optional: a copy made by split or merge) | new post_meta row, ordinal = thread.post_count+1 |
| 3 | `Edit` | source, reason, reply_to, mentions[] (optional), grace (optional bool) | post_meta.current_revision++, edited flag unless `grace`; when `mentions` is present it replaces the post's mention rows |
| 4 | `Delete` | reason, hard (bool, always false in v1), by_moderator (optional bool), suspension (optional uint), auto (optional bool) | post_meta.deleted, tombstone shown; the entry's hidden-by-mod and auto-hidden flags when `auto`, its hidden-by-mod flag (clearing auto-hidden) when `by_moderator`, otherwise its deleted flag |
| 5 | `Restore` | reason | clears deleted, hidden-by-mod and auto-hidden |
| 10 | `ThreadRenamed` | title, slug | thread row |
| 11 | `ThreadMoved` | forum_id | thread row |
| 12 | `ThreadLocked` / 13 `ThreadUnlocked` | reason | thread flags |
| 14 | `ThreadPinned` / 15 `ThreadUnpinned` | scope (forum/global), until | thread flags |
| 16 | `ThreadTagged` | tags[] (full replacement) | thread_tags |
| 20 | `PostsSplitOut` | ordinals[], target_thread_id | ordinals marked moved; new thread's log gets `Post` records with `origin` |
| 21 | `PostsMergedIn` | source_thread_id, ordinals[] | mirror of the above |
| 30 | `ThreadArchived` | reason | thread read-only for everyone, moderators included; index may be dropped from memory permanently. An admin action (08) |
| 0x80 | `Redacted` | ordinal, revisions[] cleared, reason, operator, title (bool, optional), journal (ULID) | for a post: post_meta.deleted, its mentions removed, and the entry's redacted flag set and has-attachments cleared; the post is a tombstone to everyone and its history lists revisions with no content. For a title (`title` true, ordinal 0): no effect, since the `ThreadRenamed` written after it sets the title. **Critical.** Written only by the offline `ketju redact` ("Hard redaction" above). Claimed 2026-09-28 (TKT-01M29BSPE2); in the catalogue since TKT-01M29CXJZ9, and a build before that refuses a log containing it |

`Edit.mentions` was added after the version 1 freeze, on 2026-09-24. It is an
optional key in the CBOR body map, which is the kind of change the freeze
leaves open without a version bump (10, Q31): an older reader ignores it, and
an `Edit` without it keeps the post's existing mentions. It exists because
nothing downstream re-parses markdown (the renderer returns mentions
structurally, TKT-01M29C2WWG), so a mention added in an edit would otherwise
never reach the projection. Recording mentions at creation only lost because
an edited-in mention would silently never notify. Re-rendering during
projection lost because it would tie the projection to the renderer version.

`Edit.grace` was added the same way, on 2026-09-29 (TKT-01M29CKRHS). `true`
marks an author's edit within `posts.grace_edit_seconds` of posting, on a post
with no visible edit yet (08). It is a revision like any other, and history
lists it, but it does not set the entry's edited flag or `post_meta.edited_at`.
Any other `Edit` sets both, and nothing clears them. A grace `Edit` on a post
that already has the flag, which the service never writes, leaves the flag set
and moves `edited_at` to its own time. So whenever a post is marked edited, the
current revision's time is the last visible edit's. The service decides grace
when it writes the record, because the index is rebuilt from the log alone and
cannot read a setting, and a decision taken on read would put a setting into
cached responses. An older reader ignores the key and marks the post edited,
which fails safe: it shows a marker and discloses nothing.

`Delete.by_moderator` was added the same way, on 2026-09-29 (TKT-01M29CRJ5J).
`true` marks a moderator's delete of somebody else's post, which the service
writes whenever a `Delete`'s actor is not the post's author. It sets the
entry's hidden-by-mod flag rather than deleted, so that only a moderator may
restore the post (08 "Restoring a moderator's delete"). Both mark
`post_meta.deleted`, and a `Restore` clears either. The index reads the key,
not the actor, so the record's effect is what it says, and `ketju dump` needs
no rule to read it. Deriving it from the actor lost for that reason. No log
written before it holds a `Delete` by a non-author, because the service
refused one, so no existing record changes meaning. An older reader ignores
the key and shows a tombstone to everyone, which fails closed.

`Delete.suspension` was added the same way, on 2026-09-29 (TKT-01M29CP2QK).
It is the id of the `audit_log` row of the suspension whose "hide their
posts" wrote the `Delete`, which always has `by_moderator` too (08
"Suspension"). The index ignores it: a hide is a hide whatever caused it.
It lets a restore put back exactly the posts a suspension hid, and leave hidden
a post a moderator hid for its own sake or hid again after a restore. That
restore walks the log to find the `Delete` in force for each post, and
appends each `Restore` on the condition that the log is still the length it
walked to. That condition is a check the actor makes in memory, like the flag
preconditions, and nothing on disk records it. A users-level "posts hidden"
flag lost, because every count and render path would have had to learn it. An
older reader ignores the key and the post stays hidden, which fails closed.

`Delete.auto` was added the same way, on 2026-09-30 (TKT-01M29CP2PJ). `true`
marks the system's hide of a post whose open flags reached
`flags.auto_hide_threshold` (08 "Flags"). Its frame actor is 0, the system, so
that no reporter is named in the log. It sets the entry's hidden-by-mod flag
and its auto-hidden flag (bit 6), which together read as "hidden pending
review". A moderator's `Delete` (`by_moderator`) of such a post clears
auto-hidden and leaves the moderator's hide, and a `Restore` clears both. The
index reads the key, so a rebuild reproduces bit 6. An older reader ignores the
key and sees a `Delete` by someone other than the author, which it shows as a
tombstone to everyone but moderators, and fails closed.

Deletion is soft. A `Delete` record leaves the original `Post` bytes intact;
the reader consults the index flags and returns a tombstone.

#### Hard redaction

Hard deletion (legal takedown) is the one operation that is not append-only.
Decided 2026-09-28 (docs/10 item 4, TKT-01M29BSPE2):

- **The command runs offline**, with serve stopped and the data-directory lock
  held. It writes a new log beside the old one, fsyncs it, renames it over
  the old one, and then fsyncs the directory. It never edits a log in place.
  Because of the two fsyncs, a power cut leaves either the old log or the
  complete new one, never half of each. The rename is atomic but not durable
  on its own.
- **What is removed.** Every frame that carries the post's text: its `Post` and
  every `Edit` revision. Each frame keeps its record type, ordinal, revision,
  timestamp and actor, but its body is replaced by the empty body of that
  type: no source, no mentions, no attachments and no edit reason. It keeps
  only what is structure rather than text: a `Post`'s `reply_to` and
  `origin`, an `Edit`'s `reply_to` and `grace`. Every frame is
  re-checksummed, so "a CRC failure means corruption" stays true without
  exception. Attachment blobs referenced only by that post are deleted. A
  blob another post still references stays.
- **The log never gets shorter** (settled 2026-09-29 in grooming,
  TKT-01M29CXJZ9). The log length is the thread's version number ("Caching"
  below): the ETag, the response cache key, the live-update offset, and what
  the client compares before it replaces a page it holds. So each blanked
  body is padded to its frame's **original length** with a `pad` key: a CBOR
  byte string of zeros, whose header is always the five-byte form (`0x5a` and
  a uint32) so any length fits, or in a JSON body a string of `0` characters.
  No body type has a field of that name, so every reader ignores it. The
  smallest body a writer produces, `{source:"x"}`, is 10 bytes, which is
  exactly what the padding of an empty map needs. The new log is the old
  one plus the appended records, so every offset an index, `post_meta` or
  `projection_state` holds stays valid, and each catches up by replaying
  only what was appended. Compacting and padding only the marker lost, since
  a post with many long revisions would need a pad past `MaxFrameSize`; not
  padding at all lost, since catch-up quarantines a log shorter than its
  projection and the client refuses an older-looking page.
- **The marker.** A `Redacted` record is appended, naming the ordinal, the
  revisions cleared, the reason, and the operator. It takes a number from the
  **critical** range (`0x80`–`0xFF`), claimed by the procedure below, so an
  older build that does not know it refuses the log rather than rendering
  content that was legally required to be removed.
- **What the reader shows.** A redacted post renders as a tombstone saying it
  was removed, to every reader, moderators included, since nothing of it is
  left; its edit history lists the revisions with no content, to a
  moderator. The index entry's redacted flag (bit 7) marks it, and no
  record may name the post again: the thread actor refuses an `Edit`,
  `Delete` or `Restore` of it, and never writes a `Redacted` itself.
- **Copies from split and merge.** A split or merge copies a post into
  another thread's log as a new `Post` with an `origin` (below), so the same
  text lives in two logs. The command follows `origin` in both directions,
  from a copy back to its source and from a source to every copy,
  transitively, to a fixed point: it builds the origin graph over every post
  in every log and takes the whole connected component holding the named
  post, so a copy of a copy goes too (Terva review 1345). It redacts each one
  the same way, rewriting each log it touches. A redaction that cleared only
  one of them would leave the text readable through the other.
- **Several logs, one redaction.** Each log's swap is atomic, but a redaction
  that spans several logs is not. So before touching any log, the command
  writes a redaction journal to the data directory, fsyncs the journal, and
  then fsyncs the directory, so the journal's directory entry survives a
  power cut as well as its contents. The journal lists every (thread,
  ordinal) to clear, and each one's attachment hashes, read from every `Post`
  and `Edit` frame of it before any rewrite: once a log is swapped its bodies
  no longer list them, so from then on the journal is the only source
  (Terva reviews 1345 and 1351). While a journal exists, `ketju serve`
  refuses to start. Running the command again, or `ketju redact --resume`,
  finishes the job from the journal, and a pending journal is looked for
  before any check on the post's state, since right after its swap the post
  already reads as redacted (Terva review 1360). The resume points are three:
  - **The log.** The targets are grouped by thread and each log is rewritten
    once, blanking every target ordinal in it and appending one `Redacted`
    record per ordinal, in ordinal order. A log that carries the `Redacted`
    records for every ordinal the journal lists for it was swapped: resuming
    skips it and never appends a second marker. One with some of them and not
    all is damage, since one atomic swap writes all of them, and the run
    stops with the journal in place (Terva review 1351). A title's marker is
    recognised by the journal id it carries.
  - **The audit row.** Every blob reference the redaction drops, and the
    `post.redacted` row, are written in one transaction, which first looks
    for that row and does nothing if it is there. The row is the marker
    that the drops happened, so each is dropped exactly once (Terva review
    1345). It lists the blobs *planned* for deletion, never ones deleted.
  - **Each blob.** A blob is deleted when no log, avatar or draft names it,
    in its own transaction, which deletes its row and writes its own
    `blob.deleted` row and touches no file. The file is removed only after
    that commits, and a resumed run removes any journaled blob's file whose
    row is gone (Terva review 1360). The lock and the journal keep uploads
    out, so this shorter sequence is safe here where the blob collector,
    which runs beside uploads, needs its move-aside.

  The journal is deleted only after every log is swapped and the audit row
  and blob deletions are written, so an interruption never lets a
  half-redacted data directory be served. `--dry-run` writes nothing, and
  with a journal pending it reports the work left instead of resuming (Terva
  review 1366).
- **Quotes elsewhere.** Quotes of the post in other posts are other people's
  posts. The command lists them for the operator, who redacts them separately
  if the takedown covers them. It never edits them silently.
- **Audit.** The action is written to `audit_log` (04 "audit_log"), and the
  index and projections for each thread are brought current by the appended
  records. The operator must be an active admin.
- **The search index.** Decided by the run (2026-10-01, pending maintainer
  review), for TKT-01M3NS122F (Clear the search index in the hard-redaction
  tool). After each log's swap the command brings that thread's search rows
  current as it does its projection, and before it deletes the journal it
  deletes the search row of every (thread, ordinal) the journal lists and
  rewrites a redacted title's row from its replacement. The index holds one
  row per post, its current revision (04 "search"), so no older revision
  has a row to miss; but an edit freed the pages of the row it replaced,
  and those pages can still hold an earlier revision's terms. So the
  command then runs FTS5's `optimize`, which merges the index's segments,
  and then `VACUUM`, every time, which rebuilds `ketju.db` from its live
  pages, so no freed page keeps the text's terms. It then checkpoints the
  WAL into `ketju.db` and truncates it, and a checkpoint that cannot
  complete fails the run with the journal kept, so a resume finishes it.
  The `VACUUM` costs a rewrite of the database and free disk of about its
  size, which a rare, offline takedown can afford. A database without the
  search tables (one not yet migrated) has nothing to clear. A database
  restored from a backup taken before the redaction drops the rows by
  itself: its `search_state` trails the log, and catch-up reaches the
  `Redacted` records. The offset it resumes from is still a frame boundary,
  because the rewrite keeps every frame at its original length ("The log
  never gets shorter" above), and an offset that is not one makes the
  indexer re-index that thread from its first frame (Terva review 1728). Alternatives: relying on catch-up alone lost, because
  a stale `search_state` or a damaged index would leave the text searchable
  after the command reported success; deleting the rows with only `PRAGMA
  secure_delete` and `optimize` lost, because `secure_delete` cannot reach
  pages freed before it was turned on, and the terms would stay in the
  database file, which a takedown has to reach as it reaches the log (Terva
  review 1719).
- **Titles.** Decided 2026-09-29 (TKT-01M3NS147C): the command also redacts
  a thread's title, because a takedown or an erasure can cover a title as
  easily as a post, and a rename leaves every earlier title in the log. It
  blanks `title` and `slug` in `ThreadCreated`, keeping
  `forum_id` and `tags`, and in every `ThreadRenamed`, each padded so the log
  never shrinks. It appends `Redacted` with `ordinal` 0 and `title` set, then a
  `ThreadRenamed` to "Title removed" or the operator's `--title`. Every
  `Redacted` record also names the journal that wrote it, because a title can
  be redacted more than once and a resumed run must find its own marker. Old slugs
  keep redirecting by id. Rename audit rows record only the thread and log
  offset, so no copy of the title survives in the append-only `audit_log`.

**Split and merge are the hardest events.** The chosen model: posts are copied
into the target thread's log as new `Post` records with an `origin
{thread_id, ordinal}` field; the source posts get a `moved` flag and render as
a pointer. Ordinals in the source thread are never renumbered. This preserves
every permalink ever issued.

### Unrecognised record types

An older binary will meet a record type a newer one wrote. What it does then is
decided by the **high bit of the type byte**, which partitions the space:

| Range | Class | A reader that does not know the type |
|-------|-------|--------------------------------------|
| `0x01`–`0x7F` | advisory | **may skip it**, stepping over it by the frame length |
| `0x80`–`0xFF` | critical | **must refuse the thread** |
| `0x00` | — | never assigned; meeting one is damage, not a newer writer |

Skipping is mechanically trivial — the frame length lets a reader step over a
record without understanding a byte of it. The hard part is knowing when
skipping is *safe*, and the answer is not a property of the reader:

> A record may be skipped by a reader that does not understand it only if
> ignoring it cannot cause the reader to reveal content, or permit an action,
> that the record exists to withhold or prevent.

A skipped `Delete` renders a post a moderator removed. A skipped `Edit` renders
the superseded revision, which discloses whatever the edit took out. A skipped
`ThreadLocked` permits a reply to a locked thread. Those fail *open*. By
contrast a skipped `Restore` leaves a post deleted and a skipped `ThreadRenamed`
shows a stale title: those fail *closed*, and are survivable.

The class has to live in the type number because a reader that does not know a
type cannot know anything else about it. A frame flag would also work and is
worse: the class is a property of the type rather than of one record, so
deciding it once when the number is assigned is cheaper and much harder to get
wrong than asking every writer to set a bit correctly every time. It would also
spend one of the 15 reserved flag bits to say something the type byte can say
for free.

**Refusal is whole-thread.** Rendering the records before the unknown one and
stopping there is the same disclosure wearing a friendlier face: if the record
withheld post 3, showing posts 1–10 without applying it leaks post 3 exactly as
skipping would. A reader that cannot read all of a thread reports that it cannot
read the thread.

Note what this rule does *not* say. Every type in the v1 table above is
advisory, including `Delete` and `Edit`, and that is not a claim that ignoring
them is harmless. The partition governs only a reader that does not recognise a
type, and every build recognises the v1 catalogue. The two live cases today are
the reserved split and merge numbers, which no Phase 0 binary implements —
skipping those leaves posts rendering as text instead of as pointers, which is
stale and duplicated rather than disclosed, so advisory is the correct class for
them.

The first critical type is `Redacted`, `0x80`, the record hard redaction
appends (see "Hard redaction" above; docs/10 item 4). An older build that
skipped one would render content that was legally required to be removed, so it
has a `0x80`-range number, and a build from before TKT-01M29CXJZ9 refuses a
log that carries one.

### Claiming a new record type

The bands in the table above — 1–5 post lifecycle, 10–16 thread events, 20–21
split and merge, 30 archival — are a **convention, not a rule**. They exist so a
new thread event is filed among thread events rather than after the archival
record, and a gap inside a band is the first place to look for a number.

The rule is the partition, and it is binding:

1. Apply the skip test above. If ignoring the record could fail open, the number
   comes from `0x80`–`0xFF`. Otherwise from `0x01`–`0x7F`.
2. A number is assigned once and never reused, even if the type is retired. Old
   logs still contain it, and handing it to something else silently changes what
   those records mean.
3. `0x00` is never assigned.
4. Add it to the table above and to the catalogue in
   `internal/store/threadlog/records.go`. Membership in that map is what
   "known" means at runtime; a type in the table and not the map is skipped or
   refused by its range like any other unknown.

There is no registry beyond this document and that map, and no range is reserved
for private or experimental use. A fork that invents a type takes its chances
with a future upstream number, which is the honest cost of having no coordinator.

## Index file format

`<id>.idx` is a derived, rebuildable file: a 32-byte header then one 40-byte
entry per post ordinal, so entry for ordinal `n` lives at `32 + 40*(n-1)`.

### Index header

| Offset | Size | Field |
|-------:|-----:|-------|
| 0 | 8 | Magic `THRDIDX1` |
| 8 | 8 | Thread id |
| 16 | 8 | Log length this index is valid up to |
| 24 | 4 | Post count |
| 28 | 4 | CRC32C of bytes 0–27 |

### Index entry (40 bytes)

| Offset | Size | Field | Why it is here |
|-------:|-----:|-------|----------------|
| 0 | 8 | Offset of the *current revision's* record | the whole point |
| 8 | 4 | Frame length | one `pread` of exactly the right size |
| 12 | 4 | reply_to ordinal (0 = none) | reply-chain lens without touching the DB |
| 16 | 8 | Author user id | author filter lens without the DB |
| 24 | 8 | Created at (unix ms, of revision 1) | jump-to-date, "since last visit" |
| 32 | 2 | Current revision number | cache keys, history |
| 34 | 2 | Flags: deleted (0), moved (1), hidden-by-mod (2), has-attachments (3), edited (4, a visible edit: not set by a grace `Edit`), machine-authored (5), auto-hidden (6, hidden pending review, only beside hidden-by-mod), redacted (7, set by a `Redacted` record, which clears has-attachments: a tombstone to every reader) — `EntryFlags` in `flags.go`, reserved bits 8–15 as `EntryFlagsReserved` | tombstones and the agent lens without a read |
| 36 | 4 | Reserved | segment rollover, see below |

`machine-authored` mirrors frame flag bit 0, so filtering a thread by whether a
post was written by an agent is answered from the in-memory entries rather than
by reading bodies back. It is a mirror and not a reference: the index has to
answer without opening the log, which is the whole reason it exists. The bit
moves from one word to the other only through `FrameFlags.ToEntryFlags`, since
the two numberings disagree — bit 0 in the frame, bit 5 here — and copying a
constant from one set into the other would compile and be wrong.

The reserved 4 bytes at offset 36 are not general slack. They are held for the
segment number that segment rollover would add to the offset field, described at
the end of this document. Spare capacity for anything else is the unused bits
of the flags field: 8 since auto-hidden took bit 6 (TKT-01M29CP2PJ) and
redacted bit 7 (TKT-01M29CXJZ9). The index is derived and rebuilt from the
log, so assigning a bit here changes no file that outlives a rebuild.

The width is settled at 40 bytes. Measured in Phase 0 on a 50 000 post thread,
filtering by author takes 15 us from the in-memory entries against 19.8 ms when
the entry carries only an offset and the author has to be read back from the
log. That is a factor of 1300, and it is the warm-cache case, so a cold thread
is worse. The 24 extra bytes cost 1.91 MB of memory for that thread against
0.76 MB, and 0.98 MB against 0.39 MB across 512 open threads of typical size.
The chain and since lenses make the same trade with reply_to and created_at.

Because the header records the log length the index covers, opening a thread
is: read both headers, and if `idx.log_length == stat(log).size` the index is
trusted; otherwise replay records from `idx.log_length` to the end and repair.
A missing or corrupt index is rebuilt by a full replay. Rebuilding a 100 MB
log is a sequential read and takes well under a second on ordinary hardware.

The index is the only file we ever write non-sequentially (updating an entry
on `Edit`/`Delete`), and it is tiny and disposable, so that is acceptable.

## Write path

One goroutine owns each *open* thread (a "thread actor"); appends from HTTP
handlers are sent to it over a channel and the response is returned after the
record is durable. This gives us:

- Ordinals assigned without a lock dance: the actor knows `post_count`.
- Exactly one writer per file, so `O_APPEND` semantics are irrelevant and
  offsets are known before the write.
- Natural place for group commit: the actor takes one append, drains whatever
  else is already queued, and issues a single `fsync` for the whole batch. It
  never waits to form one. That is what keeps a lone append exactly as fast
  under `group` as under `always`, while a busy thread still collapses its
  fsyncs.

Steps for a `Post`:

1. Validate and render-check the markdown (render is done to catch errors,
   the HTML is discarded; see caching).
2. Encode body, build frame, compute CRC.
3. `write` frame at end of log; `fsync(log)` (configurable: `always`,
   `group` (default, drain and sync with no wait), `never`).
4. Append index entry; update in-memory index; write index header lazily
   (the header's `log_length` is what makes the index trusted, so it is
   updated *after* the log fsync — the worst case on crash is an index that
   claims to be older than it is, which triggers a harmless replay).
5. Publish an in-process event `{thread_id, ordinal, new_log_length}`.
   Subscribers: the projection writer (DB), the search indexer, the live
   update fan-out (SSE), the notification generator.
6. Return `{ordinal, revision, log_length}` to the handler.

The projection into the database (step 5) is asynchronous but ordered per
thread and idempotent (keyed on `thread_id, log_offset`). A `projection_state`
table (04) records the log length each thread has been projected up to, so a crash
between log write and DB write is repaired on startup by replaying the gap.
The writer is `internal/events/projection` (TKT-01M29C2WW5). It projects a
thread a range at a time in one transaction that reads and advances
`projection_state`, every apply sets absolute values so a replayed range
converges, and a thread whose projection fails is held and retried and
reported as lag rather than dropped. It opens logs read-only and never writes
one.
The log is the source of truth; the database catches up to it.

## Read path

```
GET /api/threads/{id}/posts?from=41&count=20
  → threadCache.Open(id)             // LRU of open threads; loads .idx into memory
  → idx.Entries(41..60)              // slice of in-memory entries
  → for each: postCache.Get(key)     // key = thread|ordinal|revision|rendererVersion
       miss → log.ReadAt(entry.off, entry.len) → verify CRC → decode → render → store
  → assemble response
```

- `os.File.ReadAt` is safe for concurrent use; readers never take the actor's
  channel. The OS page cache does the heavy lifting; we do not `mmap` in v1
  (mmap adds portability and lifecycle complexity for little gain at this scale;
  revisit if profiling says otherwise).
- Page-sized batch reads: when consecutive entries are contiguous on disk
  (the common case for unedited posts) the reader coalesces them into one
  `ReadAt` and slices the buffer.
- Tombstones and moved posts are answered from the index alone.

## Caching

| Layer | What | Bound | Eviction |
|-------|------|-------|----------|
| OS page cache | raw log bytes | free | kernel |
| Open-thread cache | decoded `.idx` + actor | N threads (default 512) | LRU, archived threads drop first |
| Rendered-post cache | HTML per revision | bytes (default 256 MB, `0` disables) | LRU; a post larger than the whole bound is refused rather than stored |
| Response cache | serialized JSON per `(view, thread, log_length, viewer-class)` | bytes (64 MB) | LRU, invalid by construction when `log_length` changes |
| HTTP | `ETag: "<thread_id>-<log_length>-<renderer_version>"` | client | conditional GET |

**The log length is the thread's version number.** It is monotonic, it changes
on every mutation, and it is available without a database query. It drives
ETags, the response cache key, and live updates ("send me everything after
offset X"). Viewer-specific data (read position, own-post markers) is layered
on top by the client or by a second, cheap query, never mixed into the cached
payload.

Renderer version is part of every cache key so upgrading the markdown pipeline
invalidates rendered HTML without rewriting a single log. That only works if
the constant is honest, so the rule in `internal/render` is: **any change that
can change the output of `Render` must bump `Version`** — an extension added
or removed, a sanitizer rule changed, a dependency upgraded — whether or not
any post in the corpus happens to render differently today. The rule is
enforced, not just written down: the golden tests keep a per-case hash of the
output, and updating a golden whose content changed is refused unless the
version was bumped first.

## Durability and recovery

- Startup scans `threads/` lazily: nothing is opened until requested, except
  that `ketju serve` validates the tail of any log whose index claims a
  shorter length than the file (a crash mid-write). A torn final frame (bad
  length or CRC) is truncated to the last good frame boundary — since we are
  the only writer and every frame is CRC'd, this is safe.
- `ketju fsck` walks every log, verifies every CRC, rebuilds every index,
  and reports drift between logs and database projections.
- `ketju reindex [--search] [--projections]` rebuilds database projections
  and/or the search index from logs. This is the designated recovery path for
  a lost or corrupted database: users and permissions must be restored from a
  DB backup, but content never is. `--projections` is the default. Reindex
  takes the data-directory lock, so it refuses to run beside `serve` and
  `serve` cannot start under it. It walks `threads/` first, then empties every
  projection table in one transaction and replays each log from its first
  frame, one transaction per thread; a run that stops partway is finished by
  another reindex or by `serve`'s catch-up. It only reads the logs. A torn
  final frame is projected around, to the last whole frame, and left for
  `serve` to cut at its next open. A thread whose log is damaged before its
  last frame, or whose records contradict each other, gets no rows, is
  reported, and makes the exit status 1; the run goes on. A failure that is
  not the log's, such as a database write, stops the run. The README's
  recovery procedure is built on it and run as a test.
- **Decided by the run (2026-10-01, pending maintainer review):** `ketju
  reindex --search` (TKT-01M29CTY9D, Phase 3; the line here used to say
  Phase 2 and name TKT-01M29C5Y25) rebuilds the search index (04 "search")
  and nothing else. `--projections` is still the default, and both flags
  together do both, projections first. The rebuild runs in one transaction:
  it empties the search tables, indexes every thread from its first frame,
  and commits, so an interrupted run leaves the old index in service. While
  it runs, the WAL grows by about the index's size. A database with no
  search rows needs no command: `serve` indexes every thread whose
  `search_state` trails its log in the background, after the listener
  opens, and search reports itself incomplete until that pass ends.
  The two differ because a reindex replaces an index that is complete,
  if stale or damaged, and an interrupted reindex should leave that one in
  place; after an upgrade there is no complete index to keep, so a partial
  one, flagged as such, is the best there is. Alternatives: one transaction
  per thread lost, because an interrupted run would put a half-built index
  in service in place of a complete one; building
  into a second set of tables and swapping lost, because their DDL would
  need a second copy outside the migrations, which `ClearProjections`
  already refuses for the same reason.
- `ketju dump <thread-id>` prints a log as JSON lines for inspection.
- Startup projection catch-up (04, `projection_state`) closes any gap between
  a log and its projection before the listener accepts requests. A log
  *shorter* than its projection cannot come from a crash, because the
  projection is written after the log. It is corruption. That thread is
  quarantined: logged at error level, reported by `/healthz` and
  `ketju fsck`, and served as 503 until an operator repairs it. Every other
  thread keeps serving. Refusing to start lost, because it lets one bad file
  take the whole forum down. (Decided 2026-09-24 in Phase 1 grooming,
  TKT-01M29C5Y1S.)

### What a rebuild can and cannot tell apart

Verified byte by byte in `crash_test.go`, against TKT-01M29C2WTJ.

Cutting the log at any offset recovers to the largest frame boundary at or
below the cut, with exactly the records that were fully durable and nothing
else. Flipping any byte before the final frame is refused rather than
truncated, because those bytes were acknowledged and cutting back to them would
discard durable posts — with one exception.

The exception is the four-byte length field. A length that overruns the end of
the file is precisely what an interrupted write leaves behind, so a rebuild
treats it as a torn tail and falls back. A flipped bit in a length field makes
the same shape, and the CRC cannot help: the CRC sits at the end of the frame
and the length is what says where that is, so the bytes are unreachable before
they can be checked. On the test log, 35 of the 762 byte positions before the
final frame can be flipped and still be accepted, and every one of them is
inside a length field.

The consequence is bounded, and the bound is what makes this acceptable rather
than a hole. A rebuild never invents a record and never reports a prefix longer
than the truth: the failure is losing durable records, not serving wrong ones.
And a rebuild is the rare path. The index header records the log length it is
valid to, so an ordinary open trusts the sidecar and never walks the frames —
a corrupt length in the middle of a log does not truncate anything, which is
also under test.

`ketju fsck` is where this is meant to be caught, since it verifies every CRC
against a log it reads whole rather than one it is trying to recover.

## Sizing

Assume 2 KB average post, 500 bytes of CBOR overhead and metadata.

| Threads | Posts | Log bytes | Index bytes |
|--------:|------:|----------:|------------:|
| 10 000 | 500 000 | ~1.2 GB | ~20 MB |
| 1 mega-thread | 50 000 | ~120 MB | 2 MB |

An in-memory index for the largest plausible thread is 2 MB; 512 open threads
of typical size (50 posts) is 1 MB. Nothing here needs segmenting. If a log
ever approaches the tens-of-GB range we add segment rollover (`.log.0001`) with
the index entry offset gaining a segment number; the reserved bytes exist for
that.

### Measured envelope

Taken 2026-09-15 against the real engine, on the baseline host: AMD Ryzen 9
5950X, ext4 on an LVM volume, KVM guest. Full tables, method and caveats are on
TKT-01M29C2WV6; the benchmarks are in `bench_test.go` and the command is in its
header. Every figure here is a p99 unless it says otherwise.

| docs/09 Phase 0 target | measured | margin |
|---|---|---|
| append p99 under 10 ms, group fsync | 0.98 ms (1 writer), 2.00 ms (8) | 5–10x |
| random-ordinal read under 100 us warm | 4.2 us | 24x |
| cold read from a page cache miss | 136 us | recorded, no target |
| index rebuild of a 100 MB log under 2 s | 178 ms cold, 142 ms warm | 11x |

All four are met. Three things the numbers say that the targets do not.

An append at one writer costs no more than a bare `write`+`fsync` on the same
filesystem — 0.854 ms against a 0.896 ms floor at p50. The engine adds nothing
measurable to the price of durability.

Group commit matches `always` at one writer and beats it 3.6x at eight
(2.00 ms against 7.15 ms at p99). The argument for drain was always structural;
this is the first time it has been measured on storage where fsync is real.

A page of 25 posts costs 1.78x more when a quarter of them have been edited
(18.4 us against 9.6 us at p50), because an edit lives where it was appended
and breaks the run the read path coalesces on. Single-ordinal reads are
unaffected. This is the cost of never rewriting history and it is small, but it
grows with edit density rather than staying flat.

Read the append figures against the fsync floor the benchmark prints beside
them. On a filesystem where fsync does nothing — tmpfs, or a virtual disk with
a lying cache — every durability number here would be a memory benchmark. The
harness refuses tmpfs outright for that reason.

## Attachments

Not in the log. Uploaded files are written to `blobs/sha256/xx/<hash>`,
deduplicated by content hash, and referenced from the post body's
`attachments[]` list by hash, filename, size, and MIME type. A `blobs` table in
the database tracks reference counts for garbage collection and holds the
scan/quarantine status. Images get server-side thumbnails cached next to the
blob. Blobs are served from a hash-addressed URL. They were to carry immutable
cache headers, and they are revalidated on every use instead, so that access
is checked each time (07 "Attachments", decided by the run 2026-10-01).

### Blob garbage collection

A daily job, `internal/jobs.BlobGC`, deletes blobs nothing refers to
(TKT-01M29C5Y2S). What it deletes has no other copy, so it deletes a blob only
when all of these hold at the moment of the delete:

- its `refcount` is zero and has been for seven days (`unreferenced_since`,
  04), checked again in the statement that deletes the row, so a reference
  committed after the pass read its candidates wins;
- no user's `avatar_blob` and no draft's source or attachments name its
  hash;
- its hash, as sixty-four hex digits in either case, appears in no record body
  of any log under `threads/`. That includes soft-deleted posts, whose `Post`
  record a `Restore` brings back with its attachments, and superseded
  revisions. The logs are the source of truth and the refcount is bookkeeping
  about them, so a count that has drifted cannot free a blob a post names.

A pass works in two phases. It reads the candidates a page at a time, in the
order they reached zero, and scans the logs for each page. Only when every
page has been scanned does it delete. Blobs the logs keep therefore cannot stop
a pass from reaching later candidates. A pass deletes nothing if any log cannot
be read to its end, if a body is in an encoding it cannot search, if `threads/`
is missing or is not a directory, if the walk meets a symlink below `threads/`
(`threads/` itself may be one, and is followed), or while a thread is
quarantined, because a missing or short log may name blobs no file on disk
does. It logs why, at
error. With no candidates it reads no logs.

A soft delete therefore never releases a blob, and an edit cannot drop one,
because `Edit` has no attachments field. Only the offline hard deletion
described under "Record types" removes a reference from a log.

A draft's attachments add no reference (07 "Drafts", TKT-01M3V73DP6). The
collector keeps a blob that any draft's attachments name, of either kind,
and so does `ketju redact`, which deletes through the same statement
(`DeleteCollectableBlob`). The seven days are still counted from the
refcount: a blob only a draft attaches is never collected while the draft
lists it, and once the draft is deleted, or saved without it, the next pass
may collect it at once if it has been at zero that long.

Because a draft holds no reference, its save asks the attach check's
per-blob questions again inside the write transaction that stores it (Terva
review 1749): does the blob still exist, did the viewer upload it, and is it
not quarantined. That write and the collector's delete are both write
transactions, so one is ordered before the other. If the delete commits
first, the save is refused as `unknown`. If the save commits first, the
delete finds the draft and keeps the blob.

The bytes are never gone while the row remains. Inside one `db.Write`, the
collector deletes the row and renames the file to `<hash>.gc` beside it. A
rename that fails rolls the row back. If the transaction does not commit, the
rename is undone, and once it commits the renamed file is removed. Each pass
first settles any `<hash>.gc` left by a crash or a failed undo. The pass puts
the file back while the row exists and removes it once the row is gone.

That sets two rules for the upload and attach paths (TKT-01M29CXK0):

- An attach raises the refcount with `AddBlobRef` in the database before the
  `Post` is appended. If `AddBlobRef` changes no row, the blob has been
  collected, and the attach is refused.
- An upload writes the blob's file and inserts or refreshes its row inside one
  `db.Write`, so an upload of bytes the collector is deleting at that moment is
  ordered before or after the collection, never interleaved with it.

Thumbnails are not named yet. The collector removes only the blob's own file,
and whoever defines the thumbnail name removes thumbnails the same way.

**Decided by the run (2026-10-01, pending maintainer review)**
(TKT-01M29CXK0K, TKT-01M29CYS9W):
- **A blob's thumbnail is `<hash>.thumb`, beside the blob** in
  `blobs/sha256/ab/`. There is one size, fitted inside 160 × 160 px with
  the aspect ratio kept. It is JPEG for a JPEG blob and PNG for a PNG or GIF
  blob. It is a cache: it is made from the blob on the first request that
  needs it and made again whenever it is missing, so no backup needs it and
  deleting every thumbnail loses nothing.
- **Uploads are staged in `blobs/tmp/`**, on the same filesystem, so the
  rename to the blob's path is atomic. A staged file is removed at once
  when its upload is refused or fails, whatever the reason: too large, a
  type off the allowlist, a failed `DecodeConfig`, a failed write, or a
  client that went away. Serve empties the directory at startup, under the
  data-directory lock, and the collector removes any staged file older than
  an hour, which covers a crash mid-upload (Terva review 1703), except in
  a held pass, which removes nothing (Terva review 1732), and except a file
  an upload in the process is still writing, however long its client has
  paused (Terva review 1735). Before the rename
  the upload syncs every directory from the blob's shard up to the data
  directory, so the shard that the first upload into it creates survives a
  power cut, and it syncs the shard after the rename.
- **A blob file with no row is an orphan and is never served,** since
  serving needs the row (`BlobForServing`). One is left when an upload's
  transaction fails after the file has been renamed into place, or when a
  crash lands between the two. The upload removes it at once, in a second
  `db.Write` that first checks the row is still absent. The collector
  removes any that remain, at least an hour old, with the same check inside
  its own `db.Write`. Uploads write inside `db.Write` too, so neither
  removal can interleave with an upload that is placing the same bytes
  (Terva review 1708). The collector keeps an orphan whose hash a thread
  log names, and removes none while a pass is held. No failed upload is on
  a post, so such a file is a database restored from a backup older than
  `blobs/`, and those bytes have no other copy (TKT-01M29CXK0K, at
  implementation).
- **The collector and `ketju redact`** remove `<hash>.thumb` once the row's
  deletion has committed. The collector also removes a `.thumb` whose blob
  has no row. A thumbnail needs no move-aside, because losing one costs only
  its regeneration.

The blob layout itself is the one above ("On-disk layout", `internal/store/blob`)
and does not change. Alternatives: a separate `thumbs/` tree lost, because
the design asks for thumbnails "next to the blob" and a second tree would
need its own collector. Several sizes lost, because the post card in the
mockups shows one 48 × 36 box, and 160 px covers it at 3× density.
Recording a failed decode on disk lost, because a cache entry that means
"never" outlives the decoder that failed.

## Alternatives considered

- **Posts as individual files.** Simple but millions of tiny files stress
  filesystems and backups; no natural ordering without a separate index anyway.
- **Everything in SQLite.** Perfectly viable at this scale and how most forums
  work. Rejected as the *primary* content store because it (a) puts the largest
  and most valuable data behind a single-writer database whose pure-Go drivers
  are the slowest part of the stack, (b) makes backups opaque, and (c) removes
  the architectural forcing function that keeps the schema small. Kept as the
  metadata and projection store.
- **A general embedded log library (e.g. a WAL package).** Adds a dependency
  for what is ~500 lines of framing code we need full control over.
- **Storing rendered HTML alongside source.** Doubles log size and couples the
  log to the renderer version. Rendering on read with a cache is cheaper and
  makes renderer upgrades free.
