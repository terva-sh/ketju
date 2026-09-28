---
schema: 3
id: TKT-01M3KB7FXCKRBV62FR6EZXCKP3
title: Stop recovered databases reusing member ids that old posts name
type: spike
status: done
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
claim: null
archive: null
created_at: 2026-09-28T06:28:54Z
updated_at: 2026-09-28T19:57:51Z
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

- [x] ketju reindex raises the next member id above the largest member id any thread log names (author, mention, and the actor of edit, delete and moderation records)
- [x] A migration makes users.id AUTOINCREMENT, keeping every row, id and foreign key (PRAGMA foreign_key_check is empty)
- [x] Members created after a reindex get ids above every logged id; a reindex over a database not behind the logs changes nothing
- [x] A post whose author id the database lacks renders as a deleted member (deleted-user-<id>), never reattributed
- [x] README 'Recovering a lost or damaged database' and docs/04 say what now happens, replacing 'unsupported'
- [x] The e2e seed still works and the log format is unchanged

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

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T19:57:42Z

### Build notes

- **Why the runner changed.** A probe showed that SQLite's table rebuild cannot run inside a transaction with foreign keys on. Even under `PRAGMA defer_foreign_keys = ON`, the implicit delete that `DROP TABLE users` performs leaves a deferred violation, and the commit fails. `PRAGMA foreign_keys` is a no-op inside a transaction. So `DB.migrate` now pins the write connection, turns foreign keys off before BEGIN, runs `PRAGMA foreign_key_check` before COMMIT when it applied anything, and turns foreign keys back on. If the restore fails, the connection is discarded. This is SQLite's documented procedure. Doing it once in the runner is less surgery than special-casing one migration.
- **Rejected alternatives for the floor:** a placeholder row kept at the floor would show a fake member. A floor setting plus explicit ids in `CreateUser` would be bypassed by every raw insert, and no trigger can assign an INTEGER PRIMARY KEY. Editing the schema with `writable_schema` is unsupported surgery.
- **How the ids are found.** Reindex reads each log a second time, before projecting it. It collects every whole frame's actor and the mentions in Post and Edit bodies. It does not hook the projection, because a thread that fails to project, or a reserved record the projection skips, still names ids. A damaged frame's bytes are not trusted. An id above the int64 range cannot be a member id and is left out. The floor is raised thread by thread in its own small write, so a partial run followed by serve's catch-up cannot hand out a logged id.
- **Rendering.** `member.Missing(id)` returns `{handle: deleted-user-<id>, role: member, kind: human}`, the shape an anonymized account's author block has (profile_test). The thread page, thread list and forum index use it where they drew an empty handle before. The user profile endpoints look accounts up by handle, so they never meet a missing id. A `deleted-user-<id>` profile link for a missing id answers not-found.
- **Verification.** Mutations are listed in the code commit's body. Each one was caught. `TestHandleRuleMatchesTheSchema` passes after rebasing onto 9c415ec (PR #130), and 0010's handle CHECK is byte-identical to 0002's. To check the e2e seed, the `-tags e2e` binary was built and `ketju e2e` was run. It seeded, served `/healthz` ok, and `/api/v1/forums` drew the seeded authors as real members. Chromium is not installed here, so Playwright itself runs in CI's step.
- **Left for a person:** a member who joined after the backup registers again under a new id, and nothing links them to their old posts. Filed as draft TKT-01M3MSFX3S (Let a restored member reclaim posts under an id the database lacks).

## Summary

Built to the maintainer's decision. Migration 0010 makes users.id AUTOINCREMENT. The migration runner now runs with foreign keys off and checks them before commit. ketju reindex raises sqlite_sequence above every member id any log names, as actor, author or mention, and never lowers it. It reports the change as max_member_id, member_id_floor and member_id_floor_was. An author the database lacks renders as deleted-user-<id>. README and docs/04 describe the recovery behaviour. Follow-up draft: TKT-01M3MSFX3S.
