import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { apiGet, peekGet, resetApiCache } from '../api/client';
import { asking, batchMs, dwellMs, flush, markUnread, readUrl, resetReadTracker, trackPosts } from './readTracker';

// The read-tracking beacon (TKT-01M29CGBWN) against a stubbed
// IntersectionObserver, a stubbed fetch and fake timers.

// FakeObserver stands in for IntersectionObserver: a test says which posts
// are on screen, and it reports them as the browser would.
class FakeObserver {
  static all: FakeObserver[] = [];
  observed = new Set<Element>();
  constructor(readonly callback: IntersectionObserverCallback) {
    FakeObserver.all.push(this);
  }
  observe(el: Element) {
    this.observed.add(el);
  }
  unobserve(el: Element) {
    this.observed.delete(el);
  }
  disconnect() {
    this.observed.clear();
  }
  // show puts el at the foot of the viewport with shown px of its height px
  // in view, and reports it.
  show(el: Element, shown: number, height = 200) {
    place(el, shown, height);
    const entry = {
      target: el,
      isIntersecting: shown > 0,
      intersectionRatio: shown / height,
      intersectionRect: { height: shown },
      boundingClientRect: { height },
      rootBounds: { height: innerHeight },
    } as unknown as IntersectionObserverEntry;
    this.callback([entry], this as unknown as IntersectionObserver);
  }
}

// place moves el to the foot of the viewport with shown px of it in view,
// without the observer reporting it: a move between two thresholds.
function place(el: Element, shown: number, height = 200) {
  const top = innerHeight - shown;
  el.getBoundingClientRect = () => ({ top, bottom: top + height, height }) as DOMRect;
}

interface Sent {
  url: string;
  method: string;
  keepalive: boolean | undefined;
  body: { ordinal?: number } | undefined;
}

// answer decides the response to each request; the default stores what was
// sent, as the server does for a position above the stored one.
let answer: (s: Sent) => Promise<Response> | Response;
let sent: Sent[];
const stored = (n: number) =>
  new Response(JSON.stringify({ last_read_ordinal: n }), { status: 200, headers: { 'Content-Type': 'application/json' } });

// list builds a thread page's post list: canonical posts 1..n as direct
// children with ids and ordinals, the unread divider among them, and a reply
// expander's copy of post 9 inside post 1, as ThreadScreen renders them.
function list(n: number): HTMLElement {
  const section = document.createElement('section');
  section.className = 'posts';
  for (let o = 1; o <= n; o++) {
    const a = document.createElement('article');
    a.id = `post-${o}`;
    a.setAttribute('data-ordinal', String(o));
    section.append(a);
  }
  const divider = document.createElement('div');
  divider.id = 'unread';
  section.insertBefore(divider, section.children[4]);
  const copy = document.createElement('article');
  copy.setAttribute('data-reply', '9');
  section.querySelector('#post-1')!.append(copy);
  document.body.replaceChildren(section);
  return section;
}

const post = (o: number) => document.getElementById(`post-${o}`)!;
const io = () => FakeObserver.all.at(-1)!;
const puts = () => sent.filter((s) => s.method === 'PUT');
// drain lets settled requests' handlers run under fake timers.
const drain = async () => {
  for (let i = 0; i < 5; i++) await Promise.resolve();
  await vi.advanceTimersByTimeAsync(0);
};

let visibility: DocumentVisibilityState = 'visible';
beforeEach(() => {
  vi.useFakeTimers();
  FakeObserver.all = [];
  sent = [];
  visibility = 'visible';
  answer = (s) => stored(s.body?.ordinal ?? 0);
  vi.stubGlobal('IntersectionObserver', FakeObserver);
  vi.spyOn(document, 'visibilityState', 'get').mockImplementation(() => visibility);
  vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
    const s: Sent = {
      url,
      method: init.method ?? 'GET',
      keepalive: init.keepalive,
      body: init.body ? JSON.parse(String(init.body)) : undefined,
    };
    sent.push(s);
    return answer(s);
  });
  resetReadTracker();
  resetApiCache();
});
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const hide = () => {
  visibility = 'hidden';
  document.dispatchEvent(new Event('visibilitychange'));
};

describe('what counts as read', () => {
  // AC1.
  it('does not count a post scrolled past in under a second', async () => {
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(7), 200);
    vi.advanceTimersByTime(dwellMs - 100);
    io().show(post(7), 0);
    vi.advanceTimersByTime(batchMs);
    await drain();
    expect(puts()).toEqual([]);
    stop();
    await drain();
    expect(puts()).toEqual([]);
  });

  it('counts a post held on screen for a second', async () => {
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(3), 200);
    vi.advanceTimersByTime(dwellMs + 100);
    vi.advanceTimersByTime(batchMs);
    await drain();
    expect(puts()).toEqual([{ url: readUrl('t1'), method: 'PUT', keepalive: true, body: { ordinal: 3 } }]);
    stop();
  });

  it('counts a post half on screen, and one taller than two screens that fills half of it', async () => {
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(2), 99, 200); // under half: not on screen
    io().show(post(4), innerHeight / 2, innerHeight * 4); // an eighth of it, but half the screen
    vi.advanceTimersByTime(dwellMs);
    await flush();
    expect(puts().map((s) => s.body)).toEqual([{ ordinal: 4 }]);
    stop();
  });

  // Reviews 1143 and 1144: a tall post can cross the half-screen line
  // between two of the observer's thresholds, with no report.
  const tall = innerHeight * 4;
  const over = innerHeight / 2 + 20;
  const under = innerHeight / 2 - 20;
  const scrolled = () => dispatchEvent(new Event('scroll'));

  it('does not count a tall post that left the line without the observer saying so', async () => {
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(4), over, tall);
    vi.advanceTimersByTime(dwellMs / 2);
    // Moved by the layout, not a scroll: measured when the second is up.
    place(post(4), under, tall);
    vi.advanceTimersByTime(dwellMs / 2);
    await flush();
    expect(puts()).toEqual([]);
    // Scrolled back over the line: a full second from then counts it.
    place(post(4), over, tall);
    scrolled();
    vi.advanceTimersByTime(dwellMs - 1);
    await flush();
    expect(puts()).toEqual([]);
    vi.advanceTimersByTime(1);
    await flush();
    expect(puts().map((s) => s.body)).toEqual([{ ordinal: 4 }]);
    stop();
  });

  it('starts the second over when a tall post dips below the line and back', async () => {
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(4), over, tall);
    vi.advanceTimersByTime(300);
    place(post(4), under, tall);
    scrolled();
    vi.advanceTimersByTime(100);
    place(post(4), over, tall);
    scrolled();
    // A second from the start, but not an unbroken one.
    vi.advanceTimersByTime(dwellMs - 400);
    await flush();
    expect(puts()).toEqual([]);
    vi.advanceTimersByTime(400);
    await flush();
    expect(puts().map((s) => s.body)).toEqual([{ ordinal: 4 }]);
    stop();
  });

  it('observes the canonical posts only, not the divider or a reply copy', () => {
    const stop = trackPosts('t1', list(10), 0);
    const ids = [...io().observed].map((el) => el.id);
    expect(ids).toEqual(['post-1', 'post-2', 'post-3', 'post-4', 'post-5', 'post-6', 'post-7', 'post-8', 'post-9', 'post-10']);
    stop();
  });

  it('does not count time while the page is hidden', async () => {
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(5), 200);
    vi.advanceTimersByTime(dwellMs - 200);
    hide();
    await drain();
    vi.advanceTimersByTime(dwellMs * 5);
    await flush();
    expect(puts()).toEqual([]);
    // Shown again, the post still on screen starts its second over.
    visibility = 'visible';
    document.dispatchEvent(new Event('visibilitychange'));
    vi.advanceTimersByTime(dwellMs - 200);
    await flush();
    expect(puts()).toEqual([]);
    vi.advanceTimersByTime(200);
    await flush();
    expect(puts().map((s) => s.body)).toEqual([{ ordinal: 5 }]);
    stop();
  });

  it('sends nothing for posts at or below the position the page reported', async () => {
    const stop = trackPosts('t1', list(10), 8);
    io().show(post(6), 200);
    io().show(post(8), 200);
    vi.advanceTimersByTime(dwellMs);
    await flush();
    expect(puts()).toEqual([]);
    stop();
  });
});

// AC2.
describe('batching', () => {
  it('sends the highest ordinal read in each batch, every 30 s', async () => {
    const stop = trackPosts('t1', list(10), 0);
    for (const o of [2, 3, 4]) io().show(post(o), 200);
    vi.advanceTimersByTime(dwellMs);
    expect(puts()).toEqual([]);
    await vi.advanceTimersByTimeAsync(batchMs - dwellMs);
    expect(puts().map((s) => s.body)).toEqual([{ ordinal: 4 }]);
    io().show(post(6), 200);
    await vi.advanceTimersByTimeAsync(batchMs);
    expect(puts().map((s) => s.body)).toEqual([{ ordinal: 4 }, { ordinal: 6 }]);
    stop();
  });

  it('never sends a position behind one already sent', async () => {
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(9), 200);
    vi.advanceTimersByTime(dwellMs);
    await flush();
    io().show(post(9), 0);
    io().show(post(5), 200);
    vi.advanceTimersByTime(dwellMs);
    await vi.advanceTimersByTimeAsync(batchMs * 2);
    stop();
    await drain();
    expect(puts().map((s) => s.body)).toEqual([{ ordinal: 9 }]);
  });

  it('nor one behind a position the server answered with', async () => {
    answer = () => stored(9);
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(3), 200);
    vi.advanceTimersByTime(dwellMs);
    await flush();
    io().show(post(7), 200);
    vi.advanceTimersByTime(dwellMs);
    await flush();
    expect(puts().map((s) => s.body)).toEqual([{ ordinal: 3 }]);
    stop();
  });

  it('nor one behind a position still in flight', async () => {
    let release!: () => void;
    answer = (s) => new Promise((r) => (release = () => r(stored(s.body!.ordinal!))));
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(8), 200);
    vi.advanceTimersByTime(dwellMs);
    void flush();
    io().show(post(4), 200);
    vi.advanceTimersByTime(dwellMs);
    void flush();
    release();
    await drain();
    expect(puts().map((s) => s.body)).toEqual([{ ordinal: 8 }]);
    stop();
  });

  it('sends a position the network lost again, and never a lower one in its place', async () => {
    let fail = true;
    answer = (s) => {
      if (fail) throw new TypeError('network down');
      return stored(s.body!.ordinal!);
    };
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(6), 200);
    vi.advanceTimersByTime(dwellMs);
    await flush();
    io().show(post(3), 200);
    vi.advanceTimersByTime(dwellMs);
    fail = false;
    await flush();
    expect(puts().map((s) => s.body)).toEqual([{ ordinal: 6 }, { ordinal: 6 }]);
    stop();
  });

  it('drops a position the server refuses for good', async () => {
    answer = () =>
      new Response(JSON.stringify({ type: 'not-found', title: 't', status: 404 }), {
        status: 404,
        headers: { 'Content-Type': 'application/problem+json' },
      });
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(6), 200);
    vi.advanceTimersByTime(dwellMs);
    await flush();
    await flush();
    expect(puts()).toHaveLength(1);
    stop();
  });
});

// AC4.
describe('flushing', () => {
  it('flushes on navigation, when the page stops being tracked', async () => {
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(3), 200);
    io().show(post(7), 200);
    vi.advanceTimersByTime(dwellMs);
    expect(puts()).toEqual([]);
    stop();
    await drain();
    expect(puts()).toEqual([{ url: readUrl('t1'), method: 'PUT', keepalive: true, body: { ordinal: 7 } }]);
    // And stops: with nothing left to send, the batch timer goes.
    await vi.advanceTimersByTimeAsync(batchMs * 2);
    expect(puts()).toHaveLength(1);
    expect(vi.getTimerCount()).toBe(0);
  });

  // Review 1143: the navigation's PUT fails once the reader has left every
  // thread, and is still sent.
  it('retries a lost position after the last page stops being tracked', async () => {
    let fail = true;
    answer = (s) => {
      if (fail) throw new TypeError('network down');
      return stored(s.body!.ordinal!);
    };
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(5), 200);
    vi.advanceTimersByTime(dwellMs);
    stop();
    await drain();
    expect(puts()).toHaveLength(1);
    fail = false;
    await vi.advanceTimersByTimeAsync(batchMs);
    expect(puts().map((s) => s.body)).toEqual([{ ordinal: 5 }, { ordinal: 5 }]);
    await vi.advanceTimersByTimeAsync(batchMs * 2);
    expect(puts()).toHaveLength(2);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('flushes on pagehide', async () => {
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(4), 200);
    vi.advanceTimersByTime(dwellMs);
    dispatchEvent(new Event('pagehide'));
    await drain();
    expect(puts()).toEqual([{ url: readUrl('t1'), method: 'PUT', keepalive: true, body: { ordinal: 4 } }]);
    stop();
  });

  it('flushes when the page is hidden', async () => {
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(5), 200);
    vi.advanceTimersByTime(dwellMs);
    hide();
    await drain();
    expect(puts()).toEqual([{ url: readUrl('t1'), method: 'PUT', keepalive: true, body: { ordinal: 5 } }]);
    stop();
  });

  it('forgets the held landing page once the position has moved', async () => {
    const landing = '/api/v1/threads/t1';
    const paged = '/api/v1/threads/t1?page=2';
    answer = (s) =>
      s.method === 'GET'
        ? new Response('{"n":1}', { status: 200, headers: { ETag: '"x"' } })
        : stored(s.body!.ordinal!);
    await apiGet(landing);
    await apiGet(paged);
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(5), 200);
    vi.advanceTimersByTime(dwellMs);
    await flush();
    expect(peekGet(landing)).toBeUndefined();
    expect(peekGet(paged)).toEqual({ n: 1 });
    stop();
  });
});

// docs/07 "Mark-unread then a late beacon".
describe('markUnread', () => {
  it('flushes and waits for every PUT before the DELETE, and sends nothing while it is out', async () => {
    const releases: (() => void)[] = [];
    answer = (s) =>
      new Promise((r) => releases.push(() => r(stored(s.method === 'DELETE' ? 2 : s.body!.ordinal!))));
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(6), 200);
    vi.advanceTimersByTime(dwellMs);
    const marked = markUnread('t1', 2);
    await drain();
    expect(sent.map((s) => s.method)).toEqual(['PUT']);
    releases.shift()!();
    await drain();
    expect(sent.map((s) => s.method)).toEqual(['PUT', 'DELETE']);
    expect(sent[1].body).toEqual({ ordinal: 2 });
    // Read while the DELETE is out: held back.
    io().show(post(8), 200);
    vi.advanceTimersByTime(dwellMs);
    void flush();
    await drain();
    expect(sent).toHaveLength(2);
    releases.shift()!();
    expect(await marked).toEqual({ last_read_ordinal: 2 });
    // The floor is the DELETE's answer now, so what is read after goes out.
    void flush();
    await drain();
    expect(sent.map((s) => [s.method, s.body])).toEqual([
      ['PUT', { ordinal: 6 }],
      ['DELETE', { ordinal: 2 }],
      ['PUT', { ordinal: 8 }],
    ]);
    releases.shift()!();
    stop();
  });

  // Review 1143: the thread is held from the call, not from when the PUTs
  // already out have answered, so none starts while markUnread waits.
  it('starts no PUT while it waits for one already out', async () => {
    const releases: (() => void)[] = [];
    answer = (s) =>
      new Promise((r) => releases.push(() => r(stored(s.method === 'DELETE' ? 2 : s.body!.ordinal!))));
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(4), 200);
    vi.advanceTimersByTime(dwellMs);
    void flush();
    await drain();
    expect(sent.map((s) => [s.method, s.body])).toEqual([['PUT', { ordinal: 4 }]]);
    const marked = markUnread('t1', 2);
    io().show(post(7), 200);
    vi.advanceTimersByTime(dwellMs);
    void flush();
    await drain();
    expect(sent).toHaveLength(1);
    releases.shift()!();
    await drain();
    expect(sent.map((s) => [s.method, s.body])).toEqual([
      ['PUT', { ordinal: 4 }],
      ['DELETE', { ordinal: 2 }],
    ]);
    releases.shift()!();
    expect(await marked).toEqual({ last_read_ordinal: 2 });
    // Read after the call: sent after the DELETE.
    void flush();
    await drain();
    expect(sent.slice(2).map((s) => [s.method, s.body])).toEqual([['PUT', { ordinal: 7 }]]);
    releases.shift()!();
    stop();
  });

  // Review 1148: a post read while the DELETE is out is at or below the
  // floor the DELETE is about to lower, and is kept until it answers.
  it('keeps what is read while the DELETE is out, and judges it by the answer', async () => {
    let release!: () => void;
    answer = (s) =>
      s.method === 'DELETE' ? new Promise((r) => (release = () => r(stored(2)))) : stored(s.body!.ordinal!);
    const stop = trackPosts('t1', list(10), 9);
    const marked = markUnread('t1', 2);
    await drain();
    expect(sent.map((s) => s.method)).toEqual(['DELETE']);
    io().show(post(5), 200);
    vi.advanceTimersByTime(dwellMs);
    await flush();
    expect(sent).toHaveLength(1);
    release();
    await marked;
    await flush();
    expect(sent.map((s) => [s.method, s.body])).toEqual([
      ['DELETE', { ordinal: 2 }],
      ['PUT', { ordinal: 5 }],
    ]);
    stop();
  });

  it('does not send after the DELETE a PUT from before the call that failed', async () => {
    let fail!: () => void;
    answer = (s) =>
      s.method === 'PUT' ? new Promise((_, reject) => (fail = () => reject(new TypeError('network down')))) : stored(2);
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(6), 200);
    vi.advanceTimersByTime(dwellMs);
    void flush();
    await drain();
    const marked = markUnread('t1', 2);
    fail();
    await marked;
    await flush();
    expect(sent.map((s) => [s.method, s.body])).toEqual([
      ['PUT', { ordinal: 6 }],
      ['DELETE', { ordinal: 2 }],
    ]);
    stop();
  });

  // Review 1144: two marks of one thread run in call order, so the later
  // one's DELETE is the one that lands last; what is read between the two
  // calls goes out between the two DELETEs.
  it('runs overlapping marks one at a time, in call order', async () => {
    const releases: (() => void)[] = [];
    answer = (s) => new Promise((r) => releases.push(() => r(stored(s.body!.ordinal!))));
    const stop = trackPosts('t1', list(10), 0);
    io().show(post(4), 200);
    vi.advanceTimersByTime(dwellMs);
    const first = markUnread('t1', 2);
    await drain();
    io().show(post(7), 200);
    vi.advanceTimersByTime(dwellMs);
    const second = markUnread('t1', 3);
    await drain();
    expect(sent.map((s) => [s.method, s.body])).toEqual([['PUT', { ordinal: 4 }]]);
    releases.shift()!();
    await drain();
    // The first DELETE is out; the second waits for its answer.
    expect(sent.slice(1).map((s) => [s.method, s.body])).toEqual([['DELETE', { ordinal: 2 }]]);
    releases.shift()!();
    expect(await first).toEqual({ last_read_ordinal: 2 });
    await drain();
    expect(sent.slice(2).map((s) => [s.method, s.body])).toEqual([['PUT', { ordinal: 7 }]]);
    releases.shift()!();
    await drain();
    expect(sent.slice(3).map((s) => [s.method, s.body])).toEqual([['DELETE', { ordinal: 3 }]]);
    releases.shift()!();
    expect(await second).toEqual({ last_read_ordinal: 3 });
    expect(sent).toHaveLength(4);
    stop();
  });

  // Reviews 1147 and 1148: a page asked for before the mark answered
  // reports the old position, and must not raise the floor back over what
  // the mark made unread; one asked for after it is taken as known.
  it('takes a page position only from a page asked for after the mark', async () => {
    const paged = '/api/v1/threads/t1?page=2';
    answer = (s) =>
      s.method === 'GET'
        ? new Response('{"n":1}', { status: 200, headers: { ETag: '"x"' } })
        : stored(s.method === 'DELETE' ? 2 : s.body!.ordinal!);
    const old = await asking(() => apiGet<object>(paged));
    trackPosts('t1', list(10), 9, old)();
    await markUnread('t1', 2);
    expect(peekGet(paged)).toBeUndefined();
    // The old page said 9; the server says 2.
    let stop = trackPosts('t1', list(10), 9, old);
    io().show(post(6), 200);
    vi.advanceTimersByTime(dwellMs);
    await flush();
    expect(puts().map((s) => s.body)).toEqual([{ ordinal: 6 }]);
    stop();
    // A fresh page says 9, from another tab's reading: believed.
    const fresh = await asking(() => apiGet<object>(paged));
    stop = trackPosts('t1', list(10), 9, fresh);
    io().show(post(8), 200);
    vi.advanceTimersByTime(dwellMs);
    await flush();
    expect(puts()).toHaveLength(1);
    stop();
  });

  it('sends no body to mark the whole thread unread', async () => {
    answer = () => stored(0);
    await markUnread('t1');
    expect(sent).toEqual([{ url: readUrl('t1'), method: 'DELETE', keepalive: undefined, body: undefined }]);
  });
});
