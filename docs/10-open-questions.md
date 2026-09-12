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
4. **Hard redaction tool semantics.** Zero the body in place (breaks CRC of
   that frame → reader treats as redacted) vs rewrite the log with a
   `Redacted` record type. Lean: rewrite with a new type, keep CRCs valid.
   Decide before Phase 2 (needed for GDPR-style requests).
5. **Reactions.** Not in v1. If added: DB table, per-post counts shown in
   the footer, never used for ordering. Decide after real usage.

## Database

6. **ncruces vs modernc.** Lean: ncruces for speed and Go tokenizers;
   revisit if its 1.0 introduces churn. Decide at Phase 1 start.
7. **Sessions: `scs/v2` vs hand-rolled.** Lean: hand-rolled (~200 lines,
   exact control over hashing, revocation, and the pre-user session
   variant). `scs` if we want its middleware ergonomics. Decide in Phase 1.
8. **Where do OIDC `state`/`nonce`/PKCE live pre-login.** Encrypted
   short-lived cookie (current design) vs an anonymous server-side session.
   Lean: cookie; no DB write for an abandoned login. Decide in Phase 1.

## Identity and accounts

9. **Handle change policy.** Never, once, or with redirects. Lean: allowed
   with a 30-day cooldown and old handles reserved for 90 days;
   `@mentions` are stored by user id so history stays correct.
10. **Should `auto_approve` providers also skip handle selection** using
    `preferred_username`? Lean: yes, with a conflict fallback screen.
11. **Guests.** Public forums readable anonymously (`visibility = public`)
    is in; anonymous posting is out. Confirm.
12. **Email.** No SMTP requirement in v1. Notifications are in-app only.
    Email digest is a later phase and needs a decision on the mail
    library and templates.

## Views and product

13. **Page size default 25 vs 20/30.** Cosmetic; pick during Phase 1 UI work.
14. **Landing position: last read vs first unread.** Both are the same
    page in practice; the divider handles the distinction. Discourse chose
    last-read; we do the same. Confirm with users.
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
21. **Component/CSS approach.** Hand-written CSS with custom properties
    (lean) vs a utility framework. Decide at Phase 1 UI start.

## Mobile

25. **Bottom tab bar vs. a menu button.** Lean: tab bar with five items
    (Forums, Unread, New, Alerts, Me) as mocked up. A hamburger hides the
    "what's new" surfaces that are the point of a forum. Confirm with
    users on a real device.
26. **Composer as a full-screen sheet vs. inline at the bottom.** Lean:
    sheet. Inline composers under a virtual keyboard are cramped and hide
    the post being replied to; the sheet can show the quoted post above
    the text area.
27. **Push notifications.** Web Push needs a VAPID key pair and a
    subscription table, and delivery differs by platform. Not in v1;
    the manifest and in-app Alerts tab come first. Decide after Phase 3.
28. **Offline reading.** A service worker caching recently read pages is
    plausible because thread pages are immutable per log length. Not in
    v1; revisit with push.
29. **Native apps.** Out of scope. The installable web app is the mobile
    story; revisit only if push or share-sheet integration demands it.

## Operations

22. **TLS.** Reverse proxy assumed; built-in ACME later. Confirm.
23. **Backup tooling.** Document `rsync` of `data/` plus `VACUUM INTO` for
    the DB, or ship `ketju backup` that does both consistently (pause
    thread actors, checkpoint WAL, snapshot). Lean: ship the command in
    Phase 4.
24. **Single-process assumption.** A lock file in `data/` refuses a second
    `serve`. Horizontal scaling is out of scope by design; confirm that is
    acceptable for the target communities.

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
