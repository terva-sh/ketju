---
schema: 3
id: TKT-01M29C2WV6G5AS18B6B5S3MG8W
title: Benchmark the storage engine against the Phase 0 targets
type: task
status: draft
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
updated_at: 2026-09-15T04:42:24Z
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

- [ ] All four measurements are recorded on this ticket with the hardware and filesystem named
- [ ] The benchmark is repeatable from a committed command rather than an ad-hoc script
- [ ] Each target is either met or has a written explanation of the gap and the options
- [ ] Results are compared across the fsync policies so the default is chosen on evidence

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
