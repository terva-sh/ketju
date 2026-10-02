---
schema: 3
id: TKT-01M3ZGWX6045WCKF1R5Q9S52YQ
title: Wait for page 2's posts before reading them in the page-size e2e test
type: bug
status: draft
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
archive: null
created_at: 2026-10-02T23:58:52Z
updated_at: 2026-10-02T23:58:52Z
created_by:
  id: agent:claude-code/phase4-run-9az
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-9az
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
