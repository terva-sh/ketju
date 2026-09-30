import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { resetApiCache } from '../api/client';
import type { Me } from '../api/dto';
import { composerFor, discardReply, loadReplyDraft, postReply, resetComposers } from './composer';
import { draftDelay, draftNote, keepaliveLimit, startDrafts } from './drafts';
import { viewer } from './viewer';
import draftsSource from './drafts.ts?raw';
import member from '../../../../internal/dto/testdata/golden/me.member.json';
import anon from '../../../../internal/dto/testdata/golden/me.anon.json';

// Draft autosave (TKT-01M3NS1931) against a fake server that keeps the
// drafts API's rules, a stubbed fetch and fake timers.

const T = 'thread1';
const url = `/api/v1/drafts/${T}`;
const key = { kind: 'reply', thread: T } as const;
const copyKey = `ketju.draft.alice.reply:${T}`;

interface Call {
  method: string;
  url: string;
  body: { source?: string; reply_to?: number | null } | undefined;
  keepalive: boolean;
}

// server is one draft key as docs/07 "Drafts" keeps it: a PUT adds one to
// the revision, a DELETE of a live draft adds one and leaves a tombstone, a
// DELETE of none changes nothing, and every 204 carries the revision.
let server: { revision: number; live: { source: string; reply_to: number | null } | null };
let calls: Call[];
// refuse answers a request instead of the server, when it returns one.
let refuse: (c: Call) => Response | undefined;

// A gate holds the next request of its method unanswered until the test
// says what happened to it: land applies it and answers; lose applies it and
// the answer never arrives, as when the page is torn down; fail neither
// applies nor answers, as a network failure.
interface Gate {
  method: string;
  call?: Call;
  resolve?: (r: Response) => void;
  reject?: (e: unknown) => void;
}
let gates: Gate[];
const hold = (method: string): Gate => {
  const g: Gate = { method };
  gates.push(g);
  return g;
};
const land = async (g: Gate) => {
  g.resolve!(apply(g.call!));
  await tick();
};
const lose = (g: Gate) => void apply(g.call!);
const fail = async (g: Gate) => {
  g.reject!(new TypeError('network'));
  await tick();
};

const jsonAnswer = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
const none = () => new Response(null, { status: 204, headers: { 'Draft-Revision': String(server.revision) } });
const problem = (type: string, status: number, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify({ type, title: type, status }), {
    status,
    headers: { 'Content-Type': 'application/problem+json', ...headers },
  });

function apply(c: Call): Response {
  if (c.url === `/api/v1/threads/${T}/posts`) return jsonAnswer({ ordinal: 5 }, 201);
  if (c.url !== url) return problem('not-found', 404);
  switch (c.method) {
    case 'GET':
      return server.live ? jsonAnswer({ thread_id: T, ...server.live, updated_at: 0, revision: server.revision }) : none();
    case 'PUT':
      server.revision++;
      server.live = { source: c.body!.source!, reply_to: c.body!.reply_to ?? null };
      return jsonAnswer({ thread_id: T, ...server.live, updated_at: 0, revision: server.revision });
    default:
      if (server.live) {
        server.revision++;
        server.live = null;
      }
      return none();
  }
}

function fakeFetch(u: string, init: RequestInit = {}): Promise<Response> {
  const c: Call = {
    method: init.method ?? 'GET',
    url: u,
    body: init.body ? JSON.parse(init.body as string) : undefined,
    keepalive: !!init.keepalive,
  };
  calls.push(c);
  const g = gates.find((g) => g.method === c.method && !g.call);
  if (g) {
    g.call = c;
    return new Promise((resolve, reject) => {
      g.resolve = resolve;
      g.reject = reject;
    });
  }
  return Promise.resolve(refuse(c) ?? apply(c));
}

async function tick() {
  for (let i = 0; i < 10; i++) await vi.advanceTimersByTimeAsync(0);
}
const wait = (ms: number) => vi.advanceTimersByTimeAsync(ms).then(tick);

// writes is every draft request but the loads, in the order they started.
const writes = () =>
  calls.filter((c) => c.url === url && c.method !== 'GET').map((c) => (c.method === 'PUT' ? `PUT ${c.body!.source}` : 'DELETE'));
const drafts = () => calls.filter((c) => c.url === url);
const copy = () => JSON.parse(localStorage.getItem(copyKey) ?? 'null');
const store = (snapshot: { source: string; reply_to: number | null }, base: number | null, inflight: number, deletes: number) =>
  localStorage.setItem(copyKey, JSON.stringify({ snapshot, base, inflight, deletes }));
const pagehide = () => dispatchEvent(new Event('pagehide'));

// newPage is this browser loading the page again: the last page's memory is
// gone, its requests in flight answer nobody, and localStorage is kept.
function newPage() {
  resetComposers();
  calls = [];
}

const saved = viewer.value;
beforeEach(() => {
  vi.useFakeTimers();
  viewer.value = member as Me;
  resetComposers();
  resetApiCache();
  localStorage.clear();
  server = { revision: 0, live: null };
  calls = [];
  gates = [];
  refuse = () => undefined;
  vi.stubGlobal('fetch', fakeFetch);
});
afterEach(() => {
  resetComposers();
  viewer.value = saved;
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

// AC3.
describe('saving', () => {
  it('saves once after typing stops', async () => {
    await loadReplyDraft(T);
    const c = composerFor(T);
    for (let i = 1; i <= 10; i++) {
      c.source.value = 'hello world'.slice(0, i);
      await wait(draftDelay - 100);
    }
    expect(writes()).toEqual([]);
    await wait(100);
    expect(writes()).toEqual(['PUT hello worl']);
    await wait(draftDelay * 3);
    expect(writes()).toHaveLength(1);
    expect(server.live).toEqual({ source: 'hello worl', reply_to: null });
    expect(draftNote(key)).toBe('Draft saved');
  });

  it('sends nothing unchanged', async () => {
    await loadReplyDraft(T);
    const c = composerFor(T);
    c.source.value = 'a';
    await wait(draftDelay);
    c.source.value = 'ab';
    c.source.value = 'a';
    await wait(draftDelay);
    c.replyTo.value = 2;
    c.replyTo.value = null;
    await wait(draftDelay);
    expect(writes()).toEqual(['PUT a']);
  });

  it('saves the reply target with the text', async () => {
    await loadReplyDraft(T);
    composerFor(T).replyTo.value = 3;
    await wait(draftDelay);
    expect(server.live).toEqual({ source: '', reply_to: 3 });
    composerFor(T).replyTo.value = null;
    await wait(draftDelay);
    expect(writes()).toEqual(['PUT ', 'DELETE']);
  });

  it('clearing deletes', async () => {
    await loadReplyDraft(T);
    const c = composerFor(T);
    c.source.value = 'x';
    await wait(draftDelay);
    c.source.value = '';
    await wait(draftDelay);
    expect(writes()).toEqual(['PUT x', 'DELETE']);
    expect(server.live).toBeNull();
    expect(draftNote(key)).toBe('');
  });

  it('an untouched empty composer sends nothing', async () => {
    await loadReplyDraft(T);
    await wait(draftDelay * 3);
    const c = composerFor(T);
    c.source.value = 'x';
    c.source.value = '';
    await wait(draftDelay * 3);
    // Blank but not what was loaded: still nothing the server holds.
    c.source.value = '   ';
    await wait(draftDelay * 3);
    expect(writes()).toEqual([]);
  });

  it('requests keep their order', async () => {
    await loadReplyDraft(T);
    const c = composerFor(T);
    const slow = hold('PUT');
    c.source.value = 'a';
    await wait(draftDelay);
    c.source.value = 'ab';
    await wait(draftDelay);
    c.source.value = '';
    await wait(draftDelay);
    // One request at a time: the newest waiting one replaces the older.
    expect(writes()).toEqual(['PUT a']);
    await land(slow);
    expect(writes()).toEqual(['PUT a', 'DELETE']);
    expect(server.live).toBeNull();
  });

  it('sends a draft at the keepalive limit without keepalive', async () => {
    await loadReplyDraft(T);
    composerFor(T).source.value = 'y'.repeat(keepaliveLimit);
    await wait(draftDelay);
    expect(drafts().filter((c) => c.method === 'PUT').map((c) => c.keepalive)).toEqual([false]);
    composerFor(T).source.value = 'short';
    await wait(draftDelay);
    expect(drafts().filter((c) => c.method === 'PUT').map((c) => c.keepalive)).toEqual([false, true]);
  });
});

// AC6.
describe('before the draft has loaded', () => {
  it('an empty composer never deletes before its draft loads', async () => {
    server = { revision: 1, live: { source: 'kept', reply_to: 2 } };
    const get = hold('GET');
    const loading = loadReplyDraft(T);
    const c = composerFor(T);
    c.replyTo.value = 1;
    c.replyTo.value = null;
    await wait(3000);
    expect(drafts().map((c) => c.method)).toEqual(['GET']);
    await land(get);
    await loading;
    expect(c.source.value).toBe('kept');
    expect(c.replyTo.value).toBe(2);
    await wait(draftDelay * 2);
    expect(writes()).toEqual([]);
    expect(server.live).toEqual({ source: 'kept', reply_to: 2 });
  });

  it('typing before the load finishes is saved once, after it', async () => {
    server = { revision: 1, live: { source: 'theirs', reply_to: null } };
    const get = hold('GET');
    const loading = loadReplyDraft(T);
    const c = composerFor(T);
    c.source.value = 'mine';
    await wait(3000);
    expect(writes()).toEqual([]);
    expect(draftNote(key)).toBe('');
    await land(get);
    await loading;
    expect(c.source.value).toBe('mine');
    await wait(draftDelay * 3);
    expect(writes()).toEqual(['PUT mine']);
  });

  it('a failed load never deletes', async () => {
    server = { revision: 1, live: { source: 'kept', reply_to: null } };
    refuse = (c) => (c.method === 'GET' ? problem('internal', 500) : undefined);
    await loadReplyDraft(T);
    const c = composerFor(T);
    c.source.value = 'x';
    c.source.value = '';
    await wait(draftDelay * 2);
    expect(writes()).toEqual([]);
    expect(server.live).toEqual({ source: 'kept', reply_to: null });
    c.source.value = 'typed';
    await wait(draftDelay);
    expect(writes()).toEqual(['PUT typed']);
  });

  it('a disarmed key sends nothing on pagehide', async () => {
    hold('GET');
    void loadReplyDraft(T);
    composerFor(T).source.value = 'x';
    pagehide();
    await wait(draftDelay);
    expect(writes()).toEqual([]);
    expect(copy()).toBeNull();
  });

  it('a post during the load deletes, and the load does not bring the draft back', async () => {
    server = { revision: 1, live: { source: 'old', reply_to: null } };
    const get = hold('GET');
    const loading = loadReplyDraft(T);
    const c = composerFor(T);
    c.source.value = 'quick reply';
    // The GET was answered before the post, and the answer arrives after it.
    const answer = apply(get.call!);
    expect(await postReply(T)).not.toBeNull();
    get.resolve!(answer);
    await tick();
    await loading;
    await wait(draftDelay * 2);
    expect(c.source.value).toBe('');
    expect(writes()).toEqual(['DELETE']);
    expect(server.live).toBeNull();
  });
});

// AC5.
describe('refusals', () => {
  it.each([
    ['forbidden', 403],
    ['conflict', 409],
    ['not-found', 404],
    ['unauthenticated', 401],
  ])('a refusal (%s) turns autosave off for the thread', async (type, status) => {
    await loadReplyDraft(T);
    refuse = (c) => (c.method === 'PUT' ? problem(type, status) : undefined);
    const c = composerFor(T);
    c.source.value = 'a';
    await wait(draftDelay);
    c.source.value = 'ab';
    await wait(draftDelay * 2);
    pagehide();
    expect(writes()).toEqual(['PUT a']);
    expect(draftNote(key)).toBe('');
    expect(copy()).toBeNull();
  });

  it('a refused load turns autosave off', async () => {
    refuse = (c) => (c.method === 'GET' ? problem('forbidden', 403) : undefined);
    await loadReplyDraft(T);
    composerFor(T).source.value = 'a';
    await wait(draftDelay * 2);
    expect(writes()).toEqual([]);
  });

  it('rate-limited waits for Retry-After', async () => {
    await loadReplyDraft(T);
    let once = true;
    refuse = (c) => {
      if (c.method !== 'PUT' || !once) return undefined;
      once = false;
      return problem('rate-limited', 429, { 'Retry-After': '5' });
    };
    composerFor(T).source.value = 'a';
    await wait(draftDelay);
    expect(writes()).toEqual(['PUT a']);
    expect(draftNote(key)).toBe('Draft not saved');
    await wait(4900);
    expect(writes()).toEqual(['PUT a']);
    await wait(100);
    expect(writes()).toEqual(['PUT a', 'PUT a']);
    expect(draftNote(key)).toBe('Draft saved');
  });

  // Terva review 1580, finding 2.
  it('neither an edit, a hidden page nor pagehide shortens a Retry-After', async () => {
    await loadReplyDraft(T);
    let once = true;
    refuse = (c) => {
      if (c.method !== 'PUT' || !once) return undefined;
      once = false;
      return problem('rate-limited', 429, { 'Retry-After': '5' });
    };
    const c = composerFor(T);
    c.source.value = 'a';
    await wait(draftDelay);
    await wait(1000);
    c.source.value = 'ab';
    await wait(draftDelay + 500);
    Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true });
    document.dispatchEvent(new Event('visibilitychange'));
    delete (document as { visibilityState?: string }).visibilityState;
    pagehide();
    expect(writes()).toEqual(['PUT a']);
    expect(copy()).toEqual({ snapshot: { source: 'ab', reply_to: null }, base: 0, inflight: 0, deletes: 0 });
    await wait(1900);
    expect(writes()).toEqual(['PUT a']);
    await wait(100);
    expect(writes()).toEqual(['PUT a', 'PUT ab']);
  });

  it('a post during a Retry-After deletes once it has passed', async () => {
    await loadReplyDraft(T);
    const c = composerFor(T);
    c.source.value = 'x';
    await wait(draftDelay);
    refuse = (c) => (c.method === 'PUT' ? problem('rate-limited', 429, { 'Retry-After': '5' }) : undefined);
    c.source.value = 'the reply';
    await wait(draftDelay);
    refuse = () => undefined;
    expect(await postReply(T)).not.toBeNull();
    await wait(4900);
    expect(writes()).toEqual(['PUT x', 'PUT the reply']);
    await wait(100);
    expect(writes()).toEqual(['PUT x', 'PUT the reply', 'DELETE']);
    expect(server.live).toBeNull();
  });

  it('another failure says so, and the next change retries', async () => {
    await loadReplyDraft(T);
    refuse = (c) => (c.method === 'PUT' ? problem('internal', 500) : undefined);
    composerFor(T).source.value = 'a';
    await wait(draftDelay * 3);
    expect(writes()).toEqual(['PUT a']);
    expect(draftNote(key)).toBe('Draft not saved');
    refuse = () => undefined;
    composerFor(T).source.value = 'ab';
    await wait(draftDelay);
    expect(writes()).toEqual(['PUT a', 'PUT ab']);
    expect(draftNote(key)).toBe('Draft saved');
  });
});

// AC7.
describe('posting and discarding', () => {
  it('a post deletes and cancels the pending save', async () => {
    await loadReplyDraft(T);
    const c = composerFor(T);
    c.source.value = 'x';
    await wait(draftDelay);
    c.source.value = 'the reply';
    expect(await postReply(T)).not.toBeNull();
    await wait(draftDelay * 3);
    expect(writes()).toEqual(['PUT x', 'DELETE']);
    expect(server.live).toBeNull();
    expect(apply({ method: 'GET', url, body: undefined, keepalive: false }).status).toBe(204);
  });

  it('a post clears the local copy', async () => {
    await loadReplyDraft(T);
    composerFor(T).source.value = 'the reply';
    store({ source: 'the reply', reply_to: null }, 0, 1, 0);
    await postReply(T);
    await tick();
    expect(copy()).toBeNull();
    newPage();
    await loadReplyDraft(T);
    await wait(draftDelay * 2);
    expect(composerFor(T).source.value).toBe('');
    expect(writes()).toEqual([]);
  });

  it('a discard clears the local copy', async () => {
    await loadReplyDraft(T);
    composerFor(T).source.value = 'unwanted';
    store({ source: 'unwanted', reply_to: null }, 0, 1, 0);
    discardReply(T);
    await tick();
    expect(copy()).toBeNull();
    newPage();
    await loadReplyDraft(T);
    await wait(draftDelay * 2);
    expect(composerFor(T).source.value).toBe('');
    expect(writes()).toEqual([]);
  });

  it('a post while a save is in flight deletes after it', async () => {
    await loadReplyDraft(T);
    const c = composerFor(T);
    const put = hold('PUT');
    c.source.value = 'the reply';
    await wait(draftDelay);
    await postReply(T);
    await tick();
    expect(writes()).toEqual(['PUT the reply']);
    await land(put);
    expect(writes()).toEqual(['PUT the reply', 'DELETE']);
    expect(server.live).toBeNull();
  });
});

// AC9.
describe('pagehide', () => {
  it('starts the save inside the handler', async () => {
    await loadReplyDraft(T);
    composerFor(T).source.value = 'x';
    pagehide();
    // No await: the request started before the handler returned.
    expect(drafts().filter((c) => c.method === 'PUT')).toEqual([
      { method: 'PUT', url, body: { source: 'x', reply_to: null }, keepalive: true },
    ]);
  });

  it('never overtakes an in-flight save', async () => {
    await loadReplyDraft(T);
    const c = composerFor(T);
    const put = hold('PUT');
    c.source.value = 'a';
    await wait(draftDelay);
    c.source.value = 'ab';
    pagehide();
    await tick();
    expect(writes()).toEqual(['PUT a']);
    expect(copy()).toEqual({ snapshot: { source: 'ab', reply_to: null }, base: 0, inflight: 1, deletes: 0 });
    // Nor does anything follow it when it is answered before the page goes.
    await land(put);
    await wait(draftDelay * 2);
    expect(writes()).toEqual(['PUT a']);
  });

  it('drops a request queued behind one in flight, and counts only what started', async () => {
    await loadReplyDraft(T);
    const c = composerFor(T);
    const put = hold('PUT');
    c.source.value = 'a';
    await wait(draftDelay);
    c.source.value = 'ab';
    await wait(draftDelay);
    c.source.value = 'abc';
    pagehide();
    await land(put);
    await wait(draftDelay * 2);
    expect(writes()).toEqual(['PUT a']);
    expect(copy()).toEqual({ snapshot: { source: 'abc', reply_to: null }, base: 0, inflight: 1, deletes: 0 });
    expect(await (async () => {
      newPage();
      await loadReplyDraft(T);
      return composerFor(T).source.value;
    })()).toBe('abc');
  });

  it('keeps a copy of a save it sends', async () => {
    await loadReplyDraft(T);
    composerFor(T).source.value = 'x';
    const put = hold('PUT');
    pagehide();
    expect(copy()).toEqual({ snapshot: { source: 'x', reply_to: null }, base: 0, inflight: 1, deletes: 0 });
    lose(put);
    newPage();
    await loadReplyDraft(T);
    await wait(draftDelay * 2);
    expect(composerFor(T).source.value).toBe('x');
    expect(writes()).toEqual([]);
    expect(copy()).toBeNull();
  });

  it('keeps nothing once the save it sent is answered', async () => {
    await loadReplyDraft(T);
    composerFor(T).source.value = 'x';
    pagehide();
    await tick();
    expect(copy()).toBeNull();
  });

  it('keeps a source of 64 KiB or more locally', async () => {
    await loadReplyDraft(T);
    const big = 'y'.repeat(keepaliveLimit);
    composerFor(T).source.value = big;
    pagehide();
    expect(writes()).toEqual([]);
    expect(copy()).toEqual({ snapshot: { source: big, reply_to: null }, base: 0, inflight: 0, deletes: 0 });
  });

  it('writes nothing for a draft already saved', async () => {
    await loadReplyDraft(T);
    composerFor(T).source.value = 'x';
    await wait(draftDelay);
    pagehide();
    expect(writes()).toEqual(['PUT x']);
    expect(copy()).toBeNull();
  });
});

// AC10: the next load in this browser, ordered by the server's revision.
describe('the local copy', () => {
  const restoredText = async () => {
    newPage();
    await loadReplyDraft(T);
    await tick();
    return composerFor(T).source.value;
  };

  describe('the revision orders the local copy', () => {
    // The client's clock disagrees with the server's updated_at every way it
    // can; nothing here reads it.
    beforeEach(() => vi.setSystemTime(new Date('1999-01-01T00:00:00Z')));

    it('a load answering base + inflight restores the copy, sends it, and removes it', async () => {
      server = { revision: 1, live: { source: 'a', reply_to: null } };
      store({ source: 'ab', reply_to: 4 }, 0, 1, 0);
      expect(await restoredText()).toBe('ab');
      expect(composerFor(T).replyTo.value).toBe(4);
      expect(writes()).toEqual(['PUT ab']);
      expect(copy()).toBeNull();
      expect(server.live).toEqual({ source: 'ab', reply_to: 4 });
    });

    it('a load answering more than that keeps the server’s draft and removes the copy', async () => {
      server = { revision: 2, live: { source: 'from the phone', reply_to: null } };
      store({ source: 'ab', reply_to: null }, 0, 1, 0);
      expect(await restoredText()).toBe('from the phone');
      await wait(draftDelay);
      expect(writes()).toEqual([]);
      expect(copy()).toBeNull();
    });

    it('a copy whose base is null loses to a 200', async () => {
      server = { revision: 1, live: { source: 'server', reply_to: null } };
      store({ source: 'local', reply_to: null }, null, 1, 0);
      expect(await restoredText()).toBe('server');
      expect(copy()).toBeNull();
    });

    it('nothing in state/drafts.ts reads the clock to decide', () => {
      expect(draftsSource).not.toMatch(/Date\.now|new Date|performance\.now|updated_at/);
    });
  });

  it('an unacknowledged first draft comes back after a 204', async () => {
    await loadReplyDraft(T);
    composerFor(T).source.value = 'first words';
    const put = hold('PUT');
    pagehide();
    await fail(put);
    expect(await restoredText()).toBe('first words');
    await tick();
    expect(writes()).toEqual(['PUT first words']);
    expect(copy()).toBeNull();
  });

  it('an unacknowledged first draft comes back after a 204 on an old tombstone', async () => {
    server = { revision: 7, live: null };
    await loadReplyDraft(T);
    composerFor(T).source.value = 'first words';
    const put = hold('PUT');
    pagehide();
    expect(copy().base).toBe(7);
    newPage();
    await fail(put);
    expect(await restoredText()).toBe('first words');
  });

  it('a draft posted on another device does not come back after a 204', async () => {
    server = { revision: 5, live: null };
    store({ source: 'posted elsewhere', reply_to: null }, 4, 1, 0);
    expect(await restoredText()).toBe('');
    expect(copy()).toBeNull();
    await wait(draftDelay);
    expect(writes()).toEqual([]);
  });

  it('a reply posted elsewhere without a saved draft leaves the copy', async () => {
    server = { revision: 0, live: null };
    store({ source: 'never on the server', reply_to: null }, 0, 1, 0);
    expect(await restoredText()).toBe('never on the server');
  });

  it('this tab’s own delete in flight does not discard its copy', async () => {
    server = { revision: 3, live: null };
    store({ source: 'typed after the delete', reply_to: null }, 2, 1, 1);
    expect(await restoredText()).toBe('typed after the delete');
  });

  it('another device’s delete landing first still restores the text typed after this tab’s delete', async () => {
    server = { revision: 4, live: { source: 'old', reply_to: null } };
    await loadReplyDraft(T);
    const c = composerFor(T);
    expect(c.source.value).toBe('old');
    const del = hold('DELETE');
    c.source.value = '';
    await wait(draftDelay);
    expect(writes()).toEqual(['DELETE']);
    c.source.value = 'new text';
    pagehide();
    expect(copy()).toEqual({ snapshot: { source: 'new text', reply_to: null }, base: 4, inflight: 1, deletes: 1 });
    // The other device's delete lands first, and this tab's on its tombstone.
    apply({ method: 'DELETE', url, body: undefined, keepalive: false });
    lose(del);
    expect(server.revision).toBe(5);
    const before = calls.filter((c) => c.body?.source === 'new text');
    expect(before).toEqual([]);
    expect(await restoredText()).toBe('new text');
  });

  // Terva review 1580, finding 1: pagehide drops the DELETE a post queued
  // behind a save in flight, so the empty copy stands in for it.
  it('a post while a save is in flight is deleted by the next load when the page goes', async () => {
    await loadReplyDraft(T);
    const c = composerFor(T);
    const put = hold('PUT');
    c.source.value = 'the reply';
    await wait(draftDelay);
    expect(await postReply(T)).not.toBeNull();
    await tick();
    pagehide();
    expect(writes()).toEqual(['PUT the reply']);
    expect(copy()).toEqual({ snapshot: { source: '', reply_to: null }, base: 0, inflight: 1, deletes: 0 });
    // The save lands after the post, and its answer is lost with the page.
    lose(put);
    expect(server.live).toEqual({ source: 'the reply', reply_to: null });
    expect(await restoredText()).toBe('');
    await tick();
    expect(writes()).toEqual(['DELETE']);
    expect(server.live).toBeNull();
    expect(copy()).toBeNull();
    expect(await restoredText()).toBe('');
  });

  it('a draft saved elsewhere after this tab’s post is kept', async () => {
    await loadReplyDraft(T);
    const c = composerFor(T);
    const put = hold('PUT');
    c.source.value = 'the reply';
    await wait(draftDelay);
    await postReply(T);
    await tick();
    pagehide();
    lose(put);
    apply({ method: 'PUT', url, body: { source: 'from the phone', reply_to: null }, keepalive: false });
    expect(await restoredText()).toBe('from the phone');
    await wait(draftDelay);
    expect(writes()).toEqual([]);
    expect(copy()).toBeNull();
  });

  it('blanking while a save is in flight is deleted by the next load when the page goes', async () => {
    await loadReplyDraft(T);
    const c = composerFor(T);
    const put = hold('PUT');
    c.source.value = 'a';
    await wait(draftDelay);
    c.source.value = '';
    await wait(draftDelay);
    pagehide();
    lose(put);
    expect(await restoredText()).toBe('');
    await tick();
    expect(writes()).toEqual(['DELETE']);
    expect(server.live).toBeNull();
  });

  it('a DELETE sent on pagehide keeps an empty copy until it is answered', async () => {
    await loadReplyDraft(T);
    const c = composerFor(T);
    c.source.value = 'a';
    await wait(draftDelay);
    c.source.value = '';
    const del = hold('DELETE');
    pagehide();
    expect(writes()).toEqual(['PUT a', 'DELETE']);
    expect(copy()).toEqual({ snapshot: { source: '', reply_to: null }, base: 1, inflight: 1, deletes: 1 });
    await land(del);
    expect(copy()).toBeNull();
  });

  it('an empty copy that cannot be ordered is dropped', async () => {
    server = { revision: 0, live: null };
    store({ source: '', reply_to: null }, null, 1, 0);
    expect(await restoredText()).toBe('');
    await tick();
    expect(draftNote(key)).toBe('');
    expect(copy()).toBeNull();
  });

  it('a delete elsewhere after a save of the copy’s text discards it', async () => {
    server = { revision: 6, live: null };
    store({ source: 'saved then posted', reply_to: null }, 4, 1, 0);
    expect(await restoredText()).toBe('');
    expect(copy()).toBeNull();
  });

  it('a copy with no base is restored on a 204 and says so', async () => {
    refuse = (c) => (c.method === 'GET' ? problem('internal', 500) : undefined);
    await loadReplyDraft(T);
    const put = hold('PUT');
    composerFor(T).source.value = 'only here';
    await wait(draftDelay);
    await fail(put);
    hold('PUT');
    pagehide();
    expect(copy()).toEqual({ snapshot: { source: 'only here', reply_to: null }, base: null, inflight: 2, deletes: 0 });
    refuse = () => undefined;
    expect(await restoredText()).toBe('only here');
    await tick();
    expect(draftNote(key)).toBe('Restored from this browser');
    expect(server.live).toEqual({ source: 'only here', reply_to: null });
    composerFor(T).source.value = 'only here, edited';
    expect(draftNote(key)).toBe('Saving…');
  });

  it('a copy fills only an empty composer', async () => {
    store({ source: 'local', reply_to: null }, 0, 1, 0);
    newPage();
    const get = hold('GET');
    const loading = loadReplyDraft(T);
    composerFor(T).source.value = 'typed meanwhile';
    await land(get);
    await loading;
    await wait(draftDelay);
    expect(composerFor(T).source.value).toBe('typed meanwhile');
    expect(writes()).toEqual(['PUT typed meanwhile']);
  });
});

// AC8.
describe('whose copies are kept', () => {
  it('a load clears copies that are not the viewer’s', () => {
    localStorage.setItem('ketju.draft.bob.reply:t2', '{}');
    localStorage.setItem('ketju.draft.alice.reply:t2', '{}');
    localStorage.setItem('ketju.theme', 'dark');
    const stop = startDrafts();
    expect(localStorage.getItem('ketju.draft.bob.reply:t2')).toBeNull();
    expect(localStorage.getItem('ketju.draft.alice.reply:t2')).toBe('{}');
    viewer.value = anon as Me;
    expect(localStorage.getItem('ketju.draft.alice.reply:t2')).toBeNull();
    expect(localStorage.getItem('ketju.theme')).toBe('dark');
    stop();
  });

  it('nobody signed in keeps none', () => {
    viewer.value = anon as Me;
    localStorage.setItem('ketju.draft.alice.reply:t2', '{}');
    const stop = startDrafts();
    expect(localStorage.getItem('ketju.draft.alice.reply:t2')).toBeNull();
    stop();
  });

  it('an owner change drops pending saves', async () => {
    await loadReplyDraft(T);
    composerFor(T).source.value = 'alice’s words';
    viewer.value = { ...(member as Me), user: { ...(member as Me).user!, handle: 'bob' } };
    composerFor(T);
    await wait(draftDelay * 2);
    pagehide();
    expect(writes()).toEqual([]);
    expect(copy()).toBeNull();
  });

  it('a viewer change drops pending saves before any composer is asked for', async () => {
    const stop = startDrafts();
    await loadReplyDraft(T);
    composerFor(T).source.value = 'alice’s words';
    viewer.value = anon as Me;
    await wait(draftDelay * 2);
    pagehide();
    expect(writes()).toEqual([]);
    stop();
  });
});
