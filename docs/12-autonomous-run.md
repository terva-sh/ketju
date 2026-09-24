# 12 — The Autonomous Phase 1 Run

Status: in force from the merge of the grooming PR on 2026-09-24 until the
Phase 1 exit gate closes. It covers one run, not a standing policy.

This document tells an agent working without a person present how to take
Phase 1 from its groomed backlog to its exit gate. It repeats nothing that
`AGENTS.md` or [pr-reviews.md](pr-reviews.md) already say; it adds the loop,
the authority the agent has, and the conditions under which it stops.

The tracking ticket is `TKT-01M38WZPCW` (Drive Phase 1 to its exit gate
autonomously). The ticket also records the authority this document grants,
because a ticket is where a person weighs an action with no undo, and the
agent this document constrains can edit this document.

## What the run is for

Phase 1 ends when a friend group can hold a conversation on ketju: sign in,
read a thread, write a reply, come back and see what is new, on a phone as
much as on a desktop ([09-roadmap.md](09-roadmap.md)). The gate that proves it
is `TKT-01M29CGBY1`, Playwright smoke tests at 390 px and 1440 px, beside
`TKT-01M29CGBXK`, the accessibility pass.

The run is done when both are `done`. Everything else in the milestone is on
the way there. Nothing outside the `phase-1` milestone is in scope.

## What was settled before the run started

The user took the decisions the backlog was waiting on during grooming on
2026-09-24. [10-open-questions.md](10-open-questions.md) and the design
document named beside each one record them:

- ncruces for SQLite, reached through sqlc, with hand-written SQL for the view
  queries (04);
- a hand-rolled session store, with pre-login state in a sealed cookie (05);
- ASCII handles, and a fresh install where members read and post (04);
- quarantine, not refusal, for a thread found corrupt at startup (03);
- an optional `mentions` key on `Edit` records, added without a format
  version bump (03);
- hand-written CSS, self-hosted fonts outside the size budget, and 25 posts
  per page (07);
- last-read landing, a five-item phone tab bar with Phase 3 placeholder
  screens, and a composer sheet (06);
- a reverse proxy for TLS, and one process per data directory (02).

**The run does not reopen them.** If the code proves one wrong, that is a stop
condition, not a design change.

One decision is taken but not yet confirmed: the SQLite driver. The spike
`TKT-01M29BSPE8` benchmarks both drivers because its criteria require it, and
it is the first ticket of the run. The choice stands unless ncruces is more
than twice as slow as modernc on the projection write path. If it is, the run
halts.

Every open Phase 1 task carries a pre-groom implementation plan written on
2026-09-24 from the code as it stood then. It is a starting point. Revise it
with `git ticket plan` once you have read the code as it stands when you claim
the ticket, because earlier tickets in the run will have changed it.

## The loop

One ticket, one branch, one pull request. Do not batch tickets into a PR: a
review names an exact head, and a failure in one ticket should not hold
another.

1. **Start from main.** `git fetch origin`, then branch from `origin/main` as
   `run/<short-id>-<slug>`, for example `run/bspe8-sqlite-driver`. Other work
   lands on `main` during the run, including changes to the review workflow;
   always branch from the current tip, never from your previous branch.

2. **Pick.** `git ticket ready` is the queue. Choose in this order:
   1. the next ticket on the critical path, which runs `BSPE8`, `W9Q1`,
      `BYYHG`, `C5Y2E`, `CA9AX`, `CEE6V`, `CGBY1`;
   2. then `urgent` over `high` over `normal`;
   3. then the earlier wave in the tracking ticket's plan;
   4. then the lower ID.

   Skip any ticket labelled `human-only`; it is never yours.

3. **Claim.** `git ticket claim ID`, then `git ticket status ID in-progress`,
   each as its own visible command with stderr left alone. A refused
   transition prints its reason and nothing else tells you it happened. Use
   `--actor agent:<tool>/phase1-run-<session>` on every write.

4. **Plan.** Read the ticket, its design reference, and the code it touches.
   Replace the pre-groom plan with one that fits what you found. If what you
   found contradicts the design documents, stop that ticket (see "When a
   ticket cannot proceed").

5. **Build.** Follow the repository's habits:
   - golden tests with `-update` where a shape is a contract;
   - table-driven tests for rules;
   - property or race tests where concurrency or ordering is the point;
   - a short account in the commit body of the mutations you tried against
     the tests and whether each was caught.

   Before pushing, run what CI runs:

   ```sh
   test -z "$(gofmt -l .)"
   go build ./... && go vet ./...
   go test -race ./...
   ```

   Once the client exists, also run its build, type check and Playwright
   suite as `ci.yml` does at that point.

6. **Record.** Tick each acceptance criterion you earned and no other. For
   one you could not meet, leave it unticked and write a note saying what
   stopped you. If work remains, file a draft for it and link it. Then
   `git ticket summary`, `git ticket status ID done`, `git ticket release ID`.

   The move to `done/` goes in the same pull request as the code, so the
   merge lands both together and no close-out PR is needed. If the PR is
   later refused for good, move the ticket back with a note.

7. **Open the PR.** Push, open it with `tea pulls create`, and put the ticket
   ID and title in the title. Say what changed and why, list any unticked
   criteria, and end with the attribution line. Link the PR to the ticket with
   `git ticket link ID --ref pr:<n>` in a follow-up commit on the same branch,
   before requesting review, so the reviewed head is the head you merge.

8. **Request review.** Request one Terva review of the ready head as
   [pr-reviews.md](pr-reviews.md) describes. That file owns the mechanics and
   may change during the run; read it, do not remember it.

9. **Merge, if and only if the gate holds** (next section). Then confirm the
   PR shows as merged, and only after that delete the branch.

10. **Log.** Append one note to the tracking ticket: ticket, PR, merge commit,
    and anything the next ticket needs to know. Commit it on the next branch
    you create, because `main` takes no direct pushes.

## The merge gate

The user granted this run standing authority to merge its own pull requests,
on 2026-09-24, in the conversation that produced this document; the tracking
ticket quotes it. That authority covers a PR only when all of the following
hold **on the exact head SHA being merged**:

- The status `ci / Build and Test (pull_request)` is `success`. Read the field
  named `status`, not `state`. Rank the event log rather than taking the
  newest row, and let a failure win a tie. Then read the step log and confirm
  the tests ran; a green summary over a skipped step looks the same from
  outside.
- A Terva review ran on that head and its gate status (`terva-review/code`)
  is `success`, meaning no finding at medium severity or above.
- Every low-severity finding is either fixed or answered in a ticket note
  with the review ID, the head and base, and the reason.
- The PR changes nothing outside the ticket's scope, apart from the ticket
  store and documentation the ticket names.

A fix pushed after a review creates a new head, and the review does not carry
over. Request a follow-up review of the new head before merging.

Merge server-side through the API with `{"Do":"rebase"}`, as earlier tickets
did. A refusal comes back as a bare HTTP 405 with no JSON body. Retry once
after a short wait, then read the PR state, because a 405 is sometimes
transient and a merge that reported failure has sometimes succeeded.

**The authority does not cover:**
- a PR the gate has not cleared;
- a PR with a medium or higher finding you disagree with. Write the
  disposition as pr-reviews.md prescribes, mark it pending maintainer
  acceptance, and leave the PR open for a person;
- a PR that changes `.forgejo/workflows/` beyond what "Changing CI" below
  allows, or changes `AGENTS.md` or this document;
- anything outside the `phase-1` milestone.

## Changing CI

Four tickets have to change CI:
- the sqlc regeneration check in `W9Q1`;
- Node and the client build in `CCC02`;
- the bundle budget in `CGBYH`;
- Playwright in `CGBY1`.

The user allowed this on 2026-09-24, within limits. A run PR may:
- add steps to the `Build and Test` job in `.forgejo/workflows/ci.yml`;
- change that job's container image to a Debian-based Go image, because
  Playwright's browsers do not run on Alpine.

A run PR may not:
- rename the job or the workflow, since the required check is matched by
  name;
- change triggers or permissions;
- add a job that would need to become a required check;
- touch `terva-review.yml`, secrets, or branch protection.

Everything the gate needs runs inside the one job that is already required.

## Gaps the design leaves open

The pre-groom plans list the questions each ticket's design leaves open.
Some are named in the ticket itself, and more were found while planning. You
may settle one yourself when the answer affects only internals:
- a name or package boundary;
- a default nobody sees;
- a limit that has no product meaning;
- a query shape.

Settle it the way the nearest document would. Write it into that document in
the same PR, and say so in a ticket note.

Block the ticket instead when the answer changes any of these:
- what a member sees or can do;
- who can see what;
- what an operator must configure;
- what is written to disk in a format that outlives the release.

Those are product, security, and format decisions, and they belong to a
person.

## When a ticket cannot proceed

Block it with `git ticket status ID blocked --reason "..."`. File a draft for
whatever would unblock it, and take the next ready ticket. Block when:

- CI stays red after three honest attempts to fix it;
- a medium or higher review finding cannot be fixed in scope, or you disagree
  with it;
- the design documents contradict each other or the code, and choosing
  between them is a product, security, or format decision (see "Gaps the
  design leaves open");
- the work would change the frozen thread log format;
- the work needs a new external service, a credential, or a setting on the
  forge;
- the ticket turns out to need something outside Phase 1.

## When the whole run halts

Stop, write a handoff, and leave the rest to a person when:

- `git ticket ready` is empty while tickets remain open, which means
  everything left is blocked;
- more than three tickets are blocked at once;
- CI on `main` is red and your work did not cause it;
- the `BSPE8` benchmark finds ncruces more than twice as slow as modernc on
  the projection write path;
- a merged change turns out to have broken something that has already
  landed on `main`. Revert it through a PR, then halt; do not stack fixes on
  a broken base unattended.

## Never

- Force-push, or rewrite a branch someone else has pushed to.
- Change repository or branch-protection settings, required checks, secrets,
  or organization variables.
- Promote a ticket outside `phase-1`, or edit a decided entry in
  [10-open-questions.md](10-open-questions.md).
- Tick a criterion you did not verify, including one that needs a real
  device. Those belong to `TKT-01M38WTKSQ` (Verify the Phase 1 client on real
  iOS and Android devices).
- Put a token, key, or credential into a commit, a ticket, or a handoff.

## Running two lanes

The default is one lane. A second agent can work the client track in its own
worktree, because the client does not touch the Go data path until it needs
the API endpoints:

- `CCC02` scaffold
- `CCC0F` CSS
- `CEE4K` shell
- `CEE52` router
- `CEE5H` fetch and signals

The two lanes share only `internal/dto`. Each lane branches from the current
`main` for every ticket, and a lane that finds its PR conflicting rebases onto
`main` and asks for a fresh review. It does not merge `main` into its branch.

## Ending a session

- `git ticket check` passes.
- Every ticket write is committed.
- The tracking ticket has a note for each merge.
- A handoff in `~/.local/state/agent-handoffs/ketju/` names the ticket in
  hand, its branch and PR, and what state it is in.

When the gate closes:
- Refresh the counts in [11-mvp-critical-path.md](11-mvp-critical-path.md)
  from `git ticket list --all --json`, in the same PR as the last ticket or
  the one after.
- Stop. Closing the Phase 1 epics and the tracking ticket, and running the
  real-device checks, belong to a person.

## Tracking

The tracking ticket holds the wave order, the standing authority in the
user's words, and one note per merged PR. It depends on the gate and on every
Phase 1 epic, so it closes last.
