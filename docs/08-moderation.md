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
