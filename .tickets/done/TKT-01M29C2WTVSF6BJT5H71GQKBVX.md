---
schema: 3
id: TKT-01M29C2WTVSF6BJT5H71GQKBVX
title: Fuzz the record frame parser
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:storage
  - area:test
  - effort:S
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29BW9PTAQ34EC78E61AHZM0
blocks_on: none
references:
  - ref: design:testing
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:15:46Z
updated_at: 2026-09-15T01:09:20Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

A Go fuzz target over the frame parser and the body decoder. The parser reads a length field off disk and allocates from it, which is the classic shape of a memory blow-up, so a hostile or corrupt length must be refused before it is trusted.

The threat here is mostly corruption rather than an attacker, since logs are not user-supplied files. That does not change the failure mode: a panic in the parser takes down the process for every thread, not just the damaged one.

## Acceptance criteria

- [x] The parser never panics on arbitrary input and always returns an error instead
- [x] A length field larger than the remaining file is refused before any allocation of that size
- [x] The fuzz target runs in CI for a bounded time and its corpus is committed
- [x] Any crash found is added to the corpus with a regression test

## Implementation plan

Two targets in `internal/store/threadlog/fuzz_test.go`: `FuzzParseFrame` over
the frame parser and `FuzzDecodeBody` over the body decoder, which is the layer
underneath and the one that actually allocates from what it read.

The assertion that matters is NOT "did not panic" — the fuzzing harness gives
that away free, and a target that checks only for panics is a target that passes
while the parser quietly mis-slices. `FuzzParseFrame` asserts that any frame the
parser ACCEPTS re-encodes to exactly the bytes it came from, plus that the
header-only parser and the full parser never disagree about size. `FuzzDecodeBody`
asserts that anything decoded can be encoded again.

The allocation criterion gets its own deterministic test rather than a fuzz
target, because "before any allocation of that size" is a measurement and not a
crash. Allocation COUNT is the wrong instrument — building the error value
allocates, so the count is never zero. It measures total bytes across 2000
refusals and compares against one believed allocation.

CI runs each target for 20 seconds on the committed corpus: a regression gate,
not a discovery run.

## Notes

**agent:claude-code/storage** at 2026-09-15T01:09:06Z

NO CRASHES FOUND. Roughly 26 million executions across the two targets —
14.3M for FuzzParseFrame and 11.8M for FuzzDecodeBody, 45 seconds each on this
machine — produced zero failures. Criterion 4 is therefore ticked for the
mechanism rather than for a discovery: nothing was found, so nothing was added.

The mechanism itself is not theoretical, and that part is measured. 37 corpus
files are committed under testdata/fuzz/, and `go test` replays 53 corpus
entries (37 committed plus 16 f.Add seeds) as ordinary subtests with no -fuzz
flag. So a crash found tomorrow becomes a permanent regression test the moment
its file lands, whether or not anyone runs the fuzzer again.

The corpus is curated, not dumped. Go keeps generated inputs in GOCACHE, which
CI never has, so they had to be copied in — but FuzzDecodeBody alone had
867 of them. Committed are the smallest from each target: 31 to 1099 bytes for
the parser, 49 to 54 for the decoder. A minimised input carries far more
coverage per byte than a padded one, and a corpus nobody can review is a corpus
nobody will prune. Real content is about 4.5 KB; `du` says 156 KB because of
4 KB block allocation per file.

The allocation measurement came out at 105 to 117 bytes per refusal across all
three oversize cases (length just under the cap, at the cap, and maximum
uint32). That is the error string and nothing else. One believed allocation
would have been about a megabyte.

-race is deliberately NOT on the fuzz step. The detector costs roughly an order
of magnitude in executions per second, and the deterministic suite already runs
under it. Fuzzing wants executions.

Worth knowing for later: `go test -fuzz` takes exactly one target per
invocation, which is why the CI step is two commands rather than one.

## Summary

Zero crashes found, in about 26 million executions. Read the fourth criterion's
tick in that light: it records that the corpus-to-regression-test pathway exists
and was exercised, not that a crash was discovered and fixed.

`internal/store/threadlog/fuzz_test.go` adds two targets — FuzzParseFrame over
the frame parser, FuzzDecodeBody over the body decoder underneath it — plus a
deterministic allocation test. CI runs each target 20 seconds against the
committed corpus.

The targets assert more than the absence of a panic, which the harness gives
away free and which a mis-slicing parser would pass anyway. A frame the parser
ACCEPTS must re-encode to exactly the bytes it came from, and the header-only
parser and the full parser must never disagree about a frame's size. Anything
DecodeBody accepts must be encodable again.

The allocation criterion is a measurement, not a crash, so it is a normal test.
Allocation count is the wrong instrument — constructing the error allocates, so
the count is never zero — and it measures bytes instead: 2000 refusals of a
length claiming the largest frame the parser will entertain cost 105 to 117
bytes each, which is the error string. One believed allocation would have been
about a megabyte.

37 corpus files are committed under testdata/fuzz/, curated rather than dumped:
Go keeps generated inputs in GOCACHE where CI will never see them, and
FuzzDecodeBody alone had produced 867. The smallest from each target are the
ones kept, 31 to 1099 bytes for the parser and 49 to 54 for the decoder, because
a minimised input carries more coverage per byte and a corpus nobody can review
is one nobody will prune. `go test` replays 53 corpus entries as ordinary
subtests with no -fuzz flag, so a crash found later becomes a permanent
regression the moment its file lands.

-race is deliberately off on the fuzz step: it costs about an order of magnitude
in executions per second, and every deterministic test already runs under it.
The step is two commands because `go test -fuzz` takes exactly one target per
invocation.
