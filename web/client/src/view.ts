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
  const page = parsePage(query.page);
  // Only the query changes: the path and any #post-N anchor stay.
  const go = (key: string, value: string) => {
    const q = new URLSearchParams(location.search);
    if (value === '') q.delete(key);
    else q.set(key, value);
    const s = q.toString();
    route((s ? `${path}?${s}` : path) + location.hash);
  };
  return {
    page: page ?? 1,
    hasPage: page !== null,
    lens: query.lens ?? '',
    setPage: (p) => go('page', String(p)),
    setLens: (l) => go('lens', l),
  };
}

// parsePage accepts a page parameter only when the whole of it is a positive
// integer. Anything else — 2junk, 0, -1, 1e3, a number too large to be
// exact — is treated as no page at all, so the view a malformed link shows is
// the same for everyone who opens it rather than a guess at a prefix.
export function parsePage(raw: string | undefined): number | null {
  if (raw === undefined || !/^[1-9][0-9]*$/.test(raw)) return null;
  const n = Number(raw);
  return Number.isSafeInteger(n) ? n : null;
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
