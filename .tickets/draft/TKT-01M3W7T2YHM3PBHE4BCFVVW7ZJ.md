---
schema: 3
id: TKT-01M3W7T2YHM3PBHE4BCFVVW7ZJ
title: Keep a lens's Full thread in reach once the reader has scrolled
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels: []
assignees: []
milestone: null
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T17:22:19Z
updated_at: 2026-10-01T17:22:19Z
created_by:
  id: agent:claude-code/phase3-cty71
  name: ""
updated_by:
  id: agent:claude-code/phase3-cty71
  name: ""
extensions: {}
---

## Description

Found while building TKT-01M29CTY71 (PR carrying #328).

The lens header's Full thread follows the post at the top of the viewport (docs/06 "the lens bar"). The header sits above the posts and scrolls away with them, so a reader deep in a long lens page must scroll back up to press it. Scrolling up changes the post at the top, so the press lands near the top of the page rather than where they were reading. A copied link or a new tab taken before scrolling is right. A press after scrolling is not.

### Options

- A sticky lens header, or a compact sticky "Full thread" control.
- A Full thread entry in the post's own footer or more menu, going to that post.
- Leave it to TKT-01M29CGBZD's phone sheet on phones, and decide desktop separately.

Any of these changes the reader bundle a little and the thread page's layout, so it wants a decision against docs/06 and the mockups.
