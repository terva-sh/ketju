import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { apiGet, apiSend, forgetGets, heldLimit, peekGet, resetApiCache } from './client';
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
