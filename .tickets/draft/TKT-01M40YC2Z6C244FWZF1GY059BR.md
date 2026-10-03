---
schema: 3
id: TKT-01M40YC2Z6C244FWZF1GY059BR
title: Measure the client's first load, not every chunk it could load
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:client
  - area:ci
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-03T13:13:35Z
updated_at: 2026-10-03T13:13:35Z
created_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

The reader bundle budget (docs/07 "Preact client", `web/client/budget.json`, `scripts/bundle.mjs`) measures the wrong thing. On 2026-10-03, reviewing Phase 4, the maintainer said the size limit "seems really nonsensical for anything under 1MB". They asked for this ticket to change what is measured and tested, and not to do it now.

### What it measures today

CI adds up the gzipped JavaScript and CSS of every file the reader can load, lazy chunks included, except fonts and the lazy admin chunk. It fails above 120000 B. The reader was 92290 B at c7fec0e8.

### Why that is the wrong measure

- **It counts code a reader never loads.** A lazy chunk counts the same as the entry. Splitting code out therefore saves nothing, as docs/07 has noted twice. Phase 4's split work showed it again: the post-selection chunk loads only when a moderator selects posts, yet it counts against every reader.
- **It does not measure what a phone pays.** The cost that matters is what the thread page fetches, parses and runs before it works. Code the bundler preloads belongs in that number. Code only an action loads does not.
- **The cap turned into a decision point.** Raising it is a stop class for the autonomous runs (docs/14, docs/15), so every lane reports byte deltas and the maintainer is asked to move a number that measures nothing they care about. It was raised from 60 KB to 70 KB to 120 KB in two days.

### What to do

The maintainer has set the direction: keep a sanity ceiling on total size, set around 1 MB, and test what the reader actually experiences. The details are this ticket's to settle:
- **First-load budget:** the bytes the thread view fetches before it is interactive, meaning the entry, every preloaded and modulepreload chunk, and CSS. Lazy chunks are left out.
- **Total ceiling:** about 1 MB of all client code, as a guard against something gross such as a dependency landing by accident, not as a target.
- **Behaviour check (optional):** time to interactive on the thread page, measured in Playwright under CPU and network throttling at 390 px. Any such check needs a margin generous enough that it does not flake on a loaded runner; see TKT-01M3VDYENN.
- **Docs and runs:** update docs/07 and `bundle.mjs`/`bundle.test.mjs`, and say what the runs' stop class 5 means once this lands.

Alternatives to weigh in the plan:
- dropping the budget entirely;
- a time-only check;
- keeping 120 KB but excluding lazy chunks.

## Acceptance criteria

- [ ] docs/07 "Preact client" says what is measured and why, and records the maintainer's 2026-10-03 direction and the alternatives that lost
- [ ] CI fails when the thread view's first-load bytes (entry, preloaded chunks and CSS, gzip) exceed a committed number, and lazy chunks do not count toward it
- [ ] CI fails when the total of all client code exceeds a committed ceiling of about 1 MB
- [ ] bundle.test.mjs proves each check fails on a build over its number and passes otherwise, and that a lazy-only chunk moves the total but not the first-load figure
