---
schema: 3
id: TKT-01M29BYYHQ7PTYNYA3VBBD4PDB
title: "Add the viewer-state schema: reads, watches, notifications, drafts"
type: task
status: ready
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
updated_at: 2026-09-24T05:21:19Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

`read_positions` as one row per user and thread ever visited, `watches` over threads or forums at level watch or mute, `notifications` with the six kinds from docs/04, and `drafts` keyed on user and thread.

Read tracking is one integer per pair, not a row per post. Combined with `threads.post_count` that yields unread counts, and `users.mark_all_read_at` covers every thread the user never opened so those need no rows at all. That decision is why unread tracking stays cheap at 1000 users and 10000 threads, and it is worth stating in the schema comments so nobody later adds a per-post table.

## Acceptance criteria

- [ ] Read positions are one row per (user, thread) with a primary key that enforces it
- [ ] A drafts row is unique per (user, thread), with a null thread meaning a new-thread draft under a forum
- [ ] Notification kinds are constrained to the documented set
- [ ] Unread counts for a user with no read_positions rows resolve correctly from mark_all_read_at alone

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. Add migration `NNNN_viewer_state.sql` with the tables `read_positions`, `watches`, `notifications`, and `drafts`, plus `users.mark_all_read_at INTEGER NULL` (ALTER TABLE, if BYYH7 did not already add it). docs/04 names the column under read_positions but leaves it out of the users column list. Pick one owner at claim.
2. `read_positions(user_id, thread_id, last_read_ordinal, last_read_at, PRIMARY KEY (user_id, thread_id)) WITHOUT ROWID`. The schema comment states the one-row-per-pair rule, and warns that a per-post table is the mistake to avoid, per the ticket.
3. `watches(user_id, thread_id NULL, forum_id NULL, level)` has `CHECK (level IN ('watch','mute'))` and `CHECK ((thread_id IS NULL) <> (forum_id IS NULL))`. Add unique partial indexes on (user_id, thread_id) and (user_id, forum_id).
4. `notifications` has `CHECK (kind IN ('reply_to_you','mention','quoted','thread_you_watch','application_decided','new_application'))` and an index on `(user_id, read_at, at)` for the unread list.
5. Drafts: docs/04 declares `PRIMARY KEY (user_id, thread_id)` with a nullable thread_id. In SQLite that allows many NULL rows and cannot key a new-thread draft by forum. Use `UNIQUE (user_id, ifnull(thread_id, 0), ifnull(forum_id, 0))` as an expression index instead, with `CHECK (thread_id IS NOT NULL OR forum_id IS NOT NULL)`. This departs from docs/04's literal PK, so update docs/04 "drafts" in the same commit.
6. Add an unread-count query as a static sqlc query in `queries/viewer_state.sql`. It has no dynamic filter, so it does not belong in `views/`. The query: `post_count - COALESCE(rp.last_read_ordinal, CASE WHEN t.last_post_at <= u.mark_all_read_at THEN post_count ELSE 0 END)`. It joins BYYJ8's `threads`. The rule for a thread partly posted after mark_all_read_at is not spelled out in docs/04. The candidate rule: with no read_positions row, the thread counts as read if last_post_at <= mark_all_read_at, and otherwise every post counts as unread. Record the rule chosen.

### Files and packages
New: the migration, `queries/viewer_state.sql`, the regenerated `dbq/`, and `viewer_state_test.go`. Reuses `DB` from BW9Q1. The unread query depends on the `threads` table, so either land after BYYJ8 or keep the query in a later ticket (TKT-01M29CTY7K). Say which at claim.

### Tests
- AC1: `TestReadPositionOnePerPair` checks that a second insert for the same pair fails, and that the upsert query updates in place.
- AC2: `TestDraftUniqueness` covers a thread draft twice (refused), new-thread drafts in two forums (ok), the same forum twice (refused), and both NULL (refused).
- AC3: `TestNotificationKindsClosed` checks that the six insert and that `'like'` is refused. The Go constant list is compared against the CHECK, the same pattern as BYYHG.
- AC4: a table-driven `TestUnreadFromMarkAllReadAlone` uses a user with zero read_positions rows and threads before and after mark_all_read_at. Expected counts are computed by hand.

### Design
docs/04-data-model.md "read_positions", "watches", "notifications", "drafts", and "Sizing sanity check". docs/06-thread-views.md, for the unread divider semantics the query serves.

### Interactions
Consumed by TKT-01M29CA9CE (viewer-state endpoints), TKT-01M29CRJ2T (notifier), TKT-01M29CRJ29 (watches), and TKT-01M29CTY7K (cross-forum lists). Note that docs/04 calls the table `thread_projection.post_count`, but the table is `threads` (docs/04 "threads"). Fix that wording in the same commit.

### Settled in grooming (2026-09-24)

- One draft per forum for new threads: add an expression index on (user_id, forum_id) WHERE thread_id IS NULL, because a nullable column in a primary key does not enforce uniqueness in SQLite.
