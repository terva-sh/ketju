import { useEffect } from 'preact/hooks';
import { useLocation, useRoute } from 'preact-iso';
import { apiGet, peekGet } from '../api/client';
import type { ThreadList } from '../api/dto';
import { isProblem } from '../api/problem';
import { useResource } from '../api/resource';
import { ErrorPanel } from '../components/ErrorPanel';
import { MarkAllRead } from '../components/MarkAllRead';
import { ThreadRow } from '../components/ThreadRow';
import { rememberList, useAfterMark } from '../state/lists';
import { useScrollMemory } from '../state/scroll';
import { useShellContext } from '../state/shell';
import { viewer } from '../state/viewer';
import { NotFound } from './Placeholder';
import './threadlist.css';

// threadListUrl is the API address for one page of a forum's threads
// (docs/07): the cursor and sort are passed through as the address has them.
export function threadListUrl(forum: string, cursor?: string, sort?: string): string {
  const q = new URLSearchParams();
  if (cursor) q.set('cursor', cursor);
  if (sort) q.set('sort', sort);
  const s = q.toString();
  return `/api/v1/forums/${encodeURIComponent(forum)}/threads${s ? '?' + s : ''}`;
}

// pageHref is this screen's own address for a page and sort.
function pageHref(forum: string, cursor?: string, sort?: string): string {
  const q = new URLSearchParams();
  if (sort) q.set('sort', sort);
  if (cursor) q.set('cursor', cursor);
  const s = q.toString();
  return `/f/${encodeURIComponent(forum)}${s ? '?' + s : ''}`;
}

// ThreadListScreen is a forum's threads (docs/06 "Thread list views"): pins
// first on the first page, then by latest activity, or by creation with
// ?sort=created, in the server's order; the client does not re-sort. Paging
// is the server's cursor, carried in the address, so a page can be linked,
// reloaded and returned to.
//
// A page's answer is held (useResource's peek), so going back to it renders
// at once and useScrollMemory can put the reader where they were.
//
// A member is offered Mark all read for this forum (PUT /forums/{slug}/read,
// TKT-01M3V6Y4JP): their place in each of its threads moves to the last
// post, and every other forum is left as it was (docs/06 "Read tracking").
export function ThreadListScreen() {
  const { params } = useRoute();
  const { query } = useLocation();
  const forum = params.forum;
  const cursor = query.cursor || undefined;
  const sort = query.sort || undefined;
  const key = threadListUrl(forum, cursor, sort);
  const res = useResource(key, (signal) => apiGet<ThreadList>(key, { signal }), () => peekGet<ThreadList>(key));
  const list = res.data;
  // A mark all read changes the rows' counts (state/lists.ts).
  useAfterMark(res.retry);
  const name = list?.forum?.name ?? '';
  const signedIn = !!viewer.value.user;

  useShellContext(
    list || res.error ? { title: name || 'Threads', back: { href: '/', label: 'Forums' }, actions: [] } : null,
    [name, !!list, !!res.error],
  );
  useScrollMemory(key, !!list);
  // A thread marked unread from here comes back to this page (state/lists.ts).
  useEffect(() => rememberList(forum, pageHref(forum, cursor, sort)), [forum, cursor, sort]);

  if (isProblem(res.error, 'not-found')) return <NotFound />;

  return (
    <main class="thread-list" data-screen="thread-list">
      <header class="thread-list-head">
        <div>
          <a class="thread-list-up" href="/">
            Forums
          </a>
          <h1>{name || ' '}</h1>
        </div>
        {signedIn && list && (
          <div class="list-actions">
            <MarkAllRead url={`/api/v1/forums/${encodeURIComponent(forum)}/read`} />
            <a class="new-thread" href={`/f/${encodeURIComponent(forum)}/new`}>
              New thread
            </a>
          </div>
        )}
      </header>
      <nav class="thread-list-sort" aria-label="Sort threads">
        <a href={pageHref(forum)} aria-current={!sort ? 'page' : undefined}>
          Latest activity
        </a>
        <a href={pageHref(forum, undefined, 'created')} aria-current={sort === 'created' ? 'page' : undefined}>
          Newest threads
        </a>
      </nav>
      {res.error && !list && <ErrorPanel error={res.error} what="This forum's threads" onRetry={res.retry} />}
      {list && list.threads.length === 0 && (
        <p class="thread-list-empty" data-empty-list>
          {cursor ? 'No more threads.' : 'No threads here yet.'}
        </p>
      )}
      {list && list.threads.length > 0 && (
        <ul class="thread-rows">
          {list.threads.map((t) => (
            <ThreadRow key={t.id} thread={t} signedIn={signedIn} />
          ))}
        </ul>
      )}
      {list && (cursor || list.next_cursor) && (
        <nav class="thread-list-pages" aria-label="Pages">
          {cursor && <a href={pageHref(forum, undefined, sort)}>Newest</a>}
          {list.next_cursor && (
            <a href={pageHref(forum, list.next_cursor, sort)} rel="next" data-older>
              Older threads
            </a>
          )}
        </nav>
      )}
    </main>
  );
}
