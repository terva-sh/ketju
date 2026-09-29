import { describe, expect, it } from 'vitest';
import catalogue from '../../../../internal/problem/testdata/catalogue.json';
import { asProblem, isProblem, knownTypes, problemMessage, ProblemError, toProblem } from './problem';

const codes = Object.keys(catalogue) as (keyof typeof catalogue)[];

// AC3: every code the server can send becomes words here, in one table, and
// the table names no code the server does not send.
describe('problemMessage', () => {
  it.each(codes)('has words for %s', (code) => {
    const m = problemMessage(new ProblemError(code, catalogue[code].status));
    expect(m.text.trim()).not.toBe('');
  });

  it('names nothing the catalogue lacks, but network', () => {
    expect(knownTypes.filter((t) => !(t in catalogue))).toEqual(['network']);
  });

  it('offers a sign-in for unauthenticated and never for forbidden', () => {
    const offered = knownTypes.filter((t) => problemMessage(new ProblemError(t, 0)).signIn);
    expect(offered).toEqual(['unauthenticated']);
  });

  it('carries Retry-After through', () => {
    expect(problemMessage(new ProblemError('rate-limited', 429, 12)).retryAfter).toBe(12);
    expect(problemMessage(new ProblemError('rate-limited', 429)).retryAfter).toBeNull();
  });

  it('reads anything thrown that is not a problem as a network failure', () => {
    expect(problemMessage(new TypeError('x')).text).toBe(problemMessage(new ProblemError('network', 0)).text);
    expect(isProblem(asProblem('?'), 'network')).toBe(true);
  });
});

describe('toProblem', () => {
  // The server's own answer: its type is used, from the catalogue golden.
  it.each(codes)('reads the served %s', async (code) => {
    const c = catalogue[code];
    const p = await toProblem(
      new Response(JSON.stringify(c.body), { status: c.status, headers: { 'Content-Type': c.content_type } }),
    );
    expect(p.type).toBe(code);
    expect(p.status).toBe(c.status);
  });

  // A body that is not a problem (a proxy's error page) maps from the
  // status, and to the same code the server would have sent for it. Where
  // one status carries several codes, as 403 carries forbidden and the two
  // author windows (TKT-01M29CKRHS), a bare status is the general code: it
  // cannot say which window closed.
  const general: Partial<Record<number, keyof typeof catalogue>> = { 403: 'forbidden' };
  it.each(codes)('maps a bare %s status to its code', async (code) => {
    const status = catalogue[code].status;
    const p = await toProblem(new Response('<html>', { status, headers: { 'Content-Type': 'text/html' } }));
    expect(p.type).toBe(general[status] ?? code);
  });

  it('names a general code of the status it stands for', () => {
    for (const [status, code] of Object.entries(general)) expect(catalogue[code!].status).toBe(Number(status));
  });

  it('maps a gateway failure to unavailable and anything else to internal', async () => {
    expect((await toProblem(new Response('', { status: 502 }))).type).toBe('unavailable');
    expect((await toProblem(new Response('', { status: 418 }))).type).toBe('internal');
  });

  it('does not believe a type it does not know', async () => {
    const p = await toProblem(
      new Response(JSON.stringify({ type: 'made-up', status: 404 }), {
        status: 404,
        headers: { 'Content-Type': 'application/problem+json' },
      }),
    );
    expect(p.type).toBe('not-found');
  });

  it.each([
    ['30', 30],
    [' 5 ', 5],
    ['0', 0],
    ['-1', null],
    ['1.5', null],
    ['Wed, 21 Oct 2026 07:28:00 GMT', null],
  ])('reads Retry-After %j as %j', async (raw, want) => {
    const p = await toProblem(new Response('', { status: 429, headers: { 'Retry-After': raw } }));
    expect(p.retryAfter).toBe(want);
  });
});
