import { useLocation } from 'preact-iso';

// A thread's view is its page and lens, and both live in the query string and
// nowhere else (docs/07 "URL scheme"): a link somebody shares is the exact
// view they were looking at, and the HTML client can serve the same URL. No
// signal or component state holds either, so there is nothing to fall out of
// step with the address bar.
export interface ThreadView {
  // page is 1 when the URL names none. The server chooses the landing page
  // for a URL with no page (TKT-01M29CEE6V); this is only what to ask for.
  page: number;
  hasPage: boolean;
  // lens is the raw lens parameter, empty for none.
  lens: string;
  setPage(page: number): void;
  setLens(lens: string): void;
}

export function useThreadView(): ThreadView {
  const { path, query, route } = useLocation();
  const page = Number.parseInt(query.page ?? '', 10);
  const go = (key: string, value: string) => {
    const q = new URLSearchParams(location.search);
    if (value === '') q.delete(key);
    else q.set(key, value);
    const s = q.toString();
    route(s ? `${path}?${s}` : path);
  };
  return {
    page: page > 0 ? page : 1,
    hasPage: page > 0,
    lens: query.lens ?? '',
    setPage: (p) => go('page', String(p)),
    setLens: (l) => go('lens', l),
  };
}

// canonicalThreadUrl is the URL a thread route should be at, given the
// thread's current slug, or null when it is already there. A slug is
// advisory (docs/07): a stale one is replaced, and everything else the URL
// says — the ordinal, /history, the page, the lens, any other parameter — is
// kept exactly as written.
export function canonicalThreadUrl(
  loc: { pathname: string; search: string; hash: string },
  thread: { id: string; slug: string },
): string | null {
  const parts = loc.pathname.split('/');
  // ['', 't', id, slug, ...rest]
  if (parts[1] !== 't' || parts[2] !== thread.id || parts.length < 4) return null;
  if (decodeURIComponent(parts[3]) === thread.slug) return null;
  parts[3] = encodeURIComponent(thread.slug);
  return parts.join('/') + loc.search + loc.hash;
}
