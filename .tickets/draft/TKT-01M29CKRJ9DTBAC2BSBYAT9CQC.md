---
schema: 3
id: TKT-01M29CKRJ9DTBAC2BSBYAT9CQC
title: Render tombstones for deleted and moved posts
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:moderation
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CA9AXKRTS6KMN5CTH7461
  - TKT-01M29BYYGYZ8FQB2W2XC14S2T3
blocks_on: none
references:
  - ref: design:tombstones
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-11T23:24:59Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

"Post removed by author" or "Post removed by a moderator", with the ordinal preserved so numbering, permalinks, and reply lenses keep working. Moderators see the original inline on a distinct background.

Replies to a deleted post keep their reply_to link. A tombstone is a gap in content, never a gap in the sequence, and that is the property that keeps a linear thread readable after moderation.

## Acceptance criteria

- [ ] A tombstone keeps the ordinal and its permalink resolves
- [ ] Replies to a deleted post keep their reply_to indicator and expander
- [ ] Moderators see original content inline and members never receive it in any response
- [ ] A moved post renders as a pointer to its new location
