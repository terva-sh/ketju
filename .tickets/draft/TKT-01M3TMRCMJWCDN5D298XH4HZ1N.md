---
schema: 3
id: TKT-01M3TMRCMJWCDN5D298XH4HZ1N
title: Close the shortcuts overlay on an Escape pressed before it takes focus
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - area:test
  - effort:S
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T02:30:06Z
updated_at: 2026-10-01T02:30:06Z
created_by:
  id: agent:claude-code/phase2-run-sz3gce
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-sz3gce
  name: ""
extensions: {}
---

## Description

Found by the TKT-01M3SZ3GCE lane on 2026-10-01, while reproducing that ticket's flake under load. `web/client/e2e/shortcuts.spec.ts:73`, on the desktop project, failed in 2 of 21 full-suite runs on main (tree f3ba70d). The runs were in the CI image (golang:1.27-alpine with Alpine's Chromium 152.0.7977.82), in three containers limited to 2 CPUs each, running at the same time under 24 busy host loops. Each run took 8 to 9 minutes, as CI's runs do when other jobs share the runner. The failing runs are labelled par-c2 run 2 and par-c2 run 4.

```
Error: expect(locator).toHaveCount(expected) failed
Locator:  getByRole('dialog', { name: 'Keyboard shortcuts' })
Expected: 0
Received: 1
Timeout:  5000ms
```

The step is `page.keyboard.press('Escape')`, followed by `expect(dialog).toHaveCount(0)`.

### The likely cause (not yet proven)

`HelpOverlay` (web/client/src/keyboard/HelpOverlay.tsx) closes on Escape through `onKeyDown` on the dialog element. So it catches Escape only when focus is inside the dialog. A `useEffect` moves focus to Close, and Preact runs that effect after the next frame. The spec checks that the dialog is visible and that its list is right, then presses Escape at once. If the effect has not run yet under load, focus is still on the post, the key never reaches the dialog, and the dialog stays open. A person who presses `?` and then Escape quickly would see the same thing.

### What to do

1. Confirm the cause from a trace: focus at the moment Escape is pressed.
2. Fix it in the app, not the spec. For example, move focus in `useLayoutEffect`, or handle Escape for the open overlay at the document level.
3. Add a vitest test that presses Escape before the effect would have run.
4. Show 20 runs under load without the failure. Do not add a retry, a longer timeout or a skip.
