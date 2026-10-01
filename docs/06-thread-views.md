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
  **Decided by the run (2026-10-01, pending maintainer review):** the
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

### Landing on return

A signed-in user opening a thread they have visited lands on the page
containing `last_read_ordinal + 1`, with a **"N new posts below"** divider
drawn between read and unread posts. The divider is the single most valuable
piece of UI in a linear forum and is present from the first milestone.

Last read rather than first unread is decided (10 Q14, TKT-01M29BW9P4). The two
land on the same page in practice, and the divider carries the difference. The
confirmation with real readers on real phones is a Phase 1 human verification
item, not a gate on building it.

**Decided by the run (2026-10-01, pending maintainer review):** the divider
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
- Forum and site level "mark all read" set `users.mark_all_read_at`; a thread
  with `last_post_at < mark_all_read_at` and no row is read.

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
  **Decided by the run (2026-10-01, pending maintainer review):** the
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
- Optional per-forum display `?sort=created` for announcement forums.
- The forum index (the front page). **Decided by the run (2026-10-01,
  pending maintainer review):** its heading is the `site_name` setting, the
  same value the wordmark shows (07 "Site settings"), as the mockups head
  the page with the community's name (TKT-01M3S3ZZYA). Alternatives: the
  client's "Forums" lost because it names no community; and the mockups'
  "Ketju" wordmark beside it, which would avoid the name appearing twice,
  lost because 07 already puts `site_name` in the wordmark (TKT-01M29CXK2H).
- **Decided by the run (2026-10-01, pending maintainer review):** the forum
  index ends with a footer, "N members · M posts", then at 720 px and above
  "Press ? for keyboard shortcuts" (TKT-01M3S3ZRYD). M is the sum of
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

## Live updates

While a thread page is open the client holds an SSE subscription. On a
`{thread_id, log_length}` hint it fetches new posts and shows a
"3 new posts — show" pill at the bottom rather than inserting them unasked
(which would move the page under the reader). If the user is at the very
bottom, new posts stream in directly. Polling every 30 s is the fallback
when SSE is unavailable; the non-JS client gets a plain "refresh" link.

**Decided by the run (2026-10-01, pending maintainer review)**
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
  **Decided by the run (2026-10-01, pending maintainer review):** the
  sentence above names what stays in the phone footer, not its order. The
  footer reads the reply expander when the post offers one, then reply,
  then quote, then "more" at the right edge, as the phone mockup draws it.
  The header puts the 30 px avatar in its first row, with the author and
  role badge on one line beside it and the ordinal, time and edited mark on
  the next, and the body takes the full width below (TKT-01M3S3Z9E1).
  Alternatives: reply, quote and then the expander, read as an order from
  this sentence, lost because it moves the expander away from where "Post
  anatomy" and both thread mockups put it, so the footer would differ
  between widths for no reason the sentence gives; and the desktop's quote
  before reply lost because this sentence and the phone mockup both put
  reply first.
- **Pages.** A "Page 3 of 9" control with previous and next; tapping the
  label opens a picker sheet with page numbers and the jump-to-unread and
  jump-to-date actions. Lenses open from a sheet as well.
  **Decided by the run (2026-10-01, pending maintainer review;
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
  10). **Decided by the run (2026-10-01, pending maintainer review;
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
