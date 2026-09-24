---
schema: 3
id: TKT-01M29CA99RTPEA1B3HG24B2KNJ
title: Implement GET /me
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:api
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29C801M9WBEAVCCC6AXNZ4Z
blocks_on: none
references:
  - ref: design:api-resources
    path: docs/07-api-and-clients.md
  - ref: pr:62
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:19:48Z
updated_at: 2026-09-24T12:31:30Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

The viewer envelope: user, role, unread counts, pending notification count, and settings. The client bootstraps from it, and the SPA shell inlines its response in a `<script type="application/json">` block to save a round trip on first load.

That inlining is why the response has to be cheap and free of anything expensive to compute. An unread count that costs a table scan turns every page load into one.

## Acceptance criteria

- [x] Returns the documented shape for an anonymous viewer, a member, a moderator, and a pre-user session
- [x] Counts come from projections rather than being computed from logs
- [x] The handler issues at most 2 SQL queries per request, asserted by a test that counts them, so it can be inlined on every shell render

## Implementation plan

Revised at claim (2026-09-24), after reading the code as it stands: the session store just merged (C5Y3E), `dto.Me` and its goldens, `perm.Evaluator`, `internal/web/api`, the viewer-state and projection tables, and the `UnreadCount` query.

### Approach
1. **Budget: at most 2 statements per request for a member, 0 for anonymous.** Transaction control is not counted.
   - Statement 1 is the middleware's `LookupSession`, which already returns every user field /me shows (C5Y3E designed it for this).
   - Statement 2 is `MeCounts`.
   - `perm.ReadableForums` cannot be used: it lists forums with a query on every call, which would be a third statement.
   - Instead, `MeCounts` returns unread counts grouped by `forum_id`, and the service keeps the forums `perm.Can(v, forum, read)` allows. That check is served from perm's cache (60 s TTL, docs/02). A cold cache adds perm's own lookups, and the budget test states that it is measured with a warm cache.
2. **`MeCounts`, one sqlc statement.** It returns one row per forum with unread threads, plus the viewer's unread notification count on every row (a single row with a null forum when there are none).
   - **Unread thread:** `last_post_ordinal > coalesce(last_read_ordinal, 0)`, and `last_post_at > mark_all_read_at` when the mark is set.
     - This is `UnreadCount`'s per-post rule (docs/04) applied to the thread's newest post. The newest post has both the highest ordinal and the latest time, so "some post is unread" and "the newest post is unread" are the same test.
     - Using `threads` directly avoids scanning post_meta per thread.
     - A test holds the two rules together over random data.
   - **Participated:** the thread is among those the viewer has authored a post in, reached through the `post_meta_author` index.
   - **Watching:** a thread-level watch at level 'watch'.
   - **Muted:** a thread-level mute removes the thread from all three counts.
   - Forum-level watches, and the exact membership of each list, belong to the cross-forum lists ticket (CTY7K, draft). This definition is recorded there so the lists and the badges agree.
3. **`internal/service/users`: `Service{DB, Perm}` and `Me(ctx) (dto.Me, error)`.** It reads the viewer and the session from the context.
   - For an anonymous viewer, it returns the anonymous shape: settings UTC, en, 25, as in `me.anon.json`.
   - For a member, the user comes from the session and the counts from `MeCounts`, filtered through perm. Settings: `timezone` or "UTC", `locale` or "en", and `page_size` from `prefs_json.page_size` when it is an integer from 10 to 50, otherwise 25 (docs/06).
   - For a pre-user session, it returns the anonymous shape as well. `dto.Me` says a nil user is the only signed-out signal, and a pre-user is not a member; whether the approval flow needs a marker is TKT-01M29CKRG9's call (the ticket's open item). AC1's pre-user clause therefore stays open until that shape is decided.
   - For an inactive user (suspended), the session resolves with Active=false. That viewer is not a member, so it also gets the anonymous shape: nothing about their account is disclosed after suspension.
4. **API.**
   - `GET /api/v1/me` in `internal/web/api`, with `Deps.Users`.
   - `Handler.ShellPage(r)` for `web.Shell.SetPage`. It gives the bootstrap `Me` and leaves the title at its default. The site name and the wiring into `ketju serve` are CCBYX's.
5. **Test hook: `db.Options.Trace func(sql string)`.** It installs SQLite's statement trace on every pool connection, only when set. That lets a test count what actually ran, rather than trusting a wrapper around the call sites.

### Tests
- **AC1:** anonymous, member and moderator responses are decoded with `DisallowUnknownFields` into `dto.Me` and compared with seeded data. The pre-user and suspended cases get the anonymous shape.
- **AC2:**
  - An import test: `internal/service/users` must not import threadlog or threadcache.
  - The counts are asserted with no thread logs on disk at all; the projection rows are written directly.
  - A property test: over random threads, positions, marks and posts, `MeCounts`'s thread rule agrees with `UnreadCount` > 0 for every thread.
- **AC3:** through `web.New` with the session middleware and a warm perm cache, GET /me runs at most 2 statements for a member and 0 for an anonymous viewer. So does a shell render through `ShellPage`.
- **Also:**
  - readability: a thread in a restricted forum the viewer cannot read does not count;
  - mute excludes a thread;
  - a watch counts;
  - participation counts;
  - the page_size bounds;
  - the shell inlines a member's Me, escaped, with the nonce.

### Settled in grooming (2026-09-24)

Restored verbatim from the groomed plan. The claim-time `git ticket plan` replaced the whole Implementation plan section, and this sub-section went with it. The work above contradicted it until review 408 caught that; see the note answering that review.

- A pre-user session gets `user: null` plus a separate `pre_user` object. A null user stays the only signed-out signal. Document it in docs/07 with a golden test.
- The 2-query budget covers the handler. The session middleware's single lookup is outside it.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'Cheap enough, with a stated query budget' did not state the budget. It is now at most 2 queries, one for the viewer and session and one for the projected counts, asserted by a test that counts queries.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T11:59:38Z

Built and verified.

### What landed
- **`GET /api/v1/me`** in `internal/web/api`, marked `Cache-Control: no-store`. `api.ShellPage(users)` gives `web.Shell.SetPage` the viewer's `Me` as the bootstrap block. The title stays the default until CCBYX wires site_name.
- **`internal/service/users`:**
  - `Me(ctx)` reads the viewer and the session from the context. The user fields come from the session lookup that has already run.
  - The counts come from one statement, `MeCounts`, kept per forum where `perm.Can(read)` allows (served from perm's cache).
  - Settings: `timezone` or UTC, `locale` or en, and `page_size` from `prefs_json` when it is a JSON integer from 10 to 50, otherwise 25.
- **`MeCounts`:**
  - A thread is unread when its newest post is: `last_post_ordinal > coalesce(last_read_ordinal, 0)`, and after `mark_all_read_at`.
  - Participated goes through post_meta's author index. Watching is a thread-level 'watch'. A thread-level mute excludes the thread from all three counts.
  - The notification count comes back on every row, and there is always one row.
- **`db.Options.Trace`:** SQLite's statement trace on both pools, only when set, so tests count what actually ran.
- **`internal/auth/session/sessiontest`:** SignIn and PreUser for other packages' tests. The store still exports no way to mint a session. `session.TestSessiontestMatchesTheStore` keeps the helper on the store's scheme.

### Decisions (alternatives considered)
- **Not `perm.ReadableForums`.** It lists forums with a query on every call, which would have made /me three statements. Grouping the counts by forum and filtering through `perm.Can` costs nothing on a warm cache.
- **The thread rule rather than per-post counting.** Scanning post_meta per thread is what the ticket warned would turn a page load into a table scan. The two rules agree because the newest post has both the highest ordinal and the latest time, and `TestThreadRuleMatchesPostRule` checks that over 20 random datasets: every thread, positions from none to past the end, ties in time, marks set and unset.
- **Mute excludes; forum-level watches are not counted.** docs/06 names the surfaces without defining them. A thread mute is unambiguous. Forum watches, and exactly what each cross-forum list holds, belong to TKT-01M29CTY7K, which is noted to reuse this rule.
- **A pre-user session and a suspended user get the anonymous shape.** `dto.Me` makes a nil user the only signed-out signal. A pre-user is not a member, and a suspended user's account is not theirs to read. Whether approval mode needs a marker is TKT-01M29CKRG9's question, which is noted there.

### Against the acceptance criteria
- **AC1 is not ticked: its pre-user clause has no documented shape to return.**
  - Anonymous, member and moderator are tested: each response is decoded with `DisallowUnknownFields` and compared in full with the seeded data. That includes the counts across read, unread, muted, marked-read, watched, participated and unreadable-forum threads, and a display name containing `</script>`.
  - Pre-user and suspended admin both return the anonymous shape and are tested to.
  - Ticking the criterion needs CKRG9 to decide the pre-user shape.
- **AC2:**
  - `users` does not import threadlog or threadcache. The check covers direct imports only, because `internal/config` names threadcache's types and every package that logs reaches it transitively.
  - Every /me test answers from a database with no thread log on disk.
  - The rule-agreement test above.
- **AC3, with two conditions stated here rather than hidden.**
  - Through `web.New` with the session middleware and a warm perm cache, a member's /me and a member's shell render each run exactly 2 statements, counted from SQLite's trace: `LookupSession` and `MeCounts`. Anonymous runs 0.
  - A request on which the session's last_used_at falls due (at most once per 5 minutes per session, docs/05) also runs the store's rate-limited UPDATE, 3 statements in all. `TestMeBudgetWhenTouchIsDue` pins that exactly, and the next request is back to 2.
  - A cold perm cache adds perm's own lookups, at most once per 60 s TTL.
- **Mutation:** 16 mutants, 15 killed:
  - SQL: both unread conditions, their boundaries, the mute exclusion, the participation filter, and both notification filters;
  - Go: the perm filter, the member guard, the default timezone and locale, notifications, watching, and the page_size bounds;
  - the trace hook.
  The survivor, dropping `level = 'watch'` from the watching test, is equivalent: a muted thread is already excluded from every count. The filter stays for clarity.
- The full CI set passes locally: gofmt, build, vet, sqlc stable, and `go test -race ./...`.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T12:19:02Z

Review 408 (1 high, 2 medium; all three addressed).

### How the settled section was lost
The groomed plan had a "Settled in grooming" sub-section inside the Implementation plan. The claim-time `git ticket plan` replaces that whole section, so it went with it. I planned and built against the ticket's older open item, which said the pre-user shape was undecided, not against the settlement. Both settled decisions are now back in the plan, verbatim.

Lesson for the next claimant of a groomed ticket: read the plan section before running `git ticket plan`, and carry forward anything under "Settled".

### (1) High: pre_user marker (fixed, per the settlement)
- `dto.Me` gains `pre_user`: null for everyone but an applicant, and `{provider}` for a pre-user session. `user` stays null for them, so a null user is still the one signed-out signal.
- The provider is the session's `oidc_provider_key`, now returned by `LookupSession`.
- It is documented in docs/07's /me row. There is a new golden, `me.pre_user.json`, and the other three /me goldens gained `"pre_user": null`.
- The client's `Me` type and `anonymous` value carry the field, and dist is rebuilt.
- `TestMeShapes` asserts the pre-user shape end to end.
- A suspended user still gets the anonymous shape.
- AC1 is now ticked.

### (2) Medium: MeCounts examined every thread (fixed and measured)
- The ticket said to "measure at 10k threads before adding a counter table", and `BenchmarkMeCounts` now does. The dataset is 10,000 threads in 20 forums, about 100k posts, a heavy reader (2,000 read positions, around 300 posts, 50 watches, 20 mutes, a mark set) and a light reader (20 read positions, no mark).
- The old query did per-row joins across every thread whoever asked: 9.9 ms for the heavy reader, 14.7 ms for the light one.
- **The rewrite has one site-sized part:** a per-forum count of threads with a post after the mark, all of which are unread to someone who never opened them.
- **Everything viewer-specific starts from the viewer's own rows** (read positions, watches, their posts) and subtracts from that count, restricted to threads after the mark. Cost therefore grows with what they have touched, not with the site.
- **Result: 5.8 ms for the heavy reader and 4.2 ms for the light one.** This is SQLite under wazero (ncruces), at roughly 1.5 µs per row step; the query plans use an index at every join.
- **What remains is not zero.** The site-wide count reads the threads table (2.2 ms at 10k). A covering index on `threads (forum_id, last_post_at, last_post_ordinal)` would make it index-only (0.8 ms measured without the ordinal test), and a per-user counter table would remove it entirely. Both are new schema, beyond this ticket, and I filed neither: at 4–6 ms on the page-load path this is not urgent, and the benchmark is here for whoever revisits it.
- **Correctness:** `TestThreadRuleMatchesPostRule` now covers every count, not just the thread count, and threads now share forums. It runs 60 random datasets with read positions, marks, ties in time, zero-post threads, watches, mutes and authorship. Each forum's threads, participated and watching counts are compared with `UnreadCount` and the reader's own rows. Sharing forums matters: two mutants (dropping the after-mark filter, and treating zero-post threads as active) survived while every thread had a forum of its own, and both are killed now.

### (3) Medium: budget exceeded on a cold perm cache (fixed within the settled budget)
- Per the settlement, the budget is the handler's two statements, with the middleware's lookup outside it.
- `perm.Evaluator.CanEach` evaluates every forum not yet in the viewer's cache in one statement, however many there are: `ForumAccessRows`, which returns each forum's visibility with every permission row a group of the viewer's holds there or globally. perm's rules are still applied in Go.
- `evaluate` (one forum, used by Check) now goes through the same code, so the rules exist once.
- The old one-forum evaluation is kept in the test file as `referenceEvaluate`. `TestCanEachAgreesWithCheck` holds the batch to it, and to Check, cold and warm, over 8 random worlds: forums of every visibility, global and local grants, group memberships, and anonymous, member, moderator, admin and inactive viewers.
- `TestCanEachColdCostIsFixed` shows one statement for one forum or twenty, and none when warm. `TestCanEachDoesNotCacheMissing` checks that a forum which does not exist yet is not remembered as missing, as Check guarantees.
- **The budget test, by statement name:**
  - member /me cold: `LookupSession`, `MeCounts`, `ForumAccessRows`, so the handler runs 2;
  - member /me warm, and the member shell: `LookupSession`, `MeCounts`;
  - anonymous: nothing.
- **Two things are not counted, and the test says so:**
  - pool connection setup (`PRAGMA`);
  - perm finding its implicit groups, once per process: the test warms that with another viewer first, so alice's first request is a truly cold per-viewer cache on a running server.
- A due session touch adds the middleware's rate-limited `TouchSession`, and `TestMeBudgetWhenTouchIsDue` pins the sequence.
- A `sqlc` pitfall hit on the way: a named argument placed after an expanded slice gets a number that points at the wrong value. The query puts `user_id` first and says why.

### Mutation on this round
All killed except those stated as equivalent:
- **MeCounts (14):** the mark and ordinal tests, the subtraction, the mute exclusion, each branch of the touched union, the after-mark filter, the zero-post rule, the mark default, the perm filter, and pre_user on and off. Equivalent: `watched` from any watch level (a muted thread is never unread), and the `> 0` row filter (it changes which forums reach perm, not the counts).
- **perm (13):** the members-forum exclusion of everyone, the local-over-global rule, restricted with no fallback, hidden, the members group and the user id, caching a missing forum, the per-permission answer, global rows, and the membership filter.

The full CI set passes locally: gofmt, build, vet, sqlc stable, the client build and tests, `go test -race ./...`, and `git ticket check --strict`.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T12:31:30Z

### Review 409, answered

**Finding 1: a cold Evaluator paid an extra statement to find the implicit groups.** `loadImplicit` is gone. ForumAccessRows now matches the everyone and members groups by slug inside the same statement, and returns each grant's group slug, so `evaluateMany` can tell an everyone grant apart without knowing the group ids up front. A cold /me is now LookupSession, MeCounts, ForumAccessRows, and me_test measures the very first request rather than warming up first. `TestCanEachColdCostIsFixed` uses brand-new Evaluators, and the reference evaluator looks the groups up by slug on its own.

**Finding 2: MeCounts counted active threads by scanning every thread.** Migration 0007 adds two things.
- `forum_thread_counts`: per forum, the threads that have a post. Triggers on `threads` maintain it (insert; delete; update of forum_id or last_post_ordinal). It is a projection table: it is in `ProjectionTables`, carries the marker, and ClearProjections clears it by name.
- `threads_forum_active (forum_id, last_post_at, last_post_ordinal)`.

MeCounts' `active` now starts from the counter. A reader with no mark takes the stored count. A reader with a mark gets a correlated count per forum over the index, `INDEXED BY`, so dropping the index is an error rather than a quiet scan.

Alternatives considered:
- A covering index alone: still a full index scan for a reader with no mark, who is the common case.
- A count the projection writer maintains: every path that writes threads (the writer, catch-up, reindex) would have to know about it. Triggers cover them all.
- A per-user unread counter table: kept for later if the viewer-side part ever dominates. The benchmark says it does not yet.

**Tests:**
- `TestForumThreadCountsFollowThreads`: 40 seeds × 200 random insert, delete, move, post, move-and-post and ClearThreads steps, compared after every step with a count from threads.
- `TestForumThreadCountsBackfill`: migrates to 0006, adds threads, then runs 0007.
- `TestMeCountsScansNoTable`: EXPLAIN QUERY PLAN of the source query. It allows SCAN only of its own CTEs and the counter.
- The clear test plants a drifted counter row.

**Mutants:** 19 mutants over the triggers, the backfill, the `active` CTE, INDEXED BY and the clear call. All are killed.

**Benchmark** (10,000 threads, 20 forums, ~100,000 posts):

| Reader | Before | After |
|---|---|---|
| Heavy (mark set, 2,000 read positions) | 5.8 ms | 4.5 ms |
| Light (no mark) | 4.2 ms | 0.33 ms |

The heavy reader's remaining cost is their own rows.

docs/04 documents the table and the index, and the docs/07 /me row names the cold-cache statement.

## Summary

GET /api/v1/me returns the viewer's user, unread counts (threads, participated, watching), unread notifications and settings. An approval-mode applicant gets user null plus pre_user {provider}, as settled, with a golden and docs/07. The handler runs at most two statements, as settled: MeCounts, plus perm's one-statement batch (CanEach) when its cache is cold. The middleware's session lookup is outside that. MeCounts reads only a per-forum count across the site and otherwise the viewer's own rows: 4-6 ms at 10k threads, against 10-15 ms before. api.ShellPage inlines the same value into the shell. db.Options.Trace lets tests count statements.
