---
schema: 3
id: TKT-01M29C2WWGF4ERSR2AM2QNEH8C
title: Return structured mentions, quotes, and reply_to from the render call
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area:render
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBM7004ZPAA9T81HC939
origin: null
dependencies:
  - TKT-01M29C0WEEPGNGE9EDDTK1AZHN
  - TKT-01M29C0WEQ2SBTSZSHSV8Z989S
blocks_on: none
references:
  - ref: design:api-render
    path: docs/07-api-and-clients.md
claim:
  actor: agent:claude-code/phase1-run-415af6aa
  branch: run/c2wwg-render-structure
  worktree: /home/sothr/.t3/worktrees/ketju/t3code-415af6aa
  commit: 8f1d368fbcb577467b2fd90f4553c9a446723b12
  session: null
  claimed_at: 2026-09-24T21:09:58Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:15:46Z
updated_at: 2026-09-24T21:09:58Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

One render call returns HTML plus the structured facts the post carries: resolved mention user ids, quoted ordinals, and the validated reply_to. The composer preview endpoint returns the same shape, so the client sees before posting exactly what the server will record.

Extraction happening at render time is what keeps the projection writer, the notifier, and the search indexer from each parsing markdown in their own slightly different way.

## Acceptance criteria

- [ ] A single call returns html, mentions, quotes, and reply_to
- [ ] The preview endpoint and the stored-post path return identical structured output for identical source
- [ ] A reply_to pointing at a nonexistent or out-of-thread ordinal is rejected at validation rather than stored
- [ ] No consumer of these facts parses markdown itself

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. In `internal/render/render.go`, add `Context.ReplyTo *uint32` (the requested target) and `Result.ReplyTo *uint32` (the validated one). `RenderWith` validates it through `Context.Posts`. A nil resolver with a non-nil ReplyTo, or a resolver that answers `exists=false`, returns a new sentinel `ErrInvalidReplyTo`. The request maps it to `validation-failed` (422), so the bad value is never stored.
2. Leave the HTML unchanged: reply_to never reaches the output markup, so `Version` stays 3 and `pipeline.sum` does not move. Say so in the PR, because the version rule in the package doc will prompt the question.
3. Add `dto.RenderResult {html, mentions, quotes, reply_to}` plus `dto.MentionRef` and `dto.QuoteRef` to `internal/dto`, with no omitempty, per the package's null rule. Add fixtures to `fixtures_test.go` and goldens `render.member.json`, plus a second golden with `reply_to` set, because every nullable field must appear both null and set.
4. One conversion function from `render.Result` to `dto.RenderResult`, called by both the preview path (CA9D4) and the create path (CA9B9). internal/service does not exist yet, so put it wherever CA9B9 and CA9D4 will both import. It cannot go in dto, which imports nothing of ours.
5. Add a structural test (modelled on `internal/dto/layering_test.go`) asserting that no non-test package outside `internal/render` imports `github.com/yuin/goldmark`. That is AC4, "no consumer parses markdown", made checkable.
6. Decide at claim whether JSON exposes mention `user_id` or only `handle`. No DTO exposes user ids today; see interfaces item 1.

### Files
`internal/render/render.go`, `render_test.go`; `internal/dto/dto.go`, `fixtures_test.go`, `testdata/golden/render*.json`; the new conversion function; a new layering test (location decided at claim).

### Tests
- AC1: a table test in `render_test.go` checks one `RenderWith` call returns HTML, mentions, quotes and ReplyTo together. The dto golden pins the JSON.
- AC2: a determinism test feeds the same source and Context twice through the conversion and deep-compares the results. The HTTP-level equality of preview and stored output lands in CA9D4 and CA9B9, which depend on this ticket.
- AC3: cases for a nonexistent ordinal, a nil resolver, and a resolver scoped to a different thread (answers false) each return `ErrInvalidReplyTo`.
- AC4: the goldmark import test. `go test ./internal/render ./internal/dto`; existing goldens unchanged.

### Design
docs/06 "Composer" (quote, reply, mention rules); docs/07 resource table row `POST /render`; the `render.go` package doc (version rule); the notes on done tickets C0WEE and C0WEQ.

### Interactions
Unblocks CA9D4 (POST /render) and CA9B9 (create/reply). CEE79 (composer) consumes `dto.RenderResult`. docs/07 gives the request as `{source}` only, which cannot carry reply_to or a thread for resolution; interfaces item 2.
Open in docs: whether a reply may target a deleted post.

### Settled in grooming (2026-09-24)

- Mentions are exposed as handles, not user ids. No DTO exposes user ids.
