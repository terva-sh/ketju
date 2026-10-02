# 04 — Data Model (embedded database)

Rule: **the database holds coordination and projections, never content.**
Post bodies live only in thread logs (03). Every table below is tagged as
either *authoritative* (only copy; must be backed up) or *projection*
(rebuildable from thread logs with `ketju reindex`).

Engine: **SQLite in WAL mode via `github.com/ncruces/go-sqlite3`** (pure
Go, no cgo; see 01-research for the comparison and 10-open-questions for the
modernc fallback). Full-text search uses **FTS5 in the same database file**.
Connection pool capped at 4–8; writes use `BEGIN IMMEDIATE`; `busy_timeout`
set explicitly, at 5 s; `synchronous=FULL`, because in WAL mode `NORMAL` can
lose the latest commits on power loss and this file holds authoritative tables
(about 0.6 ms a commit on a desktop NVMe, TKT-01M29BW9Q1). Writes go through a single connection, so in-process writers
queue in Go rather than in SQLite's busy handler: the driver benchmark
(TKT-01M29BSPE8, `spikes/sqlite-driver/`) saw ncruces commits starve for
seconds with four or eight writer connections contending. The same benchmark
found that ncruces never fsyncs the directory when SQLite asks it to after
creating the WAL file, so a power loss could take the WAL, and the committed
transactions in it, with it. The opener therefore fsyncs the data directory
itself once the WAL exists and before the first authoritative write, and no
authoritative write may happen without that step (TKT-01M29BW9Q1; the upstream
report and eventual removal are TKT-01M392DP19). Upstream had no fix as of
v0.35.6 on 2026-09-28, and a test fails on any ncruces version nobody has
checked for the bug, so an upgrade cannot quietly change what the workaround
relies on. Backups: `VACUUM INTO` for a
consistent snapshot.

Access from Go: **sqlc**, generating typed `database/sql` code from `.sql`
query files, with the migration directory as its schema source (decided
2026-09-24, TKT-01M29BW9Q1). The rule "SQL stays driver-agnostic" is what
makes modernc a drop-in fallback (09), and sqlc is its mechanism: it emits
only `database/sql` calls, so the generator cannot produce a driver-specific
query. Hand-written `database/sql` everywhere was the alternative. It lost
because it leaves driver-agnosticism to whoever reviews each query. sqlc has
a known weak spot, and there the split is a rule rather than a discovery: the
lens-filtered and cursor-paged view queries (06) are dynamic in shape and are
hand-written `database/sql` beside the generated set. Generated code is
committed, and CI fails if regenerating it produces a diff. The migration
runner stays ours; sqlc does not run migrations.

## Conventions

- All ids are `INTEGER` 64-bit, time-sortable where the entity is created by
  users (threads, posts, applications), sequential otherwise.
- Timestamps are unix milliseconds, UTC.
- No cascading deletes. Users are deactivated, never removed; content is
  soft-deleted in logs. Foreign keys are NO ACTION, with one exception:
  `applications.identity_id` is ON DELETE SET NULL, which deletes nothing
  (see "applications"). `TestNoCascadingDeletes` names it, so a second
  exception needs a decision of its own.
- Every table has `created_at`; mutable tables have `updated_at`.

## Authoritative tables

### users
```
id, handle (unique, case-insensitive), display_name, avatar_blob,
status ('active'|'pending'|'suspended'|'deactivated'),
role ('member'|'moderator'|'admin'), kind ('human'|'agent'),
bio_source, timezone, locale, prefs_json, mark_all_read_at (nullable),
created_at, updated_at, last_seen_at,
status_reason (default ''), status_until (nullable, Unix ms),
status_version (default 0),
anonymized (0|1, default 0),
handle_changed_at (nullable, Unix ms)
```
`handle` is chosen at registration, not taken from the IdP, so a user keeps
their identity when providers change.

`prefs_json` is a JSON object of the member's own preferences, written by
`PATCH /settings` (07, TKT-01M29CCC0X). Three keys are read: `page_size`,
the posts per thread page, an integer from 10 to 50 (06 "Paged, not
infinite"); `theme`, one of `system`, `light` and `dark` (07 "Preact
client"); and `shortcuts`, a JSON boolean saying whether the single-key
shortcuts are on (06 "Paged, not infinite", TKT-01M3V9DWTN). Each key that
is missing, malformed or out of range reads as its default, 25, `system`
and `true`, and so does every key when the column is not a JSON object. A
write keeps the keys it does not name, and replaces a column that is not an
object with one. `timezone` is its own column, an IANA zone name; `locale`
has no writer yet.

**The account lifecycle** (migration 0012, TKT-01M29CKRFS, 05 "Account
lifecycle"). `status_reason` and `status_until` are a suspension's reason and
end, which the member is shown at sign-in; `status_until` is null when no end
is set. Every status change writes all three columns, so a reason never
outlives its suspension, and adds 1 to `status_version`, which the Reactivate
button's cookie is bound to (05 "Login flow"); a timestamp would not change
between two changes in the same millisecond. `anonymized` marks an anonymized account, and is what
every author block reads to withhold the display name and avatar
(`member.Author`); the `deleted-user` handle prefix no longer marks one.
Anonymizing sets the handle to `deleted-user-<id>`, clears the display name,
avatar and bio, and makes the account `deactivated`, so it drops out of every
list of active accounts. A fifth status lost to the flag: it would need a
rebuild of `users` to change the CHECK, and a fifth case in every status rule
of 05 and 07. Three triggers hold it in the schema: an anonymized row's
handle, display name, avatar, bio, flag and status never change
(`users_anonymized_is_final`), the flag goes on only with the
`deleted-user-<id>` handle (`users_anonymized_handle`), and no row is inserted
anonymized. Status changes are decision writes and go through `internal/perm`
with their audit row (see "audit_log").

**Handle changes** (migration 0016, TKT-01M3NRP3W9, 05 "Changing a handle").
`handle_changed_at` is when the member last changed their own handle, null
until they first do; the 30-day cooldown counts from it. Only the member's
own change writes it, so an admin's rename neither waits for the cooldown
nor starts it. A column lost to reading `audit_log`, which would tie a rule
to an operational record's JSON, and to `max(released_at)`, which counts
admin renames. A rename goes through `identity.Service` alone
(`RenameUser`, `TestOnlyIdentityRenames`), which writes the new handle, a
`handle_releases` row for the old one and `user.renamed` in one
transaction; the old handle in the update's `WHERE` makes two renames that
race produce one.

**Member ids are never reused.** Thread logs name members by `id`, as the
author and actor of every record and in a post's mentions, so an id handed to
a second member would give them the first member's posts. `id` is
`INTEGER PRIMARY KEY AUTOINCREMENT` (migration 0010): the next member gets an
id above SQLite's high-water mark in `sqlite_sequence`, not above whatever
`max(id)` happens to be. A database restored from a backup older than the
logs lacks the members who joined since, and their ids. `ketju reindex`
raises the mark above the largest member id any log names, as author,
mention, or the actor of any record, edits, deletes and moderation included
(`db.RaiseMemberIDFloor`). It only ever raises it, so a reindex over a
database that is not behind the logs changes nothing. A post whose author
the database lacks renders as a deleted member (see "Projection tables").
The migration rebuilds `users` with foreign keys off, as SQLite's
documentation prescribes, and the runner checks `PRAGMA foreign_key_check`
before it commits any migration. (Maintainer decision, 2026-09-28,
TKT-01M3KB7FXC.)

A handle is 3 to 30 characters from `a-z`, `0-9`, `_` and `-`, starts with a
letter or digit, does not end in `-`, and is stored lowercased. That makes `COLLATE NOCASE`
exactly right, because it folds ASCII and a handle holds nothing else. Display
names stay free-form Unicode. Unicode handles with NFKC folding and a
confusables check lost: they buy non-Latin `@mentions` at the cost of
homoglyph spoofing, which a handle in a URL and a mention must never allow.
(Decided 2026-09-24 in Phase 1 grooming.)

The mention syntax reads a trailing `-` as punctuation, so `@bob-` mentions
`bob`, and a handle ending in `-` could never be mentioned. Registration
therefore refuses one. The rule applies to new handles only. A handle given
out before it stays valid, and the CHECK still accepts one, so no migration
is needed and a database that holds one keeps working. Such a handle still
cannot be mentioned, and renaming it waits for the handle change policy
(TKT-01M29BSPEN). Forbidding the dash lost to changing the mention syntax,
because reading `@bob-` as `bob-` would change what existing posts and
ordinary prose such as "@bob- see above" mention. (Decided 2026-09-28,
TKT-01M3ANX1NF.)

Registration also refuses reserved handles: those that collide with top-level
routes (`admin`, `api`, `assets`, `auth`, `blobs`, `events`, `healthz`, `me`,
`new`), and anything starting with `deleted-user`, the handle an anonymized
account is given (05). A handle is lowercased before either check, so the
comparison ignores case; only ASCII letters are lowercased, so a character
such as the Kelvin sign is refused rather than read as a `k`. The reserved
list is registration's rule, not the column's, and the CHECK does not carry
it. (TKT-01M29C8005.)

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

### handle_releases
```
id, handle (case-insensitive, the users.handle rule), user_id (nullable),
released_at, reserved_until
```
A handle that has left an account, and until when nobody else may take it.
Anonymizing writes a row for the old handle with no `user_id`, reserved for
90 days (`domain.HandleReservation`), in the transaction that anonymizes, and
registration answers "taken" for a handle some row still reserves
(TKT-01M29CKRFS, Terva review 1448). A row that names no user ties the old
handle to nothing, so nobody can learn which account it became. Renames
write the same rows with the user they redirect to (TKT-01M3NRP3W9): while
one is in force, a member asking for the old handle's profile is redirected
to the account, and a new mention of it mentions the account. Anonymizing
also sets `user_id` to null on the account's earlier rows, so none of its
old handles redirects to `deleted-user-<id>`. Rows are kept after their
reservation ends, because mentions need to know who held a handle when: an
old post's mention is resolved against them (07 "The response cache"), so
it never moves to the handle's next holder; nothing sweeps them. AUTHORITATIVE.

### identities
```
id, user_id, provider_key, subject (IdP 'sub'), email, email_verified,
issuer, last_login_at, raw_claims_json (last seen, for admin debugging),
blocked_at (nullable), blocked_by (nullable)
UNIQUE (provider_key, subject)
```
One user, many identities: account linking is a first-class operation.
`provider_key` refers to a provider block in `config.toml`, not to a table, so
an operator can remove a provider from config and its identities become
inert rather than orphaned.

`blocked_at` and `blocked_by` are the "block this subject" marker a denial
can set (05 "mode = approval" step 5), and a blocked identity's applications
are refused. The schema refuses them as well as the submit path:
`applications_identity_not_blocked` refuses inserting an application for a
blocked identity, and `applications_identity_not_blocked_update` refuses an
update (of `status` or `identity_id`) that leaves a blocked identity with a
pending application. Denying an application and blocking its identity pass
in either order. A block keeps `user_id` NULL: the triggers
`identities_blocked_has_no_user` (on an update of `user_id`),
`identities_block_needs_no_user` (on an update of `blocked_at`) and
`identities_insert_blocked` refuse a row that has both, so a block cannot be
undone by linking the identity to an account, and a member cannot be blocked
this way. Clearing both columns unblocks. Two columns lost against a separate
blocks table: a block has one state per identity, and the application form's
check would pay a join for nothing. (Migration 0011, TKT-01M29CKRDQ.)

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

### agents, agent_credentials
```
agents: user_id (pk, references users), bucket_burst (nullable, >= 1),
  bucket_refill_seconds (nullable, 1 to 604800), created_by, created_at
agent_credentials: id, user_id, token_hash (32 bytes, unique), created_by,
  created_at, last_used_at (nullable), revoked_at (nullable), revoked_by
```
An agent is a `users` row with `kind = 'agent'`. `agents` holds what only an
agent has: who provisioned it and its token-bucket overrides, where null means
the site default (`agents.write_burst`, `agents.write_refill_seconds`).

`agent_credentials` holds an agent's bearer credential (05 "Agent accounts"),
as the SHA-256 of the token, never the token. It is a table of its own
rather than rows in `sessions`, because `sessions` has the user and pre-user
CHECK, the expiry rules the sweeper applies, and the chokepoint scan, and a
credential would need an exemption from all three. There is no expiry;
revocation is the only way a credential ends, and a revoked row is kept. A
partial unique index, `agent_credentials_live`, allows one live credential
per agent. Writing one goes through `internal/auth/agentcred`, which checks
in the same transaction that the agent is active, and revoking goes through
`internal/perm`, which writes the audit row; `TestCredentialWritesHaveOneHome`
holds both. (Migration 0013, TKT-01M29P9RSW.)

### applications (approval mode)
```
id, identity_id (nullable: application may precede any login),
form_id, form_version, answers_json, status ('pending'|'approved'|'denied'|'withdrawn'),
user_id (nullable), decided_by, decided_at, decision_note, created_at
FOREIGN KEY (form_id, form_version) -> form_versions
UNIQUE (identity_id) WHERE status = 'pending'
```
An application names the form version it answered, so an admin's later edit
cannot change what an old answer meant. At most one application per identity
is pending (`applications_one_pending`).

`identity_id` is ON DELETE SET NULL, the one foreign key that is not NO
ACTION. Account linking removes identities (07 `DELETE
/settings/identities/{id}`), and so does anonymization (05). Under NO ACTION an
identity that had ever applied could never be removed. NO ACTION exists
because users are never deleted; identities are, and the application outlives
its identity with a null `identity_id`.

`user_id` is the member an approval created. It records that even after the
identity is removed, where following identity to user would not.

CHECKs keep the decision fields consistent with the status: `decided_at` is
set exactly when the status is not `pending` (a withdrawal records when);
`user_id` is set exactly when it is `approved`; and `decided_by` and
`decision_note` appear only on `approved` or `denied`, because a withdrawal
has no deciding moderator. A CHECK runs per statement, not at commit, so an
approval creates the user first and then sets `status` and `user_id` in one
UPDATE. (Migration 0011, TKT-01M29CKRDQ.)

### application_notes
```
id, application_id, author_id, body (1 to 2000 characters), created_at
```
The free-text notes moderators leave on an application (05 "mode =
approval" step 5). `decision_note` is the decision's one reason; these are the
discussion. Rows rather than a JSON array on the application, so a note is
appended without rewriting the application and names its author by foreign
key. (Migration 0011.)

### forms, form_versions
```
forms: id, key (e.g. 'registration'), title, active_version, created_at, updated_at
form_versions: form_id, version, schema_json, created_by, created_at
PRIMARY KEY (form_id, version)
```
Schema is a small declarative field list (see 05). Versioned so old
applications remain readable after the form changes.

`forms.active_version` is NOT NULL under a foreign key to `form_versions`
that is deferred to commit, because a form and its first version are
inserted in one transaction. A form committed without a version, or naming
one it does not have, is refused at commit, with no trigger behind it. A
`key` is lowercase ASCII letters, digits and `_`, starting with a letter.

A version is immutable once written. The triggers
`form_versions_no_update`, `form_versions_no_delete` and
`form_versions_no_replace` refuse UPDATE, DELETE, and an INSERT reusing an
existing (form, version), which is how `INSERT OR REPLACE` would otherwise
overwrite one, as `audit_log`'s do. An edit writes the next version and moves
`active_version`. `created_by` is null for the seeded version.

Migration 0011 seeds the `registration` form at version 1, because
registration defaults to approval and a fresh install needs the form on its
first start. It holds 05's example without the illustrative `interests`
select.

### forums
```
id, parent_id (nullable, one level of nesting max), slug, name, description,
position, visibility ('public'|'members'|'restricted'), archived, created_at,
updated_at
```
The one-level rule is enforced by triggers, so every write path gets it
(TKT-01M29BYYHG).

Settled in TKT-01M29CXK19, the admin forum screen:
- **Positions are sparse.** `position` orders siblings and means nothing
  else. A new forum goes 1024 after its last sibling. A move puts the forum
  at the integer midway between its new neighbours, with 0 as the bound
  before the first, so a move writes its own row and no other. Only when the
  neighbours have no integer between them are that parent's children
  renumbered to multiples of 1024, in their order, in the move's
  transaction. Rewriting every sibling on every move lost because it
  rewrites rows the move did not change. Fractional string keys lost
  because `position` is an integer and a forum list is tens of rows.
- **The slug is fixed at creation.** `/f/{slug}` is in every shared link,
  and no design covers redirecting an old forum slug. The name and
  description are editable.
- **No forum is deleted.** Archiving is how a forum is retired, since
  nothing here deletes by cascade.
- **Archiving is reversible.** A forum's `archived` is a column that no log
  record fixes, so an admin may unarchive. A thread's archive cannot be
  undone only because the log has no unarchive record.

### groups, group_members
```
groups: id, name, slug, description, is_system, created_at, updated_at
group_members: group_id, user_id, source ('admin'|'idp'), added_by, added_at
```
System groups: `everyone`, `members`, `moderators`, `admins`, seeded by
migration with fixed ids. A group an admin creates has a slug of 1 to 40
characters from a-z, 0-9 and `-`, starting with a letter or a number
(`domain.ValidateGroupSlug`, settled in TKT-01M29CXK1X; the slug is what an
IdP `group_map` names). A group can be deleted only when it has no stored
member and no permission row, in one statement, so nothing is deleted by
cascade. Triggers keep them from being deleted or losing their
slug or flag, and stop any other group becoming one. `source` records whether
an admin added the membership or an IdP claim mapping did; an `idp` row names
no adder.

### permissions
```
forum_id (nullable = global), group_id, permission, created_at
permission ∈ {read, create_thread, reply, upload, moderate}
UNIQUE (ifnull(forum_id, 0), group_id, permission)
```
Small, explicit, phpBB-like. Evaluation: union of the user's groups'
permissions for the forum, falling back to global rows.

The fallback is per group: a group with any row on the forum is governed by
those rows alone, and a group with none there uses its global rows, whatever
other groups have on the forum. A per-forum reading, where any row on a forum
silences every global row, lost because granting one extra group on a forum
would then quietly revoke the members default there. `role = 'moderator'`
grants `moderate` on every forum the user can see and implies nothing about
the `moderators` group, which grants per forum through rows like any other.
A viewer whose status is not `active` is evaluated as anonymous, whatever
their role. Evaluation lives in `internal/perm` (TKT-01M29C5Y2E), which also
owns every write that can change a decision so that the 60 s cache (02) is
invalidated when they commit; a check answers Allow, Deny (403), or Hidden
(404). A `restricted` forum without `read`, and a forum that does not exist,
are Hidden, so a forum's existence does not leak. A role or status change
applies from the user's next request: the viewer is a snapshot taken when a
request starts, so the session layer must read role and status from `users`
on every request rather than caching them in the session. An `archived` forum is
read-only. Decided 2026-09-28 (TKT-01M39788AT): `read` is unchanged, so the
forum stays visible to whoever could see it before. `create_thread`, `reply`
and `upload` are denied to everyone except admins, moderators included. That
matches an archived thread (03 `ThreadArchived`), except that an admin may
still post. The alternatives lost for these reasons. Hiding the forum from
non-moderators makes archiving a delete in disguise. A listing-only flag would
leave "archived" meaning nothing a member can rely on. Moderation stays open
(08, decided 2026-09-29): `read` and `moderate` are unchanged, so moderators
may still hide, restore, lock and pin there. Built in TKT-01M3N24RHD (Make
archived forums read-only in permission evaluation). `internal/perm` drops the
three posting permissions for an archived forum in the same place as every
other rule, so `Check`, `CanEach` and the grant preview agree, and
`Evaluator.SetForumArchived` is the one write of `forums.archived`, clearing
every Evaluator's cache when it commits. The posting routes do not rely on the
cache: they read `forums.archived` from the forum's row on every request and
answer `conflict` to everyone but an admin (07), so an archive written by
another process refuses the next post at once.

Membership of `everyone` is implicit for every viewer, signed in or not, and
membership of `members` is implicit for every active user; neither is stored
as rows, and a trigger refuses such a row. A fresh install seeds global rows granting `members` read,
create_thread and reply, and granting `everyone` read, which applies only
where a forum's `visibility` is `public`: a `members` forum ignores the
`everyone` grant, and a `restricted` forum is readable only through explicit
per-forum rows. Admins pass every check without rows (05). The result is a
forum a friend group can use the moment it is installed, and anything tighter
is an explicit grant. Read-only members and a private-until-granted install
both lost on the same ground: they make the first hour of a small forum
administration. (Decided 2026-09-24 in Phase 1 grooming.)

### audit_log
```
id, at, actor_id, action, target_type, target_id, details_json, ip
```
Every moderation and administration action. Append-only, and not only by
convention: triggers refuse UPDATE, DELETE, and an INSERT reusing an existing
id (which is how `INSERT OR REPLACE` would otherwise overwrite a row without
firing a DELETE trigger), and a test refuses any query or SQL literal in the
tree that tries any of them.

Rows reach the table only through `internal/audit`, and
`TestOneWayIntoAuditLog` refuses a call to the insert anywhere else. The
row is written inside the transaction that makes the change, so the two
commit together. A service writes it from its own write transaction. For an
action the thread log records, the projection writer writes it from the
transaction that projects the record, with the frame's timestamp as `at`.

- `actor_id` is the member who acted, or NULL when no account did: the
  command line, the configuration, a background job, the flag auto-hide. It
  is never 0. Such a row names what did the action in `details_json` as
  `"via"` (`"cli"`, `"expiry"`, `"auto-hide"`).
- `ip` is the request's address as the per-address rate limiter reads it,
  never `X-Forwarded-For` (docs/08). It is empty when there was no request,
  and for a row the projection writer writes, since the log carries no
  address.
- `target_type` and `target_id` name what the action was done to, in one
  format per type, so every row about one thing can be found through the
  `audit_log_target` index:

<!-- audit-targets:begin -->
| target_type | target_id |
|---|---|
| `user` | the user's id, in decimal |
| `forum` | the forum's id, in decimal |
| `group` | the group's id, in decimal |
| `flag` | the flag's id, in decimal |
| `application` | the application's id, in decimal |
| `thread` | the thread's id in base32, as URLs spell it |
| `post` | `<base32 thread id>/<ordinal>` |
| `setting` | the setting's key, such as `registration.mode` |
| `permission` | `<forum id>:<group id>:<permission>`, or `global:<group id>:<permission>` for a site-wide row |
| `form` | the form's key, such as `registration` |
| `identity` | the identity's id, in decimal |
| `blob` | the blob's SHA-256, 64 lower-case hex digits |
<!-- audit-targets:end -->

- Hard redaction (`ketju redact`, TKT-01M29CXJZ9, 03 "Hard redaction")
  writes, with the operator as actor and `"via": "cli"`: `post.redacted`
  against the post the operator named, with `"reason"`, `"journal"`,
  `"targets"` (every `{thread, ordinal, revisions}` cleared, copies
  included), `"references_dropped"`, `"blobs_planned"` and `"quotes"`, in
  the transaction that drops the redacted posts' blob references;
  `blob.deleted` against each blob it deletes, with `"journal"`, in the
  transaction that deletes the blob's row; and `thread.title_redacted`
  against the thread for a title, with `"reason"`, `"journal"`,
  `"frames_blanked"` and `"replacement_by_operator"`, and never any title
  text.

- `action` names what was done, such as `user.promoted` or
  `setting.changed`. The IdP group mapping writes `group.member_added` and
  `group.member_removed` against the user, one row per membership a sign-in
  changes (05 "Roles and groups"). Saving a form version writes
  `form.version_created` against the form, with `{key, version}` in
  `details_json` (05 "Forms").
- The admin users screen (TKT-01M29CXK1X) writes, each in the transaction
  that makes the change:
  - against the user: `user.promoted` for a change to `admin`, with
    `"from"`; `user.demoted` for a change from `admin`, with `"from"` and
    `"to"`; `user.role_changed` between `member` and `moderator`, with
    `"from"` and `"to"`; and `group.member_added` or
    `group.member_removed`, with the group, its id and `"source":
    "admin"`. A demotion leaves the user's `user.promoted` rows alone,
    because bootstrap reads them as "made admin once" (05 "Principles").
  - against the permission: `permission.granted` or `permission.revoked`,
    one row per change in a batch, with the group, the forum (null for
    global) and the permission.
  - against the group: `group.created`, `group.updated` and
    `group.deleted`.
- The account lifecycle (TKT-01M29CKRFS) writes, against the user and in
  the transaction that changes the status and deletes the account's
  sessions: `user.deactivated`, `user.suspended` with `"reason"` and
  `"until"` (Unix ms, or null for no end), `user.unsuspended`,
  `user.reactivated`, whose actor is the member coming back, and
  `user.anonymized`. Each carries the status it came `"from"`.
  `user.anonymized` names no handle; the rows written before it, a rename's
  old and new handle included, stay as they are (05 "Account lifecycle").
  An expired suspension's `user.unsuspended` has no actor and `"via":
  "expiry"` (TKT-01M29CP2QK).
- A handle change (TKT-01M3NRP3W9) writes `user.renamed` against the user,
  with `"from"` and `"to"`, the old and new handle, in the transaction that
  renames; the actor is the member for their own change and the admin for
  theirs.
- A suspension's posts (TKT-01M29CP2QK, 08 "Suspension") write, against the
  user, after the thread records are appended and projected:
  `user.posts_hidden` with `"suspension"`, the id of the `user.suspended`
  row, and `"posts"`, the number hidden; and `user.posts_restored` with
  `"posts"`. A run that changed no post writes neither. Each post also has
  the projection's `post.hidden` or `post.restored` row below, and a
  `post.hidden` written by a suspension carries its `"suspension"` too.
- Account linking (TKT-01M29CKRES) writes `identity.linked` against the
  user, with the member as actor, `"provider"` and `"identity"`, the new
  identity's id, in the transaction that writes the identity (05 "Login
  flow" step 3). It is the record of when the link happened: the identity's
  `last_login_at` stays empty until it first signs in.
- Removing a linked identity (TKT-01M29CCC0X) writes `identity.unlinked`
  against the user, with the member as actor, `"provider"` and
  `"identity"`, the removed identity's id, in the transaction that ends the
  sessions signed in through it and deletes it (05 "Sessions and CSRF").
- Application review (TKT-01M29CP2MG) writes one row per decision, in the
  transaction that makes it, with the deciding moderator as actor:
  `application.approved` against the application, with `"user_id"`,
  `"identity_id"`, `"handle"` and `"note"` (empty when none was given);
  `application.denied` against the application, with `"note"` and
  `"blocked"`, true only when the block was written; and
  `identity.unblocked` against the identity, with `"application_id"`, the
  application the block was lifted from. A block is part of its denial and
  writes no row of its own. A moderator's note on an application decides
  nothing and writes none.
- Moderation the thread log records (TKT-01M29CKRGS) is audited by the
  projection writer, one row per record, in the transaction that projects
  it (`internal/events/projection` `audit.go`). Every such row's details
  carry `"thread"`, the base32 id, and `"log_offset"`, the record's offset,
  and a row is looked for by target, action and offset before it is
  written, so a reindex replaying the log adds none. Which records write
  one is decided from the record and the rows it is projected against:
  - always, against the thread: `thread.locked` and `thread.unlocked` with
    `"reason"`; `thread.pinned` and `thread.unpinned` with `"scope"` and
    `"until"` (Unix ms, or null for no end); `thread.moved` with
    `"from_forum"` and `"to_forum"`, both forum ids, the first read from
    the thread row before the move is applied (TKT-01M29CKRH9);
    `thread.retagged`, a `ThreadTagged`, with nothing beyond `"thread"`
    and `"log_offset"`, for the reason a rename's row names no title: the
    log is the record of what the tags were (TKT-01M29CYSCP);
    `thread.archived` with `"reason"`, an admin's, as a lock's is
    (TKT-01M29CRJ64);
  - when the actor is not the thread's author: `thread.renamed`, against
    the thread, with nothing beyond `"thread"` and `"log_offset"`. It
    carries no title, old or new: the log is the record of what the title
    was, and a title copied into this append-only table could never be
    removed by a hard redaction (03 "Hard redaction", maintainer decision
    2026-09-29 on TKT-01M3NS147C);
  - when the actor is not the post's author: `post.edited` with `"reason"`
    and `"revision"`, `post.hidden` (a `Delete` with `by_moderator`, 03,
    with `"suspension"` when the `Delete` has one), `post.auto_hidden` (a
    `Delete` with `auto`, whose actor is 0, the system, TKT-01M29CP2PJ),
    `post.deleted` (one with neither, which the service never writes) and
    `post.restored` with `"reason"`, against the post. The service projects
    a moderator's edit, hide and restore, and the auto-hide, before it
    answers, so the row exists when the request returns (TKT-01M29CRJ5J).
    No row names a reporter or counts flags: moderators read those from
    `flags`.

  An author's own rename, edit, delete or restore writes none, nor does a
  `ThreadCreated`, its tags included, or `Post`. A record's
  actor 0 is written as NULL with `"via": "thread-log"`. An actor the log
  names but `users` lacks, as in a database restored from a backup older
  than the logs, is written as NULL with the log's id as `"log_actor"`, so
  the foreign key cannot hold a thread's projection back.
- Agent accounts (TKT-01M29P9RSW) write, against the agent and in the
  transaction that makes the change: `user.created` when an admin provisions
  one, with `"kind"` and `"handle"`; `agent.credential_issued`, with
  `"replaced"`, true when a live credential was revoked for it;
  `agent.credential_revoked`, with `"cause"`: `"admin"` for an admin's
  revocation, `agent.credential_issued` for a replacement, or the status
  action (`user.suspended`, `user.deactivated`, `user.anonymized`) that took
  the agent out of active, written beside that action's own row; and
  `agent.limits_changed`, with the new `"burst"` and `"refill_seconds"`, null
  for the site default. The key is `"cause"` rather than `"via"`, because
  `"via"` says what did an action when no account did.
- The admin forum screen (TKT-01M29CXK19) writes, against the forum and in
  the transaction that makes the change, each with the forum's `"slug"`:
  `forum.created` with `"name"`, `"parent"` (the parent's id, or null for a
  root) and `"visibility"`; `forum.updated` with `"name"`, `"description"`
  or both, each `{"from", "to"}`, for the fields that changed;
  `forum.moved` with `"from_parent"` and `"to_parent"` (ids, or null for
  the top level), the new `"position"`, and `"renumbered"`, true when the
  siblings were renumbered; `forum.visibility_changed` with `"from"` and
  `"to"`; and `forum.archived` and `forum.unarchived`. The last three are
  written by `internal/perm`, which clears the decision cache when they
  commit. A change to what the forum already has writes nothing.
- Slow mode (TKT-01M29CRJ50) writes `thread.slow_mode` against the thread,
  in the transaction that writes `thread_slow_mode`, with the moderator as
  actor, `"from"` (the thread's previous value, or null when it had none)
  and `"to"`. The service writes this row, not the projection, because
  slow mode appends no record.
- The moderator queue (TKT-01M29CRJ4E, 08 "Flags") writes `flag.resolved`
  or `flag.dismissed` against the post, one row per queue action, with
  `"flags"`, the ids it closed, `"action"`, `"note"` and `"post"`, what
  happened to the post.

Every action written today is in this table, with the target type it is
written against and the ticket that writes it. `audit.Actions` is the same
list, and the admin audit view offers it as its action filter
(TKT-01M29CP2R4, 07 "The audit log"). `TestActionsDocumented` holds the
two together, and `TestActionsCoverTheWriters` fails on an action a writer
names that the list lacks.

<!-- audit-actions:begin -->
| action | target_type | written by |
|---|---|---|
| `agent.credential_issued` | `user` | TKT-01M29P9RSW |
| `agent.credential_revoked` | `user` | TKT-01M29P9RSW |
| `agent.limits_changed` | `user` | TKT-01M29P9RSW |
| `application.approved` | `application` | TKT-01M29CP2MG |
| `application.denied` | `application` | TKT-01M29CP2MG |
| `blob.deleted` | `blob` | TKT-01M29CXJZ9 |
| `flag.dismissed` | `post` | TKT-01M29CRJ4E |
| `flag.resolved` | `post` | TKT-01M29CRJ4E |
| `form.version_created` | `form` | TKT-01M29CP2KE |
| `forum.archived` | `forum` | TKT-01M29CXK19 |
| `forum.created` | `forum` | TKT-01M29CXK19 |
| `forum.moved` | `forum` | TKT-01M29CXK19 |
| `forum.unarchived` | `forum` | TKT-01M29CXK19 |
| `forum.updated` | `forum` | TKT-01M29CXK19 |
| `forum.visibility_changed` | `forum` | TKT-01M29CXK19 |
| `group.created` | `group` | TKT-01M29CXK1X |
| `group.deleted` | `group` | TKT-01M29CXK1X |
| `group.member_added` | `user` | TKT-01M29CKRF9, TKT-01M29CXK1X |
| `group.member_removed` | `user` | TKT-01M29CKRF9, TKT-01M29CXK1X |
| `group.updated` | `group` | TKT-01M29CXK1X |
| `identity.linked` | `user` | TKT-01M29CKRES |
| `identity.unblocked` | `identity` | TKT-01M29CP2MG |
| `identity.unlinked` | `user` | TKT-01M29CCC0X |
| `permission.granted` | `permission` | TKT-01M29CXK1X |
| `permission.revoked` | `permission` | TKT-01M29CXK1X |
| `post.auto_hidden` | `post` | TKT-01M29CP2PJ |
| `post.deleted` | `post` | TKT-01M29CKRGS |
| `post.edited` | `post` | TKT-01M29CKRGS |
| `post.hidden` | `post` | TKT-01M29CKRGS |
| `post.redacted` | `post` | TKT-01M29CXJZ9 |
| `post.restored` | `post` | TKT-01M29CKRGS |
| `setting.changed` | `setting` | TKT-01M29BYYJ0 |
| `thread.archived` | `thread` | TKT-01M29CRJ64 |
| `thread.locked` | `thread` | TKT-01M29CKRGS |
| `thread.moved` | `thread` | TKT-01M29CKRH9 |
| `thread.pinned` | `thread` | TKT-01M29CKRGS |
| `thread.renamed` | `thread` | TKT-01M29CKRH9 |
| `thread.retagged` | `thread` | TKT-01M29CYSCP |
| `thread.slow_mode` | `thread` | TKT-01M29CRJ50 |
| `thread.title_redacted` | `thread` | TKT-01M29CXJZ9 |
| `thread.unlocked` | `thread` | TKT-01M29CKRGS |
| `thread.unpinned` | `thread` | TKT-01M29CKRGS |
| `user.anonymized` | `user` | TKT-01M29CKRFS |
| `user.created` | `user` | TKT-01M29P9RSW |
| `user.deactivated` | `user` | TKT-01M29CKRFS |
| `user.demoted` | `user` | TKT-01M29CXK1X |
| `user.posts_hidden` | `user` | TKT-01M29CP2QK |
| `user.posts_restored` | `user` | TKT-01M29CP2QK |
| `user.promoted` | `user` | TKT-01M29C800G |
| `user.reactivated` | `user` | TKT-01M29CKRFS |
| `user.renamed` | `user` | TKT-01M3NRP3W9 |
| `user.role_changed` | `user` | TKT-01M29CXK1X |
| `user.suspended` | `user` | TKT-01M29CKRFS |
| `user.unsuspended` | `user` | TKT-01M29CKRFS |
<!-- audit-actions:end -->

### notifications
```
id, user_id, kind, thread_id, ordinal, actor_id, at, read_at, application_id
```
Kinds: reply_to_you, mention, quoted, thread_you_watch, application_decided,
new_application (moderators).

`application_id` names the application the two application kinds are about,
since `thread_id` and `ordinal` name posts. It is required for exactly those
two kinds: the triggers `notifications_application_kind` (on insert) and
`notifications_application_kind_update` (on an update of `kind` or
`application_id`) refuse anything else. A row of either kind written before
the column existed could not name its application, so migration 0011
deletes any it finds. No earlier code wrote notifications of any kind, so
none is expected. (Migration 0011.)

The application kinds are written by `internal/service/notify`, in the
transaction of the act that causes them (TKT-01M29CP2N0): a submission writes
one `new_application` row for each active user with role `moderator` or
`admin`, and an approval writes one `application_decided` row for the member
it created, with the deciding moderator as `actor_id`. A denial or a
withdrawal writes none.

`read_at` records that the recipient read the notification, and nothing else
sets it: closing an application does not. `GET /me`'s `notifications` count
is exactly the unread rows `GET /notifications` serves (TKT-01M29CRJ2T). An
`application_decided` row counts while unread. A `new_application` row counts
only while its application is `pending` and the viewer is an active moderator
or admin, so the badge drops when an application is decided or withdrawn and
the row stays unread; the list serves such a row as read, so the two agree
(run decision below). A post notification counts only while the serving rule
below admits it. `MeCounts` offers the post rows grouped by thread, and the
threads service's `VisibleNotifications` decides them for the badge and the
list alike, so neither can count what the other hides.

The post kinds are written by the notification generator, a subscriber on
the event bus (TKT-01M29CRJ2T). These run decisions bind it:

- **Decided by the run (2026-10-01, maintainer approved):** one post
  gives one member at most one notification. When it would give several,
  the kept kind is the strongest: `mention`, then `reply_to_you`, then
  `quoted`, then `thread_you_watch`. A mention is the author addressing
  the member by name. A reply answers something they wrote. A quote
  repeats their words. A watch is ambient. Alternatives: a reply ranked
  first, as in Discourse, lost because a post that names someone is
  written to them whatever it answers; one row per kind lost because the
  ticket that asked for this collapse exists to stop three alerts for one
  post.
- **Decided by the run (2026-10-01, maintainer approved):** a member
  has at most one unread `thread_you_watch` per thread. It names the first
  post they were alerted to, and later posts add nothing until they read it,
  so the link lands where the new posts begin. Alternatives: one row per
  post lost because an agent thread at twenty posts a minute (06 "Live
  updates") would bury every other alert; a row with a running count lost
  because the count needs a column and goes stale whenever a post is
  removed.
- **Decided by the run (2026-10-01, maintainer approved):** an `Edit`
  notifies the members its `mentions` names, as `mention`, and nothing else.
  A member already notified about the post is not notified again, though an
  unread row may move to a stronger kind. This way a mention added to fix a
  forgotten `@` reaches the person. Alternatives: edits notify nobody,
  which lost because the author believes they addressed someone who never
  hears of it; every edit notifying everyone again lost because it repeats
  alerts.
- **Decided by the run (2026-10-01, maintainer approved):** a post
  notification is served (listed and counted) only while three things
  hold. The post is visible to every reader, so not deleted, hidden, hidden
  pending review, redacted or moved. The viewer may read the thread's forum
  at that moment. The thread's effective level for the viewer is not `mute`
  at that moment, so muting a thread also silences what it already sent.
  All three are checked when the list is served, and moderators are held to
  the same rule. Rows are never deleted for any of this, so a restored post
  or an unmuted thread brings its notification back. A `new_application`
  row is served only to a viewer whose role is moderator or admin at that
  moment. Once its application is no longer `pending`, the list serves it
  as read without writing `read_at`. That makes it agree with the badge,
  which stops counting it at the same moment.
  No notification carries any of the post's text. It carries the kind, the
  actor, the thread's title, the forum and the ordinal. Alternatives: let
  moderators see notifications of removed posts lost because an alert asks
  the reader to go and read, and a moderator reaches removed posts through
  the queue and the moderator lens instead; an excerpt of the body lost
  because it is a second path for post text, and that path would have to
  follow edits, removals and redaction the way the thread page does.

Generation skips the post's author, any member who is not `active`, any
member who cannot read the forum when the post is processed, and any member
whose effective level on the thread is `mute` (see `watches`). It also skips
two kinds of record. The first is one written at or before the moment the
generator's migration was applied (its `schema_migrations.created_at`), so
installing it alerts nobody to history, even in a range the projection had
not reached by then. The second is one more than seven days older than the
moment it is processed, which covers a database restored without the
generator's progress. Progress is kept
per thread in `notification_progress` (TKT-01M29CRJ2T). That table is
authoritative and is not a projection: `ketju reindex` leaves both it and
`notifications` alone.

Migration 0024 adds the table and the indexes the generator and the list
need. `notifications_post` is unique on `(user_id, thread_id, ordinal)` for
the post kinds, and the generator upserts on it, moving an unread row only to a
stronger kind, leaving a read row as it was read, and never touching `read_at` or `at`, so a replay converges (Terva review 1781).
`notifications_watch_unread` is unique on `(user_id, thread_id)` for an
unread `thread_you_watch`, which is the one-unread-watch rule.
`notifications_recent` on `(user_id)` serves the list's keyset on `id`.
`watches_by_thread` and `watches_by_forum` find a post's watchers without a
scan. The generator processes a thread only up to the length its projection
has reached, so it never reads `post_meta`, `threads` or `watches` for a
range they do not yet describe, and holds the thread until the projection
catches up.

### watches
```
user_id, thread_id (nullable), forum_id (nullable), level ('watch'|'mute')
```
Exactly one of thread and forum, and at most one watch per user per thread
and per user per forum.

**Decided by the run (2026-10-01, maintainer approved):** a thread's
effective level for a member is the thread's own row when it has one, then
the row for the forum the thread is in now, then none. So a muted thread in
a watched forum is muted, and a watched thread in a muted forum is watched.
A forum's row covers that forum's own threads, not its children's, as
`GET /forums` counts each forum's own (07). `mute` removes the thread from
every notification kind and from every unread count: `/me`, the forum
index, the thread list's badge and the cross-forum lists. `watch` adds
`thread_you_watch`. Alternatives: forum before thread lost because it makes
mute useless in a watched forum, the case the ticket names; a forum row
covering child forums lost because nothing else in the forum tree inherits
down, and a member can watch a child forum directly.

`domain.EffectiveWatch` is the rule in Go, and the queries state it as a
coalesce of the thread's row, then the forum's, then the empty string; tests
hold the two together. A member sets either level through the two watch
routes in 07, and an empty level deletes the row (TKT-01M29CRJ29).

### read_positions
```
user_id, thread_id, last_read_ordinal, last_read_at
```
One row per (user, thread) ever visited. Combined with the thread's posts in
`post_meta` this yields unread counts without a per-post table. A user-level
`mark_all_read_at` timestamp handles "everything before this date is read"
so we do not need rows for threads never opened.

The rule, as the `UnreadCount` query applies it (TKT-01M29BYYHQ): a post is
unread when its ordinal is past `last_read_ordinal` (0 with no row) and it was
written after `mark_all_read_at`. A post written at the mark's exact
millisecond counts as read. The time is revision 1's, so an edit does not make
an old post unread, and a read position only moves forward.

The thread page reads the two together as one *read boundary* (`ReadBoundary`,
TKT-01M29CTY7K): the stored position, moved to just before the first post past
it written after the mark, or to the last post when there is none. Posts are
appended in time order, so the posts after the boundary are exactly the unread
ones, and the thread list's rows compute the same column.

### flags
```
id, thread_id, ordinal, reporter_id, reason, note, created_at,
status ('open'|'resolved'|'dismissed'), resolved_by, resolved_at, resolution_note,
audit_id, audit_pending
```
Reports on posts; feeds the moderator queue (08). A flag is resolved or
dismissed in place and never deleted (a trigger refuses it). `other` requires
a note. An open flag has no resolver, and a closed one names who and when.
A partial unique index, `flags_one_open_per_reporter` on `(thread_id, ordinal,
reporter_id) WHERE status = 'open'` (migration 0014, TKT-01M29CP2PJ), keeps a
member to one open flag per post. A repeat adds no row, so the open count the
auto-hide threshold reads is a count of reporters. Once a moderator closes
their flag, a member may flag the post again.

Migration 0015 (TKT-01M29CRJ4E) adds two columns for the moderator queue.
`audit_id` names the `audit_log` row that records the flag's closing, and
the queue sets it in the transaction that writes that row. A flag closed any
other way, or before the migration, keeps it null, and a null `audit_id`
means nothing on its own. `audit_pending` is null, `dismiss` or `resolve`.
It is set only by the queue's restore, in the transaction that dismisses the
flags before the `Restore` append, because the row may be written only once
the append succeeds. Writing the row clears it, and so does reopening the
flag. A `CHECK` allows it only on a dismissed flag with no `audit_id`.
Recovery reads only flags with the marker set (08 "Flags").

### drafts
```
user_id, thread_id (nullable = new-thread draft, then forum_id), title
(nullable, new-thread drafts only), source, reply_to, updated_at,
revision (0 or more), deleted (0 or 1),
attachments (JSON array of blob hashes, '[]' for none)   -- 0021
UNIQUE (user_id, thread_id) WHERE thread_id IS NOT NULL
UNIQUE (user_id, forum_id) WHERE thread_id IS NULL
```
Composer autosave, one per user per thread, and one new-thread draft per user
per forum. A draft holds exactly one of thread and forum, and a `reply_to`
only with a thread. The first version of this section declared
`PRIMARY KEY (user_id, thread_id)`, but a nullable key column does not enforce
uniqueness in SQLite and could not key a new-thread draft by forum, so the
two partial indexes replace it (TKT-01M29BYYHQ). The API serves reply
drafts at `/drafts/{thread_id}` and new-thread drafts at
`/forums/{slug}/draft` (07 "Drafts"). A new-thread draft saves its title as well as its body. Decided 2026-09-28
(TKT-01M3K9XBJA, Autosave new-thread drafts, and decide whether the title is
saved): a draft restored on another device should come back whole. The
`title` column is added by that ticket's migration (0018), and a `CHECK`
keeps it null on a reply draft.

`revision` counts a key's writes and only grows: a save adds one, starting
from 1, and so does deleting a live draft. A delete keeps the row as a
tombstone, `deleted = 1`, whose `CHECK` requires an empty `source` and a
null `reply_to` and `title`. The key's revision therefore survives a delete,
and a draft started again answers a higher revision than any copy of the old
one. The revision is computed in SQL from the stored value, so no clock
enters it. A client orders a local copy of a draft by it (07 "Drafts",
TKT-01M3NS1931). Tombstones are never swept, because a sweep would restart
the count. There is at most one per thread or forum a member has ever
drafted in, and blob GC finds no attachment in an empty source. Both columns
come from migration 0018 (TKT-01M3K9XBJA), which sets every draft stored
before it to revision 1.

`attachments` holds the composer's files as the attach check answers them:
each hash once, in the order sent, at most ten (07 "Drafts",
TKT-01M3V73DP6). A `CHECK` keeps it a JSON array. A tombstone holds none, and
two triggers hold that, because SQLite cannot add it to `deleted`'s `CHECK`
without rebuilding the table. A draft adds no reference to a blob, so blob GC
and `ketju redact` keep a blob any draft's attachments name (03 "Blob garbage
collection"). The column comes from migration 0021, whose default reads every
draft stored before it as one with no attachments.

### blobs
```
hash (pk), size, mime, original_name, uploaded_by, uploaded_at,
refcount, status ('ok'|'quarantined'), unreferenced_since,
width, height                -- an image's, null otherwise (0020)
```
`hash` is the lowercase hex SHA-256 the file is stored under (03).
`unreferenced_since` is set whenever `refcount` is zero (from the upload
itself, since a new blob has no references yet) and is null otherwise, a
CHECK holding the two together, so garbage collection can wait "seven days at
zero" rather than seven days from upload. `BlobForServing` is the only lookup the serve path uses, and
it does not find a quarantined blob.

`original_name` and `uploaded_by` are the first upload's, kept for an
operator. What a post shows is `blob_uploads`'. An upload of bytes already
stored sets `unreferenced_since` to its own time while `refcount` is zero,
so the collector's seven days start again (TKT-01M29CXK0K).

### blob_uploads
```
hash, user_id, original_name, uploaded_at
PRIMARY KEY (hash, user_id)
hash REFERENCES blobs ON DELETE CASCADE
```
Who has uploaded each blob, and the name they first gave it (migration
0020, TKT-01M29CXK0K). A post may attach only a blob its author has a row
for, and its attachment shows that row's name. A row is written once and
never updated, so uploading the same bytes again under another name renames
nothing already posted, and a second uploader never learns the first one's
name (docs/07 "Attachments"). The cascade is the schema's one deleting
foreign key: a blob row goes only when the collector or `ketju redact`
finds nothing naming it, and its upload rows go in the same transaction.

### settings
```
key (pk), value_json, updated_at, updated_by
```
Runtime-changeable settings (site name, registration mode, slow-mode
defaults). Bootstrap and provider configuration stay in `config.toml`
because they must be readable before the database exists.

The key list is closed. `internal/settings` defines each key with its type
and default, and `settings.Set` refuses an unknown key or an ill-typed value
before anything is stored, writing an audit row (`setting.changed`, with the
old and new values) in the same transaction. A key with no row reads as its
default, so a fresh database needs no seed rows. `ketju admin setting`
reads and sets them from the command line through the same registry (05).
The table itself only
insists that `value_json` is valid JSON. The list below is checked against
the code by `TestKeysDocumented`:

<!-- settings:begin -->
| Key | Default | Meaning |
|-----|---------|---------|
| `agents.write_burst` | `20` | Writes an agent may make at once before its token bucket refuses (08) |
| `agents.write_refill_seconds` | `30` | Seconds for an agent's bucket to regain one write (08) |
| `flags.auto_hide_threshold` | `3` | Open flags on a post that hide it pending review (08) |
| `posts.delete_window_seconds` | `900` | How long after posting an author may delete a post (08) |
| `posts.edit_window_seconds` | `86400` | How long after posting an author may edit a post (08) |
| `posts.grace_edit_seconds` | `300` | Edits this soon after posting display as the original revision (08) |
| `public_edit_history` | `true` | Whether members, not only moderators, may read a post's revisions (08) |
| `registration.allow_local_passwords` | `false` | Local passwords besides a provider (05) |
| `registration.mode` | `"approval"` | `"open"` or `"approval"`; invite is a later phase and is refused until then (05) |
| `site_name` | `"Ketju"` | Shown in the header and page titles |
| `slow_mode.default_seconds` | `0` | Minimum seconds between one member's posts in a thread; 0 is off (08) |
| `threads.rename_window_seconds` | `86400` | How long after starting a thread its author may rename it; moderators may at any time (08) |
<!-- settings:end -->

Two defaults are not stated in 05 and 08 and were chosen in TKT-01M29BYYJ0.
`registration.mode` defaults to `approval`, the v1 mode and the one that
admits nobody unreviewed. `site_name` defaults to the product's name. The
agent token-bucket defaults were the maintainer's decision of 2026-09-29
(TKT-01M29P9RSW, 08 "Rate limits"), and so was the rename window's 24 hours,
the post edit window, since a title is the opening post's headline
(TKT-01M29CKRH9).

The default page size is not a key. 25 posts a page appears in shared
URLs, so the API and the client hold it as a constant (10 item 13, 07
"Preact client"), and a member's own page size of 10 to 50 is their
setting, not the site's (06). The admin settings screen lists the keys
above and nothing else (07 "Site settings").

### thread_slow_mode
```
thread_id (pk), seconds, set_by, set_at
```
A thread's own slow mode, set by a moderator (08 "Primitives",
TKT-01M29CRJ50). `seconds` is the minimum interval between one member's
posts in the thread, 0 to 86400, where 0 is off. A row overrides
`slow_mode.default_seconds`, and an explicit 0 turns slow mode off under a
non-zero default. A thread with no row takes the default. The bound of a day
guards against a typo and is not a policy.

The table is authoritative. Slow mode is a live control, not part of the
thread's history, so the log does not record it and `ketju reindex` neither
clears nor rebuilds it. `thread_id` is a plain integer with no foreign key,
because `threads` is a projection that reindex empties. Each change writes
`thread.slow_mode` to `audit_log` in the same transaction (see there).

## Projection tables (rebuildable)

Projection tables carry no foreign keys, to `users` or to anything else, and
their author and forum columns are plain integers. `ketju reindex` rebuilds
them from the logs alone, into a database whose users may have come from an
older backup or from none, and a row naming a user the database lacks is the
reader's to render rather than a write to refuse (settled in Phase 1
grooming). The reader draws such an author as a deleted member, with the
placeholder handle `deleted-user-<id>` an anonymized account has and no
display name or avatar, and never as anyone else. Reindex keeps that true:
see "Member ids are never reused" under `users`. (Maintainer decision,
2026-09-28, TKT-01M3KB7FXC.) Reindex empties the tables and replays the logs into them; it does
not drop them, because their DDL lives in the migrations and nowhere else.
`db.ProjectionTables` is the list, and a test holds it equal to the tables
whose schema carries the PROJECTION marker (TKT-01M29BYYJ8).

### threads
```
id, forum_id, slug, title, author_id, created_at,
post_count, last_post_at, last_post_author_id, last_post_ordinal,
locked, pinned_scope, pinned_until, archived,
log_length_projected,     -- how far into the log this row reflects
live_post_count           -- post_meta rows neither deleted nor moved
```

`post_count` is the latest ordinal, so it counts deleted and moved posts.
`live_post_count` counts the thread's `post_meta` rows with `deleted = 0`
and `moved = 0`, and triggers on `post_meta` keep it (migration 0009,
TKT-01M3B6A68B). A thread row inserted after its `post_meta` rows counts
them, so the column is a count of `post_meta` whatever order the rows arrive
in, and the projection writer never sets it.

`INDEX (forum_id, last_post_at, last_post_ordinal)` answers, per forum, the
threads with a post after a moment by a seek rather than a scan: GET /me's
unread count for a reader who has used "mark all read" (migration 0007,
TKT-01M29CA99R).

Migration 0023 (TKT-01M29CTY7K) adds three for the cross-forum lists.
`threads_activity (last_post_at)` and `threads_created (created_at)` let
`/unread` and `/new` walk the whole site newest first, from the reader's
mark up to the cursor, and stop at a page, rather than sort every
qualifying thread across the reader's forums.
`threads_forum_fresh (forum_id, created_at, last_post_ordinal)` is GET /me's
new count for a reader with a mark: per forum, the threads with a post
started after it, from index entries alone. It is not folded into
`threads_forum_created`, whose keyset needs `created_at` beside the rowid.

### forum_thread_counts
```
forum_id (pk), threads     -- threads in the forum with at least one post
live_posts                 -- live_post_count summed over those threads
```
GET /me's unread count for a reader who has never used "mark all read" is
every thread with a post, per forum, and the shell asks on every page load.
This table answers it without reading `threads`. Triggers on `threads`
maintain it rather than the projection writer, so every path that changes
`threads`, reindex's clear included, keeps it right; a forum with no such
thread has no row. Reindex also clears it by name, so a count that had
drifted does not survive one.

`live_posts` is GET /forums's post total (migration 0009). It moves with
its thread: the same triggers take a thread's `live_post_count` out of the
forum it leaves and into the one it joins, and follow every change to it.
It is not named `posts` because that word names a per-post table to the
test that keeps the forum index's queries from scanning one.

### thread_tags
```
thread_id, tag
```
The thread's tags, replaced wholesale by `ThreadCreated` and `ThreadTagged`
(03). **Decided by the maintainer (2026-10-02, TKT-01M3M4KVNW):**
- A tag is lowercase ASCII words joined by single hyphens,
  `[a-z0-9]+(-[a-z0-9]+)*`. Uppercase input is folded to lowercase, so
  `Garden` and `garden` are one tag, and the row holds the folded string.
- A tag is 1 to 24 characters, and a thread carries at most 8.
- Tags are free text: any tag that passes these rules is accepted. A
  moderator corrects a thread with a retag (08). A curated list per forum
  may come later.
- Who may set a tag, and the reserved tags, are in 08 "Thread tags".

These rules bind what the service writes, not what a reader accepts: a log
written before them, or by the e2e seed, is read as it stands.

**Decided by the run (2026-10-02, pending maintainer review):**
- A request that names a tag twice, before or after folding, keeps one
  copy and is not refused. Alternatives: refusing it with
  `validation-failed` lost because the author meant the tag once and
  nothing is lost by keeping it once; a post's attachments drop repeats the
  same way (07).
- Members see a thread's tags sorted. The service writes them sorted into
  the record, and the projection and the API list them `ORDER BY tag`, as
  they already do. Alternatives: the order the author gave lost because
  this table keeps no order, a retag would reorder them anyway, and one
  order reads the same in every row and on the thread page.

### post_meta
```
thread_id, ordinal, author_id, created_at, current_revision, edited_at,
reply_to, deleted, moved, log_offset
PRIMARY KEY (thread_id, ordinal)
INDEX (author_id, created_at)
INDEX (thread_id, reply_to)
```
`log_offset` is the offset of the post's current revision's record, the
same fact as the index entry's `Offset`: a Post or Edit sets it and a Delete
or Restore does not. Migration 0004's column comment calls it the record last
projected into the row; that comment is wrong, and stays in the file only
because an applied migration is checksummed (TKT-01M29C2WW5, review 389).
Idempotence comes from `projection_state`, not from this column.

Tiny rows. Exists for cross-thread questions: a user's post history,
"replies to post X" reverse lookups when the thread is not open, moderation
lists. The per-thread index file already answers within-thread questions.

### mentions
```
thread_id, ordinal, mentioned_user_id
```

### post_attachments
```
thread_id, ordinal, hash, position
PRIMARY KEY (thread_id, ordinal, hash)
INDEX (hash, thread_id, ordinal)
```
The hashes each post's `Post` record lists in `attachments`, in order
(`position`), for the serve path's question "which posts name this blob"
(migration 0020, TKT-01M29CXK0K). A `Redacted` record removes the post's
rows. Nothing else changes them, since an `Edit` has no attachments field
(03), and a thread page reads an edited post's attachments from here. The
refcount on `blobs` is the collector's bookkeeping; this table is what
access is checked against.

### post_quotes
```
thread_id, ordinal, quoted_ordinal
PRIMARY KEY (thread_id, quoted_ordinal, ordinal)
INDEX (thread_id, ordinal)
```
Quote backlinks: which posts of its thread each post quotes (migration
0025, TKT-01M3YME9AZ). The thread log carries no quotes field, and adding
one would change the frozen `Post` record (03), so the projection writer
finds them by rendering the current revision's source with no resolver
(`render.QuotedOrdinals`). Every quotation is reported, nested ones
included, each once, and never the post itself. The rows depend on the
source alone, so a replay or a reindex writes the same ones.

- A `Post` replaces the post's rows; a split or merge copy (one with
  `origin`) has none.
- An `Edit`, grace edits included, replaces them from its source.
- A `Redacted` post marker removes them, as the blanked source quotes
  nothing.
- `Delete` and `Restore` leave them.

Whether a quoted post exists, and whether a quoting post is shown, is
decided when the rows are read. The thread page's `quoted_by` (07) lists
a quoting post only while its `post_meta` row is neither deleted nor moved
and its index entry is visible. The notification generator reads a new
post's rows for `quoted`, keeping the earlier posts it quotes, instead of
rendering the source again.

A database projected before migration 0025 has no rows for the posts it
already holds. The migration therefore moves every `projection_state` row
back to the end of its log's file header, and the next start's catch-up
replays each log from the beginning over the rows already there, which
fills the table. Every apply sets absolute values and an audit row is
written once per record, so the replay changes nothing else. The notifier
processes a thread only up to the length the projection holds, so it waits
for the replay. The cost is one replay of every log at the first start
after upgrading (Terva review 1998).

### search (FTS)
Full-text index over `(thread_id, ordinal, revision, title, source_plain)`.
Kept in the same database if the engine supports FTS (SQLite FTS5) or in a
separate pure-Go index (bleve) otherwise. Always rebuildable.

ncruces v0.35.6 has FTS5 only on a connection that `ext/fts5` registers it
on. Checked on 2026-10-01: without `fts5.Register`, `CREATE VIRTUAL TABLE …
USING fts5` fails with "no such module: fts5"; with it, the table, `MATCH`
and `snippet()` work on SQLite 3.53.4. `ext/fts5` is a package of the module
already pinned, so `go.mod` does not change. The call belongs in `openPool`
in `driver.go`, the one file that may import the driver, so every connection
has it, the read-only ones included (TKT-01M29CRJ3B).

**Decided by the run (2026-10-01, maintainer approved):** what the
index holds, for TKT-01M29CRJ3B (Build the FTS5 search index and indexer).
- **Only what every reader of the thread may see.** There is one row per
  post that is not deleted, hidden, hidden pending review, moved or redacted
  (`EntryFlags.Visible`), holding its current revision only. An edit
  replaces the row. A post that stops being visible loses its row, and one
  restored gets it back. A thread's title is a row of its own, ordinal 0,
  and a thread with no post yet is left out when served, as the thread page
  leaves it out. Alternatives: indexing every post and
  filtering per viewer lost, because it keeps removed text in the database
  and puts every removal rule into one query; a moderator reviews a hidden
  post in the queue and on the thread page, not in search. Indexing every
  revision lost, because a snippet could then show text a member may not
  read when `public_edit_history` is off, and the index would grow with
  every edit.
- **Shape.** `search_docs (rowid, thread_id, ordinal, revision, author_id,
  created_at, title, source_plain)`, unique on `(thread_id, ordinal)`, is
  the external content table of the FTS5 table `search (title,
  source_plain)`. `author_id` and `created_at` come from the post's index
  entry and serve the author and date filters. `search_state (thread_id,
  log_length)` records how far each thread is indexed, as `projection_state`
  does for projections. `search_meta` holds one number, the version of the
  plain-text extraction and tokenizer that built the index. A build that
  finds another version, or none, empties the whole index and records its
  own version in one transaction. `serve` does this before its listener
  opens, so no row of another version is ever served. It then re-indexes
  every thread in the background, search
  says it is incomplete meanwhile, and a restart mid-pass resumes rather
  than starting again. Alternatives: one FTS5 table with `UNINDEXED` key
  columns lost, because removing a post's row by `(thread_id, ordinal)`
  would scan the table; the title in every post's row lost, because a
  rename would rewrite every row of the thread.
- **Not a projection table.** The search tables are listed in
  `db.SearchTables`, not in `db.ProjectionTables`. `ketju reindex
  --projections` leaves them alone and `ketju reindex --search` rebuilds
  them (03). They hold the plain text of visible posts, so a database backup
  holds that text too. Content is still restored from the logs, never from
  a backup.
- **Tokenizer.** FTS5's built-in `unicode61 remove_diacritics 2`, with no
  stemming. Alternatives: `porter` lost, because it stems English only and
  mangles other languages; a Go tokenizer lost for now, because modernc, the
  fallback driver, cannot load one (`spikes/sqlite-driver/README.md`), and
  multi-language stemming is under "Later" in 09.

As built (TKT-01M29CRJ3B, migration 0022): `search_docs`'s key column is
`id`, the alias of its rowid and the FTS5 table's `content_rowid`. The
indexer is `internal/events/search`, the plain text is `render.Plain`, and the
query is `views.Search`. Split and merge records are reserved and have no body
in this build, so no post is marked moved yet. Until TKT-01M29CXJZZ gives
them bodies, the indexer skips them, as the index does.

**Decided by the run (2026-10-01, maintainer approved):** what the
plain text leaves out, for TKT-01M29CRJ3B.
- **An attributed quotation (`[quote=#N]`) is left out of the quoting
  post's row.** A plain blockquote keeps its text. The quotation copies
  another post of the same thread, and that post has its own row while it is
  visible. Once that post is deleted, the page shows the quotation without its
  text (`render` drops it). Indexing the copy would let search find, and its
  snippet show, text the page no longer shows. Alternative: index the
  quotation like any blockquote. It lost because the indexer cannot tell
  whether the quoted post is still visible: it would have to re-index every
  quoting post whenever a quoted post is deleted.
- **An autolink is left out**, `<https://…>` and a bare `www.…` alike,
  because its text is its destination. A link keeps its text, and an image
  keeps its alt text. Alternative: keep an autolink's text because a reader
  sees it. It lost because the index then holds URLs, which the ticket's rule
  "link destinations stripped" excludes, and one rule for every destination
  is easier to hold than an exception.

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

Measured for the cross-forum lists (TKT-01M29CTY7K, `BenchmarkCrossLists10k`
in `internal/store/db/views`, 2026-10-01, one core of a Ryzen 9 5950X under a
load average near 9): 10 000 threads in 50 forums, five posts each, and a
reader with 1 000 read positions, 100 watches, 20 mutes and 200 threads
posted in. A first page of 25 rows, and GET /me's count statement:

| Reader | `/unread` | `/new` | `/participated` | `/watching` | `MeCounts` |
|--------|-----------|--------|-----------------|-------------|------------|
| no mark | 0.5 ms | 0.5 ms | 1.0 ms | 0.7 ms | 5.4 ms |
| mark halfway through the site | 0.6 ms | 0.6 ms | 1.2 ms | 0.8 ms | 6.0 ms |
| no mark, all but the 30 oldest threads read | 9.8 ms | 10.2 ms | 1.2 ms | 0.7 ms | 37 ms |

The last row is the walk's worst case: `/unread` and `/new` read the whole
site through their index to fill one page, since everything recent is read.
A mark bounds the walk from below, so a reader who uses "mark all read" never
pays it. `MeCounts` grows with the reader's own read positions (9 970 in the
last row), as it did before the lists: on the same site it measured about 5 ms,
4.5 ms and about 31 ms before `new` was added. Without
`threads_activity` and `threads_created`, `/unread` and `/new` would sort
every qualifying thread on every page instead.
