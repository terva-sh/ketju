# SQLite driver spike

Throwaway benchmark and checks for TKT-01M29BSPE8. They confirm the choice of
`github.com/ncruces/go-sqlite3` over `modernc.org/sqlite` that
`docs/10-open-questions.md` item 6 took ahead of measurement. Nothing in
`internal/` should ever import this package.

It is kept so that the question can be measured again when ncruces reaches 1.0,
or if the modernc fallback is ever needed.

## Result

**ncruces stands, on condition that ketju works around its missing directory
fsync** (see below). The decision reopens only if ncruces is more than twice as
slow as modernc on the projection write path. On the configuration ketju uses,
with a single writer connection, it never was: it matched or beat modernc in
every case, and it was ahead most clearly where the disk sync dominates.

Measured on 2026-09-24 on an AMD Ryzen 9 5950X with an ext4 root on LVM, Go
1.27.1, ncruces v0.35.6 and modernc v1.59.0 (both SQLite 3.53.4). Each figure
is the median of three 1-second runs, in µs per post:

| sync | batch | ncruces | modernc |
|------|------:|--------:|--------:|
| full | 1     | 645     | 804     |
| full | 16    | 52.9    | 87.4    |
| full | 256   | 18.4    | 22.3    |
| off  | 1     | 92.8    | 115     |
| off  | 16    | 18.7    | 20.3    |
| off  | 256   | 10.0    | 10.1    |

`BenchmarkSyncFloor` on the same disk: fsync 559 µs and fdatasync 467 µs for a
4 KiB overwrite. At batch 1 with sync=full, both drivers are mostly timing the
disk.

With `sync=off` the disk drops out, and ncruces is between 0.71× and 1.20× of
modernc across every batch size and every connection count from 1 to 8.

## Findings that are not the headline

**The drivers do not sync the same way.** ncruces's VFS syncs with
`fdatasync` (`vfs/os_linux.go`). modernc is built without `HAVE_FDATASYNC`, so
SQLite's unix VFS falls back to a full `fsync`. This explains most of the gap
at `sync=full`. It also means switching to modernc would cost write latency for
a reason unrelated to the driver itself.

**Several writer connections starve each other on ncruces.** With 4 or 8
connections all committing at batch 1, individual commits waited for seconds
and then succeeded without reporting SQLITE_BUSY. In a five-run rerun, the
median for 8 connections was 225 ms per post, where modernc took 1.9 ms. The
cause is ncruces's `busy_timeout` handler (`conn.go`, `Xgo_busy_timeout`). It
retries after a random sleep of 0 to 2 ms with no fairness, so the connection
that has just committed usually wins the lock again. Both drivers also
returned a rare SQLITE_BUSY after the 5 s timeout under this load. ketju
avoids this by design: TKT-01M29BW9Q1 gives the write pool one connection, so
in-process writers queue in Go and never contend inside SQLite. That single
writer is load-bearing, not an optimisation.

**ncruces skips the directory fsync.** In `vfs/file.go`, `(*vfsFile).Sync`
opens the parent directory when SQLite asks for a directory sync, then syncs
the file a second time instead of the directory. That is true of v0.35.6 and of
upstream main on 2026-09-24. A power loss just after the WAL is created could
lose the WAL, and with it committed authoritative transactions. **The choice of
ncruces is conditional on a workaround:** the database opener fsyncs the data
directory itself once the WAL exists, before any authoritative write. That is an
acceptance criterion of TKT-01M29BW9Q1, the ticket that writes the opener, so no
authoritative write can land without it. TKT-01M392DP19 covers reporting the
bug upstream and removing the workaround once a fixed release is pinned.

## FTS5

- **ncruces:** import `github.com/ncruces/go-sqlite3/ext/fts5` and call
  `fts5.Register(conn)` in the connection callback of `driver.Open`. For a Go
  tokenizer, call `fts5.RegisterCustom(conn, func(api *fts5.API) error {
  return api.CreateTokenizer(name, ctor) })`, where the constructor returns a
  type implementing `fts5.Tokenizer`. `TestFTS5WithGoTokenizer` registers a toy
  stemming tokenizer and shows that `MATCH 'run'` finds "running" with it and
  not with `unicode61`. `snippet()` highlights the original word.
- **modernc:** FTS5 is compiled in, and no registration is needed.
  `TestFTS5OnModernc` confirms that the built-in `porter` tokenizer stems
  English. There is no Go API for tokenizers. A schema that names a Go
  tokenizer fails with an error on modernc; it does not silently fall back.

## What falling back to modernc would cost

This lists every ncruces-specific call the spike needed. The SQL, the DSN and
the transaction handling are identical for both drivers.

| ncruces surface | used for | modernc equivalent |
|---|---|---|
| `driver.Open(dsn, func(*sqlite3.Conn) error)` | per-connection setup | `sqlite.RegisterConnectionHook`, global to the driver |
| `fts5.Register` | enabling FTS5 | none needed; FTS5 is built in |
| `fts5.RegisterCustom` + `API.CreateTokenizer` | Go tokenizers, such as per-language stemming | **none**; only the built-in `unicode61`, `ascii`, `porter` and `trigram` |
| `fts5.API.CreateFunction` | custom ranking functions (not used yet) | **none** |
| error types (`*sqlite3.Error`) | recognising SQLITE_BUSY | `*sqlite.Error`; wrap the check in one function so only that function changes |
| driver name `sqlite3` | `sql.Open` | `sqlite` |
| DSN keys `_txlock`, `_pragma` | IMMEDIATE, WAL, busy_timeout | the same keys |
| `fdatasync` in the VFS | commit latency | full `fsync`, measurably slower |

The real fallback cost is the Go tokenizer. Search stemming beyond English
(TKT-01M29CRJ3B, Phase 3) would have to live outside FTS5 on modernc, for
example by stemming in Go before insert and at query time.

## Running it

```sh
go -C spikes/sqlite-driver test ./...                                   # assertions
go -C spikes/sqlite-driver test -run '^$' -bench . -benchtime=1s -count=3  # figures
```

Scratch databases go to `$HOME/.cache/ketju-bench/sqlite-driver`, or to
`-bench.dir`. The benchmark refuses to run on tmpfs, where fsync costs nothing.
A full run takes about five minutes.
