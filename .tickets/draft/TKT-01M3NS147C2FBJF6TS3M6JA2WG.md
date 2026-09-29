---
schema: 3
id: TKT-01M3NS147C2FBJF6TS3M6JA2WG
title: Decide how a thread title is hard-redacted
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:storage
  - effort:S
  - decision
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T05:08:35Z
updated_at: 2026-09-29T07:21:01Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

docs/03 "Hard redaction" covers a post's text: its `Post` and every `Edit`. A thread's title lives elsewhere, in the `ThreadCreated` body and in every `ThreadRenamed` body, and so does its slug. It is also copied into the `threads` projection row. A legal takedown or a GDPR erasure can cover a title as easily as a post, and today the only tool, a rename, leaves every earlier title in the log, readable by `ketju dump` and restorable by a reindex.

Found while planning TKT-01M29CXJZ9 (Build the offline hard-redaction tool), whose plan leaves titles out of scope because the design says nothing about them.

### To decide
- Whether `ketju redact` also takes a thread and blanks titles. It would blank `ThreadCreated.title`/`slug` and `ThreadRenamed.title`/`slug` the same way post bodies are blanked, with the same padding so the log does not shrink.
- What a thread with a redacted title then shows, since every surface needs some title.
- Whether the old slugs, which docs/03 keeps valid as redirects, stop resolving.
- Whether this needs a second critical record type or a `Redacted` body field naming the thread level (ordinal 0).

## Acceptance criteria

- [ ] The decision is recorded in docs/03 Hard redaction with the reasoning
- [ ] If titles are redacted, the work is filed or folded into TKT-01M29CXJZ9 before that ticket is claimed

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T07:21:01Z

Options and a recommendation, written in Phase 2 grooming (TKT-01M3M9RR26) on 2026-09-29. **Nothing here is decided.** The maintainer decides, and the answer goes into docs/03 "Hard redaction" (this ticket's first criterion).

### Where a title lives

- `ThreadCreatedBody{title, slug, forum_id, tags}` and every `ThreadRenamedBody{title, slug}` (`internal/store/threadlog/records.go`). ThreadCreated also carries `forum_id` and `tags`, so "the empty body of that type" would lose the forum: a blanked ThreadCreated must keep those two.
- The `threads` projection row (`title`, `slug`), rebuilt from the log by a reindex.
- `audit_log`: TKT-01M29CKRH9's plan writes `thread.renamed` with `{from, to}` titles for a moderator's rename. `audit_log` is append-only by trigger.
- The search index, when it exists (TKT-01M3NS122F).
- Not in the render or response caches: the thread header comes from the projection on each request, and serve is stopped while `ketju redact` runs.
- Split copies posts, not titles: a split thread gets a new title from the moderator. No `origin` following is needed.
- Old slugs: the id is the permalink, and any slug redirects to the current one (docs/07). No table keeps old slugs.

### 1. Does `ketju redact` take a title?

- **A.** Yes. `ketju redact --thread ID --title --reason …` blanks `title` and `slug` in ThreadCreated and every ThreadRenamed frame, keeping `forum_id` and `tags`. Each frame is padded to its old length and re-checksummed, as TKT-01M29CXJZ9 does for post bodies. The log keeps its length plus the new frames, so `log_length` and the ETag still only grow. The journal and resume work as for posts, with a target `{thread, title: true}`. One run may name a title and posts together, since a takedown of an opening post usually covers its title too.
- **B.** No. A title takedown is a moderator rename, and docs/03 says plainly that earlier titles stay in the log, `ketju dump` and a reindex. That fails a GDPR erasure of a title.
- **C.** Only whole threads: redact every post and the title. Too blunt for a takedown of one title.

### 2. What the thread shows afterwards

- **i.** A fixed title, "Title removed", with the slug from `Slug()`, written as an ordinary `ThreadRenamed` the tool appends after the marker. Every surface needs a title, and a rename record needs no special case in the projection, the reader or the client.
- **ii.** A title the operator gives with `--title`, for a takedown that covers only part of a title, so the thread stays findable.
- **iii.** No appended rename. The projection draws a placeholder when it applies the marker. Every reader of `threads.title` and every build then needs to know the marker.

### 3. Old slugs

- **Keep redirecting.** Nothing stores them, so an old URL redirects to the replacement slug by id. The old slug stays in links outside the forum and in request logs, which ketju cannot reach.
- **Stop resolving.** This needs a slug table that does not exist, only to answer `not-found` for a slug that no longer says anything.

### 4. The marker in the log

- **M1.** `Redacted` (0x80) with `ordinal: 0` and a new `title: true` field, meaning every title frame before this record was blanked. One critical type, and one resume check. `Index.Apply` must not look up post 0. This is safe only if it lands with TKT-01M29CXJZ9, before any build knows `Redacted` without the field.
- **M2.** A second critical type, `0x81 TitleRedacted {frames, reason, operator}`, claimed by docs/03's procedure. It is explicit, but it is one more catalogue entry, index case and projection decision.
- **M3.** No marker. The blanked frames and the appended rename reveal nothing to any build. The audit row and the journal record the act. Resume would then have to look inside bodies to tell whether a log is done, and nothing in the log says the title was removed on purpose.

### 5. Audit rows that carry the title

- **a.** TKT-01M29CKRH9 records `{thread, log_offset}` for `thread.renamed`, without the titles. The log is the record, and a redaction then leaves no copy in `audit_log`.
- **b.** Accept the copy, as handles are accepted (docs/05, decided 2026-09-29). A title is user content, and it may be the unlawful text itself, so this is weaker than the handle case.
- **c.** Let the offline tool rewrite matching audit rows under a sanctioned trigger exemption. This breaks the one rule `audit_log` has.

### Recommendation

A, with i as the default and ii as an option; keep old slugs redirecting; M1, folded into TKT-01M29CXJZ9 before it is claimed (this ticket's second criterion); and a, a change to TKT-01M29CKRH9's plan before that ticket is claimed. The search index drops the title when TKT-01M3NS122F exists. A second critical type (M2) is the fallback if TKT-01M29CXJZ9 lands before this is decided.
