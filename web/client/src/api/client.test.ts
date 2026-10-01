import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { apiGet, apiGetOptional, apiSend, apiUpload, forgetGets, heldLimit, peekGet, resetApiCache } from './client';
import { isProblem, ProblemError } from './problem';

const json = (body: unknown, init: ResponseInit = {}) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' }, ...init });

interface Call {
  url: string;
  headers: Record<string, string>;
  method: string;
  body: unknown;
}

// stub answers each request with the next response and records what was
// asked.
function stub(...answers: (Response | Error)[]) {
  const calls: Call[] = [];
  vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
    calls.push({
      url,
      headers: (init.headers ?? {}) as Record<string, string>,
      method: init.method ?? 'GET',
      body: init.body,
    });
    const a = answers.shift();
    if (!a) throw new Error('no answer left');
    if (a instanceof Error) throw a;
    return a;
  });
  return calls;
}

beforeEach(() => resetApiCache());
afterEach(() => vi.unstubAllGlobals());

// AC1: a conditional request, and a 304 served from what is held.
describe('apiGet', () => {
  it('sends If-None-Match with the held tag and serves the held body on 304', async () => {
    const calls = stub(json({ n: 1 }, { headers: { ETag: '"a-1"' } }), new Response(null, { status: 304 }));
    expect(await apiGet('/api/v1/x')).toEqual({ n: 1 });
    expect(calls[0].headers['If-None-Match']).toBeUndefined();
    expect(await apiGet('/api/v1/x')).toEqual({ n: 1 });
    expect(calls[1].headers['If-None-Match']).toBe('"a-1"');
  });

  it('replaces the held body and tag on a 200', async () => {
    const calls = stub(
      json({ n: 1 }, { headers: { ETag: '"a-1"' } }),
      json({ n: 2 }, { headers: { ETag: '"a-2"' } }),
      new Response(null, { status: 304 }),
    );
    await apiGet('/api/v1/x');
    expect(await apiGet('/api/v1/x')).toEqual({ n: 2 });
    expect(await apiGet('/api/v1/x')).toEqual({ n: 2 });
    expect(calls[2].headers['If-None-Match']).toBe('"a-2"');
  });

  it('holds nothing for a response without a tag, and drops what it held', async () => {
    const calls = stub(
      json({ n: 1 }, { headers: { ETag: '"a-1"' } }),
      json({ n: 2 }),
      json({ n: 3 }),
    );
    await apiGet('/api/v1/x');
    await apiGet('/api/v1/x');
    expect(peekGet('/api/v1/x')).toBeUndefined();
    await apiGet('/api/v1/x');
    expect(calls[2].headers['If-None-Match']).toBeUndefined();
  });

  it('keeps each URL apart', async () => {
    const calls = stub(json({ n: 1 }, { headers: { ETag: '"a"' } }), json({ n: 2 }, { headers: { ETag: '"b"' } }));
    await apiGet('/api/v1/a');
    await apiGet('/api/v1/b');
    expect(calls[1].headers['If-None-Match']).toBeUndefined();
    expect(peekGet('/api/v1/a')).toEqual({ n: 1 });
    expect(peekGet('/api/v1/b')).toEqual({ n: 2 });
  });

  it('treats a 304 it did not ask for as an error, not as data', async () => {
    stub(new Response(null, { status: 304 }));
    await expect(apiGet('/api/v1/x')).rejects.toBeInstanceOf(ProblemError);
  });

  it('holds at most heldLimit bodies, evicting the oldest', async () => {
    const answers: Response[] = [];
    for (let i = 0; i <= heldLimit; i++) answers.push(json({ i }, { headers: { ETag: `"${i}"` } }));
    stub(...answers);
    for (let i = 0; i < heldLimit; i++) await apiGet(`/u/${i}`);
    expect(peekGet('/u/0')).toEqual({ i: 0 });
    await apiGet(`/u/${heldLimit}`); // one past the limit
    expect(peekGet('/u/0')).toBeUndefined();
    expect(peekGet('/u/1')).toEqual({ i: 1 });
    expect(peekGet(`/u/${heldLimit}`)).toEqual({ i: heldLimit });
  });

  it('counts a 304 as a use, so a page read again is kept', async () => {
    const answers: Response[] = [json({ i: 0 }, { headers: { ETag: '"0"' } })];
    for (let i = 1; i < heldLimit; i++) answers.push(json({ i }, { headers: { ETag: `"${i}"` } }));
    answers.push(new Response(null, { status: 304 }), json({ i: 'new' }, { headers: { ETag: '"n"' } }));
    stub(...answers);
    for (let i = 0; i < heldLimit; i++) await apiGet(`/u/${i}`);
    await apiGet('/u/0'); // 304: now the most recent
    await apiGet('/u/new'); // evicts /u/1, not /u/0
    expect(peekGet('/u/0')).toEqual({ i: 0 });
    expect(peekGet('/u/1')).toBeUndefined();
  });

  it('turns a network failure into a network problem', async () => {
    stub(new TypeError('Failed to fetch'));
    const err = await apiGet('/api/v1/x').catch((e) => e);
    expect(isProblem(err, 'network')).toBe(true);
  });

  it('passes an abort through as an abort', async () => {
    stub(new DOMException('aborted', 'AbortError'));
    const err = await apiGet('/api/v1/x').catch((e) => e);
    expect(err).toBeInstanceOf(DOMException);
  });

  it('reads a problem+json body for its type', async () => {
    stub(
      new Response(JSON.stringify({ type: 'rate-limited', title: 'x', status: 429 }), {
        status: 429,
        headers: { 'Content-Type': 'application/problem+json', 'Retry-After': '7' },
      }),
    );
    const err = (await apiGet('/api/v1/x').catch((e) => e)) as ProblemError;
    expect(err.type).toBe('rate-limited');
    expect(err.retryAfter).toBe(7);
  });

  it('does not keep a failed response as the held body', async () => {
    const calls = stub(
      json({ n: 1 }, { headers: { ETag: '"a"' } }),
      new Response('{}', { status: 500 }),
      new Response(null, { status: 304 }),
    );
    await apiGet('/api/v1/x');
    await expect(apiGet('/api/v1/x')).rejects.toBeInstanceOf(ProblemError);
    expect(await apiGet('/api/v1/x')).toEqual({ n: 1 });
    expect(calls[2].headers['If-None-Match']).toBe('"a"');
  });

  // A refusal of access takes the held copy with it; any other failure
  // leaves it, to be revalidated on the next try.
  it.each([404, 410, 403, 401])('drops the held body on a %d', async (status) => {
    stub(json({ n: 1 }, { headers: { ETag: '"a"' } }), new Response('{}', { status }));
    await apiGet('/api/v1/x');
    await apiGet('/api/v1/x').catch(() => {});
    expect(peekGet('/api/v1/x')).toBeUndefined();
  });

  it('keeps the held body through a 503', async () => {
    stub(json({ n: 1 }, { headers: { ETag: '"a"' } }), new Response('{}', { status: 503 }));
    await apiGet('/api/v1/x');
    await apiGet('/api/v1/x').catch(() => {});
    expect(peekGet('/api/v1/x')).toEqual({ n: 1 });
  });

  it('forgets exactly the held bodies it is asked to', async () => {
    stub(json({ n: 1 }, { headers: { ETag: '"a"' } }), json({ n: 2 }, { headers: { ETag: '"b"' } }));
    await apiGet('/api/v1/a');
    await apiGet('/api/v1/b');
    forgetGets((u) => u.endsWith('/a'));
    expect(peekGet('/api/v1/a')).toBeUndefined();
    expect(peekGet('/api/v1/b')).toEqual({ n: 2 });
  });

  // Terva review 1624 on PR #248: with inFlight, a request still out to a
  // matching URL is superseded, and its answer is returned but not held;
  // without it, and for a URL that does not match, the answer is held.
  it('supersedes matching requests still out when asked to', async () => {
    const releases: (() => void)[] = [];
    vi.stubGlobal(
      'fetch',
      (url: string) =>
        new Promise<Response>((r) => releases.push(() => r(json({ url }, { headers: { ETag: `"${url}"` } })))),
    );
    const a = apiGet('/api/v1/forums/a/threads');
    const b = apiGet('/api/v1/other');
    const c = apiGet('/api/v1/forums/c/threads');
    await Promise.resolve();
    forgetGets((u) => u.startsWith('/api/v1/forums/a'), { inFlight: true });
    forgetGets((u) => u.startsWith('/api/v1/forums/c'));
    for (const r of releases) r();
    expect(await a).toEqual({ url: '/api/v1/forums/a/threads' });
    await b;
    await c;
    expect(peekGet('/api/v1/forums/a/threads')).toBeUndefined();
    expect(peekGet('/api/v1/other')).toEqual({ url: '/api/v1/other' });
    expect(peekGet('/api/v1/forums/c/threads')).toEqual({ url: '/api/v1/forums/c/threads' });
  });

  // Review 412: an aborted request's answer is late, and is not held.
  it('does not hold the answer to an aborted request', async () => {
    stub(json({ n: 'old' }, { headers: { ETag: '"old"' } }));
    const ctl = new AbortController();
    const late = apiGet('/api/v1/x', { signal: ctl.signal });
    ctl.abort();
    expect(await late).toEqual({ n: 'old' });
    expect(peekGet('/api/v1/x')).toBeUndefined();
  });

  it('keeps what it held through the aborted answer', async () => {
    stub(json({ n: 'new' }, { headers: { ETag: '"new"' } }), json({ n: 'old' }));
    await apiGet('/api/v1/x');
    const ctl = new AbortController();
    const late = apiGet('/api/v1/x', { signal: ctl.signal });
    ctl.abort();
    await late;
    expect(peekGet('/api/v1/x')).toEqual({ n: 'new' });
  });

  it('keeps what it held through an aborted request refused access', async () => {
    stub(json({ n: 'new' }, { headers: { ETag: '"new"' } }), new Response('{}', { status: 403 }));
    await apiGet('/api/v1/x');
    const ctl = new AbortController();
    const late = apiGet('/api/v1/x', { signal: ctl.signal });
    ctl.abort();
    await late.catch(() => {});
    expect(peekGet('/api/v1/x')).toEqual({ n: 'new' });
  });

  it('does not count an aborted 304 as a use', async () => {
    const answers: Response[] = [];
    for (let i = 0; i < heldLimit; i++) answers.push(json({ i }, { headers: { ETag: `"${i}"` } }));
    answers.push(new Response(null, { status: 304 }), json({ i: 'new' }, { headers: { ETag: '"n"' } }));
    stub(...answers);
    for (let i = 0; i < heldLimit; i++) await apiGet(`/u/${i}`);
    const ctl = new AbortController();
    const late = apiGet('/u/0', { signal: ctl.signal });
    ctl.abort();
    await late;
    await apiGet('/u/new'); // /u/0 is still the oldest, and goes
    expect(peekGet('/u/0')).toBeUndefined();
  });

  // Review 413: overlapping requests for one URL, answered out of order.
  // Only the newest one writes what is held.
  describe('overlapping requests for one URL', () => {
    // manual answers each request when the test says, in any order.
    function manual() {
      const pending: ((r: Response) => void)[] = [];
      vi.stubGlobal('fetch', () => new Promise<Response>((res) => pending.push(res)));
      return pending;
    }

    it('does not let an older 304 put back the body a newer 200 replaced', async () => {
      stub(json({ v: 1 }, { headers: { ETag: '"1"' } }));
      await apiGet('/api/v1/x');
      const answer = manual();
      const older = apiGet('/api/v1/x');
      const newer = apiGet('/api/v1/x');
      await Promise.resolve();
      answer[1](json({ v: 2 }, { headers: { ETag: '"2"' } }));
      expect(await newer).toEqual({ v: 2 });
      answer[0](new Response(null, { status: 304 }));
      expect(await older).toEqual({ v: 1 }); // its own answer, to its caller
      expect(peekGet('/api/v1/x')).toEqual({ v: 2 });
    });

    it('does not let an older 200 replace a newer one', async () => {
      const answer = manual();
      const older = apiGet('/api/v1/x');
      const newer = apiGet('/api/v1/x');
      await Promise.resolve();
      answer[1](json({ v: 'new' }, { headers: { ETag: '"n"' } }));
      await newer;
      answer[0](json({ v: 'old' }, { headers: { ETag: '"o"' } }));
      await older;
      expect(peekGet('/api/v1/x')).toEqual({ v: 'new' });
    });

    it('does not let an older refusal drop what a newer answer held', async () => {
      const answer = manual();
      const older = apiGet('/api/v1/x');
      const newer = apiGet('/api/v1/x');
      await Promise.resolve();
      answer[1](json({ v: 'new' }, { headers: { ETag: '"n"' } }));
      await newer;
      answer[0](new Response('{}', { status: 403 }));
      await older.catch(() => {});
      expect(peekGet('/api/v1/x')).toEqual({ v: 'new' });
    });

    it('lets the newer one write even when it finishes first and the older fails', async () => {
      const answer = manual();
      const older = apiGet('/api/v1/x');
      const newer = apiGet('/api/v1/x');
      await Promise.resolve();
      answer[0](new Response('{}', { status: 500 }));
      await older.catch(() => {});
      answer[1](json({ v: 'new' }, { headers: { ETag: '"n"' } }));
      await newer;
      expect(peekGet('/api/v1/x')).toEqual({ v: 'new' });
    });

    it('keeps requests for different URLs apart', async () => {
      const answer = manual();
      const a = apiGet('/api/v1/a');
      const b = apiGet('/api/v1/b');
      await Promise.resolve();
      answer[1](json({ v: 'b' }, { headers: { ETag: '"b"' } }));
      answer[0](json({ v: 'a' }, { headers: { ETag: '"a"' } }));
      await Promise.all([a, b]);
      expect(peekGet('/api/v1/a')).toEqual({ v: 'a' });
      expect(peekGet('/api/v1/b')).toEqual({ v: 'b' });
    });
  });

  it('calls a 200 whose body is not JSON internal', async () => {
    stub(new Response('<html>', { status: 200 }));
    const err = await apiGet('/api/v1/x').catch((e) => e);
    expect(isProblem(err, 'internal')).toBe(true);
  });
});

describe('apiSend', () => {
  it('sends JSON, as the API requires of a mutation', async () => {
    const calls = stub(json({ ok: true }, { status: 201 }));
    expect(await apiSend('POST', '/api/v1/x', { a: 1 })).toEqual({ ok: true });
    expect(calls[0].method).toBe('POST');
    expect(calls[0].headers['Content-Type']).toBe('application/json');
    expect(calls[0].body).toBe('{"a":1}');
  });

  it('answers 204 with undefined', async () => {
    stub(new Response(null, { status: 204 }));
    expect(await apiSend('DELETE', '/api/v1/x')).toBeUndefined();
  });

  // TKT-01M3NS1931: a draft's DELETE hands over the revision its 204 carries.
  it('hands a 204’s headers to onHeaders', async () => {
    stub(new Response(null, { status: 204, headers: { 'Draft-Revision': '7' } }));
    let seen: string | null = null;
    await apiSend('DELETE', '/api/v1/x', undefined, { onHeaders: (h) => (seen = h.get('Draft-Revision')) });
    expect(seen).toBe('7');
  });

  it('calls fetch before it returns, so a pagehide handler starts the request', () => {
    const calls = stub(new Response(null, { status: 204 }));
    void apiSend('PUT', '/api/v1/x', { a: 1 }, { keepalive: true });
    expect(calls).toHaveLength(1);
  });

  // TKT-01M3QQWA5Z: a caller that names its status accepts no other 2xx.
  it('refuses a success other than the status asked for', async () => {
    stub(json({ ok: true }, { status: 201 }));
    const err = await apiSend('POST', '/api/v1/x', {}, { status: 200 }).catch((e) => e);
    expect(isProblem(err, 'internal')).toBe(true);
    stub(json({ ok: true }, { status: 200 }));
    expect(await apiSend('POST', '/api/v1/x', {}, { status: 200 })).toEqual({ ok: true });
  });

  // Review 417: every failure is a ProblemError, one that happens before
  // any request is made included.
  it.each([
    ['a cycle', (() => {
      const o: Record<string, unknown> = {};
      o.self = o;
      return o;
    })()],
    ['a BigInt', { n: BigInt(1) }],
  ])('turns a body it cannot serialize (%s) into a problem, without asking', async (_, body) => {
    const calls = stub();
    const err = await apiSend('POST', '/api/v1/x', body).catch((e) => e);
    expect(err).toBeInstanceOf(ProblemError);
    expect(isProblem(err, 'internal')).toBe(true);
    expect(calls).toEqual([]);
  });

  it('throws a problem for a refusal', async () => {
    stub(
      new Response(JSON.stringify({ type: 'validation-failed', title: 't', status: 422 }), {
        status: 422,
        headers: { 'Content-Type': 'application/problem+json' },
      }),
    );
    const err = await apiSend('PUT', '/api/v1/x', {}).catch((e) => e);
    expect(isProblem(err, 'validation-failed')).toBe(true);
  });
});

// TKT-01M3NS1931: the drafts routes answer 204 for "there is none", with the
// draft's revision in a header.
describe('apiGetOptional', () => {
  it('answers a null body and the headers for 204', async () => {
    stub(new Response(null, { status: 204, headers: { 'Draft-Revision': '3' } }));
    const r = await apiGetOptional('/api/v1/drafts/t');
    expect(r.body).toBeNull();
    expect(r.headers.get('Draft-Revision')).toBe('3');
  });

  it('answers the body for 200, and holds nothing', async () => {
    const calls = stub(json({ n: 1 }, { headers: { ETag: '"a-1"' } }), json({ n: 2 }, { headers: { ETag: '"a-2"' } }));
    expect((await apiGetOptional('/api/v1/drafts/t')).body).toEqual({ n: 1 });
    expect((await apiGetOptional('/api/v1/drafts/t')).body).toEqual({ n: 2 });
    expect(calls[1].headers['If-None-Match']).toBeUndefined();
    expect(peekGet('/api/v1/drafts/t')).toBeUndefined();
  });

  it('throws a problem for an error', async () => {
    stub(new Response(JSON.stringify({ type: 'forbidden', title: 't', status: 403 }), {
      status: 403,
      headers: { 'Content-Type': 'application/problem+json' },
    }));
    const err = await apiGetOptional('/api/v1/drafts/t').catch((e) => e);
    expect(isProblem(err, 'forbidden')).toBe(true);
  });
});

// TKT-01M3V73J8K: POST /blobs takes one multipart part named file.
describe('apiUpload', () => {
  it('apiUpload sends one file part and maps problems', async () => {
    const calls = stub(
      json({ hash: 'h', size: 1 }, { status: 201 }),
      new Response(JSON.stringify({ type: 'validation-failed', status: 422, errors: [{ field: 'file', code: 'type_not_allowed' }] }), {
        status: 422,
        headers: { 'Content-Type': 'application/problem+json' },
      }),
      new Response('', { status: 413 }),
    );
    const f = new File(['bytes'], 'a.png', { type: 'image/png' });
    expect(await apiUpload('/api/v1/blobs?thread=t', f)).toEqual({ hash: 'h', size: 1 });
    const [c] = calls;
    expect(c.method).toBe('POST');
    // No Content-Type: the browser writes the multipart boundary.
    expect(c.headers).toEqual({ Accept: 'application/json' });
    const body = c.body as FormData;
    expect([...body.keys()]).toEqual(['file']);
    expect((body.get('file') as File).name).toBe('a.png');

    const refused = await apiUpload('/api/v1/blobs?thread=t', f).catch((e) => e);
    expect(refused).toBeInstanceOf(ProblemError);
    expect((refused as ProblemError).fieldErrors).toEqual({ file: 'type_not_allowed' });
    expect(isProblem(await apiUpload('/api/v1/blobs?thread=t', f).catch((e) => e), 'payload-too-large')).toBe(true);
  });
});
