---
schema: 3
id: TKT-01M29C2WWGF4ERSR2AM2QNEH8C
title: Return structured mentions, quotes, and reply_to from the render call
type: task
status: done
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
  - ref: pr:73
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:15:46Z
updated_at: 2026-09-24T21:20:47Z
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

- [x] A single call returns html, mentions, quotes, and reply_to
- [ ] The preview endpoint and the stored-post path return identical structured output for identical source
- [x] A reply_to pointing at a nonexistent or out-of-thread ordinal is rejected at validation rather than stored
- [x] No consumer of these facts parses markdown itself

## Implementation plan

Revised at claim (2026-09-24), after reading internal/render, internal/dto and internal/service/threads as they stand.

### Approach
1. **`render.Context.ReplyTo *uint32`** is the requested target, and **`render.Result.ReplyTo *uint32`** the validated copy.
   - `RenderWith` checks the target before parsing. With no `Posts` resolver, or when the resolver answers `exists=false` (ordinal 0, past the end, or another thread's), it returns `render.ErrInvalidReplyTo`, so the value is never stored. The caller maps that to validation-failed.
   - A deleted post exists and is accepted. This is the permissive reading of the AC, and refusing it later is a narrower rule that changes no stored fact.
   - reply_to never reaches the HTML, so `Version` and `pipeline.sum` are unchanged, and a test proves the HTML is identical with and without it.
2. **DTOs.** `dto.RenderResult{html, mentions, quotes, reply_to}`, `dto.MentionRef{handle}` (handles only, as settled) and `dto.QuoteRef{ordinal, deleted}`. Ordinals are `int` like every other DTO ordinal. No omitempty; the lists are empty rather than null.
   - Goldens are `render.member.json` (reply_to set, a live and a deleted quote, a mention) and `render-plain.member.json` (reply_to null, empty lists). Both HTML values are the renderer's real output.
3. **`threads.RenderDTO(render.Result) dto.RenderResult`** in internal/service/threads is the one conversion, which the preview (CA9D4) and create (CA9B9) paths both reach. dto may import nothing of ours, and render should not know the wire shape.
4. **AC4 is made checkable.** `internal/render/layering_test.go` fails when any Go file outside internal/render imports goldmark, test files included. It is proved live against testdata/layering.
5. **docs/07's POST /render row** gives the element shapes. The request shape stays CA9D4's to settle, because `{source}` alone cannot carry reply_to or a thread.

### Tests
- **AC1**: `TestOneCallReturnsEveryFact` gets HTML, mentions, quotes and reply_to from one `RenderWith`. `TestRenderDTOMatchesTheGoldens` renders the golden's source and compares the converted JSON byte for byte.
- **AC2**: `TestTheSameSourceGivesTheSameFacts` shows the same source and thread give the same DTO through the one conversion. Endpoint-level equality is CA9D4's AC1 and AC3.
- **AC3**: `TestReplyToMustNameAPostInTheThread`, with past the end, zero, another thread's resolver, and no resolver.
- **AC4**: `TestOnlyRenderImportsGoldmark` and `TestTheGoldmarkWalkFindsAStrayImport`.

### Design
docs/06 "Composer"; docs/07 `POST /render`; the render.go package doc (version rule).

### Interactions
Unblocks CA9D4 (POST /render) and CA9B9 (create/reply), which call `RenderWith` with `ReplyTo` and answer through `RenderDTO`. CEE79 (composer) consumes `dto.RenderResult`.

### Settled in grooming (2026-09-24)

- Mentions are exposed as handles, not user ids. No DTO exposes user ids.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T21:15:14Z

Built and verified (2026-09-24).

- `go test -race ./...`, `go vet ./...` and gofmt are all clean. The existing render goldens and pipeline.sum are unchanged.
- Mutants: 12 over render.go's reply_to validation, RenderDTO and the layering walk are all killed.
  - One first-pass "kill" was only a compile failure of a mutated test, and it exposed that the goldmark walk's liveness was unproven. The walk is now a function, tested against a fixture tree.
  - A guard in it that could not fire was removed.

**AC2 is left unticked.** It names the preview endpoint, which is TKT-01M29CA9D4's, and the stored-post path, which is TKT-01M29CA9B9's; neither exists yet. What exists now is the single conversion both will use (threads.RenderDTO), with a determinism test through it. The endpoint-level equality is CA9D4's AC1 ("Output is identical to what the post read path returns") and AC3 ("the same structured output the create path uses"), so no new draft is filed.

**Decisions:**
- **A reply to a deleted post is accepted.** The AC refuses a nonexistent or out-of-thread ordinal. A deleted post exists, and the docs leave open whether it may be answered. Accepting is the permissive reading, and it can be narrowed later in RenderWith without changing any stored fact. This is flagged for the maintainer: if replies to deleted posts should be refused, it is a one-line change here plus a test.
- **RenderDTO lives in internal/service/threads.** dto imports nothing of ours, render should not know the wire shape, and both CA9D4 and CA9B9 go through the threads service.
- **QuoteRef carries deleted as well as the ordinal**, so the composer can say a quoted post is gone. It is a fact the render already has.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T21:20:47Z

Two things on head 2e6b391, both fixed.

1. CI run 356 failed at the client tests: web/client's contract test requires every Go golden to be type-checked against dto.ts, and the two new render goldens were not. dto.ts now has RenderResult, and dto-contract.ts checks render.member and render-plain.member. My mistake: before pushing I ran only routes.test.tsx rather than the whole client suite.

2. Review 516 raised one medium, accepted: converting a validated uint32 ordinal to int loses range on a 32-bit build. The reviewer offered a wider wire type or rejecting out-of-range values, and the wire type was taken. RenderResult.ReplyTo and QuoteRef.Ordinal are now uint32, the renderer's and the log's own type, so RenderDTO converts nothing. Rejecting large values in RenderWith lost, because narrowing what the quote syntax accepts would change output and force a render Version bump. As it turns out, a quote's ordinal is at most nine digits by its syntax, so only reply_to could ever have overflowed. TestRenderDTOKeepsEveryOrdinal carries MaxUint32 through. The older DTOs (Post.Ordinal int, Post.ReplyTo *int) have the same latent 32-bit limit in threads/view.go, which this PR does not touch; no platform statement in the docs names 32-bit as a target.

## Summary

One RenderWith call now returns the HTML with every fact the post carries: mentions, quotes, and a reply_to validated against the thread. A reply to a post that is not in the thread fails with ErrInvalidReplyTo and is never stored. dto.RenderResult (with MentionRef as a handle and QuoteRef as an ordinal plus deleted) is the wire shape, pinned by goldens made from real renders, and threads.RenderDTO is the one conversion the preview and create paths will share. A layering test keeps goldmark inside internal/render. AC2's endpoint-level check waits on TKT-01M29CA9D4.
