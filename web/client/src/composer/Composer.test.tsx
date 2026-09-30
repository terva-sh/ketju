import { render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { CreatedPost, Post, RenderResult, ThreadPage, UserSuggestions } from '../api/dto';
import { composerFor, resetComposers, type Slow } from '../state/composer';
import { viewer } from '../state/viewer';
import { renderAt, threadPage, unmount } from '../testing';
import { Composer, mentionDelay, previewDelay, slowModeText, waitText } from './Composer';
import { draftDelay } from '../state/drafts';

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
    removal: null,
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
        '<details class="quote" open=""><summary>Quote from #1</summary>' +
          '<blockquote>\n<p class="quote-attribution"><a href="#post-1" class="quote-source" rel="nofollow">Post #1</a></p>\n<p>smaller half</p>\n</blockquote>\n</details>\n<p>Agreed.</p>\n<p>And another thing.</p>',
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

  it('opens again after Escape once the mention changes, even back to the same letters (review 557)', async () => {
    serve(page(), people);
    const root = await renderAt(path);
    await type(root, '@al');
    await wait(mentionDelay + 20);
    await key(root, 'Escape');
    expect(root.querySelector('[role=listbox]')).toBeNull();
    const a = area(root);
    a.value = '@a';
    a.setSelectionRange(2, 2);
    await act(() => void a.dispatchEvent(new Event('input', { bubbles: true })));
    await type(root, 'l');
    await wait(mentionDelay + 20);
    expect(root.querySelector('[role=listbox]')).not.toBeNull();
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

  it('offers nobody for a prefix the moment it changes, until its own answer comes (review 547)', async () => {
    serve(page(), people);
    const root = await renderAt(path);
    await type(root, '@al');
    await wait(mentionDelay + 20);
    expect(root.querySelector('[role=listbox]')).not.toBeNull();
    await type(root, 'i');
    expect(root.querySelector('[role=listbox]')).toBeNull();
    expect(area(root).getAttribute('aria-expanded')).toBe('false');
    await key(root, 'Enter');
    expect(area(root).value).toBe('@ali');
    await wait(mentionDelay + 20);
    expect(root.querySelector('[role=listbox]')).not.toBeNull();
  });

  it("looks up @alice- as alice, and replaces the '-' too (review 555)", async () => {
    const sent = serve(page(), people);
    const root = await renderAt(path);
    await type(root, 'hi @alice-');
    await wait(mentionDelay + 20);
    expect(sent.filter((s) => s.url.startsWith('/api/v1/users')).map((s) => s.url)).toEqual(['/api/v1/users?prefix=alice&limit=8']);
    await key(root, 'ArrowDown');
    await key(root, 'Enter');
    expect(area(root).value).toBe('hi @alice ');
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

describe('whose composer it is', () => {
  it("does not show one reader's unsent reply to the next who signs in (review 557)", async () => {
    serve(page());
    const me = viewer.value;
    const as = (handle: string) => ({ ...me, user: { ...me.user!, handle } });
    viewer.value = as('mara');
    try {
      const root = await renderAt(path);
      await type(root, 'private words');
      await click(root.querySelector('#post-2 [data-post-action=reply]'));
      expect(composerFor(thread.id).source.value).toBe('private words');

      await act(() => void (viewer.value = as('drew')));
      expect(area(root).value).toBe('');
      expect(root.querySelector('.reply-chip')).toBeNull();
      // Not kept aside for the first reader either: gone from memory.
      await act(() => void (viewer.value = as('mara')));
      expect(area(root).value).toBe('');
    } finally {
      viewer.value = me;
    }
  });
});

// TKT-01M3NS1931: the footer says how the autosaved draft stands.
describe('the draft', () => {
  it('says whether the draft is saving, saved, or not saved', async () => {
    let refuse = false;
    const drafts: string[] = [];
    vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
      if (url === `/api/v1/drafts/${thread.id}`) {
        const method = init.method ?? 'GET';
        drafts.push(method);
        if (method === 'GET') return new Response(null, { status: 204, headers: { 'Draft-Revision': '0' } });
        if (refuse) return new Response('{}', { status: 500, headers: { 'Content-Type': 'application/problem+json' } });
        const body = JSON.parse(String(init.body));
        return new Response(JSON.stringify({ thread_id: thread.id, ...body, updated_at: 0, revision: drafts.length }), { status: 200 });
      }
      if (url === '/api/v1/render') return new Response(JSON.stringify({ html: '', mentions: [], quotes: [], reply_to: null }));
      return new Response(JSON.stringify(page()), { status: 200 });
    });
    const root = await renderAt(path);
    const note = () => root.querySelector('.composer-foot [role=status]')!.textContent;
    expect(drafts).toEqual(['GET']);
    expect(note()).toBe('');
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    await type(root, 'Hello');
    expect(note()).toBe('Saving…');
    await act(async () => void (await vi.advanceTimersByTimeAsync(draftDelay)));
    expect(drafts).toEqual(['GET', 'PUT']);
    expect(note()).toBe('Draft saved');
    refuse = true;
    await type(root, ' again');
    await act(async () => void (await vi.advanceTimersByTimeAsync(draftDelay)));
    expect(note()).toBe('Draft not saved');
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

// TKT-01M3R0PM0K: slow mode in the composer. The countdown cases render the
// composer alone, so the clock and every timer are the fake ones from the
// start, and a page's slow block is whatever the test hands it.
describe('slow mode', () => {
  const T0 = Date.UTC(2026, 8, 30, 12);
  const day = 86_400_000;
  let host: HTMLElement | null = null;
  let posts: number;
  beforeEach(async () => {
    await unmount();
    posts = 0;
    vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
      if (url === '/api/v1/render') return new Response(JSON.stringify({ html: '', mentions: [], quotes: [], reply_to: null }));
      if (init.method === 'POST' && url.endsWith('/posts')) {
        posts++;
        const created: CreatedPost = { post: page().posts[0], ordinal: 4, revision: 1, log_length: 2000 };
        return new Response(JSON.stringify(created), { status: 201 });
      }
      throw new Error(`unexpected request ${url}`);
    });
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
    vi.setSystemTime(T0);
  });
  afterEach(async () => {
    if (host) await act(() => render(null, host!));
    host?.remove();
    host = null;
  });

  const slow = (o: Partial<Slow> = {}): Slow => ({ seconds: 30, nextPostAt: null, exempt: false, ...o });
  async function mount(s: Slow) {
    host ??= document.body.appendChild(document.createElement('div'));
    await act(() => render(<Composer thread={thread.id} authorOf={() => undefined} onPosted={() => {}} slow={s} />, host!));
  }
  const run = (ms: number) => act(async () => void (await vi.advanceTimersByTimeAsync(ms)));
  const draft = (text: string) => act(() => void (composerFor(thread.id).source.value = text));
  const send = () => host!.querySelector<HTMLButtonElement>('[data-post-reply]')!;
  const wait = () => host!.querySelector('[data-slow-wait]')?.textContent ?? null;

  it('shows the slow mode notice', async () => {
    await mount(slow({ seconds: 30 }));
    expect(host!.querySelector('[data-slow-mode]')?.textContent).toBe('Slow mode: one post every 30 seconds');
    await mount(slow({ seconds: 0 }));
    expect(host!.querySelector('[data-slow-mode]')).toBeNull();
    expect(
      [300, 60, 7200, 90, 3600, 1, 86400].map(slowModeText),
    ).toEqual([
      'Slow mode: one post every 5 minutes',
      'Slow mode: one post every minute',
      'Slow mode: one post every 2 hours',
      'Slow mode: one post every 90 seconds',
      'Slow mode: one post every hour',
      'Slow mode: one post every second',
      'Slow mode: one post every 24 hours',
    ]);
    expect([0, 4, 42, 60, 599, 3600, 3661, 3_456_000].map(waitText)).toEqual([
      '0:00', '0:04', '0:42', '1:00', '9:59', '1:00:00', '1:01:01', '960:00:00',
    ]);
  });

  it('holds Send until next_post_at', async () => {
    await mount(slow({ nextPostAt: T0 + 42_000 }));
    await draft('my reply');
    expect(send().disabled).toBe(true);
    expect(wait()).toBe('You can post again in 0:42');
    expect(send().getAttribute('aria-describedby')).toBe(host!.querySelector('[data-slow-wait]')!.id);
    await run(41_000);
    expect(send().disabled).toBe(true);
    expect(wait()).toBe('You can post again in 0:01');
    await run(1_000);
    expect(send().disabled).toBe(false);
    expect(send().hasAttribute('aria-describedby')).toBe(false);
    expect(wait()).toBeNull();
    // It came back on the clock alone: the page was not asked for again.
    expect(posts).toBe(0);
  });

  // Review 1564 on PR #211: a wait weeks long, from a huge site default,
  // neither fires at once, as one timer that long would, nor runs a timer a
  // second across the whole of it in the test.
  it('a far wait holds Send', async () => {
    const fake = globalThis.setTimeout;
    let fired = 0;
    globalThis.setTimeout = ((fn: () => void, ms?: number) =>
      fake(() => {
        fired++;
        fn();
      }, ms)) as typeof setTimeout;
    try {
      await mount(slow({ nextPostAt: T0 + 40 * day }));
      await draft('my reply');
      expect(send().disabled).toBe(true);
      expect(wait()).toBe('You can post again in 960:00:00');
      await run(3_000);
      expect(wait()).toBe('You can post again in 959:59:57');
      expect(send().disabled).toBe(true);

      vi.setSystemTime(T0 + 40 * day - 5_000);
      await run(1_000);
      expect(wait()).toBe('You can post again in 0:04');
      expect(send().disabled).toBe(true);
      await run(3_999);
      expect(send().disabled).toBe(true);
      await run(1);
      expect(Date.now()).toBe(T0 + 40 * day);
      expect(send().disabled).toBe(false);
      expect(wait()).toBeNull();
      expect(fired).toBeLessThanOrEqual(10);
    } finally {
      globalThis.setTimeout = fake;
    }
  });

  it('a post starts the wait', async () => {
    await mount(slow({ seconds: 30 }));
    await draft('first');
    expect(send().disabled).toBe(false);
    await act(() => send().click());
    await run(0);
    expect(posts).toBe(1);
    await draft('second');
    expect(send().disabled).toBe(true);
    expect(wait()).toBe('You can post again in 0:30');
    await run(29_999);
    expect(send().disabled).toBe(true);
    await run(1);
    expect(send().disabled).toBe(false);
  });

  it('a moderator is never held', async () => {
    await mount(slow({ seconds: 30, exempt: true }));
    await draft('first');
    await act(() => send().click());
    await run(0);
    expect(posts).toBe(1);
    await draft('second');
    expect(send().disabled).toBe(false);
    expect(wait()).toBeNull();
    // The notice still says the thread is in slow mode.
    expect(host!.querySelector('[data-slow-mode]')).not.toBeNull();
  });

  // Review 1540 on PR #203: the page may be one fetched before the viewer's
  // own post, so its next_post_at is later only when it is later.
  it('the page never shortens the wait', async () => {
    await mount(slow());
    await draft('first');
    await act(() => send().click());
    await run(0);
    await draft('second');
    expect(wait()).toBe('You can post again in 0:30');
    await mount(slow({ nextPostAt: T0 + 5_000 }));
    expect(wait()).toBe('You can post again in 0:30');
    await mount(slow({ nextPostAt: null }));
    expect(wait()).toBe('You can post again in 0:30');
    await run(29_999);
    expect(send().disabled).toBe(true);
    await run(1);
    expect(send().disabled).toBe(false);

    // A later one moves it.
    await mount(slow({ nextPostAt: Date.now() + 60_000 }));
    expect(send().disabled).toBe(true);
    expect(wait()).toBe('You can post again in 1:00');
    await mount(slow({ nextPostAt: null }));
    expect(send().disabled).toBe(true);
  });
});
