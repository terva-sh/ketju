import { signal } from '@preact/signals';
import { apiGet, forgetGets, forgetHeld } from '../api/client';
import type { ThreadHeader, ThreadPage } from '../api/dto';
import { revokesAccess } from '../api/problem';

// The per-thread cache (docs/07 "State"), keyed on log length.
//
// A thread's pages are valid exactly as long as its log has not grown, which
// is the key the server's own response cache uses. So an entry holds one log
// length, and a page answered at a longer one replaces the whole entry rather
// than joining it: pages from two lengths side by side would show a thread
// that never existed, one page before an edit and the next after it. The
// held GET bodies of the thread's other pages go with it, so none of them is
// shown while it is asked for again.
//
// A page answered at a shorter length is older than the entry and is not
// stored. A served length never goes down: the log is append-only, and the
// only truncation (docs/03 "Durability and recovery") cuts a torn frame that
// was never acknowledged, so never served. A shorter answer is a request
// that finished late.

export interface ThreadEntry {
  logLength: number;
  header: ThreadHeader;
  // Keyed by pageKey: the page the response names, not the request, since a
  // request without a page lands wherever the server chooses.
  pages: ReadonlyMap<string, ThreadPage>;
}

// threadCache is replaced, never mutated, so a component that reads it
// re-renders when it changes.
export const threadCache = signal<ReadonlyMap<string, ThreadEntry>>(new Map());

export interface ThreadQuery {
  page?: number;
  lens?: string;
  ordinal?: string;
}

export function threadUrl(id: string, q: ThreadQuery): string {
  const p = new URLSearchParams();
  if (q.page !== undefined) p.set('page', String(q.page));
  if (q.lens) p.set('lens', q.lens);
  // A permalink, or a post's history, asks for the page holding that post.
  if (q.ordinal) p.set('ordinal', q.ordinal);
  const s = p.toString();
  return `/api/v1/threads/${encodeURIComponent(id)}${s ? '?' + s : ''}`;
}

export function pageKey(page: Pick<ThreadPage, 'page' | 'lens'>): string {
  return page.lens ? `${page.page.number} ${page.lens.kind}:${page.lens.value}` : String(page.page.number);
}

// loadThreadPage fetches one full page and stores it. A thread that is no
// longer there, or no longer this viewer's to see (signed out, or access
// withdrawn), leaves the cache, with every held page of it, so what they
// could once read is not shown.
//
// An answer to a request that was aborted, because the screen moved on, is
// returned but changes nothing, success or refusal: whatever superseded it
// may already be newer.
//
// It takes no after_log_length. That answer holds only what is new on the
// page, and storing it here would replace a page with a fragment; applying a
// fragment to the page it extends is the live-update client's
// (TKT-01M29CTYA0).
export async function loadThreadPage(id: string, q: ThreadQuery, signal?: AbortSignal): Promise<ThreadPage> {
  const url = threadUrl(id, q);
  const n = ++loads;
  let page: ThreadPage;
  try {
    page = await apiGet<ThreadPage>(url, { signal });
  } catch (err) {
    // A superseded request's answer is older than whatever replaced it, a
    // refusal included: it changes nothing. Nor does a refusal older than a
    // page already stored from a request that started after it.
    if (revokesAccess(err) && !signal?.aborted && n > (stored.get(id) ?? 0)) {
      refused.set(id, n);
      forgetThread(id);
    }
    throw err;
  }
  if (signal?.aborted) return page;
  // A page whose request started before a refusal that has been applied is
  // from before the refusal, however late it lands, and is not stored: with
  // the entry gone there is no length left to compare it against. Nor is a
  // page older than the entry. Neither is kept under its URL either, or it
  // would be shown at once on a return there; but only it is dropped, not a
  // newer answer another request has held there since.
  if (n < (refused.get(id) ?? 0) || !storeThreadPage(id, page, url)) {
    forgetHeld(url, page);
    return page;
  }
  stored.set(id, Math.max(stored.get(id) ?? 0, n));
  return page;
}

// storeThreadPage files page under id, fetched from url, and reports whether
// it did.
export function storeThreadPage(id: string, page: ThreadPage, url?: string): boolean {
  const prev = threadCache.value.get(id);
  const logLength = page.thread.log_length;
  if (prev && logLength < prev.logLength) return false;
  const same = prev?.logLength === logLength;
  const pages = new Map(same ? prev.pages : undefined);
  pages.set(pageKey(page), page);
  const next = new Map(threadCache.value);
  next.set(id, { logLength, header: page.thread, pages });
  threadCache.value = next;
  if (prev && !same) forgetGets((u) => u !== url && isThreadUrl(u, id));
  return true;
}

export function cachedThread(id: string): ThreadEntry | undefined {
  return threadCache.value.get(id);
}

// forgetLandings drops the held bodies of id's landing requests: those that
// name neither a page nor a post, so the server chooses the page from the
// viewer's read position (docs/06 "Landing on return"). Once that position
// has moved, a held landing page is the wrong page, and showing it while it
// revalidates would place the reader there. A request naming its page is
// the same page whatever the position, and keeps its held body.
export function forgetLandings(id: string) {
  const base = threadUrl(id, {});
  forgetGets((u) => {
    if (u !== base && !u.startsWith(base + '?')) return false;
    const q = new URLSearchParams(u.slice(base.length + 1));
    return !q.has('page') && !q.has('ordinal');
  });
}

// isThreadUrl is a request for one of id's pages, or anything under it.
function isThreadUrl(url: string, id: string): boolean {
  const base = threadUrl(id, {});
  return url === base || url.startsWith(base + '?') || url.startsWith(base + '/');
}

// Loads are numbered in the order they start. Per thread, refused is the
// newest load whose refusal was applied and stored the newest whose page was,
// so each outcome is applied only if no newer one of the other kind already
// was: overlapping loads can finish in any order, and the later-started one's
// answer is the truer. Pages at one length still accumulate whatever their
// order; the length rule above orders those.
let loads = 0;
const refused = new Map<string, number>();
const stored = new Map<string, number>();

function forgetThread(id: string) {
  forgetGets((u) => isThreadUrl(u, id));
  if (!threadCache.value.has(id)) return;
  const next = new Map(threadCache.value);
  next.delete(id);
  threadCache.value = next;
}

// resetThreadCache empties the cache, for tests.
export function resetThreadCache() {
  threadCache.value = new Map();
  refused.clear();
  stored.clear();
}
