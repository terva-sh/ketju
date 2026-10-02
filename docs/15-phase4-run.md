# 15 — The Autonomous Phase 4 Run

Status: in force from the merge of this document until every `phase-4`
ticket is done or archived, or until the only work left waits on a question
the maintainer must answer. Like [14-phase3-run.md](14-phase3-run.md), it
covers one run, not a standing policy.

This document takes Phase 4 (docs/09: split and merge, reply as a new thread,
archive, thread tags, metrics, backup, the release) from an ungroomed backlog
to the point where the maintainer can tag v1.0. It is written as changes to
docs/14. **Every section of docs/12, docs/13 and docs/14 applies, including
docs/14 "Amended during the run", unless a section below replaces it.**
Where they say `phase-3`, read `phase-4`.

The tracking ticket is `TKT-01M3YMG0M1` (Drive Phase 4 to done
autonomously). It quotes the grant.

## The grant

On 2026-10-02 the maintainer wrote: "Let's land our merges, rebase and then
start burning this down". Asked what to include, they chose all four options
offered:
- Phase 1 cleanup;
- the small unplaced tickets;
- a Phase 4 run;
- the profile and account drafts.

The same day they answered the thread-tag questions; see "Decided by the
maintainer" below.

The run may do everything docs/14 "The grant" allows, for `phase-4`.

## Scope

- **Every ticket with the `phase-4` milestone.** On 2026-10-02 that was 11
  drafts:
  - split and merge records, and split, merge and reply as a new thread;
  - archival;
  - thread tags, their rules and the tag-rules decision;
  - metrics, backup and the release pipeline.
- **Placed in Phase 4 by the run, as the maintainer chose on 2026-10-02:**
  - the small unplaced tickets:
    - `TKT-01M3V9DWTN`, Let a member turn off the single-key shortcuts
      (WCAG 2.1.4). The front page's "Press ? for keyboard shortcuts" hint
      (`TKT-01M3S3ZRYD` criterion 2) waits on it.
    - `TKT-01M3V7GE59`, Add the / shortcut that opens search on desktop.
    - `TKT-01M3V7CFJ5`, Move focus into layout effects in the admin and
      apply screens.
    - `TKT-01M3VVZE4G`, Compare ordinals as unsigned in index and page
      bounds on 32-bit builds.
    - `TKT-01M3VRFRFW`, Order a new post's index entry before the header
      that covers it.
  - the profile and account drafts:
    - `TKT-01M3V6XTYG`, Add handle change and self-deactivation to the
      settings screen.
    - `TKT-01M3V6XVVG`, Let a member edit their display name, bio and
      avatar.
    - `TKT-01M3V73KM1`, Bound how much one member may upload.
- **Filed by the Phase 1 cleanup of 2026-10-02.** Each criterion that
  check found unmet becomes a ticket:
  - `TKT-01M3YME9AZ`, Record quotes as backlinks, as docs/06 promises.
  - `TKT-01M3YME9DM`, Keep API handlers off the stores, and test the
    layering.
  - `TKT-01M3YME9G0`, Golden-test what every API endpoint actually serves.
  - `TKT-01M3YME9KG`, Answer method-not-allowed for a wrong verb on an API
    route.
- **Filed for this run:** `TKT-01M3YMG0NX`, Check Phase 4's exit criterion
  and prepare the v1.0 release.

Out of scope, and staying out:
- `TKT-01M3VDYENN`, the runner record. It is outside the repository.
- `TKT-01M3MSFX3S`, Let a restored member reclaim posts under an id the
  database lacks.
- The Phase 1 real-device check, `TKT-01M38WTKSQ`.
- Phase 5 and the Later list.

## Decided by the maintainer

**Thread tags, 2026-10-02** (`TKT-01M3M4KVNW`). The tags grooming lane
writes these into docs/04, docs/07 and docs/08:
- A tag is lowercase ASCII words joined by single hyphens,
  `[a-z0-9]+(-[a-z0-9]+)*`. Uppercase input is folded to lowercase.
- A tag is 1 to 24 characters, and a thread carries at most 8.
- Tags are free text. A moderator corrects a thread with a retag (docs/08).
  A curated list per forum may come later.
- Anyone who may create a thread in the forum may tag it, except with a
  reserved tag such as `agent`. Only a moderator or an agent account may set
  a reserved tag. The reserved list lives in docs/08.

The run proposes two smaller points as run decisions:
- a repeated tag keeps one copy;
- members see tags sorted.

## The stop rule, changed

docs/14's six classes stand, with these additions for Phase 4:
- **Class 2, a format that outlives the release, covers split and merge.**
  `PostsSplitOut` and `PostsMergedIn` have type numbers reserved since the
  freeze. If docs/03 does not already fix every field of their bodies, the
  ticket that would define them stops. So does any change to the `Post`
  record, such as adding quotes to it. A projection that reindex rebuilds is
  not in this class.
- **Class 3, anything outside the repository, covers the release.** The
  run may build the release pipeline and run it dry. These stay with the
  maintainer:
  - publishing a binary or a container image;
  - creating a signing key or a registry credential;
  - pushing a tag, v1.0 included.
- **Class 4 reads "a docs/09 Phase 4 deliverable".**
- **Class 5:** the reader bundle budget is 120000 B gzip (docs/07).

## Order

Grooming runs first, in parallel lanes by area. Each lane's plans merge
before anything in its area is promoted:
1. **Moving posts:** split and merge records, split, merge, and reply as a
   new thread. This lane settles stop class 2 before anything else in it.
2. **Tags and archive:**
   - the tag-rules spike's docs;
   - the tag rules on create;
   - thread tags;
   - archival.
3. **Operations:** backup, metrics and the release pipeline.
4. **Client and account:**
   - the shortcut off switch, then the / shortcut and the footer hint;
   - focus in the admin and apply screens;
   - handle change and self-deactivation;
   - profile editing;
   - the upload bound.
5. **Correctness:**
   - 32-bit ordinals;
   - index entry order;
   - quote backlinks;
   - API layering;
   - served-response goldens;
   - method-not-allowed.

The exit check is groomed last, against the merged plans.

Implementation follows dependency order. Server tickets run in parallel.
Client tickets queue for the committed dist, one open PR at a time, as in
docs/14.

## Invariants, added

Every invariant of docs/13 and docs/14 holds. These are added:
- **Moving posts never widens access.** A post split out of or merged into a
  thread is readable by exactly the viewers who could read it before, unless
  the target forum's rules say otherwise. In that case the target's rules
  apply and the move is refused if it would expose a post the target cannot
  show. A source pointer reveals nothing a viewer could not already read.
  Any finding against this is a security finding.
- **Archived means read-only for everyone**, moderators included, until a
  moderator unarchives the thread.
- **A backup restores.** A snapshot that `ketju backup` produces is one that
  `ketju fsck` passes after restoring it.

## Never, added

- Promote a ticket outside `phase-4` and the scope above.
- Publish a release, push a tag, or create a credential.
