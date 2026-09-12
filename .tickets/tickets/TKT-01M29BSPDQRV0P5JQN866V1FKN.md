---
schema: 3
id: TKT-01M29BSPDQRV0P5JQN866V1FKN
title: Settle the Phase 0 thread log format decisions
type: spike
status: review
status_reason: "Decisions 1, 2, and 3 are settled and written into docs/03 and docs/10. Criterion 2 is unticked on purpose: append latency was measured under WSL2, which is not target storage, so the docs/09 p99 target is unverified and TKT-01M29C2WV6 inherits it. Needs a person to accept closing on two of three criteria."
priority: urgent
due_on: null
labels:
  - area:storage
  - effort:S
  - decision
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:open-questions-storage
    path: docs/10-open-questions.md
claim: null
archive: null
created_at: 2026-09-11T23:10:45Z
updated_at: 2026-09-12T00:23:48Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Questions 1, 2, and 3 in docs/10, all of which must be answered before the format is frozen and none of which can be answered from a document.

Body encoding: CBOR through fxamacker/cbor, or JSON. The lean is CBOR because bodies are written once and read many times. If measured decode cost is close, JSON's debuggability may win.

Index entry width: 40 bytes carrying author, created_at, and reply_to, or 16 bytes with the offset alone. The lean is 40. It costs 2 MB per 50k posts and buys the author, chain, and since lenses without a database query, which is most of the argument for the whole lens design.

Default fsync policy: `group` with a window of 5 ms or so, or `always`. The lean is `group`, with `always` and `never` remaining configurable.

## Acceptance criteria

- [x] Decode cost of CBOR and JSON bodies is measured on realistic post sizes and the numbers are recorded on this ticket
- [ ] Append latency under `group` and `always` fsync is measured on the target storage and recorded
- [x] All three decisions are written into docs/03 and docs/10 is updated to say which option won and why

## Notes

**agent:terva/mieli** at 2026-09-12T00:17:37Z

### Decision 1: body encoding is CBOR

Measured with `spikes/format-decisions`, Go 1.26.2, AMD Ryzen 9 9950X3D, `fxamacker/cbor/v2` v2.9.3 against `encoding/json`. Both encoders use string keys, because docs/03 calls for a self-describing map so fields can be added without a format bump. Integer CBOR keys would be much smaller but would give that up, so measuring them would compare two designs rather than two encodings.

Decode, which is the operation that matters, since bodies are written once and read many times:

| body | cbor | json | ratio | cbor allocs | json allocs |
|------|------|------|-------|-------------|-------------|
| 300 B | 390 ns | 2393 ns | 6.1x | 1 | 7 |
| 2 KB | 963 ns | 7982 ns | 8.3x | 1 | 7 |
| 8 KB | 2134 ns | 26780 ns | 12.5x | 1 | 7 |

Encode: 856 ns vs 1876 ns at 2 KB, 2.0 KB vs 2.7 KB allocated.

On-disk size, mean over 256 generated bodies:

| body | source | cbor | json | json/cbor |
|------|--------|------|------|-----------|
| 300 B | 540 | 606 | 638 | 1.053 |
| 2 KB | 2297 | 2363 | 2427 | 1.027 |
| 8 KB | 8415 | 8479 | 8658 | 1.021 |

**The size argument for CBOR is nearly worthless and should stop being repeated.** A post body is one large UTF-8 string plus a handful of small fields, and both encodings store that string the same way. JSON costs 2 to 5 percent more bytes, not the multiple the "binary is smaller" instinct suggests.

The decode argument is the whole case, and it is strong enough on its own. At 8.3x and one allocation instead of seven, a 25-post page costs about 24 us of decode under CBOR against 200 us under JSON, before rendering. That is the difference between decode being invisible and decode being a line item.

Two caveats for whoever revisits this:

- Measured against `encoding/json` v1. Go 1.27's json/v2 is expected to narrow the decode gap, and docs/09 already lists it as a risk. It would have to get roughly 8x faster to change this decision, which is not what the v2 proposal claims, so the decision stands. Worth re-running the benchmark when the toolchain moves.
- The rendered-post cache means a hot thread decodes almost nothing. The decode cost lands on cold reads, reindexing, and search indexing, which are exactly the paths that touch every post in a thread at once.

JSON stays a supported body encoding (encoding byte 2) for `neoforum dump` and debugging, as docs/03 already specifies. CBOR is the default that writers use.

**agent:terva/mieli** at 2026-09-12T00:22:49Z

### Decision 2: index entry width is 40 bytes

Measured on a generated 50 000 post thread (113.7 MB log, 40 participants, uneven activity), filtering by one author:

| path | time | allocs |
|------|------|--------|
| 40-byte entries, in memory | 14.9 us | 0 |
| 16-byte entries, author read back from the log | 19.8 ms | 0 |

A factor of 1326, and that is the flattering case: the log was warm in the page cache, having just been written. A cold thread makes the 16-byte path worse, not better.

Memory cost of the extra 24 bytes:

- 50 k post thread: 1.91 MB against 0.76 MB
- 512 open threads at 50 posts each: 0.98 MB against 0.39 MB

The 2 MB estimate in docs/03 for the largest plausible thread was right; measured 1.91 MB.

So 1.14 MB on the largest thread anybody will have buys a lens that runs in 15 us instead of 20 ms. The author, chain, and since lenses all make this same trade, and all three are the argument for the lens design in docs/06. At 20 ms per lens application the design would not survive contact with a phone.

Informational, since TKT-01M29C2WV6 owns it properly: a header-only walk of the 113.7 MB log found all 50 000 frames in 3 ms warm, so the "rebuild 100 MB in under 2 s" target has enormous headroom on the CPU side. Cold disk is the part that still needs measuring.

### Decision 3: fsync default is `group`, with drain semantics rather than a fixed window

This one changes the design rather than confirming it.

docs/10 described `group` as "a 5 ms window", and docs/03 said the actor "writes them and issues a single fsync" when several appends arrive within a few milliseconds. Implemented literally, that means waiting up to 5 ms to see whether anybody else shows up. Measured against an actor that instead drains whatever is already queued and syncs immediately:

| policy | writers | p50 | p99 | fsyncs | mean batch | appends/s |
|--------|---------|-----|-----|--------|------------|-----------|
| always | 1 | 0.94 ms | 1.10 ms | 400 | 1.00 | 1057 |
| group (drain) | 1 | 0.96 ms | 1.15 ms | 400 | 1.00 | 1024 |
| group (wait 5 ms) | 1 | 6.48 ms | 6.92 ms | 400 | 1.00 | 156 |
| always | 8 | 7.63 ms | 8.73 ms | 1600 | 1.00 | 1040 |
| group (drain) | 8 | 1.99 ms | 3.18 ms | 399 | 4.01 | 3949 |
| group (wait 5 ms) | 8 | 6.27 ms | 7.21 ms | 200 | 8.00 | 1234 |

Median of three interleaved repetitions; full output in the commit.

The fixed window is the wrong reading. At one writer it batches **nothing** (mean batch 1.00) and charges 5 ms for the privilege, which is 6.5x the median of doing nothing clever. A small forum is at one writer almost always. Drain semantics give a lone append exactly the latency of `always` while still collapsing 1600 fsyncs into 399 at eight concurrent writers, with better median latency and 3.8x the throughput of `always`.

`group(wait)` keeps the tightest p99 under load, and if that ever matters the window can come back as a tunable. It should not be the default.

**What this measurement does not establish.** It was taken under WSL2 on a virtual disk reporting a write-back cache. That is not a VPS and not a home server, and an fsync there may not mean what an fsync means on the target. So:

- The **relative** result stands anywhere. "Waiting for a batch that never arrives costs a full window" is arithmetic, not a property of a disk.
- The **absolute** numbers do not transfer, and the docs/09 target of append p99 under 10 ms with fsync is therefore **not verified**. Acceptance criterion 2 on this ticket says "on the target storage" and I am leaving it unticked for that reason.

TKT-01M29C2WV6 already owns benchmarking against the Phase 0 targets on real hardware, and I have added a note there naming what to re-run. Note also that fsync policy is a runtime config value and not a field in the on-disk format, so this open item does not gate the format freeze in TKT-01M29C5Y1E.

**agent:terva/mieli** at 2026-09-12T00:23:45Z

in-progress to review: Decisions 1, 2, and 3 are settled and written into docs/03 and docs/10. Criterion 2 is unticked on purpose: append latency was measured under WSL2, which is not target storage, so the docs/09 p99 target is unverified and TKT-01M29C2WV6 inherits it. Needs a person to accept closing on two of three criteria.

## Summary

All three decisions are settled and written into docs/03 and docs/10. Measurements come from `spikes/format-decisions/`, committed so they can be re-run.

1. **Body encoding: CBOR.** Decode is 8.3x faster than `encoding/json` at a 2 KB body with one allocation instead of seven, widening to 12.5x at 8 KB. The size argument turned out to be worth almost nothing, 2 to 5 percent, because a body is one large UTF-8 string both encodings store the same way. JSON stays available as encoding byte 2 for `neoforum dump`.

2. **Index entry width: 40 bytes.** An author filter over a 50 k post thread runs in 15 us from memory against 19.8 ms when the author must be read back from the log, a factor of 1326 measured warm. Cost is 1.14 MB on the largest plausible thread. This is the measurement the whole lens design in docs/06 rests on, and it holds.

3. **fsync default: `group`, with drain semantics rather than a fixed window.** This changed the design. Read literally, the "5 ms window" in docs/10 means waiting to see if anyone else shows up, and at one writer it batches nothing while charging 6.5x the median latency. Draining whatever is already queued gives a lone append the same latency as `always` and still collapses 1600 fsyncs into 399 at eight concurrent writers. docs/03 and docs/10 now say drain.

**One criterion is deliberately unticked.** Append latency was measured, but under WSL2 on a virtual disk with a write-back cache, which is not target storage. The relative comparison between policies transfers and is what settled the default; the absolute numbers do not, so the docs/09 target of append p99 under 10 ms is **not verified**. TKT-01M29C2WV6 carries a note naming exactly what to re-run on real hardware.

This does not block the format freeze in TKT-01M29C5Y1E: fsync policy is a runtime config value, not a field in the on-disk format. Decisions 1 and 2 are format decisions and both are settled on evidence.

Left in review rather than done, because accepting a spike with an unticked criterion is a call for a person to make.
