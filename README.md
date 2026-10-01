# Ketju

A small-community message board in the spirit of phpBB, rebuilt as a single
static Go binary with a Preact client: linear threads with non-tree
"lenses", threads stored as append-only logs with offset indexes, SQLite
(pure Go) for coordination only, OIDC-first identity with an approval mode.
Humans and agents participate as members on equal terms.

**Status: Phase 1, under way.** The thread log storage engine is done and its
on-disk format is frozen at version 1. The HTTP server, the markdown pipeline,
OIDC provider discovery, and the API's error and DTO contracts are in place.
The database, sign-in, the API endpoints, and the Preact client are the work in
progress; [docs/11-mvp-critical-path.md](docs/11-mvp-critical-path.md) says
what remains and in what order. Start with
[docs/00-overview.md](docs/00-overview.md).

**One process per data directory.** Ketju runs as a single process. `serve`
takes a lock file in the data directory, and a second `serve` against the same
directory refuses to start. There is no horizontal scaling and none is
planned; put a reverse proxy in front for TLS. The reasoning is in
[docs/02-architecture.md](docs/02-architecture.md).

## First run

A fresh install admits nobody. Registration defaults to `approval`, and this
version has no approval queue yet, so a new visitor who signs in is told that
registration is not open. That includes the first admin: `bootstrap.admin_emails`
and `bootstrap.admin_subjects` apply only to someone who can register. Open
registration once, from the command line:

1. Write `config.toml` with `data_dir`, `site_url`, at least one
   `[[oidc.providers]]` entry, and your own address in
   `bootstrap.admin_emails`. Check it with `ketju config check`.
2. Start `ketju serve` once, so that it creates the data directory and its
   database, and then stop it.
3. Open registration:

   ```sh
   ketju admin setting set --root DATA_DIR registration.mode open
   ```

   It refuses while `serve` runs. `ketju admin setting list --root DATA_DIR`
   shows every setting, its value, and what it takes.
4. Start `ketju serve` again and sign in. You register, choose a handle, and
   become the admin. If your provider does not assert a verified email,
   use `ketju admin promote --root DATA_DIR HANDLE` instead.
5. To close registration after your members have joined, stop `serve` and
   run `ketju admin setting set --root DATA_DIR registration.mode approval`.

`ketju admin setting -h` says how each kind of value is written.

## Recovering a lost or damaged database

The thread logs under `threads/` hold every thread and post. The database
holds two kinds of data. Members, sign-ins, groups, permissions, read
positions and settings exist only in the database. Its projection tables are
derived from the logs. So recovery takes the members from a database backup
and the content from the logs, never from the backup. Until `ketju backup`
arrives, take backups as
[docs/02-architecture.md](docs/02-architecture.md) describes: `rsync` the
`threads/` and `blobs/` directories, and snapshot the database with
`VACUUM INTO`.

1. Stop `ketju serve`. Reindex refuses to run while the server holds the data
   directory.
2. Set `KETJU_DATA` to the data directory and `KETJU_BACKUP` to the latest
   database backup. Then run:

   ```sh
   cd "$KETJU_DATA"
   mv db db.damaged
   mkdir db
   cp "$KETJU_BACKUP" db/ketju.db
   ketju reindex --root . --projections --search
   ketju fsck --root .
   ```

   `mv` moves the damaged database out of the way together with its `-wal`
   and `-shm` files, which must not be paired with the backup. If `db/` is
   gone entirely, leave out the `mv` line. `ketju reindex` empties the
   restored database's projection tables and search index, which are as old
   as the backup, and rebuilds both from the logs, so posts written after the
   backup come back and can be found. It reports its progress on stderr while
   it runs. Without `--search`, `serve` rebuilds the search index in the
   background instead, and search says it is incomplete until it is done.
   `ketju fsck` then checks every log.
3. Start `ketju serve`, and delete `db.damaged` once the forum is working.

If reindex exits 1, a thread's log cannot be projected: it is damaged, or its
records contradict each other. Every other thread is projected. The report
names each failed thread and the reason, and `ketju fsck` details any damage.
`ketju reindex -h` lists the other exit codes.

What the database held after the backup was taken is lost: members who
joined since, sessions, read positions, and permission changes. The posts of a
member who joined since come back naming a member id the restored database
does not have. Those posts show as written by a deleted member, as an
anonymized account's do (`deleted-user-<id>`), and are never attributed to
anyone else. Reindex raises the next member id above the largest member id
any log names, as author, mention, or the actor of an edit, a delete or a
moderation record. So a member who joins later never takes the id of an old
post's author. The report ends with a `member ids:` line saying whether
reindex raised the next id and to what. Over a database that is not behind
the logs, reindex leaves the next id as it is. A member who joined since the
backup and signs in again gets a new account with a new id. Their old posts
still show as a deleted member, and nothing yet links the new account to
them.

Without any backup, reindex still restores every thread, but the database has
no members, and reindex warns about this. Every post shows as written by a
deleted member. Members who sign up again get new ids above every id the logs
name.

## Removing a post or a title for good

Deleting a post, or a moderator hiding it, leaves its text in the thread log.
For a legal takedown or an erasure request, an admin removes the text itself
with `ketju redact`, with the server stopped:

```sh
ketju redact --root . --operator HANDLE --reason "court order 17" THREAD ORDINAL
ketju redact --root . --operator HANDLE --reason "erasure request" [--title TEXT] THREAD title
```

THREAD is the thread's id as its URL spells it. The first form removes every
revision of the post, and of every copy a split or merge made of it, and
deletes any attachment no other post names. The post stays at its number as
a tombstone, to moderators too. The second form removes every title the
thread has had and renames it to "Title removed", or to `--title`. Quotes of
the post in other posts are listed and left alone; redact them separately if
the takedown covers them. Add `--dry-run` to see the plan first.

The command writes `redaction.journal` in the data directory before it
changes anything, and `ketju serve` will not start while that file exists. If
the command is interrupted, finish it with:

```sh
ketju redact --root . --resume
```

Backups and filesystem snapshots taken before the redaction still hold the
text. The command cannot reach them. `ketju redact -h` lists the exit codes.

## The name, and where this lives

*Ketju* is Finnish for "chain", and it is the ordinary Finnish word for a forum
thread. Both readings are meant. A thread is a chain of posts, and a thread log
on disk is an append-only chain of framed records.

Development happens on the internal forge, which is `origin`:

```
ssh://git@git.local.sothr.com:2222/terva-sh/ketju.git
```

Release commits are pushed to a public mirror at `github.com/terva-sh/ketju`.

The Go module path is `github.com/terva-sh/ketju`, naming the mirror rather than
`origin`, because that is the path an importer resolves. **The module path and
`origin` are supposed to disagree. Do not change either one to match the
other.**

| Doc | Contents |
|-----|----------|
| [00-overview](docs/00-overview.md) | Vision, principles, glossary |
| [01-research](docs/01-research.md) | Verified ecosystem and UX research, with decisions |
| [02-architecture](docs/02-architecture.md) | Process layout, packages, request flows |
| [03-thread-storage](docs/03-thread-storage.md) | Append-only thread logs and offset index |
| [04-data-model](docs/04-data-model.md) | What the database holds and why |
| [05-identity-and-accounts](docs/05-identity-and-accounts.md) | OIDC, sessions, approval mode, forms |
| [06-thread-views](docs/06-thread-views.md) | Linear thread and its lenses |
| [07-api-and-clients](docs/07-api-and-clients.md) | JSON API, Preact client, non-JS path |
| [08-moderation](docs/08-moderation.md) | Moderation primitives |
| [09-roadmap](docs/09-roadmap.md) | Phased plan |
| [10-open-questions](docs/10-open-questions.md) | Deferred decisions |
| [11-mvp-critical-path](docs/11-mvp-critical-path.md) | The ticket backlog in `.tickets/`, and the shortest route through it to a usable forum |
| [mockups/](docs/mockups/) | Front page and thread page mockups, desktop (1440) and mobile (390), light and dark. Regenerate with `python3 docs/mockups/generate.py`. Published canvas: https://claude.ai/code/artifact/8feff20e-cd58-4031-bc63-9277da945285 |

Design overview page (shareable summary of these docs): https://claude.ai/code/artifact/0838f811-4461-4fe3-81b7-dcded7352015
