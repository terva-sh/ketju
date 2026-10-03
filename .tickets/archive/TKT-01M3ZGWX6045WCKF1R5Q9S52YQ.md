---
schema: 3
id: TKT-01M3ZGWX6045WCKF1R5Q9S52YQ
title: Wait for page 2's posts before reading them in the page-size e2e test
type: bug
status: archived
status_reason: null
priority: normal
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
archive:
  archived_at: 2026-10-03T01:08:15Z
  from_status: draft
  reason: Folded into TKT-01M3ZE2211, which fixes it in the same PR
created_at: 2026-10-02T23:58:52Z
updated_at: 2026-10-03T01:08:15Z
created_by:
  id: agent:claude-code/phase4-run-9az
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-ze2211
  name: ""
extensions: {}
---

## Description

`web/client/e2e/phase3-exit.spec.ts:446`, "a member changes their page size in settings and the thread pages by it", failed once in CI run 2558 (PR #380, head b235cb03, desktop project):

```
await page.goto(`${seed.thread}?page=2`);
expect((await ordinals(page))[0]).toBe(11);   // Received: undefined
```

`ordinals` (line 75) is `locator.evaluateAll`, which reads the DOM at once and does not wait. After `goto` the thread screen fetches the page and renders the posts afterwards, so on a loaded runner the list can still be empty when it is read. The page-1 half of the same test waits first (`toBeVisible` on the first post, then `toHaveCount(10)`), and the page-2 half does not.

The change under test in that run (quote backlinks, server only, no dist change) does not touch the client, and 288 other tests passed.

### Fix

Wait for page 2's first post before reading, for example `await expect(page.locator('.posts > [id^="post-"]').first()).toHaveAttribute('data-ordinal', '11')`, or poll `ordinals` with `expect.poll`. `lenses.spec.ts` has the same `ordinals` helper; check its callers for the same unawaited read.

## Notes

**agent:claude-code/phase4-run-ze2211** at 2026-10-03T01:08:14Z

Folded into TKT-01M3ZE2211 (Stop the Phase 3 exit Unread surface test failing under CI load) at the orchestrator's direction: that ticket's scope is the Phase 3 exit spec under CI load, and this is its fifth failure. The cause is as filed: page.goto returns at the load event and the thread page draws its posts after its API answer, so the one-shot ordinals read found none. TKT-01M3ZE2211's PR polls the first ordinal on page 2 instead (expect.poll), and was checked by delaying /api/v1/threads/* 800 ms through page.route: 10 of 10 failed before, 10 of 10 passed after. lenses.spec.ts's callers of the same helper each wait first for the lens header or count, which come from the same answer, so they were left alone. Archived rather than removed, because it carries the run 2558 evidence.

**agent:claude-code/phase4-run-ze2211** at 2026-10-03T01:08:15Z

archived from draft: Folded into TKT-01M3ZE2211, which fixes it in the same PR
