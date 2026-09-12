---
schema: 3
id: TKT-01M29BN36D34YVKVRPNQD3PHDV
title: Lenses, unread surfaces, search, and live updates
type: epic
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:views
  - effort:XL
assignees: []
milestone: phase-3
parent: null
origin: null
dependencies:
  - TKT-01M29BN367F3YQZESM63VN26GP
blocks_on: children
references:
  - ref: design:lenses
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:08:14Z
updated_at: 2026-09-11T23:32:15Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Everything that answers "what is new and where is the part I care about", without ever reordering a thread.

A lens is a filter, marker, or overlay over the ordered post list. It never reorders and never indents. Most lenses are computed server-side from the in-memory index alone, which is what the 40-byte index entry buys: author, reply_to, and created_at are there, so the author, chain, and since lenses never touch the database.

Every lens is URL-addressable through `?lens=`, so a lens can be shared and the future non-JS client gets it for free. A lens header always shows the count of hidden posts and a one-click return to the full thread at the same ordinal.

The cross-forum surfaces (unread, new, participated, watching) are the "what's new" navigation. On phones they are the bottom tab bar, which is the whole argument in docs/10 against hiding them behind a hamburger.

Live updates are deliberately dumb. SSE carries `{thread_id, log_length}` hints with no content and no per-viewer logic, and the client reconciles through the normal authorized read path.

## Acceptance criteria

- [ ] Every lens is reachable by URL alone with no client-only state
- [ ] The author, chain, and since lenses execute without a database query
- [ ] Search returns FTS5 snippets and the index is rebuildable with `ketju reindex --search`
- [ ] SSE payloads carry no post content, and authorization happens on the follow-up fetch
- [ ] New posts never move the page under a reader who is not at the bottom; they arrive behind a 'N new posts' pill
- [ ] Polling every 30 s takes over when SSE is unavailable
