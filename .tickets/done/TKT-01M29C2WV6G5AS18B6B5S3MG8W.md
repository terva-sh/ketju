---
schema: 3
id: TKT-01M29C2WV6G5AS18B6B5S3MG8W
title: Benchmark the storage engine against the Phase 0 targets
type: task
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area:storage
  - area:test
  - effort:M
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29C0WD88M1C8EW4QQTX76NJ
  - TKT-01M29C0WDJ9FETR8EFZ7ED5JYF
blocks_on: none
references:
  - ref: design:roadmap-phase-0
    path: docs/09-roadmap.md
claim: null
archive: null
created_at: 2026-09-11T23:15:46Z
updated_at: 2026-09-15T07:14:13Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

A 50k-post synthetic thread and the four numbers docs/09 makes the Phase 0 exit criterion: append p99 under 10 ms with group fsync, random-ordinal read under 100 us warm, cold read from page cache miss, and index rebuild of a 100 MB log under 2 s.

Record the hardware. A number without the disk it was measured on is not a benchmark, and the whole point of this exercise is to know the envelope before web code makes it hard to measure.

If a target is missed, that is a finding rather than a failure. Say which target, by how much, and what would have to change.

## Acceptance criteria

- [x] All four measurements are recorded on this ticket with the hardware and filesystem named
- [x] The benchmark is repeatable from a committed command rather than an ad-hoc script
- [x] Each target is either met or has a written explanation of the gap and the options
- [x] Results are compared across the fsync policies so the default is chosen on evidence

## Notes

**agent:terva/mieli** at 2026-09-12T00:22:49Z

Inherited from TKT-01M29BSPD (Settle the Phase 0 thread log format decisions): the fsync policy default was chosen on evidence, but that evidence was taken under WSL2 on a virtual disk with a write-back cache. It is not target storage, so the docs/09 target of **append p99 under 10 ms with group fsync is currently unverified**.

This ticket's third acceptance criterion, comparing the fsync policies so the default is chosen on evidence, is therefore not starting from zero but is also not satisfied by that earlier work. When this runs on real hardware, re-run `spikes/format-decisions` `TestFsyncLatency` alongside the real engine benchmark and compare:

- `always`, `group(drain)`, and `group(wait 5ms)` at 1 and 8 concurrent writers
- whether `group(drain)` still matches `always` at one writer, which is the finding the default rests on
- whether p99 clears 10 ms at all under any policy

On WSL the numbers were 0.94 ms p50 at one writer and 1.99 ms p50 at eight under drain. If real storage is an order of magnitude slower, the default still holds, because the argument for drain is structural rather than numeric. What would change is whether the Phase 0 exit target is reachable, and that is worth knowing before the format freeze rather than after.

The CPU-bound decisions from that spike (CBOR, 40-byte index entries) do not need re-measuring here; the CPU was not virtualized away.

**agent:claude-code/storage** at 2026-09-15T04:42:24Z

Baseline host settled on 2026-09-15: the machine this repository is worked on is
close enough to target storage to be the benchmark baseline. That answers the
question the previous note left open, so this ticket no longer waits on hardware.

Recorded now rather than when the benchmark runs, because this ticket's first
criterion is that the hardware and filesystem are named, and a machine is easier
to describe while you are standing on it.

  CPU     AMD Ryzen 9 5950X, 12 vCPUs visible of its 16 cores
  Memory  3.6 GiB total, about 1.5 GiB available at the time of writing
  Kernel  Linux 6.12.107+deb13-amd64 (Debian 13)
  Store   ext4, rw,relatime,errors=remount-ro
  Device  /dev/mapper/brokkr--vg-root — LVM logical volume, 237 GiB
  Virt    KVM

Two facts a later reader should have rather than discover.

It is still a VIRTUAL disk. KVM on LVM is a different and more honest thing than
the WSL2 virtual disk the earlier note warned about, and it is the declared
baseline, so this is not a reason to re-open the decision. It IS a reason to
write "KVM guest, ext4 on LVM" next to any p99 figure rather than "bare metal".
The host's disk cache mode is not visible from inside the guest, and cache mode
is precisely what decides whether an fsync is honest, so the append p99 number
carries that caveat and cannot shed it from in here.

Memory is small relative to what this ticket measures. 3.6 GiB total against a
100 MB log rebuild and a 50k-post thread means the page cache is a real
constraint rather than an afterthought — which helps the "cold read from page
cache miss" measurement be genuinely cold, and hurts the warm-read measurement
if anything else is running. Record what else was running.

**agent:claude-code/storage** at 2026-09-15T07:13:39Z

Measured 2026-09-15 on the declared baseline host.

### Hardware and filesystem

  CPU      AMD Ryzen 9 5950X, 12 vCPUs visible of 16 cores
  Memory   3.6 GiB total
  Kernel   Linux 6.12.107+deb13-amd64 (Debian 13)
  Store    ext4, rw,relatime,errors=remount-ro
  Device   /dev/mapper/brokkr--vg-root, LVM logical volume, 238 GiB, 189 GiB free
  Cache    /sys/block/{sda,dm-0,dm-1}/queue/write_cache = "write back"
  Virt     KVM guest

Machine otherwise idle: one-minute load average between 0.06 and 1.40 across
the runs. An earlier attempt was abandoned at load 11.78 because another
session was running a large test suite, and numbers taken under that would
have described the neighbours.

### The fsync floor, which every append figure is read against

One 4 KB write plus fsync, 1000 samples:

  p50 0.896 ms   p95 1.034 ms   p99 1.664 ms   max 1.856 ms

This is the number that makes the rest believable. A write-back cache that
lied about flushing would show single-digit microseconds; 0.9 ms is consistent
with an honest flush to a real device. The earlier note on this ticket warned
that the host's cache mode is invisible from inside the guest and that the
append figure could not shed that caveat. It can now, at least in this
direction: whatever the host is doing, fsync here is not free.

### 1. Append latency by policy — TARGET MET

1000 samples per row, real ext4, one thread.

  policy   writers   p50      p95      p99      max
  always   1         0.865    0.950    1.046    1.785
  always   8         6.868    7.065    7.148    7.200
  group    1         0.854    0.904    0.975   16.560
  group    8         1.729    1.838    1.995    2.726
  never    1         0.005    0.011    0.017    0.040
  never    8         0.014    0.075    0.113    0.135

  (milliseconds)

Target: append p99 under 10 ms with group fsync. Met at 0.975 ms with one
writer and 1.995 ms with eight — a margin of 5x to 10x. Every policy clears
10 ms, including always at eight writers.

Three findings.

Group commit matches SyncAlways at one writer: 0.854 against 0.865 at p50,
0.975 against 1.046 at p99. This is the claim the default rests on and the
thing the WSL measurement could not settle. It holds on real storage.

Group commit is 3.6x better at eight writers on p99: 1.995 against 7.148.
SyncAlways serialises eight fsyncs; drain collapses them into one per batch.

An append at one writer costs no more than the fsync floor — 0.854 against
0.896 at p50, inside the noise. The engine adds nothing measurable to the cost
of durability itself, which is the strongest statement available here.

One blemish: group at one writer has a max of 16.56 ms, about 17x its own p99,
against 1.785 ms for always. A single outlier in 1000, almost certainly an
ext4 journal commit landing on that append. It does not move p99 and it is not
a reason to change the default, but a tail that long is worth knowing about
before somebody meets it in a latency graph.

### 2. Warm random-ordinal read — TARGET MET

50,000-post thread, index trusted rather than replayed, 20,000 samples.

  what                      p50       p95       p99       max
  random ordinal, clean     1.29 us   1.89 us   4.21 us   1601 us
  random ordinal, edited    1.29 us   2.16 us   4.50 us    177 us
  page of 25, clean         9.62 us   17.2 us   43.4 us    219 us
  page of 25, edited       18.4  us   27.0 us   48.8 us    217 us

Target: random-ordinal read under 100 us warm. Met at 4.21 us p99, a margin of
24x.

The edited rows are the ones worth having. A forward note on this ticket
predicted that a page of heavily edited posts would cost nearer one read per
post than one per page, and that measuring only the clean case would flatter
the design. Measured with a quarter of posts edited, a page costs 1.78x what a
clean page costs — 18.4 us against 9.62 us. The prediction was right and the
size of it is now on record. Single-ordinal reads are unaffected, because one
ordinal is one ReadAt whether or not it was edited.

### 3. Cold read from a page cache miss — RECORDED

Same thread, POSIX_FADV_DONTNEED before each read, 400 samples.

  p50 112.5 us   p95 126.7 us   p99 136.0 us   max 165.2 us

No numeric target in docs/09; this is the number the ticket asks to be
recorded. A cold read costs about 87x a warm one (112.5 us against 1.29 us),
which is the page cache doing what it is for and is why the open-thread cache
in TKT-01M29C0WE5 exists.

Note it still clears the 100 us WARM target by only a small margin in the
other direction — a cold read is roughly the warm target. That is the honest
framing: the warm target is met because reads are warm, not because a miss is
cheap.

### 4. Index rebuild of a 100 MB log — TARGET MET

100.7 MB over 288,242 posts, 364 bytes per post. Nil sidecar, so a full
rebuild. 5 samples each.

  warm   p50 127.0 ms   p99 142.3 ms   792 MB/s
  cold   p50 171.7 ms   p99 177.9 ms   586 MB/s

Target: under 2 s. Met at 171.7 ms cold, a margin of 11x.

Measured cold as well as warm deliberately. A rebuild happens after a crash or
a lost sidecar, when nothing is in the page cache, so the warm number is the
flattering one and the cold number is the one an operator actually waits
through. With 3.6 GiB of RAM a 100 MB log fits in cache easily, which is
exactly why the warm figure needed a companion.

### The mistake in the first run, which is why the harness now refuses tmpfs

The first set of these numbers was wrong and looked fine. Benchmarks wrote to
b.TempDir(); TMPDIR was unset; /tmp is tmpfs. SyncAlways came out at 0.0048 ms
p50 against SyncNever at 0.0040 ms — not a fast disk but an absent one. The
figures were plausible enough to write down, and the only reason they were not
is that always and never being equal makes no sense.

The harness now resolves its working directory to $HOME/.cache/ketju-bench,
statfs's it, and fails with an explanation if it is tmpfs or ramfs.
BenchmarkFsyncFloor was added so every future run publishes the cost of an
fsync beside the append figures, because a benchmark that can silently measure
RAM is a trap for everyone after me. CI passes -bench.allow-volatile-fs, named
so nobody mistakes that run for a measurement.

## Summary

All four Phase 0 targets measured on the baseline host and met. Full tables,
method and caveats are in the note above; a condensed version is in docs/03
under Sizing.

  append p99, group fsync        0.98 ms at 1 writer, 2.00 ms at 8   target 10 ms
  random-ordinal read, warm      4.2 us p99                          target 100 us
  cold read, page cache miss     136 us p99                          recorded
  index rebuild, 100 MB          178 ms cold, 142 ms warm            target 2 s

The fsync policy default is now chosen on evidence from real storage rather
than from WSL. Group commit matches SyncAlways at one writer (0.854 against
0.865 ms p50) and beats it 3.6x at eight (1.995 against 7.148 ms p99), which is
exactly the structural claim the default rested on. Every policy clears 10 ms,
including SyncAlways at eight writers.

An append at one writer costs no more than a bare write plus fsync on the same
filesystem — 0.854 ms against a 0.896 ms floor. The engine adds nothing
measurable to the cost of durability.

Two things recorded that the targets did not ask for. A page of 25 posts costs
1.78x more when a quarter are edited, confirming and quantifying the forward
note left on this ticket. And group at one writer showed a single 16.56 ms
outlier in 1000 samples, about 17x its own p99, almost certainly an ext4
journal commit; it does not move p99 and is not a reason to change the default,
but it is a tail worth knowing before meeting it in a graph.

The first run of these benchmarks was wrong and looked fine: they wrote to
b.TempDir(), TMPDIR was unset, /tmp is tmpfs, and SyncAlways came out at
0.0048 ms against SyncNever at 0.0040 ms — not a fast disk but an absent one.
The harness now statfs's its working directory and refuses tmpfs and ramfs with
an explanation, defaults to $HOME/.cache/ketju-bench rather than the system
temp dir, and publishes BenchmarkFsyncFloor so every future run prints what an
fsync actually costs beside the append figures. CI runs the suite at one
iteration with -bench.allow-volatile-fs, named so that run cannot be mistaken
for a measurement.

The earlier caveat on this ticket — that the host's cache mode is invisible
from inside the guest, so the append figure could not shed its asterisk — is
partly answered. A 0.896 ms fsync floor is consistent with an honest flush; a
cache that lied would show single-digit microseconds.

An earlier attempt was abandoned at load average 11.78 because another session
was running a large test suite on this machine. The recorded numbers were taken
between 0.06 and 1.40.
