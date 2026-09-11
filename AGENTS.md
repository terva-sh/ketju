<!-- git-ticket:begin -->

## Tickets

Work is tracked as Markdown tickets in `.tickets/`, managed with `git ticket`.
Run `git ticket help` for the full command list.

Every write records who made it, and `--actor` is how you say. Name yourself on
every command that writes, as `agent:tool/session`:

```sh
git ticket note TKT-01M1PQ7T "..." --actor agent:terva/mieli
```

With no `--actor` the store falls back to the first actor in `config.yml`, which
is usually a person. Your notes then arrive signed with their name, and your
claim tells every other agent that a human is holding the ticket. Nothing
downstream repairs it: a commit carries the committer's Git identity while an
actor is a session, so committing collapses every agent that touched the store
into whoever ran `git commit`.

The tool warns on stderr when a write lands that way, naming the actor it used.
That warning is aimed at you, so pass the flag rather than silencing it. The
opt-out it mentions, `defaults.actor` in `config.yml`, belongs to somebody who
works a store alone and is tired of being told. Setting it because a warning is
noisy puts the store back where it started, with your work under a name you did
not pick.

### Finding work

`git ticket ready` lists what is open, unblocked, and has every dependency
closed. That is the queue. `git ticket list --status in-progress` shows what is
already underway, and `git ticket search PATTERN` takes a regular expression
over the title and body when you only roughly know what you are after.

`git ticket list` answers with open work: every status except `done` and
`archived`. Naming a status brings it back, so `git ticket list --status done`
works, and `git ticket list --all` drops the exclusion. Search is the exception
and spans everything, because finding what was already decided means reading a
done ticket.

A short queue does not mean there is little to do. Everything filed lands in
`draft` and stays there until a person promotes it, so
`git ticket list --status draft` is the rest of the backlog, and it is usually
the larger half. Read it before you report that there is nothing to pick up.

Do not promote a draft yourself. Name the ones that look startable, say what
makes each one startable, and let the person you are working with choose.
Promotion is where somebody weighs this work against everything you cannot see,
so an agent that promotes its own next ticket has appointed itself.

Read the whole ticket with `git ticket show ID` before you start, including its
acceptance criteria and its dependencies. `show` is also how you read the
criteria at all: `git ticket ac ID` with no flag is a refusal rather than a
listing.

Anywhere an ID is taken, a unique prefix works, with or without the `TKT-` part,
down to four characters. Do not shorten one yourself. A ULID opens with about
ten characters of timestamp, so tickets filed in the same session are identical
that far in and a prefix that looks distinctive comes back `ambiguous_id`. Copy
the ID from `git ticket list`, which already shortens each row to what resolves
across this store.

Before you change a file, `git ticket files PATH` lists the tickets that
recorded a reference to it. It reports what other agents wrote, so it is only as
complete as they were, and nothing derives it from Git history.

### Doing the work

Work starts from a ticket that is `ready`, and a draft cannot be claimed. If you
were asked to pick up something still in `draft`, that request is the promotion:
run `git ticket status ID ready` first and carry on. One you took off the queue
is already there.

Then `git ticket claim ID`, and `git ticket status ID in-progress`. A claim
records who is working, on which branch, from which commit. It is advisory and
reserves nothing, so it tells another agent what is in flight rather than
locking anything.

Then read the code and write the approach into the ticket with
`git ticket plan ID "..."`. Do that after you claim rather than when you file
the ticket. A plan written before anybody read the code is a guess, and it goes
stale between filing and starting. It replaces rather than appends, so revising
it when the approach changes leaves one plan rather than a stack of them.
Somebody who wants to redirect you reads it before there is code to throw away.

While you work:

- `git ticket note ID "..."` records what the next person will need and does
  not have. A note travels with the ticket, so it reaches whoever picks this up
  after you.
- `git ticket ac ID --check N` ticks an acceptance criterion. N counts checkbox
  lines from one, not array positions.

Leave unticked any criterion you could not satisfy. Nothing reports an empty
box, so an honest one costs nothing, while a tick you did not earn costs the
next reader their trust in every other box on the ticket. Say what stopped you
in a note.

Finish with `git ticket summary ID "..."` saying where it landed, then
`git ticket status ID done` and `git ticket release ID`.

`note` appends and `summary` replaces, as `plan` does. So a summary is rewritten
by setting it again, while a note you got wrong stays where it is: correct it by
adding another that says which one it supersedes.

If you cannot proceed, `git ticket status ID blocked --reason "..."`. The
reason is required, because a blocked ticket that does not say why tells the
next person nothing.

### When the store check fails

CI verifies the store with `git ticket check --fix --dry-run --strict`. That
plans every repair, prints what it would do, writes nothing, and exits 1 when
one is pending. A ticket under the wrong filename, a ticket in the directory its
status does not imply, and a stale `.tickets/epics.md` all land there.

Run `git ticket check --fix` and commit what it changed. CI reports the repair
and never commits it for you, the same way it reports unformatted code rather
than reformatting it behind your back.

### Filing new work

`git ticket create --title "..." --type bug --priority high` files a ticket.
Types are task, bug, chore, spike, and epic. Add `--description`, `--label`,
and `--assignee` as they apply, and `--parent` to file it under an epic.

Run `git ticket config` before you invent a label. It prints the labels and
milestones this store permits, and a label outside that set is `label_unknown`,
which is a warning that fails `check --strict`. When `enforced` is false nothing
is listed and any value is fine. `git ticket schema` is the other half, the
types, priorities and statuses that every store shares.

Write a description longer than a line to a file and pass the file. Every
command that takes prose will read it: `--description-file` and `--plan-file` on
`create`, `--description-file` on `update`, and `--file` on `plan`, `note`,
`comment`, and `summary`. A path of `-` reads stdin. Passing a paragraph as one
shell argument puts an apostrophe and a backtick between you and what you meant
to write, and a file has neither problem.

Write subheadings inside that prose as `###`. A line opening with `## ` ends the
section and starts a new one, so everything below it lands somewhere you did not
intend. The command warns on stderr when it sees one, and the write still
happens, so read the warning rather than the exit status.

When you file one wrong, `git ticket remove ID` deletes it and you file it
again. Repairing it in place does not work, because `update --description`
replaces the description alone and the stray sections survive. `remove` refuses
a ticket another one depends on, and a ticket carrying notes, comments, a
summary, or a claim, because that is work somebody did rather than a mistake.
Use `archive` for those. Nothing is staged, so the deletion is a working-tree
change you commit like any other.

It lands in `draft` and it stays there. That keeps something you filed on the
way past other work out of the queue, and the decision to promote it belongs to
a person. File it, say that you filed it, and go back to what you were doing.

When work blocks or belongs to other work, record it rather than leaving it in
prose:

- `git ticket link ID --depends-on OTHER` says this ticket waits on that one. A
  dependency is satisfied when the other is done, or archived out of done.
- `git ticket link ID --ref proposal:name --path docs/plan.md` points at a
  document or an external record.
- `git ticket deps ID` walks the dependencies, `--transitive` follows the
  chain, and `--dependents` walks it backwards to what waits on this one.

Split a ticket rather than growing it. If you find work that is not what the
ticket asked for, file it and link it.

Keep the title under 72 characters. Over that `check` warns, and over 120 the
write is refused. The title is what a person reads instead of the ID, so it has
to say what the work is and it has to fit on a line beside one.
`git ticket schema` prints both numbers.

### Naming a ticket in what you write

When you mention a ticket in prose, put its title beside the ID the first time:

```text
TKT-01M1PQ7T (Build git ticket remove, per plan 9.1)
```

After that, in the same summary or comment or commit message, the bare ID is
enough. The point is to orient a reader once, not to pad every line.

Do this because a ULID is unreadable on purpose. It sorts and it never
collides, and it tells a person nothing. A summary that names three bare IDs
asks whoever reads it to look up three tickets before they know what it says,
and they are usually reading precisely because they were not the one doing the
work.

### Driving it from a script

Add `--json` to any command for one envelope on stdout with a stable error
`code` to switch on. `git ticket schema` prints the legal statuses, types,
priorities, transitions, and codes, so read those rather than hard-coding them.

Three shapes are worth knowing before you parse one. A write answers with
`mutation-result`, whose `ticket` is a `{id, revision}` stub and not the ticket,
so read the body back with `show --json` when you need it. `check` answers with
`errors` and `warnings` as two arrays and no combined `findings` key. Body
sections come back camelCase, as `implementationPlan` and `acceptanceCriteria`.

Every write takes `--if-revision R` and refuses if the ticket moved since you
read it. Pass it whenever you read, decide, and then write, which is most of
what an agent does.

Text that opens with a dash goes after a bare `--`.

<!-- git-ticket:end -->
