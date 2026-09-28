---
schema: 3
id: TKT-01M3KB7FXCKRBV62FR6EZXCKP3
title: Stop recovered databases reusing member ids that old posts name
type: spike
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area:db
  - area:ops
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/phase1-run-438e26c1
  branch: run/kb7fxc-reindex-id-floor
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a8a966ba62d2cc42e
  commit: 9b960cf722ef87cf9aa1ba28375bd2f6aee88639
  session: null
  claimed_at: 2026-09-28T19:39:49Z
  expires_at: null
archive: null
created_at: 2026-09-28T06:28:54Z
updated_at: 2026-09-28T19:44:32Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

Found while shipping `ketju reindex` (TKT-01M29C5Y25, Ship ketju reindex for projections).

`users.id` is `INTEGER PRIMARY KEY`, so SQLite gives a new member max(id)+1. Thread logs name authors by member id. After a recovery from a database backup older than the logs, the posts of members who joined after the backup name ids the restored database does not have, and the next members to join are given exactly those ids: old posts then show as written by a stranger. Without any backup, every id is free and every post is misattributed once people sign up again.

`ketju reindex` warns when the database has no members at all, and the README's recovery section states both cases as unsupported. Nothing prevents the reuse.

### Options to weigh

- Reindex raises the next member id above the largest author, mention, or actor id in the logs (for example by inserting and deleting a placeholder row, or by switching the column to AUTOINCREMENT and bumping `sqlite_sequence`). Posts by unknown ids then render as a deleted member rather than a new one.
- Serve refuses to start while the logs name member ids the database lacks, until an operator acknowledges it.
- Keep member ids out of reuse by construction: allocate them like thread ids (time-based), which is a schema change.

Each changes what members see and who a post is attributed to, so it is a product and security decision for a person.

## Acceptance criteria

- [ ] ketju reindex raises the next member id above the largest member id any thread log names (author, mention, and the actor of edit, delete and moderation records)
- [ ] A migration makes users.id AUTOINCREMENT, keeping every row, id and foreign key (PRAGMA foreign_key_check is empty)
- [ ] Members created after a reindex get ids above every logged id; a reindex over a database not behind the logs changes nothing
- [ ] A post whose author id the database lacks renders as a deleted member (deleted-user-<id>), never reattributed
- [ ] README 'Recovering a lost or damaged database' and docs/04 say what now happens, replacing 'unsupported'
- [ ] The e2e seed still works and the log format is unchanged

## Implementation plan

### Settled in grooming

Maintainer decision, 2026-09-28, "Reindex bumps the id sequence" (note on this ticket):
- `ketju reindex` raises the next member id above the largest member id any thread log names, as author, mention or actor, including edit, delete and moderation records.
- A post whose author id the database lacks renders as a deleted member, never reattributed.
- A migration may switch `users.id` to AUTOINCREMENT and set `sqlite_sequence`, so the floor persists. Reindex only ever raises it.
- A reindex over a database that isn't behind the logs changes nothing.
- README "Recovering a lost or damaged database" and docs/04 must say what now happens, replacing "unsupported".

### Approach

1. **Migration 0010, `users.id` AUTOINCREMENT.** Rebuild `users` the way SQLite documents (create `users_new` with the same columns and constraints plus AUTOINCREMENT, copy every row with its id, drop `users`, rename). The copy seeds `sqlite_sequence` with max(id). Every foreign key that names `users (id)` keeps naming it, because the rename targets the old name and nothing referenced `users_new`.
   - A probe showed the rebuild cannot run with foreign keys on, even with `defer_foreign_keys`: the DROP's implicit delete leaves a deferred violation that fails the commit. So the migration runner turns `foreign_keys` off on its pinned write connection before BEGIN (it is a no-op inside a transaction), runs the pending migrations, runs `PRAGMA foreign_key_check` before commit and refuses to commit on any violation, then turns foreign keys back on. A connection whose restore fails is discarded, never returned to the pool. This is SQLite's documented procedure for schema changes, done once in the runner rather than per migration.
   - Rejected: a kept placeholder row at the floor (a visible fake member); a floor setting plus explicit ids in CreateUser (every raw insert bypasses it, and no trigger can assign an INTEGER PRIMARY KEY); `writable_schema` editing (unsupported surgery).
2. **The floor.** `db.RaiseMemberIDFloor(ctx, tx, n)` sets `sqlite_sequence.seq` for `users` to n only when n is above it, and reports the value before and after. Hand-written SQL, as `recordedMigrations` is, because `sqlite_sequence` is not in the schema sqlc compiles.
3. **Reindex.** For each thread, before projecting it, scan every whole frame of its log for member ids: the frame actor of every record (author of ThreadCreated and Post, actor of Edit, Delete, Restore and every thread moderation record, and of advisory or reserved records too), and the mentions of Post and Edit bodies. Damaged frames are not trusted; whole frames of a thread that then fails to project still count, since the log names them. Raise the floor in its own write when the running max goes up, so a run that stops partway has already covered every thread it projected, and serve's catch-up after it cannot hand out a logged id. Report the largest logged id and the floor before and after.
   - `ketju reindex` prints them (text and JSON: `max_member_id`, `next_member_id`, `member_ids_raised`). The zero-members warning stays, reworded: posts now show as a deleted member rather than being given away.
4. **Rendering.** `member.Missing(id)` draws an id the database lacks as the anonymized placeholder does: `deleted-user-<id>`, no display name or avatar. The thread page, thread list and forum tree use it instead of an empty handle.
5. **Tests.** Migration test: 0010 over a populated database keeps every row and id, `foreign_key_check` is empty, FKs still enforced after, and the sequence equals max(id). Runner: a migration that leaves an FK violation is refused and rolled back. Reindex: logs naming ids (author, mention, edit/delete/moderation actor) above the database's max; afterwards new members (CreateUser, as `identity.Register` and admin promote use) get ids above every logged id; a second reindex changes nothing; a database ahead of the logs is not lowered; an old post by a missing id renders as `deleted-user-<id>`. The e2e seed still runs.
6. **Docs.** README recovery section and docs/04 (users, projection tables) say what now happens.

The log format does not change: only the database and the reader change.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T18:06:55Z

### Maintainer decision, 2026-09-28: "Reindex bumps the id sequence"

`ketju reindex` raises the next member id above the largest member id any thread log names: as author, mention or actor, including edit, delete and moderation records. A member who joins afterwards therefore never takes an id an old post already names.

A post whose author id the database lacks renders as a deleted member, as the anonymized placeholder does. It is never reattributed.

To make the floor persist, a migration may switch `users.id` to AUTOINCREMENT and set `sqlite_sequence`. Reindex then only ever raises it. A reindex over a database that is not behind the logs changes nothing.

Name the new behaviour in README "Recovering a lost or damaged database" and docs/04. Replace the "unsupported" wording there with what now happens.
