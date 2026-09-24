import { useEffect } from 'preact/hooks';
import { useLocation, useRoute } from 'preact-iso';
import { peekGet } from '../api/client';
import type { ThreadPage } from '../api/dto';
import { isProblem } from '../api/problem';
import { useResource } from '../api/resource';
import { ErrorPanel } from '../components/ErrorPanel';
import { cachedThread, loadThreadPage, threadUrl, type ThreadQuery } from '../state/threads';
import { canonicalThreadUrl, useThreadView } from '../view';
import { NotFound } from './Placeholder';

// ThreadScreen is the thread route until the thread page lands
// (TKT-01M29CEE6V). What it does now is the router's part: ask for the page
// the URL names, and when the thread's slug has changed, replace the URL with
// the canonical one.
//
// useResource keys the answer on the request, so moving to another thread,
// or another page of this one, never renders the last answer's title or 404,
// not even for one render. The title shown while a page loads, or after it
// fails, is the thread's own from the cache, which only ever holds this
// thread's. A failure that is not a 404 says so, with a retry; it does not
// leave an earlier 404 standing, which would call a thread that exists
// missing.
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

  if (isProblem(res.error, 'not-found')) return <NotFound />;
  return (
    <main class="scaffold" data-screen={history ? 'post-history' : 'thread'}>
      <h1>{head?.title ?? 'Thread'}</h1>
      {res.error && <ErrorPanel error={res.error} what="This thread" onRetry={res.retry} />}
      <p>
        {history ? `History of post ${params.ordinal}. ` : ''}
        Page {view.page}
        {view.lens ? `, lens ${view.lens}` : ''}. This screen is not built yet.
      </p>
    </main>
  );
}
