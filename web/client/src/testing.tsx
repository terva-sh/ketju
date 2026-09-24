import { render } from 'preact';
import { act } from 'preact/test-utils';
import { App } from './app';

// renderAt renders the whole app at url, as a reader arriving there would,
// and waits for the screen to settle: a lazy chunk, a fetch.
export async function renderAt(url: string): Promise<HTMLElement> {
  history.replaceState(null, '', url);
  const root = document.createElement('div');
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
