import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Post, RenderResult, ThreadPage, UserSuggestions } from '../api/dto';
import { resetComposers } from '../state/composer';
import { renderAt, threadPage } from '../testing';
import { mentionDelay, previewDelay } from './Composer';

const thread = { id: '3yymw47g00001', slug: 'why-we-left-slack', title: 'Why we left Slack' };
const path = `/t/${thread.id}/${thread.slug}?page=1`;

// page is the member golden with three posts of this test's own; the member
// may reply unless told otherwise.
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
    posts: [
      post(1, '<p>The bill was the smaller half of it.</p>', 'mara'),
      post(
        2,
        '<details class="quote" open=""><summary><a href="#post-1" class="quote-source" rel="nofollow">Post #1</a></summary>' +
          '<blockquote>\n<p>smaller half</p>\n</blockquote>\n</details>\n<p>Agreed.</p>\n<p>And another thing.</p>',
        'drew',
      ),
      post(3, '<p>third</p>', 'ines'),
    ],
  };
}

interface Sent {
  url: string;
  body: unknown;
}

// serve answers the thread page, /render with a rendering that names what
// it was sent, and /users with alice and al-x; it records every other
// request.
function serve(p: ThreadPage, users: UserSuggestions = { users: [] }) {
  const sent: Sent[] = [];
  vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
    if (url === '/api/v1/render') {
      const body = JSON.parse(String(init.body));
      sent.push({ url, body });
      const r: RenderResult = {
        html: `<p data-rendered>${body.source}</p>`,
        mentions: [],
        quotes: [],
        reply_to: body.reply_to,
      };
      return new Response(JSON.stringify(r), { status: 200 });
    }
    if (url.startsWith('/api/v1/users')) {
      sent.push({ url, body: null });
      return new Response(JSON.stringify(users), { status: 200 });
    }
    return new Response(JSON.stringify(p), { status: 200 });
  });
  return sent;
}

const renders = (sent: Sent[]) => sent.filter((s) => s.url === '/api/v1/render');

let scrolled: Element[];
beforeEach(() => {
  resetComposers();
  scrolled = [];
  Element.prototype.scrollIntoView = function (this: Element) {
    scrolled.push(this);
  };
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  getSelection()?.removeAllRanges();
});

const area = (root: HTMLElement) => root.querySelector<HTMLTextAreaElement>('.composer textarea')!;

// type puts text into the textarea at the caret as a keystroke would: the
// value, the caret, then the input event the composer listens for.
async function type(root: HTMLElement, text: string) {
  const a = area(root);
  const at = a.selectionStart;
  a.value = a.value.slice(0, at) + text + a.value.slice(a.selectionEnd);
  a.setSelectionRange(at + text.length, at + text.length);
  await act(() => void a.dispatchEvent(new Event('input', { bubbles: true })));
}

async function key(root: HTMLElement, k: string) {
  const a = area(root);
  await act(() => void a.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true })));
  await act(() => void a.dispatchEvent(new KeyboardEvent('keyup', { key: k, bubbles: true })));
}

// wait lets real timers and the fetches they start run.
const wait = (ms: number) => act(() => new Promise((r) => setTimeout(r, ms)));

const click = (el: Element | null) => act(() => (el as HTMLElement).click());

describe('where the composer is', () => {
  it('is at the foot of the thread, with quote and reply on each post, for a reader who may reply', async () => {
    serve(page());
    const root = await renderAt(path);
    const composer = root.querySelector('.composer');
    expect(composer).not.toBeNull();
    // After the posts and the pager beneath them.
    expect(root.querySelector('.posts')!.compareDocumentPosition(composer!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(root.querySelectorAll('[data-post-action=quote]')).toHaveLength(3);
    expect(root.querySelectorAll('[data-post-action=reply]')).toHaveLength(3);
    expect(root.querySelector('[data-shell=toolbar] [data-action=reply]')).not.toBeNull();
  });

  it('is not offered on the post history route, which has no composer (review 543)', async () => {
    serve(page());
    const root = await renderAt(`/t/${thread.id}/${thread.slug}/2/history`);
    expect(root.querySelector('[data-screen=post-history]')).not.toBeNull();
    expect(root.querySelector('.composer')).toBeNull();
    expect(root.querySelector('[data-action=reply]')).toBeNull();
    expect(root.querySelector('[data-action=copy-link]')).not.toBeNull();
  });

  it('is not there, and neither are quote and reply, for a reader who may not', async () => {
    serve(page({ canReply: false }));
    const root = await renderAt(path);
    expect(root.querySelector('.composer')).toBeNull();
    expect(root.querySelector('[data-post-action]')).toBeNull();
    expect(root.querySelector('[data-action=reply]')).toBeNull();
  });
});

// AC1 and AC5.
describe('the preview', () => {
  it('is the server rendering of the source, in this thread, sent once the typing stops', async () => {
    const sent = serve(page());
    const root = await renderAt(path);
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });

    // Twenty characters over a second: nothing is sent while they arrive.
    for (const ch of 'twenty characters!!') {
      await type(root, ch);
      await act(async () => void (await vi.advanceTimersByTimeAsync(50)));
    }
    await type(root, '.');
    expect(renders(sent)).toHaveLength(0);
    await act(async () => void (await vi.advanceTimersByTimeAsync(previewDelay - 1)));
    expect(renders(sent)).toHaveLength(0);
    await act(async () => void (await vi.advanceTimersByTimeAsync(1)));
    expect(renders(sent)).toHaveLength(1);
    expect(renders(sent)[0].body).toEqual({ source: 'twenty characters!!.', thread_id: thread.id, reply_to: null });
    vi.useRealTimers();
    await wait(5);

    // The preview holds exactly what the server sent, inserted as a post's
    // body is.
    await click(root.querySelector('#composer-tab-preview'));
    const shown = root.querySelector('.composer-preview [data-preview]')!;
    expect(shown.innerHTML).toBe('<p data-rendered="">twenty characters!!.</p>');
    expect(shown.classList.contains('post-body')).toBe(true);
    expect(root.querySelector<HTMLElement>('#composer-panel-write')!.hidden).toBe(true);
  });

  // The server's answers arrive, and are read, in whatever order the
  // network gives; cancelling one that has already come back does not take
  // it back, so this fetch ignores the abort.
  it('shows only the newest source, whatever order the answers come in', async () => {
    const answers: ((r: Response) => void)[] = [];
    const sources: string[] = [];
    serve(page());
    const root = await renderAt(path);
    vi.stubGlobal('fetch', (url: string, init: RequestInit = {}) => {
      if (url !== '/api/v1/render') return Promise.resolve(new Response('{}', { status: 404 }));
      expect(init.signal).toBeInstanceOf(AbortSignal);
      sources.push(JSON.parse(String(init.body)).source);
      return new Promise<Response>((resolve) => answers.push(resolve));
    });
    const answer = (i: number) =>
      answers[i](new Response(JSON.stringify({ html: `<p>${sources[i]}</p>`, mentions: [], quotes: [], reply_to: null })));

    await type(root, 'first');
    await wait(previewDelay + 20);
    await type(root, ' second');
    await wait(previewDelay + 20);
    expect(sources).toEqual(['first', 'first second']);
    await act(async () => answer(1));
    await act(async () => answer(0));
    await wait(5);
    await click(root.querySelector('#composer-tab-preview'));
    expect(root.querySelector('[data-preview]')!.innerHTML).toBe('<p>first second</p>');
  });

  it('drops a rendering as soon as the source or the reply it rendered changes (review 537)', async () => {
    serve(page());
    const root = await renderAt(path);
    await type(root, 'first');
    await wait(previewDelay + 20);
    await click(root.querySelector('#composer-tab-preview'));
    expect(root.querySelector('[data-preview]')!.innerHTML).toContain('first');

    await click(root.querySelector('#composer-tab-write'));
    await type(root, ' second');
    await click(root.querySelector('#composer-tab-preview'));
    expect(root.querySelector('[data-preview]')).toBeNull();
    expect(root.querySelector('.preview-busy')).not.toBeNull();
    await wait(previewDelay + 20);
    expect(root.querySelector('[data-preview]')!.innerHTML).toContain('first second');

    await click(root.querySelector('#post-1 [data-post-action=reply]'));
    await click(root.querySelector('#composer-tab-preview'));
    expect(root.querySelector('[data-preview]')).toBeNull();
    await wait(previewDelay + 20);
    expect(root.querySelector('[data-preview]')).not.toBeNull();
  });

  it('asks nothing for an empty source, and says why a rendering failed', async () => {
    const sent = serve(page());
    const root = await renderAt(path);
    await type(root, '   ');
    await wait(previewDelay + 20);
    expect(renders(sent)).toHaveLength(0);
    await click(root.querySelector('#composer-tab-preview'));
    expect(root.querySelector('.preview-empty')).not.toBeNull();

    vi.stubGlobal('fetch', async () =>
      new Response(JSON.stringify({ type: 'validation-failed', title: 'x', status: 422 }), {
        status: 422,
        headers: { 'Content-Type': 'application/problem+json' },
      }),
    );
    await click(root.querySelector('#composer-tab-write'));
    await type(root, 'x');
    await wait(previewDelay + 20);
    await click(root.querySelector('#composer-tab-preview'));
    expect(root.querySelector('.preview-error')!.textContent).toBe('That was not accepted. Check it and try again.');

    // The failure was about that input; the next input's preview does not
    // show it while it renders (review 541).
    serve(page());
    await click(root.querySelector('#composer-tab-write'));
    await type(root, 'y');
    await click(root.querySelector('#composer-tab-preview'));
    expect(root.querySelector('.preview-error')).toBeNull();
    expect(root.querySelector('.preview-busy')).not.toBeNull();
    await wait(previewDelay + 20);
    expect(root.querySelector('[data-preview]')!.innerHTML).toContain('xy');
  });
});

// AC2.
describe('quoting a post', () => {
  it('quotes the words selected in it, attributed and linked to its ordinal', async () => {
    const sent = serve(page());
    const root = await renderAt(path);
    await type(root, 'Before.');
    const text = root.querySelector('#post-1 .post-body p')!.firstChild!;
    const range = document.createRange();
    range.setStart(text, 4);
    range.setEnd(text, 29);
    getSelection()!.addRange(range);

    const quote = root.querySelector<HTMLElement>('#post-1 [data-post-action=quote]')!;
    await act(() => void quote.dispatchEvent(new MouseEvent('mousedown', { bubbles: true })));
    getSelection()!.removeAllRanges(); // as a click may do before its handler runs
    await click(quote);

    expect(area(root).value).toBe('Before.\n\n> [quote=#1]\n> bill was the smaller half\n\n');
    expect(area(root).selectionStart).toBe(area(root).value.length);
    expect(document.activeElement).toBe(area(root));
    expect(scrolled).toContain(root.querySelector('.composer'));

    await wait(previewDelay + 20);
    expect(renders(sent).at(-1)!.body).toMatchObject({ source: area(root).value });
  });

  it('quotes the whole post when nothing is selected, without the quotation it holds', async () => {
    serve(page());
    const root = await renderAt(path);
    await click(root.querySelector('#post-2 [data-post-action=quote]'));
    expect(area(root).value).toBe('> [quote=#2]\n> Agreed.\n>\n> And another thing.\n\n');
  });

  it('ignores a selection in another post', async () => {
    serve(page());
    const root = await renderAt(path);
    const range = document.createRange();
    range.selectNodeContents(root.querySelector('#post-3 .post-body p')!);
    getSelection()!.addRange(range);
    await click(root.querySelector('#post-1 [data-post-action=quote]'));
    expect(area(root).value).toBe('> [quote=#1]\n> The bill was the smaller half of it.\n\n');
  });
});

// AC3.
describe('the reply chip', () => {
  it('sets reply_to from a post, shows it, and is removable', async () => {
    const sent = serve(page());
    const root = await renderAt(path);
    expect(root.querySelector('.reply-chip')).toBeNull();

    await click(root.querySelector('#post-2 [data-post-action=reply]'));
    const chip = root.querySelector('.reply-chip')!;
    expect(chip.getAttribute('data-reply-chip')).toBe('2');
    expect(chip.textContent).toContain('#2');
    expect(chip.textContent).toContain('drew');
    expect(document.activeElement).toBe(area(root));

    await type(root, 'yes');
    await wait(previewDelay + 20);
    expect(renders(sent).at(-1)!.body).toEqual({ source: 'yes', thread_id: thread.id, reply_to: 2 });

    await click(root.querySelector('.reply-chip button'));
    expect(root.querySelector('.reply-chip')).toBeNull();
    await wait(previewDelay + 20);
    expect(renders(sent).at(-1)!.body).toEqual({ source: 'yes', thread_id: thread.id, reply_to: null });
  });

  it('is cleared by the thread Reply, which answers the thread itself', async () => {
    serve(page());
    const root = await renderAt(path);
    await click(root.querySelector('#post-3 [data-post-action=reply]'));
    expect(root.querySelector('.reply-chip')).not.toBeNull();
    scrolled = [];
    await click(root.querySelector('[data-shell=toolbar] [data-action=reply]'));
    expect(root.querySelector('.reply-chip')).toBeNull();
    expect(scrolled).toContain(root.querySelector('.composer'));
    expect(document.activeElement).toBe(area(root));
  });
});

// AC4.
describe('mention autocomplete', () => {
  const people: UserSuggestions = {
    users: [
      { handle: 'al-x', display_name: 'Al X', avatar: '', role: 'member', kind: 'human' },
      { handle: 'alice', display_name: 'Alice Nakamura', avatar: '', role: 'member', kind: 'human' },
    ],
  };

  it('offers the handles the server has for what is typed, and inserts the one chosen as the renderer reads it', async () => {
    const sent = serve(page(), people);
    const root = await renderAt(path);
    await type(root, 'thanks @AL');
    expect(root.querySelector('[role=listbox]')).toBeNull();
    await wait(mentionDelay + 20);
    expect(sent.filter((s) => s.url.startsWith('/api/v1/users')).map((s) => s.url)).toEqual(['/api/v1/users?prefix=AL&limit=8']);

    const list = root.querySelector('[role=listbox]')!;
    const options = [...list.querySelectorAll('[role=option]')];
    expect(options.map((o) => o.getAttribute('data-handle'))).toEqual(['al-x', 'alice']);
    expect(area(root).getAttribute('aria-expanded')).toBe('true');
    expect(area(root).getAttribute('aria-activedescendant')).toBe(options[0].id);

    await key(root, 'ArrowDown');
    expect(area(root).getAttribute('aria-activedescendant')).toBe(options[1].id);
    await key(root, 'Enter');
    expect(area(root).value).toBe('thanks @alice ');
    expect(area(root).selectionStart).toBe('thanks @alice '.length);
    expect(root.querySelector('[role=listbox]')).toBeNull();
  });

  it('can be picked with the pointer, and closed with Escape', async () => {
    serve(page(), people);
    const root = await renderAt(path);
    await type(root, '@a');
    await wait(mentionDelay + 20);
    await key(root, 'Escape');
    expect(root.querySelector('[role=listbox]')).toBeNull();
    await type(root, 'l');
    await wait(mentionDelay + 20);
    const opt = root.querySelector('[data-handle=al-x]')!;
    await act(() => void opt.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true })));
    expect(area(root).value).toBe('@al-x ');
  });

  it('asks nothing where the renderer would see no mention', async () => {
    const sent = serve(page(), people);
    const root = await renderAt(path);
    await type(root, 'mail me@al');
    await wait(mentionDelay + 20);
    expect(sent.filter((s) => s.url.startsWith('/api/v1/users'))).toHaveLength(0);
    expect(root.querySelector('[role=listbox]')).toBeNull();
  });

  it('asks nothing while a range is selected, which typing would replace', async () => {
    const sent = serve(page(), people);
    const root = await renderAt(path);
    await type(root, 'x @al yz');
    await wait(mentionDelay + 20);
    area(root).setSelectionRange(5, 8);
    await key(root, 'Shift');
    await wait(mentionDelay + 20);
    expect(sent.filter((s) => s.url.startsWith('/api/v1/users'))).toHaveLength(0);
  });
});

describe('the toolbar', () => {
  it('edits the selection and puts it back', async () => {
    serve(page());
    const root = await renderAt(path);
    await type(root, 'make this bold');
    area(root).setSelectionRange(5, 9);
    await click(root.querySelector('[data-edit=bold]'));
    expect(area(root).value).toBe('make **this** bold');
    expect([area(root).selectionStart, area(root).selectionEnd]).toEqual([7, 11]);
  });
});
