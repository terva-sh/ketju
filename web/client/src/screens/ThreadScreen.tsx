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
  const { route } = useLocation();
  const view = useThreadView();
  const [head, setHead] = useState<ThreadHead | null>(null);
  const [missing, setMissing] = useState(false);
  const history = path.endsWith('/history');

  useEffect(() => {
    const q = new URLSearchParams();
    if (view.hasPage) q.set('page', String(view.page));
    if (view.lens) q.set('lens', view.lens);
    if (params.ordinal && !history) q.set('ordinal', params.ordinal);
    const s = q.toString();
    let live = true;
    fetch(`/api/v1/threads/${encodeURIComponent(params.id)}${s ? '?' + s : ''}`, {
      headers: { Accept: 'application/json' },
    })
      .then(async (r) => {
        if (!live) return;
        if (r.status === 404) return setMissing(true);
        if (!r.ok) return;
        const body = (await r.json()) as ThreadHead;
        if (!live) return;
        setHead(body);
        const to = canonicalThreadUrl(location, body.thread);
        if (to) route(to, true);
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [params.id, params.ordinal, view.page, view.hasPage, view.lens, history]);

  if (missing) return <NotFound />;
  return (
    <main class="scaffold" data-screen={history ? 'post-history' : 'thread'}>
      <h1>{head?.thread.title ?? 'Thread'}</h1>
      <p>
        {history ? `History of post ${params.ordinal}. ` : ''}
        Page {view.page}
        {view.lens ? `, lens ${view.lens}` : ''}. This screen is not built yet.
      </p>
    </main>
  );
}
