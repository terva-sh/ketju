---
schema: 3
id: TKT-01M3TS0ZV9V7WRQQ4QM5RZC3D1
title: Take Escape in the flag, moderate and forum-picker dialogs before focus
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
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
created_at: 2026-10-01T03:44:42Z
updated_at: 2026-10-01T03:44:42Z
created_by:
  id: agent:claude-code/phase2-run-tmrcmj
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-tmrcmj
  name: ""
extensions: {}
---

## Description

Found while grooming TKT-01M3TMRCMJ (Close the shortcuts overlay on an Escape pressed before it takes focus) on 2026-10-01, against main at c3de983.

Three modal dialogs have the race that ticket fixes in `HelpOverlay`:

- `web/client/src/components/thread/FlagDialog.tsx`
- `web/client/src/components/thread/ModerateDialog.tsx`
- `web/client/src/screens/ForumPicker.tsx`

Each moves focus into itself in a `useEffect`, which Preact runs after the next frame (`afterNextFrame`: a `requestAnimationFrame` or a 35 ms timeout). Each catches Escape and Tab only in the dialog's `onKeyDown`. A key pressed between the render and that frame is dispatched on whatever opened the dialog, so the dialog never sees it. ForumPicker's own comment says focus goes inside "so Escape and Tab always reach the dialog's handler" (review on PR #86), but that holds only once a frame has passed. Under load, a frame can take hundreds of milliseconds.

### What to do

Apply what TKT-01M3TMRCMJ lands for HelpOverlay to each dialog: focus moved in a `useLayoutEffect`, and Escape and Tab taken on the document while the dialog is mounted. With three callers, extract a shared hook into `components/focus.ts` if the three fit one. For each dialog, add a Vitest case that opens the dialog outside `act` and presses Escape before a frame passes. It must fail before the change.

ComposerSheet already uses `useLayoutEffect` and is not affected in the same way. Check it anyway.
