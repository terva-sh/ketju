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
    neoforum.db                  # embedded database (see 04)
  config.toml
```

Thread ids are 64-bit, time-sortable (millisecond timestamp in the high bits,
per-process sequence in the low bits). The path shards on the first two bytes
of the hex form so no directory exceeds a few thousand entries. URLs use a
shorter base32 form plus a slug: `/t/h6ute4a1000001/why-we-left-slack`.

## Log file format

### File header (64 bytes, written once)

| Offset | Size | Field |
|-------:|-----:|-------|
| 0 | 8 | Magic `NEOFTHRD` |
| 8 | 2 | Format version (1) |
| 10 | 2 | Header length (64) |
| 12 | 4 | Reserved |
| 16 | 8 | Thread id |
| 24 | 8 | Created at (unix ms) |
| 32 | 8 | Forum id at creation (informational; moves are recorded as events) |
| 40 | 20 | Reserved |
| 60 | 4 | CRC32C of bytes 0–59 |

### Record frame

Every record after the header is one frame:

| Offset | Size | Field |
|-------:|-----:|-------|
| 0 | 4 | Frame length `L` (header + body, excludes this field and the trailing CRC) |
| 4 | 1 | Record type |
| 5 | 1 | Body encoding (1 = CBOR, 2 = JSON; both supported, CBOR is default) |
| 6 | 2 | Flags |
| 8 | 4 | Post ordinal this record concerns (0 for thread-level events) |
| 12 | 4 | Revision number (1 for a new post, incremented by each edit) |
| 16 | 8 | Timestamp (unix ms) |
| 24 | 8 | Actor user id |
| 32 | `L-32` | Body (type-specific, CBOR-encoded map) |
| 4+L | 4 | CRC32C over bytes 0 … 4+L−1 |

Rationale for the split: the fixed 32-byte header lets the reindexer and the
index builder walk a log without decoding bodies. The body is a self-describing
map so we can add fields without a format bump. CBOR over JSON for the body
because bodies are written once and read many times, and the pure-Go CBOR
libraries are fast and dependency-light; JSON remains a supported encoding for
debugging and for `neoforum dump`.

### Record types

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

## Index file format

`<id>.idx` is a derived, rebuildable file: a 32-byte header then one 40-byte
entry per post ordinal, so entry for ordinal `n` lives at `32 + 40*(n-1)`.

### Index header

| Offset | Size | Field |
|-------:|-----:|-------|
| 0 | 8 | Magic `NEOFIDX1` |
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
| 34 | 2 | Flags: deleted, moved, hidden-by-mod, has-attachments, edited | tombstones without a read |
| 36 | 4 | Reserved | |

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
- Natural place for group commit: if several appends arrive within a few
  milliseconds the actor writes them and issues a single `fsync`.

Steps for a `Post`:

1. Validate and render-check the markdown (render is done to catch errors,
   the HTML is discarded; see caching).
2. Encode body, build frame, compute CRC.
3. `write` frame at end of log; `fsync(log)` (configurable: `always`,
   `group` (default, ≤ 5 ms window), `never`).
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
  that `neoforum serve` validates the tail of any log whose index claims a
  shorter length than the file (a crash mid-write). A torn final frame (bad
  length or CRC) is truncated to the last good frame boundary — since we are
  the only writer and every frame is CRC'd, this is safe.
- `neoforum fsck` walks every log, verifies every CRC, rebuilds every index,
  and reports drift between logs and database projections.
- `neoforum reindex [--search] [--projections]` rebuilds database projections
  and/or the search index from logs. This is the designated recovery path for
  a lost or corrupted database: users and permissions must be restored from a
  DB backup, but content never is.
- `neoforum dump <thread-id>` prints a log as JSON lines for inspection.

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
