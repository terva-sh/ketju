---
schema: 3
id: TKT-01M29GGG3W51QT2BBABXW5AMVD
title: Explore what an agent-native board changes in the design
type: spike
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - effort:M
  - decision
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:overview
    path: docs/00-overview.md
  - ref: design:thread-views
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-12T00:33:06Z
updated_at: 2026-09-12T00:33:06Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Captured from a direction the owner raised while the frame codec was being started: the board becomes a place where humans and models both participate as first-class members, rather than a human forum with an API bolted to the side.

Most of the existing design absorbs this without change, which is the encouraging part. A model is a user with an identity, so docs/05 already models it. Posts, ordinals, lenses, and moderation do not care what kind of thing wrote a post.

The places it could bite, in rough order of how expensive they get if decided late:

**Before the format freeze (TKT-01M29C5Y1E).** The record frame carries an 8-byte actor and 2 bytes of flags, plus reserved bytes in the frame and the index entry. If anything about agent authorship belongs *per post* rather than per author, this is the only cheap moment to reserve room for it. Candidates: a generated-content marker, model and version provenance, a reference to the tool-call transcript or conversation that produced the post, a human-approved-this flag. Per-author facts need nothing here; per-post facts need a decision now.

**Identity and permissions (docs/05, docs/04).** Do agents authenticate through OIDC like everyone else, or through a token path? docs/07 currently says personal API tokens are a later phase and are never issued to browser code. An agent-native board probably promotes that path to first-class, which changes the session model's shape.

**Rate limits and moderation (docs/08).** Every limit in the design is sized for a human typing. Agents post faster, and slow mode, flag thresholds, and the auto-hide count are all tuned to human volume. The "what is deliberately missing" list in docs/08 also assumes human social dynamics.

**The linear-thread thesis (docs/06).** This is the interesting one. The argument against trees is that a shared scalar reading position is what makes unread tracking, permalinks, and "write to the room" norms work, and those are arguments about human attention. Whether they survive contact with participants that read the entire thread every time is a real question rather than a rhetorical one.

Deliverable is a written position on each of the four, not an implementation.

## Acceptance criteria

- [ ] A written position on whether anything agent-specific belongs in the frame or index reserved bytes, delivered before TKT-01M29C5Y1E freezes the format
- [ ] A decision on how agents authenticate and whether the token path becomes first-class
- [ ] A statement on whether the linear-thread argument in docs/06 still holds when some participants are models
- [ ] Any change to the documents is made as an edit to them, not left in this ticket
