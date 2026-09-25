import { Fragment } from 'preact';
import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { useLocation, useRoute } from 'preact-iso';
import { peekGet } from '../api/client';
import type { CreatedPost, ThreadPage } from '../api/dto';
import { isProblem } from '../api/problem';
import { useResource } from '../api/resource';
import { ErrorPanel } from '../components/ErrorPanel';
import { Composer } from '../composer/Composer';
import { ComposerSheet } from '../composer/ComposerSheet';
import { Pager } from '../components/thread/Pager';
import { permalink, Post, type PostActions, type ThreadRef } from '../components/thread/Post';
import { PostPopover } from '../components/thread/PostPopover';
import { Tombstone } from '../components/thread/Tombstone';
import { dividerBefore, UnreadDivider } from '../components/thread/UnreadDivider';
import '../components/thread/thread.css';
import { Toolbar } from '../shell/Toolbar';
import { threadActions } from '../shell/threadActions';
import { isPhone } from '../state/layout';
import { composerFor, quotePost, replyToPost } from '../state/composer';
import { useShellContext } from '../state/shell';
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
  const head = res.data?.thread ?? cachedThread(params.id)?.header;

  // The slug is checked whenever the address or the answer changes, not only
  // when a request comes back: moving to an old slug of the thread already on
  // screen asks for nothing new, and must still land on the canonical URL.
  useEffect(() => {
    if (!res.data) return;
    const to = canonicalThreadUrl(location, res.data.thread);
    if (to) route(to, true);
  }, [res.data, url]);

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
  const actions = useMemo(() => (known ? threadActions(known, { canReply }) : []), [known?.id, known?.slug, canReply]);
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
          <a class="forum" href={`/f/${encodeURIComponent(head.forum.slug)}`}>
            {head.forum.name}
          </a>
        )}
        <h1>{head?.title ?? 'Thread'}</h1>
        {head?.locked && <p class="locked">This thread is locked.</p>}
        {!isPhone.value && <Toolbar actions={actions} label="Thread actions" />}
      </header>
      {res.error && <ErrorPanel error={res.error} what="This thread" onRetry={res.retry} />}
      {history ? (
        <PostHistoryPending thread={head ?? { id: params.id, slug: params.slug }} ordinal={params.ordinal} />
      ) : (
        <>
          {page && <ThreadBody key={key} page={page} lens={view.lens} />}
          {!page && !res.error && <p class="loading">Loading…</p>}
        </>
      )}
    </main>
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

function ThreadBody({ page, lens }: { page: ThreadPage; lens: string }) {
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
  const postOf = (o: number) => page.posts.find((p) => p.ordinal === o);
  const authorOf = (o: number) => postOf(o)?.author.handle;
  // On a phone the composer is a sheet the reply bar opens (Shell), and
  // the inline one is not drawn.
  const sheetOpen = composerFor(thread.id).open.value;
  // A posted reply is shown where it landed: its permalink, which asks for
  // the page holding it at the log length the post made, and scrolls to it.
  // Nothing moves a reader who has left the thread while it was posting.
  const { route } = useLocation();
  const posted = (res: CreatedPost) => {
    if (location.pathname.startsWith(`/t/${encodeURIComponent(thread.id)}/`)) route(permalink(thread, res.ordinal));
  };

  // One handler for every link to another post in this list: a reply-to
  // indicator, or a quote's source link in a body (internal/render/quote.go).
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
      <section class="posts" onClick={follow}>
        {page.posts.map((p) => (
          <Fragment key={p.ordinal}>
            {p.ordinal === divider && <UnreadDivider unread={page.thread.post_count - lastRead} lastRead={lastRead} />}
            {p.deleted && !p.html ? (
              <Tombstone ordinal={p.ordinal} unread={lastRead > 0 && p.ordinal > lastRead} />
            ) : (
              <Post post={p} thread={thread} unread={lastRead > 0 && p.ordinal > lastRead} actions={actions} />
            )}
          </Fragment>
        ))}
      </section>
      {pager}
      {canReply &&
        (isPhone.value ? (
          sheetOpen && <ComposerSheet thread={thread.id} authorOf={authorOf} postOf={postOf} onPosted={posted} />
        ) : (
          <Composer thread={thread.id} authorOf={authorOf} onPosted={posted} />
        ))}
      {popover !== null && <PostPopover thread={thread} ordinal={popover} onClose={() => setPopover(null)} />}
    </>
  );
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
