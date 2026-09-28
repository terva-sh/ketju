---
schema: 3
id: TKT-01M3B6A68BPHWY0S0A3KAM0FF2
title: Add per-forum post totals to the forum index
type: task
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area:api
  - effort:S
assignees: []
milestone: phase-1
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:115
    path: null
claim: null
archive: null
created_at: 2026-09-25T02:29:04Z
updated_at: 2026-09-28T14:42:24Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

The forum index mockup (docs/mockups/Main.dc.html) shows a post total per forum, as in "148 threads · 3,912 posts". GET /forums (TKT-01M29CA9A5 "Implement the forums endpoints") leaves it out.

Computing it on read means summing `threads.post_count` per forum, which scans every thread on the front page's request. MeCounts and the forum index queries scan no table that grows with the site (TestMeCountsScansNoTable, TestForumIndexQueriesScanNoTable).

### Proposed
- Add a `posts` column to `forum_thread_counts` in a new migration.
- Keep it with the same triggers that keep `threads`: insert, delete, and updates of `forum_id` and `post_count`.
- Backfill it in the migration, and add `Forum.post_count` to the DTO.

### Open
- Should hidden or deleted posts count? `post_count` today is the projection's count, whatever that includes.

## Acceptance criteria

- [x] GET /forums answers each forum's post total without scanning threads

## Implementation plan

The ticket's open question is answered by the maintainer's 2026-09-28 profile ruling (TKT-01M3KJJNDC, docs/07 `/users/{handle}` row): a post count counts non-deleted posts. The forum total follows it. A post counts when its post_meta row has deleted = 0 and moved = 0: a moved post (Phase 4) is counted in the thread it moved to, as the profile history leaves moved rows out. Hidden-by-mod has no record that sets it in Phase 1 and no projection column; a Hide record must exclude it when it lands. docs/07's /forums row says so, in this PR.

`threads.post_count` cannot be summed as the ticket proposed, because it is the latest ordinal and counts deleted posts. So:

1. Migration 0009 adds `threads.live_post_count`, the thread's post_meta rows with deleted = 0 and moved = 0, kept by triggers on post_meta (insert, delete, update of deleted/moved): a key seek on threads per post write. UpsertThread leaves it alone, because it mirrors post_meta rows that a replay from the log's start overwrites rather than removes.
2. It adds `forum_thread_counts.posts`, the sum of live_post_count over the forum's threads with a post, and replaces 0007's three threads triggers with ones that move a thread's whole contribution (1 thread, live_post_count posts) between forums, now also when live_post_count changes.
3. Both columns are backfilled in the migration, before the triggers exist.
4. ForumThreadCounts reads posts too; forums.Tree fills `dto.Forum.post_count`; golden files regenerate; dto.ts gains the field; the forum index row shows "N posts" under the thread count, as the mockup does; dist regenerates within budget.
5. Tests: the forum_thread_counts property walk grows post_meta inserts, deletes and deleted/moved flips, and compares posts to a count from post_meta; a backfill test for 0009; the reindex and incremental-equals-full tests cover the columns through the projection dump (their random logs delete and restore posts); TestForumIndexQueriesScanNoTable keeps ForumThreadCounts scanning only forum_thread_counts; an API test for post_count with a deleted post.
6. docs/04 (threads, forum_thread_counts) and docs/07 (/forums row) updated.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T14:33:30Z

The ticket's open question (do hidden or deleted posts count?) is settled by the maintainer's 2026-09-28 profile ruling, TKT-01M3KJJNDC (Decide who may see a user profile and what its post count counts), as the orchestrator's brief directed: a forum's post total counts posts that are neither deleted nor moved. Written into docs/07's GET /forums row and docs/04 in this PR.

- Moved posts are left out because a moved post counts in the thread it moved to; the profile history (TKT-01M29CCBYG branch) also filters `pm.moved = 0`. Phase 1 writes no record that moves or hides a post, so neither case can occur yet; docs/07 says the record that hides one must leave the total too.
- The total is visible only for forums the viewer can read, since GET /forums already leaves the others out, so it discloses nothing about restricted forums.

Alternatives considered for the storage, and why each lost:
- Summing `threads.post_count` per forum on read (the ticket's worry): a scan of threads on the front page, and post_count counts deleted posts anyway.
- A `posts` column fed from `threads.post_count` (the ticket's proposal): counts deleted posts, against the ruling.
- A forum-level count kept by triggers on post_meta alone: post_meta is written before SetThreadLastPost makes a thread count, and a thread move would have to recount its posts. A per-thread `threads.live_post_count` column, kept by post_meta triggers and moved between forums by the threads triggers, keeps each trigger a key seek.

The column is `forum_thread_counts.live_posts`, not `posts`: TestForumIndexQueriesScanNoTable treats the word `posts` as a per-post table and failed on it. Renaming the column was better than loosening the guard.

Reindex: both columns are trigger-derived from post_meta and threads, so `ketju reindex` rebuilds them. The reindex stale case now also scribbles both columns, and agreesWithIndex checks live_post_count against the index's Visible() entries in every incremental-equals-full case.

Mutations tried, each caught: the update trigger ignoring live_post_count changes; the insert trigger skipping its recount from post_meta; the post_meta update counting moved posts; the backfill counting deleted posts; the delete trigger leaving posts behind; the tree reporting thread count as post count.

Catch-up over 10,000 threads: 35 ms locally, against the 2 s bound. Thread-view bundle after the client change: 38,008 B gzip of 60,000 B.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T14:42:24Z

Terva review 1164 (run d61522c1-5f72-4474-9a97-6209637bf673, request ready-review-0d80e4c, Actions run #666) of head 0d80e4c5f8874ca2f0c5445c69e92fe13ea4bd18, base 50da12475294a46dcbbd9c1231fca6440d1b1708 reported one medium finding, finding-1, "Show post totals on mobile forum rows", so `terva-review/code` is failure. CI run #665 on the same head passed.

Disposition: declined, maintainer acceptance pending. It is recorded on PR #115 as https://git.local.sothr.com/terva-sh/ketju/pulls/115#issuecomment-16742.

The facts are right: the phone rule hides `.forum-activity`, so phones do not show the total. That is the phone design. docs/mockups/MobileFront{Light,Dark}.dc.html show neither a thread total nor a post total, and the same rule already hides the thread total on main. Showing totals on phones would change what a member sees against the mockup, which is a product decision for a person (docs/12 "Gaps the design leaves open").

Under the run brief (trap 9) this is a medium finding answered by argument, so the PR stays open and unmerged for a maintainer. If a maintainer wants totals on phones, the fix is CSS only: un-hide `.forum-activity` in the phone rule and place it in the card grid.

## Summary

GET /forums answers post_count per forum: posts neither deleted nor moved, per the maintainer's profile ruling (TKT-01M3KJJNDC). Migration 0009 adds threads.live_post_count (kept by post_meta triggers) and forum_thread_counts.live_posts (kept by 0007's threads triggers, rewritten), both backfilled and rebuilt by reindex; ForumThreadCounts still scans only forum_thread_counts. The forum index shows 'N posts' under the thread count, as the mockup does. docs/04 and docs/07 updated.
