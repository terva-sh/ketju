---
schema: 3
id: TKT-01M3AZGKBPRQH3M272DPY61CMY
title: "Keep client PRs reviewable: exclude the built bundle from review"
type: chore
status: draft
status_reason: null
priority: high
due_on: null
labels: []
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T00:30:14Z
updated_at: 2026-09-25T03:40:46Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

Terva can no longer review a client PR once it regenerates `internal/web/spa/dist`.

The committed bundle's hashed JS file is one minified line of about 62 KB. A client change deletes the old file and adds a new one, so a PR's diff carries about 125 KB of minified JS before any source. On TKT-01M29CEE79 (composer), PR #77's diff was about 260 KB. Its first review used 89,491 input tokens, and the re-review failed with `context_limit` on the diff alone (Actions run 392). PR #76 had failed the same way (run 386), after five rounds of review bodies also filled its discussion. The maintainer merged #77 with the fix for review 557 unreviewed.

Every client ticket from here on regenerates dist, so each will hit this sooner or later: TKT-01M29CGBVN, CGBW6, CGBX4, CGBXK, CGBY1, and the posting draft.

### Options, for a person, because they touch the review workflow or CI, which the autonomous run may not change
- Have the review exclude `internal/web/spa/dist/**` from the diff it reads. It is generated, CI's "built client is current" step already proves it matches the source, and a model reading minified JS learns nothing.
- Stop committing dist, and build it in CI and in `go generate` for release. This reverses TKT-01M29CCC02's decision that `go build` alone serves the client.
- Split every client PR into a source PR and a dist PR. This doubles the merges, and the source PR's CI would fail the "built client is current" step.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T03:40:46Z

2026-09-25, one more data point.

PR #85 (TKT-01M29CEE5Y, forum index) has a 260 KB diff, of which 190 KB is dist. Its review failed with context_limit (run 445). For comparison, #80 at 220 KB reviewed fine.

Workaround used, pending a person's decision here:
- PR #86 is review-only: #85's commits plus one commit restoring dist to main, a 70 KB diff.
- It is reviewed in #85's place. Its CI fails the built-client check by design and it is never merged.
- #85 merges only if #86's review is clean, and #85's CI, which proves dist matches the source, is green.

This keeps a real review of every source line, but the clean review is for a different head than the one merged. That makes it a stopgap, not a fix: the options above still need choosing.
