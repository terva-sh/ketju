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

Every number below is in the advisory range (`0x01`–`0x7F`); see
"Unrecognised record types" for what that means and how a new number is
claimed. The constants are in `internal/store/threadlog/records.go` and code
uses the name, never the literal.

| Type | Name | Body fields | Effect on projection |
|-----:|------|-------------|----------------------|
| 1 | `ThreadCreated` | title, slug, forum_id, tags[] | thread row |
| 2 | `Post` | source (markdown), reply_to (ordinal, optional), mentions[], attachments[] | new post_meta row, ordinal = thread.post_count+1 |
| 3 | `Edit` | source, reason, reply_to | post_meta.current_revision++, edited flag |
| 4 | `Delete` | reason, hard (bool, always false in v1) | post_meta.deleted, tombstone shown |
| 5 | `Restore` | reason | clears deleted |
| 10 | `ThreadRenamed` | title, slug | thread row |
| 11 | `ThreadMoved` | forum_id | thread row |
| 12 | `ThreadLocked` / 13 `ThreadUnlocked` | reason | thread flags |
| 14 | `ThreadPinned` / 15 `ThreadUnpinned` | scope (forum/global), until | thread flags |
| 16 | `ThreadTagged` | tags[] (full replacement) | thread_tags |
| 20 | `PostsSplitOut` | ordinals[], target_thread_id | ordinals marked moved; new thread's log gets `Post` records with `origin` |
| 21 | `PostsMergedIn` | source_thread_id, ordinals[] | mirror of the above |
| 30 | `ThreadArchived` | reason | thread read-only, index may be dropped from memory permanently |

Deletion is soft. A `Delete` record leaves the original `Post` bytes intact;
the reader consults the index flags and returns a tombstone. Hard deletion
(legal takedown) is handled by a separate offline command that rewrites the
log with the body zeroed and appends an `Edit` with a redaction marker; this is
the one operation that is not append-only and is logged in the audit table.

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

The first critical type is already foreseeable: docs/10 item 4 leans toward a
`Redacted` record for hard redaction, needed before Phase 2. An older build that
skipped one would render content that was legally required to be removed, so it
gets a `0x80`-range number.

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
| 32 | 2 | Current revision number | cache keys, "edited" marker |
| 34 | 2 | Flags: deleted (0), moved (1), hidden-by-mod (2), has-attachments (3), edited (4), machine-authored (5) — `EntryFlags` in `flags.go`, reserved bits 6–15 as `EntryFlagsReserved` | tombstones and the agent lens without a read |
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
the end of this document. Spare capacity for anything else is the 10 unused bits
of the flags field.

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
thread and idempotent (keyed on `thread_id, log_offset`). A `projections`
table records the log length each thread has been projected up to, so a crash
between log write and DB write is repaired on startup by replaying the gap.
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
| Rendered-post cache | HTML + metadata per revision | bytes (default 256 MB) | LRU |
| Response cache | serialized JSON per `(view, thread, log_length, viewer-class)` | bytes (64 MB) | LRU, invalid by construction when `log_length` changes |
| HTTP | `ETag: "<thread_id>-<log_length>-<renderer_version>"` | client | conditional GET |

**The log length is the thread's version number.** It is monotonic, it changes
on every mutation, and it is available without a database query. It drives
ETags, the response cache key, and live updates ("send me everything after
offset X"). Viewer-specific data (read position, own-post markers) is layered
on top by the client or by a second, cheap query, never mixed into the cached
payload.

Renderer version is part of every cache key so upgrading the markdown pipeline
invalidates rendered HTML without rewriting a single log.

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
  DB backup, but content never is.
- `ketju dump <thread-id>` prints a log as JSON lines for inspection.

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

## Attachments

Not in the log. Uploaded files are written to `blobs/sha256/xx/<hash>`,
deduplicated by content hash, and referenced from the post body's
`attachments[]` list by hash, filename, size, and MIME type. A `blobs` table in
the database tracks reference counts for garbage collection and holds the
scan/quarantine status. Images get server-side thumbnails cached next to the
blob. Blobs are served with immutable cache headers from a hash-addressed URL.

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
