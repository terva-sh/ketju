---
schema: 3
id: TKT-01M2HTEXT3VSKJX73EG7VYG6WK
title: Decide whether AGENTS.md carries the short git-ticket instructions block
type: spike
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area:ops
  - area:docs
  - effort:S
assignees: []
milestone: phase-0
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:agents-instructions
    path: AGENTS.md
claim: null
archive: null
created_at: 2026-09-15T06:00:56Z
updated_at: 2026-09-15T06:20:50Z
created_by:
  id: agent:claude-code/ci
  name: ""
updated_by:
  id: agent:claude-code/ci
  name: ""
extensions: {}
---

## Description

`AGENTS.md`: decide whether to adopt the short form of the generated
git-ticket instructions block.

v0.18.0 split the block into a short and a long form. `instructions --core`
now emits 132 lines against the 211 currently installed here, and the long
form remains available at `git ticket instructions` for an agent that wants
the reasoning behind a rule. The wording differs throughout, not only in
length: the short form leads with the failure each rule prevents.

This is a judgement call about what an agent reads first in this repository,
not a mechanical upgrade, which is why it is filed rather than applied. The
decision is whether AGENTS.md should carry the short form and let agents reach
for the long one, or keep the fuller block inline.

## Acceptance criteria

- [x] A decision is recorded on this ticket, with the alternative and why it lost
- [x] If the short form is adopted, AGENTS.md is regenerated with git ticket instructions --write and the diff reviewed rather than taken on trust

## Notes

**agent:claude-code/ci** at 2026-09-15T06:14:38Z

Applied `instructions --write` from v0.18.0 on a branch and diffed it: 211
lines become 132, 83 inserted against 162 deleted. `--write` installs the
short form; the long form stays at `git ticket instructions` and is 281 lines.

What the short form adds that this repository wants:

- The `section_heading_demoted` rule, which is the fix from TKT-01M2HEEX that
  this store was pinned to CI for. It states outright that `### Acceptance
  criteria` is prose no command can reach, and to use `--ac` and `--dod`.
- A pointer to the long form, with the advice to read it when a rule surprises
  you or you are about to work around one.
- Why `--actor` matters: without it the write is attributed to the first actor
  in config.yml, usually a person, so your claim tells other agents a human
  holds the ticket.
- That a request to pick up a draft is itself the promotion.
- That an unticked criterion costs nothing and an unearned tick costs the
  reader their trust in every other box.

What it drops. All of it survives in `--full`, so nothing is lost outright,
but two of these are things an agent gets wrong SILENTLY, where a pointer to
the long form does not help because you do not know to follow it:

- `git ticket list` excludes done and archived; `--all` drops the exclusion.
  Measured here: 150 against 166. This is the exact trap that let docs/11
  claim 155 tickets against the real count for a whole phase.
- `archive` versus `remove`, and which tickets must be archived rather than
  deleted. Dropping it leaves the destructive verb documented and the safe one
  not.
- `link --ref`, which exists and works: `--ref commit:560b01c` is accepted,
  with an optional `--path`. The short form documents only `--depends-on`.
- `deps --transitive` and `--dependents`.
- That a dependency is satisfied only at done, or archived out of done.
- Milestones.

Also found: AGENTS.md here is 211 lines and the generated block is all 211 of
them. This repository carries no ketju-specific agent guidance at all, which
the global agent-kit directive expects it to ("adds what is specific to that
repository"). Verified that `--write` rewrites only between the markers and
leaves content above and below in place, so a repo section can live beside the
generated block and survive regeneration.

## Summary

Adopted the short form and added a repository section beside it. AGENTS.md
goes from 211 lines, all of them generated, to 132 generated plus 56 specific
to ketju.

The short form wins on what it adds: the `section_heading_demoted` rule from
TKT-01M2HEEX, a pointer to the long form and when to follow it, why `--actor`
matters, and that a request to pick up a draft is the promotion. It is also
the artifact the tool maintains, so keeping the old block meant drifting from
its generator.

The alternative, taking the short form alone, lost. It drops six things, all
recoverable from `git ticket instructions --full`, but two of them are
mistakes an agent makes silently, where a pointer to the long form cannot help
because nothing signals that it is needed: that `list` excludes done and
archived, and that `archive` keeps what `remove` destroys. The first one had
already cost this repository a whole phase of drift in
docs/11-mvp-critical-path.md.

Keeping the old 211-line block also lost, on the same reasoning plus the loss
of the heading rule CI was just pinned for.

The repository section carries those six omissions, the facts about working
here that no generated block can know — main is protected, the required status
is event-suffixed, the statuses endpoint names the field `status` and not
`state`, a green summary is not evidence the step ran — and the reminder that
docs/11 is hand-written and needs re-reading whenever the backlog moves.

Verified that `--write` rewrites only between the markers: content above and
below survives in place, and a second `--write` over the finished file changed
nothing.
