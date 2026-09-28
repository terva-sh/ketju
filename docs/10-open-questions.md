# 10 — Open Questions and Deferred Decisions

Each item states the options, the current lean, and when it must be decided.

## Storage

1. **Body encoding: CBOR vs JSON in the log.** **Decided: CBOR**, with JSON
   kept as encoding byte 2 for `ketju dump` and debugging. Decoding a 2 KB
   body is 8.3x faster under CBOR with one allocation instead of seven, and
   the on-disk difference is only 2 to 5 percent, so JSON's debuggability was
   never buying what it looked like it cost. Re-run the benchmark when Go
   1.27's json/v2 arrives; it would have to close an 8x gap to reopen this.
   (TKT-01M29BSPD)
2. **Index entry width: 40 bytes with author/time/reply_to vs 16 bytes
   offset-only.** **Decided: 40.** An author filter over a 50 k post thread
   takes 15 us from the index against 19.8 ms when the author must be read
   back from the log, a factor of 1300 measured warm. The memory cost is
   1.91 MB against 0.76 MB on that thread, which also confirms the 2 MB
   estimate in 03. (TKT-01M29BSPD)
3. **fsync policy default: `group` vs `always`.** **Decided: `group`, with
   drain semantics rather than a fixed window.** The actor batches whatever is
   already queued and never waits, so a lone append pays nothing and eight
   concurrent writers still collapse 1600 fsyncs into 400. The fixed 5 ms
   window this document previously assumed measured 6.5x worse at the median
   for a single writer while batching nothing at all, which is the normal
   state of a small forum. Absolute latency remains unverified: the
   measurement was taken under WSL on a virtual disk with a write-back cache,
   which is not target storage. TKT-01M29C2WV6 owns verification on real
   hardware, and the p99 target in 09 is not confirmed until it runs there.
   (TKT-01M29BSPD)
4. **Hard redaction tool semantics.** **Decided 2026-09-28: rewrite the log
   with a `Redacted` record**, from the critical range (`0x80`–`0xFF`), so
   every CRC stays valid and a checksum failure keeps meaning corruption. An
   older build meeting it refuses the log rather than showing removed
   content. One redaction removes the post's original and every revision's
   source, its mentions, and any attachment only it references. Quotes of it
   in other posts are listed for the operator, never silently edited. The
   shape is in 03, "Hard redaction". Zeroing in place lost because it makes a
   bad CRC sometimes normal, and corruption then hides. (TKT-01M29BSPE2.)
5. **Reactions.** Not in v1. If added: DB table, per-post counts shown in
   the footer, never used for ordering. Decide after real usage.

## Database

6. **ncruces vs modernc.** **Decided: ncruces**, for speed and for FTS5
   tokenizers written in Go (2026-09-24). The decision is taken ahead of the
   benchmark: TKT-01M29BSPE8 still measures both drivers on the projection
   write path and confirms the FTS5 story. It reopens the question only if
   ncruces is more than twice as slow there. Revisit if its 1.0 introduces
   churn.
7. **Sessions: `scs/v2` vs hand-rolled.** **Decided: hand-rolled**
   (2026-09-24). It is about 200 lines. Hand-rolled gives exact control
   over id hashing and immediate revocation, and it can model the pre-user
   session that approval mode needs, which `scs` does not. The trade is
   `scs`'s middleware ergonomics, which the existing chain in `internal/web`
   already covers. Recorded in 05. (TKT-01M29BSPEF)
8. **Where do OIDC `state`/`nonce`/PKCE live pre-login.** **Decided: an
   encrypted, signed, short-lived cookie** (2026-09-24). An abandoned login
   costs no database write, and an unauthenticated caller cannot fill a
   table by starting logins. Recorded in 05. (TKT-01M29BSPEF)

## Identity and accounts

9. **Handle change policy.** **Decided 2026-09-28: allowed, once per 30
   days.** The old handle is reserved for 90 days, and `/u/{old}` answers a
   temporary 302 to the new profile until then, never a cacheable 301,
   because the handle is reusable afterwards. Admins may rename anyone without the
   cooldown, which is how the grandfathered handles ending in `-`
   (TKT-01M3ANX1NF) get fixed. `@mentions` are stored by user id, so history
   stays correct. Never and once-only lost as friction with no gain, because
   history does not depend on the handle. (TKT-01M29BSPEN; the rules are in 05.)
10. **Should `auto_approve` providers also skip handle selection** using
    `preferred_username`? **Decided 2026-09-28: yes, with a fallback.** A
    valid, free `preferred_username` becomes the handle without a screen;
    otherwise the normal handle screen appears, prefilled, with the reason.
    (TKT-01M29BSPEX; 05.)
11. **Guests.** **Confirmed 2026-09-28:** public forums are readable
    anonymously (`visibility = public`), and anonymous posting is out.
    Profiles stay members-only (TKT-01M3KJJNDC). (TKT-01M29BSPEX.)
12. **Email.** No SMTP requirement in v1. Notifications are in-app only.
    Email digest is a later phase and needs a decision on the mail
    library and templates.

## Views and product

13. **Page size default 25 vs 20/30.** **Decided: 25** (2026-09-24). It
    is baked into shared URLs, so it is picked once. The user setting still
    ranges from 10 to 50. Recorded in 06 and 07. (TKT-01M29BW9NW)
14. **Landing position: last read vs first unread.** **Decided: last
    read** (2026-09-24). Both land on the same page in practice, and the
    divider handles the distinction. Discourse made the same choice. The
    check on a real device with real readers is still open and belongs to
    the Phase 1 human verification pass. (TKT-01M29BW9P4)
15. **"Only OP" as a button vs discoverable through the author lens.**
    Lean: button on the thread header.
16. **Digest lens scoring** once reply/quote counts exist:
    `replies*3 + quotes*2 + mentions`? Decide in Phase 3+ with data.
17. **Infinite scroll as an opt-in user preference** on top of pages.
    Lean: no in v1.
18. **Sub-topic dividers** (moderator-set labels for drift regions inside a
    thread) — cheap because they are thread events; value unproven.

## Clients

19. **Should the non-JS client become the primary shell** (server-rendered
    HTML enhanced by Preact islands) rather than a second client? The
    research argues server-first is lighter and more robust; the stated
    goal is Preact-first with a non-JS alternative later. The architecture
    supports both; the choice is about where the UI effort goes. Revisit at
    the Phase 5 boundary with real usage data (how many members would
    benefit, how much of the SPA is actually interactive).
20. **Preact 11** adoption timing. Pin 10.x through v1.0.
21. **Component/CSS approach.** **Decided: hand-written CSS with custom
    properties** (2026-09-24). It adds no dependency to the 60 KB gzipped
    budget. The mockups in `docs/mockups/` are already written this way.
    Recorded in 07. (TKT-01M29BW9NW)

## Operations

22. **TLS.** **Decided: a reverse proxy terminates TLS in v1**, and
    built-in ACME comes in a later phase (2026-09-24). "Just run it" is
    allowed to mean Caddy in front. Recorded in 02. (TKT-01M29BW9PB)
23. **Backup tooling.** **Decided: ship `ketju backup` in Phase 4**
    (2026-09-24). It pauses the thread actors, checkpoints the WAL, and
    snapshots. Until then, a documented `rsync` of `data/` plus
    `VACUUM INTO` for the database is the procedure. Recorded in 02.
    (TKT-01M29BW9PB)
24. **Single-process assumption.** **Decided: one process per data
    directory**, enforced by a lock file in `data/` that refuses a second
    `serve` (2026-09-24). Horizontal scaling is out of scope by design for
    the communities ketju targets. Recorded in 02 and stated in the README.
    (TKT-01M29BW9PB)

## Mobile

25. **Bottom tab bar vs. a menu button.** **Decided: a tab bar with five
    items** (Forums, Unread, New, Alerts, Me), as mocked up (2026-09-24). A
    hamburger menu hides the "what's new" surfaces that are the point of a
    forum. The real-device check is still open; see 14. (TKT-01M29BW9P4)
26. **Composer as a full-screen sheet vs. inline at the bottom.**
    **Decided: sheet** (2026-09-24). An inline composer under a virtual
    keyboard is cramped and hides the post being replied to. The sheet can
    show the quoted post above the text area. The real-device check is still
    open; see 14. (TKT-01M29BW9P4)
27. **Push notifications.** Web Push needs a VAPID key pair and a
    subscription table, and delivery differs by platform. Not in v1;
    the manifest and in-app Alerts tab come first. Decide after Phase 3.
28. **Offline reading.** A service worker caching recently read pages is
    plausible because thread pages are immutable per log length. Not in
    v1; revisit with push.
29. **Native apps.** Out of scope. The installable web app is the mobile
    story; revisit only if push or share-sheet integration demands it.

## Agents

Settled by TKT-01M29GGG3W, by interview rather than by inference. The reasoning
lives in the documents these items point at; what follows records what closed.

30. **Are members necessarily human?** **Decided: no.** An agent is a
    first-class member with `users.kind = 'agent'` (04): its own handle, its own
    standing, its own credential. This turned out to be the load-bearing
    choice. The record frame's actor is one `uint64` user id inside a 32-byte
    fixed part (03), and the alternative reading, where a human authors and a
    model is the instrument, needs two actor ids and therefore a wider frame.
    First-class membership leaves the format alone.
31. **Does anything agent-specific belong in the frozen format?** **Decided:
    one bit, in two places.** Frame flag bit 0 is machine-authored, mirrored
    into the index entry's flags so a lens filters without decoding bodies
    (03). Nothing else. Richer provenance, which model and version, or a
    reference to the transcript, goes in the CBOR body, which is a
    self-describing map and needs no reservation and no version bump. So the
    freeze in TKT-01M29C5Y1E forces only what a lens must see without reading a
    body, and that is one bit.
32. **How does an agent authenticate?** **Decided: its own bearer token,
    issued by the admin who provisions it** (05, 07). Not the provisioning
    human's credential, so revocation stops one agent and nothing else. It
    ships in Phase 2 with the admin users screen, ahead of and separate from
    personal API tokens.
33. **Does the linear-thread argument survive models as participants?**
    **Decided: yes, unchanged.** The argument is about a shared reading order
    rather than about who is reading (06). Volume is the real change, and the
    answer is thread-level: an agent working thread carries a tag and humans
    filter by it. No post-level collapsing and no new UI. One cost is recorded
    and unsolved, that the live-update pill never settles at twenty posts a
    minute.
34. **A purpose-built agent surface** (MCP, a feed format). Gated rather than
    scheduled. It needs written friction from a named agent that did real work,
    and a measurement of round trips or tokens. 07 states the trigger.
