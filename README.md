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

## Installing a release

Each release is a static binary for `linux/amd64` and `linux/arm64`, with a
`SHA256SUMS` file beside them. It is published on the GitHub mirror's
release page (`github.com/terva-sh/ketju/releases`), and on this forge's.
Each forge builds its own, so the two `SHA256SUMS` files may differ; check a
binary against the sums from the same page. Download the binary for your
machine and the sums, check them, and install:

```sh
VERSION=v1.0.0
ARCH=amd64   # or arm64
BASE="https://github.com/terva-sh/ketju/releases/download/$VERSION"
curl -fsSLO "$BASE/ketju_${VERSION}_linux_${ARCH}"
curl -fsSLO "$BASE/SHA256SUMS"
sha256sum -c --ignore-missing SHA256SUMS
install -m 0755 "ketju_${VERSION}_linux_${ARCH}" /usr/local/bin/ketju
ketju version
```

`sha256sum -c` must print `OK` for the file you downloaded; a release is not
signed. `ketju version` names the version and the commit it was built from.
The release notes for each version are in [docs/releases/](docs/releases/).
Then write `config.toml` as "First run" says.

### The container image

The GitHub release's binaries are published as an image,
`ghcr.io/terva-sh/ketju`. It is tagged with the version without its `v`, such
as `1.0.0`; a release that is not a pre-release also moves `1.0` and
`latest`. The image holds the binary, CA
certificates and nothing else. It runs `ketju serve` as uid 65532, reads
`/data/config.toml`, and keeps its data in the `/data` volume, so
`config.toml` sets `data_dir = "/data"` and a `listen` address such as
`":8080"`.

The data directory must belong to uid 65532 as the container sees it. Under
rootless Podman, container uids map to your subordinate uids, so give it to
65532 inside your user namespace with `podman unshare`:

```sh
mkdir data
cp config.toml data/
podman unshare chown -R 65532:65532 data
podman run -d --name ketju -p 8080:8080 -v "$PWD/data:/data" \
  ghcr.io/terva-sh/ketju:1.0.0
```

Under Docker or rootful Podman, container uids are host uids, so
`sudo chown -R 65532:65532 data` takes the place of the `podman unshare`
line, and the `run` line is the same.

The other commands run the same way, with the command after the image name:
`podman run --rm ghcr.io/terva-sh/ketju:1.0.0 version`.

### Publishing a release

This is the maintainer's: pushing a tag publishes, and an agent never does it
(docs/15, stop class 3). Like the other repositories in the `terva-sh`
organization, ketju has two release workflows, one on each forge, and a `v*`
tag such as `v1.0.0` or the pre-release `v1.0.0-rc.1` starts each one when it
reaches that forge. Neither reaches into the other.

`.github/workflows/release.yml` runs on the public mirror, `github.com/terva-sh/ketju`:

1. refuses a tag that is not on the mirror's `main`, or not
   `vMAJOR.MINOR[.PATCH][-PRE]`;
2. builds and checks the binaries with `go run ./tools/release`, as CI's dry
   run does, and writes `SHA256SUMS`;
3. publishes them as a GitHub release, with `docs/releases/<tag>.md` as its
   description, then downloads them anonymously and checks them with
   `sha256sum -c`;
4. builds the image for both platforms from the published binaries, pushes
   it to `ghcr.io/terva-sh/ketju`, and pulls each platform back to check its
   version and its uid.

`.forgejo/workflows/release.yml` runs here. It makes the same checks, builds
the binaries, publishes them as a release on this forge, and checks the
download. It builds and runs the image too, and pushes it nowhere: `ghcr.io`
is the one registry.

A tag with a hyphen is published as a pre-release, and its image moves no
floating tag. Rerunning either workflow is safe: a binary already published
is kept if its bytes match, and the run fails if they do not. An image
already pushed for the version is kept and nothing is pushed over it, so a
rerun does not move `latest` either; if an earlier run stopped before
pushing the floating tags, the job's warning says how to point them at the
version.

The GitHub workflow needs no secret: it uses the `GITHUB_TOKEN` Actions
provides. The Forgejo workflow needs one, which the organization already
holds. It reads it by name and fails, naming it, when it is missing:

| Name | What it is |
|------|------------|
| `BOT_TOKEN` | A Forgejo token with write access to `terva-sh/ketju`; the organization secret `terva-review.yml` also uses |

To release, with a remote named `github` for the mirror:

1. Work through the release checklist for the version, such as
   `docs/releases/v1.0-checklist.md`, and finish its release notes.
2. Tag `main` after its pull request merges, then push `main` and the tag
   to the mirror. The tag must be annotated, because `--follow-tags` pushes
   only annotated tags:

   ```sh
   git checkout main && git pull --ff-only
   git tag -a v1.0.0 -m "ketju v1.0.0"
   git push github main --follow-tags
   ```

   The mirror is a plain push of the same history, and `main` goes there
   only at release time.
3. Push the tag here: `git push origin v1.0.0`.

Try it first with a pre-release tag, and check its downloads on a clean
machine with the commands at the top of this section. A new package on
`ghcr.io` may start private; make it public in the package's settings once.

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

## Backing up and restoring

`ketju backup` writes a consistent copy of the data directory, whether
`ketju serve` is running or not:

```sh
ketju backup --root "$KETJU_DATA" "$KETJU_DEST"
```

`$KETJU_DEST` must not exist yet, and must be outside the data directory.
With `serve` running, the command asks it for a snapshot over the control
socket `ketju.sock` in the data directory, which only the user `serve` runs
as can use. `serve` holds new posts for the moment it takes to bring the
database up to date and copy it, a fraction of a second on a small forum and
never more than a minute, and pages are served meanwhile. With `serve`
stopped, the command takes the data directory's lock instead, so `serve`
cannot start until it has finished. Either way the source is only read. The
backup is checked as `ketju fsck` checks a data directory, and for every
attachment its posts name, before it is given its name, and `ketju backup
-h` lists the exit codes. A database written by another version of ketju is
copied at its own schema and never migrated, so the version that wrote it
can restore the backup.

A backup holds the database, every thread log and every attachment. It
leaves out the thread indexes and thumbnails, which are rebuilt when first
needed, uploads still in progress, `config.toml` and `secrets/`. Keep your
own copy of `config.toml`, and of every `client_secret_file` it names,
which lives outside the data directory. Without the old
`secrets/cookie.key`, only sign-ins in progress at the moment of the
restore are lost.

To restore, copy the backup into an empty data directory and check it:

```sh
mkdir "$KETJU_RESTORE"
cp -R "$KETJU_DEST"/. "$KETJU_RESTORE"/
ketju fsck --root "$KETJU_RESTORE"
```

Then point `data_dir` in your `config.toml` at it, and start `ketju serve`.

### Backing up with your own tools

With `serve` stopped, copying the whole data directory with `rsync` or any
other tool is a consistent backup. With `serve` running, copy in this order:

1. The database, with SQLite's `VACUUM INTO`, for example
   `sqlite3 "$KETJU_DATA/db/ketju.db" "VACUUM INTO '/backups/ketju.db'"`.
   The `sqlite3` build must include FTS5, because the search index is an
   FTS5 table. Copying `ketju.db` itself while `serve` runs is not a backup:
   recent changes may sit in its `-wal` file.
2. The logs: `rsync -a --exclude '*.idx' "$KETJU_DATA/threads/" /backups/threads/`.
3. The attachments:
   `rsync -a --exclude tmp/ --exclude '*.thumb' "$KETJU_DATA/blobs/" /backups/blobs/`.

The logs are copied after the database, so each is at least as long as the
database says, and a blob is written before the post that names it, so
every blob a copied log names is already on disk. The logs usually end up a
little ahead of the database. `ketju fsck` reports that as drift and exits
3, and `serve` closes it when it next starts, so a restored copy passes
`fsck` once `serve` has run on it. Copied in another order, a log can end
up shorter than the database says, and `serve` quarantines that thread.
`ketju backup` has none of these caveats.

## Upgrading

An upgrade replaces the binary. `ketju serve` brings the database's schema
up to date when it starts, applying each migration the new binary has in
one transaction, so a migration that fails leaves the database as it was
and `serve` does not start. The thread logs are never rewritten.

1. Read the release notes in `docs/releases/` for every version after the
   one you run, up to and including the new one.
2. Back up:

   ```sh
   ketju backup --root "$KETJU_DATA" "$KETJU_BACKUP"
   ```

   With `serve` running, this is the `ketju` you run now, asking `serve`
   for a snapshot. If that `ketju` has no `backup` command, stop `serve`
   first and run the new binary's from wherever you downloaded it: with
   `serve` stopped it takes the data directory's lock instead. Either way
   the database is copied at the schema it has and never migrated, so the
   old binary can restore the backup.
3. Stop `serve`, if it is still running, for example with
   `systemctl stop ketju`, and put the new binary where the old one was.
4. Check the new binary and your configuration against each other:

   ```sh
   ketju version
   ketju config check --config "$KETJU_CONFIG"
   ```

   A setting the new version no longer accepts, or reads differently, is
   reported here rather than at startup.
5. Start `serve`. It applies the migrations, catches the database up with
   the logs, and then listens.
6. Check the data directory:

   ```sh
   ketju fsck --root "$KETJU_DATA"
   ```

Going back to the old binary means going back to the backup as well. Once
the new binary has migrated the database, an older binary refuses to start
on it, saying the database's schema is newer than it knows, rather than run
old queries against a schema they were not written for. Restore the backup
as the "Backing up and restoring" section says, and start the old binary on
it. What was posted after the backup is not in it, so decide soon after
the upgrade.

Never run a binary older than v1.0 on thread logs that v1.0 or later has
written once a moderator has split or merged posts. Those binaries do not
know the two move records and skip them, so their `ketju reindex` would
show the moved posts in their old thread again, to that thread's forum's
readers. Going back past v1.0 means the backup taken before the upgrade,
and nothing written since (docs/03 "Reserved record type numbers").

## Recovering a lost or damaged database

The thread logs under `threads/` hold every thread and post. The database
holds two kinds of data. Members, sign-ins, groups, permissions, read
positions and settings exist only in the database. Its projection tables are
derived from the logs. So recovery takes the members from a database backup
and the content from the logs, never from the backup. The database backup
is the `db/ketju.db` of a backup `ketju backup` wrote, or a `VACUUM INTO`
copy ("Backing up and restoring" above).

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

## Metrics

Ketju exports Prometheus metrics on a listener of its own, and only when
`config.toml` names one:

```toml
metrics_listen = "127.0.0.1:9090"
```

or `KETJU_METRICS_LISTEN`. Point the scraper at
`http://127.0.0.1:9090/metrics`. That listener serves `/metrics` and nothing
else, and it asks for no credentials, so where it can be reached from is the
only guard: keep it on a loopback or private address, and do not route it
through the reverse proxy. The forum's own listener answers 404 for
`/metrics`. With `metrics_listen` empty, the default, no metrics are served.

The metrics are request latency, cache hits and misses, fsync latency,
projection lag and the live-update streams.
[docs/02-architecture.md](docs/02-architecture.md) "Observability" lists
each one with its type, unit and labels. `ketju_projection_lag_bytes` is the
one to alert on: when it stays above zero, posts are written but not yet
listed.

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
