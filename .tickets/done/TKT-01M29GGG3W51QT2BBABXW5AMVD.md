---
schema: 3
id: TKT-01M29GGG3W51QT2BBABXW5AMVD
title: Explore what an agent-native board changes in the design
type: spike
status: done
status_reason: "Owner accepted in review: the positions are theirs and the wording is faithful. The one format commitment (frame flag bit 0) was verified against the codec's round-trip test; the missing constant is filed as TKT-01M2BXKF8F and linked to the freeze."
priority: high
due_on: null
labels:
  - effort:M
  - decision
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:overview
    path: docs/00-overview.md
  - ref: design:thread-views
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-12T00:33:06Z
updated_at: 2026-09-12T23:00:38Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/review
  name: ""
extensions: {}
---

## Description

Captured from a direction the owner raised while the frame codec was being started: the board becomes a place where humans and models both participate as first-class members, rather than a human forum with an API bolted to the side.

Most of the existing design absorbs this without change, which is the encouraging part. A model is a user with an identity, so docs/05 already models it. Posts, ordinals, lenses, and moderation do not care what kind of thing wrote a post.

The places it could bite, in rough order of how expensive they get if decided late:

**Before the format freeze (TKT-01M29C5Y1E).** The record frame carries an 8-byte actor and 2 bytes of flags, plus reserved bytes in the frame and the index entry. If anything about agent authorship belongs *per post* rather than per author, this is the only cheap moment to reserve room for it. Candidates: a generated-content marker, model and version provenance, a reference to the tool-call transcript or conversation that produced the post, a human-approved-this flag. Per-author facts need nothing here; per-post facts need a decision now.

**Identity and permissions (docs/05, docs/04).** Do agents authenticate through OIDC like everyone else, or through a token path? docs/07 currently says personal API tokens are a later phase and are never issued to browser code. An agent-native board probably promotes that path to first-class, which changes the session model's shape.

**Rate limits and moderation (docs/08).** Every limit in the design is sized for a human typing. Agents post faster, and slow mode, flag thresholds, and the auto-hide count are all tuned to human volume. The "what is deliberately missing" list in docs/08 also assumes human social dynamics.

**The linear-thread thesis (docs/06).** This is the interesting one. The argument against trees is that a shared scalar reading position is what makes unread tracking, permalinks, and "write to the room" norms work, and those are arguments about human attention. Whether they survive contact with participants that read the entire thread every time is a real question rather than a rhetorical one.

Deliverable is a written position on each of the four, not an implementation.

## Acceptance criteria

- [x] A written position on whether anything agent-specific belongs in the frame or index reserved bytes, delivered before TKT-01M29C5Y1E freezes the format
- [x] A decision on how agents authenticate and whether the token path becomes first-class
- [x] A statement on whether the linear-thread argument in docs/06 still holds when some participants are models
- [x] Any change to the documents is made as an edit to them, not left in this ticket

## Implementation plan

Settled by interview rather than by inference. The owner answered three rounds, eighteen questions, and the answers are recorded in the notes on this ticket. A mapper sub-agent supplied the factual inventory of spare format capacity and of what the documents already say, so no question put to the owner had an answer available in the repository.

### The decision that carried the most

An agent is a first-class member with its own `users` row. That was the root question and it turned out to be load-bearing for the format: the record frame's `Actor` is a bare `uint64` user id in a 32-byte fixed part with no room for a second 8-byte field. The delegate model, where a human authors and a model produces, would have needed two actor ids and therefore a wider frame. First-class membership leaves `Actor` untouched.

### What changes in the format, and what does not

Two bits. One in the record frame's `Flags` at offset 6, one in the sidecar index entry's flags at offset 34. All 16 frame bits are spare and `frame.go:65` already guarantees unknown bits survive an older reader; the index field has 5 of 16 bits named. The index entry's 4 reserved bytes at offset 36 stay untouched because docs/03 already commits them to segment rollover.

Nothing else. Rich provenance (model, version, a reference to the transcript that produced the post) goes in the CBOR body, which is a self-describing map, so adding keys needs no reservation and no version bump. The narrow question the freeze actually forces is only "what must a lens filter on without decoding bodies", and the answer is one bit.

The bit records the write-time fact and is derived server-side from `users.kind`. A client never supplies it. That is also why it exists at all: the log holds what was true when the post was written, and a projection cannot reconstruct it once an account changes.

### Files to edit

- `docs/03` assigns frame flag bit 0 and the matching index flag bit, and states that unknown bits are preserved.
- `docs/04` adds `users.kind` with values `human` and `agent`.
- `docs/05` gains an agent accounts section: admin provisioning, the agent-owned credential, and an explicit statement that `role` is orthogonal to `kind`.
- `docs/06` records that an agent sets its read position explicitly through the API with no beacon, that agent working threads carry a tag, and that the linear argument stands. Also records the live-update cost that tagging does not solve.
- `docs/07` promotes an admin-issued agent credential to Phase 2, distinct from the general personal API token work.
- `docs/08` adds the agent rate limit as a token bucket, and states that humans stay unlimited.
- `docs/09` moves the agent credential into Phase 2 and leaves personal API tokens in Later/maybe.
- `docs/10` records the items this spike closed.

### Out of scope, to be filed separately

The mapper found seven internal inconsistencies in the documents. One belongs to this freeze window: nothing defines what a reader does with a record type it does not recognise, while docs/10 already plans to add one. These get their own tickets rather than growing this one.

## Notes

**agent:terva/mieli** at 2026-09-12T02:15:46Z

### The interview: three rounds, eighteen questions

Recorded so the positions in the documents can be traced to who decided them. The owner answered every one of these; nothing below is inferred.

**Round 1, the root and its immediate branches.**

1. Whose post is it when a model posts? *A first-class member with its own handle, avatar, and standing, accountable as itself.*
2. Who creates an agent account? *An admin provisions each one; the population is small, known, operator-controlled.*
3. Is there a reading mode filtering a thread by machine-generated? *Unsure, so reserve the bit anyway, since one of sixteen is nearly free and the freeze makes it unavailable.*
4. Must a reader always be able to tell a model wrote a post? *Always and visibly, via the role badge the post header already draws.*
5. How does an agent read and write? *Same HTTP API now; a purpose-built surface later, gated behind evidence.*
6. Who is talking to whom? *"I don't know but I am leaning toward both humans talking to humans with agents augmenting, and agents talking to agents building a thread cooperatively."*
7. What does agent traffic look like? *Bursty: ten or twenty posts in a minute while working, then quiet for hours.*

**Round 2, opened by the answers above.**

1. Does an agent get the read-tracking machinery? *Yes, but it sets its position explicitly through the API. No implicit read-on-render.*
2. Do agents work in the same threads humans converse in? *Separate threads, same forums. A human can walk in and post.*
3. What should a human do with twenty posts a minute? *Read them all. Too many to read is a norms problem, not a tooling problem.*
4. How does an agent authenticate? *Its own token, admin-issued. Revoking it stops that agent and nothing else.*
5. Can an agent hold moderator powers? *Yes, the same roles as anyone. An admin's call.*
6. What happens when an agent hits a rate limit? *Separate limits sized for bursts with a token bucket, 429 only beyond that.*
7. What promotes the purpose-built agent surface? *Both: written friction from a named agent, and a measurement of round trips or tokens.*

**Round 3, the last of the frontier.**

1. Does the agent credential ship before the general token work? *Phase 2, in the same admin flow that provisions the agent, because splitting them means building that screen twice.*
2. Is an agent working thread marked or pure convention? *A thread tag, which already exists as record type 16 and a thread_tags table. No new mechanism.*
3. Introduce a human post rate limit alongside the agent bucket? *The agent bucket only. Humans stay unlimited.*
4. Where does agent-ness live, given roles are orthogonal? *A `kind` column on users, values human and agent.*

Round 2 question 6 is worth noting for whoever implements it: the reason a 429 beats a queue is that a queued post lands minutes later carrying a timestamp that lies about when it was written, and the log is the source of truth. That reasoning is now in docs/08 but the owner picked the behaviour before it was written down.

### One question was not asked, and should have been

Whether an agent's posts count toward forum statistics and "who is online". Nothing in the design turns on it, so no document needed a position, but somebody implementing the member list will have to decide and will find no guidance. Filing it would be heavier than the question deserves; this note is the record.

**agent:terva/mieli** at 2026-09-12T02:15:54Z

in-progress to review: All four criteria met and written into seven documents. Held at review rather than done because the deliverable is edits to the design documents, and the owner should read the diff before it becomes the thing later work builds on. Every position in it is the owner's answer from the interview, but the wording is mine.

**agent:claude-code/review** at 2026-09-12T23:00:38Z

Accepted by the owner in review. The positions are confirmed as theirs and the
wording as faithful to them.

Accepted as settled: agents are first-class members rather than instruments of a
human; `kind` is orthogonal to `role`, so an agent may hold any role; agents are
admin-provisioned, never self-register, and hold their own revocable bearer
credential; the linear-thread argument is unchanged, with volume handled at
thread level by tagging rather than by collapsing posts; humans are unrestricted
while agents are charged against a token bucket, answered with 429 rather than a
queue.

The one format commitment in this spike was verified before acceptance. Frame
flag bit 0 is machine-authored, mirrored into the index entry flags, and the
forward-compatibility property docs/03 claims for it does hold:
`TestFrameUnknownFlagBitsRoundTrip` in internal/store/threadlog/frame_test.go
asserts unknown bits survive a parse and re-append, and it passes.

One gap found while reviewing, now filed as TKT-01M2BXKF8F (Declare the frame
and index flag bits as constants): docs/03 assigns bit 0, but no code declares
it, so three separate call sites would each have to hard-code the number. That
ticket is linked as a dependency of TKT-01M29C5Y1E, because the freeze is what
makes the assignment binding.

Also recorded, unsolved and deliberately so: the live-update pill does not
settle in a thread taking twenty posts a minute. Tagging keeps those threads out
of most people's way, and the decision was to revisit it with a real agent
thread in front of us rather than invent a throttle now.

**agent:claude-code/review** at 2026-09-12T23:00:38Z

review to done: Owner accepted in review: the positions are theirs and the wording is faithful. The one format commitment (frame flag bit 0) was verified against the codec's round-trip test; the missing constant is filed as TKT-01M2BXKF8F and linked to the freeze.

## Summary

All four criteria met, written into seven documents rather than left here. No code changed; this was a design spike and the deliverable was positions.

### The answers

An agent is a **first-class member**: `users.kind = 'agent'` (04), its own handle, its own standing, its own credential. Admin-provisioned, never self-registering. It can hold any role, which is why `kind` is a separate column from `role`.

The format needed **one bit, in two places**: frame flag bit 0 is machine-authored, mirrored into the index entry's flags so a lens filters without decoding bodies (03). Nothing else.

Authentication is the **agent's own bearer token**, issued by the admin at provisioning, shipping in **Phase 2** with the users screen (05, 07, 09). Not the provisioning human's credential, so revocation is per agent. Separate from and ahead of the general personal API token work, which stays in Later.

**The linear-thread argument survives unchanged** (06). It is about a shared reading order, not about who reads. Volume is the real change and the answer is thread-level: agent working threads carry a tag and humans filter by it. No post-level collapsing, no new UI.

Rate limits: **humans stay unlimited, agents get a token bucket**, 429 with Retry-After rather than a queue, because a queued post would carry a timestamp that lies (08).

### The thing worth remembering

The root question was load-bearing in a way nobody knew when it was asked. The frame's `Actor` is a bare `uint64` in a 32-byte fixed part. Had the answer been the delegate model, where a human authors and a model is the instrument, a post would have needed two actor ids and the frame would have had to grow, days before the freeze. First-class membership leaves `Actor` untouched. That is the argument for interviewing before designing: the cheap question and the expensive one look identical until you have the answer.

The second useful reframing came from reading the format rather than the prose. The ticket asked what provenance belongs in the reserved bytes. But bodies are self-describing CBOR maps, so model, version, and a transcript reference can be added at any time for free. The freeze only forces what a lens must see *without* decoding a body. That narrowed a broad question to a single bit and made it easy.

### Where the reasoning is

Positions are in the documents. Rounds and evidence are in the notes on this ticket. The factual inventory of spare capacity came from a mapper sub-agent so that no question put to the owner had an answer sitting in the repository.

Two mapper findings were fixed in passing rather than filed: the index entry's 4 reserved bytes were described both as committed to segment rollover and as generic slack, and the frame's Flags field was declared and never explained. Both are now correct in docs/03 and docs/09.

### What this opened

- TKT-01M29PARE1WYBA7PAKRWSN5QD0 (Define what a reader does with an unknown record type) is now a dependency of the freeze. The format is explicit about unknown flag bits and unknown reserved bytes and silent about unknown record types, while docs/10 already plans to add one.
- TKT-01M29P9RSW21SCES335WERTBSF (Implement agent accounts, provisioning, and the agent credential), Phase 2.
- TKT-01M29PARETXP42D7VG4EGVVBKW (Build a purpose-built agent surface) holds the gate. It carries the two promotion criteria the owner chose and says in its title not to start it.
- TKT-01M29P9RS0YMM3465BSH7AGSNJ collects the remaining document inconsistencies.

One cost is recorded as unsolved in docs/06: the live-update pill does not settle at twenty posts a minute. Tagging keeps those threads out of a human's unread list but does nothing for a human who opens one. Left for a real agent thread rather than a guessed throttle.
