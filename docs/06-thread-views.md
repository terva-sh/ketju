# 06 — Thread Views (the linear thread and its lenses)

## The stance

A thread is read in post order. Always. Every feature in this document is a
**lens**: a filter, marker, or overlay applied to the ordered list of posts
that never reorders them and never indents them. The research summary in
01 records why: a shared, scalar reading position is what makes unread
tracking, permalinks, "write to the room" norms, and moderation work. Trees
buy reply-to-reply reciprocity at the cost of all of those.

**This survives models as participants.** The argument is about a shared reading
order, not about who is reading, and an agent that consumes a whole thread every
turn still needs its posts to land somewhere a human can cite by ordinal. What
models change is volume: agents working together produce dense runs that would
bury human conversation if they landed in the same threads. The answer is
thread-level rather than post-level. An agent working thread is an ordinary
thread in an ordinary forum carrying a tag (record type 16, already in 03), and
a human filters by tag from the forum row. Inside any thread, every post is
read. Nothing collapses, nothing hides by default, and no new UI appears. An
agent that posts more than anyone can follow is a norms problem, exactly as a
human who does.

## The canonical view

### Paged, not infinite

- Fixed page size (default 25 posts, user setting 10–50; the default is
  decided, 10 Q13, because it is baked into every shared URL), URL
  `/t/{id}/{slug}?page=3` and permalink `/t/{id}/{slug}/{ordinal}` which
  resolves to the right page and anchors the post.
- Pages give shareable positions, browser-native scroll restoration, and a
  stopping cue. A Discourse-style timeline scrubber is a later enhancement on
  top of pages, not a replacement.
- Keyboard: `j`/`k` post navigation, `g` then a number to jump to an ordinal,
  `u` to jump to first unread, `n` to next unread thread in the forum, `?`
  for the list. "Next" follows the forum's thread-list order: the first
  unread thread below this one, or when there is none below, the first
  above it. Each shortcut has a visible control that does the same, below
  the posts or in the thread's actions.
  **Decided by the run (2026-10-01, maintainer approved):** the
  desktop top bar keeps its link to `/search` rather than the mockups'
  240 px field, as 07 "Search" decided for the search screen
  (TKT-01M3V70X0F), and the mockups' `/` key hint is left out
  (TKT-01M3S3ZZSZ, item D1). Every shortcut here is a single key with no
  modifier, and none can yet be turned off, which WCAG 2.1.4 asks for
  (TKT-01M3V9DWTN). `/` waits on that off switch, and TKT-01M3V7GE59 owns
  it. Alternatives: the mockups' field lost because 07 "Search" had already
  chosen the link, and two run decisions should not disagree about one
  control. `/` now lost because it would add one more shortcut that does
  not meet 2.1.4.
  **Decided by the run (2026-10-02, pending maintainer review;
  TKT-01M3V9DWTN):** the single-key shortcuts can be turned off, which is
  the first of the three ways WCAG 2.1 SC 2.1.4 allows. They are on unless
  turned off. A member turns them off in Settings (`/settings`), and the
  choice is stored on the account as `shortcuts` in `prefs_json` and
  carried in `/me`'s `settings`, as the theme is (07 "Preact client"). A
  signed-out reader turns them off from the shortcut list, and the choice
  stays in the browser. For a member the list links to Settings instead,
  so the account has one writer. With them off, no character key acts on
  any page: not `j`, `k`, `g` and its digits, `u`, `n`, `?` or `/`. The
  "Keyboard shortcuts" buttons still open the list, which says that the
  shortcuts are off and how to turn them on.
  Alternatives:
  - Settings alone lost, because a signed-out reader has the shortcuts and
    would have no way to turn them off.
  - A per-browser switch for everyone lost, because a member's choice
    would not follow them, and docs/07 keeps the theme on the account for
    that reason.
  - A switch in the list that saves a member's account lost, because the
    settings screen's save queue is the one place saves are ordered (Terva
    reviews 1838 and 1875).
  - Off by default lost, because it would take the shortcuts away from
    every desktop reader when an off switch is all 2.1.4 asks for.
  - Remapping to a modifier, or acting only on focus, lost. A remap screen
    is more UI and collides with browser keys, and `j` and `k` act on the
    page rather than on one control.

  **Decided by the run (2026-10-02, pending maintainer review;
  TKT-01M3V7GE59):** `/` opens search on every desktop screen that has the
  top bar, while the shortcuts are on. Elsewhere it goes to `/search`,
  whose field takes focus with no query. On `/search` it focuses the field
  and selects its text. The top bar's Search link is its visible control,
  and the shortcut list names it. `ignored()` keeps it out of text fields,
  the composer and dialogs, as for every other key. Alternatives: `/` on
  the thread page only lost, because search is reached from every screen
  through the top bar, which is where the mockups draw the hint; a field in
  the top bar lost, because 07 "Search" chose the link.

### The thread head

Under the title the thread head says who started the thread, when, and how
many posts it has, then the thread's tags, pin and watch state as their own
tickets add them. **Decided by the run (2026-10-01, pending maintainer
review):** it reads "started by HANDLE · TIME · N posts" at both widths.
HANDLE is the thread's author as its row in the thread list names it. TIME is
the thread's creation time, written as every other time is (absolute,
`PostTime`, see "Post anatomy"). N is the thread's post count, the number the
pager counts in, deleted posts included. Above the title, the forum is a
breadcrumb, "Forums › General" (TKT-01M3S3ZF6J). Alternatives: the phone
mockup's shorter "mara · 3 days ago" lost because the same components render
at every size (see "Mobile") and the words cost no width that wrapping does
not absorb; the mockups' "3 days ago" lost to the absolute-time decision of
2026-10-01; and a post count without deleted posts lost because it would
disagree with the pager's "posts 51–75 of 212" on the same screen.

**Decided by the run (2026-10-02, pending maintainer review)**, for
TKT-01M3YSQPZX and TKT-01M3YSQQ33:
- The thread's tags follow that line as chips, drawn as the thread list
  draws them, each a link to the forum's list filtered to it ("Thread list
  views"). A thread with no tags shows nothing there.
- An archived thread says "This thread is archived." where a locked one
  says it is locked, and offers nobody, admins included, a reply,
  an edit, a delete, a restore, a flag or a moderation control, because
  the server refuses every one (08 "Locking semantics"). Its row in a list
  carries an "Archived" flag where a locked row carries the lock. An
  archived thread is listed and read as before.
- Tags are set on the new thread screen in one optional "Tags" field below
  the title, separated by spaces or commas, and a moderator changes them
  from the thread's moderation menu with "Edit tags", the same field
  filled in. A refused tag is named under the field.

Alternatives: hiding an archived thread from lists lost because 04 decided
an archived forum stays readable, and hiding makes archiving a delete in
disguise; a disabled composer on an archived thread lost because it offers
a control that can never work; a widget that turns each tag into a chip as
it is typed lost because it costs reader bytes and its own keyboard
handling for at most 8 short words, where a text field needs neither; a
list of tags to pick from lost because tags are free text (04
`thread_tags`).

### Landing on return

A signed-in user opening a thread they have visited lands on the page
containing `last_read_ordinal + 1`, with a **"N new posts below"** divider
drawn between read and unread posts. The divider is the single most valuable
piece of UI in a linear forum and is present from the first milestone.

Last read rather than first unread is decided (10 Q14, TKT-01M29BW9P4). The two
land on the same page in practice, and the divider carries the difference. The
confirmation with real readers on real phones is a Phase 1 human verification
item, not a gate on building it.

**Decided by the run (2026-10-01, maintainer approved):** the divider
ends with "last read #N · TIME" at both widths, where TIME is
`read_positions.last_read_at`. That is when the viewer's place in this
thread was last set: by a read beacon, or by marking the thread unread,
which sets the place where the reader says it is at that moment. It is
written as every other time is (absolute, `PostTime`), and the thread page
carries it in its per-viewer block (TKT-01M3S3ZS0H). Alternatives:
- The phone mockup's shorter "read to #63" lost because "Mobile" below says
  the divider is identical to desktop.
- Leaving the time out lost because the time is what tells a returning
  reader how stale their place is.
- The time the post numbered N was written lost because that says when
  somebody else wrote, not when the reader set their place.
- A separate column for the last beacon alone, so that a mark-unread does
  not move the time, lost because it is a schema change for one line of
  text. A mark-unread is itself the reader saying where they are, and the
  time of that statement is the truthful one to show.

### Read tracking

Position-based, one integer per (user, thread): `read_positions.last_read_ordinal`.

- The client sends a beacon `PUT /api/threads/{id}/read {ordinal}` when a post
  has been on screen for ≥ 1 s, batched every 30 s and on navigation/unload
  (`fetch` with `keepalive`, not `navigator.sendBeacon`, which can only POST;
  TKT-01M29CGBWN). "On screen" is at least half the post in the viewport, or
  half the viewport filled by a post taller than that. The server only ever
  moves the position forward unless the user explicitly marks a thread unread.
- Marking unread is offered in two places: as a whole-thread action in the
  toolbar and phone menu, and as "Unread from here" on each post. Either way
  the page then returns to the list, because staying would re-read the
  visible posts within a second and undo the mark. Decided 2026-09-29
  (TKT-01M3KFFFJR).
- A non-JS client cannot beacon; it marks the highest ordinal *rendered* on a
  page as read when that page is served. Coarser but consistent — the same
  integer, same semantics.
- An agent has no beacon and no mark-on-render. It sets its position explicitly
  with the same `PUT` (07), so the position is whatever it last declared, and an
  agent that never calls it has none. "On screen for at least a second"
  describes a human reading and generalizes to nothing.
- "Mark all read" works at two levels (TKT-01M29CTY7K). The site level
  (`PUT /me/read`) sets `users.mark_all_read_at` to now, never back: every
  post written up to then is read, whatever the read positions say, and a
  thread with no row whose last post is at or before the mark is read. The
  forum level (`PUT /forums/{slug}/read`) moves the member's position in
  each of that forum's threads to its last post, forward only, and leaves
  the mark and every other forum alone.
  **Decided by the run (2026-10-01, maintainer approved):** only the
  site-level mark sets `users.mark_all_read_at`, which is one moment for the
  whole site. The forum-level mark moves the member's read position in each
  of that forum's threads to its last post, forward only, in one statement.
  Read literally, the line above has a forum's mark set a site-wide value
  and mark every other forum read with it. Alternatives: a per-forum mark
  table lost because it adds a third input to every unread rule (`MeCounts`,
  `ForumUnread`, the thread list's badge, `UnreadCount` and the unread
  lens); setting the site-wide mark lost because it does what the member did
  not ask for. The forum-level cost is one row per thread in the forum the
  member has not opened, which docs/04's sizing already allows for
  (TKT-01M29CTY7K).
- **Decided by the run (2026-10-01, maintainer approved):** the thread
  page reads one integer for the member's place, the *read boundary*: the
  stored position, moved forward to just before the first post past it
  that was written after the site-level mark, or to the last post when
  there is none (dbq `ReadBoundary`, TKT-01M29CTY7K). The landing page,
  the divider, first unread, the bare unread lens (`unread:<boundary>`),
  each list row's `last_read_ordinal` and the read routes' answers all use
  it, so after a site-level mark a thread opens past the posts the badge
  already counts as read. Posts are appended in time order, so the posts
  after the boundary are exactly the unread ones. When the mark is what
  moved the boundary, the divider's time is the mark's, since the mark is
  what set the place. Alternatives:
  - Leaving the page on the stored position lost because, after a mark,
    a thread the badge calls read would open on post 1 under "N new posts
    below", and the unread lens would show posts nothing counts.
  - Writing every thread's position when the site is marked lost because
    it is a row per thread per member, which the mark exists to avoid.
  - Showing the stored position's time beside a boundary the mark set lost
    because that time is when the reader was at a place they are no longer
    shown.

### Post anatomy

```
┌──────────────────────────────────────────────────────────────────┐
│ #42  avatar  handle · role badge · Tue 14:05 · (edited) ↩ #37   │  ← header: ordinal, author, time, reply_to indicator
│                                                                  │
│  > quoted excerpt from #37 (collapsible, links to #37)           │  ← quotes render as linked blockquotes
│  body …                                                          │
│                                                                  │
│  📎 attachments                                                  │
│  ── 3 replies ▾ ──   quote · reply · link · flag · history · ⋯   │  ← footer: reply expander + actions
└──────────────────────────────────────────────────────────────────┘
```

- The `↩ #37` indicator is **suppressed when #37 is the post immediately
  above** (Discourse's rule). In practice most replies are to the previous
  post and the thread reads as plain flat text.
- Times are **absolute**, in the viewer's timezone and locale: the time for
  a post written today, the weekday and time within the last week, and the
  date before that. The full moment is in the element's title and its
  `datetime` attribute. The same rule holds for the forum index's last post
  and the thread list, so a time reads the same wherever it appears and does
  not go stale on a page left open. The mockups' "14 min ago" and "yesterday"
  predate this; the maintainer decided on absolute times on 2026-10-01
  (TKT-01M3S3ZF48), and `PostTime` in the client is the implementation.
- The **role badge** carries account kind as well as role (04), so an agent's
  posts are identifiable on every post with no separate marker. The badge reads
  the account as it stands; the API also carries a per-post `machine_authored`
  flag from the log, for the case where an account's kind changed after the
  posts were written (07).
- "3 replies ▾" expands the replying posts **inline, in place, read-only,
  visually recessed**, and collapses again. Ordering within the expansion is
  ordinal order. Nothing about the canonical page moves: no canonical post
  changes place in the order, gains an indent, or is replaced, and the
  expanded post and everything above it hold still on screen. The expansion
  sits inside the post, below its footer, so the posts after it move down by
  its height while it is open and return when it closes, as anything inline
  does. The replies are copies: they carry no anchor, no actions and no
  reply-to indicator, and each one's ordinal links to its permalink.
- The expander is **suppressed when the post's only reply is the post
  directly below it** on the page, the same rule that suppresses that
  reply's `↩` indicator. When the only reply is the first post of the next
  page, the reader cannot see it below, so the expander stays
  (TKT-01M29CGBW6).
- Clicking `↩ #37` scrolls to #37 if it is on the page, otherwise shows it in
  a popover with a "go to" link. Same widget for quote links.
- **Decided by the run (2026-10-02, pending maintainer review;
  TKT-01M3YME9AZ):** a quoted post shows its backlinks as a line between its
  body and its footer, "Quoted by #51 #60", at both widths. Each ordinal is a
  link that behaves as the `↩` indicator does: it scrolls to the quoting post
  if that post is on the page, and otherwise opens it in the popover. The
  ordinals are in ascending order. Past ten, the line ends with "and N more"
  as plain text. The line is absent when nothing quotes the post, and on a
  tombstone. A quoting post is listed only while every reader is shown it
  as itself, so no backlink comes from a deleted, hidden, moved or redacted
  post, for moderators either. A backlink therefore never says what a
  removed post quoted. A quotation nested inside a quotation counts, as the
  renderer reports it. The server records backlinks as a projection
  (04 `post_quotes`), and the API serves them as `quoted_by`.
  Alternatives:
  - A count, "quoted 3 times", lost because a reader cannot follow it.
  - A place in the footer lost because the phone footer's contents and
    order are decided below, and a row of links there would crowd its 44 px
    targets.
  - A place under "more" lost because docs/01 adopts backlinks as one of the
    ways a flat thread shows its structure, and a menu hides them.
  - Listing removed quoting posts to moderators lost because the cached posts
    are shared by a viewer class. A post's author sees their own removed post,
    but they form a class of their own only when that post is on the page,
    and a quoting post usually is not.

## Lenses

All lenses are URL-addressable (`?lens=…`) so they can be shared and so the
non-JS client gets them for free. They are computed server-side from the
in-memory index of the thread (03) and, for cross-thread data, `post_meta`.

| Lens | Query | Source | Notes |
|------|-------|--------|-------|
| **Unread** | `?lens=unread` | index + read_position | Posts after last-read, paged. The default target of "jump to first unread". |
| **By author** | `?lens=author:handle` | index `author_id` | "Only OP" is `author:<thread author>` with a one-click button. Header shows "showing 7 of 212 posts". |
| **Reply chain** | `?lens=chain:42` | index `reply_to` | The ancestor line of #42 (following reply_to upward) plus its direct replies, in ordinal order, with gaps shown as "… 5 posts …" dividers. Linear rendering of a conversation slice. |
| **Since** | `?lens=since:2026-09-01` | index `created_at` | Jump-to-date; the scrubber's backing query. |
| **Mentions me** | `?lens=mentions` | `mentions` table | Posts that mention or reply to the viewer. |
| **Moderator** | `?lens=mod` | index flags | Includes deleted, hidden, and moved posts inline with their tombstone reason. Moderators only. |
| **Digest** (later) | `?lens=digest` | replies count + quotes count | Posts most replied-to/quoted, with "N hidden" bars. Explicitly *not* like-score based: no likes exist in v1. Discourse's summary needs reaction volume a small group will not produce. |

A lens header always shows the count of hidden posts and a one-click return to
the full thread at the same ordinal, so nobody gets lost in a filter.

**Decided by the run (2026-10-01, maintainer approved): what each lens
keeps of removed posts.** Unread, since and reply chain are slices of the
thread. Each keeps a removed post in its place as the tombstone the canonical
page shows, so numbering and the chain's reply links hold (08 "Tombstones").
By author and mentions keep only posts shown as themselves, so "showing 7 of
212" counts posts a reader can read, and an author whose every post was
removed is an empty lens. The moderator lens keeps exactly the removed posts:
deleted, hidden, hidden pending review, moved and redacted, each with its
tombstone reason. It carries a body only where 07 "Removed posts" already
serves that body to the moderator. A lens selects the same posts for every
viewer, except unread and mentions, which depend on the viewer. Alternatives:
keeping tombstones in every lens lost because the author lens of a member
whose posts a suspension hid would be a page of tombstones. Dropping them from
every lens lost because a reply chain through a removed post would lose its
link. A moderator lens showing the whole thread with removed bodies inline lost
because the moderator's canonical page has shown exactly that since
TKT-01M29CKRJ9, so the lens would add nothing (TKT-01M29CRJ1P,
TKT-01M29CTY6E).

**Decided by the run (2026-10-01, maintainer approved): the unread lens
holds its boundary.** `?lens=unread` takes the viewer's `last_read_ordinal` B
and answers as `unread:B`, the posts after B. The client replaces the address
with `?lens=unread:B`, so paging, reload and Back show the same posts while
reading moves the position. Opening the bare lens again starts from the new
position. B is the integer the divider, the landing page and "first unread"
already use, so the lens's first post is the post "first unread" lands on.
An anonymous viewer's unread lens is empty, and the client asks them to sign
in. Alternatives: re-evaluating on each page lost because reading page 1
moves the position, and page 2 would then skip a page of posts. Holding the
boundary in server state lost because lenses are URL state (07 "Path to a
non-JS client") (TKT-01M29CTY4J).

**Decided by the run (2026-10-01, maintainer approved): read tracking
under a lens.** The read beacon runs on the canonical view and under the
unread lens, and under no other lens. The beacon sends the highest post
seen, so under author, chain, mentions or moderator it would mark every post
the lens skipped as read. Since skips posts too: its first post can be well
after the reader's place, and reading it would mark the gap before it read.
Under `unread:B` the beacon runs only when B is at or below the reader's
read position as the client knows it then. The lens then starts at or
before their first unread post, as it does when they open their own bare
`?lens=unread`. A shared or typed `unread:99` opened by a reader at #10
would otherwise mark #11 to #98 read unseen, so that view tracks nothing.
First unread under the unread lens goes to the reader's current position
plus one, within the lens, never to B + 1, which they may have read since
the lens opened. Under an `unread:B` whose B is above the position, the
lens leaves that post out, so First unread goes to it in the full thread
(Terva review 1792).

Alternatives:
- Tracking under every lens lost for the skipped-posts reason.
- Tracking under none lost because the unread lens is where a daily reader
  reads.
- Tracking under since, whose posts run on without a gap once it starts,
  lost to the gap before its first post (Terva review 1730).
- Tracking under any `unread:B` lost to the shared-link case (Terva review
  1741).

(TKT-01M29CTY71)

**Decided by the run (2026-10-01, maintainer approved): the lens bar.**
It sits under the thread head at both widths, as the mockups draw it. It holds
"Only <handle>", the author lens for the thread's author, and "Lens", which
opens the switcher. The switcher offers the full thread, unread, by author
for each author on the page, mentions me, since a date, and moderator to a
viewer who moderates the forum. The date goes into the address as written
and is read as midnight UTC (07), so the field and a shared link select the
same posts. A post that is a reply or has replies offers
"Conversation", its chain lens, in its footer, under "more" on a phone. Every
lens draws a "… N posts …" divider where it skipped posts, not only the
chain. "Full thread" in the lens header is a link to the canonical permalink
of the post at the top of the viewport, kept current as the reader scrolls so
a new tab or a copied link goes there too, or to the thread's own address when the lens
has no posts. On phones TKT-01M29CGBZD presents the
switcher as a sheet. Alternatives: Only OP among the thread actions lost
because on a phone those sit under the more menu, and 10 Q15 leaned to the
header. Dividers in the chain lens alone lost because an author lens would
then read as one unbroken conversation, which it is not (TKT-01M29CTY71,
TKT-01M29CTY58).

**Decided by the run (2026-10-01, maintainer approved): the lens
header's details.**
- **Counts.** The header reads "Showing N of M posts · H hidden". M is the
  thread's posts, shown and hidden together, so it matches the head's post
  count.
- **The unread divider.** It is drawn on the canonical view and under the
  unread lens, and under no other lens, whose neighbouring posts are not the
  thread's.
- **The switcher's authors.** "By @handle" lists the authors of the page's
  posts that are shown as themselves, so a removed post does not offer a
  lens that would leave its author out.
- **The date field.** It shows a since lens's moment as its UTC day, the
  same rule by which a chosen day is read.
- **The header stays on top.** The lens header stays at the top of the
  screen while the lens is read, under the phone's top bar. Full thread is
  therefore in reach wherever the reader is, and reaching it does not
  scroll the page and change the post it goes to (Terva review 1793). The
  post at the top is the first one below the header's edge.

Alternatives:
- A count of the lens's posts alone lost because "showing 7 of 212" is
  what the table above promises.
- The unread divider under every lens lost because "N new posts below"
  under an author lens would count posts the lens does not show.
- The field in the viewer's own timezone lost for the reason the address
  does not use it (07).
- A header that scrolls away with the posts lost because a reader who
  scrolled back up to press Full thread would land near the top of the
  page, not where they were reading.
- A per-post "full thread" entry in place of a header that stays lost
  because the post's ordinal already links its canonical permalink, and
  docs/06 promises the one-press return in the header.

(TKT-01M29CTY71)

**Decided by the run (2026-10-01, maintainer approved): the mentions
lens** keeps the posts that mention the viewer, by the user id the `mentions`
projection records, and the posts that reply to a post the viewer wrote. It
leaves out the viewer's own posts. Alternative: keeping the viewer's own posts
lost because a reply to yourself is not what the lens is for
(TKT-01M29CTY6E).

## Composer

- Markdown with a toolbar; live preview rendered by the **same server
  pipeline** via `POST /api/render` (debounced) so the preview is exact and
  we ship no client-side markdown parser. The non-JS client posts a form and
  gets a preview page; same endpoint, HTML response.
- **Quote**: selecting text in a post and clicking "quote" inserts a
  `> ` block whose **first line** is the attribution `[quote=#37]`, which the
  renderer turns into a collapsible `<details>` linked to post 37; the server
  records the quote as a backlink. The `<summary>` is plain text ("Quote from
  #37") and the link to post 37 is the quotation's first line, because a link
  inside the summary is a control inside a control (TKT-01M3BFSBPJ). Keeping the attribution inside the
  blockquote means a renderer that does not know the syntax still shows a
  quotation. Quoting a deleted post renders a tombstone rather than the copied
  text — the copy lives in the quoting post's own source, so dropping it at
  render time is the only thing that makes a delete effective. An ordinal that
  does not resolve degrades to a plain blockquote.
- **Reply**: clicking "reply" on #37 sets `reply_to = 37` (shown as a chip
  in the composer, removable). Replying via the thread-bottom composer sets
  no `reply_to`. Both land at the end of the thread.
- Mentions `@handle` autocomplete; the renderer resolves them and the server
  records them. Two rules the renderer enforces and the client should match:
  a mention is recorded by **user id**, not handle, so an old post stays
  correct after a rename; and a mention inside a quotation renders as plain
  text and notifies nobody, because quoting someone repeats their words rather
  than addressing the people they addressed. A handle nobody has is text, not
  a broken link. A handle a rename released within 90 days mentions the
  member it redirects to, so a mention typed from memory still reaches them
  (05 "Changing a handle", TKT-01M3NRP3W9).
- Drafts autosave to the server (`drafts` table, one per user+thread) so a
  half-written reply survives a browser crash and appears on another device.
  **Decided by the run (2026-10-01, maintainer approved):** the
  composer's footer says "Draft saved TIME", where TIME is the `updated_at`
  of the server's latest answer for the draft, written as every other time
  is (absolute, `PostTime`), so a draft loaded from another device says
  when it was saved there (TKT-01M3S3ZZYA). Alternatives: the mockup's
  "12 seconds ago" lost to the absolute-time decision of 2026-10-01, and a
  counter that ticks would redraw the composer every second; the bare
  "Draft saved" the composer says now lost because it cannot tell a draft
  saved a moment ago from one left on a phone last week.
- Composer sits at the **bottom** of the page after the posts (Tildes'
  reasoning: read before you write). A sticky "Reply" button scrolls to it.

## Thread list views (forum index)

- Forum page: pinned threads first, then by `last_post_at` descending,
  each row showing title, tags, author, reply count, last poster, last post
  time, and an unread badge with count (from `read_positions`). Clicking
  the badge jumps to first unread; clicking the title goes to page 1.
- **Unread** across all forums (`/unread`), **New since your last visit**,
  **Participated**, **Watching**. These are the "what's new" surfaces and
  live in the top navigation.
  **Decided by the run (2026-10-01, maintainer approved)**, for
  TKT-01M29CTY7K, so that each list agrees with its `/me` badge:
  - *Unread* lists the threads with an unread post, by the rule `/me`
    counts (docs/04 `read_positions`), most recent activity first.
  - *New* lists the threads the member has never opened, meaning they have
    no read position there, that were started after their site-level mark
    (any thread, with no mark), newest started first. This is the
    Discourse reading: New is conversations you have not joined, and Unread
    is conversations with posts you have not read. `/me` gains a `new` count
    for the New badge that docs/06 "Mobile" asks for.
  - *Participated* lists every thread where the member has a post that was
    not moved, read or unread, by most recent activity, and each row
    carries its unread count.
  - *Watching* lists the threads with a thread-level `watch`, in the same
    order and with the same counts. A forum-level watch drives
    notifications only (docs/04 `watches`).
  - A thread whose effective level is `mute` is in none of the four lists.
  - Neither replying to a thread nor starting one changes its watch level.

  Alternatives: "since your last visit" read as a timestamp lost because
  `users.last_seen_at` is never written, and a visit boundary across two
  devices ends the "new" on one before it was read on the other. New as
  threads started in the last N days lost because the threads drop out
  whether or not they were read. Watching including forum-watched threads
  lost because Watching would then copy the forum's own list. Watching on
  reply lost because Participated would then be a copy of Watching, and
  an agent thread a member answered once would alert them on every post.
- **Decided by the run (2026-10-01, maintainer approved;
  TKT-01M3V6Y4JP):** where "Mark all read" is offered, and how the four
  lists treat a reader with nothing to show.
  - The site-level mark is a button on `/unread`, shown while the list
    has rows, and in the forum index's header beside New thread from
    720 px up, as the desktop mockup draws it. The forum-level mark is a
    button in a forum's header, beside New thread, at every width. Each
    is offered to a member only, and neither asks for confirmation.
  - A signed-out reader on any of the four lists is asked to sign in,
    and the client sends no request, since every list is a member's own.
  - A list row names its forum as a link, first in the row's meta line.

  Alternatives:
  - Only `/unread` and the forum page, as grooming wrote it, lost because
    the mockup's front-page button (divergence F3 on TKT-01M39WBN4Q) is
    the site-level mark and would stay missing.
  - The front-page button on phones lost because the phone mockup draws
    none. A phone reader has the button on `/unread`, one tab away.
  - A confirmation before the site-level mark, which cannot be undone,
    lost because the mockup draws a plain button. A reader who marks by
    mistake loses unread markers, not posts.
  - The forum as a chip before the title lost because it moves each
    title along its line by a different amount, so the titles no longer
    start at one edge.
- Optional per-forum display `?sort=created` for announcement forums.
- **Filtering by tag.** **Decided by the run (2026-10-02, pending
  maintainer review)**, for TKT-01M29CYSCP and TKT-01M3YSQPZX: a tag chip
  is a link. In a forum's list and on the thread head it opens that
  forum's list filtered to the tag, `/f/{slug}?tag=TAG`; on a cross-forum
  list it opens the row's own forum filtered the same way. A filtered list
  shows only threads carrying the tag. Pins carrying it still lead the
  list, and a pin without it is left out. Its header says "Tagged TAG"
  with a link back to the whole list. One tag filters at a time. Alternatives: a tag picker in the forum's header
  lost because it needs every tag a forum uses, a new endpoint and a design
  nobody has drawn, where the chip a reader is already looking at needs
  none; several tags at once lost for the same reason, and because nothing
  in 06 or 10 item 33 asks for more than "filters by tag"; keeping every
  pin at the head of a filtered list lost because the reader asked for one
  tag's threads.
- The forum index (the front page). **Decided by the run (2026-10-01,
  maintainer approved):** its heading is the `site_name` setting, the
  same value the wordmark shows (07 "Site settings"), as the mockups head
  the page with the community's name (TKT-01M3S3ZZYA). Alternatives: the
  client's "Forums" lost because it names no community; and the mockups'
  "Ketju" wordmark beside it, which would avoid the name appearing twice,
  lost because 07 already puts `site_name` in the wordmark (TKT-01M29CXK2H).
- **Decided by the run (2026-10-01, maintainer approved):** the forum
  index ends with a footer, "N members · M posts", then at 720 px and above
  a way to the keyboard shortcuts (TKT-01M3S3ZRYD; see below). M is the sum of
  `post_count` over the forums the page lists, so it counts only forums the
  viewer may read and needs no new field. N is exactly the accounts the
  mention lookup (`GET /users?prefix=`) can offer: active, with a handle
  that does not end in `-`. It is sent only to a member session, in a
  `members` field of `GET /forums`. An anonymous viewer or an applicant
  gets null, and their footer shows the post count alone. A member can
  already list every one of those accounts through the lookup, so the
  count tells a member nothing they could not learn. An anonymous viewer
  cannot list them, so an anonymous viewer is not told the count. The
  mockups' Guidelines and Markdown help links are left out, since neither
  page exists. Alternatives:
  - A site-wide post count lost because it would count posts in forums the
    viewer cannot read.
  - Counting every active account lost because it would include the few
    whose handle ends in `-`, which the lookup leaves out, and so would
    tell members something the lookup does not. Registration no longer
    gives out such a handle (04 "users"), so the difference is small.
  - The member count for everyone lost because it would tell an anonymous
    visitor something no other answer does.
  - The count on `/me` lost because `/me` is read on every page and inlined
    in the shell, for a number one screen shows.
  - The shortcut hint on phones lost because shortcuts are absent there
    ("Mobile").

  **Decided by the run (2026-10-01, maintainer approved)**, while
  building it: the hint is a "Keyboard shortcuts" button that opens the
  shortcut list, and no key is bound on the forum index. The list opened
  there leads with "These keys work on a thread page, where ? opens this
  list." Alternatives: binding `?` on the index so that the mockup's
  "Press ? for keyboard shortcuts" would hold lost because it adds a
  single-character shortcut, which WCAG 2.1 SC 2.1.4 allows only with a way
  to turn it off, and that switch is TKT-01M3V9DWTN's (Terva review 1887,
  as TKT-01M3S3ZZSZ left out `/`). The mockup's words with no key bound
  lost because the hint would be false. Front-page shortcuts of their own,
  such as j and k between forums, lost for the same WCAG reason and
  because nothing asked for them.

  **Decided by the run (2026-10-02, pending maintainer review;
  TKT-01M3V9DWTN):** once the shortcuts can be turned off ("Paged, not
  infinite"), `?` is bound on the forum index while they are on, and the
  footer's button reads "Press ? for keyboard shortcuts", as the mockup
  does. While they are off, `?` does nothing there and the button reads
  "Keyboard shortcuts". The list's lead line says which keys work on the
  index and which only on a thread page. This replaces the decision above
  only in what it waited on, the off switch. Alternatives: keeping the
  index unbound lost, because the reason for it was 2.1.4, and the switch
  meets it; binding `j` and `k` on the index too lost for the reason
  above, that nothing asks for them.
- The Alerts screen (`/notifications`, TKT-01M29CTY86) lists the viewer's
  notifications as one sentence each, the thread's title linking to the
  post's permalink, and never shows post text (04 "notifications"). The
  rows a page shows are marked read as they are shown, and keep their
  unread marking until the reader leaves. **Decided by the run
  (2026-10-01, maintainer approved):** "Older" adds the next page
  below the rows already shown and moves focus to its first row, rather
  than replacing the page as a thread list's "Older threads" does, because
  the rows shown stay marked unread only for this visit, and a page
  replaced would lose that marking. "Mark all read" clears the unread
  marking of every row shown at once, because the reader asked for exactly
  that. Alternatives: a paged list in the address, as the thread lists
  have, lost for the reason above; keeping the marking after mark-all lost
  because the reader would see no effect of the button but a status line.

## Moderator flow for drift: split and merge

Drift is the known cost of flat threads, and moving posts is the answer.
Moderators can multi-select posts (checkbox per post in mod mode) and
**split to a new thread** (title prompt; new thread gets the posts in ordinal
order, source shows "5 posts were moved to <link>" at the position of the
first moved post) or **merge into an existing thread**. Ordinals in the
source never change (03). Split threads carry a "split from <link>" header.
"Reply as new thread" is the member-facing version of the same primitive:
it opens a composer for a new thread that quotes the source post and
leaves a small "continued in <link>" note under it.

docs/15 "Invariants, added" governs every notice and pointer here. A notice
names the other thread only to a viewer who may read it. Everyone else
reads "N posts were moved to another thread", or "merged from" or "split from
another thread".

The rest of this section was **decided by the run (2026-10-02, pending
maintainer review)** for TKT-01M29CYSAM and TKT-01M29CYSBA:
- **Where merged posts go.**
  - Posts merged into an existing thread land at its end, in their source
    order, after a "N posts were merged from <link>" notice.
  - Each keeps its original author and time, so the times run backwards at
    the notice.
  - A merge does not move the thread up any list, because a thread's
    latest-post time is the newest post time it holds.
  - A merge notifies nobody, because the posts are not new writing.

  Alternatives:
  - Interleaving by original time lost. A post's ordinal is its place:
    pages, permalinks and the read boundary count ordinals, and 03 never
    renumbers. Interleaving would need a second order beside the ordinal,
    and would move every later post onto another page under a reader.
  - Showing the move time lost, because a reply would seem to answer posts
    written after it.
  - Bumping the thread lost, because its list row would name a latest post
    that the thread page shows at another time.
- **What may move.** Only posts shown as themselves can be selected. A
  removed post, one hidden pending review, a redacted one and one already
  moved stay where they are.

  Alternative: moving removed posts with their removal lost. A hidden post's
  text would reach the target forum's moderators, apart from the flags that
  led to the hide, and an author's own delete would be carried to a thread
  they never posted in.
- **Quotes in a moved post.** In a post a move copied, a `[quote=#N]`
  attribution draws as "Quote from another thread", without the quoted text
  or a link, because N counts posts in the thread the text was written in.

  Alternatives:
  - Renumbering at copy time lost. It rewrites the author's text in the log,
    and a quote of a post left behind would keep its text after that post
    was removed.
  - Resolving N in the origin thread lost. A copy of a copy cannot name the
    thread its text counted in, and a cached page would keep a quote whose
    post was removed in another thread.
- **Who started a split thread.** It is the moderator who split it. They wrote
  its title, and the "split from" header and each post's author block credit
  the posts.

  Alternative: the first moved post's author lost, because that member would
  get the starter's rename window over a title they did not write.
- **Selecting posts.**
  - A moderator turns selection on with "Select posts" in the thread's
    actions, in the toolbar on a desktop and the more menu on a phone.
  - Each selectable post then shows a checkbox at the start of its header.
  - A bar fixed to the bottom of the viewport, above the tab bar on a phone,
    shows the count, "Split…", "Merge…" and "Cancel".
  - The selection survives page changes within the thread and ends on
    leaving it.
  - Split asks for a title and a forum, the source's by default.
  - Merge asks for the target thread as a link or id, and shows its title
    before it confirms.
  - Both dialogs say that a move is undone only by moving the posts back.

  Alternatives:
  - A checkbox on every post at all times lost, because it costs a moderator
    width on every post while they read.
  - Selecting from the moderator lens lost, because that lens holds only
    removed posts, which do not move.

## Live updates

While a thread page is open the client holds an SSE subscription. On a
`{thread_id, log_length}` hint it fetches new posts and shows a
"3 new posts — show" pill at the bottom rather than inserting them unasked
(which would move the page under the reader). If the user is at the very
bottom, new posts stream in directly. Polling every 30 s is the fallback
when SSE is unavailable; the non-JS client gets a plain "refresh" link.

**Decided by the run (2026-10-01, maintainer approved)**
(TKT-01M29CTYA0):
- **"At the very bottom"** means the end of the page's last post is inside
  the viewport, give or take 48 px. On a phone the reply bar does not count
  as content.
- **What the pill counts.** It counts posts that arrived after the reader
  opened the page, or after they last took the pill: the thread's post
  count now, less its count then. Posts that already existed on later pages
  never count, and neither do edits.
- **Posting a reply takes the reader to it,** as the composer already does
  by routing to the reply's permalink. That is a page open: the page
  holding the reply loads afresh with every post up to it, and the reader
  is placed at their reply. A reply that does not fit on the page in view
  goes to its own page in the same way. Posts that were held behind the
  pill appear above the reply, and nothing moves under the reader, because
  the reader has moved. The new page's count includes the reply, so a
  reply posted from this page never counts in the pill (Terva reviews 1703
  and 1716).
- **What "show" does.** It reveals the held posts in place when they all
  fall on the page in view. Otherwise it goes to the page holding the first
  one. The pill is a button at the bottom of the post list, and its count
  is announced politely. A post held behind it is not marked read.
- **Changes to posts already shown** (an edit, a removal, a restore) are
  applied at once. The scroll position is corrected against the first post
  in view, so the text under the reader does not move. Each hint refetches
  the whole page rather than an `after_log_length` fragment. A removal or a
  restore writes no revision, so a fragment that also held an edit would
  leave a removed post on screen (Terva review 1696).
- **Polling takes over** when the stream errors, or when its `ready` event
  does not arrive within 10 s, which is what a buffering proxy looks like.
  The client polls every 30 s and tries the stream again every fourth poll,
  stopping the polling once a stream says `ready`.
- **A tab hidden for 60 s closes its stream.** It reopens, and catches up by
  log length, when the tab is shown again.

Alternatives: counting edits in the pill lost, because "3 new posts" would
then miscount. Holding edits behind the pill lost, because a reader would
keep seeing text its author had already corrected or a moderator had
removed. Keeping hidden tabs connected lost, because an open tab costs a
held connection (TKT-01M29CRJ3X), and background tabs are most of them.

**Decided by the run (2026-10-01, maintainer approved)**
(TKT-01M29CTYA0, while building it): a lens page holds no subscription.
Live updates run on the canonical view, the thread's pages in ordinal order,
and a lens page stays as it was answered until the reader asks again. The
unread lens is computed from the read position, which moves while the
reader reads, so asking for its page again would take posts away under
them. The pill counts and reveals the whole thread's posts by ordinal,
which a filtered page does not hold in order. The lens header already
returns the reader to the full thread. Alternatives: refetching every lens
but unread lost, because live behaviour would then differ by lens, and a
pill counting the thread's posts would count posts the lens hides. A pill
on a lens page that only links to the full thread lost, because it needs
the thread's post count, which a hint does not carry, so it costs a second
request per hint for a page the reader chose to filter.

A known cost, unsolved. In a thread where agents are working, twenty posts a
minute means the pill count never settles. Tagging keeps those threads out of a
human's unread list so most people never meet it, but a human who opens one gets
a counter that churns. Worth revisiting with a real agent thread in front of us
rather than inventing a throttle now.

## Mobile

The phone layout is the same product at a different width, not a reduced
one. Every lens, page, and moderation action is reachable; what changes is
arrangement.

- **Breakpoints.** Below 720 px is the phone layout; 720–1100 px is a
  single-column tablet layout with the desktop navigation; above 1100 px is
  the desktop layout shown in the mockups. Layout switches in CSS; the same
  components render at every size.
- **Navigation.** The cross-forum lists move from the top bar to a bottom
  tab bar: Forums, Unread, New, Alerts, Me. Badges on Unread and New carry
  the same counts as the desktop nav. Each screen has a 52 px top bar with
  a back control, a truncating context title (the forum name on a thread
  page), search, and a "more" menu holding the thread actions that live in
  the desktop toolbar (watch, share link, moderator actions). The tab bar is
  decided over a menu button (10 Q25), because a menu hides exactly the
  "what's new" surfaces a forum exists for. Phase 1 ships all five tabs even
  though the Unread, New, and Alerts screens are Phase 3 work: those three
  route to a minimal placeholder until their screens land, and a badge shows
  only a count the API already provides. The bar keeps its final shape from
  the first release, so nobody relearns it later.
- **Composing.** List screens have a floating "new thread" button above the
  tab bar. The thread page replaces the tab bar with a sticky reply bar
  showing the reply-to chip and the first line of any draft; tapping it
  opens the composer as a full-screen sheet with the same toolbar, preview
  tab, and server-rendered preview. Reply and quote from a post prefill
  that sheet. The sheet is decided over an inline composer (10 Q26): under a
  virtual keyboard an inline composer is cramped and hides the post being
  answered, and the sheet has room to show it.
- **Posts.** One column, 30 px avatars, author and time stacked, reply-to
  indicator at the right of the header. Footer actions are reply, quote,
  and the reply expander; link, flag, "unread from here", and history
  collapse under a "more" control. The unread divider, tombstones, and the
  unread edge marker are identical to desktop.
  **Decided by the run (2026-10-01, maintainer approved):** the
  sentence above names what stays in the phone footer, not its order. The
  footer reads the reply expander when the post offers one, then reply,
  then quote, then "more" at the right edge, as the phone mockup draws it.
  The header puts the 30 px avatar in its first row, with the author and
  role badge on one line beside it and the ordinal, time and edited mark on
  the next, and the body takes the full width below (TKT-01M3S3Z9E1). The
  reply-to indicator sits at the right end of that second line, so the
  header's two links, the ordinal and then the reply-to, read left to right
  in focus order; at the right of the first line, Tab would move from the
  second line back up to the first (Terva review 1725).
  Alternatives: reply, quote and then the expander, read as an order from
  this sentence, lost because it moves the expander away from where "Post
  anatomy" and both thread mockups put it, so the footer would differ
  between widths for no reason the sentence gives; and the desktop's quote
  before reply lost because this sentence and the phone mockup both put
  reply first.
  **Decided by the run (2026-10-01, maintainer approved;
  TKT-01M3V3CFMA):** once the author's name links to their profile, the
  phone header's ordinal is text, as the phone mockup draws it, and the
  permalink is under "more". Stacked one over the other, the name and the
  ordinal sit about 20 px apart, and two 44 px targets need 44 px between
  their centres. So the header's links are the author on the first line
  and the reply-to at the right end of the second, and focus reads them
  in that order. The desktop keeps its ordinal link. Alternatives: about
  25 px of room between the two lines lost because it left the avatar
  floating between two widely spaced lines, far from the mockup; leading
  the DOM with the author while keeping both links lost because it does
  not make room for both targets; and leaving the name without the
  enlarged target lost because "Targets and gestures" names author names
  among the links that take one.
- **Pages.** A "Page 3 of 9" control with previous and next; tapping the
  label opens a picker sheet with page numbers and the jump-to-unread and
  jump-to-date actions. Lenses open from a sheet as well.
  **Decided by the run (2026-10-01, maintainer approved;
  TKT-01M29CGBZD):** the picker sheet shows the pages as numbered links
  in a grid, each at least 44 px, with the current page marked and
  scrolled into view. Up to 60 pages, the grid holds every page. Past
  60, it holds the first and last pages and the 20 on either side of the
  current one, and a "Go to page" number field above it reaches any other,
  so the sheet's size does not grow with the thread. "First unread" sits above the grid, offered on the
  terms the thread page already offers it: a signed-in reader with posts
  they have not read. "Jump to date" is a date field that opens the `since`
  lens at that date. The lens sheet opens from a "Lens" button in the
  thread header, as the phone mockups draw it, and lists exactly the
  lenses the desktop switcher offers this viewer. Every choice is a link,
  so it changes the URL. Alternatives: a page-number field alone lost
  because a tap on a number beats typing on a phone keyboard; the desktop
  pager's window of five pages lost because the sheet exists to reach a
  nearby page in one tap; a grid of every page however many lost because
  a thread of thousands of pages would draw thousands of controls
  (Terva review 1691).
  **Decided by the run (2026-10-01, maintainer approved;
  TKT-01M29CGBZD):** "First unread" in the sheet is a button that runs the
  thread page's own First unread, not a link. It goes to the reader's
  position at the press plus one, which reading on the page moves after
  the sheet is drawn, and to a post already on screen it moves focus
  without changing the URL, as First unread in the thread actions does
  ("read tracking under a lens"). Alternative: a link to the post after
  the position the page arrived with lost because it would send a reader
  who had read on back to a post they have read (Terva review 1730).
- **Targets and gestures.** Every standalone control (buttons, tabs,
  pager, post actions, the "more" menu) is at least 44 px on its shorter
  side. Inline links inside text (author names, ordinals, tag chips) keep
  their visual size but get an enlarged hit area via padding. No action is
  swipe-only; swipes are never required to reach a feature. Keyboard
  shortcuts are simply absent.
- **Read tracking** uses the same on-screen beacon; the reply bar does not
  cover the last post because the post list has bottom padding equal to the
  bar height.
- **Installable.** The SPA ships a web app manifest so it can be added to a
  home screen. Offline caching and push notifications are not in v1 (see
  10). **Decided by the run (2026-10-01, maintainer approved;
  TKT-01M29CGBYY):** the design names no icon, so the run draws a
  placeholder: two linked rings ("ketju" is Finnish for chain) in the
  light theme's accent and white, as an SVG source with PNGs at 180, 192
  and 512 px and a maskable 512 px with the mark inside the safe zone. The
  server writes the manifest at `/manifest.webmanifest` so that its `name`
  and `short_name` are the `site_name` setting, as the title and the
  wordmarks are. `theme-color` is given for the light and the dark theme,
  and follows a theme the reader chose. Alternatives: a letterform lost
  because drawing one needs a font rasterizer in the build or a
  hand-traced path, for a placeholder; a static manifest under `/assets/`
  lost because a club's home-screen icon would then carry the software's
  name rather than the club's.

Mockups for both widths are in `docs/mockups/`.

## What we deliberately do not build

- **Nested or indented rendering**, at any depth, including a "one level
  only" compromise. Once indentation exists, people write for the branch.
- **Vote-ordered views.** No likes in v1 at all; if reactions arrive later
  they never reorder anything.
- **Infinite scroll as the default.** Pages plus jump-to-unread cover the
  need; a timeline scrubber can be layered on later.
- **Side-panel threads (Slack style).** Documented by Slack themselves as
  causing double-reading and hidden conversations.

## Later enhancements (post-v1)

- Timeline scrubber with last-read marker and date jumps (backed by `since`).
- Digest lens once reply/quote counts are populated.
- Bookmarks with a reminder time.
- Per-thread "sub-topic" tags set by moderators to label drift regions,
  rendered as dividers.
- Thread-level table of contents from headings in the first post.
