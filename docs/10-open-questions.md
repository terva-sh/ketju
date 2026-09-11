# 10 — Open Questions and Deferred Decisions

Each item states the options, the current lean, and when it must be decided.

## Storage

1. **Body encoding: CBOR vs JSON in the log.** Lean: CBOR (`fxamacker/cbor`
   is pure Go, fast, well maintained), JSON supported for dumps. Decide in
   Phase 0 after measuring decode cost; if the difference is negligible,
   JSON's debuggability may win.
2. **Index entry width: 40 bytes with author/time/reply_to vs 16 bytes
   offset-only.** Lean: 40. Costs 2 MB per 50 k posts and buys DB-free
   per-thread lenses. Decide in Phase 0.
3. **fsync policy default: `group` (≤ 5 ms) vs `always`.** Lean: `group`.
   Posts are precious but a 5 ms window on a single-node forum is an
   acceptable trade; operators on ZFS/battery-backed storage can set `never`.
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
    the DB, or ship `neoforum backup` that does both consistently (pause
    thread actors, checkpoint WAL, snapshot). Lean: ship the command in
    Phase 4.
24. **Single-process assumption.** A lock file in `data/` refuses a second
    `serve`. Horizontal scaling is out of scope by design; confirm that is
    acceptable for the target communities.
