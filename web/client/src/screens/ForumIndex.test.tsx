import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ForumTree, Me } from '../api/dto';
import { viewer } from '../state/viewer';
import { renderAt, screenOf, settle } from '../testing';
import { render } from 'preact';
import { groups } from './ForumIndex';
import { ForumPicker } from './ForumPicker';
import anonMe from '../../../../internal/dto/testdata/golden/me.anon.json';
import memberMe from '../../../../internal/dto/testdata/golden/me.member.json';
import anonTree from '../../../../internal/dto/testdata/golden/forums.anon.json';
import memberTree from '../../../../internal/dto/testdata/golden/forums.member.json';

const saved = viewer.value;
beforeEach(() => {
  viewer.value = structuredClone(anonMe) as Me;
});
afterEach(() => {
  viewer.value = saved;
  vi.unstubAllGlobals();
});

function serve(tree: unknown) {
  vi.stubGlobal('fetch', async (url: string) => {
    if (String(url).startsWith('/api/v1/forums')) return new Response(JSON.stringify(tree), { status: 200 });
    return new Response('{}', { status: 404 });
  });
}

async function index(tree: unknown, me: unknown) {
  viewer.value = structuredClone(me) as Me;
  serve(tree);
  const root = await renderAt('/');
  expect(screenOf(root)).toBe('forum-index');
  return root;
}

const rowOf = (root: HTMLElement, slug: string) => root.querySelector<HTMLElement>(`[data-forum="${slug}"]`);

// AC1: the unread counts are the content for a signed-in reader, and are
// absent, not zero, for an anonymous one.
describe('unread counts', () => {
  it('render for a member', async () => {
    const root = await index(memberTree, memberMe);
    expect(rowOf(root, 'general')?.querySelector('[data-unread]')?.textContent).toBe('7 new');
    expect(rowOf(root, 'general-offtopic')?.querySelector('[data-unread]')?.textContent).toBe('2 new');
    // Nothing unread is a dash, not a zero.
    expect(rowOf(root, 'archive-2024')?.querySelector('[data-unread]')).toBeNull();
    expect(rowOf(root, 'archive-2024')?.querySelector('.forum-unread')?.textContent).toBe('—');
    expect(root.querySelector('.forum-cols')?.textContent).toContain('Unread');
    expect(root.querySelector('.forum-index-summary')?.textContent).toBe(`${memberMe.unread.threads} unread threads.`);
  });

  it('are absent for an anonymous reader', async () => {
    const root = await index(anonTree, anonMe);
    expect(root.querySelectorAll('[data-forum]').length).toBeGreaterThan(0);
    expect(root.querySelector('[data-unread]')).toBeNull();
    expect(root.querySelector('.forum-unread')).toBeNull();
    expect(root.querySelector('.forum-cols')?.textContent).not.toContain('Unread');
    expect(root.querySelector('.forum-index-summary')).toBeNull();
    expect(root.textContent).not.toMatch(/\b0 new\b/);
  });
});

// AC2: a forum with no threads, and a site with no forums, say so usably.
describe('empty states', () => {
  it('offers a member a way to start the first thread', async () => {
    const root = await index(memberTree, memberMe);
    const empty = rowOf(root, 'general-events')!.querySelector('[data-no-threads]')!;
    expect(empty.textContent).toContain('No threads yet.');
    expect(empty.querySelector('a')?.getAttribute('href')).toBe('/f/general-events/new');
  });

  it('offers an anonymous reader no link it cannot use', async () => {
    const root = await index(anonTree, anonMe);
    const empty = rowOf(root, 'general-events')!.querySelector('[data-no-threads]')!;
    expect(empty.textContent).toContain('No threads yet.');
    expect(empty.querySelector('a')).toBeNull();
  });

  it('offers no start link in an archived forum', async () => {
    const tree = structuredClone(memberTree) as ForumTree;
    tree.forums[1].last_post = null;
    tree.forums[1].thread_count = 0;
    const root = await index(tree, memberMe);
    expect(rowOf(root, 'archive-2024')!.querySelector('[data-no-threads] a')).toBeNull();
  });

  it('says there are no forums', async () => {
    const root = await index({ forums: [] }, memberMe);
    expect(root.querySelector('[data-empty-index]')?.textContent).toContain('no forums');
    expect(root.querySelector('[data-fab]')).toBeNull();
  });
});

describe('rows', () => {
  it('link the forum, its last thread, and mark a private one', async () => {
    const root = await index(memberTree, memberMe);
    const row = rowOf(root, 'general-offtopic')!;
    expect(row.querySelector('.forum-name')?.getAttribute('href')).toBe('/f/general-offtopic');
    const last = memberTree.forums[0].children.find((c) => c.slug === 'general-offtopic')!.last_post!;
    expect(row.querySelector('.forum-last-title')?.getAttribute('href')).toBe(`/t/${last.thread.id}/${last.thread.slug}`);
    expect(row.querySelector('.forum-last-by')?.textContent).toContain(last.author.handle);
    expect(row.querySelector('.forum-lock')?.textContent).toContain('Members only');
    expect(rowOf(root, 'general')!.querySelector('.forum-lock')).toBeNull();
    expect(rowOf(root, 'archive-2024')!.querySelector('.forum-archived')).not.toBeNull();
  });

  it('show each forum its thread and post totals, as the mockup does', async () => {
    const tree = structuredClone(memberTree) as ForumTree;
    tree.forums[1].thread_count = 1;
    tree.forums[1].post_count = 1;
    const root = await index(tree, memberMe);
    expect(rowOf(root, 'general')!.querySelector('.forum-activity')?.textContent).toBe('418 threads9637 posts');
    expect(rowOf(root, 'general-events')!.querySelector('.forum-posts')?.textContent).toBe('0 posts');
    expect(rowOf(root, 'archive-2024')!.querySelector('.forum-activity')?.textContent).toBe('1 thread1 post');
  });

  it('keep the server order: a root with children is a labelled group, itself first', () => {
    const got = groups(memberTree.forums as ForumTree['forums']).map((g) => [g.label ?? '', g.rows.map((f) => f.slug)]);
    expect(got).toEqual([
      ['General', ['general', 'general-events', 'general-offtopic']],
      ['', ['archive-2024']],
    ]);
    const plain = groups([
      { ...memberTree.forums[1], slug: 'a' },
      { ...memberTree.forums[1], slug: 'b' },
      memberTree.forums[0],
      { ...memberTree.forums[1], slug: 'c' },
    ] as ForumTree['forums']).map((g) => [g.label ?? '', g.rows.map((f) => f.slug)]);
    expect(plain).toEqual([
      ['', ['a', 'b']],
      ['General', ['general', 'general-events', 'general-offtopic']],
      ['', ['c']],
    ]);
  });

  it('report a failure with a retry', async () => {
    viewer.value = structuredClone(memberMe) as Me;
    let calls = 0;
    vi.stubGlobal('fetch', async () => {
      if (calls++ === 0) throw new TypeError('network down');
      return new Response(JSON.stringify(memberTree), { status: 200 });
    });
    const root = await renderAt('/');
    expect(root.querySelector('[role=alert]')?.textContent).toContain('forums could not be loaded');
    const retry = [...root.querySelectorAll('button')].find((b) => b.textContent === 'Retry')!;
    await act(async () => retry.click());
    await settle(root);
    expect(rowOf(root, 'general')).not.toBeNull();
  });
});

// The new-thread control opens the forum picker (settled in grooming), a
// modal dialog that gives focus back to what opened it.
describe('the forum picker', () => {
  it('is offered to a member only', async () => {
    let root = await index(anonTree, anonMe);
    expect(root.querySelector('[data-fab]')).toBeNull();
    expect(root.querySelector('.new-thread')).toBeNull();
    root = await index(memberTree, memberMe);
    expect(root.querySelector('[data-fab]')).not.toBeNull();
    expect(root.querySelector('.new-thread')).not.toBeNull();
  });

  it('is not offered when every forum is archived', async () => {
    const tree = structuredClone(memberTree) as ForumTree;
    for (const f of tree.forums) {
      f.archived = true;
      for (const c of f.children) c.archived = true;
    }
    const root = await index(tree, memberMe);
    expect(root.querySelector('[data-fab]')).toBeNull();
    expect(root.querySelector('.new-thread')).toBeNull();
  });

  it('lists the open forums, closes on Escape, and returns focus', async () => {
    const root = await index(memberTree, memberMe);
    const fab = root.querySelector<HTMLButtonElement>('[data-fab]')!;
    fab.focus();
    await act(async () => fab.click());
    const dialog = root.querySelector<HTMLElement>('[role=dialog]')!;
    expect(dialog.getAttribute('aria-modal')).toBe('true');
    const links = [...dialog.querySelectorAll('a')].map((a) => a.getAttribute('href'));
    // archive-2024 is archived: not a place to start a thread.
    expect(links).toEqual(['/f/general/new', '/f/general-events/new', '/f/general-offtopic/new']);
    expect(dialog.textContent).not.toContain('Archive 2024');
    expect(document.activeElement).toBe(dialog.querySelector('a'));
    await act(async () => {
      dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(root.querySelector('[role=dialog]')).toBeNull();
    expect(document.activeElement).toBe(fab);
  });

  // The picker is drawn on the microtask after the press that opens it, and
  // must take an Escape from then on, before a frame has passed
  // (TKT-01M3TS0ZV9). The presses are outside act, which would run the
  // effects and hide the race.
  it('closes on an Escape pressed straight after opening', async () => {
    const root = await index(memberTree, memberMe);
    const opener = root.querySelector<HTMLButtonElement>('.new-thread')!;
    opener.focus();
    try {
      opener.click();
      await Promise.resolve();
      expect(root.querySelector('[role=dialog]')).not.toBeNull();
      document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
      await Promise.resolve();
      expect(root.querySelector('[role=dialog]')).toBeNull();
      expect(document.activeElement).toBe(opener);
    } finally {
      await act(() => new Promise((r) => setTimeout(r, 50)));
    }
  });

  it('takes focus with nothing to offer, so Escape still closes it', async () => {
    const tree = structuredClone(memberTree) as ForumTree;
    for (const f of tree.forums) f.archived = true;
    tree.forums[0].children = [];
    const host = document.createElement('div');
    document.body.append(host);
    let closed = 0;
    await act(async () => render(<ForumPicker forums={tree.forums} onClose={() => closed++} />, host));
    const dialog = host.querySelector<HTMLElement>('[role=dialog]')!;
    expect(dialog.querySelector('a')).toBeNull();
    expect(document.activeElement).toBe(dialog.querySelector('button'));
    await act(async () => {
      document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(closed).toBe(1);
    await act(async () => render(null, host));
    host.remove();
  });

  it('keeps Tab inside', async () => {
    const root = await index(memberTree, memberMe);
    await act(async () => root.querySelector<HTMLButtonElement>('.new-thread')!.click());
    const dialog = root.querySelector<HTMLElement>('[role=dialog]')!;
    const last = [...dialog.querySelectorAll<HTMLElement>('a')].at(-1)!;
    last.focus();
    const e = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true });
    await act(async () => void dialog.dispatchEvent(e));
    expect(e.defaultPrevented).toBe(true);
    expect(dialog.contains(document.activeElement)).toBe(true);
  });
});
