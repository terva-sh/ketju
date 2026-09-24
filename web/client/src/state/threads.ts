import { signal } from '@preact/signals';
import { apiGet } from '../api/client';
import type { ThreadHeader, ThreadPage } from '../api/dto';
import { isProblem } from '../api/problem';

// The per-thread cache (docs/07 "State"), keyed on log length.
//
// A thread's pages are valid exactly as long as its log has not grown, which
// is the key the server's own response cache uses. So an entry holds one log
// length, and a page answered at any other length replaces the whole entry
// rather than joining it: pages from two lengths side by side would show a
// thread that never existed, one page before an edit and the next after it.

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

// The answers that mean the viewer may no longer see a thread.
const gone = ['not-found', 'gone', 'forbidden', 'unauthenticated'] as const;

// loadThreadPage fetches one full page and stores it. A thread that is no
// longer there, or no longer this viewer's to see (signed out, or access
// withdrawn), leaves the cache, so what they could once read is not shown.
//
// It takes no after_log_length. That answer holds only what is new on the
// page, and storing it here would replace a page with a fragment; applying a
// fragment to the page it extends is the live-update client's
// (TKT-01M29CTYA0).
export async function loadThreadPage(id: string, q: ThreadQuery, signal?: AbortSignal): Promise<ThreadPage> {
  let page: ThreadPage;
  try {
    page = await apiGet<ThreadPage>(threadUrl(id, q), { signal });
  } catch (err) {
    if (gone.some((t) => isProblem(err, t))) dropThread(id);
    throw err;
  }
  storeThreadPage(id, page);
  return page;
}

export function storeThreadPage(id: string, page: ThreadPage) {
  const prev = threadCache.value.get(id);
  const logLength = page.thread.log_length;
  const pages = new Map(prev?.logLength === logLength ? prev.pages : undefined);
  pages.set(pageKey(page), page);
  const next = new Map(threadCache.value);
  next.set(id, { logLength, header: page.thread, pages });
  threadCache.value = next;
}

export function cachedThread(id: string): ThreadEntry | undefined {
  return threadCache.value.get(id);
}

function dropThread(id: string) {
  if (!threadCache.value.has(id)) return;
  const next = new Map(threadCache.value);
  next.delete(id);
  threadCache.value = next;
}

// resetThreadCache empties the cache, for tests.
export function resetThreadCache() {
  threadCache.value = new Map();
}
