import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me } from '../api/dto';
import { isPhone } from '../state/layout';
import { viewer } from '../state/viewer';
import { renderAt, screenOf, threadJSON } from '../testing';
import css from './shell.css?raw';
import { threadActions } from './threadActions';
import anon from '../../../../internal/dto/testdata/golden/me.anon.json';
import member from '../../../../internal/dto/testdata/golden/me.member.json';
import moderator from '../../../../internal/dto/testdata/golden/me.moderator.json';

const thread = { id: '01k4z3j0w8x2c', slug: 'a-thread', title: 'A thread' };

let saved: Me;
beforeEach(() => {
  saved = viewer.value;
  viewer.value = structuredClone(member) as Me;
  isPhone.value = false;
  // Nothing answers unless a test says what does.
  vi.stubGlobal('fetch', () => new Promise(() => {}));
});
afterEach(() => {
  viewer.value = saved;
  isPhone.value = false;
  vi.unstubAllGlobals();
});

const setPhone = (v: boolean) => act(() => void (isPhone.value = v));

// badgesOf reads each navigation entry's badge, by destination.
function badgesOf(root: HTMLElement): Record<string, string | null> {
  const out: Record<string, string | null> = {};
  for (const a of root.querySelectorAll<HTMLElement>('[data-nav]')) {
    out[a.dataset.nav!] = a.querySelector('[data-badge]')?.firstChild?.textContent ?? null;
  }
  return out;
}

const shellParts = (root: HTMLElement) =>
  [...root.querySelectorAll<HTMLElement>('[data-shell]')].map((e) => e.dataset.shell).filter((s) => s !== 'title');

describe('the navigation shell', () => {
  // AC1: the shell is a component choice at the breakpoint, and crossing it
  // keeps the address and the screen, element and all.
  it('swaps the bars at the phone breakpoint and nothing else', async () => {
    const root = await renderAt('/unread?x=1');
    expect(shellParts(root)).toEqual(['topbar']);
    const screen = root.querySelector('[data-screen]');

    await setPhone(true);
    expect(shellParts(root)).toEqual(['phone-topbar', 'tabbar']);
    expect(location.pathname + location.search).toBe('/unread?x=1');
    expect(root.querySelector('[data-screen]')).toBe(screen);

    await setPhone(false);
    expect(shellParts(root)).toEqual(['topbar']);
    expect(root.querySelector('[data-screen]')).toBe(screen);
    expect(screenOf(root)).toBe('unread');
  });

  // AC4: the counts /me provides, the same at both widths; none for New,
  // which /me has no count for.
  it('shows the same badges in both layouts', async () => {
    const root = await renderAt('/');
    const desktop = badgesOf(root);
    await setPhone(true);
    const phone = badgesOf(root);
    expect(desktop).toMatchObject({ unread: '12', new: null, alerts: '2', me: null });
    expect(phone).toEqual({ forums: null, unread: '12', new: null, alerts: '2', me: null });
    for (const k of Object.keys(phone)) if (k in desktop) expect(desktop[k]).toBe(phone[k]);
  });

  it('follows the viewer, and shows no badge for a count of none', async () => {
    const root = await renderAt('/');
    await act(() => void (viewer.value = { ...viewer.value, notifications: 0, unread: { ...viewer.value.unread, threads: 140 } }));
    expect(badgesOf(root)).toMatchObject({ unread: '99+', alerts: null });
    await setPhone(true);
    expect(badgesOf(root)).toMatchObject({ unread: '99+', alerts: null });
  });

  it('sends a signed-out reader to settings, with no badges', async () => {
    viewer.value = structuredClone(anon) as Me;
    const root = await renderAt('/');
    const me = root.querySelector<HTMLAnchorElement>('[data-nav=me]')!;
    expect(me.getAttribute('href')).toBe('/settings');
    expect(me.textContent).toBe('Sign in');
    expect(root.querySelectorAll('[data-badge]')).toHaveLength(0);
    await setPhone(true);
    expect(root.querySelector('[data-nav=me]')!.getAttribute('href')).toBe('/settings');
  });

  it('routes each tab to its screen', async () => {
    const root = await renderAt('/');
    await setPhone(true);
    const hrefs = [...root.querySelectorAll('.tabbar a')].map((a) => a.getAttribute('href'));
    expect(hrefs).toEqual(['/', '/unread', '/new', '/notifications', '/u/alice']);
    expect(root.querySelector('.tabbar [aria-current=page]')?.getAttribute('data-nav')).toBe('forums');
  });

  it('offers the moderation queue to a moderator only', async () => {
    let root = await renderAt('/');
    expect(root.querySelector('a[href="/admin/queue"]')).toBeNull();
    viewer.value = structuredClone(moderator) as Me;
    root = await renderAt('/');
    expect(root.querySelector('a[href="/admin/queue"]')).not.toBeNull();
  });

  it('marks the section the reader is in', async () => {
    const root = await renderAt('/t/01k4z3j0w8x2c/a-thread');
    expect(root.querySelector('.topbar [aria-current=page]')?.getAttribute('data-nav')).toBe('forums');
    const unread = await renderAt('/unread');
    expect(unread.querySelector('.topbar [aria-current=page]')?.getAttribute('data-nav')).toBe('unread');
  });
});

// TKT-01M29CGBXK: the skip link, and focus on a new screen.
describe('focus', () => {
  it('offers a skip link first, which moves focus to the heading and leaves the address', async () => {
    const root = await renderAt('/unread');
    const first = root.querySelector('a, button')!;
    expect(first.classList.contains('skip-link')).toBe(true);
    expect(first.textContent).toBe('Skip to content');
    expect(root.querySelector('#main')?.contains(root.querySelector('main'))).toBe(true);
    const before = location.href;
    await act(async () => (first as HTMLElement).click());
    expect(document.activeElement).toBe(root.querySelector('main h1'));
    expect(location.href).toBe(before);
    await setPhone(true);
    expect(root.querySelector('a, button')?.classList.contains('skip-link')).toBe(true);
  });

  it('moves focus to the new screen’s heading when the path changes', async () => {
    const root = await renderAt('/unread');
    const link = root.querySelector<HTMLElement>('[data-nav="new"]')!;
    link.focus();
    await act(async () => link.click());
    expect(screenOf(root)).toBe('new');
    expect(document.activeElement).toBe(root.querySelector('main h1'));
    expect(document.activeElement?.textContent).toBe(root.querySelector('[data-screen="new"] h1')?.textContent);
  });
});

describe('the phone top bar', () => {
  it("titles a screen with its name, and the index with the wordmark", async () => {
    const root = await renderAt('/unread');
    await setPhone(true);
    expect(root.querySelector('[data-shell=title]')?.textContent).toBe('Unread');
    const index = await renderAt('/');
    expect(index.querySelector('[data-shell=title]')?.textContent).toBe('Ketju');
  });

  it("titles a thread with its forum, with back to the forum", async () => {
    vi.stubGlobal('fetch', async () => new Response(threadJSON(thread), { status: 200 }));
    isPhone.value = true;
    const root = await renderAt('/t/01k4z3j0w8x2c/a-thread');
    const title = root.querySelector('[data-shell=title]')!;
    const back = root.querySelector<HTMLAnchorElement>('[aria-label^="Back to"]')!;
    expect(title.textContent).not.toBe('Thread');
    expect(back.getAttribute('aria-label')).toBe(`Back to ${title.textContent}`);
    expect(back.getAttribute('href')).toMatch(/^\/f\//);
  });

  // AC2: a long forum name is cut short on one line and leaves the controls
  // their room. The real widths are checked in a browser; this holds the rule
  // that does it.
  it('cuts a long title short on one line', () => {
    const rule = /\.topbar\.phone \.title \{([^}]*)\}/.exec(css)?.[1] ?? '';
    for (const decl of ['min-width: 0', 'overflow: hidden', 'white-space: nowrap', 'text-overflow: ellipsis']) {
      expect(rule).toContain(decl);
    }
    const button = /\.tb-button,\n\.tb-spacer \{([^}]*)\}/.exec(css)?.[1] ?? '';
    expect(button).toContain('flex: none');
  });

  // The desktop bar runs down to 720 px, where the mockup's spacing does not
  // fit (review 490). The widths are checked in a browser; this holds the
  // tablet rules that make them fit.
  it('tightens the desktop bar at tablet widths', () => {
    const tablet = css.split('@media (min-width: 720px) and (max-width: 1100px) {')[1] ?? '';
    expect(tablet).toMatch(/\.topbar\.desktop \{[^}]*gap: 16px/);
    expect(tablet).toMatch(/\.topbar \.search span \{[^}]*clip: rect/);
    // The links keep their width; the reader's handle is what gives way.
    expect(/\.topbar\.desktop nav \{([^}]*)\}/.exec(css)?.[1]).toContain('flex: none');
    expect(/\.topbar \.tools \{([^}]*)\}/.exec(css)?.[1]).toContain('min-width: 0');
    const who = /\.topbar \.who \{([^}]*)\}/.exec(css)?.[1] ?? '';
    for (const decl of ['min-width: 0', 'max-width: 16ch', 'overflow: hidden', 'white-space: nowrap', 'text-overflow: ellipsis']) {
      expect(who).toContain(decl);
    }
  });
});

describe('thread actions', () => {
  // AC3: the toolbar and the more menu render the one list.
  it('puts every toolbar action in the phone more menu', async () => {
    vi.stubGlobal('fetch', async () => new Response(threadJSON(thread), { status: 200 }));
    const root = await renderAt('/t/01k4z3j0w8x2c/a-thread');
    // The member golden may reply, so Reply leads the list, and has posts
    // its reader has not read, so First unread is offered.
    const want = threadActions(thread, { canReply: true, firstUnread: () => {} }).map((a) => a.id);
    expect(want).toEqual(['reply', 'first-unread', 'copy-link']);
    const toolbar = [...root.querySelectorAll<HTMLElement>('[data-shell=toolbar] [data-action]')].map((b) => b.dataset.action);
    expect(toolbar).toEqual(want);
    expect(root.querySelector('[aria-label=More]')).toBeNull();

    await setPhone(true);
    expect(root.querySelector('[data-shell=toolbar]')).toBeNull();
    const more = root.querySelector<HTMLButtonElement>('[aria-label=More]')!;
    await act(() => more.click());
    expect(more.getAttribute('aria-expanded')).toBe('true');
    const menu = [...root.querySelectorAll<HTMLElement>('[data-shell=more-menu] [data-action]')].map((b) => b.dataset.action);
    expect(menu).toEqual(toolbar);
  });

  it('runs an action from the menu, closes it, and gives focus back', async () => {
    vi.stubGlobal('fetch', async () => new Response(threadJSON(thread), { status: 200 }));
    const writeText = vi.fn(async () => {});
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } });
    isPhone.value = true;
    const root = await renderAt('/t/01k4z3j0w8x2c/a-thread?page=2');
    const more = root.querySelector<HTMLButtonElement>('[aria-label=More]')!;
    await act(() => more.click());
    await act(() => root.querySelector<HTMLButtonElement>('[data-action=copy-link]')!.click());
    expect(writeText).toHaveBeenCalledWith(`${location.origin}/t/01k4z3j0w8x2c/a-thread`);
    expect(root.querySelector('[data-shell=more-menu]')).toBeNull();
    expect(document.activeElement).toBe(more);
  });

  it('closes the menu on Escape and on a click elsewhere', async () => {
    vi.stubGlobal('fetch', async () => new Response(threadJSON(thread), { status: 200 }));
    isPhone.value = true;
    const root = await renderAt('/t/01k4z3j0w8x2c/a-thread');
    const more = root.querySelector<HTMLButtonElement>('[aria-label=More]')!;
    await act(() => more.click());
    expect(document.activeElement?.getAttribute('data-action')).toBe('reply');
    await act(() => void document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' })));
    expect(root.querySelector('[data-shell=more-menu]')).toBeNull();
    expect(document.activeElement).toBe(more);

    await act(() => more.click());
    expect(document.activeElement?.getAttribute('data-action')).toBe('reply');
    await act(() => void document.body.dispatchEvent(new Event('pointerdown', { bubbles: true })));
    expect(root.querySelector('[data-shell=more-menu]')).toBeNull();
    // Focus was on an item that is gone; it goes back to the button
    // (review 490).
    expect(document.activeElement).toBe(more);
  });

  it('leaves focus alone when a press outside finds it outside the menu', async () => {
    vi.stubGlobal('fetch', async () => new Response(threadJSON(thread), { status: 200 }));
    isPhone.value = true;
    const root = await renderAt('/t/01k4z3j0w8x2c/a-thread');
    const more = root.querySelector<HTMLButtonElement>('[aria-label=More]')!;
    await act(() => more.click());
    const search = root.querySelector<HTMLAnchorElement>('a[aria-label=Search]')!;
    search.focus();
    await act(() => void search.dispatchEvent(new Event('pointerdown', { bubbles: true })));
    expect(root.querySelector('[data-shell=more-menu]')).toBeNull();
    expect(document.activeElement).toBe(search);
  });

  // A thread that is not found is NotFound, and offers nothing to do with
  // it (review 490).
  it('offers no actions for a thread that is not found', async () => {
    vi.stubGlobal('fetch', async () =>
      new Response(JSON.stringify({ type: 'not-found', title: 't', status: 404 }), {
        status: 404,
        headers: { 'Content-Type': 'application/problem+json' },
      }),
    );
    isPhone.value = true;
    const root = await renderAt('/t/01k4z3j0w8x2c/a-thread');
    expect(screenOf(root)).toBe('not-found');
    expect(root.querySelector('[aria-label=More]')).toBeNull();
    expect(root.querySelector('[aria-label^="Back to"]')).toBeNull();
    expect(root.querySelector('[data-shell=title]')?.textContent).toBe('Ketju');
    await setPhone(false);
    expect(root.querySelector('[data-shell=toolbar]')).toBeNull();
  });

  it('offers no actions before the thread has been shown', async () => {
    isPhone.value = true;
    const root = await renderAt('/t/01k4z3j0w8x2c/a-thread');
    expect(screenOf(root)).toBe('thread');
    expect(root.querySelector('[aria-label=More]')).toBeNull();
    expect(root.querySelector('[data-shell=title]')?.textContent).toBe('Thread');
  });

  it('leaves a screen with no actions without a menu', async () => {
    isPhone.value = true;
    const root = await renderAt('/unread');
    expect(root.querySelector('[aria-label=More]')).toBeNull();
  });
});

describe('the shell context', () => {
  // A screen coming off the page takes its context down only if it is still
  // the one up: a route that mounts its successor before it unmounts (a lazy
  // screen) must not blank the bar the successor just set.
  it('is not taken down by a screen that has been superseded', async () => {
    const { render } = await import('preact');
    const { shellContext, useShellContext } = await import('../state/shell');
    const Screen = ({ title }: { title: string }) => (useShellContext({ title, actions: [] }, [title]), null);
    const a = document.createElement('div');
    const b = document.createElement('div');
    await act(() => render(<Screen title="A" />, a));
    await act(() => render(<Screen title="B" />, b));
    await act(() => render(null, a));
    expect(shellContext.value?.title).toBe('B');
    await act(() => render(null, b));
    expect(shellContext.value).toBeNull();
  });
});

describe('the toolbar', () => {
  it('renders nothing for no actions', async () => {
    const { render } = await import('preact');
    const { Toolbar } = await import('./Toolbar');
    const root = document.createElement('div');
    await act(() => render(<Toolbar actions={[]} label="x" />, root));
    expect(root.querySelector('[role=toolbar]')).toBeNull();
  });
});

describe('the current section', () => {
  it('is a whole path segment, not a prefix of one', async () => {
    const { current } = await import('./nav');
    expect(current('/u/alice', '/u/alice/posts')).toBe(true);
    expect(current('/u/alice', '/u/alicebob')).toBe(false);
    expect(current('/new', '/newsletter')).toBe(false);
  });
});
