# 11 — MVP Critical Path

Status: the ticket backlog derived from documents 00 through 10 lives in
`.tickets/`. This document is the reading of it: which epics gate which, what
the shortest honest route to a usable forum is, and what is deliberately not
on that route.

Counts last read 2026-09-15, after Phase 0 closed, against the store. They are written by hand, so
nothing checks them and they rot silently; the first version of this document
was eight tickets and a whole phase out of date before anyone noticed. Re-read
them with `git ticket list --all --json` rather than by counting files, which
misses the status directories.

MVP here means the Phase 1 exit criterion from
[09-roadmap.md](09-roadmap.md): a friend group can hold a conversation on it.
Sign in, read a thread, write a reply, come back and see what is new, on a
phone as much as on a desktop.

## The backlog in one table

166 tickets: 12 epics and 154 children. Every ticket carries an `area:` label,
an `effort:` label of S, M, L, or XL, a phase milestone, and a reference back
to the document section it came from. The nine open questions in
[10-open-questions.md](10-open-questions.md) that carry a decision deadline are
filed as spikes that block the work they gate, so a decision like CBOR versus
JSON blocks the frame codec rather than sitting in a document nobody rereads
before coding.

| Epic | Phase | Effort | What it gates |
|------|-------|--------|---------------|
| `TKT-01M29BKBKP` Thread log storage engine | 0 | XL | Everything that touches post content |
| `TKT-01M29BKBKV` Embedded database and projections | 1 | XL | Every cross-thread query |
| `TKT-01M29BKBKZ` OIDC identity and sessions | 1 | XL | Any signed-in action |
| `TKT-01M29BKBM7` Markdown pipeline and attachments | 1 | L | Reading and writing a post |
| `TKT-01M29BKBMC` JSON API and service layer | 1 | XL | Both clients |
| `TKT-01M29BN367` Preact client, both layouts | 1 | XL | The MVP itself |
| `TKT-01M29BN36Q` Operations: config, CLI, release | 1 | L | Running it at all |
| `TKT-01M29BKBM3` Approval mode and forms | 2 | L | Opening registration to strangers |
| `TKT-01M29BN36J` Moderation and admin | 2 | XL | Running a community |
| `TKT-01M29BN36D` Lenses, unread, search, live | 3 | XL | Daily-driver quality |
| `TKT-01M29BN36W` Non-JS HTML client | 5 | XL | Placeholder, not broken down |
| `TKT-01M29BN37` Later and maybe | later | XL | Placeholder, nothing committed |

Distribution by phase: 25 in Phase 0, 72 in Phase 1, 33 in Phase 2, 20 in
Phase 3, 11 in Phase 4, 1 in Phase 5, 2 filed as later, and 2 carrying no
milestone.

Note that the storage epic's children are not all Phase 0: of 23, 19 are, two
are Phase 2 and two are Phase 4. Phase 0 reaches 25 by adding the epic itself
and five tickets parented elsewhere or not at all — CI, the naming decision,
the CI pin, the instructions-block question, and this document's own refresh.

## Where it stands

25 done, 3 ready, 138 draft.

**Phase 0 is complete.** 24 of its 25 tickets are done and the format was
frozen at version 1 on 2026-09-15 (`TKT-01M29C5Y1E`). The twenty-fifth is the
storage epic `TKT-01M29BKBKP` itself, which blocks on its children and still
owns four of them in Phases 2 and 4 — hard redaction, its offline tool, thread
archival, and split and merge. It stays open because that is the accurate
state, not because anything in Phase 0 is outstanding.

What landed: the format decisions, the frame codec and CRC32C, the record
catalogue, the offset index, the flag bits, the unknown-type partition, tail
recovery, the thread actor with group commit, the concurrent read path, thread
ids and the sharded layout, the open-thread LRU cache, `ketju dump`,
`ketju fsck`, CI, the frame fuzzer, property tests, crash tests, the benchmark,
and the freeze.

The three `ready` tickets are Phase 1 and unstarted — the markdown pipeline,
config loading, and the event bus — and none of them waits on storage. Phase 1
is where the work is now.

## The critical path

Twenty tickets sit on the longest dependency chain from nothing to the MVP.

### Phase 0: storage, ending in a frozen format

```
TKT-01M29BSPD   settle the format decisions (CBOR, 40 byte entry, fsync default)   done
  → TKT-01M29BW9PT   log header, record frame, CRC32C codec                        done
    → TKT-01M29BYYGY   offset index file and in-memory index                       done
    → TKT-01M29BYYGN   record type catalogue and body codecs                       done
      → TKT-01M29C0WD8   thread actor, ordinal assignment, group commit            done
        → TKT-01M29C0WDW   tail recovery and index repair                          done
          → TKT-01M29C0WDJ   concurrent read path with coalesced reads             done
            → property, crash, fuzz and benchmark tests                            done
              → TKT-01M29C5Y1E   freeze the format at version 1                    done
```

The gate is closed. The format is frozen at version 1 and every change from
here is a version bump with a migration path, written down in docs/03 before
it is needed.

The freeze was signed on measurements rather than on confidence, which was the
point of gating it on the benchmark. All four Phase 0 exit targets are met:
append p99 0.98 ms against a 10 ms target, warm random-ordinal read 4.2 us
against 100 us, index rebuild of a 100 MB log 178 ms cold against 2 s. The
fourth, a cold read from a page cache miss, is 136 us and had no numeric
target. Full tables are on `TKT-01M29C2WV6`.

The freeze is the first gate and the only irreversible decision in the plan.
After it, a format change is a version bump with a migration path. Before it,
it is an edit. That is why it depends on the benchmark and not only on the
tests: a format frozen before the numbers are in is a format frozen on hope.

### Phase 1: three tracks, converging

Data. `TKT-01M29BSPE8` driver choice, `TKT-01M29BW9Q1` access layer and
migrations, `TKT-01M29BYYJ8` projection tables, `TKT-01M29C0WF9` event bus,
converging on `TKT-01M29C2WW5` projection writer.

Identity. `TKT-01M29BSPEF` session design, `TKT-01M29C5Y3E` session store,
`TKT-01M29C5Y34` provider registry, `TKT-01M29C7ZZ9` OIDC start and callback,
`TKT-01M29C8005` open-mode registration. `TKT-01M29C7ZZR` holds the single
session-creation chokepoint, which is small, structural, and the reason the
approval-bypass bug in the docs/09 risk table cannot happen here.

Render. `TKT-01M29BW9Q8` pipeline with a renderer version, through quote and
mention extensions, to `TKT-01M29C2WWG` structured extraction.

Then the convergence: `TKT-01M29C5Y3S` server and router, `TKT-01M29C801M`
shared DTOs and golden tests, and the two endpoints that carry the product,
`TKT-01M29CA9AX` thread page and `TKT-01M29CA9B9` create and reply.

Then the client: `TKT-01M29CCC02` scaffold and `TKT-01M29CCC0F` CSS system,
`TKT-01M29CEE4` shell with both navigation layouts, `TKT-01M29CEE6V` thread
page with the unread divider, `TKT-01M29CEE79` composer, and `TKT-01M29CGBV`
the mobile sheet and sticky reply bar.

The second gate is `TKT-01M29CGBY1`, Playwright at 390 px and 1440 px, with
`TKT-01M29CCBYX` serve and graceful shutdown beside it.

## Two things worth arguing about before starting

**Phase 0 is less serial than it looks, but less so than this document first
claimed.** The original reading said five Phase 1 tickets depend on storage.
Counted against the store: seven depend on it directly — the database epic,
the projection writer, the thread page endpoint, create and reply, edit and
soft delete, single post and replies, and `ketju serve` — and 21 depend on it
transitively, because the database epic carries much of the client behind it.

That still leaves 51 of 72 Phase 1 tickets that never reach storage: identity,
config, CI, the markdown pipeline, and the entire client scaffold and shell
can start on day one alongside the format spike. With one developer, Phase 0
is the bottleneck. With two, it is not, and the plan should be staffed
accordingly. The correction weakens the argument by a third; it does not
overturn it.

**Phase 1 carries 72 tickets, which is roughly double what "walking skeleton"
suggests.** This is the largest risk in the breakdown, because a Phase 1 that
grows does not end and the exit criterion never gets tested against real
people. Candidates to push to Phase 2 without touching the criterion:
keyboard shortcuts `TKT-01M29CGBX4`, profile endpoints `TKT-01M29CCBYG`, the
inline reply expander `TKT-01M29CGBW6`, and the flags half of the viewer-state
endpoints. That is roughly a week back for nothing a friend group would miss
in its first month.

What must not be trimmed is the phone layout. docs/09 names "phone layout
treated as an afterthought and bolted on late" as a risk and makes both
layouts a Phase 1 exit criterion, and `TKT-01M29CGBV` and `TKT-01M29CGBY1`
are the tickets that make that binding rather than aspirational.

## What is deliberately off the path

Approval mode and the forms system, all of moderation beyond soft delete,
every lens, cross-forum unread surfaces, search, live updates, attachments,
split and merge, and the whole non-JS client. That is 65 tickets across Phases
2 through 5 that a friend group does not need to hold its first conversation.

Approval mode is the first thing to pick up afterwards, because it is what
turns a forum for people who already know each other into one that can take a
stranger.

## Starting

Promotion is a decision for a person, and the backlog stays out of the ready
queue until somebody makes it. Three tickets are `ready` and unclaimed, all
Phase 1:

- `TKT-01M29BW9Q8` markdown pipeline with a renderer version
- `TKT-01M29BW9QF` config loading with env overrides and file secrets
- `TKT-01M29C0WF9` in-process event bus

None of the three waits on storage, which is the point the staffing argument
above was making: Phase 0 being finished was never what gated them.

Of the six originally listed as having no dependencies, three are now done —
the format decisions `TKT-01M29BSPD`, CI `TKT-01M29BW9QP`, and thread ids
`TKT-01M29BW9PJ`.

`git ticket ready` lists what is startable, and
`git ticket deps ID --transitive` walks the chain for any ticket here.

One caveat when reading this document against the store: the route described
above is prose, not a query. The recorded `depends-on` graph has a longest
chain of eight tickets, because most of the ordering argued for here was never
written down as a dependency link. Trusting `git ticket deps` alone will
understate what has to happen first.
