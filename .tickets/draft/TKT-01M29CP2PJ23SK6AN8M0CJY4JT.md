---
schema: 3
id: TKT-01M29CP2PJ23SK6AN8M0CJY4JT
title: Implement flags with the auto-hide threshold
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:moderation
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29BYYJ0WJ3C7RRZQMDRP5ZB
  - TKT-01M29CA9CECTE02DFZGBGN2HC2
blocks_on: none
references:
  - ref: design:flags
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-28T22:39:20Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

Any member can flag a post with a reason of spam, abuse, off-topic, or other with text. A post reaching the threshold of open flags, default 3 and a setting, is auto-hidden pending review through a Delete variant marked `auto=true`, which a moderator's Restore clears.

Reporters see "thanks, a moderator will look" and nothing else. Telling a reporter what happened to their report turns moderation into a conversation with the reporter, and in a small community that is how grudges start.

## Acceptance criteria

- [ ] A post reaching the threshold is auto-hidden and the hide is recorded in the thread log
- [ ] A moderator Restore clears the auto-hide and the flags stay for the record
- [ ] A reporter sees only the acknowledgement, whatever the outcome
- [ ] A member cannot flag the same post twice to inflate the count

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T22:29:23Z

Maintainer decision, 2026-09-28: the threshold defaults to 3 and is a site setting. While a post is auto-hidden, its author and moderators see it marked 'hidden pending review', and everyone else sees a tombstone. The auto-hide is hidden-by-mod, so the author cannot restore it; only a moderator can. Hiding it from the author too lost because it tells them they were reported. Queue-only lost because spam would stay up. Written into docs/08 'Flags'. Carry this into the plan's Settled section.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T22:39:20Z

Supersedes the rationale in the 2026-09-28 decision note, not the decision. Showing the author 'hidden pending review' does tell them flags put the post under review, and that is intended. What stays confidential is who flagged it and how many did. Hiding the post from the author too lost because a tombstone tells them the same thing. docs/08 is corrected to match. From Terva review 1280 on PR #134.
