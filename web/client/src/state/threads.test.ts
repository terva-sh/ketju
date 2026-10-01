import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { peekGet, resetApiCache } from '../api/client';
import type { ThreadPage } from '../api/dto';
import { isProblem } from '../api/problem';
import { threadPage } from '../testing';
import {
  cachedThread,
  forgetAllThreadPages,
  loadThreadPage,
  pageKey,
  resetThreadCache,
  storeThreadHeader,
  threadCache,
  threadUrl,
} from './threads';

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
    serve(page(1, 100), page(2, 100), page(1, 100, { kind: 'author', value: 'alice', shown: 1, hidden: 2, gaps: [] }));
    await loadThreadPage(id, { page: 1 });
    await loadThreadPage(id, { page: 2 });
    await loadThreadPage(id, { page: 1, lens: 'author:alice' });
    expect([...cachedThread(id)!.pages.keys()].sort()).toEqual(['1', '1 author:alice', '2']);
  });

  // Review 411: a request that finishes late must not roll the cache back.
  // Review 420: nor is it returned. The page is asked for again, and the
  // answer at the entry's length is the one stored and returned.
  it('does not let an answer at a shorter length replace a longer one', async () => {
    const urls = serve(page(1, 120), page(2, 100), page(2, 120));
    await loadThreadPage(id, { page: 1 });
    const got = await loadThreadPage(id, { page: 2 });
    expect(got.thread.log_length).toBe(120);
    expect(urls).toEqual([threadUrl(id, { page: 1 }), threadUrl(id, { page: 2 }), threadUrl(id, { page: 2 })]);
    expect(cachedThread(id)!.logLength).toBe(120);
    expect([...cachedThread(id)!.pages.keys()].sort()).toEqual(['1', '2']);
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
        .mockResolvedValueOnce(new Response(JSON.stringify(page(2, 100)), { status: 200, headers: { ETag: '"b"' } }))
        // The second asking, which must go out unconditionally.
        .mockImplementationOnce(async (_url: string, init: RequestInit) => {
          expect(new Headers(init.headers).get('If-None-Match')).toBeNull();
          return new Response(JSON.stringify(page(2, 120)), { status: 200 });
        }),
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
      // Review 420: and its caller is told of the refusal, not given the page.
      await expect(older).rejects.toMatchObject({ name: 'ProblemError', status });
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
      await expect(older).rejects.toMatchObject({ name: 'ProblemError', status: 403 });
      expect(cachedThread(id)).toBeUndefined();
      expect(peekGet(threadUrl(id, { page: 1 }))).toBeUndefined();
    });

    // Review 415: the mirror case. A newer page landed first; the older
    // refusal, landing after it, is old news.
    it.each([403, 401, 404, 410])('does not let an older %d erase a newer page', async (status) => {
      const answer = manual();
      const older = loadThreadPage(id, { page: 1 });
      const newer = loadThreadPage(id, { page: 2 });
      await Promise.resolve();
      answer[1](ok(page(2, 100), '"b"'));
      await newer;
      answer[0](new Response('{}', { status }));
      await older.catch(() => {});
      expect([...cachedThread(id)!.pages.keys()]).toEqual(['2']);
      expect(peekGet(threadUrl(id, { page: 2 }))).toBeDefined();
    });

    it('applies a refusal newer than every stored page', async () => {
      const answer = manual();
      const older = loadThreadPage(id, { page: 1 });
      const newer = loadThreadPage(id, { page: 2 });
      await Promise.resolve();
      answer[0](ok(page(1, 100), '"a"'));
      await older;
      answer[1](new Response('{}', { status: 403 }));
      await newer.catch(() => {});
      expect(cachedThread(id)).toBeUndefined();
      expect(peekGet(threadUrl(id, { page: 1 }))).toBeUndefined();
    });

    it('keeps an older page that lands after a newer one at the same length', async () => {
      const answer = manual();
      const older = loadThreadPage(id, { page: 1 });
      const newer = loadThreadPage(id, { page: 2 });
      await Promise.resolve();
      answer[1](ok(page(2, 100), '"b"'));
      await newer;
      answer[0](ok(page(1, 100), '"a"'));
      await older;
      expect([...cachedThread(id)!.pages.keys()].sort()).toEqual(['1', '2']);
    });

    // Review 417: dropping a stale page's held body must not drop a newer
    // one another request held at the same URL.
    it('keeps the newer held body when an older page at a shorter length lands', async () => {
      const answer = manual();
      const older = loadThreadPage(id, { page: 1 });
      const newer = loadThreadPage(id, { page: 1 });
      await Promise.resolve();
      answer[1](ok(page(1, 120), '"new"'));
      await newer;
      answer[0](ok(page(1, 100), '"old"'));
      // Review 420: the older caller gets the page the cache holds instead.
      expect((await older).thread.log_length).toBe(120);
      expect((peekGet(threadUrl(id, { page: 1 })) as ThreadPage).thread.log_length).toBe(120);
    });

    // The mirror of review 417: the newer request answered at the shorter
    // length and finished first, so its body is the one held. The older one
    // lands later, longer, and is stored but not held; the shorter body must
    // not stay held under the URL of a cache that has moved on.
    it('drops the held body at its own URL when an older request moves the length', async () => {
      const answer = manual();
      const older = loadThreadPage(id, { page: 1 });
      const newer = loadThreadPage(id, { page: 1 });
      await Promise.resolve();
      answer[1](ok(page(1, 100), '"new"'));
      await newer;
      answer[0](ok(page(1, 120), '"old"'));
      expect((await older).thread.log_length).toBe(120);
      expect(cachedThread(id)!.logLength).toBe(120);
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

  // Review 420: a load resolves only with a page the cache holds. Rounds 411
  // to 417 each found one more ordering of two overlapping loads, so this
  // settles two of them by hand in every order: A starts first and B second,
  // each answers a page at log length 100 or 120 or a 403, and either lands
  // first. Each row says, by hand, what each caller is given and what the
  // cache holds afterwards.
  describe('two overlapping loads, in every order', () => {
    // An outcome: a page at that log length, or a refusal.
    type Answer = 100 | 120 | 403;
    // What a caller is given: a page at that length, or a rejection.
    type Given = { len: number } | { status: number };
    interface Row {
      a: Answer;
      b: Answer;
      first: 'A' | 'B';
      // What the second asking answers, when a refused page is asked again.
      again?: Answer;
      wantA: Given;
      wantB: Given;
      // The cache afterwards: its length and page keys, or null for none.
      cache: { len: number; pages: string[] } | null;
    }

    const ok = (len: number): Given => ({ len });
    const no = (status: number): Given => ({ status });
    const none = null;
    const at = (len: number, ...pages: string[]) => ({ len, pages });

    // A asks for page 1 and B for page 2, so a refused page has nothing
    // cached under its own key and is asked for again.
    const otherPages: Row[] = [
      { a: 100, b: 100, first: 'A', wantA: ok(100), wantB: ok(100), cache: at(100, '1', '2') },
      { a: 100, b: 100, first: 'B', wantA: ok(100), wantB: ok(100), cache: at(100, '1', '2') },
      { a: 120, b: 120, first: 'A', wantA: ok(120), wantB: ok(120), cache: at(120, '1', '2') },
      { a: 120, b: 120, first: 'B', wantA: ok(120), wantB: ok(120), cache: at(120, '1', '2') },
      { a: 100, b: 120, first: 'A', wantA: ok(100), wantB: ok(120), cache: at(120, '2') },
      { a: 100, b: 120, first: 'B', again: 120, wantA: ok(120), wantB: ok(120), cache: at(120, '1', '2') },
      { a: 120, b: 100, first: 'A', again: 120, wantA: ok(120), wantB: ok(120), cache: at(120, '1', '2') },
      { a: 120, b: 100, first: 'B', wantA: ok(120), wantB: ok(100), cache: at(120, '1') },
      // The case review 420 reported: A lands after B's refusal.
      { a: 100, b: 403, first: 'A', wantA: ok(100), wantB: no(403), cache: none },
      { a: 100, b: 403, first: 'B', wantA: no(403), wantB: no(403), cache: none },
      { a: 120, b: 403, first: 'A', wantA: ok(120), wantB: no(403), cache: none },
      { a: 120, b: 403, first: 'B', wantA: no(403), wantB: no(403), cache: none },
      { a: 403, b: 100, first: 'A', wantA: no(403), wantB: ok(100), cache: at(100, '2') },
      { a: 403, b: 100, first: 'B', wantA: no(403), wantB: ok(100), cache: at(100, '2') },
      { a: 403, b: 120, first: 'A', wantA: no(403), wantB: ok(120), cache: at(120, '2') },
      { a: 403, b: 120, first: 'B', wantA: no(403), wantB: ok(120), cache: at(120, '2') },
      { a: 403, b: 403, first: 'A', wantA: no(403), wantB: no(403), cache: none },
      { a: 403, b: 403, first: 'B', wantA: no(403), wantB: no(403), cache: none },
    ];

    // Both ask for page 1, so a page refused for its length is replaced by
    // the one cached under its key, with no second asking.
    const samePage: Row[] = [
      { a: 100, b: 100, first: 'A', wantA: ok(100), wantB: ok(100), cache: at(100, '1') },
      { a: 100, b: 100, first: 'B', wantA: ok(100), wantB: ok(100), cache: at(100, '1') },
      { a: 120, b: 120, first: 'A', wantA: ok(120), wantB: ok(120), cache: at(120, '1') },
      { a: 120, b: 120, first: 'B', wantA: ok(120), wantB: ok(120), cache: at(120, '1') },
      { a: 100, b: 120, first: 'A', wantA: ok(100), wantB: ok(120), cache: at(120, '1') },
      { a: 100, b: 120, first: 'B', wantA: ok(120), wantB: ok(120), cache: at(120, '1') },
      { a: 120, b: 100, first: 'A', wantA: ok(120), wantB: ok(120), cache: at(120, '1') },
      { a: 120, b: 100, first: 'B', wantA: ok(120), wantB: ok(100), cache: at(120, '1') },
      { a: 100, b: 403, first: 'A', wantA: ok(100), wantB: no(403), cache: none },
      { a: 100, b: 403, first: 'B', wantA: no(403), wantB: no(403), cache: none },
      { a: 120, b: 403, first: 'A', wantA: ok(120), wantB: no(403), cache: none },
      { a: 120, b: 403, first: 'B', wantA: no(403), wantB: no(403), cache: none },
      { a: 403, b: 100, first: 'A', wantA: no(403), wantB: ok(100), cache: at(100, '1') },
      { a: 403, b: 100, first: 'B', wantA: no(403), wantB: ok(100), cache: at(100, '1') },
      { a: 403, b: 120, first: 'A', wantA: no(403), wantB: ok(120), cache: at(120, '1') },
      { a: 403, b: 120, first: 'B', wantA: no(403), wantB: ok(120), cache: at(120, '1') },
      { a: 403, b: 403, first: 'A', wantA: no(403), wantB: no(403), cache: none },
      { a: 403, b: 403, first: 'B', wantA: no(403), wantB: no(403), cache: none },
    ];

    // manual holds every request until the test answers it, and records its
    // URL.
    function manual() {
      const pending: { url: string; answer: (r: Response) => void }[] = [];
      vi.stubGlobal('fetch', (url: string) => new Promise<Response>((answer) => pending.push({ url, answer })));
      return pending;
    }
    let tags = 0;
    function respond(a: Answer, number: number): Response {
      if (a === 403) return new Response('{}', { status: 403 });
      return new Response(JSON.stringify(page(number, a)), { status: 200, headers: { ETag: `"t${++tags}"` } });
    }
    // given settles p into what its caller is given, without throwing.
    const given = (p: Promise<ThreadPage>): Promise<Given> =>
      p.then(
        (pg) => ({ len: pg.thread.log_length }),
        (err) => ({ status: (err as { status: number }).status }),
      );
    // settled lets every pending callback run, so a second asking, if one
    // follows, has reached fetch.
    const settled = () => new Promise((r) => setTimeout(r, 0));

    async function run(row: Row, pages: [number, number]) {
      const req = manual();
      const a = given(loadThreadPage(id, { page: pages[0] }));
      const b = given(loadThreadPage(id, { page: pages[1] }));
      await Promise.resolve();
      expect(req.map((r) => r.url)).toEqual([threadUrl(id, { page: pages[0] }), threadUrl(id, { page: pages[1] })]);
      const order = row.first === 'A' ? [0, 1] : [1, 0];
      for (const i of order) {
        req[i].answer(respond(i === 0 ? row.a : row.b, pages[i]));
        await settled();
      }
      // A second asking goes out only where the row expects one, and only
      // for the page that was refused.
      if (row.again === undefined) {
        expect(req).toHaveLength(2);
      } else {
        expect(req).toHaveLength(3);
        const refusedIdx = order[1];
        expect(req[2].url).toBe(threadUrl(id, { page: pages[refusedIdx] }));
        req[2].answer(respond(row.again, pages[refusedIdx]));
      }
      expect(await a).toEqual(row.wantA);
      expect(await b).toEqual(row.wantB);

      const entry = cachedThread(id);
      if (row.cache === null) {
        expect(entry).toBeUndefined();
      } else {
        expect(entry?.logLength).toBe(row.cache.len);
        expect([...entry!.pages.keys()].sort()).toEqual(row.cache.pages);
      }
      // Nothing held for either URL is a page the cache refused: it is at
      // the entry's length, or there is none.
      for (const n of new Set(pages)) {
        const h = peekGet<ThreadPage>(threadUrl(id, { page: n }));
        if (h !== undefined) expect(h.thread.log_length).toBe(entry?.logLength);
      }
    }

    const name = '$a then $b, $first landing first';
    it.each(otherPages)(`on two pages: ${name}`, (row) => run(row, [1, 2]));
    it.each(samePage)(`on one page: ${name}`, (row) => run(row, [1, 1]));

    // Every pair of answers, in both orders, is in each table.
    it.each([
      ['two pages', otherPages],
      ['one page', samePage],
    ] as const)('covers every ordering on %s', (_, rows) => {
      const seen = new Set(rows.map((r) => `${r.a} ${r.b} ${r.first}`));
      const answers: Answer[] = [100, 120, 403];
      for (const a of answers) for (const b of answers) for (const f of ['A', 'B']) expect(seen).toContain(`${a} ${b} ${f}`);
      expect(rows).toHaveLength(18);
    });

    // The second asking is itself a load, ordered like any other.
    it('rejects with a conflict when the second asking is refused as older too', async () => {
      const req = manual();
      const a = loadThreadPage(id, { page: 1 });
      const b = given(loadThreadPage(id, { page: 2 }));
      await Promise.resolve();
      req[1].answer(respond(120, 2));
      await settled();
      req[0].answer(respond(100, 1));
      await settled();
      req[2].answer(respond(100, 1));
      await expect(a).rejects.toMatchObject({ name: 'ProblemError', type: 'conflict' });
      await settled();
      // Asked for once more, not chased.
      expect(req).toHaveLength(3);
      expect(await b).toEqual({ len: 120 });
      expect([...cachedThread(id)!.pages.keys()]).toEqual(['2']);
      expect(peekGet(threadUrl(id, { page: 1 }))).toBeUndefined();
    });

    it('rejects with a refusal that answers the second asking, and applies it', async () => {
      const req = manual();
      const a = given(loadThreadPage(id, { page: 1 }));
      const b = given(loadThreadPage(id, { page: 2 }));
      await Promise.resolve();
      req[1].answer(respond(120, 2));
      await settled();
      req[0].answer(respond(100, 1));
      await settled();
      req[2].answer(respond(403, 1));
      expect(await a).toEqual({ status: 403 });
      expect(await b).toEqual({ len: 120 });
      expect(cachedThread(id)).toBeUndefined();
      expect(peekGet(threadUrl(id, { page: 2 }))).toBeUndefined();
    });

    // A refusal that a later load has since superseded, by storing a page,
    // no longer stands, so an older page is merely stale: it is replaced,
    // not reported as a refusal the reader has already seen lifted.
    // With the same page stored it is given that one; with another, page 1
    // is asked for again, a fourth request.
    it.each([
      ['the same page', 1, 3],
      ['another page', 2, 4],
    ] as const)('replaces a page from before a superseded refusal, when the newer load stored %s', async (_, later, calls) => {
      const req = manual();
      const a = given(loadThreadPage(id, { page: 1 }));
      const r = given(loadThreadPage(id, { page: 1 }));
      await Promise.resolve();
      req[1].answer(respond(403, 1));
      expect(await r).toEqual({ status: 403 });
      const c = given(loadThreadPage(id, { page: later }));
      await Promise.resolve();
      req[2].answer(respond(120, later));
      expect(await c).toEqual({ len: 120 });
      req[0].answer(respond(100, 1));
      await settled();
      expect(req).toHaveLength(calls);
      if (calls === 4) {
        expect(req[3].url).toBe(threadUrl(id, { page: 1 }));
        req[3].answer(respond(120, 1));
      }
      expect(await a).toEqual({ len: 120 });
      expect(cachedThread(id)!.logLength).toBe(120);
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

  // TKT-01M3QGH66W: a moderation action answers with the thread's header at
  // the log length its record made, which replaces the entry: the pages at
  // the old length go, and so do the held GETs, which are as stale.
  it('replaces the entry with a longer header and forgets its pages', async () => {
    const tagged = (p: ThreadPage, tag: string) =>
      new Response(JSON.stringify(p), { status: 200, headers: { ETag: tag } });
    const o = page(1, 5);
    const otherPage = { ...o, thread: { ...o.thread, id: `${id}0` } };
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(tagged(otherPage, '"o"'))
        .mockResolvedValueOnce(tagged(page(1, 100), '"p1"'))
        .mockResolvedValueOnce(tagged(page(2, 100), '"p2"'))
        .mockResolvedValueOnce(tagged(page(1, 101), '"p1b"')),
    );
    await loadThreadPage(`${id}0`, { page: 1 });
    await loadThreadPage(id, { page: 1 });
    await loadThreadPage(id, { page: 2 });
    const before = threadCache.value;
    const header = { ...page(1, 101).thread, locked: true };
    expect(storeThreadHeader(id, header)).toBe(true);
    expect(threadCache.value).not.toBe(before);
    const e = cachedThread(id)!;
    expect(e.logLength).toBe(101);
    expect(e.header).toEqual(header);
    expect(e.pages.size).toBe(0);
    expect(peekGet(threadUrl(id, { page: 1 }))).toBeUndefined();
    expect(peekGet(threadUrl(id, { page: 2 }))).toBeUndefined();
    // Another thread's are its own.
    expect(peekGet(threadUrl(`${id}0`, { page: 1 }))).toBeDefined();
    // The page asked for again at the header's length joins it.
    await loadThreadPage(id, { page: 1 });
    expect(cachedThread(id)!.logLength).toBe(101);
    expect([...cachedThread(id)!.pages.keys()]).toEqual(['1']);
  });

  it.each([100, 90])('changes nothing for a header at length %d, no longer than the entry', async (len) => {
    const urls = serve(page(1, 100), page(2, 100));
    await loadThreadPage(id, { page: 1 });
    await loadThreadPage(id, { page: 2 });
    const before = threadCache.value;
    expect(storeThreadHeader(id, { ...page(1, len).thread, locked: true })).toBe(false);
    expect(threadCache.value).toBe(before);
    expect(cachedThread(id)!.header.locked).toBe(false);
    expect([...cachedThread(id)!.pages.keys()].sort()).toEqual(['1', '2']);
    expect(urls).toHaveLength(2);
  });

  it('files a header for a thread it holds nothing of', () => {
    const header = page(1, 7).thread;
    expect(storeThreadHeader(id, header)).toBe(true);
    expect(cachedThread(id)).toEqual({ logLength: 7, header, pages: new Map() });
  });

  it('files a page under the page the server answered with, not the one asked for', async () => {
    serve(page(3, 100));
    await loadThreadPage(id, {}); // the server picks the landing page
    expect([...cachedThread(id)!.pages.keys()]).toEqual(['3']);
  });

  // Terva review 1801: a mark all read wipes the cache (forgetAllThreadPages)
  // while a page asked for before it is still out. That page has the same
  // log length and the old read boundary; it is not stored, and the page is
  // asked for again.
  it('does not store a page asked for before the cache was wiped', async () => {
    const before = page(1, 100);
    before.viewer = { ...before.viewer, last_read_ordinal: 3 };
    const after = page(1, 100);
    after.viewer = { ...after.viewer, last_read_ordinal: 100 };
    let answer!: () => void;
    const urls: string[] = [];
    vi.stubGlobal('fetch', (url: string) => {
      urls.push(url);
      if (urls.length === 1)
        return new Promise<Response>((r) => (answer = () => r(new Response(JSON.stringify(before), { headers: { ETag: '"b"' } }))));
      return Promise.resolve(new Response(JSON.stringify(after)));
    });
    const loading = loadThreadPage(id, { page: 1 });
    await Promise.resolve();
    forgetAllThreadPages();
    answer();
    const got = await loading;
    expect(got.viewer.last_read_ordinal).toBe(100);
    expect(urls).toHaveLength(2);
    expect(cachedThread(id)!.pages.get('1')!.viewer.last_read_ordinal).toBe(100);
    expect(peekGet(threadUrl(id, { page: 1 }))).toBeUndefined();
  });

  it('stores a page asked for after the cache was wiped', async () => {
    serve(page(1, 100));
    forgetAllThreadPages();
    await loadThreadPage(id, { page: 1 });
    expect([...cachedThread(id)!.pages.keys()]).toEqual(['1']);
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
    const p = page(4, 1, { kind: 'author', value: 'bob', shown: 1, hidden: 1, gaps: [] });
    expect(pageKey(p)).toBe('4 author:bob');
    expect(pageKey(page(4, 1))).toBe('4');
  });
});
