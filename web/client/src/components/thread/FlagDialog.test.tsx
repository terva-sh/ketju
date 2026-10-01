import { render } from 'preact';
import { useState } from 'preact/hooks';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { problemAnswer, stubServer, type Answer, type StubServer } from '../../testing';
import { FlagDialog, flagThanks, maxFlagNote } from './FlagDialog';
import { PostMore } from './PostMore';

// The flag dialog (TKT-01M3QQWA5Z; docs/08 "Flags", docs/07
// POST /threads/{id}/flags), against a stubbed server.

const thread = '3yymw47g00001';
const url = `POST /api/v1/threads/${thread}/flags`;

let root: HTMLElement;
let returnTo: HTMLButtonElement;
let article: HTMLElement;
let onClose: ReturnType<typeof vi.fn<() => void>>;

beforeEach(() => {
  root = document.createElement('div');
  returnTo = document.createElement('button');
  returnTo.textContent = 'flag';
  article = document.createElement('article');
  article.id = 'post-4';
  article.tabIndex = -1;
  article.append(returnTo);
  document.body.replaceChildren(article, root);
  onClose = vi.fn<() => void>();
});
afterEach(async () => {
  await act(async () => render(null, root));
  vi.unstubAllGlobals();
});

async function open() {
  await act(async () => render(<FlagDialog thread={thread} ordinal={4} returnTo={returnTo} onClose={onClose} />, root));
  return root.querySelector<HTMLElement>('[role=dialog]')!;
}
const close = async () => act(async () => render(null, root));

const radio = (d: HTMLElement, value: string) => d.querySelector<HTMLInputElement>(`input[type=radio][value="${value}"]`)!;
const noteOf = (d: HTMLElement) => d.querySelector<HTMLTextAreaElement>('textarea')!;
const sendOf = (d: HTMLElement) => d.querySelector<HTMLButtonElement>('.flag-send')!;
const closeOf = (d: HTMLElement) => d.querySelector<HTMLButtonElement>('[data-flag-close]')!;

async function choose(d: HTMLElement, value: string) {
  await act(async () => radio(d, value).click());
}
async function type(d: HTMLElement, text: string) {
  const t = noteOf(d);
  t.value = text;
  await act(async () => void t.dispatchEvent(new Event('input', { bubbles: true })));
}
async function send(d: HTMLElement) {
  await act(async () => sendOf(d).click());
  await act(() => new Promise((r) => setTimeout(r, 5)));
}
const flags = (s: StubServer) => s.sent.filter((r) => r.method === 'POST');

describe('the flag dialog', () => {
  it('sends one of the four reasons', async () => {
    const s = stubServer({ [url]: () => ({ status: 200, body: { received: true } }) });
    const d = await open();
    expect([...d.querySelectorAll<HTMLInputElement>('input[type=radio]')].map((r) => [r.value, r.checked])).toEqual([
      ['spam', false],
      ['abuse', false],
      ['off-topic', false],
      ['other', false],
    ]);
    expect(d.querySelector('h2')?.textContent).toBe('Flag post #4');
    expect(sendOf(d).disabled).toBe(true);
    await choose(d, 'spam');
    await send(d);
    expect(flags(s)).toHaveLength(1);
    expect(flags(s)[0].url).toBe(`/api/v1/threads/${thread}/flags`);
    expect(JSON.stringify(flags(s)[0].body)).toBe('{"ordinal":4,"reason":"spam","note":""}');
  });

  it('other needs a note', async () => {
    const s = stubServer({ [url]: () => ({ status: 200, body: { received: true } }) });
    const d = await open();
    await choose(d, 'other');
    expect(sendOf(d).disabled).toBe(true);
    await type(d, '   ');
    expect(sendOf(d).disabled).toBe(true);
    await type(d, '  a scam link ');
    expect(sendOf(d).disabled).toBe(false);
    await send(d);
    // The note goes as typed, untrimmed.
    expect(flags(s).map((r) => r.body)).toEqual([{ ordinal: 4, reason: 'other', note: '  a scam link ' }]);
  });

  it('the note is counted in code points', async () => {
    const s = stubServer({ [url]: () => ({ status: 200, body: { received: true } }) });
    const d = await open();
    await choose(d, 'abuse');
    await type(d, '😀'.repeat(maxFlagNote + 1));
    expect(d.textContent).toContain('The note is too long.');
    expect(sendOf(d).disabled).toBe(true);
    await act(async () => void d.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    expect(flags(s)).toHaveLength(0);

    await type(d, '😀'.repeat(maxFlagNote));
    expect(d.textContent).not.toContain('too long');
    await send(d);
    expect(flags(s)).toHaveLength(1);
    expect((flags(s)[0].body as { note: string }).note).toBe('😀'.repeat(maxFlagNote));
  });

  it('the receipt says thanks and nothing else', async () => {
    stubServer({ [url]: () => ({ status: 200, body: { received: true } }) });
    const d = await open();
    await choose(d, 'spam');
    await send(d);
    expect(d.querySelector('form')).toBeNull();
    expect(d.textContent).toBe(`Flag post #4${flagThanks}Close`);
    expect(document.activeElement).toBe(closeOf(d));
  });

  // Terva review 1538: apiSend resolves on any 2xx, and only the receipt
  // earns the thanks.
  it.each([
    [{ status: 204 }],
    [{ status: 201, body: {} }],
    [{ status: 201, body: { received: true } }],
    [{ status: 202, body: { received: true } }],
    [{ status: 200, body: {} }],
    [{ status: 200, body: { received: false } }],
  ] as [Answer][])('a success that is not the receipt is not thanked: %j', async (answer) => {
    stubServer({ [url]: () => answer });
    const d = await open();
    await choose(d, 'spam');
    await send(d);
    expect(d.textContent).not.toContain(flagThanks);
    expect(d.querySelector('[role=alert]')?.textContent).toBe('Something went wrong on the server.');
    expect(radio(d, 'spam').checked).toBe(true);
  });

  it('unavailable is not a receipt', async () => {
    stubServer({ [url]: () => problemAnswer('unavailable', 503) });
    const d = await open();
    await choose(d, 'other');
    await type(d, 'why');
    await send(d);
    expect(d.querySelector('[role=alert]')?.textContent).toBe('Your report could not be sent. Try again shortly.');
    expect(d.textContent).not.toContain(flagThanks);
    expect(radio(d, 'other').checked).toBe(true);
    expect(noteOf(d).value).toBe('why');
    expect(sendOf(d).disabled).toBe(false);
  });

  it.each([
    [problemAnswer('validation-failed', 422), 'Choose a reason, and say what is wrong when it is Other.'],
    [problemAnswer('conflict', 409), 'This post can no longer be flagged. It may have been removed or moved, or its thread archived.'],
    [problemAnswer('forbidden', 403), 'You cannot flag this post. It is your own, or you no longer have access to it.'],
    [problemAnswer('not-found', 404), 'This post is no longer here.'],
    [problemAnswer('rate-limited', 429), 'Too many requests. Wait a moment and try again.'],
  ] as [Answer, string][])('refusals are said in words: %j', async (answer, words) => {
    stubServer({ [url]: () => answer });
    const d = await open();
    await choose(d, 'off-topic');
    await type(d, 'kept');
    await send(d);
    expect(d.querySelector('[role=alert]')?.textContent).toBe(words);
    expect(d.textContent).not.toContain(flagThanks);
    expect(radio(d, 'off-topic').checked).toBe(true);
    expect(noteOf(d).value).toBe('kept');
  });

  it('sends once', async () => {
    let release!: () => void;
    const s = stubServer({
      [url]: () => new Promise<Answer>((r) => (release = () => r({ status: 200, body: { received: true } }))),
    });
    const d = await open();
    await choose(d, 'spam');
    const button = sendOf(d);
    await act(async () => {
      button.click();
      button.click();
    });
    await act(async () => void d.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    expect(flags(s)).toHaveLength(1);
    await act(async () => release());
    await act(() => new Promise((r) => setTimeout(r, 5)));
    expect(d.textContent).toContain(flagThanks);
    expect(flags(s)).toHaveLength(1);
  });

  it('is a modal', async () => {
    stubServer({});
    const d = await open();
    expect(d.getAttribute('aria-modal')).toBe('true');
    expect(d.contains(document.activeElement)).toBe(true);
    // Send is disabled, so Close is the last control: Tab from it wraps.
    closeOf(d).focus();
    await act(async () => void d.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })));
    expect(document.activeElement).toBe(radio(d, 'spam'));
    await act(async () =>
      void d.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true })),
    );
    expect(document.activeElement).toBe(closeOf(d));

    await act(async () => void d.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })));
    expect(onClose).toHaveBeenCalledTimes(1);
    await act(async () => closeOf(d).click());
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it('returns focus where it is told', async () => {
    stubServer({});
    await open();
    await close();
    expect(document.activeElement).toBe(returnTo);

    await open();
    returnTo.remove();
    await close();
    expect(document.activeElement).toBe(article);
  });
});

// The dialog is drawn on the microtask after the press that opens it, and
// must take an Escape from then on, before a frame has passed: a quick
// reader, or a loaded e2e run, presses it there (TKT-01M3TS0ZV9). The
// presses are dispatched outside act, which would run the effects and hide
// the race.
describe('an Escape pressed straight after opening', () => {
  const key = (k: string) => document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }));
  const dialog = () => root.querySelector('[role=dialog]');
  // settle lets the raced frame run before the next test.
  const settle = () => act(() => new Promise((r) => setTimeout(r, 50)));

  it('closes the dialog', async () => {
    stubServer({});
    function Opener() {
      const [open, setOpen] = useState(false);
      return (
        <>
          <button type="button" data-opener onClick={() => setOpen(true)}>
            flag
          </button>
          {open && <FlagDialog thread={thread} ordinal={4} returnTo={returnTo} onClose={() => setOpen(false)} />}
        </>
      );
    }
    await act(async () => render(<Opener />, root));
    const opener = root.querySelector<HTMLButtonElement>('[data-opener]')!;
    opener.focus();
    try {
      opener.click();
      await Promise.resolve();
      expect(dialog()).not.toBeNull();
      key('Escape');
      await Promise.resolve();
      expect(dialog()).toBeNull();
      expect(document.activeElement).toBe(returnTo);
    } finally {
      await settle();
    }
  });

  // On a phone, flag is an item of the post's More list. The list's Escape
  // listener stays on the document until the frame after it closed, and
  // took the key, putting focus on its button with the dialog still open.
  it('closes the dialog opened from a phone’s More list', async () => {
    stubServer({});
    function Phone() {
      const [to, setTo] = useState<HTMLElement | null>(null);
      return (
        <>
          <PostMore thread={{ id: thread, slug: 'a' }} ordinal={4} onFlag={(_, r) => setTo(r)} />
          {to && <FlagDialog thread={thread} ordinal={4} returnTo={to} onClose={() => setTo(null)} />}
        </>
      );
    }
    await act(async () => render(<Phone />, root));
    const more = root.querySelector<HTMLButtonElement>('[data-post-more]')!;
    await act(async () => more.click());
    const item = root.querySelector<HTMLButtonElement>('[data-post-action=flag]')!;
    item.focus();
    try {
      item.click();
      await Promise.resolve();
      expect(dialog()).not.toBeNull();
      key('Escape');
      await Promise.resolve();
      expect(dialog()).toBeNull();
      expect(document.activeElement).toBe(more);
    } finally {
      await settle();
    }
  });
});
