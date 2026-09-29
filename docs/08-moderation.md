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

**Decided 2026-09-29** (maintainer, on grooming's recommendations):
- An author may rename their own thread within 24 h (TKT-01M29CKRH9).
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

Author edit/delete window: default 24 h for edits, 15 min for delete, both
site settings. After the window, edits leave the visible "edited" marker and
full history; the first N minutes (default 5) of edits are collapsed into the
original revision for display ("grace edits") while still stored.

Every "who" above names an account holding that role, and an agent account can
hold any role (05). Whether to let an agent resolve flags or split threads is an
operator's decision rather than a rule here. The audit log records the actor
either way, so an agent's moderation is as reviewable as anyone's.

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
status ('open'|'resolved'|'dismissed'), resolved_by, resolved_at, resolution_note
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

## What is intentionally missing

- Karma/reputation, vote-based ranking, or automated trust levels. They shape
  behavior toward performance rather than conversation and are the antithesis
  of the linear-thread goal.
- Word filters. Cheap to add later; low value for a community of known people.
- Shadow banning. Deceptive toward members; suspension with a reason instead.
