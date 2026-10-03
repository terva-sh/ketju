import { render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Forum, MovedPosts, ThreadHeader, ThreadPage } from '../../api/dto';
import { resetApiCache } from '../../api/client';
import { endSelection, isSelected, maxSelection, selection, toggleSelected } from '../../state/selection';
import { cachedThread, resetThreadCache } from '../../state/threads';
import { problemAnswer, stubServer, type Answer, type StubServer } from '../../testing';
import golden from '../../../../../internal/dto/testdata/golden/thread.member.json';
import { countText, SelectBox, SelectionBar, SplitDialog, splitRefusal } from './MovePosts';

// Selecting posts and splitting them into a new thread (TKT-01M29CYSAM;
// docs/06 "Moderator flow for drift", docs/07 "Moving posts"): the selection
// bar and the split dialog, against a stubbed server.

const source: ThreadHeader = (golden as ThreadPage).thread;
const url = `POST /api/v1/admin/threads/${source.id}/split`;
const forum = (slug: string, name: string, archived = false): Forum =>
  ({ slug, name, archived, children: [] }) as unknown as Forum;
const forums = [forum('general', 'General'), forum('meta', 'Meta'), forum('attic', 'Attic', true)];

let root: HTMLElement;
let title: HTMLButtonElement;
beforeEach(() => {
  root = document.createElement('div');
  title = document.createElement('button');
  title.textContent = 'title';
  document.body.replaceChildren(title, root);
  resetApiCache();
  resetThreadCache();
});
afterEach(async () => {
  await act(async () => render(null, root));
  vi.unstubAllGlobals();
});

const wait = () => act(() => new Promise((r) => setTimeout(r, 5)));
// press sends key from the focused element, as a key press does, so a
// listener in the capture phase hears it before one in the bubble phase.
const press = (key: string) =>
  act(async () => void (document.activeElement ?? document).dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })));

describe('the selection bar', () => {
  beforeEach(() => {
    endSelection();
    stubServer({ 'GET /api/v1/forums': () => ({ status: 200, body: { forums } }) });
  });
  async function bar(onDone = vi.fn(), onSplit = vi.fn()) {
    await act(async () =>
      render(
        <>
          <SelectBox thread={source.id} ordinal={61} />
          <SelectBox thread={source.id} ordinal={62} />
          <SelectionBar thread={source} admin={false} onDone={onDone} onSplit={onSplit} returnTo={() => title} />
        </>,
        root,
      ),
    );
    return { onDone, onSplit, b: root.querySelector<HTMLElement>('[data-selection-bar]')! };
  }
  const box = (o: number) => root.querySelector<HTMLInputElement>(`[data-select-post="${o}"]`)!;
  const count = () => root.querySelector('[data-selection-count]')!;

  it('counts the selection in a live region, and splits only a selection', async () => {
    const { b } = await bar();
    expect(count().textContent).toBe('0 posts selected');
    expect(count().getAttribute('aria-live')).toBe('polite');
    expect(b.querySelector<HTMLButtonElement>('.selection-split')!.disabled).toBe(true);
    expect(box(61).getAttribute('aria-label')).toBe('Select post #61');
    await act(async () => box(61).click());
    expect(count().textContent).toBe('1 post selected');
    await act(async () => box(62).click());
    expect(count().textContent).toBe('2 posts selected');
    expect(selection.value?.ordinals).toEqual([61, 62]);
    await act(async () => box(61).click());
    expect(count().textContent).toBe('1 post selected');
    expect(b.querySelector<HTMLButtonElement>('.selection-split')!.disabled).toBe(false);
    // Merge is TKT-01M29CYSBA's.
    expect([...b.querySelectorAll('button')].find((x) => x.textContent === 'Merge…')!.disabled).toBe(true);
  });

  it('hands Cancel and Escape to the page, and gives focus back when it goes', async () => {
    const { onDone } = await bar();
    await act(async () => box(61).click());
    await act(async () => [...root.querySelectorAll('button')].find((x) => x.textContent === 'Cancel')!.click());
    expect(onDone).toHaveBeenCalledTimes(1);
    await press('Escape');
    expect(onDone).toHaveBeenCalledTimes(2);
    // An open dialog takes Escape first.
    await act(async () => root.querySelector<HTMLButtonElement>('.selection-split')!.click());
    await wait();
    await press('Escape');
    expect(onDone).toHaveBeenCalledTimes(2);
    expect(root.querySelector('[data-split-dialog]')).toBeNull();
    // Gone with focus inside it, it gives focus to the title. The selection
    // is the page's to end, and outlives the bar.
    root.querySelector<HTMLButtonElement>('.selection-split')!.focus();
    await act(async () => render(null, root));
    expect(document.activeElement).toBe(title);
    expect(selection.value?.ordinals).toEqual([61]);
  });

  it('stops at the most one move takes', async () => {
    await bar();
    for (let o = 1; o <= maxSelection; o++) toggleSelected(source.id, o + 1000);
    await wait();
    expect(count().textContent).toBe(`${maxSelection} posts selected, the most one move takes`);
    expect(box(61).disabled).toBe(true);
    toggleSelected(source.id, 61);
    expect(isSelected(source.id, 61)).toBe(false);
    // Taking one out makes room again.
    toggleSelected(source.id, 1001);
    await wait();
    expect(box(61).disabled).toBe(false);
    expect(countText(1)).toBe('1 post selected');
  });
});

describe('the split dialog', () => {
  let server: StubServer;
  let answer: (b: unknown) => Answer;
  const onClose = vi.fn();
  const onSplit = vi.fn();
  const moved: MovedPosts = {
    source: { ...source, log_length: source.log_length + 100 },
    thread: { ...source, id: '3yymw47g00099', slug: 'split-out', title: 'Split out', post_count: 2 },
  };
  beforeEach(() => {
    onClose.mockReset();
    onSplit.mockReset();
    answer = () => ({ status: 200, body: moved });
    server = stubServer({
      [url]: (b) => answer(b),
      'GET /api/v1/forums': () => ({ status: 200, body: { forums } }),
    });
  });
  async function open(admin = false) {
    await act(async () =>
      render(
        <SplitDialog source={source} ordinals={[61, 62]} admin={admin} onClose={onClose} onSplit={onSplit} returnTo={() => title} />,
        root,
      ),
    );
    await wait();
    return root.querySelector<HTMLElement>('[role=dialog]')!;
  }
  async function type(d: HTMLElement, text: string) {
    const t = d.querySelector<HTMLInputElement>('input[type=text]')!;
    t.value = text;
    await act(async () => void t.dispatchEvent(new Event('input', { bubbles: true })));
  }
  const send = async (d: HTMLElement) => {
    await act(async () => d.querySelector<HTMLButtonElement>('.flag-send')!.click());
    await wait();
  };
  const splits = () => server.sent.filter((r) => r.method === 'POST');

  it('is modal, takes focus, and starts in the thread forum', async () => {
    const d = await open();
    expect(d.getAttribute('aria-modal')).toBe('true');
    expect(d.querySelector('h2')!.textContent).toBe('Split 2 posts into a new thread');
    expect(document.activeElement).toBe(d.querySelector('input[type=text]'));
    const select = d.querySelector<HTMLSelectElement>('select')!;
    expect(select.value).toBe(source.forum.slug);
    // An archived forum is offered to an admin alone.
    expect([...select.options].map((o) => o.value)).toEqual(['general', 'meta']);
    expect(d.textContent).toContain('The posts move to the new thread. To undo, merge them back.');
    // Tab stays inside.
    const last = d.querySelector<HTMLButtonElement>('.flag-send')!;
    await type(d, 'x');
    last.focus();
    await act(async () => void document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true })));
    expect(d.contains(document.activeElement)).toBe(true);
    // Escape closes it, and focus goes back.
    await press('Escape');
    expect(onClose).toHaveBeenCalled();
  });

  it('offers an archived forum to an admin', async () => {
    const d = await open(true);
    expect([...d.querySelector('select')!.options].map((o) => o.value)).toEqual(['general', 'meta', 'attic']);
  });

  it('splits, stores both headers, and goes to the new thread', async () => {
    const d = await open();
    expect(d.querySelector<HTMLButtonElement>('.flag-send')!.disabled).toBe(true);
    await type(d, '  Split out  ');
    const select = d.querySelector<HTMLSelectElement>('select')!;
    select.value = 'meta';
    await act(async () => void select.dispatchEvent(new Event('change', { bubbles: true })));
    await send(d);
    expect(splits()).toEqual([
      { method: 'POST', url: `/api/v1/admin/threads/${source.id}/split`, body: { ordinals: [61, 62], title: 'Split out', forum: 'meta' } },
    ]);
    expect(onSplit).toHaveBeenCalledWith(moved.thread);
    expect(cachedThread(source.id)?.header.log_length).toBe(moved.source.log_length);
    expect(cachedThread('3yymw47g00099')?.header.title).toBe('Split out');
  });

  // Terva review 2057: a dialog closed while its split was on the way takes
  // nobody to the new thread, but the caches still learn of the split.
  it('goes nowhere when closed before the split answers', async () => {
    let release = () => {};
    server.routes[url] = () => new Promise<Answer>((r) => (release = () => r({ status: 200, body: moved })));
    const d = await open();
    await type(d, 'Split out');
    await act(async () => d.querySelector<HTMLButtonElement>('.flag-send')!.click());
    await act(async () => render(null, root));
    release();
    await wait();
    expect(splits()).toHaveLength(1);
    expect(onSplit).not.toHaveBeenCalled();
    expect(cachedThread('3yymw47g00099')?.header.title).toBe('Split out');
  });

  it.each([
    ['conflict', 409, 'A post may have been removed or moved'],
    ['forbidden', 403, 'You cannot move posts into that forum.'],
    ['not-found', 404, 'That forum, or one of these posts, is no longer there.'],
    ['validation-failed', 422, 'Enter a title of 1 to 200 characters'],
  ])('says why a %s refusal was given, and keeps what was typed', async (type_, status, words) => {
    answer = () => problemAnswer(type_, status);
    const d = await open();
    await type(d, 'Split out');
    await send(d);
    expect(d.querySelector('[role=alert]')!.textContent).toContain(words);
    expect(d.querySelector<HTMLInputElement>('input[type=text]')!.value).toBe('Split out');
    expect(onSplit).not.toHaveBeenCalled();
    expect(splitRefusal(new Error('x'))).not.toBe('');
  });
});
