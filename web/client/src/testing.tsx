import { render } from 'preact';
import { act } from 'preact/test-utils';
import { resetApiCache } from './api/client';
import type { ThreadPage } from './api/dto';
import { App } from './app';
import { resetThreadCache } from './state/threads';
import golden from '../../../internal/dto/testdata/golden/thread.member.json';

let mounted: HTMLElement | null = null;

// renderAt renders the whole app at url, as a reader arriving there would,
// and waits for the screen to settle: a lazy chunk, a fetch. The previous
// test's app is unmounted first; left mounted, it would go on following
// history and render into this test.
export async function renderAt(url: string): Promise<HTMLElement> {
  if (mounted) {
    const prev = mounted;
    await act(async () => render(null, prev));
  }
  resetApiCache();
  resetThreadCache();
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

export async function settle(root: HTMLElement) {
  for (let i = 0; i < 50 && !root.querySelector('[data-screen]'); i++) {
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
