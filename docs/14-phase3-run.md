# 14 — The Autonomous Phase 3 Run

Status: in force from the merge of this document until every `phase-3`
ticket is done or archived, or until the only work left waits on a question
the maintainer must answer. Like [13-phase2-run.md](13-phase2-run.md), it
covers one run, not a standing policy.

This document takes Phase 3 (docs/09: lenses, unread surfaces, search, live
updates, attachments, phone polish) from an ungroomed backlog to its exit
criterion, "daily-driver quality for an active community, on a phone as much
as on a desktop". It is written as changes to docs/13, which is written as
changes to docs/12. **Every section of docs/12 and docs/13 applies unless a
section below replaces it.** Where they say `phase-2`, read `phase-3`.

The tracking ticket is `TKT-01M3V3BVPY` (Drive Phase 3 to done
autonomously). It quotes the grant.

## The grant

On 2026-10-01 the maintainer wrote: "Let's plan, file tickets, groom and
promote, them and continue autonomously through phase 3 or until we hit an
issue I must absolutely resolve".

Under this grant the run may:
- file tickets for Phase 3 work the backlog lacks;
- groom every `phase-3` ticket and merge its grooming PRs on the docs/12 gate;
- promote a groomed `phase-3` ticket once its plan has merged;
- merge its own implementation PRs on the gate, as docs/13 describes. It
  records a non-security disagreement and merges, always fixes a security
  finding, and caps review at five rounds.

## Scope

- **Every ticket with the `phase-3` milestone.** On 2026-10-01 that was 21
  drafts under the epics `TKT-01M29BN36D` (Lenses, unread surfaces, search,
  and live updates), `TKT-01M29BKBM7` (Markdown render pipeline and
  attachments) and `TKT-01M29BN367` (Preact client).
- **Pulled forward from Phase 4**, because they back screens the client
  already routes to and shows as placeholders:
  - `TKT-01M29CCC0X`, Implement the settings endpoints and session management;
  - `TKT-01M29CXK4E`, Build the user settings screens.
- **Filed for this run:**
  - `TKT-01M3V3CFMA`, Build the member profile screen and their post list;
    the profile endpoints are done, but no ticket built the screen;
  - `TKT-01M3V3CFRE`, the Phase 3 exit check, named the way Phase 2's
    was (`TKT-01M3NTSCMN`).
- **Placed in Phase 3 by the run**, because each bears on the exit criterion's
  "on a phone as much as on a desktop" or on CI health:
  - the eight open visual-divergence drafts under `TKT-01M29BN367` that the
    screenshot work filed (`TKT-01M3S3Z9E1`, `TKT-01M3S3Z9GT`,
    `TKT-01M3S3ZF6J`, `TKT-01M3S3ZRYD`, `TKT-01M3S3ZS0H`, `TKT-01M3S3ZZQD`,
    `TKT-01M3S3ZZSZ`, `TKT-01M3S3ZZYA`);
  - `TKT-01M3TV4HBF`, Move focus and key listeners into layout effects in
    menus and popovers;
  - `TKT-01M3TSZHSW`, Stop the slow-mode e2e test failing on a phone under
    CI load.

  The maintainer had been asked to place these and had not. Placing them here
  is the run's call, and the maintainer may move any of them back out.

Work the run finds along the way is filed as a draft with **no milestone**
unless it blocks a Phase 3 exit criterion. A run that keeps growing its own
scope never ends.

## The stop rule, changed

docs/12 "Gaps the design leaves open" blocks a ticket on any question that
changes what a member sees or can do. Under this grant, the run settles those
questions itself, as long as a default follows from the design documents, the
mockups or an earlier maintainer decision. The run:
- records the decision in the nearest design document, in the same PR, under
  a dated "Decided by the run" line naming the alternatives and why each
  lost;
- adds a ticket note marked `run decision: pending maintainer review`;
- lists every such decision in the tracking ticket's summary.

The maintainer reviews them afterwards rather than before.

**A question the maintainer must absolutely resolve** stops the work it
touches. These are:
1. **Security posture beyond the design.** For example, who may see what when
   docs/05, 07 and 08 give no rule, or a change to how sessions,
   credentials or roles work.
2. **A format that outlives the release.** That covers a new record type or
   field in the frozen thread-log format beyond docs/03's procedure, and a
   change to data that `ketju reindex` cannot rebuild and a rollback cannot
   undo. Projections and indexes that reindex rebuilds are not in this class.
3. **Anything outside the repository.** That covers an external service,
   account, credential, DNS name, or a change to the dogfood deployment's
   configuration.
4. **Dropping or materially changing a docs/09 Phase 3 deliverable.**
5. **Raising the reader bundle budget** (docs/07). The maintainer raised it
   from 70000 to 120000 B gzip on 2026-10-01, during the run. The run
   lazy-loads where a chunk genuinely saves bytes. If the reader would still
   exceed the budget after that, it stops.
6. **A contradiction between design documents** that has no safer reading.

When one of these comes up:
- the run blocks the affected ticket with the question as its reason;
- it records the question on the tracking ticket;
- it sends the maintainer a notification, since they may be away;
- it keeps working on everything the question does not touch.

The run halts only when nothing else can proceed.

## Order

Grooming runs first, in parallel lanes by area. Every plan merges before
anything in its area is promoted. The orchestrator then promotes in batches.

Implementation follows dependency order:
1. The server roots: the lens framework (`TKT-01M29CRJ1P`), watches and
   mutes (`TKT-01M29CRJ29`), the notification generator (`TKT-01M29CRJ2T`),
   the search index (`TKT-01M29CRJ3B`), the SSE endpoint (`TKT-01M29CRJ3X`),
   the blob store (`TKT-01M29CXK0K`) and the settings endpoints
   (`TKT-01M29CCC0X`).
2. The screens and lenses that need them.
3. The phone sheets, the polish drafts and the exit check.

Server tickets run in parallel. Client tickets queue for the committed dist,
one open PR at a time (docs/13 "Running two lanes, changed").

## Invariants, added

Every docs/13 invariant holds. These are added:
- **Search and notifications never widen access.** A result, a snippet, a
  notification or a live event reaches a viewer only if that viewer may read
  the post at the moment it is served. That means the same `perm` evaluation
  the thread page uses, with removed, redacted and hidden posts included.
  Any finding against this is a security finding.
- **Live updates carry no more than the thread page would.**
- **Uploads are content-addressed and size-limited** (`limits.attachment_mb`).
  A blob is served only to a viewer who can read a post that references it.

## Never, added

- Promote a ticket outside `phase-3` and the scope above.
- Raise the bundle budget, change the frozen log format, or touch anything
  outside the repository without the maintainer.

## Amended during the run

The run changed its own procedure on 2026-10-01 as it learned. Each change is
recorded here so a later reader does not have to reconstruct it from lane
briefs.

- **Review cap for grooming.** A grooming PR past five review rounds carries
  its remaining non-security findings as dated ticket notes headed
  "Requirement carried from review <id>". They bind the implementer as much as
  the plan does. The PR then merges on a carry review of the ticket-only
  commit. Security findings are always fixed. A sixth round was granted once
  (#271), when round five's findings were narrow.
- **Carries.** A ticket-only commit after a clean review needs a carry review
  (`carry=true`) on the new head. A carry never crosses to a new PR: a PR
  opened to replace a conflicted one needs a full review of its own.
- **Lanes promote their own ticket** (draft, ready, claim, in-progress) once
  its plan has merged, instead of waiting for the orchestrator.
- **Lanes link PRs on their own tickets only.** Linking every PR on the
  tracking ticket made every grooming PR conflict with every other; the
  orchestrator logs PRs on the tracking ticket instead.
- **Client lanes may prepare while another holds the dist.** A lane
  implements and tests without committing the dist or opening a PR, and
  rebases its unpushed branch and regenerates the dist when handed it. One
  open PR still changes the dist at a time.
- **Main's push CI.** Each merge cancels the previous push run, and Forgejo
  reports a cancelled run as `failure` with no steps. Read the run's status
  before treating a push failure as a broken build.
- **The runner freeze does not hold the exit check.** On 2026-10-02 the
  maintainer accepted the evidence that the nine-second thread-page stall in
  CI run 1790 was runner b3cc8959 freezing its jobs, not ketju: "Let's accept
  the evidence as it stands and file this away. We'll keep it on record for if
  it happens again and we need to go fishing for some sort of race or deadlock
  in ketju itself." TKT-01M3VBWMA8 is done with its runner criterion unticked,
  and TKT-01M3VDYENN stays open as a draft with no milestone, to record the
  runner problem.
