---
schema: 3
id: TKT-01M29CEE79NN4Q0RXXEYVJHYYE
title: Build the composer with server-rendered preview
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:client
  - effort:L
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CA9D4R0YMPNSDZK9XENM6
  - TKT-01M29CCC02K8F14P0SKAYFMSE6
blocks_on: none
references:
  - ref: design:composer
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:22:04Z
updated_at: 2026-09-11T23:22:04Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Markdown with a toolbar, a debounced live preview through `POST /render`, the quote action that inserts an attributed blockquote from selected text, the reply chip that sets reply_to and can be removed, and `@handle` autocomplete.

The composer sits at the bottom of the page after the posts, which is Tildes' reasoning: read before you write. A sticky Reply button scrolls to it.

No client-side markdown parser. The preview is a server round trip, which is both exact and the thing that makes the same preview work for a non-JS client later.

## Acceptance criteria

- [ ] Preview output is identical to the posted result, because both come from the server pipeline
- [ ] Selecting text in a post and clicking quote inserts an attributed blockquote linked to that ordinal
- [ ] The reply chip sets reply_to, is removable, and replying from the bottom composer sets none
- [ ] Mention autocomplete resolves handles and inserts them in the form the renderer expects
- [ ] Preview requests are debounced rather than sent per keystroke
