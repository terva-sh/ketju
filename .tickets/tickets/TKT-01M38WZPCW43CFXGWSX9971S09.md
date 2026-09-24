---
schema: 3
id: TKT-01M38WZPCW43CFXGWSX9971S09
title: Drive Phase 1 to its exit gate autonomously
type: chore
status: ready
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
  - ref: pr:47
    path: null
claim: null
archive: null
created_at: 2026-09-24T05:07:34Z
updated_at: 2026-09-24T10:12:34Z
created_by:
  id: agent:claude/phase1-groom
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
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
with, a change to `AGENTS.md` or docs/12, or anything outside `phase-1`.

On 2026-09-24 the user also allowed run PRs to edit the existing Build and
Test job in `ci.yml`: adding steps and moving it to a Debian-based Go image
for Playwright. The job and workflow names, the triggers, the permissions,
`terva-review.yml`, secrets, and branch protection all stay untouched. docs/12,
"Changing CI", states the limits.

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
- ASCII handles, and a fresh install where members read and post;
- quarantine, not refusal, for a corrupt thread at startup;
- Edit records carry optional mentions, added without a format version bump;
- hand-written CSS;
- 25 posts per page;
- the reader lands at the last-read post;
- a five-item tab bar with Phase 3 placeholder screens, and a composer sheet,
  on phones;
- self-hosted fonts that do not count toward the size budget;
- a reverse proxy terminates TLS;
- one process per data directory.

### Queued behind the gate

The real-device checks are TKT-01M38WTKSQ, labelled `human-only`.

## Acceptance criteria

- [ ] TKT-01M29CGBY1 and TKT-01M29CGBXK are done
- [ ] Every merged run PR has a note here with its ticket, PR number, and merge commit
- [ ] docs/11 counts are refreshed from the store at the gate

## Implementation plan

These waves were recomputed after promotion on 2026-09-24. A ticket's wave is
one more than the highest wave among its open dependencies. `git ticket ready`
is the source of truth for what can start; this list only helps choose among
ready tickets and estimate what remains.

Critical path: BSPE8 → W9Q1 → BYYHG → C5Y2E → CA9AX → CEE6V → CGBY1.

- W0: BSPE8 (SQLite benchmark, go first), P9RS0 (doc fixes), 38WW7 (go.mod
  tidy), C2WWG (structured render output), CA99R (GET /me), CA9C2 (post,
  replies, history), CCBZ8 (healthz), BYYJG (config check), CCBYG (profiles),
  CCBZN (lock file), CCC02 (Preact scaffold), CCC0F (CSS and themes)
- W1: W9Q1 (DB layer, sqlc, migrations), CA9BP (edit, delete, restore), CA9D4
  (POST /render), CEE4K (app shell), CEE52 (router), CEE5H (fetch and signals)
- W2: BYYH7 (users and sessions), BYYHG (forums and permissions), BYYHQ
  (viewer state), BYYJ0 (flags, audit, blobs, settings), BYYJ8 (projection
  tables), CCBYX (serve and shutdown)
- W3: C2WW5 (projection writer), C5Y2E (permission evaluation), C5Y2S
  (sweeper, blob GC), C5Y3E (session store), C800G (bootstrap admin), CA9CE
  (viewer-state endpoints)
- W4: C5Y1S (startup catch-up), C5Y25 (reindex), C7ZZR (session
  chokepoint), CA9A5 (forums endpoints), CA9AG (thread list), CA9AX (thread
  page), CA9B9 (create and reply), 38XJJKM (handle autocomplete)
- W5: C7ZZ9 (OIDC flow), C8005 (open registration), CCBY0 (response cache),
  CEE5Y (forum index screen), CEE6D (thread list screen), CEE6V (thread page
  screen), CEE79 (composer)
- W6: CGBVN (mobile composer sheet), CGBW6 (reply expander), CGBWN (read
  beacon), CGBX4 (shortcuts), CGBXK (accessibility, gate), CGBY1 (Playwright,
  gate), CGBYH (bundle budget)
- After the gate: 38WTKSQ, the real-device check. It is human-only and the
  run never picks it.

A second lane (docs/12, "Running two lanes") can start the client track from
W0, with CCC02 and CCC0F.

Each merged PR adds a note here giving the ticket, the PR number, the merge
commit, and anything the next ticket needs.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:23:36Z

The grooming PR is #47 (https://git.local.sothr.com/terva-sh/ketju/pulls/47). A person merges it, because it is the PR that creates this run's merge authority and so falls outside it. The run starts once it lands on main.

**agent:claude/phase1-groom** at 2026-09-24T05:24:25Z

Terva review of PR #47: requested by workflow dispatch with request-id ready-review. The tea call returned an empty-JSON error, but the run was created: run 106 (https://git.local.sothr.com/terva-sh/ketju/actions/runs/106), reviewing base 39b54b2b6746b1f235540a6f910d7f93fd5fd351 and head 4c88d86e6537657cb77b69c5b6fcb26c2d5507c9. The Review PR step failed with context_limit and nothing was published: the diff, mostly ticket-store files, is larger than the reviewer takes. That is a runtime failure, not a clean review, so there is no model evidence for this PR. It does not block the merge, because a person merges this PR. docs/12 now tells the run how to treat context_limit on its own PRs. No retry was made, because the same diff would fail the same way.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T06:36:48Z

Amendment to the merge gate, from the user on 2026-09-24, after PR #47 merged and the run started:

> The reviews are nice to have and if they fail consistently we should stop and address but they don't need to block if they fail occasionally

This changes one rule in docs/12, "The merge gate": "A review that fails to run is not a clean review, and the gate does not hold." It does not change the rest of that section, and docs/12 is left as written because a run PR may not edit it.

How the run applies it:
- A Terva review that fails to run, whether from infrastructure, a timeout, or context_limit, does not block a merge when everything else in the gate holds on the exact head. The PR description and this log say that the review did not run, and link the failed run.
- A review that runs is still binding. A finding at medium or above blocks as before, and low findings are still fixed or answered.
- "Consistently" means review runs failing on two run PRs in a row. The run then halts and a person looks at the review workflow. A single retry on the same head does not count as a second PR.
- PR #47 merged by the user at ac05a3c on 2026-09-24, without a completed review, is outside this rule. A person merged it.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T06:59:01Z

Merged: TKT-01M29BSPE8 (Decide the pure-Go SQLite driver), PR #48, rebased onto main at 9aa4cf1b7dac9d793968462dbc3a487a3ea9eda0. Gate: CI run 115 succeeded, and its step log shows the race tests and the new 'check the SQLite driver spike' step ran. The clean Terva review is run 5299b680 on 9aa4cf1. Two earlier findings were fixed: 364 (high, directory fsync must be a prerequisite) and 365 (medium, unbounded busy retries). For the next tickets: ncruces stands, and BSPE8's 2× halt rule was not met. TKT-01M29BW9Q1 now has an extra acceptance criterion, a data-directory fsync once the WAL exists, and its single write connection is required, not optional. The shared DSN keys are proven on both drivers. Reviews ran on all three heads; none failed to run.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T07:21:49Z

Merged: TKT-01M29BW9Q1 (Build the database access layer and migration runner), PR #49, rebased onto main at 1c605e5f9a3f03edeee4206d68467597a137f363. Gate: CI run 126 succeeded, and its step log shows the new 'generated database code is current' step ran sqlc v1.31.1 with no diff, and internal/store/db passed under -race. The clean Terva review is run 72c6f25a on 1c605e5. Six medium findings over three rounds were all accepted and fixed: 368 (embed pattern, path escaping), 370 (dot and underscore files, blocked checkpoint), 372 (created parent directories, bad migration record). Every review ran; none failed to run. For the next tickets: internal/store/db exposes Open, Read, Write, Migrate, Close and ErrBusy, ErrSchemaNewer, ErrMigrations. Queries go in internal/store/db/queries/*.sql, and schema goes in migrations/NNNN_name.sql, renumbered on rebase if two branches collide. Run CGO_ENABLED=0 go generate ./internal/store/db and commit dbq/. sqlc parses triggers with RAISE, but not FTS5's whole-table MATCH. synchronous=FULL. The draft TKT-01M393S6P7 is a flaky threadlog test that can fail any PR's CI at 1–2%.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T07:36:38Z

Merged: TKT-01M29BYYH7 (Add the users, identities, and sessions schema), PR #50, rebased onto main at e422e22113dafe542a645d09588bd3c52e5101f7. Gate: CI run 135 succeeded, and its step log shows internal/domain and internal/store/db passing under -race and the regen step clean. The clean Terva review is run 020e43ee on e422e22. Two medium findings were accepted: 374 (embedded NUL in a handle; reproduced and fixed) and 377 (BLOB handle; did not reproduce because substr() of a BLOB never matches the GLOB, so typeof = 'text' was added as hardening). Order change: TKT-01M29BYYHG was claimed first, but group_members references users, so BYYH7 went first and the missing dependency is now recorded. For the next tickets: migration 0002_identity exists, so the next migration is 0003. domain.ValidateHandle is in internal/domain, and sessions use sqltypes.SessionHash. Migration-runner tests use openUnmigrated; schema tests use openTest.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T07:57:27Z

Merged: TKT-01M29BYYHG (Add the forums, groups, and permissions schema), PR #51, rebased onto main at 3340bc14e2471aad6e4774c9f7c6d6761ac59c4c. Gate: CI run 148 had all 16 steps succeed, internal/domain and internal/store/db passed under -race, and the regen step ran sqlc v1.31.1 with no diff. The clean Terva review is run 0f113922 on 3340bc1. Five medium findings over four rounds were all accepted and fixed: 378 (update into implicit groups; system group ids), 379 (forum id 0 colliding with the global-grant sentinel), 380 (an admin taking over an IdP membership), 381 (root forum ordering). Every review ran. For the next tickets: migration 0003 exists, so the next is 0004. Group ids 1–4 are everyone, members, moderators and admins. PermissionRows(forum, groups) is the evaluation query. TestQueriesDoNotMixParameterStyles refuses sqlc queries that mix sqlc.arg with a bare ?; name every parameter when using sqlc.arg.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T08:16:54Z

Merged TKT-01M29C5Y2E (permission evaluation) as PR #52 at ad7dbde. The first review, 383 at c2c4a6f, raised two high findings, both accepted and fixed: invalidation is now shared by every Evaluator over the same DB, and a role change applies from the next request's Viewer. The clean review ran at ad7dbde, CI run 155, and the race step ran internal/perm. For later tickets: use perm.New(db) and route every grant, membership, visibility and role write through its methods (a layering test enforces it). TKT-01M29C5Y3E must build Viewer from the users row on every request. Filed draft TKT-01M39788AT, which asks what an archived forum does to permissions. Next: TKT-01M29CA9AX.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T08:29:08Z

Merged TKT-01M29BYYJ8 (projection tables) as PR #53 at a15a1aa, with a clean review at a15a1aa and CI run 160. AC3 (rebuild equality) is left unticked for TKT-01M29C5Y25. For later tickets: db.ProjectionTables, db.ClearProjections (reindex empties the tables, never drops them) and dbtest.DumpProjections. Before that, TKT-01M29CA9AX (thread page) turned out to need the threads projection (BYYJ8 and C2WW5) and the viewer-state schema (BYYHQ). Those are now recorded as dependencies, and CA9AX was released to ready. Next: TKT-01M29C2WW5, then BYYHQ, then CA9AX.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T08:57:07Z

Merged TKT-01M29C2WW5 (projection writer) as PR #54 at 46b6a13, with a clean review at 46b6a13 and CI run 169. Two review rounds ran first, all findings accepted and fixed. Review 389: Delete must not move post_meta.log_offset, and Sweep became required. Review 390: a thread row must exist before a thread counts as projected, and the log header is now validated. For later tickets: projection.New(Options{DB, Root, Sweep}) returns an error if any is missing. Run(ctx) is the retry loop. ProjectThread(ctx, id, len) is idempotent catch-up. post_meta.log_offset means the current revision's record, and migration 0004's comment on it is wrong (docs/04 says so). Filed draft TKT-01M39973KG: docs/03 says reserved types are refused, while the reader skips them. Next: TKT-01M29BYYHQ, then TKT-01M29CA9AX.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T09:11:02Z

Merged TKT-01M29BYYHQ (viewer-state schema) as PR #55 at 0e70101, with a clean review at 0e70101 and CI run 174. For later tickets: UnreadCount(user, thread) and UpsertReadPosition (which only moves forward). Drafts use partial unique indexes. TKT-01M29CA9CE decides whether a new-thread draft needs a title. Next: TKT-01M29CA9AX, whose dependencies are all done now.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T09:47:41Z

Merged PR #56, TKT-01M29CA9AX 'Implement the thread page endpoint', as aab5c59 (rebase). CI run 185 passed, and its step log shows go test -race ran over render, service/threads, threadlog and web/api. Terva: reviews 391 (1 high, 2 medium), 392 (1 medium) and 393 (3 medium) all ran and found real issues, each fixed; review 394 is clean at medium, with one low finding (empty numeric query parameters accepted), deferred to TKT-01M29CA9C2. These were not run failures, so the two-in-a-row halt rule does not apply.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T10:12:34Z

Merged PR #57, TKT-01M29BYYJ0 'Add the flags, audit log, blobs, and settings schema', as 1afc4e8 (rebase). CI run 191 passed, and its step log shows go test -race ran over config, settings and store/db. Terva review 395 raised 1 high and 2 medium findings, all fixed. The clean run on 1afc4e8 is 4a7f9f79. The review ran each time, so none of these is a review-run failure.
