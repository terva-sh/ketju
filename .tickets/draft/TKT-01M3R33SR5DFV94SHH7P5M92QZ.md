---
schema: 3
id: TKT-01M3R33SR5DFV94SHH7P5M92QZ
title: Bound the post edit and delete windows before making Durations
type: bug
status: draft
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
references: []
claim: null
archive: null
created_at: 2026-09-30T02:43:17Z
updated_at: 2026-09-30T02:43:17Z
created_by:
  id: agent:claude-code/phase2-run-krh9
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-krh9
  name: ""
extensions: {}
---

## Description

Found in Terva review 1535 on PR #193 (TKT-01M29CKRH9). authorWindows in internal/service/threads/mutate.go computes time.Duration(n) * time.Second for posts.edit_window_seconds, posts.delete_window_seconds and posts.grace_edit_seconds. Each setting is atLeast(0) with no upper bound. A count past math.MaxInt64/time.Second, such as 10000000000, overflows into a negative Duration, and within() then treats the window as closed at once. TKT-01M29CKRH9 clamped threads.rename_window_seconds to the longest Duration in renameWindow in moderate.go; the same clamp, or an upper bound on the settings, fixes these.

## Acceptance criteria

- [ ] A window setting past what a time.Duration holds leaves the window open rather than closed, with a test per setting
