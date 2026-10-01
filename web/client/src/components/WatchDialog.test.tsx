import { render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { apiGet, peekGet } from '../api/client';
import type { Me } from '../api/dto';
import { viewer } from '../state/viewer';
import { problemAnswer, stubServer, type StubServer } from '../testing';
import { followChoice, watchLabel, WatchDialog, type WatchTarget } from './WatchDialog';
import memberMe from '../../../../internal/dto/testdata/golden/me.member.json';

// The watch dialog (TKT-01M3V6Y87D; docs/04 `watches`, docs/07 POST
// /threads/{id}/watch and POST /forums/{slug}/watch), against a stubbed
// server.

const thread = '3yymw47g00001';
const threadUrl = `POST /api/v1/threads/${thread}/watch`;
const forumUrl = 'POST /api/v1/forums/general/watch';

const saved = viewer.value;
let root: HTMLElement;
let opener: HTMLButtonElement;
let fallback: HTMLHeadingElement;
let onClose: ReturnType<typeof vi.fn<() => void>>;
let onSaved: ReturnType<typeof vi.fn<(a: unknown) => void>>;

beforeEach(() => {
  root = document.createElement('div');
  opener = document.createElement('button');
  opener.textContent = 'Watch';
  fallback = document.createElement('h1');
  fallback.tabIndex = -1;
  document.body.replaceChildren(fallback, opener, root);
  onClose = vi.fn<() => void>();
  onSaved = vi.fn<(a: unknown) => void>();
});
afterEach(async () => {
  await act(async () => render(null, root));
  viewer.value = saved;
  vi.unstubAllGlobals();
});

async function open(target: WatchTarget, level = '') {
  opener.focus();
  await act(async () =>
    render(
      <WatchDialog target={target} level={level} returnTo={opener} fallback={() => fallback} onSaved={onSaved} onClose={onClose} />,
      root,
    ),
  );
  return root.querySelector<HTMLElement>('[role=dialog]')!;
}
const onThread = (forumLevel = '') => ({ thread, forumLevel });
const onForum = { forum: 'general', name: 'General' };

const radios = (d: HTMLElement) => [...d.querySelectorAll<HTMLInputElement>('input[type=radio]')];
const labels = (d: HTMLElement) => [...d.querySelectorAll('label')].map((l) => l.textContent);
async function choose(d: HTMLElement, i: number) {
  await act(async () => radios(d)[i].click());
}
async function save(d: HTMLElement) {
  await act(async () => d.querySelector<HTMLButtonElement>('button[type=submit]')!.click());
  await act(() => new Promise((r) => setTimeout(r, 5)));
}
const posts = (s: StubServer) => s.sent.filter((r) => r.method === 'POST');

describe('the watch dialog', () => {
  it('posts each choice, the third as an empty level', async () => {
    for (const [i, level] of [
      [0, 'watch'],
      [1, 'mute'],
      [2, ''],
    ] as const) {
      const s = stubServer({ [threadUrl]: (b) => ({ status: 200, body: { watch: (b as { level: string }).level, forum_watch: '' } }) });
      // Open on another level than the one chosen, so the choice is a change.
      const d = await open(onThread(), i === 0 ? 'mute' : 'watch');
      await choose(d, i);
      await save(d);
      expect(posts(s).map((r) => [r.url, r.body])).toEqual([[`/api/v1/threads/${thread}/watch`, { level }]]);
      expect(onSaved).toHaveBeenLastCalledWith({ watch: level, forum_watch: '' });
      await act(async () => render(null, root));
    }
    expect(onClose).toHaveBeenCalledTimes(3);
  });

  it('opens on the thread’s own level', async () => {
    let d = await open(onThread('mute'), 'watch');
    expect(radios(d).map((r) => r.checked)).toEqual([true, false, false]);
    expect(document.activeElement).toBe(radios(d)[0]);
    await act(async () => render(null, root));
    d = await open(onThread('mute'), '');
    expect(radios(d).map((r) => r.checked)).toEqual([false, false, true]);
    expect(document.activeElement).toBe(radios(d)[2]);
  });

  // Review 1693: the third choice says what it will do. It reads Normal only
  // when the forum has no level.
  it('names the third choice by the forum’s level', async () => {
    let d = await open(onThread(''));
    expect(labels(d)).toEqual([
      'WatchAlert me to every new post.',
      'MuteNo alerts, and no unread count.',
      'NormalAlert me when someone mentions, replies to or quotes me.',
    ]);
    await act(async () => render(null, root));
    d = await open(onThread('watch'));
    expect(labels(d)[2]).toBe('Follow the forum (Watch)Alert me to every new post.');
    await act(async () => render(null, root));
    d = await open(onThread('mute'));
    expect(labels(d)[2]).toBe('Follow the forum (Mute)No alerts, and no unread count.');
    expect(followChoice(null).label).toBe('Normal');
  });

  it('sets a forum’s level, with no forum above it', async () => {
    const s = stubServer({ [forumUrl]: () => ({ status: 200, body: { watch: 'mute' } }) });
    const d = await open(onForum, 'watch');
    expect(d.querySelector('h2')?.textContent).toBe('Alerts for General');
    expect(labels(d)[2]).toBe('NormalAlert me when someone mentions, replies to or quotes me.');
    expect(d.textContent).toContain('not its subforums');
    await choose(d, 1);
    await save(d);
    expect(posts(s).map((r) => [r.url, r.body])).toEqual([['/api/v1/forums/general/watch', { level: 'mute' }]]);
    expect(onSaved).toHaveBeenCalledWith({ watch: 'mute' });
  });

  it('asks for /me again after a save, and drops the held counts', async () => {
    const me = structuredClone(memberMe) as Me;
    me.unread.threads = 0;
    const s = stubServer({
      [threadUrl]: () => ({ status: 200, body: { watch: 'mute', forum_watch: '' } }),
      'GET /api/v1/me': () => ({ status: 200, body: me }),
      'GET /api/v1/forums': () => ({ status: 200, body: { forums: [] }, headers: { ETag: '"f"' } }),
      [`GET /api/v1/threads/${thread}?page=2`]: () => ({ status: 200, body: {}, headers: { ETag: '"t"' } }),
      'GET /api/v1/threads/other': () => ({ status: 200, body: {}, headers: { ETag: '"o"' } }),
    });
    await apiGet('/api/v1/forums');
    await apiGet(`/api/v1/threads/${thread}?page=2`);
    await apiGet('/api/v1/threads/other');
    const d = await open(onThread(), 'watch');
    await choose(d, 1);
    await save(d);
    expect(s.sent.filter((r) => r.url === '/api/v1/me')).toHaveLength(1);
    expect(viewer.value.unread.threads).toBe(0);
    expect(peekGet('/api/v1/forums')).toBeUndefined();
    expect(peekGet(`/api/v1/threads/${thread}?page=2`)).toBeUndefined();
    // Another thread's level did not change.
    expect(peekGet('/api/v1/threads/other')).toBeDefined();
  });

  it('shows a refusal and stays open as chosen', async () => {
    const s = stubServer({ [threadUrl]: () => problemAnswer('forbidden', 403) });
    const d = await open(onThread());
    await choose(d, 0);
    await save(d);
    expect(d.isConnected).toBe(true);
    expect(d.querySelector('[role=alert]')).not.toBeNull();
    expect(radios(d)[0].checked).toBe(true);
    expect(onSaved).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
    expect(s.sent.filter((r) => r.url === '/api/v1/me')).toHaveLength(0);
  });

  it('takes an answer without a level as the server’s fault', async () => {
    stubServer({ [threadUrl]: () => ({ status: 200, body: {} }) });
    const d = await open(onThread());
    await save(d);
    expect(d.querySelector('[role=alert]')).not.toBeNull();
    expect(onSaved).not.toHaveBeenCalled();
  });

  it('keeps focus inside, and gives it back on close', async () => {
    const d = await open(onThread());
    const buttons = [...d.querySelectorAll('button')];
    buttons[buttons.length - 1].focus();
    await act(async () => void document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true })));
    expect(d.contains(document.activeElement)).toBe(true);
    await act(async () => void document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    expect(onClose).toHaveBeenCalledOnce();
    await act(async () => render(null, root));
    expect(document.activeElement).toBe(opener);
  });

  it('gives focus to the fallback when the opener has gone', async () => {
    await open(onThread());
    opener.remove();
    await act(async () => render(null, root));
    expect(document.activeElement).toBe(fallback);
  });

  it('labels the control by the level', () => {
    expect([watchLabel('watch'), watchLabel('mute'), watchLabel('')]).toEqual(['Watching', 'Muted', 'Watch']);
  });
});
