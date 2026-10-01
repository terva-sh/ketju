---
schema: 3
id: TKT-01M3WBBF4HDDDFP2MPK9774ZRN
title: Stop TestServeGeneratesNotifications racing the restore's notification
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:test
  - area:api
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T18:24:14Z
updated_at: 2026-10-01T18:24:14Z
created_by:
  id: agent:claude-code/phase3-tszhsw
  name: ""
updated_by:
  id: agent:claude-code/phase3-tszhsw
  name: ""
extensions: {}
---

## Description

Found by the TKT-01M3TSZHSW lane on 2026-10-01 while running `go test -race ./...`. `cmd/ketju/notifications_test.go:120` sometimes fails with `the list is empty: {"notifications":[],"next_cursor":""}`.

- On main at 86752c4, run from a clean `git archive` with `go test -race ./cmd/ketju -run TestServeGeneratesNotifications`, it failed 1 time in 45 runs. The host load average was about 17 to 24.
- On the TKT-01M3TSZHSW branch, which changes no Go code, it failed 5 times in 15 runs under the same load.

Line 120 is the `got0(t, list)` call that comes right after the restore. The test polls for the first notification for up to 10 s (line 89). After the restore it reads the list once, with no wait. If the restore brings the notification back through the event bus, as the reply did, the read can come before the generator has run. The test then fails on an empty list.

### What to do
Confirm whether the restore's notification comes back asynchronously. If it does, make the test wait for the condition it reads, as it already does for the first notification. If the restore is meant to answer only once the notification is back, fix that in the server instead. Do not add a retry around the assertion. The wait has to be on the list holding the notification.
