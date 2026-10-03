---
schema: 3
id: TKT-01M3YMG0NXX1FXRARA7X4EK0HD
title: Check Phase 4's exit criterion and prepare the v1.0 release
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:test
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies:
  - TKT-01M29CEE7SVHNRGVDH53QAFPN0
  - TKT-01M29CRJ64G7PJBPK8Z52SE8NW
  - TKT-01M29CXJZZZN7X4EZX37JJBF91
  - TKT-01M29CXK35WS83Z6CAZBAVDFRD
  - TKT-01M29CXK3SMVHGVBGF4N57QACG
  - TKT-01M29CYSAMEVQ7MJG41HR7HPRH
  - TKT-01M29CYSBAVXNFWDZVAK5PD71S
  - TKT-01M29CYSC0Q4WHMQNVTKB6PN4B
  - TKT-01M29CYSCPQJ5ZZHCFDGV1S4TF
  - TKT-01M3B12ZZ403NEPFH3DASEW1D8
  - TKT-01M3M4KVNW0VTZFWHRZK8V28RZ
  - TKT-01M3V6XTYGE9R6MRRH27HYSMHM
  - TKT-01M3V6XVVGSTDA1SH15DKYP7NZ
  - TKT-01M3V73KM1VJ5XTC53MFCEJ0XW
  - TKT-01M3V7CFJ5RYC1RCZRNYD8RSQS
  - TKT-01M3V7GE5986PGNHTJJBZT1Z0H
  - TKT-01M3V9DWTNNAMF2WA9CWY0AJGQ
  - TKT-01M3VRFRFW9M4XY8D14FXAJA23
  - TKT-01M3VVZE4G37YMQG2RVXSWYM3Z
  - TKT-01M3YME9AZ4RJACSMV8VX4JMZJ
  - TKT-01M3YME9DMH3PMJGD8A4A7ZMNN
  - TKT-01M3YME9G0PM84B9NR4EQW4C2P
  - TKT-01M3YME9KGJ1XW7RHCRFZ621A0
  - TKT-01M3YSQPZXP67VF9K0HJ94BKN8
  - TKT-01M3YSQQ33K01M960MGMSZ90P2
  - TKT-01M3YSQ4CRYBDMEM1MY51D736K
  - TKT-01M3YTHAZB21Y6T36WC4FXZVR8
  - TKT-01M3YSQQ5XDCKEJ60MWJYQKWCW
  - TKT-01M3ZE2211GDC23K09PZ1GJ8BQ
  - TKT-01M3Z50NKE109GGWWC2WJA6S61
  - TKT-01M40PCE7PDJB4X6Z4N90BXY9N
blocks_on: none
references:
  - ref: pr:375
    path: null
claim: null
archive: null
created_at: 2026-10-02T15:42:30Z
updated_at: 2026-10-03T11:19:00Z
created_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-mg0nx
  name: ""
extensions: {}
---

## Description

docs/09 gives Phase 4's exit criterion as "v1.0 tag". Making the tag is the maintainer's act, because it is outside the repository (docs/15 stop class 3). This ticket proves that the work behind it is done, the way TKT-01M3V3CFRE did for Phase 3. It then prepares everything the maintainer needs to cut the release.

Grooming names the tests. At least:
- a moderator splits posts into a new thread and merges posts into an existing one, and both threads show bidirectional notices;
- a member replies as a new thread;
- a member creates a thread with tags under the decided rules, a moderator retags it, and a list filters by tag;
- a moderator archives a thread, and nobody can write to it afterwards;
- `ketju backup` produces a snapshot that restores and passes `ketju fsck`;
- `/metrics` serves only behind its guard;
- the release pipeline builds the binaries in a dry run without publishing anything.

Each browser test runs at 390 and 1440 px with one axe scan. The server invariants are proven through the real serve, as `TestPhase3Exit*` are. They cover docs/15 "Invariants, added":
- a cross-forum split or merge is refused unless the actor holds `moderate` in both forums;
- a moved post is served under the target forum's rules and by no other path: not a pointer, a notice, a notification, a search hit or a live event;
- the source's moved notice names the target only to viewers who may read it;
- an archived thread refuses every write, moderators' included.

## Acceptance criteria

- [ ] e2e/phase4-exit.spec.ts holds the plan's seven named tests (split, merge, the reveal rule, reply as a new thread, tags, archive, quote backlinks), each passing in the phone (390 px) and desktop (1440 px) projects with one axe scan per test, and no spec added here calls /auth/* (e2e/session.ts unchanged)
- [ ] cmd/ketju/phase4_exit_test.go holds the plan's eight TestPhase4Exit tests, a Build and Test step 'Phase 4 exit tests, by name' runs go test -race ./cmd/ketju/ -run '^TestPhase4Exit' -count=1 -v, and the PR quotes from the merged head's step logs every TestPhase4Exit line, every phase4-exit.spec.ts line in both projects, and the npm run budget line within 120000 B gzip
- [ ] Through the real serve, TestPhase4ExitMovesNeedModerateInBoth, TestPhase4ExitMovedPostsFollowTheTargetForum and TestPhase4ExitNoticesNameOnlyWhatTheViewerMayRead prove docs/15's move rule: an admin and a moderator by group grant in both forums may move; a moderator of only the source or only the target is refused with both logs byte-identical; a viewer of only the source, only the target, or neither is served exactly what the target forum's rules allow by every path the plan lists
- [ ] TestPhase4ExitArchivedThreadIsReadOnlyToEveryone proves every write from the author, a member, moderators by role and by group, an admin and an agent is refused on an archived thread and its log does not grow, also after a restart; TestPhase4ExitBackupRestoresAndPassesFsck proves a ketju backup taken while serve takes posts, of a data directory holding every Phase 4 record, restores, passes ketju fsck with exit 0, and serves the same pages before and after a reindex
- [ ] A ticket note records TestPhase4Exit*'s time and cmd/ketju's package time under -race locally, and the PR quotes cmd/ketju's time from the merged head's race step; if it is over 900 s the ticket is blocked for the maintainer instead of the step or the package being changed
- [ ] Every other phase-4 ticket except the tracking ticket TKT-01M3YMG0M1 and TKT-01M3YSVEDJ is done or archived before this one closes, and TKT-01M3YSVEDJ's release.yml has merged, its tag-dependent criteria named in the checklist's maintainer steps
- [ ] docs/releases/v1.0-checklist.md, linked from docs/releases/v1.0.md, holds the evidence with its run links; every Phase 4 run decision pending maintainer review, recorded disagreement, carried requirement and unticked criterion, each with its document or ticket and the command that finds them; the advisory move records on rollback; and the maintainer's steps (secrets, a pre-release tag and its verification, then the v1.0 tag)
- [ ] The release dry run step is green on the merged head and the PR quotes its lines naming both binaries and the verified SHA256SUMS; the run pushes no tag, creates no secret and runs no publishing job

## Implementation plan

Groomed 2026-10-02 for the Phase 4 run (docs/15), last, as docs/15 "Order" says, against main at b31d0938, after every other Phase 4 plan had merged (grooming PRs #358, #359, #360, #361, #362 and #371, and the maintainer-decision PRs #363 and #365). Phase 3's exit, TKT-01M3V3CFRE, is the model.

### Settled in grooming

- **The exit criterion is "v1.0 tag"** (docs/09 Phase 4). The tag is outside the repository, so it is the maintainer's (docs/15 "The stop rule, changed", class 3, and "Never, added"). This ticket proves the work behind the tag is done, writes the v1.0 checklist, and stops there. The run never pushes a tag, creates a secret, or runs a publishing job.
- **The maintainer's release decisions** (docs/15 "Decided by the maintainer", TKT-01M3YSVEDJ's note of 2026-10-02):
  - `.forgejo/workflows/release.yml` runs on a pushed `v*` tag;
  - it publishes to a Forgejo release and to a GitHub release on the public mirror, plus a `scratch` image at `container.local.sothr.com`;
  - v1.0 is not signed;
  - the GitHub token, the Forgejo upload token and the registry credential are created and stored by the maintainer.
- **`/metrics`** is served only on the separate `metrics_listen` address, off by default (docs/15, docs/02 "Observability", TKT-01M29CEE7S, done).
- **This ticket does not wait on TKT-01M3YSVEDJ** (Publish release binaries, checksums and an image from a tag). Its first and last criteria need a pushed tag: "A pushed v* tag runs the release workflow", and "A pre-release tag the maintainer pushes publishes artifacts that verify". A dependency would hold this ticket until the maintainer acts, and the maintainer acts on this ticket's checklist, so the two would wait on each other. Instead, the checklist's maintainer steps name those criteria, and criterion 6 below requires only that its `release.yml` has merged. Every other `phase-4` ticket in scope is a dependency, except the tracking ticket, TKT-01M3YMG0M1.
- **The tests are named per deliverable and per invariant**, and the selectors, seeds and routes come from the landed feature tickets, as TKT-01M3V3CFRE took them. These are the docs/15 invariants:
  - **The move rule.** A cross-forum split or merge needs `moderate` in both forums, by role or through a group. A moved post is read under the target forum's rules and by no other path. A notice names the other thread only to a viewer who may read it. Reply as a new thread follows the same reveal rule (docs/06, maintainer decision 2026-10-02). Any finding here is a security finding.
  - **Archived means read-only for everyone**, admins included, and v1 has no unarchive (TKT-01M3YSQQ5X, maintainer decision 2026-10-02).
  - **A backup restores**: `ketju backup` output, restored, passes `ketju fsck`.
  - **The reader bundle stays within 120000 B gzip** (docs/07, docs/15 class 5).
  - The earlier invariants hold too. Search, notifications, lenses and live events never widen access. A blob is served only to a reader of a post that names it. An avatar is the maintainer's explicit exception, served to signed-in members only (docs/05 "Display name and bio", decision of 2026-10-02 on TKT-01M3YSQ4CR).
- **Reply as a new thread from a locked or archived thread** is allowed. It writes only the new thread's log (docs/06 "Moderator flow for drift", decided by the run in this grooming PR, pending maintainer review). The archive test asserts it.
- **Where the Go tests live, and how they keep `cmd/ketju` in bounds.** This is an internal choice, settled under docs/12 "Gaps the design leaves open".
  - The tests go in `cmd/ketju/phase4_exit_test.go`, in package `main`, because they drive the assembled binary: `start()` and `serveOpts`, `backupMain`, `fsckMain` and `reindexMain`. Another package cannot import `main`.
  - `cmd/ketju` under `-race` is already near CI's limit. TKT-01M3YFYYGV's re-run 2379 took 490 s, and the race step has run with `-timeout 20m` since #354. Phase 4 adds the backup, archive-reindex and upgrade tests to the package too. So the exit tests are built to be cheap:
    - eight top-level tests, each with one serve and one world;
    - the people seeded straight into the database, as `seedPeople` does, with at most five sign-ins per serve (the `/auth/*` burst, docs/08);
    - the agent actor through its bearer credential, which spends no `/auth/*`;
    - no fixed sleeps, waiting on the projection as `phase3_exit_test.go` waits;
    - a bounded writer in the backup test (40 replies), not one that runs until stopped;
    - browser flows only in the spec, never repeated in Go.
  - Phase 2's and 3's nine exit tests took 18 s together under `-race` on the 12-core grooming host, at b31d0938. These eight have the same shape.
  - They run in the full race step and again, verbose, in their own step, as Phases 2 and 3 do. Keeping them out of the full step would mean editing that step or hiding them behind an environment variable. The first needs the maintainer, as #354 did. With the second, a step that forgot the variable would go green with nothing run.
  - **The bound.** If the race step's log on this PR shows `cmd/ketju` above 900 s, three quarters of the 20-minute limit, this ticket is blocked and the maintainer is asked, rather than splitting the package or raising the limit. TKT-01M3YFYYGV's note gives the reasons against both.
- **The checklist lives in the repository**, at `docs/releases/v1.0-checklist.md`, beside the release notes TKT-01M29CXK3S drafts at `docs/releases/v1.0.md`. Each links the other. A ticket summary would not travel with the tag, and the maintainer reads the checklist when cutting the release.
- **Conventions:**
  - stored sessions (`e2e/session.ts` `stateFile`) for alice, carol, mona and ada;
  - one axe scan per test, WCAG 2.1 A and AA, as `phase3-exit.spec.ts` `scan` does;
  - no `/auth/*` call, so `e2e/session.ts` is unchanged and no `paceAuth` is needed;
  - a failing test prints its error context (#261);
  - both projects, `phone` at 390 px and `desktop` at 1440 px;
  - writes go only into threads each test starts, so no other spec's seed changes.
- **A green summary is not evidence** (AGENTS.md). The PR quotes the step-log lines on the merged head.

### Approach

1. **Seed** (`cmd/ketju/e2e.go`). Add only what a test below needs and no feature ticket seeded. The cross-forum browser test moves posts from a `site-feedback` thread into `moderators`, which alice cannot read and mona can. At implementation, grep the specs for anything reading `moderators`' index row or list as mona or ada. If one does, add a restricted forum of this spec's own after the existing forums, so the ids stay as they were.
2. **`web/client/e2e/phase4-exit.spec.ts`.** Every test runs in both projects, with one axe scan each. alice is the member, carol the second member, mona the moderator and ada the admin, all from stored sessions. Each test starts its own thread in `site-feedback` through the API, titled with the project and the time, as `phase3-exit.spec.ts` `start` does.
   1. **Split.** "a moderator selects posts and splits them into a new thread, and both threads show their notices". mona: Select posts, two checkboxes, Split…, a title. The source shows "2 posts were moved to <title>" at the first moved post, and the moved posts' tombstones link into the new thread. The new thread shows "Split from <title>", and the posts keep their authors and times.
   2. **Merge.** "a moderator merges posts into an existing thread, and both threads show their notices". mona merges two posts into a second thread of the test's own, by its link. The target shows "2 posts were merged from <title>" before the copies, at its end. The source shows its moved notice.
   3. **The reveal rule in the browser.** "a member who cannot read the target forum sees that posts were moved and nothing more". mona splits posts of a `site-feedback` thread into `moderators`. alice sees "2 posts were moved to another thread" and "Post moved to another thread", with no link and no title. The page holds no text from the moved posts.
   4. **Reply as a new thread.** "a member replies to a post as a new thread, edits the pre-filled quote, and the source post shows the continued-in note". alice uses "Reply as new thread" on carol's post, edits the blockquote and posts. The new thread shows "continued from <link>", and carol's post shows "continued in <link>".
   5. **Tags.** "a member starts a thread with tags, a moderator edits them, and a tag chip filters the forum's list". alice starts a thread with `Garden, long-read` and sees the chips `garden` and `long-read`. mona uses Edit tags to set `garden` alone. A chip opens `/f/site-feedback?tag=garden`, which reads "Tagged garden", holds the thread, and links back to the whole list.
   6. **Archive.** "an admin archives a thread with a reason, and afterwards nobody is offered a composer or a control, and its row carries the Archived flag". ada archives the test's thread from the moderation menu. The confirmation says it cannot be undone. Afterwards:
      - ada, mona and alice each see "This thread is archived.";
      - none of them is offered a composer, the sticky Reply, an edit, delete, restore or flag, or a moderation entry;
      - watch and Mark unread are still offered;
      - the thread's row in `site-feedback` carries "Archived".

      The axe scan runs on mona's archived page.
   7. **Quote backlinks.** "a post shows who quoted it, and a backlink opens the quoting post". carol quotes alice's post 1 in post 3. Post 1 shows "Quoted by #3", and the link brings post 3 into view. At 390 px it opens it in the popover when it is off the page, as TKT-01M3YTHAZB's spec does.
3. **`cmd/ketju/phase4_exit_test.go`.** A world builder, `newP4World`, is shared by the moving-posts tests:
   - forums: `general` (public), and `vault` and `annex` (restricted);
   - `ada`, an admin;
   - `kim`, who holds `moderate` and `read` in vault and annex through groups. kim is the moderator by group grant in both forums;
   - `gail`, who moderates and reads vault alone, through a group;
   - `otto`, who moderates annex and reads vault and annex, through groups. otto is a moderator of the target alone who can still read the source;
   - `pat`, who reads annex alone, through a group. pat is the viewer of the target alone;
   - anonymous, who reads neither restricted forum.

   Each expected answer comes from the viewer's effective permission, read from what the target forum's thread page serves that viewer, and never from a blanket "nothing". The tests:
   - `TestPhase4ExitMovesNeedModerateInBoth`. It runs a split vault→annex and a merge annex→vault through the routes.
     - Allowed: ada (admin by role) and kim (group grant in both).
     - Refused: gail (source forum only), `forbidden`; otto (target forum only), refused; pat, at the gate.
     - Allowed: a same-forum split by gail through her grant.

     Every refusal leaves both logs byte-identical and creates no thread file.
   - `TestPhase4ExitMovedPostsFollowTheTargetForum`. kim splits two vault posts into annex. One mentions gail, so she was notified before the move, and carries an attachment. The other is flagged, and is quoted by a post that stays in vault. For gail (reads the source only) and anonymous, no path serves a moved post's body or blob:
     - the vault page and post;
     - the post's replies and history;
     - the moderator lens, and the queue's flag entry, which gail moderates;
     - a search for a word only the moved post holds;
     - her notifications;
     - `/users/{h}/posts`;
     - the blob;
     - an SSE subscription to vault and to annex;
     - the vault post's quote, drawn as a tombstone;
     - `quoted_by`, which a moved quoting post never fills.

     pat (reads the target only) reads the copies in annex, finds them by search, and is served the blob. The test asserts exactly what annex's thread page serves pat, attributed to the original authors and times.
   - `TestPhase4ExitNoticesNameOnlyWhatTheViewerMayRead`. For a split vault→annex, a merge annex→vault, and a reply as a new thread from a vault post into annex, by a member of both:
     - kim and ada see every link;
     - gail sees the moved notice and `moved_to` with a null thread and no title. She sees no "continued in" note;
     - pat sees "split from" and "merged from" with a null thread, and no "continued from" header;
     - the bodies differ between gail and kim, who share a viewer class, and so do their ETags. A second request is served from the response cache with the same per-viewer answer, so nothing per viewer is cached.
   - `TestPhase4ExitArchivedThreadIsReadOnlyToEveryone`. ada archives a `general` thread through `POST /api/v1/admin/threads/{id}/archive`. Table-driven over the actors {the author, a member, a moderator by role, a moderator by group grant, ada, an agent by credential} and every write each could make on a live thread:
     - reply;
     - edit, delete and restore, their own and another's;
     - hide;
     - flag and queue resolve;
     - lock, pin, move, rename and slow mode;
     - retag;
     - split out of it, and merge into and out of it;
     - archive again.

     Each is `conflict`, or `forbidden` where it already was, and the log length is unchanged after each. A reply as a new thread from one of its posts succeeds, and the archived log is unchanged. The thread stays readable, watchable and in its list. After a restart on the same data directory, a reply is still `conflict`.
   - `TestPhase4ExitTagsFollowTheirRules`. A member creates a thread with `["Garden","long-read","garden"]` and is served `["garden","long-read"]`. A member's `agent` is refused with `reserved_tag`. The agent's `agent` is accepted. A group-granted moderator retags. The author, and a moderator of another forum, get `forbidden`. An archived thread is `conflict` to a moderator. `?tag=` answers a forum the viewer cannot read as the unfiltered list does, and lists no global pin from it.
   - `TestPhase4ExitAvatarsAreServedOnlyToMembers`. A member sets an avatar through the route TKT-01M3YSQ4CR lands. A signed-in member fetches it. A signed-out request and a pre-user are refused, as docs/05 records. An attachment in a restricted forum is still refused to a member who cannot read it.
   - `TestPhase4ExitBackupRestoresAndPassesFsck`. The world holds every Phase 4 write:
     - a finished cross-forum split and a merge;
     - a reply as a new thread;
     - tags and a retag;
     - an archived thread;
     - a quoting post;
     - an avatar.

     While a writer adds 40 replies to one `general` thread, `backupMain` writes DEST. Then:
     1. DEST is copied into an empty directory with the source's `config.toml` and `secrets/`, as the README's restore says.
     2. `fsckMain` returns nil (exit 0): no damage, no drift, no quarantine.
     3. A serve on the copy gives each of kim, gail and pat the same thread pages, notices, tags, archived state, `quoted_by` and avatar answer as the source gave them, for every thread except the one being written.
     4. That thread dumps as a prefix of the source's.
     5. `reindexMain` with `--projections --search` on the copy leaves the same pages.
   - `TestPhase4ExitMetricsServeOnlyOnTheirOwnListener`. The serve has `metrics_listen` on loopback. After the world's requests (split, merge, archive, retag):
     - the main listener answers `/metrics` with 404;
     - the metrics listener serves `/metrics` with the docs/02 families, and 404 for `/api/v1/forums`;
     - no label carries a thread id, a handle, a forum slug or a path, so the new routes appear as their patterns.

     A serve with `metrics_listen` unset opens no second listener.
4. **CI** (`.forgejo/workflows/ci.yml`). Add a step to Build and Test, "Phase 4 exit tests, by name", after the Phase 3 step: `go test -race ./cmd/ketju/ -run '^TestPhase4Exit' -count=1 -v`. docs/12 "Changing CI" allows an added step. The Playwright step's list reporter already names each spec test, and TKT-01M29CXK3S's "release dry run" step is the pipeline's own test.
5. **The deliverable map, in the PR.** Each docs/09 Phase 4 deliverable, with the test that proves it and the step-log line quoted from the merged head:
   - multi-select, split, merge and bidirectional notices: spec tests 1 to 3, `TestPhase4ExitMovesNeedModerateInBoth`, `TestPhase4ExitMovedPostsFollowTheTargetForum` and `TestPhase4ExitNoticesNameOnlyWhatTheViewerMayRead`;
   - reply as a new thread: spec test 4;
   - archive: spec test 6 and `TestPhase4ExitArchivedThreadIsReadOnlyToEveryone`;
   - thread tags: spec test 5 and `TestPhase4ExitTagsFollowTheirRules`;
   - slow mode, built in Phase 2: `thread-moderation.spec.ts` "a moderator sets slow mode and turns it off", and `TestSlowModeSurvivesRestartAndReindex`;
   - user settings (sessions list, identities, timezone, page size, theme), built in Phase 3: the three `settings.spec.ts` tests, and `phase3-exit.spec.ts` "a member changes their page size in settings and the thread pages by it";
   - the metrics endpoint: `TestPhase4ExitMetricsServeOnlyOnTheirOwnListener`;
   - backup and its documentation: `TestPhase4ExitBackupRestoresAndPassesFsck` and `TestREADMEBackupProcedure`;
   - `ketju config check`: `TestConfigCheckAsTheBinary`;
   - the release: the "release dry run" step's lines naming both binaries and the verified `SHA256SUMS`, and `TestREADMEUpgradeProcedure`.

   The scope docs/15 added (quote backlinks, the account and profile tickets, the shortcut switch, the upload bound, and the correctness tickets) is proven by each ticket's own criteria. Criterion 6 holds them to done. Spec test 7 covers the backlinks, because they were a docs/09 Phase 3 deliverable that slipped.
6. **`docs/releases/v1.0-checklist.md`**, linked from `docs/releases/v1.0.md`, in four parts, written at implementation from main as it then stands:
   1. **Evidence.** The merged head and its CI run link, with:
      - the quoted lines from step 5;
      - the budget line;
      - the dry run's lines, and `ketju version` from the host binary it built;
      - `cmd/ketju`'s race-step time;
      - `git ticket list --all --json` filtered to `phase-4`, showing every ticket done or archived except TKT-01M3YMG0M1 and TKT-01M3YSVEDJ.
   2. **Open items the maintainer should see before tagging.** Each is listed with its document or ticket, and with the command that finds it, so the list can be rerun after later merges:
      - **Phase 4 run decisions pending maintainer review**: `git diff ed82f15e..origin/main -- docs/ | grep -n 'pending maintainer review'` for the design lines, and `git log ed82f15e..origin/main -p -- .tickets/ | grep -n 'run decision: pending maintainer review'` for the ticket notes. The ops lane flagged one for a closer look: the backup control socket (docs/02 "Backups"), a new local channel the run read as within the design.
      - **Recorded disagreements**: `git log ed82f15e..origin/main -p -- .tickets/ | grep -n 'disagreement: pending maintainer'`, with the PR comment beside each. Also every "Requirement carried from review" note, with whether it was met.
      - **Criteria merged unticked**, ticket by ticket, with the note that says why.
      - **The advisory move records on rollback.** docs/03 "Reserved record type numbers" puts `PostsSplitOut` (20) and `PostsMergedIn` (21) in the advisory class, because skipping one leaves "a stale render, not a disclosure". That holds for every released build, since v1.0 is the first to write or read them. A build from before Phase 4 that read a v1.0 log would skip them. It would then show a cross-forum split's posts in the source thread to readers of the source forum, which docs/15's move rule forbids. A migrated database refuses an older binary (`TestNewerSchemaRefused`), so the exposure is narrow: only an older binary's `ketju reindex` over logs v1.0 wrote. The checklist says so, and says: do not downgrade past v1.0 with logs that hold a move. The record classes are frozen, so this is a known limit, not something to change.
      - **Out of scope and still open**, shipping without them: the Phase 1 real-device check (TKT-01M38WTKSQ), the runner record (TKT-01M3VDYENN), and reclaiming posts under a missing id (TKT-01M3MSFX3S).
   3. **What the maintainer does**, in order. Each step is stop class 3, and the run does none of them:
      1. Settle part 2: approve, revert, or file.
      2. Create the repository secrets that `release.yml` names: the Forgejo upload token, the GitHub token for the mirror, and the `container.local.sothr.com` registry credential. The names are copied from the merged workflow.
      3. Push a pre-release tag on main's head, for example `v1.0.0-rc.1`. Check that the release run's log shows `tools/release`'s assertions, the two uploads and the image push. On a clean machine, download the artifacts and run `sha256sum -c SHA256SUMS`. Run the image with an empty volume and a minimal config, and check that `/healthz` answers and the uid is 65532. This is TKT-01M3YSVEDJ's first, second and last criteria. The run then records the result there and closes it.
      4. Push the v1.0 tag on the commit the pre-release was built from, or on a newer main after a fresh pre-release. The tag's name is the maintainer's: `v*` matches `v1.0` and `v1.0.0` alike, though Go's module tooling reads only the three-part form as a version.
   4. **After the tag**: what the run closes (TKT-01M3YSVEDJ, then the tracking ticket), and nothing it publishes.
7. **The PR** quotes, from the merged head's step logs:
   - every `TestPhase4Exit` line;
   - every `phase4-exit.spec.ts` line in both projects;
   - the `npm run budget` line;
   - the dry run's lines;
   - `cmd/ketju`'s race-step time;
   - the deliverable map.

### Tests

The tests above are the deliverable. Beside them:

- `TestOneDoorIntoSessions` passes with its caller lists unchanged. This ticket adds no session path.
- Before pushing, record two times in a ticket note: `go test -race ./cmd/ketju/ -run '^TestPhase4Exit' -count=1` on the grooming host, and the full `cmd/ketju` package under `-race`, before and after.
- Try these mutations, and say in the commit whether each was caught:
  - dropping the target-forum `moderate` check from `movePosts`, which `TestPhase4ExitMovesNeedModerateInBoth` should catch;
  - filling `moved_to` without the read check, for the notices test;
  - serving a moved post's blob, for the moved-posts test;
  - letting the actor's seal lapse on reopen, for the archive test's restart;
  - copying logs whole in an online backup, for the backup test;
  - serving `/metrics` on the main listener, for the metrics test.

### Out of scope

- Every feature under test. A failure here goes back to the owning ticket as a bug:
  - TKT-01M29CXJZZ, TKT-01M29CYSAM and TKT-01M29CYSBA (moving posts);
  - TKT-01M29CYSC0 (reply as a new thread);
  - TKT-01M29CRJ64 and TKT-01M3YSQQ33 (archive);
  - TKT-01M3B12ZZ4, TKT-01M29CYSCP and TKT-01M3YSQPZX (tags);
  - TKT-01M3YSQ4CR (avatars);
  - TKT-01M29CXK35 (backup);
  - TKT-01M29CEE7S (metrics);
  - TKT-01M29CXK3S (the dry run and the release notes);
  - TKT-01M3YME9AZ and TKT-01M3YTHAZB (backlinks).
- Publishing, the secrets and the tag: the maintainer, through TKT-01M3YSVEDJ and the checklist.
- Changing the race step or its timeout: the maintainer, if the 900 s bound is crossed.
- Real devices: TKT-01M38WTKSQ. No agent ticks such a criterion.

## Notes

**agent:claude-code/phase4-groom-exit** at 2026-10-02T21:25:11Z

run decision: pending maintainer review. Recorded in docs/06 "Moderator flow for drift", after the maintainer's "Reply as a new thread" block (2026-10-02, branch groom/p4-exit). "Reply as new thread" stays offered under a post of a locked or archived thread, to a member who may read it and may create a thread. It writes only the new thread's log, so an archived source stays read-only. The "continued in" note shows under the source post as for any other thread.

Alternatives:
- Withholding it on an archived thread lost. Archiving closes a thread's history, not its subject, and a member who picks the subject up would otherwise copy the text by hand and lose the link.
- Refusing it on the server lost. 08 names no such refusal, and the refusal would guard a log the action never writes.

Why the exit check had to decide it: TestPhase4ExitArchivedThreadIsReadOnlyToEveryone needs an expected answer for this action. TKT-01M3YSQQ33's plan withholds "reply" on an archived thread, and TKT-01M29CYSC0's plan has no archived check, so the two could otherwise disagree. Both implementers should read the docs/06 line. This lane writes only this ticket, so the orchestrator relays it to them.

Grooming also settled these, without a design-document line because no member sees them:
- **TKT-01M3YSVEDJ is not a dependency.** Its first and last criteria need a tag the maintainer pushes, and the maintainer pushes it from this ticket's checklist, so a dependency would make each ticket wait on the other. Criterion 6 asks only that its release.yml has merged.
- **The Go exit tests stay in cmd/ketju** and run in both the full race step and their own step, as Phases 2 and 3 do. They have a 900 s bound on cmd/ketju's race-step time. Past it, the ticket is blocked for the maintainer, because changing the existing step needs them (#354).
- **The checklist is a file**, docs/releases/v1.0-checklist.md.

Phase 2's and 3's nine exit tests took 18.1 s together under -race on the grooming host (12 cores, b31d0938), as a baseline for criterion 5.

**agent:claude-code/phase4-run-mg0nx** at 2026-10-03T11:19:00Z

Prepared on branch run/mg0nx-phase4-exit (unpushed), from main at fbe569d4, while TKT-01M3V6XVVG, TKT-01M3V73KM1 and TKT-01M3YSQ4CR still merge. TestPhase4ExitAvatarsAreServedOnlyToMembers waits on TKT-01M3YSQ4CR's route.

### Times under -race, locally (12-core host, load 1.4 to 4.5)

- `go test -race ./cmd/ketju/ -run '^TestPhase4Exit' -count=1 -v`: 20.2 s for the seven tests written so far (1.5 s to 6.9 s each).
- The whole `cmd/ketju` package: 234.5 s before (`-skip '^TestPhase4Exit'`) and 243.1 s after. The 900 s bound is CI's race step, quoted in the PR.

### Where the tests differ from the plan, and why

- **gail's refusals are `not-found`, not `forbidden`.** gail cannot read annex, and a move answers a forum or thread the actor cannot read as not-found, so it never tells a hidden forum from one that does not exist (TKT-01M29CYSBA). Both logs stay byte-identical and no thread file appears, as the plan asks. An extra refused case, otto splitting annex into vault (he moderates the source and reads the target), is the only one that reaches the split's target-forum check.
- **The notices test merges both ways.** The plan merges annex into vault, yet asks pat (annex only) to see "merged from" with a null thread, which only a merge into annex can show. The test merges vault into annex and annex into vault, so gail and pat each see a merged-from notice without its thread.
- **Each signed-in actor sends from a loopback address of its own** (127.0.0.11 and up). The write ceiling is per address, 60 burst at 2 a second (docs/08), and the archive test's 114 refused writes plus its setup are more than one address's burst. The shared `actor` helper gained an optional client (cmd/ketju/flow_test.go); every other test is unchanged.
- **"Served from the response cache"** is read from `ketju_cache_hits_total{cache="response"}` on the world's metrics listener, before and after each second request.
- **The backup is held after serve answers the snapshot** (a `backupOpts.ask` hook) until three more replies land, so the logs it copies have grown past the snapshot. The test also asks for `caught_up` 0 and no drift. Without the hold, copying logs whole went unseen.
- **The e2e seed** gives the `moderators` group read on the `moderators` forum, with mona in it, as the forum's description says. The moderator role moderates everywhere but reads a restricted forum only through a grant, so mona could not split into it. No user or forum is added, so no id moves. forum-index, search, thread-rename-move, posts-split, phase3-exit, phase2-exit, admin-queue, thread-moderation, a11y, admin-applications, profile, new-thread and admin-forms pass on it.

### Mutations, each caught

- Dropping the target-forum moderate check from the split and the merge: TestPhase4ExitMovesNeedModerateInBoth.
- Filling moved_to without the read check: TestPhase4ExitNoticesNameOnlyWhatTheViewerMayRead and TestPhase4ExitMovedPostsFollowTheTargetForum.
- Serving a moved post's blob: TestPhase4ExitMovedPostsFollowTheTargetForum (blob and thumbnail 200 to gail).
- Letting the actor's seal lapse on reopen: TestPhase4ExitArchivedThreadIsReadOnlyToEveryone (three replies 201 after the restart).
- Copying logs whole in an online backup: TestPhase4ExitBackupRestoresAndPassesFsck (`caught_up` 1).
- Serving /metrics on the main listener: TestPhase4ExitMetricsServeOnlyOnTheirOwnListener.

### Found by the exit spec

TKT-01M40PCE7P (Give a removed post's footer buttons the contrast its links have): axe color-contrast at 1440 px on a removed post's footer buttons, 4.44:1. Filed phase-4 because it fails criterion 1; this ticket depends on it, and its CSS fix rides in this PR with the regenerated dist once the dist is handed to this lane.
