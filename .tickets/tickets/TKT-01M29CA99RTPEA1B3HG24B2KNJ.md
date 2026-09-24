---
schema: 3
id: TKT-01M29CA99RTPEA1B3HG24B2KNJ
title: Implement GET /me
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase1-run-415af6aa
  branch: run/ca99r-get-me
  worktree: /home/sothr/.t3/worktrees/ketju/t3code-415af6aa
  commit: 0644d0cf879f27db4157bad718f1a8fea226a978
  session: null
  claimed_at: 2026-09-24T11:51:49Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:19:48Z
updated_at: 2026-09-24T11:53:54Z
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

- [ ] Returns the documented shape for an anonymous viewer, a member, a moderator, and a pre-user session
- [ ] Counts come from projections rather than being computed from logs
- [ ] The handler issues at most 2 SQL queries per request, asserted by a test that counts them, so it can be inlined on every shell render

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

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'Cheap enough, with a stated query budget' did not state the budget. It is now at most 2 queries, one for the viewer and session and one for the projected counts, asserted by a test that counts queries.
