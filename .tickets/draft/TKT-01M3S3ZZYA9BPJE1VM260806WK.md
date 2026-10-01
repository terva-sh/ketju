---
schema: 3
id: TKT-01M3S3ZZYA9BPJE1VM260806WK
title: Head the forum index with the site name and show draft-saved status
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:client
assignees: []
milestone: phase-3
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CXK2HYBSGKYBBRHJ5NA0T
  - TKT-01M3NS19315QTWBKNZN782VRF1
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T12:17:55Z
updated_at: 2026-10-01T06:45:52Z
created_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
updated_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
extensions: {}
---

## Description

Found comparing the thread screenshots with `docs/mockups/Thread{Light,Dark}.dc.html` (TKT-01M39WBN4Q, divergences F1 and T9). Both are small and each extends a ready ticket, so they are filed together.

- F1: the forum index's heading is "Forums". The mockup's is the community's name, "The Thursday Group". The note of 2026-09-25 on TKT-01M29CEE5Y left it out because the API had no site name. TKT-01M29CXK2 (Build the admin site settings screen) puts `site_name` into /me and the wordmarks, but its criteria do not mention the page heading.
- T9: the mockup's composer footer shows "Draft saved 12 seconds ago". TKT-01M3NS19 (Autosave reply drafts from the composer) saves drafts, but none of its criteria is a visible saved status.

### What to do
Once those two are done, head the forum index with `site_name` (falling back to "Forums"), and show when the composer last saved the draft.

## Acceptance criteria

- [ ] The forum index's heading is the site name, or Forums when none is set
- [ ] The composer footer says when the draft was last saved
