<!-- git-ticket:begin -->

## Tickets

Work is tracked as Markdown tickets in `.tickets/`, managed with `git ticket`.
`git ticket help` lists every command.

`git ticket instructions` prints the long form of this block, carrying the same
rules with the reason for each one and the failure it prevents. Read it when a
rule here surprises you, when you are about to work around one, or when you want
the parts this summary leaves out.

Name yourself on every command that writes, as `agent:tool/session`. Without it
the write is attributed to the first actor in `config.yml`, usually a person, and
your claim then tells other agents that a human is holding the ticket.

```sh
git ticket note TKT-01ABCD "..." --actor agent:yourtool/session-3
```

### Finding work

`git ticket ready` is the queue: open, unblocked, every dependency closed.

`git ticket list --status draft` is the rest of the backlog and is usually the
larger half, because everything filed lands in `draft` and stays there until a
person promotes it. Read both before reporting that there is nothing to pick up.

Do not promote a draft yourself. Name the ones that look startable, say what
makes each startable, and let the person you are working with choose.

`git ticket show ID` reads one ticket. It prints the newest note in full and
replaces older ones with a line naming how many there are; `--list` and
`--show N` on the `note` command print those.

`git ticket search QUERY` matches a substring of the title, the body, and the
references. Add `--regex` to search by pattern instead.

Any unique ID prefix works, down to four characters. Copy one from a listing
rather than shortening it yourself, because a ULID opens with a timestamp and
tickets filed together are identical that far in.

### Doing the work

A draft cannot be claimed. If you were asked to pick up something still in
`draft`, that request is the promotion, so run `git ticket status ID ready`
first. A ticket already on the queue needs no such step.

Then `git ticket claim ID` and `git ticket status ID in-progress`.

Read the code before you plan, then write the approach with
`git ticket plan ID "..."`. It replaces rather than appends, so revising it
leaves one plan instead of a stack.

While you work, `git ticket note ID "..."` records what the next person will
need and does not have, and `git ticket ac ID --check N` ticks an acceptance
criterion, counting checkbox lines from one.

Leave unticked any criterion you could not satisfy, and say in a note what
stopped you. Nothing reports an empty box, so an honest one costs nothing, while
a tick you did not earn costs the next reader their trust in every other box.

Finish with `git ticket summary ID "..."` saying where it landed, then
`git ticket status ID done` and `git ticket release ID`.

If you cannot proceed, `git ticket status ID blocked --reason "..."`. The reason
is required.

`note` appends. `plan` and `summary` replace, so correct a note by adding another
that says which one it supersedes.

### Filing new work

`git ticket create --title "..." --type bug --priority high` files a ticket.
Types are task, bug, chore, spike, and epic. Add `--parent` to file it under an
epic.

Run `git ticket config` before you invent a label. It prints what this store
permits, and a label outside that set is a warning that fails
`check --strict`. `git ticket schema` prints the types, priorities, statuses and
error codes every store shares.

Write prose longer than a line to a file and pass the file: `--description-file`
on `create` and `update`, and `--file` on `plan`, `note`, `comment`, and
`summary`. A path of `-` reads stdin.

Inside that prose write subheadings as `###`, because a line opening with `## `
starts a new section and everything below it lands somewhere you did not intend.
The exception runs the other way. If the subheading names a section the format
owns, `### Acceptance criteria` is prose no command can reach. Use `--ac` and
`--dod`, or write `## `, which opens the real section. `check` reports the
mistake as `section_heading_demoted`.

A ticket you file lands in `draft` and stays there. File it, say that you filed
it, and go back to what you were doing.

Record structure rather than describing it in prose:
`git ticket link ID --depends-on OTHER` says this waits on that, and
`git ticket deps ID` walks the chain.

Keep the title under 72 characters. Over that `check` warns, and over 120 the
write is refused.

When you mention a ticket in prose, put its title beside the ID the first time,
then the bare ID is enough. A ULID tells a reader nothing on its own.

### When check fails

`git ticket check --fix --dry-run --strict` plans every repair, prints what it
would do, and writes nothing. Run `git ticket check --fix` and commit what it
changed.

If this project runs the check in CI, it reports the repair and does not commit
it for you.

### Driving it from a script

`--json` on any command gives one envelope on stdout with a stable error `code`
to switch on. Read the vocabulary from `git ticket schema` rather than
hard-coding it.

A write answers with `mutation-result`, whose `ticket` is an `{id, revision}`
stub rather than the ticket, so read the body back with `show --json` when you
need it. Body sections come back camelCase, as `implementationPlan` and
`acceptanceCriteria`.

Every write takes `--if-revision R` and refuses if the ticket moved since you
read it. Pass it whenever you read, decide, and then write.

Text that opens with a dash goes after a bare `--`.

<!-- git-ticket:end -->

## Working this repository

Ketju is a single static Go binary and a Preact client. `go.mod` declares Go
1.26.2 and the module is `github.com/terva-sh/ketju`. `docs/00` through
`docs/11` are the design, and a ticket almost always names the section it came
from; read that section before planning. `internal/` is the implementation and
`spikes/` holds throwaway programs that must still build.

### `main` is protected

Direct pushes are rejected. Branch, open a pull request, and merge that.
`ci / Build and Test (pull_request)` is the required status, and Forgejo
suffixes a status context with the event that produced it, so the `(push)` run
on the same commit does not satisfy it.

Before deleting a branch, confirm the pull request actually merged rather than
that the merge call returned. A refused merge answers a bare HTTP 405 with no
JSON body, and 405 is sometimes transient and worth retrying.

When you poll a commit's statuses, the field is named `status`. GitHub calls it
`state`, and reading `state` here returns null for every row, which ranks as
pending and leaves a wait loop watching a build that finished. The list is an
event log rather than a state table, so rank by status and let failure win ties
instead of taking the newest row.

A green run summary is not evidence the work ran. Read the step log.

### Things the generated block above leaves out

`git ticket list` answers with open work: it excludes `done` and `archived`.
`--all` drops the exclusion, and naming a status brings that one back. Count
with `--all` or report a number that is quietly short — that is how
`docs/11-mvp-critical-path.md` came to claim 155 tickets against 166.

`remove` deletes; `archive` keeps. Archive anything another ticket depends on,
or that carries notes, comments, a summary, or a claim, because that is work
somebody did rather than a ticket filed by mistake.

`git ticket link ID --ref R` records a reference as well as a dependency, with
an optional `--path`: `--ref commit:8bd18b7`, or `--ref design:thread-storage
--path docs/03-thread-storage.md`. `git ticket deps ID` takes `--transitive` to
follow the chain and `--dependents` to walk it backwards.

A dependency counts as satisfied only when the other ticket is `done`, or was
archived out of `done`. `ready` does not release it.

`ready` cannot go straight to `done`. Claim the ticket and move it through
`in-progress`, or the transition is refused — which will break a `&&` chain and
skip the commit you expected to follow it.

### Numbers in documents

`docs/11-mvp-critical-path.md` is written by hand and nothing validates it.
If you change the shape of the backlog, re-read it with
`git ticket list --all --json` and update the document in the same commit.
