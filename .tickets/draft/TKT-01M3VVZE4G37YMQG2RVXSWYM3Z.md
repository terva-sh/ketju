---
schema: 3
id: TKT-01M3VVZE4G37YMQG2RVXSWYM3Z
title: Compare ordinals as unsigned in index and page bounds on 32-bit builds
type: bug
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:views
  - effort:S
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:362
    path: null
claim: null
archive: null
created_at: 2026-10-01T13:55:32Z
updated_at: 2026-10-02T17:29:03Z
created_by:
  id: agent:claude-code/phase3-lenses
  name: ""
updated_by:
  id: agent:claude-code/phase4-groom-correctness
  name: ""
extensions: {}
---

## Description

Review 1771 on PR #316 (TKT-01M29CTY5T) found that comparing a uint32 ordinal with a post count as `int(o) > n` is wrong on a 32-bit build. There `int(4294967295)` is -1, so the bound check passes when it should fail. The reply chain lens now compares as uint64 (`lens.pastLast`). The same pattern remains in code that ticket did not own:

- `internal/store/threadlog/index.go` `Index.Entry`: `int(ordinal) > len(ix.entries)`. Under `GOARCH=386`, `Entry(4294967295)` on a 3-post index panics with "index out of range [4294967294]" instead of returning its error. The run proved this by reverting the chain's own check under 386.
- `internal/service/threads/lenses.go` `lensPage`: `int(q.Ordinal) > sel.Posts()`.
- `internal/service/threads/view.go` `PageFor`: `int(q.Ordinal) > n`.
- `internal/service/threads/attachments.go`: `int(o) > n`.

`?ordinal=4294967295` reaches the two `q.Ordinal` checks from a URL. On amd64 and arm64, which are the builds ketju ships, every check is correct. The fix matters only if a 32-bit build is ever supported.

### Suggested fix

Compare in uint64, or add one helper in `threadlog` that every caller uses. Add a test that runs under `GOARCH=386`.

## Acceptance criteria

- [ ] threadlog.PastLast(o uint32, n int) bool exists, lens.pastLast is gone, and Index.Entry, Index.Entries, Index.entryRef, lensPage, PageFor and the attachments bound all call it; grep -rnE 'int\((o|ordinal|from|to|q\.Ordinal)\) *[<>]' over non-test Go files in internal/ and cmd/ finds nothing
- [ ] cmd/ketju/redact.go sorts ordinals with cmp.Compare rather than int subtraction
- [ ] TestPastLast, TestEntryPastTheEndIsAnErrorNotAPanic, TestEntriesPastTheEndAreClamped, TestReplayRefusesAnOrdinalPastTheEnd and TestPageForPastTheLastOrdinal pass, and ?ordinal=4294967295 is not-found on the thread page and on a lens page
- [ ] A CI step in Build and Test runs the threadlog and lens packages and TestPageFor under GOARCH=386 CGO_ENABLED=0, its step log shows each package ok, and the PR quotes that step failing on a commit holding only the tests and the step
- [ ] No change to format.go, records.go, header.go, the .idx layout or internal/dto

## Implementation plan

Groomed 2026-10-02 for the Phase 4 run (docs/15), against main at ed82f15e.

### Settled in grooming

- **Placement.** docs/15 "Scope" places this ticket in Phase 4, with the other small unplaced tickets.
- **The rule.** An ordinal is a `uint32`. A post count is an `int`, which is 32 bits on `386`, `arm`, `mips` and `mipsle`. `int(o) > n` is wrong on those: `int(4294967295)` is -1. Compare in `uint64`, as `lens.pastLast` already does (`internal/service/threads/lens/selection.go`, TKT-01M29CTY5T, review 1771).
- **One helper, in `threadlog`.** `threadlog` owns the ordinal type and the index, and the lens and service packages already import it. The new helper is `threadlog.PastLast(o uint32, n int) bool`, which returns `n < 0 || uint64(o) > uint64(n)`. It replaces `lens.pastLast`, and every bound check below calls it. Alternatives:
  - widening the counts to `int64` at each site lost, because seven sites would each repeat the conversion, and the next new site would get it wrong again;
  - keeping `lens.pastLast` and exporting it lost, because `threadlog` sits under `lens`, and `index.go` cannot import `lens`.
- **The sites on main at ed82f15e.** A search for `int(<ordinal>) <op>` over the non-test Go files finds these:
  1. `internal/store/threadlog/index.go` `Index.Entry`: `int(ordinal) > len(ix.entries)`. Under `GOARCH=386`, `Entry(4294967295)` on a 3-post index panics instead of returning `ErrNoSuchOrdinal`.
  2. `internal/store/threadlog/index.go` `Index.Entries`: `int(from) > len(...)` and `int(to) > len(...)`. With `to = 4294967295` on 386, `to` is not clamped, and the slice expression panics.
  3. `internal/store/threadlog/apply.go` `Index.entryRef`: `int(ordinal) > len(ix.entries)`. This one is not in the description. A log that names ordinal 4294967295 in an `Edit` or `Delete` panics during replay instead of returning `ErrMalformed`.
  4. `internal/service/threads/lenses.go` `lensPage`: `int(q.Ordinal) > sel.Posts()`. `?ordinal=` reaches it.
  5. `internal/service/threads/view.go` `PageFor`: `int(q.Ordinal) > n`. `?ordinal=` reaches it.
  6. `internal/service/threads/attachments.go`: `int(o) > n`.
  7. `cmd/ketju/redact.go:707`, a sort comparator: `int(a.Ordinal) - int(b.Ordinal)`. It overflows on 386 for ordinals more than 2^31 apart. Use `cmp.Compare`.

  The DTO conversions such as `int(p.Ordinal)` in `view.go` and `posts.go` are left alone. Each one converts an ordinal that has already been checked against the index, so its value is at most the post count, which fits an `int`.
- **The 386 test runs on this machine.** `GOARCH=386 CGO_ENABLED=0 go build ./...` succeeds on main. `GOARCH=386 go test ./internal/service/threads/lens/` passes. Four test packages do not compile for 386, because untyped constants overflow `int`:
  - `internal/store/threadlog`: `index_test.go:133,200,217` and `property_test.go:154`, each `1_700_000_000_000`;
  - `internal/service/threads`: `windows_test.go:15,26,29`;
  - `internal/web/api` and `internal/auth/agentcred`.

  This ticket makes the first two compile under 386. The other two are out of scope.
- **Formats.** No change to the log format, the `.idx` layout or any DTO.

### Approach

1. Add `PastLast` to `internal/store/threadlog/index.go`, with a doc comment that names the 32-bit case. Delete `lens.pastLast`, and point `lens/export_test.go` and `selection.go` at `threadlog.PastLast`.
2. Rewrite sites 1 to 6 to call it. In `Entries`, clamp with `if PastLast(to, len(ix.entries)) { to = uint32(len(ix.entries)) }`, and return nil when `PastLast(from, len(ix.entries))`.
3. In site 7, replace the subtraction with `cmp.Compare(a.Ordinal, b.Ordinal)`.
4. Make the two test packages compile for 386:
   - In `threadlog`, type the four `1_700_000_000_000` constants as `int64`. They are `CreatedMS` and timestamp values.
   - In `threads`, move the `TestSecondsDuration` rows whose constants overflow a 32-bit `int` (`most`, `most + 1` and `±10_000_000_000`) into a new `TestSecondsDurationPast32Bits` in `windows64_test.go`, with the constraint `//go:build !(386 || arm || mips || mipsle)`. The other rows and the doubling sweep stay in `windows_test.go`. They hold on any word size.
5. Add a CI step to the `Build and Test` job in `.forgejo/workflows/ci.yml`, after "go test (race)". docs/12 "Changing CI" allows adding a step. The step is named "ordinal bounds on a 32-bit build" and runs `GOARCH=386 CGO_ENABLED=0 go test -count=1 ./internal/store/threadlog/ ./internal/service/threads/lens/` and `GOARCH=386 CGO_ENABLED=0 go test -count=1 -run 'TestPageFor' ./internal/service/threads/`. The runner image is Alpine on amd64, and running a 386 binary needs the host kernel's IA32 emulation. If the step fails with `exec format error`, change it to `go test -c -o /dev/null` for those packages, so it only compiles them. Record that in a ticket note, and leave criterion 4 unticked.

### Tests

- `TestPastLast` (`threadlog`), table-driven: `(0,0)`, `(1,0)`, `(3,3)`, `(4,3)`, `(4294967295,3)`, `(4294967295, math.MaxInt)`, and `n = -1`. It runs under amd64 and under 386.
- `TestEntryPastTheEndIsAnErrorNotAPanic` (`threadlog`): on a 3-post index, `Entry(4294967295)` returns `ErrNoSuchOrdinal`. So does `Entry(4)`.
- `TestEntriesPastTheEndAreClamped` (`threadlog`): `Entries(2, 4294967295)` returns entries 2 and 3, and `Entries(4294967295, 4294967295)` returns nil.
- `TestReplayRefusesAnOrdinalPastTheEnd` (`threadlog`): a log whose `Delete` names ordinal 4294967295 makes `Open` return an error that wraps `ErrMalformed`, and does not panic.
- `TestPageForPastTheLastOrdinal` (`threads`, pure function): `PageFor(Query{Ordinal: 4294967295}, 3, 25, ...)` is `not-found`.
- In `internal/web/api`, which runs on amd64 only, `?ordinal=4294967295` is `not-found` on the thread page and on a lens page (`?lens=author:...`). These join the existing `lens_chain_test.go` case.
- Commit the 386 step and the tests before the fix, alone, and quote the step's failure on that commit. Under 386, the `Entry` case panics with "index out of range [4294967294]", as the description reports.
- Existing tests keep passing: `lens/chain_test.go` (`PastLast` cases), `TestReplayEqualsIncrementalMaintenance`, and the crash suite.

### Out of scope

- Making `internal/web/api` and `internal/auth/agentcred` tests compile for 386. Their constants are 64-bit by intent (setting limits, timestamps). The fix here makes no claim that ketju supports 32-bit builds. It only removes a panic and a wrong bound. Supporting 32-bit builds would be a release decision, and docs/15 class 3 gives that to the maintainer.
- Any other `int` conversion that is not an ordinal bound check.
