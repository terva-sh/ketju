---
schema: 3
id: TKT-01M38WZPCW43CFXGWSX9971S09
title: Drive Phase 1 to its exit gate autonomously
type: chore
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:ops
  - effort:XL
assignees: []
milestone: phase-1
parent: null
origin: null
dependencies:
  - TKT-01M29CGBY16MK20W1QATC5N7WE
  - TKT-01M29CGBXKH0HBN0HEJGDPBFYV
  - TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
  - TKT-01M29BKBKZW26HNHFM1YFFGVZC
  - TKT-01M29BKBM7004ZPAA9T81HC939
  - TKT-01M29BKBMCNDSYHYF9WXAPDNRN
  - TKT-01M29BN367F3YQZESM63VN26GP
  - TKT-01M29BN36Q9D32WQGYPFS336J0
blocks_on: none
references:
  - ref: design:autonomous-run
    path: docs/12-autonomous-run.md
claim: null
archive: null
created_at: 2026-09-24T05:07:34Z
updated_at: 2026-09-24T05:09:04Z
created_by:
  id: agent:claude/phase1-groom
  name: ""
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

This ticket tracks the autonomous run that takes Phase 1 from its groomed
backlog to its exit gate. The procedure is `docs/12-autonomous-run.md`; this
ticket holds the authority, the scope, and the log.

### Standing merge authority

On 2026-09-23 the user was asked what the autonomous agent may do with a pull
request once CI is green. They chose:

> Merge on green + clean review. Standing authorization: required CI passes,
> Terva review findings dispositioned, then it merges and moves on. Enables a
> true unattended run.

On 2026-09-24 they approved the plan that set out this run, saying "This looks
good, let's move forward with this plan."

For this run's pull requests, that choice is the separate authorization that
`docs/pr-reviews.md` and the `AGENTS.md` review rule require. It does not
extend beyond this run. The gate it permits is narrower than the words above,
and docs/12 states it exactly:
- CI succeeds on the exact head;
- the Terva gate status succeeds on the exact head, so no finding is medium or
  above;
- every low finding is fixed or answered.

It never covers a PR carrying a medium-or-higher finding the agent disagrees
with, a change to the workflows or to `AGENTS.md`, or anything outside
`phase-1`.

The grooming PR that creates this ticket is outside that authority. A person
merges it.

### Scope

Every ticket in milestone `phase-1`, except any labelled `human-only`. The run
is done when TKT-01M29CGBY1 (Playwright smoke tests at 390 px and 1440 px) and
TKT-01M29CGBXK (accessibility pass) are done.

### Decisions the run does not reopen

Recorded in docs/10, taken with the user on 2026-09-24:
- ncruces for SQLite, reached through sqlc with hand-written SQL for the view
  queries (docs/04), confirmed by the TKT-01M29BSPE8 benchmark unless ncruces
  is more than twice as slow;
- a hand-rolled session store, with pre-login state in a sealed cookie;
- hand-written CSS;
- 25 posts per page;
- the reader lands at the last-read post;
- a five-item tab bar and a composer sheet on phones;
- a reverse proxy terminates TLS;
- one process per data directory.

### Queued behind the gate

The real-device checks are TKT-01M38WTKSQ, labelled `human-only`.

## Acceptance criteria

- [ ] TKT-01M29CGBY1 and TKT-01M29CGBXK are done
- [ ] Every merged run PR has a note here with its ticket, PR number, and merge commit
- [ ] docs/11 counts are refreshed from the store at the gate

## Implementation plan

The waves below come from the dependency graph as groomed on 2026-09-24. A
ticket's wave is one more than the highest wave among its open dependencies.
`git ticket ready` stays the source of truth; this list is for choosing among
ready tickets and for estimating what remains.

Critical path: BSPE8 → W9Q1 → BYYHG → C5Y2E → CA9AX → CEE6V → CGBY1.

- W0: BSPE8 (SQLite benchmark, run first), P9RS0 (doc fixes), 38WW7 (go.mod
  tidy), C2WWG (structured render output), CA99R (GET /me), CA9C2 (post,
  replies, history), CCBZ8 (healthz), BYYJG (config check), CCBYG (profiles),
  CCC02 (Preact scaffold), CCC0F (CSS and themes), CCBZN (lock file)
- W1: W9Q1 (DB layer and migrations), CA9BP (edit, delete, restore), CA9D4
  (POST /render), CEE4K (app shell), CEE52 (router), CEE5H (fetch and
  signals, once CA99R lands)
- W2: BYYH7 (users and sessions), BYYHG (forums and permissions), BYYHQ
  (viewer state), BYYJ0 (flags, audit, blobs), BYYJ8 (projection tables),
  CCBYX (serve and shutdown), CEE79 (composer)
- W3: C2WW5 (projection writer), C5Y2E (permission evaluation), C5Y2S (sweeper
  and blob GC), C5Y3E (session store), C800G (bootstrap admin), CA9CE
  (viewer-state endpoints), CGBVN (mobile composer sheet)
- W4: C5Y1S (startup catch-up), C5Y25 (reindex), C7ZZ9 (OIDC flow), C7ZZR
  (single session constructor), C8005 (open registration), CA9A5 (forums
  endpoints), CA9AG (thread list endpoint), CA9AX (thread page endpoint),
  CA9B9 (create thread and reply)
- W5: CCBY0 (response cache), CEE5Y (forum index screen), CEE6D (thread list
  screen), CEE6V (thread page screen)
- W6: CGBW6 (inline reply expander), CGBWN (read beacon), CGBX4 (keyboard
  shortcuts), CGBXK (accessibility pass, gate), CGBY1 (Playwright, gate),
  CGBYH (bundle budget)

Three wave-0 tickets became unblocked in grooming: CCC02 and CCC0F once the UI
decision closed, and CCBZN once the ops decision closed. A second lane
(docs/12, "Running two lanes") can start the client track there.

Each merged PR adds a note here giving the ticket, the PR number, the merge
commit, and anything the next ticket needs.
