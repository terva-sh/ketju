import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { resetApiCache } from '../api/client';
import type { ThreadPage } from '../api/dto';
import { isProblem } from '../api/problem';
import { threadPage } from '../testing';
import { cachedThread, loadThreadPage, pageKey, resetThreadCache, threadCache, threadUrl } from './threads';

const id = '3yymw47g00001';

function page(number: number, logLength: number, lens: ThreadPage['lens'] = null): ThreadPage {
  const p = threadPage({ id, slug: 's', title: `at ${logLength}` });
  return { ...p, thread: { ...p.thread, log_length: logLength }, page: { ...p.page, number }, lens };
}

// serve answers each request with the next page, or with a status.
function serve(...answers: (ThreadPage | number)[]) {
  const urls: string[] = [];
  vi.stubGlobal('fetch', async (url: string) => {
    urls.push(url);
    const a = answers.shift();
    if (typeof a === 'number') return new Response('{}', { status: a });
    return new Response(JSON.stringify(a), { status: 200 });
  });
  return urls;
}

beforeEach(() => {
  resetApiCache();
  resetThreadCache();
});
afterEach(() => vi.unstubAllGlobals());

// AC2: keyed on log length; a new length replaces rather than merges.
describe('the thread cache', () => {
  it('replaces every page when the log length moves', async () => {
    serve(page(1, 100), page(2, 120));
    await loadThreadPage(id, { page: 1 });
    expect([...cachedThread(id)!.pages.keys()]).toEqual(['1']);
    await loadThreadPage(id, { page: 2 });
    const e = cachedThread(id)!;
    expect(e.logLength).toBe(120);
    expect([...e.pages.keys()]).toEqual(['2']);
    expect(e.header.title).toBe('at 120');
  });

  it('keeps pages side by side while the length holds', async () => {
    serve(page(1, 100), page(2, 100), page(1, 100, { kind: 'author', value: 'alice', shown: 1, hidden: 2 }));
    await loadThreadPage(id, { page: 1 });
    await loadThreadPage(id, { page: 2 });
    await loadThreadPage(id, { page: 1, lens: 'author:alice' });
    expect([...cachedThread(id)!.pages.keys()].sort()).toEqual(['1', '1 author:alice', '2']);
  });

  it('replaces even when the length goes down, as after a reindex', async () => {
    serve(page(1, 120), page(2, 100));
    await loadThreadPage(id, { page: 1 });
    await loadThreadPage(id, { page: 2 });
    expect(cachedThread(id)!.logLength).toBe(100);
    expect([...cachedThread(id)!.pages.keys()]).toEqual(['2']);
  });

  it('files a page under the page the server answered with, not the one asked for', async () => {
    serve(page(3, 100));
    await loadThreadPage(id, {}); // the server picks the landing page
    expect([...cachedThread(id)!.pages.keys()]).toEqual(['3']);
  });

  it('keeps each thread apart', async () => {
    const other = { ...page(1, 5), thread: { ...page(1, 5).thread, id: 'other' } };
    serve(page(1, 100), other);
    await loadThreadPage(id, { page: 1 });
    await loadThreadPage('other', { page: 1 });
    expect(cachedThread(id)!.logLength).toBe(100);
    expect(cachedThread('other')!.logLength).toBe(5);
  });

  it.each([404, 410, 403, 401])('forgets a thread that answers %d', async (status) => {
    serve(page(1, 100), status);
    await loadThreadPage(id, { page: 1 });
    const err = await loadThreadPage(id, { page: 2 }).catch((e) => e);
    expect(err).toBeDefined();
    expect(cachedThread(id)).toBeUndefined();
  });

  it.each([500, 503])('keeps a thread through a %d', async (status) => {
    serve(page(1, 100), status);
    await loadThreadPage(id, { page: 1 });
    const err = await loadThreadPage(id, { page: 2 }).catch((e) => e);
    expect(isProblem(err, status === 500 ? 'internal' : 'unavailable')).toBe(true);
    expect(cachedThread(id)!.logLength).toBe(100);
  });

  it('replaces the map rather than changing it, so readers see the change', async () => {
    serve(page(1, 100), page(2, 100));
    await loadThreadPage(id, { page: 1 });
    const before = threadCache.value;
    const entry = before.get(id)!;
    await loadThreadPage(id, { page: 2 });
    expect(threadCache.value).not.toBe(before);
    expect([...entry.pages.keys()]).toEqual(['1']);
  });

  it('asks for the URL the view names', async () => {
    const urls = serve(page(1, 1));
    await loadThreadPage(id, { page: 2, lens: 'author:alice', ordinal: '42' });
    expect(urls).toEqual([`/api/v1/threads/${id}?page=2&lens=author%3Aalice&ordinal=42`]);
    expect(threadUrl('a/b', {})).toBe('/api/v1/threads/a%2Fb');
  });

  it('keys a lens by kind and value', () => {
    const p = page(4, 1, { kind: 'author', value: 'bob', shown: 1, hidden: 1 });
    expect(pageKey(p)).toBe('4 author:bob');
    expect(pageKey(page(4, 1))).toBe('4');
  });
});
