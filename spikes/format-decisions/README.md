# Phase 0 format decisions

Throwaway benchmarks for TKT-01M29BSPD, which settles the three open storage
questions in `docs/10-open-questions.md`. Nothing here is production code and
nothing in `internal/` should ever import it.

It is committed rather than discarded for one reason: decision 1 is expected to
be revisited when Go 1.27's json/v2 lands, and a decision that cannot be
re-measured is a decision nobody can safely reopen.

## Running it

```sh
go -C spikes/format-decisions test -run TestEncodedSize -v          # on-disk size
go -C spikes/format-decisions test -bench 'Encode|Decode' -run XXX  # codec cost
go -C spikes/format-decisions test -run TestFsyncLatency -v         # append latency
go -C spikes/format-decisions test -run TestIndexWidthReport -bench AuthorLens -v
```

Scratch files land in `spikes/format-decisions/.tmp/`, which is gitignored. The
index benchmark builds a 114 MB log there and leaves it for reuse across runs in
the same process; delete the directory when you are done.

## What it measures

**`encoding_test.go`** compares `fxamacker/cbor/v2` against `encoding/json` on
generated post bodies at 300 B, 2 KB, and 8 KB. Both use string keys, because
docs/03 requires a self-describing map. Integer CBOR keys would be far smaller
but would give that up, so comparing them would be comparing two designs rather
than two encodings.

**`fsync_test.go`** measures append-to-durable latency under three policies:
`always`, `group(drain)` which batches only what is already queued, and
`group(wait)` which holds the batch open for 5 ms. It warms each file with 64
synced appends first. Without that warm-up, ext4 block allocation on a fresh
file lands entirely on whichever policy runs first, and an earlier version of
this benchmark reported a tenfold difference between two policies that were
issuing identical syscalls.

**`index_test.go`** builds a 50 000 post log and filters it by author twice:
once from 40-byte index entries in memory, once from 16-byte entries that force
a read back into the log for the author field.

## Results as measured

Go 1.26.2, AMD Ryzen 9 9950X3D, ext4, **WSL2**.

| decision | outcome |
|----------|---------|
| body encoding | CBOR. Decode 8.3x faster at 2 KB, 1 alloc vs 7. Size differs by only 2 to 5 percent. |
| index entry width | 40 bytes. Author filter 15 us vs 19.8 ms, for 1.14 MB on a 50 k thread. |
| fsync default | `group` with drain semantics. A fixed 5 ms window costs 6.5x at the median for a lone writer and batches nothing. |

## What these numbers are not

The fsync results were taken under WSL2 on a virtual disk reporting a
write-back cache. That is not the VPS or home server the design targets, and an
fsync there may not mean what an fsync means on real hardware. The **relative**
shape transfers, because "waiting for a batch that never arrives costs a full
window" is arithmetic rather than a property of a disk. The **absolute** numbers
do not, and the p99 target in docs/09 is not verified until TKT-01M29C2WV6 runs
on the real thing.

The CPU-bound results (encoding, index filtering) do transfer, since the CPU is
not virtualized away.
