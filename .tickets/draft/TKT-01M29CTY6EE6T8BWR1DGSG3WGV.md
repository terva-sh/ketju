---
schema: 3
id: TKT-01M29CTY6EE6T8BWR1DGSG3WGV
title: Implement the since, mentions, and moderator lenses
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:views
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CRJ1PY355KX3HX7A5N785
blocks_on: none
references:
  - ref: design:lenses
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-10-01T08:44:49Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-groom-lenses
  name: ""
extensions: {}
---

## Description

Three lenses that complete the set. `since:2026-09-01` filters on the index's created_at and backs jump-to-date. `mentions` uses the mentions table to show posts that mention or reply to the viewer. `mod` includes deleted, hidden, and moved posts inline with their tombstone reasons, for moderators only.

The moderator lens is the one with teeth: it is the only place a member-invisible body is rendered, so its role check has to be at the service layer rather than in the UI.

## Acceptance criteria

- [ ] since: accepts YYYY-MM-DD (midnight UTC), RFC 3339 and Unix ms, answers the moment as Unix ms, and a since page runs the canonical page's statements whatever the thread's length (db.Options.Trace)
- [ ] mentions keeps the posts that mention the viewer or reply to a post they wrote, leaves out their own posts and removed posts, and drops a post once an edit removes the mention
- [ ] mod is refused at the service layer, tested by calling Service.View directly: anonymous and suspended viewers get unauthenticated, a member and a moderator of another forum get forbidden, and a moderator by role, a moderator by group grant and an admin are served
- [ ] mod keeps exactly the removed posts with their removal reason, gives a redacted or moved post no body for anyone, and is never served from a member's cache entry
- [ ] TestLensPostsMatchCanonical gains the since, mentions and mod rows for every viewer class and removed state
- [ ] All three are URL-addressable: the lens an answer names (kind alone when value is empty, as for mentions and mod; kind:value otherwise) serves the same posts to the same viewer class when opened again (TestLensValueRoundTrips)

## Implementation plan

Planned 2026-10-01 against main at a599b48 (Phase 3 grooming, lane `lenses`).

### Settled in grooming

- `since:<when>` filters on the index entry's `created_at`, revision 1's time (docs/06 "Lenses"; docs/03 "Index entry"). `Index.Since` exists. `<when>` is `YYYY-MM-DD` (midnight UTC), an RFC 3339 time, or Unix ms, answered as Unix ms (docs/07, under "Decided by the run (2026-10-01): the `lens` parameter"). Removed posts keep their place as tombstones (docs/06, under "what each lens keeps of removed posts").
- `mentions` reads the `mentions` projection (docs/04 "mentions", primary key `(thread_id, ordinal, mentioned_user_id)`), plus the posts that reply to a post the viewer wrote. It leaves out the viewer's own posts and removed posts (docs/06, under "Decided by the run (2026-10-01): the mentions lens").
- `mod` keeps exactly the removed posts: deleted, hidden, hidden pending review, moved and redacted, each with its `removal` and `redacted`. It carries a body only where `revealRemoved` already serves one (docs/06, under "what each lens keeps of removed posts"; docs/07 "Removed posts").
- **Who may use `mod`.** Anyone with `moderate` in the thread's forum, by role or through a group, as `access.canModerate` computes it through `perm.Can`. That is the forum grant docs/08 "Decided 2026-09-30" gives thread moderation, and the rule by which `revealRemoved` already serves the same viewer every removed body on the canonical page (docs/07 "Removed posts"). The lens therefore shows a moderator nothing their canonical page does not.
  - An anonymous viewer, and a suspended or inactive one, served as anonymous, are `unauthenticated`.
  - A member without `moderate` there, a moderator of another forum included, is `forbidden`.
  - The check is in the service, not the UI (ticket description).
- Built on the framework in TKT-01M29CRJ1P.

### Approach

1. **since.**
   - `lens.Since(ms)`'s filter keeps `e.CreatedMS >= ms`. `Parse` (TKT-01M29CRJ1P) already normalizes the three forms.
   - A moment past the last post is an empty selection.
   - The cache key's `Lens` is `since:<ms>`.
2. **mentions.**
   - Add the query `MentionedOrdinals :many`, `SELECT ordinal FROM mentions WHERE thread_id = ? AND mentioned_user_id = ? ORDER BY ordinal`, to `internal/store/db/queries/viewer_state.sql`. It is a primary-key prefix scan, and needs no migration. Regenerate sqlc.
   - The resolver reads it once before the snapshot. Inside the snapshot it takes `ix.ByAuthor(viewer)` as the viewer's own set before `Select`, because a filter must not call back into the index.
   - The filter keeps `(mentioned[o] || own[e.ReplyTo]) && e.Author != viewer && e.Flags.Visible()`.
   - `lens.value` is empty. For a viewer who is not a member, the selection is empty with 200 and kind `mentions`.
   - The cache key's `Lens` is `mentions:<viewer id>`.
   - The projection trails the log by the bus's delay, so a mention written a moment ago appears on the next request. That is the same trade `/replies` makes with `post_meta`.
3. **mod.**
   - The resolver refuses first: `unauthenticated` when `!a.viewer.Member()`, `forbidden` when `!a.canModerate`. Both checks come after `access`, so a hidden thread is still `not-found`.
   - The filter keeps `!e.Flags.Visible()`, and `lens.value` is empty.
   - Pages are drawn through `pageClass`, which is always `moderator` here. The cache key's `Lens` is `mod`.
4. Docs: none beyond the decisions already merged with this grooming.

### Tests

- `TestSinceLensForms`: a date, an RFC 3339 time with an offset, and Unix ms select the same posts for the same moment, and the answer's value is Unix ms. `TestSinceLensIncludesTombstones`.
- `TestSinceLensStatements`: a since page runs the canonical page's statements on a 10-post and a 2000-post thread (db.Options.Trace counter).
- `TestMentionsLensMentionsAndReplies` covers a mention, a reply to the viewer's post, the viewer's own post (left out), and a mention inside a quotation (left out: never recorded).
- `TestMentionsLensLeavesOutRemoved`: a post that mentioned the viewer and was then deleted, hidden, hidden pending review or redacted is not listed.
- `TestMentionsLensFollowsEdits`: an edit that removes the mention removes the post from the lens once projected. No revision's text is the reason a post is listed.
- `TestMentionsLensAnonymous`: 200, `shown` 0. `TestMentionsLensCacheSeparatesViewers`.
- `TestModLensRefusedAtServiceLayer` calls `Service.View` directly. An anonymous viewer and a suspended moderator are `unauthenticated`. A member and a moderator of another forum are `forbidden`. A moderator by role, a moderator by group grant (`world.groupModerator`) and an admin are served. A hidden thread is `not-found` to everyone without read.
- `TestModLensSelectsRemovedPosts`: each removed kind appears with its `removal`. A redacted post and a moved post have no body for anyone. A post hidden pending review shows its body to the moderator.
- `TestModLensNeverServedFromAMemberCacheEntry`: mod pages are keyed `moderator`, and a member asking after a moderator gets `forbidden`, not a cached page.
- `TestLensValueRoundTrips`: the lens an answer names, `kind` alone when `value` is empty and `kind:value` otherwise (docs/07), serves the same posts to the same viewer class when opened again: `since:<ms>`, and bare `mentions` and `mod`, whose `value` is empty. `mentions:x` and `mod:x` serve the canonical page.
- `TestLensPostsMatchCanonical` gains the since, mentions and mod rows. They cover every viewer class, the moderator by group grant and the suspended viewer included, and every removed state, a suspended author's hidden posts included.

### Out of scope

- The switcher's date field, mentions and moderator items, and their empty states: `TKT-01M29CTY71`.
- Jump-to-date on the phone page picker: `TKT-01M29CGBZD`.
- Mention notifications: `TKT-01M29CRJ2T`.
- A moved post's destination: `TKT-01M29CXJZZ` (Phase 4).

## Notes

**agent:claude-code/phase3-groom-lenses** at 2026-10-01T07:51:15Z

run decision: pending maintainer review. Three decisions bind this ticket. (1) The moderator lens is a filter to the removed posts, not the whole thread with bodies inline; the inline form is already the moderator's canonical page (docs/06 'Lenses'). (2) mentions keeps posts that mention the viewer or reply to their post and leaves out their own (docs/06). (3) since: reads a date as midnight UTC, so a shared link means one moment (docs/07). Who may open mod is not a new security rule: it is moderate in the thread's forum by role or group grant (docs/08 'Decided 2026-09-30'), the rule by which revealRemoved already serves the same viewer every removed body on the canonical page (docs/07 'Removed posts'). Decided in the Phase 3 grooming of 2026-10-01, lane lenses.

**agent:claude-code/phase3-groom-lenses** at 2026-10-01T08:44:49Z

Review 1702 (head 57bb98f, base a599b48), medium, 'Make the round-trip requirement valid for bare lens kinds': fixed. mentions and mod answer an empty lens.value. The lens an answer names is kind alone when value is empty and kind:value otherwise (docs/07). TestLensValueRoundTrips and the client's lensParam (TKT-01M29CTY71) both use that rule.
