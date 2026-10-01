import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me, Notification, NotificationList } from '../api/dto';
import { isPhone } from '../state/layout';
import { shellContext } from '../state/shell';
import { viewer } from '../state/viewer';
import { problemAnswer, renderAt, screenOf, stubServer, type StubServer } from '../testing';
import { emptyAlerts } from './NotificationsScreen';
import memberMe from '../../../../internal/dto/testdata/golden/me.member.json';
import member from '../../../../internal/dto/testdata/golden/notifications.member.json';
import moderator from '../../../../internal/dto/testdata/golden/notifications.moderator.json';

const list = '/api/v1/notifications';
const read = 'POST /api/v1/notifications/read';

let saved: Me;
beforeEach(() => {
  saved = viewer.value;
  viewer.value = structuredClone(memberMe) as Me;
  isPhone.value = false;
});
afterEach(() => {
  viewer.value = saved;
  isPhone.value = false;
  vi.unstubAllGlobals();
});

const flush = () => act(() => new Promise((r) => setTimeout(r, 5)));

// meWith is /me as the server answers it once count unread alerts are left.
const meWith = (count: number) => ({ status: 200, body: { ...structuredClone(memberMe), notifications: count } });

// serve answers the list's pages, a mark, and /me after it with left
// unread alerts.
function serve(pages: Record<string, unknown>, left = 0): StubServer {
  const routes: StubServer['routes'] = {
    [read]: (body) => ({ status: 200, body: { marked: ((body as { ids?: number[] }).ids ?? [1]).length } }),
    'GET /api/v1/me': () => meWith(left),
  };
  for (const [url, page] of Object.entries(pages)) routes[`GET ${url}`] = () => ({ status: 200, body: page });
  return stubServer(routes);
}

const rows = (root: HTMLElement) => [...root.querySelectorAll<HTMLElement>('[data-alert]')];
const row = (root: HTMLElement, id: number) => root.querySelector<HTMLElement>(`[data-alert="${id}"]`)!;
const marks = (s: StubServer) => s.sent.filter((r) => `${r.method} ${r.url}` === read).map((r) => r.body);

// note is a row of kind, about the golden's thread, post 6, unless over says
// otherwise.
function note(id: number, kind: string, over: Partial<Notification> = {}): Notification {
  return { ...structuredClone(member.notifications[0]), id, kind, read: false, ...over } as Notification;
}

const page = (notifications: Notification[], next_cursor = ''): NotificationList => ({ notifications, next_cursor });

describe('the alerts screen', () => {
  it('is the Alerts screen, titled for the phone bar too', async () => {
    serve({ [list]: page([]) });
    const root = await renderAt('/notifications');
    expect(screenOf(root)).toBe('notifications');
    expect(root.querySelector('h1')?.textContent).toBe('Alerts');
    expect(shellContext.value?.title).toBe('Alerts');
  });

  // AC1: each kind is a sentence, and the thread's title links to the post
  // that caused it, as a permalink the server resolves to its page.
  it('says what each kind of alert is, and links it to its post', async () => {
    const app = { thread: null, forum: null, ordinal: null };
    serve({
      [list]: page([
        note(6, 'mention'),
        note(5, 'reply_to_you', { ordinal: 7 }),
        note(4, 'quoted', { ordinal: 8 }),
        note(3, 'thread_you_watch', { ordinal: 9 }),
        note(2, 'application_decided', { ...app }),
        note(1, 'new_application', { ...app, actor: null }),
      ]),
    });
    const root = await renderAt('/notifications');
    const what = (id: number) => row(root, id).querySelector('.alert-what')!.textContent!.replace(/^Unread: /, '');
    expect(what(6)).toBe('Alice Nakamura mentioned you in Why we left Slack');
    expect(what(5)).toBe('Alice Nakamura replied to you in Why we left Slack');
    expect(what(4)).toBe('Alice Nakamura quoted you in Why we left Slack');
    expect(what(3)).toBe('Alice Nakamura posted in Why we left Slack, which you watch');
    expect(what(2)).toBe('Your application was approved');
    expect(what(1)).toBe('A new application is waiting');

    const href = (id: number) => row(root, id).querySelector('a')?.getAttribute('href') ?? null;
    expect(href(6)).toBe('/t/3yymw47g00001/why-we-left-slack/6');
    expect(href(5)).toBe('/t/3yymw47g00001/why-we-left-slack/7');
    expect(href(4)).toBe('/t/3yymw47g00001/why-we-left-slack/8');
    expect(href(3)).toBe('/t/3yymw47g00001/why-we-left-slack/9');
    expect(href(2)).toBeNull();
    expect(href(1)).toBe('/admin/applications');
    // The forum and the time follow a post's alert.
    expect(row(root, 6).querySelector('.alert-forum')?.textContent).toBe('General');
    expect(row(root, 6).querySelector('time')?.getAttribute('datetime')).toBe(new Date(member.notifications[0].at).toISOString());
    expect(row(root, 2).querySelector('.alert-forum')).toBeNull();
  });

  it('draws the goldens as served, in their order', async () => {
    viewer.value = { ...structuredClone(memberMe), notifications: 1 } as Me;
    serve({ [list]: moderator });
    const root = await renderAt('/notifications');
    expect(rows(root).map((r) => r.dataset.kind)).toEqual(['new_application', 'mention', 'thread_you_watch', 'application_decided']);
    expect(rows(root).map((r) => r.classList.contains('unread'))).toEqual([false, true, false, false]);
    expect(row(root, 41).querySelector('.visually-hidden')?.textContent).toBe('Unread: ');
    expect(row(root, 37).querySelector('.visually-hidden')).toBeNull();
  });

  // The screen asks for nothing but the list, the mark and /me: an alert
  // carries no post text, and the screen goes nowhere to find any.
  it('fetches no post', async () => {
    const s = serve({ [list]: member });
    await renderAt('/notifications');
    await flush();
    expect(s.sent.map((r) => `${r.method} ${r.url}`)).toEqual(['GET /api/v1/notifications', read, 'GET /api/v1/me']);
  });

  it('names an anonymized actor by its placeholder', async () => {
    const actor = { handle: 'deleted-user-7', display_name: '', avatar: '', role: 'member', kind: 'human' };
    serve({ [list]: page([note(9, 'reply_to_you', { actor })]) });
    const root = await renderAt('/notifications');
    expect(row(root, 9).querySelector('.alert-actor')?.textContent).toBe('deleted-user-7');
  });

  // AC4.
  it('explains what would appear when there is nothing', async () => {
    serve({ [list]: page([]) });
    const root = await renderAt('/notifications');
    expect(root.querySelector('[data-empty-list]')?.textContent).toBe(emptyAlerts);
    expect(root.querySelector('[data-mark-all]')).toBeNull();
    expect(root.querySelector('[data-older]')).toBeNull();
  });

  it('reports a failure, and tries again', async () => {
    const s = stubServer({ [`GET ${list}`]: () => problemAnswer('unavailable', 503) });
    const root = await renderAt('/notifications');
    expect(root.querySelector('.error-panel')?.textContent).toContain('Your alerts could not be loaded.');
    s.routes[`GET ${list}`] = () => ({ status: 200, body: page([]) });
    await act(() => root.querySelector<HTMLButtonElement>('.error-panel button')!.click());
    await flush();
    expect(root.querySelector('.error-panel')).toBeNull();
    expect(root.querySelector('[data-empty-list]')).not.toBeNull();
  });
});

describe('marking read', () => {
  // AC2: the rows shown are marked once, by id, and /me is fetched so both
  // badges drop. The rows stay marked unread while the reader is here.
  it('marks exactly the unread rows shown, once, and the badges drop', async () => {
    const s = serve({ [list]: page([note(8, 'mention'), note(7, 'quoted', { read: true }), note(6, 'reply_to_you')]) });
    const root = await renderAt('/notifications');
    await flush();
    expect(marks(s)).toEqual([{ ids: [8, 6] }]);
    expect(s.sent.at(-1)?.url).toBe('/api/v1/me');
    expect(viewer.value.notifications).toBe(0);
    expect(root.querySelector('[data-nav=alerts] [data-badge]')).toBeNull();
    expect(rows(root).map((r) => r.classList.contains('unread'))).toEqual([true, false, true]);

    await act(() => void (isPhone.value = true));
    await flush();
    expect(root.querySelector('.tabbar [data-nav=alerts] [data-badge]')).toBeNull();
    expect(marks(s)).toHaveLength(1);
  });

  it('shows the badge from /me in both layouts until then', async () => {
    serve({ [list]: page([]) });
    // Nothing unread is shown, so nothing is marked and /me is not asked.
    const root = await renderAt('/notifications');
    await flush();
    const badge = (sel: string) => root.querySelector(`${sel} [data-nav=alerts] [data-badge]`)?.firstChild?.textContent;
    expect(badge('.topbar')).toBe('2');
    await act(() => void (isPhone.value = true));
    expect(badge('.tabbar')).toBe('2');
  });

  it('posts nothing when every row shown is read', async () => {
    const s = serve({ [list]: page([note(3, 'mention', { read: true })]) });
    await renderAt('/notifications');
    await flush();
    expect(marks(s)).toEqual([]);
  });

  // Review 1709: mark-all names the first row, the highest id, and clears
  // every unread row at or below it, including rows not yet paged in.
  it('marks all up to the first row', async () => {
    const s = serve({ [list]: page([note(12, 'mention', { read: true }), note(10, 'quoted', { read: true })], 'MTA') }, 0);
    viewer.value = { ...structuredClone(memberMe), notifications: 5 } as Me;
    const root = await renderAt('/notifications');
    await flush();
    expect(root.querySelector('[data-nav=alerts] [data-badge]')?.firstChild?.textContent).toBe('5');
    await act(() => root.querySelector<HTMLButtonElement>('[data-mark-all]')!.click());
    await flush();
    expect(marks(s)).toEqual([{ up_to: 12 }]);
    expect(viewer.value.notifications).toBe(0);
    expect(root.querySelector('[data-nav=alerts] [data-badge]')).toBeNull();
    expect(root.querySelector('[role=status]')?.textContent).toBe('All alerts are marked read.');
  });

  it('shows every row read after mark-all', async () => {
    serve({ [list]: page([note(12, 'mention'), note(10, 'quoted')]) });
    const root = await renderAt('/notifications');
    await flush();
    expect(rows(root).filter((r) => r.classList.contains('unread'))).toHaveLength(2);
    await act(() => root.querySelector<HTMLButtonElement>('[data-mark-all]')!.click());
    await flush();
    expect(rows(root).filter((r) => r.classList.contains('unread'))).toHaveLength(0);
  });

  // Terva review 1798: a failed second attempt does not put back the unread
  // marking the first one cleared.
  it('keeps rows cleared by a mark-all when a later one fails', async () => {
    const s = serve({ [list]: page([note(12, 'mention'), note(10, 'quoted')]) });
    const root = await renderAt('/notifications');
    await flush();
    const mark = () => act(() => root.querySelector<HTMLButtonElement>('[data-mark-all]')!.click());
    await mark();
    await flush();
    expect(rows(root).filter((r) => r.classList.contains('unread'))).toHaveLength(0);

    s.routes[read] = () => problemAnswer('unavailable', 503);
    await mark();
    await flush();
    expect(root.querySelector('[role=alert]')?.textContent).toContain('could not be marked read');
    expect(rows(root).filter((r) => r.classList.contains('unread'))).toHaveLength(0);
    expect(root.querySelectorAll('[data-alert] .visually-hidden')).toHaveLength(0);
  });

  it('says so when mark-all fails, and keeps the badge', async () => {
    const s = serve({ [list]: page([note(12, 'mention', { read: true })]) });
    s.routes[read] = () => problemAnswer('unavailable', 503);
    const root = await renderAt('/notifications');
    await flush();
    await act(() => root.querySelector<HTMLButtonElement>('[data-mark-all]')!.click());
    await flush();
    expect(root.querySelector('[role=alert]')?.textContent).toContain('could not be marked read');
    expect(viewer.value.notifications).toBe(2);
    expect(root.querySelector<HTMLButtonElement>('[data-mark-all]')!.disabled).toBe(false);
  });
});

describe('older alerts', () => {
  it('appends the next page, marks its unread rows, and moves focus to it', async () => {
    const s = serve({
      [list]: page([note(30, 'mention')], 'MjU'),
      [`${list}?cursor=MjU`]: page([note(20, 'quoted'), note(19, 'reply_to_you', { read: true })], 'MTk'),
      [`${list}?cursor=MTk`]: page([note(5, 'thread_you_watch')]),
    });
    const root = await renderAt('/notifications');
    await flush();
    expect(marks(s)).toEqual([{ ids: [30] }]);

    await act(() => root.querySelector<HTMLButtonElement>('[data-older]')!.click());
    await flush();
    expect(rows(root).map((r) => r.dataset.alert)).toEqual(['30', '20', '19']);
    expect(document.activeElement).toBe(row(root, 20));
    expect(marks(s)).toEqual([{ ids: [30] }, { ids: [20] }]);

    await act(() => root.querySelector<HTMLButtonElement>('[data-older]')!.click());
    await flush();
    expect(rows(root).map((r) => r.dataset.alert)).toEqual(['30', '20', '19', '5']);
    expect(root.querySelector('[data-older]')).toBeNull();
    expect(marks(s)).toEqual([{ ids: [30] }, { ids: [20] }, { ids: [5] }]);
  });

  it('reports a failed page under the rows, and tries it again', async () => {
    const s = serve({ [list]: page([note(30, 'mention', { read: true })], 'MjU') });
    s.routes[`GET ${list}?cursor=MjU`] = () => problemAnswer('unavailable', 503);
    const root = await renderAt('/notifications');
    await act(() => root.querySelector<HTMLButtonElement>('[data-older]')!.click());
    await flush();
    expect(root.querySelector('.error-panel')?.textContent).toContain('Older alerts could not be loaded.');
    expect(rows(root)).toHaveLength(1);
    s.routes[`GET ${list}?cursor=MjU`] = () => ({ status: 200, body: page([note(4, 'quoted', { read: true })]) });
    await act(() => root.querySelector<HTMLButtonElement>('.error-panel button')!.click());
    await flush();
    expect(root.querySelector('.error-panel')).toBeNull();
    expect(rows(root).map((r) => r.dataset.alert)).toEqual(['30', '4']);
  });
});
