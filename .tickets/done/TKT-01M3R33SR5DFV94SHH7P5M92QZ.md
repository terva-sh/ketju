---
schema: 3
id: TKT-01M3R33SR5DFV94SHH7P5M92QZ
title: Bound the post edit and delete windows before making Durations
type: bug
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area:moderation
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:218
    path: null
claim: null
archive: null
created_at: 2026-09-30T02:43:17Z
updated_at: 2026-09-30T10:41:34Z
created_by:
  id: agent:claude-code/phase2-run-krh9
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-r33sr5
  name: ""
extensions: {}
---

## Description

Found in Terva review 1535 on PR #193 (TKT-01M29CKRH9). authorWindows in internal/service/threads/mutate.go computes time.Duration(n) * time.Second for posts.edit_window_seconds, posts.delete_window_seconds and posts.grace_edit_seconds. Each setting is atLeast(0) with no upper bound. A count past math.MaxInt64/time.Second, such as 10000000000, overflows into a negative Duration, and within() then treats the window as closed at once. TKT-01M29CKRH9 clamped threads.rename_window_seconds to the longest Duration in renameWindow in moderate.go; the same clamp, or an upper bound on the settings, fixes these.

## Acceptance criteria

- [x] posts.edit_window_seconds, posts.delete_window_seconds and posts.grace_edit_seconds at 10000000000 each leave their window open 30 days after posting, one subtest per setting (TestAuthorWindowsTooLargeForADuration)
- [x] secondsDuration never returns a negative Duration for a count from 0 up, and renameWindow, authorWindows and slowModeWait all convert through it (TestSecondsDuration)
- [x] slow_mode.default_seconds at 10000000000 still refuses a member's second reply with rate-limited (TestSlowModeDefaultTooLargeForADuration)
- [x] No setting's validation rule changes

## Implementation plan

Revised 2026-09-30 at claim, against main at a9e7b8d (after PR #214, daec9af, which reworked `SetSlowMode` with `admitAt` and `stillAt`; this plan does not touch `SetSlowMode`). Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at 9a8fc76. The fix to follow is `renameWindow` in `internal/service/threads/moderate.go` (PR #193, TKT-01M29CKRH9, Terva review 1535 finding 2). A count of seconds past `math.MaxInt64/time.Second` becomes `time.Duration(math.MaxInt64)`, the longest window there is. `TestRenameWindowTooLargeForADuration` in `internal/web/api/rename_test.go` proves it.

### Settled in grooming

- **The defaults are the maintainer's and stay** (docs/08 "Primitives": "default 24 h for edits, 15 min for delete"; grace "default 5"; the rename window of 24 h, maintainer decision 2026-09-29, TKT-01M29CKRH9). The settings are defined in `internal/settings/settings.go` as `posts.edit_window_seconds`, `posts.delete_window_seconds` and `posts.grace_edit_seconds`, each `atLeast(0)`.
- **Clamp at the conversion; do not bound the settings** (engineering choice, following PR #193). An upper bound on a setting lost for two reasons. `Key.In` validates a stored value on every read and returns an error for one outside the rule, so a site that already stores a large value would fail every edit, delete and grace check after the upgrade. And a very large window is a legitimate operator choice, "never close", which the clamp keeps.
- **One helper for every seconds setting in this package** (engineering choice). `renameWindow`, `authorWindows` and the slow mode arithmetic each turn an unbounded `int` into a `time.Duration`. A single `secondsDuration(n int) time.Duration` in `internal/service/threads/mutate.go` does it for all three, so the next setting cannot repeat the bug. A copy of the clamp in each place lost because the copies can drift. Putting the helper in `internal/settings` lost: settings returns plain values and no caller outside this package needs it. `internal/auth/agentcred` already bounds its refill with `between(1, AgentRefillMaxSeconds)` and is not touched.
- **`slow_mode.default_seconds` has the same bug, and this ticket fixes it** (engineering choice, a widening of the description). `slowModeWait` in `internal/service/threads/slowmode.go` computes `time.Duration(seconds)*time.Second` from `effectiveSlowMode`, which falls back to the site default, and the default is `atLeast(0)` with no bound. An overflow makes the wait negative, and `max(0, …)` then turns slow mode off. A thread's own value is capped at `MaxSlowModeSeconds` (a day) by the handler and the `thread_slow_mode` CHECK, so only the site default reaches the overflow. Filing a separate ticket lost: it is the same one-line fix with the same helper.

### Approach

1. `internal/service/threads/mutate.go`: add `func secondsDuration(n int) time.Duration`. It returns `time.Duration(math.MaxInt64)` when `int64(n) > int64(math.MaxInt64/time.Second)`, and otherwise `time.Duration(n) * time.Second`. Its comment cites Terva review 1535 and this ticket. Revised at claim: a negative `n` returns 0 rather than passing through. No setting can hold one (every rule is `atLeast(0)`), and 0 is what every caller already reads as closed or off (`within` treats `d <= 0` as closed, `slowModeWait` treats `seconds <= 0` as off). But a very negative count multiplied by `time.Second` wraps to a positive Duration, an open window, and the helper should not have that edge at all.
2. `authorWindows`: replace `time.Duration(n) * time.Second` with `secondsDuration(n)`.
3. `renameWindow` in `moderate.go`: replace its inline clamp with `secondsDuration(n)`, keeping behaviour identical for every count from 0 up.
4. `slowModeWait` in `slowmode.go`: use `secondsDuration(seconds)`. `time.Time.Add` and `Time.Sub` saturate, so a wait of the longest Duration does not overflow further, and `nextPostAt` reports a `next_post_at` far in the future rather than null.
5. Found at claim: `slowMode` in `create.go` rounds the wait up to whole seconds with `(wait + time.Second - 1).Truncate(time.Second)`. A wait within a second of the longest Duration overflows there into a negative wait, so the refusal is still `rate-limited` but `WriteRetryAfter` drops the `Retry-After` header. Round up only when that cannot overflow, and otherwise truncate, which is at most a second early out of 292 years.
6. No migration, no settings change, no DTO change.

### Tests

- `TestSecondsDuration` (new, `internal/service/threads/windows_test.go`, package `threads`): 0, 1, 86400, `math.MaxInt64/int(time.Second)` exactly, one more, 10000000000 and `math.MaxInt`. The first four convert exactly; the rest give `time.Duration(math.MaxInt64)`. No count from 0 up gives a negative Duration, and a negative count gives 0.
- `TestAuthorWindowsTooLargeForADuration` (new, `internal/web/api/windows_test.go`), one subtest per setting, each after `w.setting(name, 10_000_000_000)` and a fresh post of carol's with the clock moved 30 days on:
  - `posts.edit_window_seconds`: carol's edit answers 200.
  - `posts.delete_window_seconds`: carol's delete answers 200.
  - `posts.grace_edit_seconds`: carol's edit is stored as a grace Edit (`w.editBodies` shows `Grace: true`) and is served with `edited_at` null.
- `TestSlowModeDefaultTooLargeForADuration` (new, in the existing `internal/web/api/slowmode_test.go`): with `slow_mode.default_seconds` at 10000000000 and no thread row, a member's second reply is `rate-limited` with a `Retry-After`, and the page's `viewer.next_post_at` is not null.
- `TestRenameWindowTooLargeForADuration` keeps passing through the shared helper.
- Tests reuse the existing `newWorld` fixture; no new `.Migrate(` call.

### Out of scope

- Bounding any setting's stored value (see Settled).
- The admin site settings screen's validation (TKT-01M29CXK2).

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T04:04:45Z

Groomed 2026-09-30 (TKT-01M3M9RR26). Scope widened as an engineering choice: slow_mode.default_seconds overflows the same way in slowModeWait (internal/service/threads/slowmode.go), where a negative wait turns slow mode off, so the shared secondsDuration helper covers it too. Bounding the settings lost because Key.In validates stored values on every read, so a stored large value would fail every edit after the upgrade.

**agent:claude-code/phase2-groom** at 2026-09-30T08:49:22Z

Plan refreshed against main at 5124209 on 2026-09-30. The plan is current and is not rewritten. Checked: authorWindows (internal/service/threads/mutate.go:47, the conversion at :62), within (:74), renameWindow's inline clamp (moderate.go:223-240), slowModeWait (slowmode.go:140) and effectiveSlowMode's fallback to slow_mode.default_seconds, the four settings (internal/settings/settings.go:121-137), TestRenameWindowTooLargeForADuration (internal/web/api/rename_test.go:382), and w.setting and w.editBodies (internal/web/api/windows_test.go:30, :52). internal/web/api/slowmode_test.go already exists, so TestSlowModeDefaultTooLargeForADuration goes into it; internal/service/threads/windows_test.go is still a new file. Post source (PR #205, ebe7d41) reads no window, so it adds no fourth conversion.

## Summary

`secondsDuration(n int) time.Duration` in `internal/service/threads/mutate.go` is now the one conversion from a seconds setting to a Duration in the threads service. `authorWindows`, `renameWindow` (its inline clamp from PR #193 removed) and `slowModeWait` all go through it. A count past `math.MaxInt64/time.Second` is `time.Duration(math.MaxInt64)`, so `posts.edit_window_seconds`, `posts.delete_window_seconds`, `posts.grace_edit_seconds`, `threads.rename_window_seconds` and `slow_mode.default_seconds` at 10000000000 keep their window open, or their slow mode on, instead of overflowing to negative. A negative count, which no rule admits, is 0 (closed or off).

Found at claim and fixed here: `slowMode` in `create.go` rounded the wait up to whole seconds with `wait + time.Second - 1`, which overflows for a wait within a second of the longest Duration and lost the `Retry-After` header. It now truncates in that one case.

No setting's validation rule, migration or DTO changed; `internal/settings` is untouched. Tests: `TestSecondsDuration` (internal/service/threads/windows_test.go), `TestAuthorWindowsTooLargeForADuration` (internal/web/api/windows_test.go) and `TestSlowModeDefaultTooLargeForADuration` (internal/web/api/slowmode_test.go), with seven mutations tried and each caught (listed in the commit body). TKT-01M3PTT97X, the client, can rely on the server never treating a too-large seconds setting as closed.
