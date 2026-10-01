import { render } from 'preact';
import { act } from 'preact/test-utils';
import { vi } from 'vitest';
import { resetApiCache } from './api/client';
import type { ThreadPage } from './api/dto';
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
