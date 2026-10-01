import { useEffect } from 'preact/hooks';
import { useLocation } from 'preact-iso';
import { apiGet, peekGet } from '../api/client';
import type { ThreadList } from '../api/dto';
import { useResource } from '../api/resource';
import { ErrorPanel } from '../components/ErrorPanel';
import { MarkAllRead } from '../components/MarkAllRead';
import { ThreadRow } from '../components/ThreadRow';
import { meHref } from '../shell/nav';
import { type CrossListName, rememberList, useAfterMark } from '../state/lists';
import { useScrollMemory } from '../state/scroll';
import { useShellContext } from '../state/shell';
import { viewer } from '../state/viewer';
import '../components/markread.css';
import './threadlist.css';

// What each list says about itself: its heading, what it holds when empty,
// and what a signed-out reader is asked to sign in for. The lists' rules are
// docs/06 "Thread list views" (decided by the run, 2026-10-01); the words
// were settled in grooming (TKT-01M3V6Y4JP).
const lists: Record<CrossListName, { title: string; empty: string; signIn: string }> = {
  unread: {
    title: 'Unread',
    empty: 'Nothing unread. Threads with posts you have not read appear here.',
    signIn: 'to see the threads with posts you have not read.',
  },
  new: {
    title: 'New',
    empty: 'No new threads. Threads you have not opened appear here.',
    signIn: 'to see the threads you have not opened.',
  },
  participated: {
    title: 'Participated',
    empty: 'You have not posted yet. Threads you post in appear here.',
    signIn: 'to see the threads you have posted in.',
  },
  watching: {
    title: 'Watching',
    empty: "You are not watching any threads. Choose Watch in a thread's menu to follow it here.",
    signIn: 'to see the threads you are watching.',
  },
};

// crossListUrl is the API address for one page of a cross-forum list
// (docs/07): the cursor is passed through as the address has it.
export function crossListUrl(list: CrossListName, cursor?: string): string {
  const q = cursor ? '?' + new URLSearchParams({ cursor }).toString() : '';
  return `/api/v1/${list}${q}`;
}

// pageHref is the screen's own address for a page.
function pageHref(list: CrossListName, cursor?: string): string {
  return `/${list}${cursor ? '?' + new URLSearchParams({ cursor }).toString() : ''}`;
}

// CrossListScreen is one of the four "what's new" lists across every forum
// the viewer may read (docs/06 "Thread list views"): /unread, /new,
// /participated and /watching. It is the forum's thread list without the
// forum: the same rows, each naming its forum, the same paging by the
// server's cursor carried in the address, and the same scroll memory
// (ThreadListScreen). The lists are a member's own, so a signed-out reader
// is asked to sign in and nothing is fetched.
//
// /unread's header offers Mark all read for the whole site (PUT /me/read).
// The other three hold threads by another rule, and offer none.
export function CrossListScreen({ list }: { list: CrossListName }) {
  const { query, url } = useLocation();
  const cursor = query.cursor || undefined;
  const signedIn = !!viewer.value.user;
  const def = lists[list];
  const key = signedIn ? crossListUrl(list, cursor) : null;
  const res = useResource(
    key,
    (signal) => apiGet<ThreadList>(key!, { signal }),
    () => (key ? peekGet<ThreadList>(key) : undefined),
  );
  const data = res.data;
  // A mark all read, from here or from the screen the reader just left,
  // changes what every list holds (state/lists.ts).
  useAfterMark(res.retry);

  useShellContext({ title: def.title, actions: [] }, [def.title]);
  useScrollMemory(key ?? pageHref(list, cursor), !!data || !signedIn);
  // A thread marked unread from here comes back to this page (state/lists.ts).
  useEffect(() => rememberList(null, pageHref(list, cursor)), [list, cursor]);

  return (
    <main class="thread-list cross-list" data-screen={list}>
      <header class="thread-list-head">
        <h1>{def.title}</h1>
        {list === 'unread' && data && data.threads.length > 0 && (
          <div class="list-actions">
            <MarkAllRead url="/api/v1/me/read" />
          </div>
        )}
      </header>
      {!signedIn && (
        <p class="thread-list-empty" data-sign-in>
          <a href={meHref(url)}>Sign in</a> {def.signIn}
        </p>
      )}
      {res.error && !data && <ErrorPanel error={res.error} what={`The ${def.title.toLowerCase()} list`} onRetry={res.retry} />}
      {data && data.threads.length === 0 && (
        <p class="thread-list-empty" data-empty-list>
          {cursor ? 'No more threads.' : def.empty}
        </p>
      )}
      {data && data.threads.length > 0 && (
        <ul class="thread-rows">
          {data.threads.map((t) => (
            <ThreadRow key={t.id} thread={t} signedIn={signedIn} />
          ))}
        </ul>
      )}
      {data && (cursor || data.next_cursor) && (
        <nav class="thread-list-pages" aria-label="Pages">
          {cursor && <a href={pageHref(list)}>Newest</a>}
          {data.next_cursor && (
            <a href={pageHref(list, data.next_cursor)} rel="next" data-older>
              Older threads
            </a>
          )}
        </nav>
      )}
    </main>
  );
}

// crossListScreen is the route component for one list.
export const crossListScreen = (list: CrossListName) =>
  function Screen() {
    return <CrossListScreen list={list} />;
  };
