# 04 — Data Model (embedded database)

Rule: **the database holds coordination and projections, never content.**
Post bodies live only in thread logs (03). Every table below is tagged as
either *authoritative* (only copy; must be backed up) or *projection*
(rebuildable from thread logs with `ketju reindex`).

Engine: **SQLite in WAL mode via `github.com/ncruces/go-sqlite3`** (pure
Go, no cgo; see 01-research for the comparison and 10-open-questions for the
modernc fallback). Full-text search uses **FTS5 in the same database file**.
Connection pool capped at 4–8; writes use `BEGIN IMMEDIATE`; `busy_timeout`
set explicitly. Backups: `VACUUM INTO` for a consistent snapshot.

## Conventions

- All ids are `INTEGER` 64-bit, time-sortable where the entity is created by
  users (threads, posts, applications), sequential otherwise.
- Timestamps are unix milliseconds, UTC.
- No cascading deletes. Users are deactivated, never removed; content is
  soft-deleted in logs.
- Every table has `created_at`; mutable tables have `updated_at`.

## Authoritative tables

### users
```
id, handle (unique, case-insensitive), display_name, avatar_blob,
status ('active'|'pending'|'suspended'|'deactivated'),
role ('member'|'moderator'|'admin'), kind ('human'|'agent'),
bio_source, timezone, locale, prefs_json, created_at, last_seen_at
```
`handle` is chosen at registration, not taken from the IdP, so a user keeps
their identity when providers change.

`kind` is orthogonal to `role`. An agent account can be a member, a moderator,
or an admin, because whether to trust an agent with moderation is an operator's
judgement and not a rule in the software. Keeping the two columns separate is
what allows that: a fourth `role` value would have pinned every agent to being
an ordinary member.

One field then answers three questions. It selects the role badge every post
header draws (06), it sets the machine-authored flag bit written into each
record frame (03), and it decides which rate limit bucket a write is charged
against (08). An admin provisions every agent account and an agent never
self-registers (05).

### identities
```
id, user_id, provider_key, subject (IdP 'sub'), email, email_verified,
issuer, last_login_at, raw_claims_json (last seen, for admin debugging)
UNIQUE (provider_key, subject)
```
One user, many identities: account linking is a first-class operation.
`provider_key` refers to a provider block in `config.toml`, not to a table, so
an operator can remove a provider from config and its identities become
inert rather than orphaned.

### local_credentials (optional feature, off by default)
```
user_id (pk), password_hash (argon2id), updated_at
```

### sessions
```
id (random 256-bit, stored hashed), user_id (nullable), identity_id (nullable),
kind ('user'|'pre_user'), created_at, expires_at, last_used_at, ip, user_agent,
oidc_provider_key, oidc_refresh_token_enc (nullable)
```
Server-side sessions so that suspension and "log out everywhere" are
immediate. Cookie carries only the session id. `pre_user` sessions carry an
`identity_id` and no `user_id`; they exist only for approval-mode applicants
and are accepted by the application endpoints alone (05).

### applications (approval mode)
```
id, identity_id (nullable: application may precede any login),
form_id, form_version, answers_json, status ('pending'|'approved'|'denied'|'withdrawn'),
decided_by, decided_at, decision_note, created_at
```

### forms, form_versions
```
forms: id, key (e.g. 'registration'), title, active_version
form_versions: form_id, version, schema_json, created_by, created_at
```
Schema is a small declarative field list (see 05). Versioned so old
applications remain readable after the form changes.

### forums
```
id, parent_id (nullable, one level of nesting max), slug, name, description,
position, visibility ('public'|'members'|'restricted'), archived, created_at
```

### groups, group_members
```
groups: id, name, slug, description, is_system
group_members: group_id, user_id, added_by, added_at
```
System groups: `everyone`, `members`, `moderators`, `admins`.

### permissions
```
forum_id (nullable = global), group_id, permission
permission ∈ {read, create_thread, reply, upload, moderate}
```
Small, explicit, phpBB-like. Evaluation: union of the user's groups'
permissions for the forum, falling back to global rows.

### audit_log
```
id, at, actor_id, action, target_type, target_id, details_json, ip
```
Every moderation and administration action. Append-only by convention.

### notifications
```
id, user_id, kind, thread_id, ordinal, actor_id, at, read_at
```
Kinds: reply_to_you, mention, quoted, thread_you_watch, application_decided,
new_application (moderators).

### watches
```
user_id, thread_id (nullable), forum_id (nullable), level ('watch'|'mute')
```

### read_positions
```
user_id, thread_id, last_read_ordinal, last_read_at
```
One row per (user, thread) ever visited. Combined with `thread_projection
.post_count` this yields unread counts without a per-post table. A user-level
`mark_all_read_at` timestamp handles "everything before this date is read"
so we do not need rows for threads never opened.

### flags
```
id, thread_id, ordinal, reporter_id, reason, note, created_at,
status ('open'|'resolved'|'dismissed'), resolved_by, resolved_at, resolution_note
```
Reports on posts; feeds the moderator queue (08).

### drafts
```
user_id, thread_id (nullable = new-thread draft, then forum_id), source,
reply_to, updated_at
PRIMARY KEY (user_id, thread_id)
```
Composer autosave, one per user per thread.

### blobs
```
hash (pk), size, mime, original_name, uploaded_by, uploaded_at,
refcount, status ('ok'|'quarantined')
```

### settings
```
key (pk), value_json, updated_at, updated_by
```
Runtime-changeable settings (site name, registration mode, slow-mode
defaults). Bootstrap and provider configuration stay in `config.toml`
because they must be readable before the database exists.

## Projection tables (rebuildable)

### threads
```
id, forum_id, slug, title, author_id, created_at,
post_count, last_post_at, last_post_author_id, last_post_ordinal,
locked, pinned_scope, pinned_until, archived,
log_length_projected      -- how far into the log this row reflects
```

### thread_tags
```
thread_id, tag
```

### post_meta
```
thread_id, ordinal, author_id, created_at, current_revision, edited_at,
reply_to, deleted, moved, log_offset
PRIMARY KEY (thread_id, ordinal)
INDEX (author_id, created_at)
INDEX (thread_id, reply_to)
```
Tiny rows. Exists for cross-thread questions: a user's post history,
"replies to post X" reverse lookups when the thread is not open, moderation
lists. The per-thread index file already answers within-thread questions.

### mentions
```
thread_id, ordinal, mentioned_user_id
```

### search (FTS)
Full-text index over `(thread_id, ordinal, revision, title, source_plain)`.
Kept in the same database if the engine supports FTS (SQLite FTS5) or in a
separate pure-Go index (bleve) otherwise. Always rebuildable.

### projection_state
```
thread_id (pk), log_length, updated_at
```
Drives startup catch-up (03, write path step 5).

## What deliberately is not in the database

| Thing | Where it lives | Why |
|-------|----------------|-----|
| Post bodies and edit history | thread logs | the whole point |
| Rendered HTML | in-memory cache only | renderer-version coupling |
| Reactions/likes | **not in v1**; if added: a `reactions` authoritative table | high churn, not content |
| Per-post read markers | derived from `read_positions` + ordinals | one row per thread, not per post |
| OIDC provider config | `config.toml` | must exist before DB, secret-bearing |
| Signed-in state | server-side `sessions` | revocability |

## Sizing sanity check

For 1 000 users, 10 000 threads, 500 000 posts: `post_meta` ≈ 500 k rows ×
~80 bytes ≈ 40 MB, `read_positions` worst case 1 000 × 10 000 = 10 M rows but
realistically < 1 M. The FTS index is the largest object, roughly the size of
the plain text (~0.5–1 GB) — and rebuildable. Whole database comfortably under
2 GB; SQLite-class engines handle this trivially.
