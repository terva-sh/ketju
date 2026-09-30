import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me } from '../api/dto';
import { composerFor, loadReplyDraft, replyToPost, resetComposers } from './composer';
import { viewer } from './viewer';
import member from '../../../../internal/dto/testdata/golden/me.member.json';

// loadReplyDraft (TKT-01M3NS1931, AC2): a loaded draft fills only an empty
// composer with no reply target and no post in flight, and loading saves
// nothing back.

const T = 'thread1';
let sent: { method: string; url: string }[];
let answer: () => Response;

const draft = (source: string, reply_to: number | null) => () =>
  new Response(JSON.stringify({ thread_id: T, source, reply_to, updated_at: 0, revision: 3 }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });

const saved = viewer.value;
beforeEach(() => {
  vi.useFakeTimers();
  viewer.value = member as Me;
  resetComposers();
  localStorage.clear();
  sent = [];
  vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
    sent.push({ method: init.method ?? 'GET', url });
    return answer();
  });
});
afterEach(() => {
  resetComposers();
  viewer.value = saved;
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const settle = () => vi.advanceTimersByTimeAsync(5000);

describe('loadReplyDraft', () => {
  it('fills an empty composer with the source and the reply target', async () => {
    answer = draft('half a reply', 2);
    await loadReplyDraft(T);
    const c = composerFor(T);
    expect(c.source.value).toBe('half a reply');
    expect(c.replyTo.value).toBe(2);
    expect(c.caret.value).toBe('half a reply'.length);
  });

  it('does not overwrite typed text', async () => {
    answer = draft('from the server', 2);
    composerFor(T).source.value = 'typed here';
    await loadReplyDraft(T);
    expect(composerFor(T).source.value).toBe('typed here');
    expect(composerFor(T).replyTo.value).toBeNull();
  });

  it('does not replace a chip', async () => {
    answer = draft('from the server', 2);
    replyToPost(T, 5);
    await loadReplyDraft(T);
    expect(composerFor(T).source.value).toBe('');
    expect(composerFor(T).replyTo.value).toBe(5);
  });

  it('does not fill a composer whose post is in flight', async () => {
    answer = draft('from the server', 2);
    composerFor(T).posting.value = true;
    await loadReplyDraft(T);
    expect(composerFor(T).source.value).toBe('');
  });

  it('changes nothing on a 204', async () => {
    answer = () => new Response(null, { status: 204, headers: { 'Draft-Revision': '0' } });
    await loadReplyDraft(T);
    expect(composerFor(T).source.value).toBe('');
    expect(composerFor(T).replyTo.value).toBeNull();
  });

  it('sends no PUT, and asks once per thread', async () => {
    answer = draft('half a reply', 2);
    await loadReplyDraft(T);
    await settle();
    await loadReplyDraft(T);
    await settle();
    expect(sent).toEqual([{ method: 'GET', url: `/api/v1/drafts/${T}` }]);
  });

  it('reads a malformed body as no draft', async () => {
    answer = () => new Response(JSON.stringify({ unrelated: true }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    await loadReplyDraft(T);
    await settle();
    expect(composerFor(T).source.value).toBe('');
    expect(sent.map((s) => s.method)).toEqual(['GET']);
  });
});
