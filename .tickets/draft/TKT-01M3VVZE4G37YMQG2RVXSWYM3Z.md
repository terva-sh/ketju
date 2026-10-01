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
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T13:55:32Z
updated_at: 2026-10-01T13:58:07Z
created_by:
  id: agent:claude-code/phase3-lenses
  name: ""
updated_by:
  id: agent:claude-code/phase3-lenses
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
