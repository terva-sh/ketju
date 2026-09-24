---
schema: 3
id: TKT-01M29BYYHQ7PTYNYA3VBBD4PDB
title: "Add the viewer-state schema: reads, watches, notifications, drafts"
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:db
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29BW9Q17KEBMHJ91G6QHW36
blocks_on: none
references:
  - ref: design:data-model-viewer
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:13:37Z
updated_at: 2026-09-24T09:06:57Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

`read_positions` as one row per user and thread ever visited, `watches` over threads or forums at level watch or mute, `notifications` with the six kinds from docs/04, and `drafts` keyed on user and thread.

Read tracking is one integer per pair, not a row per post. Combined with `threads.post_count` that yields unread counts, and `users.mark_all_read_at` covers every thread the user never opened so those need no rows at all. That decision is why unread tracking stays cheap at 1000 users and 10000 threads, and it is worth stating in the schema comments so nobody later adds a per-post table.

## Acceptance criteria

- [x] Read positions are one row per (user, thread) with a primary key that enforces it
- [x] A drafts row is unique per (user, thread), with a null thread meaning a new-thread draft under a forum
- [x] Notification kinds are constrained to the documented set
- [x] Unread counts for a user with no read_positions rows resolve correctly from mark_all_read_at alone

## Implementation plan

Revised at claim (2026-09-24), after reading the code as it stands.

### Approach
1. `users.mark_all_read_at` already exists (migration 0002), so no ALTER is needed.
2. Migration `0005_viewer_state.sql` adds `read_positions`, `watches`, `notifications` and `drafts`.
   - These are authoritative tables, so they carry foreign keys to users and forums.
   - `thread_id` is a plain INTEGER, because threads is a projection that reindex empties.
3. `read_positions` is WITHOUT ROWID with PRIMARY KEY (user_id, thread_id). The schema comment carries the "no per-post table" rule.
4. `watches` holds exactly one of thread or forum, and a partial unique index covers each.
5. `notifications` has a CHECK over the six kinds, with `domain.NotificationKinds` as the same list in Go. It is indexed on (user_id, read_at, at).
6. `drafts` holds exactly one of thread or forum, and `reply_to` only with a thread. Two partial unique indexes replace docs/04's nullable primary key, as settled in grooming.
7. Queries: GetReadPosition, UpsertReadPosition (which never moves back), ThreadWatchLevel, and UnreadCount.
   - UnreadCount counts post_meta rows past the read position and after mark_all_read_at. That is exact, because post_meta now exists (TKT-01M29BYYJ8).
   - The "no row" rule is docs/04's "everything before this date is read", applied per post.
8. docs/04:
   - Records the unread rule, the watch rule, and the drafts keys.
   - Replaces the `thread_projection` wording.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T09:06:57Z

Mutation check: 16 mutants, all killed. Two survived the first round and each exposed a weak fixture. The reply_to-without-thread case was refused by a unique index before its CHECK was reached; it now uses a fresh forum. The unread test had no other user's read positions, so a join that ignored user_id went unseen; it now has one. The other mutants were: a widened read_positions key, each drafts and watches index dropped or made non-unique, each either/or CHECK dropped, an extra notification kind or watch level, a read position moving backwards, last_read_at not updated, >= at the mark boundary, the mark or position ignored, and the Go kind list short by one. Open question for TKT-01M29CA9CE: drafts has no title column, which docs/04 does not list; noted there.

## Summary

Landed migration 0005 with four tables: read_positions (one row per user and thread, enforced by the primary key), watches (thread or forum, one of each per user), notifications (a CHECK over the six kinds, mirrored by domain.NotificationKinds), and drafts (one per thread, and one new-thread draft per forum, through partial unique indexes).

Queries: GetReadPosition, UpsertReadPosition (forward-only), ThreadWatchLevel, and UnreadCount.

The unread rule is documented in docs/04. A post is unread when it is past the read position and was written after mark_all_read_at. A user with no read_positions rows is therefore answered from mark_all_read_at alone.

16 mutants, all killed.
