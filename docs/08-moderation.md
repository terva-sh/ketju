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
| Slow mode on a thread (min seconds between posts per user) | moderator | `settings` per thread in DB, not log | yes |
| Suspend / unsuspend user | moderator | `audit_log`, `users.status` | yes |
| Approve / deny application | moderator | `applications`, `audit_log` | deny → re-apply |
| Anonymize user | admin | `audit_log` | no |
| Hard-redact post body | admin, CLI only | `audit_log` + rewritten log | no |

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
default.

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

Both are keyed by address, because the limiter deliberately runs *outside*
session resolution — a session lookup is a database query, and limiting after
it would mean an unauthenticated flood costs a query per request. Per-identity
limiting therefore needs a second stage after session resolution. Two things
want that stage: the per-agent token bucket above, and the per-identity limit
on application submission in 05, where an unapproved visitor holds a pre-user
session. Neither exists yet, and the stage should be built by whichever
arrives first.

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
