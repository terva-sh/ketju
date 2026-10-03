import { Fragment } from 'preact';
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'preact/hooks';
import { useLocation, useRoute } from 'preact-iso';
import { peekGet } from '../api/client';
import type { CreatedPost, Lens, Pinned, Post as PostDTO, ThreadPage, ThreadViewer, ThreadWatch } from '../api/dto';
import { isProblem, problemMessage, revokesAccess } from '../api/problem';
import { useResource } from '../api/resource';
import { ErrorPanel } from '../components/ErrorPanel';
import { TagChips } from '../components/TagChips';
import { WatchDialog } from '../components/WatchDialog';
import { Composer } from '../composer/Composer';
import { ComposerSheet } from '../composer/ComposerSheet';
import { delayUntil } from '../components/thread/deadline';
import { FlagDialog } from '../components/thread/FlagDialog';
import { heldSearch, lensParam, placeGaps, tracksUnder } from '../components/thread/lens';
import { LensBar } from '../components/thread/LensBar';
import { GapDivider, LensHeader } from '../components/thread/LensHeader';
import { ModerateDialog } from '../components/thread/ModerateDialog';
import { NewPostsPill } from '../components/thread/NewPostsPill';
import { Pager } from '../components/thread/Pager';
import {
  permalink,
  Post,
  type FlagHandler,
  type PostActions,
  type ThreadRef,
  type UnreadHandler,
} from '../components/thread/Post';
import { isOwn, noControls, postControls, readOnlyViewer } from '../components/thread/postControls';
import type { Own } from '../components/thread/OwnControls';
import { PostPopover } from '../components/thread/PostPopover';
import { PostTime } from '../components/thread/PostTime';
import { isTombstone } from '../components/thread/removal';
import { expanderShown } from '../components/thread/ReplyExpander';
import { Tombstone } from '../components/thread/Tombstone';
import { dividerBefore, UnreadDivider } from '../components/thread/UnreadDivider';
import { useWindowClock } from '../components/thread/useWindowClock';
import '../components/thread/thread.css';
import { Toolbar } from '../shell/Toolbar';
import { isStaff, moderateKinds, threadActions, type PanelKind } from '../shell/threadActions';
import { ThreadFoot } from '../keyboard/ThreadFoot';
import { firstUnread, tracksUnread, useThreadShortcuts } from '../keyboard/shortcuts';
import { isPhone } from '../state/layout';
import { composerFor, loadReplyDraft, quotePost, replyToPost, type Slow } from '../state/composer';
import { forgetLists, listFor } from '../state/lists';
import { subscribe } from '../state/live';
import { marking, markUnread, readPosition, trackPosts } from '../state/readTracker';
import { useShellContext } from '../state/shell';
import { refreshViewer, viewer } from '../state/viewer';
import { cachedThread, loadCount, loadThreadPage, pageRequestedAt, threadUrl, type ThreadQuery } from '../state/threads';
import { canonicalThreadUrl, useThreadView } from '../view';
import { NotFound } from './Placeholder';
import { PostHistory } from './PostHistory';

// ThreadScreen is the thread page (docs/06 "The canonical view"): the posts
// of one page in order, the pager, and the divider between read and unread.
//
// useResource keys the answer on the request, so moving to another thread,
// or another page of this one, never renders the last answer's posts or 404,
// not even for one render. The title shown while a page loads, or after it
// fails, is the thread's own from the cache, which only ever holds this
// thread's. A failure that is not a 404 says so, with a retry; it does not
// leave an earlier 404 standing, which would call a thread that exists
// missing.
//
// With no page in the address the request names none, and the server lands a
// signed-in reader on the page holding their first unread post (docs/06
// "Landing on return", docs/07). The address is left as it is: it means
// "where I left off", and naming the page would cost a second request for the
// page already on screen.
export function ThreadScreen() {
  const { params, path } = useRoute();
  const { route, url } = useLocation();
  const view = useThreadView();
  const history = path.endsWith('/history');

  const q: ThreadQuery = {
    page: view.hasPage ? view.page : undefined,
    lens: view.lens || undefined,
    ordinal: params.ordinal,
  };
  const key = threadUrl(params.id, q);
  const res = useResource(
    key,
    (signal) => loadThreadPage(params.id, q, signal),
    () => peekGet<ThreadPage>(key),
  );
  // The page's viewer block is for whoever was signed in when it was asked
  // for. When someone else signs in in this tab, the page is asked for again,
  // and until it answers the posts offer no edit, delete or restore: the old
  // block's can_moderate and author windows are not the new viewer's
  // (TKT-01M3QHKD21, Terva review 1626). A failed answer leaves them
  // withheld, since the data kept beside it is still the old viewer's.
  const me = viewer.value.user?.handle ?? null;
  const [pageFor, setPageFor] = useState(me);
  useEffect(() => {
    if (me !== pageFor) res.retry();
  }, [me]);
  useEffect(() => {
    if (!res.loading && !res.error) setPageFor(me);
  }, [res.loading]);
  // The header shown is the newer of the page's and the cache's, by log
  // length: a moderation action's answer is stored as the cache's header
  // (storeThreadHeader) before the page is asked for again, so a lock or pin
  // shows at once.
  const cached = cachedThread(params.id)?.header;
  // While the canonical view is open, a live hint asks for the page on screen
  // again (useLive), and the newer answer is shown in place of the request's:
  // its header, and its viewer block for what the screen offers. Only the
  // divider keeps the request's read position (ThreadBody's read).
  const [live, setLive] = useState<Live | null>(null);
  const shown =
    res.data && live && live.key === key && live.me === me && live.page.thread.log_length > res.data.thread.log_length
      ? live.page
      : res.data;
  useLive(params.id, !history && !view.lens && !!(res.data || cached), { key, me, page: shown, retry: res.retry }, setLive);
  const pageHead = shown?.thread;
  const head = pageHead && cached && cached.log_length > pageHead.log_length ? cached : (pageHead ?? cached);

  // The slug is checked whenever the address or the answer changes, not only
  // when a request comes back: moving to an old slug of the thread already on
  // screen asks for nothing new, and must still land on the canonical URL.
  // It is checked against the header shown, so a rename's answer, stored
  // before the page is answered again, is not undone by the older page.
  //
  // The lens is checked against the request's answer too (heldSearch): a
  // bare unread takes the boundary the server resolved, and a lens the
  // server did not know leaves the address.
  useEffect(() => {
    if (!res.data || !head) return;
    const held = history ? null : heldSearch(location.search, res.data.lens);
    const at = held === null ? location : { pathname: location.pathname, search: held, hash: location.hash };
    const to = canonicalThreadUrl(at, head) ?? (held === null ? null : at.pathname + held + at.hash);
    if (to) route(to, true);
  }, [res.data, url, head?.slug]);

  // The lens on screen: the answer's, for an address that asked for one. A
  // lens page is the lens's own pages, and the landing rule does not apply
  // to it (docs/07).
  const lens = view.lens && shown?.lens ? shown.lens : null;
  const landing = !view.hasPage && !params.ordinal && !view.lens;
  useArrival(key, res.data, params.ordinal, landing);

  // The phone top bar shows the forum this thread is in, back to it, and the
  // thread's actions in its more menu; on desktop the same actions are the
  // toolbar under the title (docs/06 "Mobile"). The actions are offered only
  // for a thread the reader has been shown, and a thread that is not found
  // publishes nothing (review 490): its screen is NotFound, not the thread's.
  const missing = isProblem(res.error, 'not-found');
  const known = head ? { id: head.id, slug: head.slug } : null;
  // Reply summons the composer at the foot of the thread body, so it is
  // offered only where there is one: not on the post history route, which
  // shows no body (review 543).
  //
  // An archived thread takes nothing from anyone (docs/08 "Locking
  // semantics"). The header shown says so as soon as an archive answers, a
  // moment before the page is answered again, so what the page offers is
  // read from it too (TKT-01M3YSQQ33).
  const archived = head?.archived ?? false;
  const canReply = !history && !archived && (shown?.viewer.can_reply ?? false);
  // The reader's draft for the thread is loaded once they may reply, here
  // rather than in the composer, so the phone's reply bar shows it before
  // the sheet opens. loadReplyDraft asks once per thread per session.
  useEffect(() => {
    if (canReply) void loadReplyDraft(params.id);
  }, [params.id, canReply]);
  // First unread is offered to a signed-in reader with posts they have not
  // read, on the thread body, where there are posts to go to.
  const lastRead = shown?.viewer.last_read_ordinal ?? 0;
  const postCount = shown?.thread.post_count ?? 0;
  const unread = !history && !!shown && tracksUnread() && lastRead < postCount;
  // Lock, unlock, pin and unpin are offered to a viewer the page says
  // moderates this thread's forum (can_moderate), on the thread body. A
  // global pin is staff's by role, which the viewer's own role says
  // (moderateKinds). Slow mode is offered to the same viewers whatever the
  // thread's state. The panel is for the thread it was opened on, so moving
  // to another thread leaves it behind.
  const mayModerate = !history && !archived && (shown?.viewer.can_moderate ?? false);
  const staff = isStaff(viewer.value.user?.role);
  // Archive is an admin's, by role, on a thread not yet archived.
  const admin = viewer.value.user?.role === 'admin';
  const offerArchive = !history && !!shown && admin && !archived;
  const [moderating, setModerating] = useState<{ thread: string; kind: PanelKind } | null>(null);
  const locked = head?.locked ?? false;
  const pinned = head?.pinned ?? null;
  // Rename is offered to a viewer who moderates the thread's forum, and to
  // its author while the server's rename_until has not passed (docs/07
  // "Renaming and moving threads"). A timer withdraws it when the window
  // closes, set for at most maxTimeout and set again when it fires early
  // (delayUntil). A rename-window-closed answer withdraws it for this thread
  // at once (closedOn), whatever this clock says.
  const renameUntil = history ? null : (shown?.viewer.rename_until ?? null);
  const windowOpen = renameUntil !== null && Date.now() < renameUntil;
  const [tick, setTick] = useState(0);
  useEffect(() => {
    if (!windowOpen) return;
    const t = setTimeout(() => setTick((n) => n + 1), delayUntil(renameUntil!, Date.now()));
    return () => clearTimeout(t);
  }, [renameUntil, windowOpen, tick]);
  const [closedOn, setClosedOn] = useState<string | null>(null);
  const renameOpen = mayModerate || windowOpen;
  const offerRename = renameOpen && (mayModerate || closedOn !== known?.id);
  // Mark unread is offered to a signed-in reader on the thread body once a
  // page is on screen, whatever else they may do there: a member who may not
  // reply in a locked thread still has a position to lower. A press while a
  // mark is out is ignored (pressed), and a failure says so under the head
  // and leaves the reader where they are, to press again.
  const forumSlug = head?.forum.slug;
  const offerMark = !history && !!shown && tracksUnread() && !!forumSlug;
  const pressed = useRef(false);
  const [markFailed, setMarkFailed] = useState<{ thread: string; error: unknown } | null>(null);
  // Watch is offered to a signed-in member once the page's viewer block is
  // theirs (pageFor), on every view of the thread, since it is about the
  // thread and not the posts on screen. A saved level shows at once
  // (watchSet) on any page of the thread whose load started before the save
  // answered, whenever that page landed: a live refresh can land while the
  // save is out (Terva review 1829). A page asked for after the answer
  // carries the level itself, and the save's barrier (forgetAllThreadPages)
  // has any page asked for before it asked for again.
  const [watchSet, setWatchSet] = useState<{ thread: string; at: number; to: ThreadWatch } | null>(null);
  const [watchOpen, setWatchOpen] = useState<{ thread: string; returnTo: Element | null } | null>(null);
  const watch =
    shown && watchSet && watchSet.thread === params.id && pageRequestedAt(shown) <= watchSet.at ? watchSet.to : shown?.viewer;
  const offerWatch = !!watch && tracksUnread() && pageFor === me;
  // First unread goes by the position as the read tracker knows it at the
  // press, which reading since the page arrived has moved (Terva review
  // 1730), and under a held unread lens stays in the lens.
  // The phone's page sheet offers the same handler (Pager).
  const now = useRef(shown);
  now.current = shown;
  const goFirstUnread = useMemo(
    () =>
      known && unread ? () => firstUnread(known, readPosition(known.id, now.current), postCount, route, view.lens) : undefined,
    [known?.id, known?.slug, unread, postCount, view.lens],
  );
  const actions = useMemo(
    () =>
      known
        ? threadActions(known, {
            canReply,
            firstUnread: goFirstUnread,
            markUnread: offerMark
              ? () => {
                  if (pressed.current) return;
                  pressed.current = true;
                  setMarkFailed(null);
                  markAndReturn(known.id, forumSlug!, route)
                    .catch((error: unknown) => setMarkFailed({ thread: known.id, error }))
                    .finally(() => (pressed.current = false));
                }
              : undefined,
            moderate: mayModerate
              ? { locked, pinned, staff, open: (kind) => setModerating({ thread: known.id, kind }) }
              : undefined,
            rename: offerRename ? () => setModerating({ thread: known.id, kind: 'rename' }) : undefined,
            archive: offerArchive ? () => setModerating({ thread: known.id, kind: 'archive' }) : undefined,
            watch: offerWatch
              ? {
                  level: watch.watch,
                  forumLevel: watch.forum_watch,
                  open: () => setWatchOpen({ thread: known.id, returnTo: document.activeElement }),
                }
              : undefined,
          })
        : [],
    [
      known?.id,
      known?.slug,
      canReply,
      goFirstUnread,
      offerMark,
      forumSlug,
      mayModerate,
      staff,
      locked,
      pinned?.scope,
      pinned?.until,
      offerRename,
      offerArchive,
      offerWatch,
      watch?.watch,
      watch?.forum_watch,
    ],
  );
  const title = useRef<HTMLHeadingElement>(null);
  const panel = moderating && head && moderating.thread === head.id && !history ? moderating : null;
  useEffect(() => {
    setModerating(null);
    setMarkFailed(null);
    setWatchOpen(null);
  }, [params.id]);
  const forum = head?.forum;
  const ctx = useMemo(
    () =>
      missing
        ? null
        : {
            title: forum?.name ?? 'Thread',
            back: forum ? { href: `/f/${encodeURIComponent(forum.slug)}`, label: forum.name } : undefined,
            actions,
            // On a phone the reply bar opens this thread's composer.
            reply: canReply ? params.id : undefined,
          },
    [missing, forum?.slug, forum?.name, actions, canReply, params.id],
  );
  useShellContext(ctx, [ctx]);

  if (missing) return <NotFound />;
  const page = shown;
  return (
    <main class="thread-page" data-screen={history ? 'post-history' : 'thread'}>
      <header class="thread-head">
        {head && (
          <nav class="breadcrumb" aria-label="Breadcrumb">
            <a class="hit" href="/">
              Forums
            </a>
            <span aria-hidden="true">›</span>
            <a class="forum hit" href={`/f/${encodeURIComponent(head.forum.slug)}`}>
              {head.forum.name}
            </a>
          </nav>
        )}
        <h1 ref={title} tabIndex={-1}>
          {head?.title ?? 'Thread'}
        </h1>
        {head && (
          <p class="thread-meta">
            started by <span class="starter">{head.author.handle}</span> · <PostTime at={head.created_at} /> ·{' '}
            {head.post_count} {head.post_count === 1 ? 'post' : 'posts'}
          </p>
        )}
        {/* The thread's tags follow the line above, each a link to its
            forum's list filtered to it (docs/06 "The thread head"). */}
        {head && <TagChips tags={head.tags} forum={head.forum.slug} />}
        {archived ? (
          <p class="locked" data-thread-state="archived">
            This thread is archived.
          </p>
        ) : (
          head?.locked && (
            <p class="locked" data-thread-state="locked">
              This thread is locked.
            </p>
          )
        )}
        {head?.pinned && <PinnedNote pinned={head.pinned} />}
        <div class="thread-tools">
          {shown && !history && <LensBar page={shown} lens={lens} />}
          {!isPhone.value && <Toolbar actions={actions} label="Thread actions" />}
        </div>
        {markFailed && markFailed.thread === params.id && (
          <p class="unread-refused" role="alert" data-mark-unread-refused>
            This thread could not be marked unread. {problemMessage(markFailed.error).text}
          </p>
        )}
      </header>
      {panel && head && (
        <ModerateDialog
          key={`${panel.thread} ${panel.kind}`}
          thread={panel.thread}
          kind={panel.kind}
          pinned={head.pinned}
          staff={staff}
          slowSeconds={shown?.viewer.slow_mode_seconds}
          offered={
            panel.kind === 'rename'
              ? offerRename
              : panel.kind === 'archive'
                ? offerArchive
              : mayModerate &&
                (panel.kind === 'move' || panel.kind === 'slow-mode' || panel.kind === 'tags' || moderateKinds({ locked, pinned, staff }).includes(panel.kind))
          }
          returnTo={() => title.current}
          onChanged={res.retry}
          onClose={() => setModerating(null)}
          current={{ title: head.title, forum: head.forum.slug, tags: head.tags }}
          admin={admin}
          onWindowClosed={() => setClosedOn(head.id)}
        />
      )}
      {watchOpen && offerWatch && watchOpen.thread === params.id && (
        <WatchDialog
          target={{ thread: watchOpen.thread, forumLevel: watch.forum_watch }}
          level={watch.watch}
          returnTo={watchOpen.returnTo}
          fallback={() => title.current}
          onSaved={(to) => setWatchSet({ thread: watchOpen.thread, at: loadCount(), to: to as ThreadWatch })}
          onClose={() => setWatchOpen(null)}
        />
      )}
      {res.error && <ErrorPanel error={res.error} what="This thread" onRetry={res.retry} />}
      {history ? (
        <PostHistory thread={head ?? { id: params.id, slug: params.slug }} ordinal={params.ordinal} />
      ) : (
        <>
          {page && (
            <ThreadBody
              key={key}
              page={page}
              read={res.data!.viewer}
              opening={res.loading}
              lens={lens}
              onChanged={res.retry}
              current={pageFor === me}
              archived={archived}
              firstUnread={goFirstUnread}
            />
          )}
          {!page && !res.error && <p class="loading">Loading…</p>}
        </>
      )}
    </main>
  );
}

// PinnedNote says a thread is pinned, where, and until when, in the thread
// head. The header's pin is null once its end has passed (docs/08: a pin's
// expiry needs no job), so the client never compares the time itself.
function PinnedNote({ pinned }: { pinned: Pinned }) {
  const { timezone, locale } = viewer.value.settings;
  const where = pinned.scope === 'global' ? 'Pinned on every forum' : 'Pinned in this forum';
  return (
    <p class="pinned" data-thread-state="pinned" data-pin-scope={pinned.scope}>
      {where}
      {pinned.until !== null && (
        <>
          {' until '}
          <time dateTime={new Date(pinned.until).toISOString()}>
            {new Date(pinned.until).toLocaleString(locale || undefined, {
              dateStyle: 'medium',
              timeStyle: 'short',
              timeZone: timezone || undefined,
            })}
          </time>
        </>
      )}
      .
    </p>
  );
}

function ThreadBody({
  page,
  read,
  opening,
  lens,
  onChanged,
  current,
  archived,
  firstUnread,
}: {
  page: ThreadPage;
  // read is the viewer block of the page as the request answered it, whose
  // read position the divider is drawn at. A live refetch reports the
  // position the reader has moved since, and the divider stays where it was
  // drawn rather than moving under them.
  read: ThreadViewer;
  // opening is whether the request for this page is still out.
  opening: boolean;
  // lens is the lens on screen, null for the full thread.
  lens: Lens | null;
  onChanged: () => void;
  // current is whether page was asked for by the viewer signed in now.
  current: boolean;
  // archived is whether the thread shown is archived, by the newest header:
  // the page then offers what readOnlyViewer leaves, no flag, and no
  // controls on posts.
  archived: boolean;
  // firstUnread is the thread page's First unread, when it offers one.
  firstUnread?: () => void;
}) {
  const thread: ThreadRef = { id: page.thread.id, slug: page.thread.slug };
  const lastRead = read.last_read_ordinal;
  const { route } = useLocation();

  // The new-posts pill (docs/06 "Live updates"). known is the thread's post
  // count when the page opened, or when the pill was last taken: posts past
  // it are held behind the pill rather than drawn, so nothing moves under
  // the reader. While the page's own request is out, a held copy may be on
  // screen, and known follows what it answers. A reader at the bottom, with
  // every new post on this page, gets them drawn as they come.
  const count = page.thread.post_count;
  const known = useRef(count);
  const settled = useRef(false);
  if (!settled.current) {
    known.current = count;
    settled.current = !opening;
  }
  const [, redraw] = useState(0);
  const posts = page.posts.filter((p) => p.ordinal <= known.current);
  const waiting = Math.max(0, count - known.current);
  const fits = page.posts.length - posts.length === waiting;
  const posting = composerFor(thread.id).posting.value;
  const end = useRef<HTMLDivElement>(null);
  const bottom = useAtBottom(end);
  const focus = useRef(0);
  // take draws the held posts; from the pill, focus goes to the first.
  const take = (focusFirst: boolean) => {
    if (known.current >= count) return;
    if (focusFirst) focus.current = known.current + 1;
    known.current = count;
    redraw((n) => n + 1);
  };
  // Before paint, so a post that streams in never shows the pill first.
  useLayoutEffect(() => {
    if (bottom && fits) take(false);
  }, [bottom, fits, count]);
  useLayoutEffect(() => {
    if (focus.current) document.getElementById(`post-${focus.current}`)?.focus();
    focus.current = 0;
    const a = anchor;
    anchor = null;
    const el = a && document.getElementById(a.id);
    if (el) scrollBy(0, el.getBoundingClientRect().top - a!.top);
  });
  const show = () => {
    if (fits) take(true);
    else route(permalink(thread, known.current + 1));
  };

  // The unread divider is the canonical view's, and the unread lens's, whose
  // posts run on from the reader's place; under any other lens the posts
  // around it are not the thread's.
  const ordinals = posts.map((p) => p.ordinal);
  const divider = !lens || lens.kind === 'unread' ? dividerBefore(ordinals, lastRead, known.current) : null;
  const gaps = placeGaps(ordinals, lens?.gaps ?? []);
  const lensArg = lens ? lensParam(lens.kind, lens.value) : '';
  const [popover, setPopover] = useState<number | null>(null);

  // Quote and reply, and the composer they feed, are offered to a reader
  // who may reply here, which the server decides (can_reply: the permission,
  // and the lock unless they moderate).
  const v = archived ? readOnlyViewer(page.viewer) : page.viewer;
  const canReply = v.can_reply;
  const actions: PostActions | undefined = canReply
    ? { quote: (o, text) => quotePost(thread.id, o, text), reply: (o) => replyToPost(thread.id, o) }
    : undefined;
  // Flag is offered to a signed-in member (a pre-user's viewer has no
  // user), on the posts Post allows (mayFlag). The dialog is told where
  // focus goes back to, because on a phone the item that opened it is gone.
  const [flagging, setFlagging] = useState<{ ordinal: number; returnTo: HTMLElement } | null>(null);
  // An archived thread takes no flag (docs/07 `POST /threads/{id}/flags`).
  const onFlag: FlagHandler | undefined =
    viewer.value.user && !archived ? (ordinal, returnTo) => setFlagging({ ordinal, returnTo }) : undefined;
  // Edit, delete and restore on the viewer's own posts, and on every post for
  // a viewer who moderates the forum, offered by one rule at this render's
  // now (postControls). The clock renders the page again when the soonest
  // offer ends, and a change asks for the page again (TKT-01M3PTT97X,
  // TKT-01M3QHKD21). Nothing is offered while the page is another viewer's
  // (current false), until the page asked for again answers.
  const now = Date.now();
  const offers = posts.map((p) => (current ? postControls(p, v, now) : noControls));
  useWindowClock(offers.map((c) => c.closesAt));
  const windows = v.author_windows;
  const moderates = v.can_moderate && !!viewer.value.user;
  const ownOf = (p: PostDTO, i: number): Own | undefined =>
    current && (moderates || isOwn(p))
      ? {
          thread: thread.id,
          offer: offers[i],
          hides: !isOwn(p),
          deleteUntil: windows ? p.created_at + windows.delete_seconds * 1000 : null,
          logLength: page.thread.log_length,
          onChanged,
        }
      : undefined;
  // Unread from here is offered to a signed-in reader, on its own rule
  // rather than with quote and reply, as flag is: it makes that post and
  // every later one unread, and returns to the list.
  const onUnread: UnreadHandler | undefined = tracksUnread()
    ? (ordinal) => markAndReturn(thread.id, page.thread.forum.slug, route, ordinal - 1)
    : undefined;
  const postOf = (o: number) => page.posts.find((p) => p.ordinal === o);
  const authorOf = (o: number) => postOf(o)?.author.handle;
  // On a phone the composer is a sheet the reply bar opens (Shell), and
  // the inline one is not drawn.
  const sheetOpen = composerFor(thread.id).open.value;
  // Slow mode as the page says it; whoever moderates the thread is exempt.
  const slow: Slow = {
    seconds: page.viewer.slow_mode_seconds,
    nextPostAt: page.viewer.next_post_at,
    exempt: v.can_moderate,
  };
  useThreadShortcuts(page, lensArg, route);
  const list = useRef<HTMLElement>(null);
  // The beacon runs on the canonical view, and under the unread lens only
  // from a boundary at or below the reader's position (lens.ts).
  const tracks = tracksUnder(lens, readPosition(thread.id, page));
  useReadTracking(thread.id, list, page, lastRead, posts.length, tracks);
  // A posted reply is shown where it landed: its permalink, which asks for
  // the page holding it at the log length the post made, and scrolls to it.
  // Nothing moves a reader who has left the thread while it was posting.
  const posted = (res: CreatedPost) => {
    if (onThread(thread.id)) route(permalink(thread, res.ordinal));
  };

  // One handler for every link to another post in this list: a reply-to
  // indicator, or a quote's source link in a body, the first line of the
  // quotation rather than its summary (internal/render/quote.go).
  // A post on this page is scrolled to and the address stays as it is; one
  // that is not opens in a popover (docs/06 "Post anatomy").
  const follow = (e: MouseEvent) => {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    const link = (e.target as Element).closest?.('a[data-reply-to], a.quote-source');
    if (!link) return;
    const ordinal = Number((link.getAttribute('href') ?? '').replace(/^#post-/, ''));
    if (!Number.isSafeInteger(ordinal) || ordinal < 1) return;
    e.preventDefault();
    const here = (e.currentTarget as Element).querySelector(`#post-${ordinal}`);
    if (here) {
      setPopover(null);
      here.scrollIntoView?.({ block: 'start' });
    } else setPopover(ordinal);
  };

  const pager = (
    <Pager page={page.page} thread={thread} lens={lensArg} postCount={page.thread.post_count} firstUnread={firstUnread} />
  );
  return (
    <>
      {lens && <LensHeader lens={lens} thread={thread} first={posts[0]?.ordinal} />}
      {pager}
      <section class="posts" ref={list} onClick={follow}>
        {posts.map((p, i) => (
          <Fragment key={p.ordinal}>
            {gaps.before.has(p.ordinal) && <GapDivider gap={gaps.before.get(p.ordinal)!} thread={thread} />}
            {p.ordinal === divider && <UnreadDivider unread={known.current - lastRead} lastRead={lastRead} at={read.last_read_at} />}
            {isTombstone(p) ? (
              <Tombstone
                post={p}
                thread={thread}
                unread={lastRead > 0 && p.ordinal > lastRead}
                own={ownOf(p, i)}
                expander={expanderShown(p, posts[i + 1])}
              />
            ) : (
              <Post
                post={p}
                thread={thread}
                unread={lastRead > 0 && p.ordinal > lastRead}
                actions={actions}
                onFlag={onFlag}
                onUnread={onUnread}
                own={ownOf(p, i)}
                expander={expanderShown(p, posts[i + 1])}
              />
            )}
          </Fragment>
        ))}
        {gaps.end && <GapDivider gap={gaps.end} thread={thread} />}
      </section>
      <div class="posts-end" ref={end} />
      <NewPostsPill count={posting ? 0 : waiting} onShow={show} />
      {pager}
      <ThreadFoot page={page} route={route} />
      {canReply &&
        (isPhone.value ? (
          sheetOpen && (
            <ComposerSheet
              thread={thread.id}
              authorOf={authorOf}
              postOf={postOf}
              onPosted={posted}
              slow={slow}
              canUpload={v.can_upload}
            />
          )
        ) : (
          <Composer thread={thread.id} authorOf={authorOf} onPosted={posted} slow={slow} canUpload={v.can_upload} />
        ))}
      {popover !== null && <PostPopover thread={thread} ordinal={popover} onClose={() => setPopover(null)} />}
      {flagging && (
        <FlagDialog
          thread={thread.id}
          ordinal={flagging.ordinal}
          returnTo={flagging.returnTo}
          onClose={() => setFlagging(null)}
        />
      )}
    </>
  );
}

// useReadTracking sends what a signed-in reader reads of this page as their
// read position (state/readTracker.ts). The body is keyed on the request, so
// it unmounts on every navigation away from this page, and stopping flushes.
// The divider stays where the page drew it: what is read while the page is
// open moves it on the next visit, not under the reader.
//
// Only the posts drawn are tracked, so one held behind the new-posts pill is
// not read, and tracking starts again when more are drawn.
//
// on is false under a lens the beacon does not run under (lens.ts).
function useReadTracking(
  id: string,
  list: { current: HTMLElement | null },
  page: ThreadPage,
  lastRead: number,
  drawn: number,
  on: boolean,
) {
  const signedIn = !!viewer.value.user;
  useEffect(() => {
    if (!on || !signedIn || !list.current) return;
    return trackPosts(id, list.current, lastRead, page);
  }, [id, page, signedIn, drawn, on]);
}

// Live is a page a live hint asked for again: for the request key and the
// viewer it was asked for, so moving on, or signing in as someone else,
// leaves it behind.
interface Live {
  key: string;
  me: string | null;
  page: ThreadPage;
}

// useLive holds the live-update subscription (state/live.ts) while on is
// true: on the canonical view of a thread the reader has been shown, so
// moving between its pages keeps one stream. It closes when the screen
// leaves the thread, or the thread leaves the cache (access withdrawn).
//
// A hint, or a poll, asks for the page on screen again, in full and by its
// number, never an after_log_length fragment: a removal or a restore writes
// no revision, so a fragment holding an edit would leave a removed post on
// screen (docs/06, Terva review 1696). A landing request, which names no
// page, is not repeated, because the server would land it on the reader's
// new position. The answer is shown if it is newer than the page on screen,
// with the first post in view held where it was (anchor). An answer that
// withdraws access asks for the page through the screen's request, which
// then shows the refusal.
//
// A lens page holds no subscription: the unread lens is recomputed from the
// read position the reader is moving, so asking for it again would take
// posts away under them, and the pill counts the whole thread's posts
// (docs/06, run decision of 2026-10-01).
function useLive(
  id: string,
  on: boolean,
  now: { key: string; me: string | null; page: ThreadPage | undefined; retry: () => void },
  setLive: (l: Live) => void,
) {
  const at = useRef(now);
  at.current = now;
  // A hint or a poll while no page is on screen, as when moving to another
  // page of the thread, is kept, and the page is asked for again once it
  // arrives: its request may have been answered from before the change, and
  // a live stream sends nothing more just because a page arrived (Terva
  // review 1778).
  const missed = useRef(false);
  const run = useRef<(() => void) | null>(null);
  useEffect(() => {
    if (!missed.current || !now.page || !run.current) return;
    missed.current = false;
    run.current();
  }, [now.page]);
  useEffect(() => {
    if (!on) return;
    // An answer that lands after the subscription closed is for a screen
    // that has gone, and sets nothing.
    let open = true;
    const held = () => at.current.page?.thread.log_length ?? -1;
    // A failure other than withdrawn access rejects, and the subscription
    // tries again (state/live.ts).
    const refresh = () => {
      const { key, me, page, retry } = at.current;
      if (!page) return void (missed.current = true);
      return loadThreadPage(id, { page: page.page.number }).then(
        (p) => {
          if (!open || at.current.key !== key || p.thread.log_length <= held()) return;
          anchor = firstInView();
          setLive({ key, me, page: p });
        },
        (err: unknown) => {
          if (!open || at.current.key !== key) return;
          if (revokesAccess(err)) retry();
          else throw err;
        },
      );
    };
    const sub = subscribe(id, held, refresh);
    run.current = sub.refresh;
    return () => {
      open = false;
      run.current = null;
      missed.current = false;
      sub.close();
    };
  }, [id, on]);
}

// anchor is the first post in view, and its top, before a live page is
// drawn: the thread body scrolls by however far it moved, so an edit or a
// removal above it does not move the text being read.
let anchor: { id: string; top: number } | null = null;

function firstInView() {
  for (const el of document.querySelectorAll('.posts > [data-ordinal]')) {
    const r = el.getBoundingClientRect();
    if (r.bottom > 0) return { id: el.id, top: r.top };
  }
  return null;
}

// useAtBottom is whether the end of the last post drawn is in view, within
// 48 px (docs/06 "Live updates"). end is an empty element after the list,
// which thread.css moves down by the phone's reply bar, so the end of a list
// still behind the bar is not in view (Terva review 1778).
function useAtBottom(end: { current: HTMLElement | null }) {
  const [bottom, setBottom] = useState(false);
  useEffect(() => {
    const io = new IntersectionObserver((es) => setBottom(es[es.length - 1].isIntersecting), {
      rootMargin: '0px 0px 48px 0px',
    });
    io.observe(end.current!);
    return () => io.disconnect();
  }, []);
  return bottom;
}

// onThread is whether the reader is still on a page of thread id.
const onThread = (id: string) => location.pathname.startsWith(`/t/${encodeURIComponent(id)}/`);

// onThreadBody is whether the reader is still on thread id's posts: a page
// of it, not a post's history, which is a screen of its own that a reader
// who went there while a mark was out has chosen (Terva review 1622).
const onThreadBody = (id: string) => onThread(id) && !location.pathname.endsWith('/history');

// markAndReturn marks thread id unread after ordinal, or all of it with none
// (state/readTracker.ts), and then returns the reader to the list they came
// from, or the forum's (docs/06 "Read tracking"): staying would read the
// posts on screen again within a second and undo the mark. The held forum
// index and thread lists carry the counts the mark changed, so they are
// dropped rather than shown on the way back, and the viewer's badge is asked
// for again.
//
// The thread's history entry is replaced, so Back does not reopen the page
// just marked. A reader who has left the thread's posts while it was
// marking, for another screen or a post's history, stays where they went. It rejects with the mark's failure, for the caller to show.
//
// Marks of one thread run in call order, and only the last to settle leaves
// the page: a list shown after an earlier one would hold counts a later one
// is about to change, and nothing asks for a list already on screen again
// (Terva review 1619 on PR #248). Each one that succeeds drops the held
// counts, so what it changed is not shown whatever the later ones do. The
// last leaves if any of them succeeded, even if it failed itself: the
// position has been lowered, and staying would read it back up (Terva
// review 1623). Its failure still rejects, for a caller that is still on
// the page to show.
//
// The viewer is asked for again and not waited for: the badge is a signal
// and follows /me whenever it answers, on whatever screen is showing, and
// the return does not wait on a request that may be slow or fail, as the
// admin screens' refreshes do not (Terva review 1623, disagreement
// recorded on TKT-01M3KFFFJR).
async function markAndReturn(id: string, forum: string, route: (url: string, replace?: boolean) => void, ordinal?: number) {
  try {
    await markUnread(id, ordinal);
    landed.add(id);
    // A list GET still out began before the DELETE answered, and is not
    // held when it lands (Terva review 1624); the cross-forum lists are
    // lists too, and the reader may be going back to one (TKT-01M3V6Y4JP).
    forgetLists();
    void refreshViewer().catch(() => undefined);
  } finally {
    if (!marking(id) && landed.delete(id) && onThreadBody(id)) route(listFor(forum), true);
  }
}

// landed is the threads a mark has succeeded for since the last of their
// queued marks settled.
const landed = new Set<string>();

// useArrival scrolls once per request key, when its page first arrives:
// to the permalink's post; else to the post the address's #post-N names;
// else, on a landing, to the divider, or to the last post read when the
// divider is not on this page. A revalidation of the same page does not move
// the reader.
function useArrival(key: string, page: ThreadPage | undefined, ordinal: string | undefined, landing: boolean) {
  const done = useRef<string | null>(null);
  useEffect(() => {
    if (!page || done.current === key) return;
    done.current = key;
    const byId = (id: string) => document.getElementById(id);
    const hash = /^#post-([1-9][0-9]*)$/.exec(location.hash)?.[1];
    const target =
      (ordinal && byId(`post-${ordinal}`)) ||
      (hash && byId(`post-${hash}`)) ||
      (landing && (byId('unread') || byId(`post-${page.viewer.last_read_ordinal}`))) ||
      null;
    target?.scrollIntoView?.({ block: 'start' });
  }, [key, page]);
}
