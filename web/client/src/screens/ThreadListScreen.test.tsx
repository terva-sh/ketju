import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me, ThreadList } from '../api/dto';
import { rememberScroll, savedScroll } from '../state/scroll';
import { viewer } from '../state/viewer';
import { renderAt, route, screenOf, settle } from '../testing';
import anonMe from '../../../../internal/dto/testdata/golden/me.anon.json';
import memberMe from '../../../../internal/dto/testdata/golden/me.member.json';
import anonList from '../../../../internal/dto/testdata/golden/threads.anon.json';
import memberList from '../../../../internal/dto/testdata/golden/threads.member.json';

const saved = viewer.value;
let asked: string[] = [];
beforeEach(() => {
  asked = [];
});
afterEach(() => {
  viewer.value = saved;
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function serve(list: unknown) {
  vi.stubGlobal('fetch', async (url: string) => {
    asked.push(String(url));
    if (String(url).startsWith('/api/v1/forums/general/threads')) {
      return new Response(JSON.stringify(list), { status: 200 });
    }
    return new Response(JSON.stringify({ type: 'not-found', title: 't', status: 404 }), {
      status: 404,
      headers: { 'Content-Type': 'application/problem+json' },
    });
  });
}

async function listAt(url: string, list: unknown, me: unknown) {
  viewer.value = structuredClone(me) as Me;
  serve(list);
  const root = await renderAt(url);
  expect(screenOf(root)).toBe('thread-list');
  return root;
}

const row = (root: HTMLElement, id: string) => root.querySelector<HTMLElement>(`[data-thread="${id}"]`)!;

// AC1: the badge and the title are two controls going to two places.
describe('links', () => {
  it('send the title to page 1 and the badge to the first unread post', async () => {
    const root = await listAt('/f/general', memberList, memberMe);
    const first = row(root, '3yymw47g00001');
    expect(first.querySelector('.thread-title')?.getAttribute('href')).toBe('/t/3yymw47g00001/why-we-left-slack?page=1');
    // last_read_ordinal 200: the badge is the permalink to post 201.
    expect(first.querySelector('[data-unread]')?.getAttribute('href')).toBe('/t/3yymw47g00001/why-we-left-slack/201');
    expect(first.querySelector('[data-unread]')?.textContent).toBe('12 new');
    // No read position: the thread's own address, where the server lands
    // the reader on their first unread post.
    const second = row(root, '3yymw47g00002');
    expect(second.querySelector('[data-unread]')?.getAttribute('href')).toBe('/t/3yymw47g00002/weekly-standup-notes');
    // Siblings, never one inside the other.
    expect(first.querySelector('.thread-title [data-unread], [data-unread] .thread-title')).toBeNull();
  });

  it('show an anonymous reader no badge', async () => {
    const root = await listAt('/f/general', anonList, anonMe);
    expect(root.querySelectorAll('[data-thread]').length).toBe(2);
    expect(root.querySelector('[data-unread]')).toBeNull();
    expect(root.querySelector('.new-thread')).toBeNull();
  });

  it('show no badge to an anonymous reader even if a row has a count, nor for a read thread', async () => {
    let root = await listAt('/f/general', memberList, anonMe);
    expect(root.querySelector('[data-unread]')).toBeNull();
    const list = structuredClone(memberList) as ThreadList;
    list.threads[0].unread = 0;
    root = await listAt('/f/general', list, memberMe);
    expect(row(root, '3yymw47g00001').querySelector('[data-unread]')).toBeNull();
    expect(row(root, '3yymw47g00002').querySelector('[data-unread]')).not.toBeNull();
  });
});

// AC2: pins come first, as the server sends them, and say so in words.
describe('pins', () => {
  it('are first, tinted and labelled', async () => {
    const root = await listAt('/f/general', memberList, memberMe);
    const rows = [...root.querySelectorAll<HTMLElement>('[data-thread]')];
    expect(rows.map((r) => r.dataset.thread)).toEqual(['3yymw47g00001', '3yymw47g00002']);
    expect(rows[0].classList.contains('row-pinned')).toBe(true);
    expect(rows[0].querySelector('.thread-flag.pinned')?.textContent).toBe('Pinned');
    expect(rows[1].classList.contains('row-pinned')).toBe(false);
    expect(rows[1].querySelector('.thread-flag.pinned')).toBeNull();
    // The second row is locked: an icon with its word for a screen reader.
    expect(rows[1].querySelector('.thread-flag.locked')?.textContent).toBe('Locked');
  });

  it('keep the server order even when it is not by time', async () => {
    const list = structuredClone(memberList) as ThreadList;
    list.threads.reverse();
    const root = await listAt('/f/general', list, memberMe);
    expect([...root.querySelectorAll<HTMLElement>('[data-thread]')].map((r) => r.dataset.thread)).toEqual([
      '3yymw47g00002',
      '3yymw47g00001',
    ]);
  });
});

describe('rows', () => {
  it('carry tags, author, replies and the last poster', async () => {
    const root = await listAt('/f/general', memberList, memberMe);
    const first = row(root, '3yymw47g00001');
    expect([...first.querySelectorAll('.thread-tags li')].map((l) => l.textContent)).toEqual(['meta', 'tools']);
    const meta = first.querySelector('.thread-row-meta')!.textContent!;
    expect(meta).toContain('by alice');
    expect(meta).toContain('211 replies');
    expect(meta).toContain('last bob');
    expect(row(root, '3yymw47g00002').querySelector('.thread-tags')).toBeNull();
  });

  it('count one reply in the singular', async () => {
    const list = structuredClone(memberList) as ThreadList;
    list.threads[0].reply_count = 1;
    list.threads[1].reply_count = 0;
    const root = await listAt('/f/general', list, memberMe);
    expect(row(root, '3yymw47g00001').querySelector('.thread-row-meta')!.textContent).toContain('1 reply');
    expect(row(root, '3yymw47g00001').querySelector('.thread-row-meta')!.textContent).not.toContain('replies');
    expect(row(root, '3yymw47g00002').querySelector('.thread-row-meta')!.textContent).toContain('0 replies');
  });
});

describe('paging and sort', () => {
  it('asks for the page in the address and links the next one', async () => {
    const root = await listAt('/f/general', memberList, memberMe);
    expect(asked).toContain('/api/v1/forums/general/threads');
    const older = root.querySelector('[data-older]')!;
    expect(older.getAttribute('href')).toBe(`/f/general?cursor=${memberList.next_cursor}`);
    // On the first page there is no way back to it.
    expect([...root.querySelectorAll('.thread-list-pages a')].map((a) => a.textContent)).toEqual(['Older threads']);
  });

  it('passes the cursor and sort through, and offers the newest page', async () => {
    const list = { ...memberList, next_cursor: '' };
    const root = await listAt('/f/general?sort=created&cursor=abc', list, memberMe);
    expect(asked).toContain('/api/v1/forums/general/threads?cursor=abc&sort=created');
    const pages = [...root.querySelectorAll<HTMLAnchorElement>('.thread-list-pages a')];
    expect(pages.map((a) => [a.textContent, a.getAttribute('href')])).toEqual([['Newest', '/f/general?sort=created']]);
    expect(root.querySelector('.thread-list-sort [aria-current=page]')?.textContent).toBe('Newest threads');
  });

  it('says a forum with no threads has none', async () => {
    const root = await listAt('/f/general', { ...memberList, threads: [], next_cursor: '' }, memberMe);
    expect(root.querySelector('[data-empty-list]')?.textContent).toBe('No threads here yet.');
  });

  it('says a page past the end has no more', async () => {
    const root = await listAt('/f/general?cursor=abc', { ...memberList, threads: [], next_cursor: '' }, memberMe);
    expect(root.querySelector('[data-empty-list]')?.textContent).toBe('No more threads.');
  });

  it('answers a forum it cannot find with NotFound', async () => {
    viewer.value = structuredClone(memberMe) as Me;
    serve(memberList);
    const root = await renderAt('/f/elsewhere');
    expect(screenOf(root)).toBe('not-found');
  });
});

// AC4's mechanism: the position is kept in the history entry and put back
// when the entry is shown again.
describe('scroll memory', () => {
  it('keeps the position in history.state beside what is there', () => {
    history.replaceState({ other: 1 }, '');
    vi.spyOn(window, 'scrollY', 'get').mockReturnValue(812.4);
    rememberScroll();
    expect(history.state).toEqual({ other: 1, scrollY: 812 });
    expect(savedScroll()).toBe(812);
    history.replaceState({ scrollY: 'x' }, '');
    expect(savedScroll()).toBeNull();
    history.replaceState(null, '');
    expect(savedScroll()).toBeNull();
    history.replaceState({ scrollY: -5 }, '');
    expect(savedScroll()).toBeNull();
    history.replaceState({ scrollY: Infinity }, '');
    expect(savedScroll()).toBeNull();
    history.replaceState({ scrollY: 0 }, '');
    expect(savedScroll()).toBe(0);
  });

  it('saves once the reader stops scrolling', async () => {
    const root = await listAt('/f/general', memberList, memberMe);
    vi.useFakeTimers();
    try {
      history.replaceState(null, '');
      vi.spyOn(window, 'scrollY', 'get').mockReturnValue(300);
      dispatchEvent(new Event('scroll'));
      vi.advanceTimersByTime(50);
      expect(history.state).toBeNull();
      vi.advanceTimersByTime(60);
      expect(history.state).toEqual({ scrollY: 300 });
    } finally {
      vi.useRealTimers();
    }
    expect(root).toBeTruthy();
  });

  it('waits for the list before restoring', async () => {
    const at: number[] = [];
    vi.spyOn(window, 'scrollTo').mockImplementation(() => void at.push(document.querySelectorAll('[data-thread]').length));
    viewer.value = structuredClone(memberMe) as Me;
    let answer!: () => void;
    vi.stubGlobal('fetch', () => new Promise<Response>((r) => (answer = () => r(new Response(JSON.stringify(memberList))))));
    const root = await renderAt('/f/general');
    expect(at).toEqual([]);
    await act(async () => answer());
    await settle(root);
    expect(at).toEqual([2]);
  });

  it('restores a saved position, and starts a new page at the top', async () => {
    const to = vi.spyOn(window, 'scrollTo').mockImplementation(() => {});
    viewer.value = structuredClone(memberMe) as Me;
    serve(memberList);
    history.replaceState({ scrollY: 640 }, '', '/f/general');
    const root = document.createElement('div');
    // renderAt replaces the state; render with the entry as it is instead.
    const { render } = await import('preact');
    const { App } = await import('../app');
    const { resetApiCache } = await import('../api/client');
    resetApiCache();
    document.body.replaceChildren(root);
    await act(async () => render(<App />, root));
    await settle(root);
    expect(to).toHaveBeenLastCalledWith(0, 640);

    to.mockClear();
    await route(`/f/general?cursor=${memberList.next_cursor}`);
    await settle(root);
    expect(to).toHaveBeenLastCalledWith(0, 0);
    await act(async () => render(null, root));
  });

  it('saves before a link is followed, so the position is the page it was on', async () => {
    const root = await listAt('/f/general', memberList, memberMe);
    vi.spyOn(window, 'scrollY', 'get').mockReturnValue(420);
    const entry = history.length;
    const link = root.querySelector<HTMLAnchorElement>('[data-older]')!;
    // The capture-phase listener writes the position into the entry that
    // is current when the click starts, before the router pushes.
    let stateAtClick: unknown;
    link.addEventListener('click', () => (stateAtClick = history.state), { once: true });
    await act(async () => link.click());
    expect(stateAtClick).toMatchObject({ scrollY: 420 });
    expect(history.length).toBeGreaterThanOrEqual(entry);
  });
});
