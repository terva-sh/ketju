import { Fragment } from 'preact';
import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { useLocation, useRoute } from 'preact-iso';
import { forgetGets, peekGet } from '../api/client';
import type { CreatedPost, Pinned, Post as PostDTO, ThreadPage } from '../api/dto';
import { isProblem, problemMessage } from '../api/problem';
import { useResource } from '../api/resource';
import { ErrorPanel } from '../components/ErrorPanel';
import { Composer } from '../composer/Composer';
import { ComposerSheet } from '../composer/ComposerSheet';
import { delayUntil } from '../components/thread/deadline';
import { FlagDialog } from '../components/thread/FlagDialog';
import { ModerateDialog } from '../components/thread/ModerateDialog';
import { Pager } from '../components/thread/Pager';
import {
  permalink,
  Post,
  type FlagHandler,
  type PostActions,
  type ThreadRef,
  type UnreadHandler,
} from '../components/thread/Post';
import { isOwn, postControls } from '../components/thread/postControls';
import type { Own } from '../components/thread/OwnControls';
import { PostPopover } from '../components/thread/PostPopover';
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
import { listFor } from '../state/lists';
import { markUnread, trackPosts } from '../state/readTracker';
import { useShellContext } from '../state/shell';
import { refreshViewer, viewer } from '../state/viewer';
import { cachedThread, loadThreadPage, threadUrl, type ThreadQuery } from '../state/threads';
import { canonicalThreadUrl, useThreadView } from '../view';
import { NotFound } from './Placeholder';

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
  // The header shown is the newer of the page's and the cache's, by log
  // length: a moderation action's answer is stored as the cache's header
  // (storeThreadHeader) before the page is asked for again, so a lock or pin
  // shows at once.
  const cached = cachedThread(params.id)?.header;
  const pageHead = res.data?.thread;
  const head = pageHead && cached && cached.log_length > pageHead.log_length ? cached : (pageHead ?? cached);

  // The slug is checked whenever the address or the answer changes, not only
  // when a request comes back: moving to an old slug of the thread already on
  // screen asks for nothing new, and must still land on the canonical URL.
  // It is checked against the header shown, so a rename's answer, stored
  // before the page is answered again, is not undone by the older page.
  useEffect(() => {
    if (!res.data || !head) return;
    const to = canonicalThreadUrl(location, head);
    if (to) route(to, true);
  }, [res.data, url, head?.slug]);

  const landing = !view.hasPage && !params.ordinal;
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
  const canReply = !history && (res.data?.viewer.can_reply ?? false);
  // The reader's draft for the thread is loaded once they may reply, here
  // rather than in the composer, so the phone's reply bar shows it before
  // the sheet opens. loadReplyDraft asks once per thread per session.
  useEffect(() => {
    if (canReply) void loadReplyDraft(params.id);
  }, [params.id, canReply]);
  // First unread is offered to a signed-in reader with posts they have not
  // read, on the thread body, where there are posts to go to.
  const lastRead = res.data?.viewer.last_read_ordinal ?? 0;
  const postCount = res.data?.thread.post_count ?? 0;
  const unread = !history && !!res.data && tracksUnread() && lastRead < postCount;
  // Lock, unlock, pin and unpin are offered to a viewer the page says
  // moderates this thread's forum (can_moderate), on the thread body. A
  // global pin is staff's by role, which the viewer's own role says
  // (moderateKinds). Slow mode is offered to the same viewers whatever the
  // thread's state. The panel is for the thread it was opened on, so moving
  // to another thread leaves it behind.
  const mayModerate = !history && (res.data?.viewer.can_moderate ?? false);
  const staff = isStaff(viewer.value.user?.role);
  const [moderating, setModerating] = useState<{ thread: string; kind: PanelKind } | null>(null);
  const locked = head?.locked ?? false;
  const pinned = head?.pinned ?? null;
  // Rename is offered to a viewer who moderates the thread's forum, and to
  // its author while the server's rename_until has not passed (docs/07
  // "Renaming and moving threads"). A timer withdraws it when the window
  // closes, set for at most maxTimeout and set again when it fires early
  // (delayUntil). A rename-window-closed answer withdraws it for this thread
  // at once (closedOn), whatever this clock says.
  const renameUntil = history ? null : (res.data?.viewer.rename_until ?? null);
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
  // mark is out is ignored (marking), and a failure says so under the head
  // and leaves the reader where they are, to press again.
  const forumSlug = head?.forum.slug;
  const offerMark = !history && !!res.data && tracksUnread() && !!forumSlug;
  const marking = useRef(false);
  const [markFailed, setMarkFailed] = useState<{ thread: string; error: unknown } | null>(null);
  const actions = useMemo(
    () =>
      known
        ? threadActions(known, {
            canReply,
            firstUnread: unread ? () => firstUnread(known, lastRead, postCount, route) : undefined,
            markUnread: offerMark
              ? () => {
                  if (marking.current) return;
                  marking.current = true;
                  setMarkFailed(null);
                  markAndReturn(known.id, forumSlug!, route)
                    .catch((error: unknown) => setMarkFailed({ thread: known.id, error }))
                    .finally(() => (marking.current = false));
                }
              : undefined,
            moderate: mayModerate
              ? { locked, pinned, staff, open: (kind) => setModerating({ thread: known.id, kind }) }
              : undefined,
            rename: offerRename ? () => setModerating({ thread: known.id, kind: 'rename' }) : undefined,
          })
        : [],
    [
      known?.id,
      known?.slug,
      canReply,
      unread,
      lastRead,
      postCount,
      offerMark,
      forumSlug,
      mayModerate,
      staff,
      locked,
      pinned?.scope,
      pinned?.until,
      offerRename,
    ],
  );
  const title = useRef<HTMLHeadingElement>(null);
  const panel = moderating && head && moderating.thread === head.id && !history ? moderating : null;
  useEffect(() => {
    setModerating(null);
    setMarkFailed(null);
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
  const page = res.data;
  return (
    <main class="thread-page" data-screen={history ? 'post-history' : 'thread'}>
      <header class="thread-head">
        {head && (
          <a class="forum hit" href={`/f/${encodeURIComponent(head.forum.slug)}`}>
            {head.forum.name}
          </a>
        )}
        <h1 ref={title} tabIndex={-1}>
          {head?.title ?? 'Thread'}
        </h1>
        {head?.locked && (
          <p class="locked" data-thread-state="locked">
            This thread is locked.
          </p>
        )}
        {head?.pinned && <PinnedNote pinned={head.pinned} />}
        {!isPhone.value && <Toolbar actions={actions} label="Thread actions" />}
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
          slowSeconds={res.data?.viewer.slow_mode_seconds}
          offered={
            panel.kind === 'rename'
              ? offerRename
              : mayModerate &&
                (panel.kind === 'move' || panel.kind === 'slow-mode' || moderateKinds({ locked, pinned, staff }).includes(panel.kind))
          }
          returnTo={() => title.current}
          onChanged={res.retry}
          onClose={() => setModerating(null)}
          current={{ title: head.title, forum: head.forum.slug }}
          admin={viewer.value.user?.role === 'admin'}
          onWindowClosed={() => setClosedOn(head.id)}
        />
      )}
      {res.error && <ErrorPanel error={res.error} what="This thread" onRetry={res.retry} />}
      {history ? (
        <PostHistoryPending thread={head ?? { id: params.id, slug: params.slug }} ordinal={params.ordinal} />
      ) : (
        <>
          {page && <ThreadBody key={key} page={page} lens={view.lens} onChanged={res.retry} />}
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

// PostHistoryPending holds the history route's place until a post's history
// has a screen of its own (TKT-01M39WZY8C, Build the post history screen). It
// says what the address is, keeps the thread's title and canonical slug, and
// links back to the post, rather than showing the thread page as if it were
// the history (review 421).
function PostHistoryPending({ thread, ordinal }: { thread: ThreadRef; ordinal: string }) {
  return (
    <section class="history-pending" data-history-for={ordinal}>
      <h2>History of post #{ordinal}</h2>
      <p>This screen is not built yet.</p>
      <p>
        <a href={permalink(thread, Number(ordinal))}>Back to post #{ordinal}</a>
      </p>
    </section>
  );
}

function ThreadBody({ page, lens, onChanged }: { page: ThreadPage; lens: string; onChanged: () => void }) {
  const thread: ThreadRef = { id: page.thread.id, slug: page.thread.slug };
  const lastRead = page.viewer.last_read_ordinal;
  const divider = dividerBefore(
    page.posts.map((p) => p.ordinal),
    lastRead,
    page.thread.post_count,
  );
  const [popover, setPopover] = useState<number | null>(null);

  // Quote and reply, and the composer they feed, are offered to a reader
  // who may reply here, which the server decides (can_reply: the permission,
  // and the lock unless they moderate).
  const canReply = page.viewer.can_reply;
  const actions: PostActions | undefined = canReply
    ? { quote: (o, text) => quotePost(thread.id, o, text), reply: (o) => replyToPost(thread.id, o) }
    : undefined;
  // Flag is offered to a signed-in member (a pre-user's viewer has no
  // user), on the posts Post allows (mayFlag). The dialog is told where
  // focus goes back to, because on a phone the item that opened it is gone.
  const [flagging, setFlagging] = useState<{ ordinal: number; returnTo: HTMLElement } | null>(null);
  const onFlag: FlagHandler | undefined = viewer.value.user
    ? (ordinal, returnTo) => setFlagging({ ordinal, returnTo })
    : undefined;
  // Edit, delete and restore on the viewer's own posts, offered by one rule
  // at this render's now (postControls). The clock renders the page again
  // when the soonest offer ends, and a change asks for the page again
  // (TKT-01M3PTT97X).
  const now = Date.now();
  const offers = page.posts.map((p) => postControls(p, page.viewer, now));
  useWindowClock(offers.map((c) => c.closesAt));
  const windows = page.viewer.author_windows;
  const ownOf = (p: PostDTO, i: number): Own | undefined =>
    isOwn(p)
      ? {
          thread: thread.id,
          offer: offers[i],
          deleteUntil: windows ? p.created_at + windows.delete_seconds * 1000 : null,
          logLength: page.thread.log_length,
          onChanged,
        }
      : undefined;
  // Unread from here is offered to a signed-in reader, on its own rule
  // rather than with quote and reply, as flag is: it makes that post and
  // every later one unread, and returns to the list.
  const { route } = useLocation();
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
    exempt: page.viewer.can_moderate,
  };
  useThreadShortcuts(page, lens, route);
  const list = useRef<HTMLElement>(null);
  useReadTracking(thread.id, list, page, lastRead);
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

  const pager = <Pager page={page.page} thread={thread} lens={lens} postCount={page.thread.post_count} />;
  return (
    <>
      {pager}
      <section class="posts" ref={list} onClick={follow}>
        {page.posts.map((p, i) => (
          <Fragment key={p.ordinal}>
            {p.ordinal === divider && <UnreadDivider unread={page.thread.post_count - lastRead} lastRead={lastRead} />}
            {isTombstone(p) ? (
              <Tombstone
                post={p}
                thread={thread}
                unread={lastRead > 0 && p.ordinal > lastRead}
                own={ownOf(p, i)}
                expander={expanderShown(p, page.posts[i + 1])}
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
                expander={expanderShown(p, page.posts[i + 1])}
              />
            )}
          </Fragment>
        ))}
      </section>
      {pager}
      <ThreadFoot page={page} route={route} />
      {canReply &&
        (isPhone.value ? (
          sheetOpen && <ComposerSheet thread={thread.id} authorOf={authorOf} postOf={postOf} onPosted={posted} slow={slow} />
        ) : (
          <Composer thread={thread.id} authorOf={authorOf} onPosted={posted} slow={slow} />
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
function useReadTracking(id: string, list: { current: HTMLElement | null }, page: ThreadPage, lastRead: number) {
  const signedIn = !!viewer.value.user;
  useEffect(() => {
    if (!signedIn || !list.current) return;
    return trackPosts(id, list.current, lastRead, page);
  }, [id, page, signedIn]);
}

// onThread is whether the reader is still on a page of thread id.
const onThread = (id: string) => location.pathname.startsWith(`/t/${encodeURIComponent(id)}/`);

// markAndReturn marks thread id unread after ordinal, or all of it with none
// (state/readTracker.ts), and then returns the reader to the list they came
// from, or the forum's (docs/06 "Read tracking"): staying would read the
// posts on screen again within a second and undo the mark. The held forum
// index and thread lists carry the counts the mark changed, so they are
// dropped rather than shown on the way back, and the viewer's badge is asked
// for again.
//
// The thread's history entry is replaced, so Back does not reopen the page
// just marked. A reader who has left the thread while it was marking stays
// where they went. It rejects with the mark's failure, for the caller to show.
async function markAndReturn(id: string, forum: string, route: (url: string, replace?: boolean) => void, ordinal?: number) {
  await markUnread(id, ordinal);
  forgetGets((u) => u.startsWith('/api/v1/forums'));
  void refreshViewer().catch(() => undefined);
  if (onThread(id)) route(listFor(forum), true);
}

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
