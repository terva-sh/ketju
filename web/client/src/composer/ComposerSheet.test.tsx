import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Post, ThreadPage } from '../api/dto';
import { resetComposers } from '../state/composer';
import { isPhone } from '../state/layout';
import { renderAt, threadPage } from '../testing';
import { firstLine } from './ReplyBar';

const thread = { id: '3yymw47g00001', slug: 'why-we-left-slack', title: 'Why we left Slack' };
const path = `/t/${thread.id}/${thread.slug}?page=1`;

function page(over: { canReply?: boolean } = {}): ThreadPage {
  const g = threadPage(thread);
  const post = (ordinal: number, html: string, handle: string): Post => ({
    ...g.posts[0],
    ordinal,
    html,
    deleted: false,
    reply_to: null,
    reply_to_suppressed: false,
    author: { ...g.posts[0].author, handle },
  });
  return {
    ...g,
    viewer: { ...g.viewer, last_read_ordinal: 0, can_reply: over.canReply ?? true },
    posts: [post(1, '<p>The bill was the smaller half of it.</p>', 'mara'), post(2, '<p>Agreed.</p>', 'drew')],
  };
}

function serve(p: ThreadPage) {
  vi.stubGlobal('fetch', async (url: string) => {
    if (url === '/api/v1/render') return new Response(JSON.stringify({ html: '', mentions: [], quotes: [], reply_to: null }));
    return new Response(JSON.stringify(p), { status: 200 });
  });
}

// A visualViewport the tests can move, as an on-screen keyboard does.
class FakeViewport extends EventTarget {
  height = 844;
  offsetTop = 0;
  resize(height: number, offsetTop = 0) {
    this.height = height;
    this.offsetTop = offsetTop;
    this.dispatchEvent(new Event('resize'));
  }
}
let vv: FakeViewport;

beforeEach(() => {
  resetComposers();
  isPhone.value = true;
  Element.prototype.scrollIntoView = () => {};
  vv = new FakeViewport();
  Object.defineProperty(window, 'visualViewport', { value: vv, configurable: true });
});
afterEach(() => {
  isPhone.value = false;
  vi.unstubAllGlobals();
  document.documentElement.style.overflow = '';
});

const sheet = (root: HTMLElement) => root.querySelector<HTMLElement>('.composer-sheet');
const area = (root: HTMLElement) => root.querySelector<HTMLTextAreaElement>('.composer-sheet textarea')!;
const bar = (root: HTMLElement) => root.querySelector<HTMLElement>('[data-shell=replybar]');
const click = (el: Element | null) => act(() => (el as HTMLElement).click());
const press = (el: Element, key: string, shiftKey = false) =>
  act(() => void el.dispatchEvent(new KeyboardEvent('keydown', { key, shiftKey, bubbles: true, cancelable: true })));

async function type(root: HTMLElement, text: string) {
  const a = area(root);
  a.value = text;
  a.setSelectionRange(text.length, text.length);
  await act(() => void a.dispatchEvent(new Event('input', { bubbles: true })));
}

describe('the reply bar', () => {
  it('takes the tab bar’s place on a thread the reader may reply to, with no inline composer', async () => {
    serve(page());
    const root = await renderAt(path);
    expect(bar(root)).not.toBeNull();
    expect(root.querySelector('[data-shell=tabbar]')).toBeNull();
    expect(root.querySelector('.shell.has-replybar')).not.toBeNull();
    expect(root.querySelector('.composer')).toBeNull();
    expect(bar(root)!.textContent).toContain('Write a reply');
  });

  it('leaves the tab bar where the reader may not reply, and on the history route', async () => {
    serve(page({ canReply: false }));
    let root = await renderAt(path);
    expect(bar(root)).toBeNull();
    expect(root.querySelector('[data-shell=tabbar]')).not.toBeNull();
    expect(root.querySelector('.has-replybar')).toBeNull();

    serve(page());
    root = await renderAt(`/t/${thread.id}/${thread.slug}/2/history`);
    expect(bar(root)).toBeNull();
    expect(root.querySelector('[data-shell=tabbar]')).not.toBeNull();
  });

  it('is not on the desktop, which keeps the composer inline', async () => {
    isPhone.value = false;
    serve(page());
    const root = await renderAt(path);
    expect(bar(root)).toBeNull();
    expect(root.querySelector('.composer:not(.in-sheet)')).not.toBeNull();
    await click(root.querySelector('#post-2 [data-post-action=reply]'));
    expect(sheet(root)).toBeNull();
  });
});

// AC2: reply and quote from a post open the sheet prefilled.
describe('opening the sheet', () => {
  it('opens from the bar, focused, as a modal dialog', async () => {
    serve(page());
    const root = await renderAt(path);
    await click(root.querySelector('.replybar-open'));
    const s = sheet(root)!;
    expect(s.getAttribute('role')).toBe('dialog');
    expect(s.getAttribute('aria-modal')).toBe('true');
    expect(s.querySelector('#composer-sheet-title')?.textContent).toBe('Reply');
    expect(document.activeElement).toBe(area(root));
    expect(document.documentElement.style.overflow).toBe('hidden');
  });

  it('opens from a post’s Reply with the chip, and shows the post being answered', async () => {
    serve(page());
    const root = await renderAt(path);
    await click(root.querySelector('#post-2 [data-post-action=reply]'));
    const s = sheet(root)!;
    expect(s.querySelector('.reply-chip')?.getAttribute('data-reply-chip')).toBe('2');
    expect(s.querySelector('[data-answering="2"]')?.textContent).toContain('Agreed.');
    expect(document.activeElement).toBe(area(root));
  });

  it('opens from a post’s Quote with the quotation in the text and the caret after it', async () => {
    serve(page());
    const root = await renderAt(path);
    await click(root.querySelector('#post-1 [data-post-action=quote]'));
    expect(sheet(root)).not.toBeNull();
    expect(area(root).value).toBe('> [quote=#1]\n> The bill was the smaller half of it.\n\n');
    expect(area(root).selectionStart).toBe(area(root).value.length);
    expect(document.activeElement).toBe(area(root));
  });

  it('opens from the more menu’s Reply, as a reply to the thread', async () => {
    serve(page());
    const root = await renderAt(path);
    await click(root.querySelector('#post-2 [data-post-action=reply]'));
    await click(root.querySelector('.sheet-close'));
    await click(root.querySelector('[aria-label=More]'));
    await click(root.querySelector('[data-action=reply]'));
    expect(sheet(root)).not.toBeNull();
    expect(sheet(root)!.querySelector('.reply-chip')).toBeNull();
    expect(document.activeElement).toBe(area(root));
  });
});

// AC3: a draft shows in the bar and survives closing the sheet.
describe('closing the sheet', () => {
  it('keeps the draft and the chip, which the bar then shows, and gives focus back', async () => {
    serve(page());
    const root = await renderAt(path);
    const reply = root.querySelector<HTMLElement>('#post-1 [data-post-action=reply]')!;
    reply.focus();
    await click(reply);
    await type(root, '\n  First thought.\nSecond line');
    await click(root.querySelector('.sheet-close'));

    expect(sheet(root)).toBeNull();
    expect(document.activeElement).toBe(reply);
    expect(document.documentElement.style.overflow).toBe('');
    const b = bar(root)!;
    expect(b.querySelector('.replybar-text')?.textContent).toBe('First thought.');
    expect(b.querySelector('.reply-chip')?.getAttribute('data-reply-chip')).toBe('1');

    await click(root.querySelector('.replybar-open'));
    expect(area(root).value).toBe('\n  First thought.\nSecond line');
    expect(sheet(root)!.querySelector('.reply-chip')?.getAttribute('data-reply-chip')).toBe('1');
  });

  it('closes on Escape, and not when Escape was for the mention list', async () => {
    serve(page());
    const root = await renderAt(path);
    await click(root.querySelector('.replybar-open'));
    const handled = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true });
    handled.preventDefault();
    await act(() => void area(root).dispatchEvent(handled));
    expect(sheet(root)).not.toBeNull();
    await press(area(root), 'Escape');
    expect(sheet(root)).toBeNull();
    expect(document.activeElement).toBe(root.querySelector('.replybar-open'));
  });

  it('keeps Tab inside the dialog', async () => {
    serve(page());
    const root = await renderAt(path);
    await click(root.querySelector('.replybar-open'));
    const close = root.querySelector<HTMLElement>('.sheet-close')!;
    const buttons = [...sheet(root)!.querySelectorAll<HTMLElement>('button, textarea')].filter((el) => !el.closest('[hidden]'));
    const last = buttons.at(-1)!;
    last.focus();
    await press(last, 'Tab');
    expect(document.activeElement).toBe(close);
    await press(close, 'Tab', true);
    expect(document.activeElement).toBe(last);
    // A Tab in the middle is the browser's.
    const tool = sheet(root)!.querySelector<HTMLElement>('[data-edit]')!;
    tool.focus();
    const mid = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true });
    tool.dispatchEvent(mid);
    expect(mid.defaultPrevented).toBe(false);
  });
});

// AC4, as far as a DOM without layout can see it: the sheet follows the
// visual viewport, and the toolbar is the composer's last row.
describe('the keyboard', () => {
  it('sizes the sheet to the visual viewport, and follows it as the keyboard opens and closes', async () => {
    serve(page());
    const root = await renderAt(path);
    await click(root.querySelector('.replybar-open'));
    const s = sheet(root)!;
    expect(s.style.getPropertyValue('--vv-height')).toBe('844px');
    await act(() => vv.resize(480, 12));
    expect(s.style.getPropertyValue('--vv-height')).toBe('480px');
    expect(s.style.getPropertyValue('--vv-top')).toBe('12px');
    await act(() => vv.resize(844));
    expect(s.style.getPropertyValue('--vv-height')).toBe('844px');
  });

  it('stops following once closed', async () => {
    serve(page());
    const root = await renderAt(path);
    await click(root.querySelector('.replybar-open'));
    const s = sheet(root)!;
    await click(root.querySelector('.sheet-close'));
    vv.resize(300);
    expect(s.style.getPropertyValue('--vv-height')).toBe('844px');
  });
});

describe('firstLine', () => {
  it.each([
    ['', ''],
    ['one', 'one'],
    ['\n\n  two  \nthree', 'two'],
    ['   \n\t', ''],
  ])('%j is %j', (src, want) => expect(firstLine(src)).toBe(want));
});
