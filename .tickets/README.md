# Tickets

This directory is a git-ticket store. Each file is one ticket: Markdown with
YAML frontmatter, meant to be read, edited, diffed, and merged like any other
file in the repository.

A ticket sits in the directory its status implies, so you can see what is worth
looking at without running anything:

    draft/     filed, not yet worth starting
    tickets/   the working set: ready, in-progress, blocked, review
    done/      finished recently, still worth reading
    archive/   retired, swept out of done from time to time

`tickets/` holding the working set alone is the point. A ticket moves at most
three times: when somebody decides it is worth starting, when it is finished,
and when it is archived.

The status in the file wins if the two ever disagree. `git ticket check` reports
a file in the wrong directory and `git ticket check --fix` moves it.

These directories key on status, which leaves nothing to browse by type.
`epics.md` is that view for the one type worth having it: the epics that are
not done or archived, each linking to its file. It is generated, so edit the
tickets rather than the table. `git ticket check` reports it stale whenever the
two disagree and `git ticket check --fix` rewrites it.

A filename is the ticket ID and nothing else, so renaming a title does not break
`git log` on the old path. Moving between these directories does rename the
file, so reach for `git log --follow` when you want a ticket's whole history.

You can edit these files by hand. Run `git ticket check` afterwards, which
reports what a hand edit tends to break: a duplicate ID, a dependency on a
ticket that does not exist, a status outside the set, or leftover merge conflict
markers.

`config.yml` sets defaults and the label vocabulary. Nothing outside this
directory has to be in sync with it, and the tickets are the source for
everything in here: `epics.md` is derived from them and every other file is
written by hand.
