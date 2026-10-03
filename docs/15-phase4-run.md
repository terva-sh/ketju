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
- **Placed in Phase 4 by the run, 2026-10-02,** because each fails CI on
  changes that do not touch it, and the exit check needs reliable green CI.
  docs/14's run placed `TKT-01M3TSZHSW` the same way. The maintainer may move
  either back out.
  - `TKT-01M3ZE2211`, Stop the Phase 3 exit Unread surface test failing under
    CI load. It has failed four runs, main's push run included.
  - `TKT-01M3Z50NKE`, Stop TestRunRetriesAFailedCatchUp cancelling before the
    catch-up ends.

Out of scope, and staying out:
- `TKT-01M3VDYENN`, the runner record. It is outside the repository.
- `TKT-01M3MSFX3S`, Let a restored member reclaim posts under an id the
  database lacks.
- The Phase 1 real-device check, `TKT-01M38WTKSQ`.
- Phase 5 and the Later list.

## Decided by the maintainer

**The run's decisions, 2026-10-03.** Reviewing the v1.0 checklist's part 2,
the maintainer approved every run decision still marked pending in docs/02
to docs/08: the 21 this run made and three left from Phase 3. Each now reads
"maintainer approved 2026-10-03". They also approved the internal choices
recorded only in ticket plans. One stands for v1.0 only: the avatar hash
signed-out readers still receive, which TKT-01M41MKE81 removes. The
checklist's part 2 records the rest of what they settled.

**Metrics and the release, 2026-10-02.**
- `/metrics` is served only on a separate `metrics_listen` address, and is
  off by default (docs/02).
- v1.0 is published from a tag-triggered `.forgejo/workflows/release.yml`.
  That is a grant beyond docs/12 "Changing CI", for that file only. The
  workflow publishes to a Forgejo release and a GitHub release on the public
  mirror, plus a `scratch`-based image on `container.local.sothr.com`. v1.0
  is not signed.
- The credentials and the tag stay with the maintainer
  (`TKT-01M3YSVEDJ`'s notes).


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
- **Moving posts follows the thread-move rule, and nothing else changes who
  can read.** docs/08 already decides that moving a thread needs `moderate`
  in both forums, and that the moved thread is then read under the target
  forum's rules (decided 2026-09-30, TKT-01M3RQP0TG). Split and merge across
  forums follow the same rule. The run does not invent a stricter or looser
  one. Within it:
  - A split or merge whose source and target forums differ is refused unless
    the actor holds `moderate` in both, by role or through a group.
  - A moved post's readership changes only as the target forum's rules
    change it. No other path, such as a pointer, a notice, a notification, a
    search hit or a live event, lets a viewer read a moved post they could
    not read in its new place.
  - The source's "N posts were moved to <link>" notice reveals the target
    thread's title and link only to a viewer who may read the target.
    Everyone else sees that posts were moved, and nothing more.

  Any finding against this is a security finding. A rule that went past
  docs/08, such as refusing every move that would widen readership, would
  change what a moderator can do. That is stop class 1 and goes to the
  maintainer.
- **Archived means read-only for everyone**, moderators included. Only an
  admin archives a thread (docs/08), and v1 has no unarchive. **Decided by
  the maintainer (2026-10-02, TKT-01M3YSQQ5X):** keep the design as written.
  The alternatives were a `ThreadUnarchived` record (type 31) that admins
  write, and the same record for moderators. Each would change the frozen
  log format. An unarchive record can come after v1. An earlier draft of
  this line said "until a moderator unarchives the thread"; that
  contradicted docs/04 and docs/08 and was wrong.
- **A backup restores.** A snapshot that `ketju backup` produces is one that
  `ketju fsck` passes after restoring it.

## Never, added

- Promote a ticket outside `phase-4` and the scope above.
- Publish a release, push a tag, or create a credential.
