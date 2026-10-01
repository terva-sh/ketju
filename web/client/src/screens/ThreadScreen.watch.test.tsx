import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me, ThreadPage } from '../api/dto';
import { isPhone } from '../state/layout';
import { viewer } from '../state/viewer';
import { FakeEventSource, renderAt, settle, stubServer, threadPage, unmount, type StubServer } from '../testing';
import member from '../../../../internal/dto/testdata/golden/me.member.json';

// The watch control on the thread page (TKT-01M3V6Y87D): the desktop
// toolbar and the phone's more menu carry it through threadActions, for a
// signed-in member, labelled with the thread's effective level (docs/04
// `watches`).

const saved = viewer.value;
const id = '01k4z3j0w8x2c';
const url = `/api/v1/threads/${id}`;
let page: ThreadPage;
let server: StubServer;

function serve(watch: string, forumWatch: string) {
  page = threadPage({ id, slug: 'a', title: 'Watched' });
  page.viewer = { ...page.viewer, watch, forum_watch: forumWatch };
  server = stubServer({
    [`GET ${url}`]: () => ({ status: 200, body: page }),
    [`POST ${url}/watch`]: (b) => ({ status: 200, body: { watch: (b as { level: string }).level, forum_watch: forumWatch } }),
    'GET /api/v1/me': () => ({ status: 200, body: member }),
  });
}

beforeEach(() => {
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  viewer.value = structuredClone(member) as Me;
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  isPhone.value = false;
  vi.unstubAllGlobals();
});

const control = (root: HTMLElement) => root.querySelector<HTMLButtonElement>('[data-shell=toolbar] [data-action=watch]');
const dialog = () => document.querySelector<HTMLElement>('[data-watch-dialog]');
const wait = () => act(() => new Promise((r) => setTimeout(r, 5)));
async function press(el: Element | null) {
  expect(el).not.toBeNull();
  await act(async () => (el as HTMLElement).click());
  await wait();
}
async function choose(label: string) {
  const choice = [...dialog()!.querySelectorAll('label')].find((l) => l.querySelector('.watch-label')?.textContent === label);
  expect(choice, label).toBeDefined();
  await press(choice!.querySelector('input'));
}

describe('the watch control on the thread page', () => {
  it('is labelled with the effective level, the thread’s own before its forum’s', async () => {
    for (const [watch, forumWatch, label] of [
      ['watch', '', 'Watching'],
      ['mute', 'watch', 'Muted'],
      ['watch', 'mute', 'Watching'],
      // Inherited from a muted forum.
      ['', 'mute', 'Muted'],
      ['', 'watch', 'Watching'],
      ['', '', 'Watch'],
    ]) {
      serve(watch, forumWatch);
      const root = await renderAt(`/t/${id}/a`);
      expect(control(root)?.textContent, `${watch}/${forumWatch}`).toBe(label);
    }
  });

  it('is not offered to an anonymous reader, at either width', async () => {
    serve('', '');
    viewer.value = { ...structuredClone(member), user: null } as Me;
    const root = await renderAt(`/t/${id}/a`);
    expect(root.querySelector('[data-shell=toolbar] [data-action=copy-link]')).not.toBeNull();
    expect(root.querySelector('[data-action=watch]')).toBeNull();
    isPhone.value = true;
    await settle(root);
    await press(root.querySelector('[aria-label=More]'));
    expect(root.querySelector('[data-shell=more-menu] [data-action=copy-link]')).not.toBeNull();
    expect(root.querySelector('[data-action=watch]')).toBeNull();
  });

  it('sets Mute from the desktop toolbar, shows it, and asks for /me again', async () => {
    serve('', '');
    const root = await renderAt(`/t/${id}/a`);
    const button = control(root)!;
    button.focus();
    await press(button);
    expect(dialog()?.querySelector('h2')?.textContent).toBe('Alerts for this thread');
    const mes = server.sent.filter((r) => r.url === '/api/v1/me').length;
    await choose('Mute');
    await press(dialog()!.querySelector('button[type=submit]'));
    expect(server.sent.filter((r) => r.method === 'POST').map((r) => [r.url, r.body])).toEqual([[`${url}/watch`, { level: 'mute' }]]);
    expect(dialog()).toBeNull();
    expect(control(root)?.textContent).toBe('Muted');
    expect(document.activeElement).toBe(control(root));
    expect(server.sent.filter((r) => r.url === '/api/v1/me').length).toBe(mes + 1);
  });

  it('follows the forum again from the phone’s more menu', async () => {
    serve('mute', 'watch');
    isPhone.value = true;
    const root = await renderAt(`/t/${id}/a`);
    const more = root.querySelector<HTMLButtonElement>('[aria-label=More]')!;
    await press(more);
    const item = root.querySelector('[data-shell=more-menu] [data-action=watch]');
    expect(item?.textContent).toBe('Muted');
    await press(item);
    expect(root.querySelector('[data-shell=more-menu]')).toBeNull();
    // The thread's own Mute is checked; the third choice names the forum's.
    expect(dialog()!.querySelector<HTMLInputElement>('input:checked')?.value).toBe('mute');
    await choose('Follow the forum (Watch)');
    await press(dialog()!.querySelector('button[type=submit]'));
    expect(server.sent.filter((r) => r.method === 'POST').map((r) => r.body)).toEqual([{ level: '' }]);
    expect(dialog()).toBeNull();
    expect(document.activeElement).toBe(more);
    await press(more);
    expect(root.querySelector('[data-shell=more-menu] [data-action=watch]')?.textContent).toBe('Watching');
  });

  // Terva review 1822: a page asked for before a save that lands after it
  // reports the level from before. It is not stored and is asked for again,
  // so the screen shows the level the save set.
  it('does not let a page asked for before a save put back the old level', async () => {
    serve('', '');
    const root = await renderAt(`/t/${id}/a`);
    const longer = (watch: string): ThreadPage => ({
      ...page,
      thread: { ...page.thread, log_length: page.thread.log_length + 100 },
      viewer: { ...page.viewer, watch },
    });
    let first: ((a: { status: number; body: ThreadPage }) => void) | null = null;
    let asked = 0;
    server.routes[`GET ${url}?page=${page.page.number}`] = () => {
      if (asked++ === 0) return new Promise((resolve) => (first = resolve));
      return { status: 200, body: longer('mute') };
    };
    // A live hint asks for the page; its answer is held back.
    await act(async () => FakeEventSource.open()[0].emit('thread', { thread_id: id, log_length: page.thread.log_length + 100 }));
    await wait();
    expect(asked).toBe(1);
    await press(control(root));
    await choose('Mute');
    await press(dialog()!.querySelector('button[type=submit]'));
    expect(control(root)?.textContent).toBe('Muted');
    // The held-back answer, from before the save, lands now.
    await act(async () => first!({ status: 200, body: longer('') }));
    await settle(root);
    expect(asked).toBe(2);
    expect(control(root)?.textContent).toBe('Muted');
  });

  // Terva review 1820: a save outlives a dialog dismissed while it is out.
  // Its answer neither closes a dialog opened since nor, once a later save
  // has started, puts back its own level.
  it('keeps a late answer from closing a newer dialog or undoing a newer save', async () => {
    serve('', '');
    const pending: ((level: string) => void)[] = [];
    server.routes[`POST ${url}/watch`] = () =>
      new Promise((resolve) => pending.push((level) => resolve({ status: 200, body: { watch: level, forum_watch: '' } })));
    const root = await renderAt(`/t/${id}/a`);
    const escape = () => act(async () => void document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    const save = async (label: string) => {
      await press(control(root));
      await choose(label);
      await press(dialog()!.querySelector('button[type=submit]'));
    };

    // Mute is sent, and the dialog dismissed before it answers.
    await save('Mute');
    await escape();
    expect(dialog()).toBeNull();
    // Opened again, its answer lands: the open dialog stays, and the level
    // it set shows, since nothing later was sent.
    await press(control(root));
    expect(dialog()).not.toBeNull();
    await act(async () => pending[0]('mute'));
    await wait();
    expect(dialog()).not.toBeNull();
    expect(control(root)?.textContent).toBe('Muted');
    await escape();

    // Watch is sent and dismissed, then Normal is sent and answers first.
    await save('Watch');
    await escape();
    await save('Normal');
    await act(async () => pending[2](''));
    await wait();
    expect(dialog()).toBeNull();
    expect(control(root)?.textContent).toBe('Watch');
    // Watch's answer, landing last, leaves the newer level shown.
    await act(async () => pending[1]('watch'));
    await wait();
    expect(control(root)?.textContent).toBe('Watch');
    expect(server.sent.filter((r) => r.method === 'POST').map((r) => r.body)).toEqual([
      { level: 'mute' },
      { level: 'watch' },
      { level: '' },
    ]);
  });
});
