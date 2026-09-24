import { useEffect, useState } from 'preact/hooks';
import { useLocation, useRoute } from 'preact-iso';
import { canonicalThreadUrl, useThreadView } from '../view';
import { NotFound } from './Placeholder';

interface ThreadHead {
  thread: { id: string; slug: string; title: string };
}

// ThreadScreen is the thread route until the thread page lands
// (TKT-01M29CEE6V). What it does now is the router's part: ask for the page
// the URL names, and when the thread's slug has changed, replace the URL with
// the canonical one. The bare fetch is temporary; the fetch layer
// (TKT-01M29CEE5H) takes it over.
export function ThreadScreen() {
  const { params, path } = useRoute();
  const { route, url } = useLocation();
  const view = useThreadView();
  // What the last answer said, and which thread it was about. It is read
  // only when that is the thread on screen, so moving to another thread never
  // renders the last one's title or 404, not even for the one render before
  // its own request is made.
  const [answer, setAnswer] = useState<{ id: string; head?: ThreadHead; missing?: boolean } | null>(null);
  const current = answer?.id === params.id ? answer : null;
  const history = path.endsWith('/history');

  useEffect(() => {
    const q = new URLSearchParams();
    if (view.hasPage) q.set('page', String(view.page));
    if (view.lens) q.set('lens', view.lens);
    if (params.ordinal && !history) q.set('ordinal', params.ordinal);
    const s = q.toString();
    const id = params.id;
    let live = true;
    fetch(`/api/v1/threads/${encodeURIComponent(params.id)}${s ? '?' + s : ''}`, {
      headers: { Accept: 'application/json' },
    })
      .then(async (r) => {
        if (!live) return;
        if (r.status === 404) return setAnswer({ id, missing: true });
        if (!r.ok) return;
        const body = (await r.json()) as ThreadHead;
        if (!live) return;
        setAnswer({ id, head: body });
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [params.id, params.ordinal, view.page, view.hasPage, view.lens, history]);

  // The slug is checked whenever the address or the answer changes, not only
  // when a request comes back: moving to an old slug of the thread already on
  // screen asks for nothing new, and must still land on the canonical URL.
  useEffect(() => {
    if (!current?.head) return;
    const to = canonicalThreadUrl(location, current.head.thread);
    if (to) route(to, true);
  }, [current, url]);

  if (current?.missing) return <NotFound />;
  return (
    <main class="scaffold" data-screen={history ? 'post-history' : 'thread'}>
      <h1>{current?.head?.thread.title ?? 'Thread'}</h1>
      <p>
        {history ? `History of post ${params.ordinal}. ` : ''}
        Page {view.page}
        {view.lens ? `, lens ${view.lens}` : ''}. This screen is not built yet.
      </p>
    </main>
  );
}
