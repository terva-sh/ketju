---
schema: 3
id: TKT-01M3ART992NAPPBS8PCYAWRT0J
title: Post a reply from the composer
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels: []
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M29CA9B9CEVWZ6A3CH3DZRMF
  - TKT-01M29CEE79NN4Q0RXXEYVJHYYE
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T22:33:12Z
updated_at: 2026-09-24T22:33:56Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

The composer from TKT-01M29CEE79 (Build the composer with server-rendered preview) previews, quotes, replies and autocompletes, but it cannot post yet.

`POST /threads/{id}/posts` and its answer, `CreatedPost` in the plan, belong to TKT-01M29CA9B9 (Implement create thread and reply). When CEE79 was built, CA9B9 was blocked and the response shape was neither documented nor pinned by a golden. A Post button that could only answer 404 would have been worse than none, so it was left out.

### What to build
Add "Post reply" and "Discard" to the composer footer, as ThreadLight draws them. Posting sends `{source, reply_to}` from `state/composer.ts`, using the TS type for the golden CA9B9 adds. Then:
- **On success:** clear the source and reply_to, and show the new post. That means either routing to its permalink or storing the returned page, depending on what CA9B9 returns; the thread cache is keyed on log length.
- **On failure:** show the problem in the composer and keep the source.
- **While in flight:** the button is disabled, so a double click cannot post twice.

TKT-01M29CGBY1's "reply" step needs this.

## Acceptance criteria

- [ ] A reply posts, the composer clears, and the new post shows on the page
- [ ] The posted post's HTML equals the last preview of the same source (TKT-01M29CEE79's AC1, end to end)
- [ ] A refused post keeps the source and says why
- [ ] A double click posts once
