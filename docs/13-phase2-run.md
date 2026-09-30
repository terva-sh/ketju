# 13 — The Autonomous Phase 2 Run

Status: in force from the merge of this document until every `phase-2`
ticket is done. The exit gate closed on 2026-09-30, and the maintainer then
extended the run to the rest of Phase 2 (see "The extension of 2026-09-30").
Like [12-autonomous-run.md](12-autonomous-run.md), it covers one run, not a
standing policy.

This document takes Phase 2 from its groomed backlog to its exit gate. It is
written as changes to docs/12, because the Phase 1 run proved that document's
loop. **Every section of docs/12 applies unless a section below replaces it.**
Where docs/12 says `phase-1`, read `phase-2`. Where it names Phase 1 tickets,
read the Phase 2 tickets below.

The tracking ticket is `TKT-01M3NTSCJQ` (Drive Phase 2 to its exit gate
autonomously). It quotes the grant.

## The grant

On 2026-09-29 the maintainer wrote: "Let's pickup what you recommend next and
continue our work through phase 2 autonomously merging after reviews are
complete and issues resolved or disagreements recorded."

Under this grant:
- The run may merge its own grooming and implementation PRs on the docs/12
  merge gate.
- The run may promote `phase-2` drafts to `ready` once they carry a groomed
  plan and have no open decision.
- **The rule for disagreements changes from docs/12.** docs/12 leaves a PR
  open when the agent disagrees with a medium or higher finding. Under this
  grant, the agent records the disagreement and then merges. The record is:
  - a ticket note naming the review ID, the head and base, the finding's
    title, and the reason, marked `disagreement: pending maintainer`;
  - a PR comment saying the same.

  The run's report lists every recorded disagreement. The maintainer may
  revert any of them.
- **A disagreement is an argument, not an absence.** "The fix is out of
  scope" is a reason only when a filed ticket owns the fix and the note names
  that ticket. Security findings are never answered by argument alone. A
  finding about sessions, roles, status, access to posts, or credentials is
  fixed, or the ticket is blocked.

The gate itself does not change:
- CI succeeds on the exact head;
- a Terva review ran on that head;
- every finding is fixed, answered, or recorded as a disagreement;
- the diff stays in scope.

A review that failed to run is still not a clean review.

## What the run is for

Phase 2 ends when "a club can open registration to strangers safely, and an
admin can give an agent an account and a credential"
([09-roadmap.md](09-roadmap.md)). The gate that proves it is `TKT-01M3NTSCMN`
(Prove Phase 2's exit: approval registration and agent credentials e2e). It is an end-to-end test of approval-mode
registration: apply, review, approve, sign in, and a refused pre-user reaching
member routes. It also tests an admin provisioning an agent and the agent
posting with its credential. The run is done when that ticket is `done`.

## The extension of 2026-09-30

The exit gate closed on 2026-09-30 with `TKT-01M3NTSCMN` (PR #194). The
first run's tracking ticket, `TKT-01M3NTSCJQ`, is done, and its summary is
that run's report. The maintainer then wrote: "Let's start grooming and
promoting the phase 2 tickets so we can continue autonomously". Asked how far
the run should go, they chose all of Phase 2, on the same terms.

So, under the extension:
- **The grant above holds unchanged.** The run merges on the gate. It records a
  non-security disagreement and then merges. It always fixes a security
  finding. It caps review at five rounds, and it may promote a groomed
  `phase-2` draft that has no open decision.
- **The run is done when every `phase-2` ticket is done or archived.** That
  includes tickets the run files and grooms along the way. The two Phase 2
  epics close once their children do.
- **The tracking ticket is `TKT-01M3RQP0VZ`** (Drive the rest of Phase 2 to
  done autonomously). The orchestrator logs merges there, and its summary is
  the report: every recorded disagreement, every criterion merged unticked,
  and every open question.
- **Plans were refreshed against `main` on 2026-09-30** before the remaining
  tickets were promoted. As before, revise the plan when you claim a ticket,
  and carry its `### Settled in grooming` forward.
- The maintainer's decisions of 2026-09-30 are in docs/08 "Decided
  2026-09-30". In short: thread moderation follows the forum grant; the flag
  receipt stays as built in #202; and the CI race step keeps its timeout.

## What was settled before the run started

Grooming ran on 2026-09-28 and 2026-09-29. Its decisions are recorded in
docs/03, 04, 05, 07, 08 and 10, and each is cited in a `### Settled in
grooming` section of the ticket it binds. **The run does not reopen them.**

Every open Phase 2 ticket carries a plan written against `main` as it stood
during grooming. As in docs/12, revise the plan when you claim the ticket,
and carry its `### Settled in grooming` forward unchanged: `git ticket plan`
replaces the whole section.

A ticket whose plan has an `### Open decision` stays in `draft` until the
maintainer answers. The orchestrator batches those questions.

## The loop, changed

- Use the actor `agent:<tool>/phase2-run-<session>`.
- Pick in this order:
  1. dependency order, since `git ticket ready` lists only tickets whose
     dependencies are done;
  2. server tickets before the client screens that need them;
  3. `high` over `normal`;
  4. the lower ID.
- Cap review rounds at five per PR. After five, block the ticket and tell the
  orchestrator. Splitting the out-of-scope part into a filed ticket usually
  ends the loop.

## Invariants every PR must keep

These are named in the plans. A PR that weakens one is out of scope, whatever
its ticket says:
- **One session chokepoint.** Sessions are issued only through the `session`
  package's issue functions, which check the user's status. The chokepoint
  test's caller list stays honest.
- **Role and status changes.** They go through `perm`, with the audit row
  written in the same transaction.
- **The response cache.** Every viewer class is in the cache key, and every
  moderation record changes the key.
- **The frozen log format.** New record types are claimed by docs/03's
  procedure. New optional fields are allowed. The index flag bits are
  assigned in docs/03.
- **The client bundle budget** is 60000 B gzip. The built client is committed,
  so run `go generate ./internal/web/spa`.

## Running two lanes, changed

There are two lanes, server and client. Only one lane at a time may have an
open PR that changes the committed client dist, because two such PRs always
conflict. The orchestrator writes the tracking notes in a batch.

## Never, added

- Answer a security finding by argument alone.
- Promote a ticket outside `phase-2`, or one with an open decision.
