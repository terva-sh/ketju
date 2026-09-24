import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { peekGet, resetApiCache } from '../api/client';
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

  // Review 411: a request that finishes late must not roll the cache back.
  it('does not let an answer at a shorter length replace a longer one', async () => {
    serve(page(1, 120), page(2, 100));
    await loadThreadPage(id, { page: 1 });
    await loadThreadPage(id, { page: 2 });
    expect(cachedThread(id)!.logLength).toBe(120);
    expect([...cachedThread(id)!.pages.keys()]).toEqual(['1']);
  });

  it('does not store the answer to an aborted request', async () => {
    // The same length, so only the abort keeps it out.
    serve(page(1, 100), page(2, 100));
    await loadThreadPage(id, { page: 1 });
    const ctl = new AbortController();
    const late = loadThreadPage(id, { page: 2 }, ctl.signal);
    ctl.abort();
    await late.catch(() => {});
    expect([...cachedThread(id)!.pages.keys()]).toEqual(['1']);
  });

  // Review 412: a superseded request's refusal is as old as its success.
  it.each([404, 410, 403, 401])('ignores a %d that answers an aborted request', async (status) => {
    serve(page(1, 100), status);
    await loadThreadPage(id, { page: 1 });
    const ctl = new AbortController();
    const late = loadThreadPage(id, { page: 2 }, ctl.signal);
    ctl.abort();
    await late.catch(() => {});
    expect(cachedThread(id)!.logLength).toBe(100);
  });

  // Review 412: a page refused for its shorter length is not held under its
  // URL either, where it would show at once on a return.
  it('does not hold a page the cache refused as older', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(new Response(JSON.stringify(page(1, 120)), { status: 200, headers: { ETag: '"a"' } }))
        .mockResolvedValueOnce(new Response(JSON.stringify(page(2, 100)), { status: 200, headers: { ETag: '"b"' } })),
    );
    await loadThreadPage(id, { page: 1 });
    await loadThreadPage(id, { page: 2 });
    expect(peekGet(threadUrl(id, { page: 2 }))).toBeUndefined();
    expect(peekGet(threadUrl(id, { page: 1 }))).toBeDefined();
  });

  // Review 414: a refusal that lands while an older request is in flight
  // wins. The older answer, however late, is from before the refusal.
  describe('a refusal overtaking an older request', () => {
    function manual() {
      const pending: ((r: Response) => void)[] = [];
      vi.stubGlobal('fetch', () => new Promise<Response>((res) => pending.push(res)));
      return pending;
    }
    const ok = (p: ThreadPage, tag: string) =>
      new Response(JSON.stringify(p), { status: 200, headers: { ETag: tag } });

    it.each([403, 401, 404, 410])('keeps a thread refused with %d out, for the same page', async (status) => {
      const answer = manual();
      const older = loadThreadPage(id, { page: 1 });
      const newer = loadThreadPage(id, { page: 1 });
      await Promise.resolve();
      answer[1](new Response('{}', { status }));
      await newer.catch(() => {});
      answer[0](ok(page(1, 100), '"a"'));
      await older;
      expect(cachedThread(id)).toBeUndefined();
      expect(peekGet(threadUrl(id, { page: 1 }))).toBeUndefined();
    });

    it('keeps it out for another page, and its held body too', async () => {
      const answer = manual();
      const older = loadThreadPage(id, { page: 1 });
      const newer = loadThreadPage(id, { page: 2 });
      await Promise.resolve();
      answer[1](new Response('{}', { status: 403 }));
      await newer.catch(() => {});
      answer[0](ok(page(1, 100), '"a"'));
      await older;
      expect(cachedThread(id)).toBeUndefined();
      expect(peekGet(threadUrl(id, { page: 1 }))).toBeUndefined();
    });

    it('stores a request started after the refusal', async () => {
      serve(403, page(1, 100));
      await loadThreadPage(id, { page: 1 }).catch(() => {});
      await loadThreadPage(id, { page: 1 });
      expect(cachedThread(id)!.logLength).toBe(100);
    });

    it("leaves other threads' loads alone", async () => {
      const answer = manual();
      const o = page(1, 5);
      const other = loadThreadPage('other', { page: 1 });
      const refused = loadThreadPage(id, { page: 1 });
      await Promise.resolve();
      answer[1](new Response('{}', { status: 403 }));
      await refused.catch(() => {});
      answer[0](ok({ ...o, thread: { ...o.thread, id: 'other' } }, '"o"'));
      await other;
      expect(cachedThread('other')!.logLength).toBe(5);
    });
  });

  // Review 411: the held GET bodies of the thread's other pages are as stale
  // as its cached pages once the length moves, and go with them.
  it('forgets the held bodies of other pages when the length moves', async () => {
    const tagged = (p: ThreadPage, tag: string) =>
      new Response(JSON.stringify(p), { status: 200, headers: { ETag: tag } });
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(tagged(page(1, 100), '"p1"'))
        .mockResolvedValueOnce(tagged(page(2, 100), '"p2"'))
        .mockResolvedValueOnce(tagged(page(1, 120), '"p1b"')),
    );
    await loadThreadPage(id, { page: 1 });
    await loadThreadPage(id, { page: 2 });
    expect(peekGet(threadUrl(id, { page: 2 }))).toBeDefined();
    await loadThreadPage(id, { page: 1 }); // the length moves to 120
    expect(peekGet(threadUrl(id, { page: 2 }))).toBeUndefined();
    expect((peekGet(threadUrl(id, { page: 1 })) as ThreadPage).thread.log_length).toBe(120);
  });

  it("keeps other threads' held bodies when one thread's length moves", async () => {
    const tagged = (p: ThreadPage, tag: string) =>
      new Response(JSON.stringify(p), { status: 200, headers: { ETag: tag } });
    const o = page(1, 5);
    const otherPage = { ...o, thread: { ...o.thread, id: `${id}0` } };
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(tagged(otherPage, '"o"'))
        .mockResolvedValueOnce(tagged(page(1, 100), '"a"'))
        .mockResolvedValueOnce(tagged(page(1, 120), '"b"')),
    );
    // An id that shares a prefix with this one is another thread.
    await loadThreadPage(`${id}0`, { page: 1 });
    await loadThreadPage(id, { page: 1 });
    await loadThreadPage(id, { page: 1 });
    expect(peekGet(threadUrl(`${id}0`, { page: 1 }))).toBeDefined();
  });

  it.each([404, 410, 403, 401])('forgets the held bodies of a thread that answers %d', async (status) => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(new Response(JSON.stringify(page(1, 100)), { status: 200, headers: { ETag: '"a"' } }))
        .mockResolvedValueOnce(new Response(JSON.stringify(page(2, 100)), { status: 200, headers: { ETag: '"b"' } }))
        .mockResolvedValueOnce(new Response('{}', { status })),
    );
    await loadThreadPage(id, { page: 1 });
    await loadThreadPage(id, { page: 2 });
    await loadThreadPage(id, { page: 1 }).catch(() => {});
    expect(peekGet(threadUrl(id, { page: 1 }))).toBeUndefined();
    expect(peekGet(threadUrl(id, { page: 2 }))).toBeUndefined();
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
