import { render } from 'preact';
import { act } from 'preact/test-utils';
import { vi } from 'vitest';
import { resetApiCache } from './api/client';
import type { Lens, LensGap, Post, ThreadPage } from './api/dto';
import { App } from './app';
import { resetThreadCache } from './state/threads';
import golden from '../../../internal/dto/testdata/golden/thread.member.json';

let mounted: HTMLElement | null = null;

// lazyChunks are the chunks routes.tsx loads lazily, by the addresses that
// load them. A test file's first import of one transforms and evaluates
// every module in it, and on a loaded machine that has outrun settle's 200
// turns, so the file's first test found no screen at all (TKT-01M3S8Y2NA:
// ApplicationDetail, FormEditor, Decision and the route table). renderAt
// awaits the chunk before it mounts anything, so the wait is on the chunk
// and not a clock, and settle's bound is left to the screen. The route's own
// lazy import still runs, and resolves to the module loaded here.
const lazyChunks: [RegExp, () => Promise<unknown>][] = [
  [/^\/admin(?:[/?#]|$)/, () => import('./screens/admin/AdminRoot')],
  [/^\/apply(?:[/?#]|$)/, () => import('./screens/apply/ApplyRoot')],
];

// preloadMovePosts loads the thread page's MovePosts chunk, which it loads
// itself the first time a moderator chooses Select posts (TKT-01M29CYSAM).
// A test that selects posts awaits it first, for the reason lazyChunks
// gives: the wait is then on the chunk, not on settle's bound.
export const preloadMovePosts = () => import('./components/thread/MovePosts');

// renderAt renders the whole app at url, as a reader arriving there would,
// and waits for the screen to settle: a lazy chunk, a fetch. The previous
// test's app is unmounted first; left mounted, it would go on following
// history and render into this test.
export async function renderAt(url: string): Promise<HTMLElement> {
  await lazyChunks.find(([path]) => path.test(url))?.[1]();
  if (mounted) {
    const prev = mounted;
    await act(async () => render(null, prev));
  }
  resetApiCache();
  resetThreadCache();
  FakeEventSource.all = [];
  vi.stubGlobal('EventSource', FakeEventSource);
  history.replaceState(null, '', url);
  const root = document.createElement('div');
  mounted = root;
  document.body.replaceChildren(root);
  await act(async () => {
    render(<App />, root);
  });
  await settle(root);
  return root;
}

// FakeEventSource stands in for EventSource, which happy-dom lacks; renderAt
// installs it, so a thread page's live-update stream (state/live.ts) opens
// one. Unless a test says otherwise (ready = false), it says ready once
// opened, as the server does, so a screen that is not about live updates
// does not fall back to polling. emit sends an event, fail an error.
export class FakeEventSource {
  static all: FakeEventSource[] = [];
  static ready = true;
  static open = () => FakeEventSource.all.filter((s) => !s.closed);
  onerror: (() => void) | null = null;
  closed = false;
  private on = new Map<string, ((e: MessageEvent) => void)[]>();
  constructor(readonly url: string) {
    FakeEventSource.all.push(this);
    if (FakeEventSource.ready) void Promise.resolve().then(() => this.emit('ready', {}));
  }
  addEventListener(type: string, fn: (e: MessageEvent) => void) {
    this.on.set(type, [...(this.on.get(type) ?? []), fn]);
  }
  emit(type: string, data: unknown) {
    if (this.closed) return;
    for (const fn of this.on.get(type) ?? []) fn(new MessageEvent(type, { data: JSON.stringify(data) }));
  }
  fail() {
    if (!this.closed) this.onerror?.();
  }
  close() {
    this.closed = true;
  }
}

// unmount takes the app renderAt mounted down. A test that changes the
// viewer back in afterEach calls it first: the app would otherwise render
// again for the restored viewer, and fetch after the stub is gone.
export async function unmount() {
  if (!mounted) return;
  const prev = mounted;
  mounted = null;
  await act(async () => render(null, prev));
}

// settle waits up to a second for a screen to appear, and returns as soon as
// one has. The first import of the lazy admin chunk in a test worker
// transforms every admin screen, and under a full parallel run that has
// taken longer than the quarter second this once allowed (TKT-01M29CXK1X),
// and then longer than the second; renderAt now loads a lazy chunk before
// it renders (lazyChunks, TKT-01M3S8Y2NA).
export async function settle(root: HTMLElement) {
  for (let i = 0; i < 200 && !root.querySelector('[data-screen]'); i++) {
    await act(() => new Promise((r) => setTimeout(r, 5)));
  }
  await act(() => new Promise((r) => setTimeout(r, 5)));
}

export const screenOf = (root: HTMLElement) =>
  root.querySelector('[data-screen]')?.getAttribute('data-screen') ?? null;

// pressRaw is a key pressed outside act, on whatever has focus, with what
// the browser would do with it by default, since a dispatched keydown does
// none of it: Enter clicks a focused button or submits a focused field's
// form, and a printable character is typed into a focused text field. It
// answers the element the key landed on. The focus-race cases
// (TKT-01M3V7CFJ5) press it one microtask after the press that opens
// something, before a plain effect would have moved focus.
export function pressRaw(key: string): Element | null {
  const target = document.activeElement;
  const e = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true });
  (target ?? document.body).dispatchEvent(e);
  if (e.defaultPrevented || !(target instanceof HTMLElement)) return target;
  if (key === 'Enter') {
    if (target instanceof HTMLButtonElement && !target.disabled) target.click();
    else if (target instanceof HTMLInputElement && target.form) target.form.requestSubmit();
  } else if (key.length === 1 && (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement)) {
    target.value += key;
    target.dispatchEvent(new Event('input', { bubbles: true }));
  }
  return target;
}

// raceSettle lets the frame a focus-race case raced run, before the next
// case, outside the case's own assertions.
export const raceSettle = () => act(() => new Promise((r) => setTimeout(r, 50)));

// route navigates in the same document, as a link click does: preact-iso
// follows pushState through its popstate listener.
export async function route(url: string) {
  await act(async () => {
    history.pushState(null, '', url);
    dispatchEvent(new PopStateEvent('popstate'));
  });
}

// threadPage is a full thread response, the member golden with the thread's
// id, slug and title replaced, as a test's stubbed server answers.
export function threadPage(thread: { id: string; slug: string; title: string }, over: Partial<ThreadPage> = {}): ThreadPage {
  const g = structuredClone(golden) as ThreadPage;
  return { ...g, ...over, thread: { ...g.thread, ...thread, ...over.thread } };
}

export const threadJSON = (thread: { id: string; slug: string; title: string }) => JSON.stringify(threadPage(thread));

// Answer is what a stubbed route answers: a status, a JSON body, and any
// headers beside the content type.
export type Answer = { status: number; body?: unknown; headers?: Record<string, string> };

export interface StubServer {
  routes: Record<string, (body: unknown) => Answer | Promise<Answer>>;
  sent: { method: string; url: string; body: unknown }[];
}

// stubServer answers fetch from routes, keyed "METHOD /url", and records
// every request. An error status is sent as problem+json; a route nobody
// set is not-found.
export function stubServer(routes: StubServer['routes']): StubServer {
  const s: StubServer = { routes, sent: [] };
  vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
    const method = init.method ?? 'GET';
    // A multipart body (an upload) is recorded as it was sent.
    const body = init.body instanceof FormData ? init.body : init.body ? JSON.parse(init.body as string) : undefined;
    s.sent.push({ method, url, body });
    const handler = s.routes[`${method} ${url}`];
    if (!handler) return new Response('{}', { status: 404, headers: { 'Content-Type': 'application/problem+json' } });
    const a = await handler(body);
    return new Response(a.body === undefined ? null : JSON.stringify(a.body), {
      status: a.status,
      headers: { 'Content-Type': a.status >= 400 ? 'application/problem+json' : 'application/json', ...a.headers },
    });
  });
  return s;
}

// problemAnswer is a problem of type at status, as the server writes one.
export const problemAnswer = (
  type: string,
  status: number,
  extra: Record<string, unknown> = {},
  headers?: Record<string, string>,
): Answer => ({ status, body: { type, title: type, status, ...extra }, headers });

// A thread served under every lens, for the lens tests (TKT-01M29CTY71):
// sixty posts, five to a page, carol's odd and alice's even, started by
// carol. The selections stand in for the server's (internal/service/threads
// lenses.go); what the client does with an answer is what is under test.

const lensId = '01k4z3j0w8xl1';
export const lensHome = `/t/${lensId}/a`;
const size = 5;
const count = 60;
const all = Array.from({ length: count }, (_, i) => i + 1);

export interface LensServed {
  // lastRead is the viewer's position as the server reports it.
  lastRead: number;
  moderator: boolean;
  // over changes a post as served.
  over: Record<number, Partial<Post>>;
  gets: string[];
  puts: number[];
}

export function lensPost(o: number, over: Partial<Post> = {}): Post {
  const handle = o % 2 ? 'carol' : 'alice';
  return {
    ...(golden as ThreadPage).posts[0],
    ordinal: o,
    author: { ...(golden as ThreadPage).posts[0].author, handle, display_name: handle },
    reply_to: null,
    reply_to_suppressed: false,
    reply_count: 0,
    deleted: false,
    removal: null,
    moved_to: null,
    html: `<p>post ${o}</p>`,
    ...over,
  };
}

// select is what a lens keeps, and the value the answer names; null for a
// lens the server does not know, which it serves as the full thread.
function select(raw: string, lastRead: number): { sel: number[]; kind: string; value: string } | null {
  const [kind, value = ''] = raw.split(':');
  switch (kind) {
    case 'unread': {
      const b = value === '' ? lastRead : Number(value);
      return { sel: all.filter((o) => o > b), kind, value: String(b) };
    }
    case 'author':
      return { sel: all.filter((o) => (o % 2 === 1) === (value === 'carol')), kind, value };
    case 'chain':
      return { sel: [1, Number(value), 55].filter((o, i, a) => a.indexOf(o) === i).sort((a, b) => a - b), kind, value };
    case 'since':
      return { sel: all.filter((o) => o > 40), kind, value: String(Date.UTC(2026, 8, 1)) };
    case 'mentions':
      return { sel: [12, 40], kind, value: '' };
    case 'mod':
      return { sel: [57], kind, value: '' };
  }
  return null;
}

// pageOf answers the thread endpoint's query q.
function pageOf(q: URLSearchParams, s: LensServed): ThreadPage {
  const raw = q.get('lens');
  const chosen = raw ? select(raw, s.lastRead) : null;
  const sel = chosen ? chosen.sel : all;
  const o = Number(q.get('ordinal') ?? 0);
  let n = Number(q.get('page') ?? 1);
  if (o) {
    const i = sel.findIndex((x) => x >= o);
    n = Math.floor((i < 0 ? sel.length - 1 : i) / size) + 1;
  }
  const on = sel.slice((n - 1) * size, n * size);
  let lens: Lens | null = null;
  if (chosen) {
    // Every gap that touches the page: before its first post, between its
    // posts, and after its last.
    const gaps: LensGap[] = [];
    const touches = new Set(on.length ? [sel[(n - 1) * size - 1] ?? 0, ...on] : []);
    let prev = 0;
    for (const x of [...sel, count + 1]) {
      if (x - prev > 1 && touches.has(prev)) gaps.push({ after: prev, count: x - prev - 1 });
      prev = x;
    }
    lens = { kind: chosen.kind, value: chosen.value, shown: sel.length, hidden: count - sel.length, gaps };
  }
  const base = golden as ThreadPage;
  return {
    ...base,
    thread: {
      ...base.thread,
      id: lensId,
      slug: 'a',
      title: 'Lenses',
      post_count: count,
      pinned: null,
      author: { ...base.thread.author, handle: 'carol', display_name: 'carol' },
    },
    page: { number: n, size, total_pages: Math.max(1, Math.ceil(sel.length / size)) },
    lens,
    viewer: { ...base.viewer, last_read_ordinal: s.lastRead, can_moderate: s.moderator, can_reply: false },
    posts: on.map((x) => lensPost(x, s.over[x])),
  };
}

// serveLenses answers the thread endpoint as s says, and records every GET and
// every read beacon.
export function serveLenses(over: Partial<LensServed> = {}): LensServed {
  const s: LensServed = { lastRead: 0, moderator: false, over: {}, gets: [], puts: [], ...over };
  vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
    const u = new URL(url, 'http://x');
    if (init.method === 'PUT' && u.pathname === `/api/v1/threads/${lensId}/read`) {
      const { ordinal } = JSON.parse(String(init.body)) as { ordinal: number };
      s.puts.push(ordinal);
      return new Response(JSON.stringify({ last_read_ordinal: ordinal }), { status: 200 });
    }
    if (u.pathname === `/api/v1/threads/${lensId}`) {
      s.gets.push(url);
      return new Response(JSON.stringify(pageOf(u.searchParams, s)), { status: 200 });
    }
    if (u.pathname.startsWith('/api/v1/drafts/')) return new Response(null, { status: 204, headers: { 'Draft-Revision': '0' } });
    return new Response('{}', { status: 404, headers: { 'Content-Type': 'application/problem+json' } });
  });
  return s;
}

// observers is every IntersectionObserver made since watchObservers was called. The
// read tracker's watch posts; the screen's own watches the foot of the list.
export const lensObservers: { observed: Element[]; callback: IntersectionObserverCallback }[] = [];
export function watchObservers() {
  lensObservers.length = 0;
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      observed: Element[] = [];
      constructor(readonly callback: IntersectionObserverCallback) {
        lensObservers.push(this);
      }
      observe(el: Element) {
        this.observed.push(el);
      }
      disconnect() {}
    },
  );
}
export const trackerObservers = () => lensObservers.filter((o) => o.observed.some((el) => el.id.startsWith('post-')));

// dwellOn puts post o on screen for the tracker's second, through the newest
// tracker, with fake timers running.
export function dwellOn(o: number, ms: number) {
  const io = trackerObservers().at(-1)!;
  const el = io.observed.find((e) => e.id === `post-${o}`)!;
  el.getBoundingClientRect = () => ({ top: 100, bottom: 300, height: 200 }) as DOMRect;
  io.callback(
    [
      {
        target: el,
        isIntersecting: true,
        intersectionRect: { height: 200 },
        boundingClientRect: { height: 200 },
        rootBounds: { height: 800 },
      } as unknown as IntersectionObserverEntry,
    ],
    io as unknown as IntersectionObserver,
  );
  vi.advanceTimersByTime(ms);
}
