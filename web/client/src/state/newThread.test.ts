import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { resetApiCache } from '../api/client';
import type { Me } from '../api/dto';
import { draftDelay } from './drafts';
import { canDiscard, createThread, discardNewThread, loadNewThreadDraft, newThreadFor, newThreadNote, refusedHere, resetNewThreads } from './newThread';
import { viewer } from './viewer';
import member from '../../../../internal/dto/testdata/golden/me.member.json';

// The new-thread composer's state and its autosave (TKT-01M3NS16VH), over
// state/drafts.ts from TKT-01M3NS1931, against a fake server that keeps the
// forum draft routes' rules (docs/07 "Drafts"), a stubbed fetch and fake
// timers. drafts.test.ts holds the autosave engine's own rules; these hold
// that a forum draft, title and all, is kept by them.

const F = 'events';
const url = `/api/v1/forums/${F}/draft`;
const create = `/api/v1/forums/${F}/threads`;
const copyKey = `ketju.draft.alice.new:${F}`;

interface Call {
  method: string;
  url: string;
  body: { title?: string; source?: string } | undefined;
}

type Live = { title: string; source: string };
let server: { revision: number; live: Live | null };
let calls: Call[];
let refuse: (c: Call) => Response | undefined;
let held: { call?: Call; resolve?: (r: Response) => void } | null;

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
const none = () => new Response(null, { status: 204, headers: { 'Draft-Revision': String(server.revision) } });
const problem = (type: string, status: number) =>
  new Response(JSON.stringify({ type, title: type, status }), { status, headers: { 'Content-Type': 'application/problem+json' } });
const draft = () => json({ forum: F, ...server.live, updated_at: 0, revision: server.revision });

function apply(c: Call): Response {
  if (c.url === create && c.method === 'POST') {
    return json({ thread: { id: 'newid00000001', slug: 'a-new-thread', title: c.body!.title }, post: { ordinal: 1 } }, 201);
  }
  if (c.url !== url) return problem('not-found', 404);
  switch (c.method) {
    case 'GET':
      return server.live ? draft() : none();
    case 'PUT':
      server.revision++;
      server.live = { title: c.body!.title ?? '', source: c.body!.source! };
      return draft();
    default:
      if (server.live) {
        server.revision++;
        server.live = null;
      }
      return none();
  }
}

function fakeFetch(u: string, init: RequestInit = {}): Promise<Response> {
  const c: Call = { method: init.method ?? 'GET', url: u, body: init.body ? JSON.parse(init.body as string) : undefined };
  calls.push(c);
  if (held && !held.call && c.method === 'GET') {
    const h = held;
    h.call = c;
    return new Promise((resolve) => (h.resolve = resolve));
  }
  return Promise.resolve(refuse(c) ?? apply(c));
}

async function tick() {
  for (let i = 0; i < 10; i++) await vi.advanceTimersByTimeAsync(0);
}
const wait = (ms: number) => vi.advanceTimersByTimeAsync(ms).then(tick);
const writes = () =>
  calls.filter((c) => c.url === url && c.method !== 'GET').map((c) => (c.method === 'PUT' ? `PUT ${c.body!.title}|${c.body!.source}` : 'DELETE'));
const posts = () => calls.filter((c) => c.url === create);
const store = (snapshot: Live, base: number | null, inflight: number, deletes: number) =>
  localStorage.setItem(copyKey, JSON.stringify({ snapshot, base, inflight, deletes }));
const pagehide = () => dispatchEvent(new Event('pagehide'));

// newPage is this browser loading the page again: memory is gone, and
// localStorage is kept.
function newPage() {
  resetNewThreads();
  calls = [];
}

const saved = viewer.value;
beforeEach(() => {
  vi.useFakeTimers();
  viewer.value = member as Me;
  resetNewThreads();
  resetApiCache();
  localStorage.clear();
  server = { revision: 0, live: null };
  calls = [];
  held = null;
  refuse = () => undefined;
  vi.stubGlobal('fetch', fakeFetch);
});
afterEach(() => {
  resetNewThreads();
  viewer.value = saved;
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('loading the draft', () => {
  it('fills an empty form with the title and the body', async () => {
    server = { revision: 3, live: { title: 'Bonfire', source: 'Friday at eight.' } };
    await loadNewThreadDraft(F);
    const n = newThreadFor(F);
    expect(n.title.value).toBe('Bonfire');
    expect(n.source.value).toBe('Friday at eight.');
    expect(n.caret.value).toBe('Friday at eight.'.length);
    await wait(draftDelay * 2);
    expect(writes()).toEqual([]);
  });

  it('never replaces what was typed, which is saved over it', async () => {
    server = { revision: 3, live: { title: 'Old', source: 'old text' } };
    held = {};
    const loading = loadNewThreadDraft(F);
    const n = newThreadFor(F);
    n.title.value = 'Typed';
    held.resolve!(apply(held.call!));
    await loading;
    expect(n.title.value).toBe('Typed');
    expect(n.source.value).toBe('');
    await wait(draftDelay);
    expect(writes()).toEqual(['PUT Typed|']);
  });

  it('asks once per forum per session', async () => {
    await loadNewThreadDraft(F);
    await loadNewThreadDraft(F);
    expect(calls.filter((c) => c.method === 'GET')).toHaveLength(1);
  });
});

// Criterion 6: the key is disarmed until its GET answers.
describe('before the draft has loaded', () => {
  it('an empty form never deletes a forum draft before it loads', async () => {
    server = { revision: 1, live: { title: 'Kept', source: 'kept' } };
    held = {};
    const loading = loadNewThreadDraft(F);
    const n = newThreadFor(F);
    n.title.value = 'x';
    n.title.value = '';
    await wait(3000);
    expect(calls.map((c) => c.method)).toEqual(['GET']);
    held.resolve!(apply(held.call!));
    await loading;
    expect(n.title.value).toBe('Kept');
    expect(n.source.value).toBe('kept');
    await wait(draftDelay * 2);
    expect(writes()).toEqual([]);
    expect(server.live).toEqual({ title: 'Kept', source: 'kept' });
  });

  it('a failed load never deletes', async () => {
    server = { revision: 1, live: { title: 'Kept', source: 'kept' } };
    refuse = (c) => (c.method === 'GET' ? problem('internal', 500) : undefined);
    await loadNewThreadDraft(F);
    const n = newThreadFor(F);
    n.title.value = 'x';
    n.title.value = '';
    await wait(draftDelay * 2);
    expect(writes()).toEqual([]);
    expect(server.live).toEqual({ title: 'Kept', source: 'kept' });
  });
});

describe('saving', () => {
  it('saves the title with the body, and clearing both deletes', async () => {
    await loadNewThreadDraft(F);
    const n = newThreadFor(F);
    n.title.value = 'A title';
    await wait(draftDelay);
    n.source.value = 'Some text';
    await wait(draftDelay);
    expect(server.live).toEqual({ title: 'A title', source: 'Some text' });
    expect(newThreadNote(F)).toBe('Draft saved');
    n.title.value = '';
    n.source.value = '';
    await wait(draftDelay);
    expect(writes()).toEqual(['PUT A title|', 'PUT A title|Some text', 'DELETE']);
    expect(server.live).toBeNull();
  });

  // A refused save turns autosave off and says so; Post is not the draft
  // route's to turn off (criterion 9).
  it.each([
    ['forbidden', 403],
    ['conflict', 409],
  ])('a %s save turns autosave off, says so, and leaves Post alone', async (type, status) => {
    refuse = (c) => (c.method === 'PUT' ? problem(type, status) : undefined);
    await loadNewThreadDraft(F);
    const n = newThreadFor(F);
    n.title.value = 'T';
    n.source.value = 'body';
    await wait(draftDelay);
    expect(newThreadNote(F)).toBe('Draft not saved');
    expect(refusedHere(n)).toBe(false);
    n.source.value = 'more';
    await wait(draftDelay * 2);
    expect(writes()).toEqual(['PUT T|body']);
    refuse = () => undefined;
    expect(await createThread(F)).not.toBeNull();
  });
});

describe('creating the thread', () => {
  it('posts once on a double click, deletes the draft, and clears the form', async () => {
    await loadNewThreadDraft(F);
    const n = newThreadFor(F);
    n.title.value = 'Bonfire';
    n.source.value = 'Friday.';
    await wait(draftDelay);
    const [a, b] = await Promise.all([createThread(F), createThread(F)]);
    expect(a?.thread.id).toBe('newid00000001');
    expect(b).toBeNull();
    expect(posts()).toHaveLength(1);
    expect(posts()[0].body).toEqual({ title: 'Bonfire', source: 'Friday.' });
    await tick();
    expect(writes()).toEqual(['PUT Bonfire|Friday.', 'DELETE']);
    expect(server.live).toBeNull();
    expect(n.title.value).toBe('');
    expect(n.source.value).toBe('');
    await wait(draftDelay * 2);
    expect(writes()).toEqual(['PUT Bonfire|Friday.', 'DELETE']);
  });

  it('asks nothing with a blank title or body', async () => {
    const n = newThreadFor(F);
    n.title.value = '   ';
    n.source.value = 'text';
    expect(await createThread(F)).toBeNull();
    n.title.value = 'T';
    n.source.value = ' ';
    expect(await createThread(F)).toBeNull();
    expect(posts()).toEqual([]);
  });

  it.each([
    ['forbidden', 403, true],
    ['conflict', 409, true],
    ['validation-failed', 422, false],
    ['payload-too-large', 413, false],
    ['rate-limited', 429, false],
  ])('a %s refusal keeps the text, and turns Post off: %s', async (type, status, off) => {
    refuse = (c) => (c.url === create ? problem(type, status) : undefined);
    await loadNewThreadDraft(F);
    const n = newThreadFor(F);
    n.title.value = 'T';
    n.source.value = 'body';
    expect(await createThread(F)).toBeNull();
    expect(n.title.value).toBe('T');
    expect(n.source.value).toBe('body');
    expect(refusedHere(n)).toBe(off);
    await wait(draftDelay);
    expect(writes()).toEqual(['PUT T|body']);
  });

  // Terva review 1591, finding 1.
  it('discard does nothing before the draft loads, or to an empty form', async () => {
    server = { revision: 2, live: { title: 'Unseen', source: 'not shown yet' } };
    held = {};
    const loading = loadNewThreadDraft(F);
    const n = newThreadFor(F);
    expect(canDiscard(n)).toBe(false);
    discardNewThread(F);
    n.title.value = 'typed early';
    expect(canDiscard(n)).toBe(false);
    discardNewThread(F);
    expect(n.title.value).toBe('typed early');
    n.title.value = '';
    // A second visit while the GET is out does not count as loaded.
    await loadNewThreadDraft(F);
    expect(n.loaded.value).toBe(false);
    held.resolve!(apply(held.call!));
    await loading;
    expect(n.loaded.value).toBe(true);
    expect(n.title.value).toBe('Unseen');
    await tick();
    expect(writes()).toEqual([]);
    expect(server.live).toEqual({ title: 'Unseen', source: 'not shown yet' });

    // Once shown, it can be discarded; an empty form then sends nothing.
    discardNewThread(F);
    await tick();
    expect(writes()).toEqual(['DELETE']);
    discardNewThread(F);
    await tick();
    expect(writes()).toEqual(['DELETE']);
  });

  // Terva review 1591, finding 2.
  it('a create answered after the viewer changed drops nothing and answers null', async () => {
    let answer!: (r: Response) => void;
    await loadNewThreadDraft(F);
    const n = newThreadFor(F);
    n.title.value = 'T';
    n.source.value = 'alice’s';
    const base = fakeFetch;
    vi.stubGlobal('fetch', (u: string, init: RequestInit = {}) =>
      u === create ? (calls.push({ method: 'POST', url: u, body: undefined }), new Promise<Response>((r) => (answer = r))) : base(u, init),
    );
    const creating = createThread(F);
    await tick();
    // Someone else signs in in this tab and opens the same forum, with a
    // draft of their own.
    viewer.value = { ...(member as Me), user: { ...(member as Me).user!, handle: 'bob' } };
    server = { revision: 7, live: { title: 'Bob’s', source: 'draft' } };
    await loadNewThreadDraft(F);
    expect(newThreadFor(F).title.value).toBe('Bob’s');
    answer(apply({ method: 'POST', url: create, body: { title: 'T', source: 'alice’s' } }));
    expect(await creating).toBeNull();
    await tick();
    expect(writes()).toEqual([]);
    expect(server.live).toEqual({ title: 'Bob’s', source: 'draft' });
  });

  it('discard deletes the draft and forgets a refusal', async () => {
    refuse = (c) => (c.url === create ? problem('conflict', 409) : undefined);
    await loadNewThreadDraft(F);
    const n = newThreadFor(F);
    n.title.value = 'T';
    n.source.value = 'body';
    await wait(draftDelay);
    await createThread(F);
    discardNewThread(F);
    await tick();
    expect(n.failure.value).toBeNull();
    expect(n.title.value).toBe('');
    expect(writes()).toEqual(['PUT T|body', 'DELETE']);
  });
});

// Criterion 10: this browser's copy of a forum draft.
describe('the local copy', () => {
  it('creating the thread removes the local copy as well as the server draft', async () => {
    await loadNewThreadDraft(F);
    const n = newThreadFor(F);
    n.title.value = 'T';
    n.source.value = 'body';
    pagehide();
    expect(localStorage.getItem(copyKey)).not.toBeNull();
    await tick();
    await createThread(F);
    await tick();
    expect(localStorage.getItem(copyKey)).toBeNull();
    expect(server.live).toBeNull();

    // Nothing comes back on the next load.
    newPage();
    await loadNewThreadDraft(F);
    expect(newThreadFor(F).title.value).toBe('');
  });

  it('a copy wins when only its own save came between', async () => {
    // The tab heard revision 2, then sent one save, which landed as 3, and
    // the page went before it heard back; the copy holds what came after.
    server = { revision: 3, live: { title: 'T', source: 'sent' } };
    store({ title: 'T', source: 'sent and more' }, 2, 1, 0);
    await loadNewThreadDraft(F);
    const n = newThreadFor(F);
    expect(n.source.value).toBe('sent and more');
    await wait(draftDelay);
    expect(server.live).toEqual({ title: 'T', source: 'sent and more' });
    expect(localStorage.getItem(copyKey)).toBeNull();
  });

  it('a copy loses to a save made elsewhere since', async () => {
    server = { revision: 4, live: { title: 'Elsewhere', source: 'newer' } };
    store({ title: 'T', source: 'older' }, 2, 1, 0);
    await loadNewThreadDraft(F);
    expect(newThreadFor(F).title.value).toBe('Elsewhere');
    await wait(draftDelay);
    expect(writes()).toEqual([]);
  });

  it('a copy does not come back once the thread was posted on another device', async () => {
    // The other device's post deleted the draft: a 204 past the copy's base.
    server = { revision: 5, live: null };
    store({ title: 'T', source: 'posted elsewhere' }, 3, 0, 0);
    await loadNewThreadDraft(F);
    expect(newThreadFor(F).source.value).toBe('');
    await wait(draftDelay);
    expect(writes()).toEqual([]);
  });

  it('an unacknowledged first draft comes back on a 204, with a notice', async () => {
    store({ title: 'First', source: 'never answered' }, null, 1, 0);
    await loadNewThreadDraft(F);
    const n = newThreadFor(F);
    expect(n.title.value).toBe('First');
    expect(newThreadNote(F)).toBe('Restored from this browser');
  });
});
