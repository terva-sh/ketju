import { render } from 'preact';
import { useState } from 'preact/hooks';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { stubServer } from '../../testing';
import { PostPopover } from './PostPopover';

// The popover for a post off this page takes focus and its Escape from the
// press that opened it, not a frame later, and leaves alone an Escape a
// dialog has already taken (TKT-01M3TV4HBF). The presses are dispatched
// outside act, which would run the effects and hide the race.

const thread = { id: '3yymw47g00001', slug: 'a' };

let root: HTMLElement;

beforeEach(() => {
  root = document.createElement('div');
  document.body.replaceChildren(root);
  // The post never arrives: these tests are about keys, not the post.
  stubServer({ [`GET /api/v1/threads/${thread.id}/posts/4`]: () => new Promise(() => {}) });
});
afterEach(async () => {
  await act(async () => render(null, root));
  vi.unstubAllGlobals();
});

function Opener() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button type="button" data-opener onClick={() => setOpen(true)}>
        #4
      </button>
      {open && <PostPopover thread={thread} ordinal={4} onClose={() => setOpen(false)} />}
    </>
  );
}

const opener = () => root.querySelector<HTMLButtonElement>('[data-opener]')!;
const popover = () => root.querySelector<HTMLElement>('.post-popover');
const escape = () => document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
// settle lets the raced frame run before the next test.
const settle = () => act(() => new Promise((r) => setTimeout(r, 50)));

describe('the post popover', () => {
  it('focus is in the popover one microtask after it opens, and Escape then closes it', async () => {
    await act(async () => render(<Opener />, root));
    opener().focus();
    try {
      opener().click();
      await Promise.resolve();
      expect(popover()).not.toBeNull();
      expect(document.activeElement).toBe(popover());
      const ev = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true });
      document.activeElement!.dispatchEvent(ev);
      await Promise.resolve();
      expect(ev.defaultPrevented).toBe(true);
      expect(popover()).toBeNull();
      expect(document.activeElement).toBe(opener());
    } finally {
      await settle();
    }
  });

  it('a key a dialog already took does not close the popover', async () => {
    await act(async () => render(<Opener />, root));
    await act(async () => opener().click());
    // A dialog that listens in the capture phase, as useModal does, hears
    // the key first and takes it.
    const take = (e: KeyboardEvent) => e.key === 'Escape' && e.preventDefault();
    document.addEventListener('keydown', take, true);
    try {
      await act(async () => void escape());
      expect(popover()).not.toBeNull();
    } finally {
      document.removeEventListener('keydown', take, true);
    }
  });
});
