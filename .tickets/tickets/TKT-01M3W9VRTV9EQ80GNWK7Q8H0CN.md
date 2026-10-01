---
schema: 3
id: TKT-01M3W9VRTV9EQ80GNWK7Q8H0CN
title: Wait for a restored notification in TestServeGeneratesNotifications
type: bug
status: in-progress
status_reason: null
priority: high
due_on: null
labels: []
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/phase3-notif-flake
  branch: run/9vrtv-notif-flake
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a10b78c78ef1a6f05
  commit: 367c90db1291b95d161870efa3add79d8c500e48
  session: null
  claimed_at: 2026-10-01T17:58:19Z
  expires_at: null
archive: null
created_at: 2026-10-01T17:58:12Z
updated_at: 2026-10-01T17:58:27Z
created_by:
  id: agent:claude-code/phase3-notif-flake
  name: ""
updated_by:
  id: agent:claude-code/phase3-notif-flake
  name: ""
extensions: {}
---

## Description

Main's push CI run 2184 (head e855e1a, runner build-0 at about 95% CPU) failed in
`go test (race)`:

```
--- FAIL: TestServeGeneratesNotifications (1.61s)
    notifications_test.go:120: the list is empty: {"notifications":[],"next_cursor":""}
```

https://git.local.sothr.com/terva-sh/ketju/actions/runs/2184

### Cause

The test is wrong, not the product. In the test, sam deletes and restores his
own post, so `setDeleted` does not project either record before it answers
(`project = rec.Author != e.Author`). The projection reaches them later,
through the bus. `ListNotifications` drops a row whose `post_meta` row is
deleted. `VisibleNotifications` reads the index only for rows the SQL passed,
in a thread whose projection trails its log. That gives the two sides
different timing:

- After the Delete, the hide happens at once. Either `post_meta` already says
  deleted, or the thread trails and the index says the post is not visible.
- After the Restore, if the projection has applied the Delete and not yet the
  Restore, `post_meta.deleted = 1`, so the row never reaches the index check.
  It comes back once the projection applies the Restore.

docs/04's serving rule says a post notification is served "only while" the
post is visible, and that a restore "brings its notification back". It does
not say how soon. The docs/14 access invariant forbids serving early, and
serving late does not break it. So this is acceptable eventual consistency.
Changing the SQL to send deleted rows to the index check would cost the
filter that keeps a page from coming back short, and would gain only the
milliseconds the projection takes.

`got0` reads the list once, right after the restore, so a loaded runner
fails it. Under 24 busy loops on 12 cores,
`go test -race -count=20 -run TestServeGeneratesNotifications$ ./cmd/ketju/`
failed 4 of 20 runs, every one at line 120.

### Fix

After the restore, poll the list with a deadline, as the test already does for
the first notification. Check that the same row came back. Keep the single
read after the delete, because the immediate hide is part of what the test
proves.

## Acceptance criteria

- [ ] The test polls with a deadline after the restore and checks that the same row came back, with no fixed sleep
- [ ] The read after the delete stays a single read, so the immediate hide is still proven
- [ ] Under a CPU hog, -race -count=20 fails before the change and passes after
- [ ] Other Phase 3 tests in cmd/ketju were checked for the same single read after an async change

## Implementation plan

Test-only change in cmd/ketju/notifications_test.go. A helper, await(cond), polls list() every 20 ms up to a 10 s deadline and returns the last read. It replaces the inline loop for the first notification. The read after the restore becomes await(len == 1), checked to be the same id and ordinal as the first notification; that id is what up_to marks. The read after the delete, and the badge check, stay single reads, because the hide is synchronous: either post_meta says deleted, or the thread trails and VisibleNotifications reads the index. got0 has no other caller and goes. No product change; the reason is in the description. Evidence: loaded.sh (24 busy loops on 12 cores, -race -count=20) before and after.
