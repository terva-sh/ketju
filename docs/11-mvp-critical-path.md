# 11 — MVP Critical Path

Status: the ticket backlog derived from documents 00 through 10 lives in
`.tickets/`. This document is the reading of it: which epics gate which, what
the shortest honest route to a usable forum is, and what is deliberately not
on that route.

Counts last read 2026-09-30, after the extended Phase 2 run landed its in-flight work, against the store. They are written by hand, so
nothing checks them and they rot silently; the first version of this document
was eight tickets and a whole phase out of date before anyone noticed. Re-read
them with `git ticket list --all --json` rather than by counting files, which
misses the status directories.

MVP here means the Phase 1 exit criterion from
[09-roadmap.md](09-roadmap.md): a friend group can hold a conversation on it.
Sign in, read a thread, write a reply, come back and see what is new, on a
phone as much as on a desktop.

## The backlog in one table

284 tickets: 12 epics and 272 children. Every ticket derived from the design
documents carries an `area:` label, an `effort:` label of S, M, L, or XL, a
phase milestone, and a reference back to the document section it came from.
Tickets filed later, by runs, reviews and deployments, often do not: on
2026-10-01, 17 had no `area:` label, 43 no `effort:` label, 30 no milestone,
and 40 no reference. The nine open questions in
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

Distribution by phase: 25 in Phase 0, 87 in Phase 1, 82 in Phase 2, 46 in
Phase 3, 11 in Phase 4, 1 in Phase 5, 2 filed as later, and 30 carrying no
milestone. Phase 3 grew from 21 on 2026-10-01, when its run (docs/14) pulled
the settings pair forward from Phase 4, filed the profile screen, the exit
check and its tracking ticket, and placed ten unplaced drafts. Its grooming
then filed two attachment tickets, a CI stall (`TKT-01M3VBWMA8`), the search
results screen and the two unread-list client tickets, and the run placed the
index write-back bug `TKT-01M3VHSYFR` in Phase 3 because it broke the
"never widen access" invariant. One Phase 3 ticket was archived when the
polish grooming merged it into another. During the run it gained three bugs:
a notifications test race (`TKT-01M3W9VRTV`), and two that the exit check
found (`TKT-01M3XNQ772`, `TKT-01M3XP6ZRZ`).

Note that the storage epic's children are not all Phase 0: of 23, 19 are, two
are Phase 2 and two are Phase 4. Phase 0 reaches 25 by adding the epic itself
and five tickets parented elsewhere or not at all — CI, the naming decision,
the CI pin, the instructions-block question, and this document's own refresh.

## Where it stands

242 done, 7 ready, 3 blocked, 27 draft, 5 archived (counted 2026-10-02 with
`git ticket list --all --json`).

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

**Phase 1 is under way.** Its first eight tickets closed in two rounds.

The first three were the markdown pipeline `TKT-01M29BW9Q8`, config loading
`TKT-01M29BW9QF`, and the event bus `TKT-01M29C0WF9`. A fourth, `TKT-01M2JZB0`,
was filed and closed along the way — the group-commit fsync test was passing by
winning a race, and the event bus's own tests made it lose.

Then four more, promoted together: the OIDC provider registry
`TKT-01M29C5Y34`, the rendered-post cache `TKT-01M29C0WF0`, quote syntax
`TKT-01M29C0WEE`, and mention syntax `TKT-01M29C0WEQ`.

Three things from that round are worth carrying forward rather than leaving in
the commit messages:

- The OIDC registry bounds its key-set refreshes. go-oidc refetches whenever a
  token's key id misses its cache, and the key id is a field in a token supplied
  by whoever called the callback endpoint; measured without the bound, 50 forged
  key ids provoked 50 fetches at the identity provider.
- The renderer version is now 3. Quotes bumped it to 2 and mentions to 3, in
  both cases because the pipeline *can* now produce different output, not
  because any existing golden case changed. Neither did.
- `TKT-01M29C0WEQ` is done with its fourth criterion unticked. The autocomplete
  endpoint needs an HTTP layer and a users table, and neither exists yet.

Six more Phase 1 tickets closed on 2026-09-16 and 2026-09-19:
- structured logging;
- the HTTP server and router;
- RFC 9457 problem details;
- security headers and CSP nonces;
- the CSRF posture and write rate limits;
- the shared DTOs with their golden tests.

Two workflow tickets outside any milestone installed targeted Terva reviews.

**Phase 1 was groomed on 2026-09-24 for an autonomous run.** 18 of its 78
tickets are done, 59 are `ready`, and one is a `human-only` draft. With the
user, grooming did the following:
- Took every decision the backlog was waiting on, and recorded each in docs/10
  or in the design document it belongs to.
- Closed four of the five decision spikes. The fifth, the SQLite driver, is
  the run's first ticket, because its criteria need a benchmark.
- Gave every open ticket an implementation plan.
- Rewrote vague criteria into checkable ones.
- Filed what no ticket owned: handle autocomplete, go.mod tidying, the
  real-device check, and the run's tracking ticket.

[12-autonomous-run.md](12-autonomous-run.md) is the procedure, and
`TKT-01M38WZPCW` tracks the run. The five Phase 1 tickets added since the
2026-09-15 count are those four new tickets and the doc-fixes ticket
`TKT-01M29P9RS0`, which moved in from no milestone. Two tickets appeared
outside Phase 1 over the same period, both workflow tickets with no milestone.

**The run reached the Phase 1 exit gate on 2026-09-25.** The Playwright smoke
tests `TKT-01M29CGBY1` and the accessibility pass `TKT-01M29CGBXK` are both
done. Phase 1 now carries 82 tickets:
- 56 done;
- 22 ready: the six Phase 1 epics, which close on their children, the
  tracking ticket, and 15 tasks and chores the gate did not need;
- 4 drafts: the real-device check `TKT-01M38WTKSQ`, which is `human-only`,
  and three the run filed.

The four tickets added since grooming are `TKT-01M3ART992` (posting from the
composer), done, and those three drafts. The run also filed thirteen
follow-ups with no milestone: twelve are drafts and one has since been
archived. Closing the epics and the tracking ticket, running the real-device
checks, and deciding what happens to the 15 remaining tickets belong to a
person (docs/12).

**A continuation run emptied the Phase 1 queue on 2026-09-28.** The user
re-granted the docs/12 merge authority to finish Phase 1, recorded on
`TKT-01M38WZPCW` (PR #94). Three drafts were promoted into the queue at the same
time. Phase 1 still carries 82 tickets:
- 73 done;
- 7 ready: the six Phase 1 epics and the tracking ticket;
- 1 blocked: the ncruces directory-fsync report `TKT-01M392DP19`. A person has
  to post the upstream report, and the workaround stays until a fixed release
  exists;
- 1 draft: the real-device check `TKT-01M38WTKSQ`, which is `human-only`.

Phase 1 still has 82 tickets because two came in and two went out:
- In: the profile-visibility decision `TKT-01M3KJJNDC` and the tag-rules
  decision `TKT-01M3M4KVNW`, both filed into Phase 1 during the continuation.
  `TKT-01M3KJJNDC` is done.
- Out: thread tags on create, `TKT-01M3B12ZZ4`, and `TKT-01M3M4KVNW` both moved
  to Phase 4, where docs/09 lists thread tags. The user chose to defer them.

The continuation also filed three drafts with no milestone:
- `TKT-01M3K9XBJA`, new-thread draft autosave;
- `TKT-01M3KB7FXC`, member ids reused after a database restore;
- `TKT-01M3KFFFJR`, the mark-unread control split out of the read beacon.

The six epics stay open by the user's choice. Each still parents drafts in
Phases 2 to 4, so none has every child done.

**Next, as filed on 2026-09-28.** Three tickets set out what comes after
the run. Two are drafts; the third, the dogfooding deployment, was promoted to
`ready` when the deployment started:
- `TKT-01M3M9S7S2` deployed a local dogfooding instance, now live at
  https://ketju.brokkr.local.sothr.com. It is done, with its phone criterion
  left to the real-device check `TKT-01M38WTKSQ`, which depends on it.
- `TKT-01M3M9R6JT` ran a hardening sweep of seven Phase 1 follow-ups before
  Phase 2 opens registration to strangers. It closed the same day. All seven
  merged under the docs/12 gate the maintainer granted it (PRs #124–#131).
  The tests found no approval bypass. The maintainer settled three questions
  along the way: the quote markup, the member-id floor after a restore, and
  new handles no longer ending in `-`. The sweep filed two drafts with no
  milestone: `TKT-01M3MSFX3S`, reclaiming posts under a lost member id, and
  `TKT-01M3MTQG5A`, Playwright timeouts under runner contention.
- `TKT-01M3M9RR26` grooms Phase 2 for an autonomous run, as Phase 1 was
  groomed. It is Phase 2, which now carries 42 tickets. Grooming began on
  2026-09-28. The three decision spikes are closed: hard redaction, handle
  changes, and whether `auto_approve` providers skip the handle screen. The
  decisions are in docs/03, 05 and 10. A second batch closed two more
  spikes: an archived forum is read-only (`TKT-01M39788AT`), and remote
  images are refused cleanly (`TKT-01M2K1JKS7`). It filed their two builds,
  `TKT-01M3N24RHD` and `TKT-01M3N24RM0`. It folded the duplicate history
  screen `TKT-01M39WZY8C` into `TKT-01M29CP2Q3` and archived it. It moved six
  unmilestoned follow-ups into Phase 2: `TKT-01M3KFFFJR`, `TKT-01M3K9XBJA`,
  `TKT-01M3A054TR`, `TKT-01M39973KG`, `TKT-01M3MTQG5A` and `TKT-01M39WBN4Q`.
  `TKT-01M3MSFX3S` stays unscheduled. The decisions are in docs/04, 07
  and 08.

The deployment found four Phase 1 bugs, all fixed the same day, which brings
Phase 1 to 87 tickets:
- `TKT-01M3MAS08R`: an issuer ending in a slash was refused, and Authentik's
  issuer ends in one (PR #119).
- `TKT-01M3MAS0A3`: a fresh install could not open registration. The
  maintainer chose a `ketju admin setting` command as the fix (PR #120).
- `TKT-01M3MAS0BQ`: `TestHealthzP99` measured the runner under `-race`
  (PR #118).
- `TKT-01M3MFS6KM`: a signed-out reader had no way to sign in. The client
  linked to the unbuilt settings screen; `/auth/signin` now goes straight to a
  lone provider (PR #121).

Phase 1 now stands at 78 done, 7 ready (the six epics and the tracking
ticket), 1 blocked (the ncruces report) and 1 draft (the real-device check).

**Phase 2 closed its exit gate on 2026-09-30.** The run under docs/13, which
the maintainer granted on 2026-09-29, merged every ticket the exit gate
depends on, the server work the run picked up next, and the run's grooming.
Each merge passed the docs/12 gate, and none carried a recorded disagreement. `TKT-01M3NTSCMN` (PR #194) proves the exit criterion
end to end through the real `serve`: a stranger applies, a moderator approves,
and the applicant signs in again as a member. A denied applicant gets no
session, a suspended member is signed out and refused, and an agent posts with
its credential until the credential is revoked. Playwright covers the same
flow at 390 and 1440 px with axe. The run summary on `TKT-01M3NTSCJQ` lists
the criteria merged unticked and the questions left for the maintainer.

On 2026-09-30 the maintainer extended the run to the rest of Phase 2 on the
same terms (docs/13 "The extension of 2026-09-30"), tracked by
`TKT-01M3RQP0VZ`. It merged 30 of the PRs numbered #208–#239 on the gate;
the other two, #213 and #219, were closed after being carried into #221. It then paused
at the maintainer's request. The client's reader bundle budget was raised to
70000 B along the way. At that pause Phase 2 carried 78 tickets: 65 done, 7 ready, 1 in
progress (the tracking ticket) and 5 draft. The seven ready are client work:
edit and delete windows (`TKT-01M3PTT97X`), moderators' post controls
(`TKT-01M3QHKD21`), post history (`TKT-01M29CP2Q3`), mark-unread
(`TKT-01M3KFFFJR`), the link-a-sign-in entry (`TKT-01M3QS0TMV`), sign out
(`TKT-01M3QVWKAG`) and a small agent-limits fix (`TKT-01M3QV39H4`). The drafts
are the two epics and three the run filed: a reply post answered after the
viewer changed (`TKT-01M3SAKPK9`), a flaky vitest file (`TKT-01M3S8Y2NA`) and
an e2e form restore (`TKT-01M3SK2HFE`). The screenshot work filed nine visual
drafts under `TKT-01M29BN367` with no milestone; on 2026-10-01 the maintainer
settled one of them, the post-times spike `TKT-01M3S3ZF48`, as absolute times,
and it is done. The same day the maintainer resumed the run for the seven ready
tickets and the three drafts. It merged #241–#262 on the gate and ended the
same day. Phase 2 closes at 82 tickets, 80 of them done, including the forms
epic `TKT-01M29BKBM3` and the tracking ticket. The moderation epic
`TKT-01M29BN36J` stays open, because four of its children are Phase 4 drafts.
The admin-applications e2e flake `TKT-01M3SZ3GCE` is blocked until it recurs:
it would not reproduce, and CI now prints a failing test's error context so
that the next occurrence carries its evidence (`TKT-01M3TNBKNQ`). The run's
report, with its one open disagreement and its questions, is the summary on
`TKT-01M3RQP0VZ`.

On 2026-10-01 the maintainer granted a Phase 3 run on the same terms, with a
narrower stop rule (docs/14), tracked by `TKT-01M3V3BVPY`. Its 36 tickets
started as drafts and were groomed before any was promoted.

**Phase 3 is complete** as of 2026-10-02. Of its 46 tickets, 44 are done and
one was archived in grooming. The forty-sixth is the epic `TKT-01M29BN36D`,
which stays open: all 24 of its children are done, and closing it is the
maintainer's call. The exit check `TKT-01M3V3CFRE` merged in #351, and CI
runs its named tests in the step "Phase 3 exit tests, by name". The reader
bundle is 79118 B of a 120000 B budget, which the maintainer raised from
70000 B during the run. The run's report is the summary on `TKT-01M3V3BVPY`.
It lists five decisions and one disagreement still awaiting the maintainer,
the criteria left unticked, and the open CI questions.

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
`TKT-01M29CCBYX` serve and graceful shutdown beside it. It closed on
2026-09-25 together with the accessibility pass `TKT-01M29CGBXK`.

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

The ready queue is the plan now. `git ticket ready` lists what can start, and
the tracking ticket `TKT-01M38WZPCW` orders it in waves. The run starts with
`TKT-01M29BSPE8`, the SQLite benchmark, because the whole data track waits on
it. A second lane can start the client scaffold `TKT-01M29CCC02` alongside.

The open question this section used to raise about `TKT-01M29BW9Q1` is
closed. Go reaches the tables through sqlc, and the view queries are
hand-written beside it (04).

`git ticket deps ID --transitive` walks the chain for any ticket. Grooming
added the dependency links that the route above had only argued for in prose,
so the recorded graph now matches it. Its longest chain runs from the SQLite
decision to the Playwright gate.
