# 09 — Roadmap

Phases are ordered so that each one ends with something a small group can
actually use, and so that the riskiest engineering (thread storage) is
proven first. Estimates assume one developer working part-time; treat them
as relative sizes, not promises.

## Phase 0 — Storage spike (1–2 weeks)

Goal: prove the thread log format and its performance envelope before any
web code exists.

- `internal/store/threadlog`: file/frame/index formats from 03, thread
  actor, open-thread LRU, CRC verification, tail recovery.
- Property and crash tests (see 02, testing). Fuzz the frame parser.
- `ketju dump` and `ketju fsck` commands.
- Benchmark: 50 k-post synthetic thread; measure append latency with
  `group` fsync, random-ordinal read latency cold and warm, index rebuild
  time. Targets: append p99 < 10 ms with fsync, warm read < 100 µs, rebuild
  of 100 MB < 2 s.
- Exit criterion: format frozen at version 1 with reserved bytes and with
  frame flag bit 0 assigned to machine-authored (03); any change after this
  is a version bump with a migration path.

## Phase 1 — Walking skeleton (3–4 weeks)

Goal: sign in with OIDC, read and write linear threads, in one binary.

- SQLite via ncruces, migrations, `users`, `identities`, `sessions`,
  `forums`, `threads`, `post_meta`, `read_positions`, `projection_state`.
- Event bus, projection writer with startup catch-up.
- OIDC login with one provider, `open` registration, handle selection,
  bootstrap admin from config.
- Markdown pipeline (goldmark v2 + bluemonday), `POST /render`.
- API: forums, thread list, thread page (canonical view only), create
  thread, reply, edit, soft delete, read position.
- Preact client: forum index, thread list, thread page with pages,
  "N new posts below" divider, composer with preview, quote, reply-to chip.
- Both layouts from the start: desktop top bar and phone bottom tab bar,
  the sticky reply bar and full-screen composer sheet on phones, 44 px
  targets. Playwright smoke tests at 390 px and 1440 px.
- Embedding, config, `serve`, structured logs, healthz.
- Exit criterion: a friend group can hold a conversation on it.

## Phase 2 — Approval mode and moderation basics (2–3 weeks)

- Registration `mode = "approval"`: forms, form versions, application
  flow, pre-user session, moderator queue with approve/deny, notifications
  for moderators and applicants.
- Multiple providers, account linking, `auto_approve` per provider,
  `allowed_email_domains`, IdP group mapping.
- Moderation: lock, pin, rename, move, flags with auto-hide, tombstones,
  edit history view, suspend, audit log.
- Admin UI: forums, users, forms, settings, queue.
- Agent accounts: `users.kind`, admin provisioning, bearer credential issue
  and revoke in the same users screen, and the per-agent rate limit bucket
  (05, 07, 08). This path is narrower than personal API tokens and does not
  wait for them.
- Exit criterion: a club can open registration to strangers safely, and an
  admin can give an agent an account and a credential.

## Phase 3 — Lenses, unread, search, live (3 weeks)

- Lenses: unread, author/only-OP, reply chain, since, mentions, moderator.
- Inline reply expansion with suppress-directly-below; quote backlinks.
- Cross-forum `/unread`, `/new`, `/participated`, `/watching`; watches and
  mutes; notifications UI.
- FTS5 search with snippets; `ketju reindex --search`.
- SSE live updates with the "N new posts — show" pill; polling fallback.
- Drafts; attachments with thumbnails and blob GC.
- Web app manifest and icons so the forum can be added to a home screen;
  page-picker and lens sheets on phones.
- Exit criterion: daily-driver quality for an active community, on a phone
  as much as on a desktop.

## Phase 4 — Split/merge and polish (2 weeks)

- Moderator multi-select, split to new thread, merge, "reply as new
  thread", bidirectional notices.
- Slow mode, archive, thread tags.
- User settings: sessions list, identities, timezone, page size, theme.
- Metrics endpoint, backup documentation (rsync + `VACUUM INTO`),
  `ketju config check`.
- Exit criterion: v1.0 tag.

## Phase 5 — Non-JS client (post-1.0, 3–4 weeks)

- `internal/web/html` with templ views for every URL in 07, form-post
  mutations, page-load read marking.
- htmx enhancement layer for the composer preview and expanders.
- Crawler and `<noscript>` routing to the HTML client.
- Decide whether the HTML client becomes the default shell with the SPA as
  enhancement (see 10).

## Later / maybe

Timeline scrubber; digest lens; bookmarks; invite mode; local passwords
with SMTP; email notifications digest; reactions (append-only table, never
reorder anything); personal API tokens for a human's own scripts, which are
separate from the agent credentials in Phase 2; ActivityPub read-only export;
ACME TLS; multi-language stemming via Go FTS5 tokenizers.

A purpose-built agent surface, an MCP server or a feed format, is deliberately
not on this list. It is gated on two pieces of written evidence rather than
scheduled, and 07 states the trigger.

## Risks and mitigations

| Risk | Mitigation |
|------|------------|
| Thread log format needs a change after data exists | version field, the file header's 24 reserved bytes, and the unused flag bits in the frame and the index entry. The index entry's 4 reserved bytes are spoken for by segment rollover and are not slack (03). `ketju migrate-logs` rewrites into a new directory then swaps |
| ncruces pre-1.0 API churn | schema and SQL are driver-agnostic; modernc is a drop-in |
| Preact 11 migration | pin 10.x; migration is documented as small |
| Go 1.27 json/v2 strictness | golden tests from Phase 1 catch duplicate-key or UTF-8 issues early |
| OIDC provider quirks (Azure issuer, Google `hd`) | go-oidc's `InsecureIssuerURLContext` behind a per-provider flag; fake issuer in tests |
| Approval bypass via a code path that creates sessions | one function creates sessions; it checks `status = active`; test the callback path explicitly (Gitea's bug) |
| Scope creep toward Discourse | 06 and 08 "deliberately not built" lists are policy, not suggestions |
| Phone layout treated as an afterthought and bolted on late | both layouts are Phase 1 exit criteria; the mockups in `docs/mockups/` are the reference; smoke tests run at both viewports |
| Mobile composer fights the on-screen keyboard | full-screen sheet with the toolbar pinned above the keyboard; test on real iOS and Android before Phase 1 exit |
