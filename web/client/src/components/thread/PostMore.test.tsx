import { render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { PostMore } from './PostMore';

// A post's More list on a phone takes its keys from the press that opened
// it, and lets them go in the commit that closes it (TKT-01M3TV4HBF), as the
// top bar's more menu does. The presses are dispatched outside act, which
// would run the effects and hide the race. Flag, whose dialog takes its
// keys in the capture phase, is FlagDialog.test.tsx's.

const thread = { id: '3yymw47g00001', slug: 'a' };

let root: HTMLElement;
// elsewhere is what an item puts focus on, as the editor puts it in its
// status block.
let elsewhere: HTMLButtonElement;

beforeEach(() => {
  root = document.createElement('div');
  elsewhere = document.createElement('button');
  elsewhere.textContent = 'elsewhere';
  document.body.replaceChildren(root, elsewhere);
});
afterEach(async () => {
  await act(async () => render(null, root));
});

const more = () => root.querySelector<HTMLButtonElement>('[data-post-more]')!;
const list = () => root.querySelector<HTMLElement>('.post-more-list')!;
const escape = () => document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
// settle lets the raced frame run before the next test.
const settle = () => act(() => new Promise((r) => setTimeout(r, 50)));

describe("a post's More list", () => {
  it('Escape one microtask after opening closes the list', async () => {
    await act(async () => render(<PostMore thread={thread} ordinal={4} onEdit={() => elsewhere.focus()} />, root));
    more().focus();
    try {
      more().click();
      await Promise.resolve();
      expect(list().hidden).toBe(false);
      escape();
      await Promise.resolve();
      expect(list().hidden).toBe(true);
      expect(document.activeElement).toBe(more());
    } finally {
      await settle();
    }
  });

  it('after choosing an item, its listener is gone: a later Escape leaves focus where the item put it', async () => {
    await act(async () => render(<PostMore thread={thread} ordinal={4} onEdit={() => elsewhere.focus()} />, root));
    await act(async () => more().click());
    const edit = root.querySelector<HTMLButtonElement>('[data-post-action=edit]')!;
    edit.focus();
    try {
      edit.click();
      await Promise.resolve();
      expect(list().hidden).toBe(true);
      expect(document.activeElement).toBe(elsewhere);
      escape();
      await Promise.resolve();
      expect(document.activeElement).toBe(elsewhere);
    } finally {
      await settle();
    }
  });

  // TKT-01M29CYSC0.
  it('lists reply as new thread when the post hands it over, as a link that closes the list', async () => {
    await act(async () => render(<PostMore thread={thread} ordinal={4} />, root));
    expect(root.querySelector('[data-post-action=continue]')).toBeNull();
    const href = '/f/general/new?from=3yymw47g00001&ordinal=4';
    await act(async () => render(<PostMore thread={thread} ordinal={4} continueHref={href} />, root));
    await act(async () => more().click());
    const item = root.querySelector<HTMLAnchorElement>('[data-post-action=continue]')!;
    expect(item.textContent).toBe('reply as new thread');
    expect(item.getAttribute('href')).toBe(href);
    item.addEventListener('click', (e) => e.preventDefault());
    await act(async () => item.click());
    expect(list().hidden).toBe(true);
  });
});
