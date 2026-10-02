# 08 — Moderation

Small communities need few tools, but each must be reliable, reversible, and
recorded. Every action here appends to `audit_log` and, when it concerns a
thread, is also a record in that thread's log (03) so the history travels
with the content.

## Primitives (v1)

| Action | Who | Where recorded | Reversible |
|--------|-----|----------------|-----------|
| Edit any post | moderator | thread log `Edit` (actor = moderator) | via history |
| Soft-delete / restore post | author (own, within window) or moderator | `Delete`/`Restore` | yes |
| Lock / unlock thread | moderator | `ThreadLocked`/`ThreadUnlocked` | yes |
| Pin / unpin (forum or global, optional expiry) | moderator | `ThreadPinned`/`ThreadUnpinned` | yes |
| Rename / retag / move thread | moderator (author may rename own thread within window) | thread events | yes |
| Split posts into a new thread / merge | moderator | `PostsSplitOut`/`PostsMergedIn` | by reverse operation |
| Archive thread | admin | `ThreadArchived` | no: there is no unarchive record in v1 (03) |
| Slow mode on a thread (min seconds between posts per user; off by default) | moderator | `settings` per thread in DB, not log | yes |
| Suspend / unsuspend user | moderator | `audit_log`, `users.status` | yes |
| Approve / deny application | moderator | `applications`, `audit_log` | deny → re-apply |
| Anonymize user | admin | `audit_log` | no |
| Hard-redact post body | admin, CLI only | `audit_log` + rewritten log | no |

**Restoring a moderator's delete.** Decided 2026-09-28 (TKT-01M29CRJ5J): when
a moderator deletes someone else's post, the post is `hidden-by-mod` rather than
`deleted`, and only a moderator may restore it. The author still sees the post,
marked as removed by a moderator. The author's restore, edit and delete are
`forbidden` (07 "Edit, delete and restore"). The alternatives lost for these
reasons. Letting the author restore makes a moderator's delete a suggestion.
A restore request would need a flow of its own, and the moderator queue and
the flag trail already give an author someone to ask. A post an author deleted
themselves is still theirs to restore within the window.

**Suspension** (TKT-01M29CP2QK, built on TKT-01M29CKRFS). The rules come
from 05 "Account lifecycle". Moderators suspend members, and admins suspend
anyone except themselves. The last active admin cannot be suspended. The
member reads the reason and any end date at sign-in, and is never shadow
banned. A suspension ends the account's sessions in the transaction that
writes it.
- **Hiding the posts is optional.** Each of the account's visible posts gets
  a moderator's `Delete` carrying the id of the suspension's `audit_log` row
  (03 `Delete.suspension`). Tombstones, the author's view, forum totals,
  profile counts, the audit rows and the response cache then treat it as any
  moderator's hide. It is written to the logs, so it survives a database
  rebuild. The suspension commits first, because a transaction cannot span
  the logs. The hide is idempotent, so running it again finishes one that
  stopped part-way. It passes over a post hidden pending review, which stays
  in the moderator queue for a moderator to decide (TKT-01M3R10VP3).
- **A hide reaches only what its moderator may moderate.** A thread in a
  forum the moderator cannot moderate or see is passed over, and the count
  says nothing about it. An admin's hide reaches every forum. An archived
  thread is read-only and is passed over. The forum is judged from the log.
  The thread is projected up to its length before the moderator is
  admitted. Each record is appended on the condition that nothing else
  landed after that length, so a move made in between sends the thread back
  to be judged again. A thread that cannot be reached is passed over, the
  rest are finished, and the answer says to run the action again.
- **The restore is exact.** It puts back only posts whose `Delete` in force
  carries one of the account's suspensions. A post a moderator hid for its
  own sake stays hidden, and so does one a moderator restored and then hid
  again. The restore walks the log to decide, and each `Restore` is written
  on the condition that nothing landed after the walk. An unsuspension's
  restore takes only the suspension it lifted. The standalone restore takes
  the suspensions the account had when it was read. Neither can reach a
  suspension made while it runs.
- **A hidden forum's threads say nothing.** Every thread is opened and
  projected, so a move into the moderator's forum that the projection has
  not caught up with is found. A thread that cannot be reached is reported
  only when the projection lets the moderator read it, so the answer tells
  them nothing about a forum they cannot see.
- **The work on one account runs one at a time.** Suspending, unsuspending,
  hiding, restoring and expiry on the same account are serialized in the
  process, which is the only writer (02). An unsuspension that restores
  waits for a hide still appending, and a hide asked for after an
  unsuspension is `conflict`.
- **Expiry is a job.** It runs every minute and lifts every suspension whose
  end has passed, as an unsuspension by no account with `"via": "expiry"`.
  It reads the end again in the transaction that lifts it, so a suspension
  replaced meanwhile stands. The posts a suspension hid stay hidden until a
  moderator restores them (maintainer decision 2026-09-29). A lazy check at
  every status read lost, because the session chokepoint, the viewer and
  perm would each have had to consult the end, and `users.status` would stop
  being the truth.

**Decided 2026-09-29** (maintainer, on grooming's recommendations):
- An author may rename their own thread within 24 h (TKT-01M29CKRH9), the
  default of `threads.rename_window_seconds` (04).
- In an archived forum, moderators may still hide, lock, pin and move
  threads out. Posting is refused to everyone but admins, and moving a thread
  in stays admin-only (TKT-01M3N24RHD).
- Suspension: see docs/05 "Account lifecycle" for who may suspend whom. When
  a suspension expires, the posts it hid stay hidden until a moderator
  restores them (TKT-01M29CP2QK).
- The audit log is admin-only; moderators see none of it (TKT-01M29CP2R4).
- A denied or blocked applicant sees only the status, never the moderator's
  note. Moderators and admins may unblock an identity, which writes an audit
  row (TKT-01M29CP2MG).
- An agent's rate bucket allows a burst of 20 and refills one token every
  30 s (TKT-01M29P9RSW).

**Decided 2026-09-30** (maintainer, at the close of the Phase 2 exit run):
- Thread moderation follows the forum grant, not the role. Anyone holding
  `moderate` in a thread's forum, by role or through a group, may lock,
  unlock, pin, unpin and set slow mode there, and may move a thread when they
  hold `moderate` in both forums. Admin screens and account actions keep their
  role gates (TKT-01M3RQP0TG, filed with this decision).
- A flag that was kept but whose auto-hide failed answers the normal 200
  receipt, and the failure is logged and retried by the sweeper;
  `unavailable` means nothing was kept (PR #202, TKT-01M3QQWA5Z). Kept as
  built.
- The CI race step keeps its default timeout. The slow `internal/perm`
  fixture is fixed by a migrated template database instead
  (TKT-01M3R5P3CN).
- The client's reader bundle budget rises from 60000 to 70000 B gzip
  (docs/07). The reader was near 55 KB, with several Phase 2 reader tickets
  still to land.

**Decided 2026-10-01** (maintainer, answering the run's recorded questions):
- Two recorded disagreements are accepted as built: a reply draft saved while
  the thread's first load is in flight is kept rather than deleted (review
  1585, TKT-01M3NS1931), and the e2e projects share one new-thread draft
  because they run serially (review 1591, TKT-01M3NS16VH).
- The audit screen's reader-bundle growth of 11 B is accepted in place of its
  "no reader growth" criterion (TKT-01M29CP2R4).
- Post, last-post and thread-list times are absolute, not relative (docs/06,
  TKT-01M3S3ZF48).

Author edit/delete window: default 24 h for edits, 15 min for delete, both
site settings. After the window, edits leave the visible "edited" marker and
full history; the first N minutes (default 5) of edits are collapsed into the
original revision for display ("grace edits") while still stored.

Every "who" above names an account holding that role, and an agent account can
hold any role (05). The thread actions are the exception (decided 2026-09-30,
TKT-01M3RQP0TG). For lock, unlock, pin, unpin, move and slow mode,
"moderator" means anyone holding `moderate` in the thread's forum, by role or
through a group. A move needs `moderate` in both forums. A global pin, and
unpinning or replacing one, stays a moderator's or admin's by role, because
a global pin heads the thread list of every forum and not only the one
moderated. Whether to let an agent resolve flags or split threads is an
operator's decision rather than a rule here. The audit log records the actor
either way, so an agent's moderation is as reviewable as anyone's.

## Thread tags

What a tag may be is in 04 `thread_tags`. Who may set one is decided here.

**Decided by the maintainer (2026-10-02, TKT-01M3M4KVNW):**
- Anyone who may create a thread in a forum (`create_thread`) may tag it
  when they create it, except with a reserved tag.
- Only a moderator or an agent account may set a reserved tag. An agent
  working thread is marked by one (06, 10 item 33), so a member cannot
  make a human thread look like an agent's.
- Tags are free text. A moderator corrects a thread's tags with a retag,
  which writes a `ThreadTagged` replacing the whole set (03). A curated
  list per forum may come later.
- The reserved list lives here. It is:
<!-- reserved-tags:begin -->
  - `agent`
<!-- reserved-tags:end -->

**Decided by the run (2026-10-02, pending maintainer review):**
- "Moderator" means anyone holding `moderate` in the thread's forum, by
  role or through a group, both for a reserved tag on create and for a
  retag. A retag needs nothing else, so a moderator may set or remove a
  reserved tag. Retagging is refused on an archived thread and allowed in
  an archived forum, as locking is. An agent account is one whose
  `users.kind` is `agent`, the account the frame's machine-authored bit
  names (03). The thread's author may not retag. Alternatives: a moderator
  or admin by role only lost because thread moderation has followed the
  forum grant since 2026-09-30 ("Decided 2026-09-30" above), and a
  group-granted moderator who may lock, move and rename a thread but not
  retag it would be a gap with no reason behind it. Letting the author
  retag lost because the Primitives table gives the author rename alone,
  and an author who wants different tags asks a moderator, as for a move.

## Rate limits

Humans are unlimited. There is no per-user post rate limit, on the same reasoning
this document gives for leaving out word filters: for a community of known people
it is low value, and adding it later is cheap. Slow mode already covers the case
a moderator actually meets, which is one heated thread rather than a member who
posts too fast everywhere.

Agents are limited, because an agent is a machine and a loop that misbehaves does
so at machine speed. Agent writes are charged against a **token bucket** rather
than a minimum interval: a burst allowance it can spend at once, and a slower
refill. Twenty posts in a minute while working and then silence for hours is the
normal shape of agent traffic, and a fixed interval would either reject that or
permit a runaway. Burst size and refill rate are settings, per agent with a site
default: `agents.write_burst` is 20 and `agents.write_refill_seconds` is 30
(maintainer decision 2026-09-29, TKT-01M29P9RSW). The burst covers the working
minute, and the refill gives 120 writes an hour, well below what a runaway would
do. An admin overrides either for one agent on the users screen, and a change
applies on the agent's next request. A refill is at most a week (604800 s),
because the limiter holds it as a duration and a larger number would overflow
into no limit at all.

What is charged is every mutating `/api/v1/*` request a bearer credential
authenticates, except the read position (`PUT` and `DELETE
/threads/{id}/read`) and drafts (`/drafts/*`). Reading, and recording what was
read, is not what the bucket guards against, and charging it would throttle an
agent for reading. The bucket belongs to the agent rather than to its token, so
issuing a new credential does not refill it. A member signed in with a cookie is
never charged.

Exceeding the bucket is `429` with `Retry-After` (07), not a queue. A queued post
would land minutes later carrying a timestamp that lies about when it was
written, and in a log that is the source of truth that is not a cost worth
paying. An agent handles the rejection the way any API client does.

Slow mode applies to agents as it does to anyone. A moderator who slows a thread
means it for every participant in it.

### The transport-level ceiling is not a posting policy

Separately from all of the above, the HTTP layer applies two per-address token
buckets, and they are infrastructure rather than moderation:

| What | Rate | Why |
|------|------|-----|
| `/auth/*` | 10 burst, 1 per 6s | Credential stuffing. Signing in is a handful of requests; past this it is a script working a list |
| Mutating `/api/v1/*` | 60 burst, 1 per 0.5s | A crash barrier. No member typing prose can reach it; a client stuck in a retry loop is stopped before it fills a disk |

Reads are not limited at all: the reasoning above for leaving out post rate
limits applies with more force to reading.

The write ceiling does not contradict "humans are unlimited" and must not be
tightened until it does. It exists so that a runaway client cannot exhaust the
disk, not to pace anybody's posting.

The write ceiling applies to agents too. An agent that trips it gets a `429`
that did not come from its own bucket. That is intended, because the ceiling is
a crash barrier for every caller, but it can look like a bug to whoever debugs
the first agent that hits it.

Both are keyed by address, because the limiter deliberately runs *outside*
session resolution — a session lookup is a database query, and limiting after
it would mean an unauthenticated flood costs a query per request. Per-identity
limiting is therefore a second stage after session resolution,
`web.KeyedLimit`: a token bucket per key, where the key is read from the
request once the viewer is known, and a request with no key passes. Two
things use it. The per-identity limit on application submission in 05, where
an unapproved visitor holds a pre-user session, built it (TKT-01M29CP2KZ): it
limits `POST /api/v1/apply` and `POST /api/v1/apply/withdraw` together at
`web.ApplyRate`, a burst of 5 and then one a minute per identity, from
however many addresses, refused attempts included. The per-agent token
bucket above reuses it (TKT-01M29P9RSW).

Members get no per-session write limit, and that is a decision rather than an
omission. A session is used from one address, so a per-session ceiling covers
nothing the per-address ceiling above does not already cover — except a session
replayed from many addresses at once, which is a stolen cookie, and a rate
limit is not the answer to a stolen cookie. The runaway this would supposedly
catch, a member's tab stuck in a retry loop, comes from one address and hits
the ceiling above. Adding the second stage for members would buy a limiter with
no threat left to stop.

`X-Forwarded-For` is not consulted. It is trivially spoofable, so trusting it
without a configured list of trusted proxies would not weaken the limiter but
remove it: every request could claim a fresh key.

## Flags (reports)

Any member can flag a post with a reason (`spam`, `abuse`, `off-topic`,
`other` + text). Flags live in a `flags` table (authoritative):

```
id, thread_id, ordinal, reporter_id, reason, note, created_at,
status ('open'|'resolved'|'dismissed'), resolved_by, resolved_at, resolution_note,
audit_id, audit_pending
```

The moderator queue (`/admin/queue`) shows open flags and pending applications
in one list, newest first, with counts in the moderator navigation. A post
with ≥ N open flags (default 3, setting) is auto-hidden pending review
(`hidden-by-mod` flag in the index entry set via a `Delete` variant with
`auto=true`; a moderator's `Restore` clears it). Reporters see "thanks,
a moderator will look" and nothing else.

Decided 2026-09-28 (TKT-01M29CP2PJ). N defaults to 3 and is a site setting.
While a post is auto-hidden, its author and moderators still see it, marked
"hidden pending review". Everyone else sees a tombstone in its place, so the
ordinal and reply links hold. Because the hide is `hidden-by-mod`, the author
cannot restore it; only a moderator's review clears it. Other members see
the tombstone "hidden pending review", and dismissing every flag on the post
restores it (decided 2026-09-29, TKT-01M29CP2PJ and TKT-01M29CRJ4E). The marker does tell
the author that flags put their post under review, and that is intended. What
stays confidential is who flagged it and how many did, which only moderators
see. Hiding the post from its author as well lost, because a tombstone on
their own post tells them the same thing and hides what is being reviewed. Queue-only,
with no auto-hide, lost because spam would stay up until a moderator is
awake.

How it runs (TKT-01M29CP2PJ):
- A member has one open flag per post. A repeat adds no row, so N counts
  reporters. It gets the same receipt even after the post is hidden, so a
  repeat does not reveal the hide. After a moderator closes their flag, they may flag the post
  again, and the new flag counts from zero.
- The flag is committed first. The hide reads only committed flags and runs on
  every flag at or over N, not only the one that crosses it. It is a `Delete`
  with `auto` and actor 0, the system, so the log names no reporter. Its audit
  row, `post.auto_hidden`, is the system's and carries no count.
- A member cannot flag their own post: the request is `forbidden` and writes
  no row, whether the post is shown or hidden (maintainer decision 2026-09-29,
  TKT-01M3QQWA5Z). The client does not offer flag on the viewer's own posts.
- If the hide cannot be appended, the flag stays and the reporter gets the
  same receipt as any other kept flag, and the failure is logged. A distinct
  answer would tell the reporter that the post reached N (TKT-01M3QQWA5Z). A
  sweep runs every minute and once at startup, and hides every shown post
  with at least N open flags. The same sweep enforces a lowered N.
- A moderator's restore of any post dismisses its open flags in place first,
  and reopens them only if the `Restore` was never appended. After a durable
  `Restore` they stay dismissed, so the sweep cannot hide the post again. A
  flag reported later counts from zero.
- A flag, the hide, a moderator's restore, and a moderator's confirmation of
  the hide hold one per-post lock. So the sweep cannot read a post's open
  flags just before a restore dismisses them and then hide the post the
  moderator has just restored. For the same reason, a flag checked before a
  restore cannot be inserted after it. `ketju serve` is the only writer, so
  an in-process lock is enough.
- A moderator who keeps the post hidden appends their own `Delete`
  (`by_moderator`). That makes the hide theirs and resolves the flags. If
  the resolve fails after the `Delete`, asking again resolves the flags and
  appends nothing. The moderator queue (TKT-01M29CRJ4E) gives this a
  button.
- An archived thread is read-only, so it takes no flags.

The queue (TKT-01M29CRJ4E):
- **One item per flagged post**, holding every flag open on it. Three
  reports of one spam post are one decision, and the badge counts work, not
  reports. An item's time is its newest open flag. The routes are in 07
  "The moderator queue".
- **Scope follows `moderate`.** Flags appear for the forums where the
  viewer may moderate (`perm.ModeratedForums`). A member granted `moderate`
  on one forum through a group sees that forum's flags. A moderator by role
  sees every forum they can read, and an admin every forum. Applications
  appear to staff only. Flags in an archived thread are not listed, because
  the thread is read-only, its flags included, and no action could clear
  them.
- **One lock.** Every queue action holds the same per-post lock as the
  flag, the hide, the restore, the confirmation, and every delete or
  restore of one post from the thread page. Under it the action reads the
  post's open flags and its state, and decides from them. A second
  moderator waits on the lock, finds no open flag, and gets `conflict`
  before touching the log.
- **State decides, errors do not.** Each append carries the state it was
  decided on as its precondition, and an action that appends nothing reads
  the state again just before its write. If something outside the lock
  changed the post first, which is a suspension's hide or restore of every
  post by one author, the answer is `conflict` with the flags left open,
  never success. A failed append leaves the flags open too.
- **The actions.** Dismissing every flag on a post hidden pending review
  restores it, through the moderator's restore path, because a review that
  dismisses the flags has found nothing to hide (maintainer decision,
  2026-09-29). "Keep post" (`resolve`) on such a post restores it the same
  way. On a post a moderator hid by hand, dismissing closes the flags and
  the post stays hidden, and keeping it is `conflict`, because it is a
  restore of that moderator's decision, for the thread page. Remove hides a
  shown post as a moderator's hide, or makes a hide pending review the
  moderator's, and then resolves the flags. It is a moderator's hide even
  of the moderator's own post, since removing it from the queue is a
  moderation decision. On a post already down it only resolves them.
- **A redacted post is down for good.** The log refuses every record naming
  a hard-redacted post (03 "Hard redaction"), so the queue takes one as
  already down, whatever it was before the redaction: dismiss, Keep post
  and Remove only close its flags, answering `already hidden`, and none
  tries a restore or a hide (TKT-01M3R1WPNS). Its flags would otherwise
  stay open for good once a post hidden pending review was redacted.
- **Audit.** The log records a queue action appends get their `post.*` rows
  from the projection. The queue writes one `flag.resolved` or
  `flag.dismissed` row against the post, with the flags it closed, what was
  asked, the note, and what happened to the post. Where no `Restore` is
  involved, the row is written with the close, in one transaction. The
  restore path writes it only after the `Restore` append succeeds, because
  `audit_log` is append-only and a failed restore reopens its flags. If
  that write fails, the answer is still 200, because the restore happened.
  The flags carry `audit_pending` (04 "flags"), and the row is written with
  the original moderator and time, marked `recovered`, on the next request
  for the post or by a job within a minute or two. Only a flag with the
  marker set is ever owed a row. Whether the `Restore` landed is read from
  the post's log: the queue stamps its `Restore` with the dismissing
  moderator and the dismissal's millisecond, and looks for that record.
  The post being shown proves nothing, since another moderator's restore
  shows it too. A marked flag whose `Restore` never landed (its append
  failed and so did the reopen after it) is reopened instead, with no row.
- **A reporter learns nothing** from a resolution: no notification, and
  the same receipt for their next report.

## Post history

Every post has a history view listing revisions with actor, time, and a
word-level diff computed on demand from the two revisions' sources. Visible
to moderators always, and to members if the site setting
`public_edit_history` is on (default on — transparency discourages
sneaky edits, which is a trust feature in small groups).

## Tombstones

A soft-deleted post renders as "Post removed by author" or "Post removed by a
moderator" with the ordinal preserved so numbering, permalinks, and reply
lenses keep working. Moderators see the original inline with a distinct
background. Replies to a deleted post keep their `reply_to` link.

## Locking semantics

Locked threads accept no posts or edits from members; moderators may still
post (their posts are styled as such). Locking does not archive. Archiving
(admin) is a stronger state used for very old threads: read-only for
everyone including moderators, and the open-thread cache drops archived
threads eagerly.

"Everyone" includes admins: after its `ThreadArchived` the service appends
nothing more to the thread's log for anyone (03; 04 "permissions" contrasts
this with an archived forum, where an admin may still post; 07 answers
`conflict`). Reading, watching, muting and the read position are unchanged,
as in an archived forum (TKT-01M29CRJ64).

**Decided by the run (2026-10-02, pending maintainer review)**, for
TKT-01M29CRJ64: the offline `ketju redact` still redacts an archived
thread. It is the tool for a takedown or an erasure that must reach every
post (03 "Hard redaction"), and v1 cannot undo an archive, so refusing it
would keep the text for good. Alternatives: refusing it, so that nothing at
all is appended after `ThreadArchived`, lost for that reason; requiring an
unarchive first lost because v1 has no unarchive record (03), and whether
it gets one is the maintainer's question (TKT-01M3YSQQ5X).

## What is intentionally missing

- Karma/reputation, vote-based ranking, or automated trust levels. They shape
  behavior toward performance rather than conversation and are the antithesis
  of the linear-thread goal.
- Word filters. Cheap to add later; low value for a community of known people.
- Shadow banning. Deceptive toward members; suspension with a reason instead.
