import { render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import type { AdminUserDetail, AdminUserList, Me } from '../../api/dto';
import { viewer } from '../../state/viewer';
import { pressRaw, raceSettle, renderAt, settle, unmount } from '../../testing';
import { mayAct, SuspensionPanel } from './SuspensionPanel';
import moderatorMe from '../../../../../internal/dto/testdata/golden/me.moderator.json';
import userList from '../../../../../internal/dto/testdata/golden/admin-users.admin.json';
import userModerator from '../../../../../internal/dto/testdata/golden/admin-user.moderator.json';
import suspendedModerator from '../../../../../internal/dto/testdata/golden/admin-user-suspended.moderator.json';

// The suspension controls on an account's detail in /admin/users
// (TKT-01M3QMRE47), against a stubbed server whose account changes as the
// routes say it does. The clock is fixed, so the ends sent are exact.

const asModerator = () => structuredClone(moderatorMe) as Me;
const as = (role: string, handle = 'bob') => {
  const me = asModerator();
  me.user = { ...me.user!, role, handle };
  return me;
};

type Answer = { status: number; body?: unknown };
let routes: Record<string, (body: unknown) => Answer | Promise<Answer>>;
let sent: { method: string; url: string; body: unknown }[];

function serve() {
  vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
    const method = init.method ?? 'GET';
    const body = init.body ? JSON.parse(init.body as string) : undefined;
    sent.push({ method, url, body });
    const handler = routes[`${method} ${url}`];
    if (!handler) return new Response('{}', { status: 404, headers: { 'Content-Type': 'application/problem+json' } });
    const a = await handler(body);
    const problem = a.status >= 400;
    return new Response(a.body === undefined ? null : JSON.stringify(a.body), {
      status: a.status,
      headers: { 'Content-Type': problem ? 'application/problem+json' : 'application/json' },
    });
  });
}

const refusal = (type: string, status: number) => ({ status, body: { type, status, title: type } });
const noContent = { status: 204 };

const now = new Date(2026, 9, 1, 12, 0).getTime();
const day = 86_400_000;
const base = '/api/v1/admin/users/alice';

// account is alice as the stubbed server has her now: an active member, as
// a moderator reads her. The detail route and the list both answer from it.
let account: AdminUserDetail;
const active = () => structuredClone(userModerator) as AdminUserDetail;
function suspended(until: number | null = null, reason = 'spam'): AdminUserDetail {
  const d = active();
  d.user.status = 'suspended';
  d.suspension = { reason, until };
  return d;
}
function listNow(): AdminUserList {
  const l = structuredClone(userList) as AdminUserList;
  l.users[0].status = account.user.status;
  return l;
}

beforeAll(async () => {
  await import('./AdminRoot');
}, 30_000);

let saved: Me;
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(now);
  saved = viewer.value;
  viewer.value = as('moderator');
  sent = [];
  account = active();
  routes = {
    'GET /api/v1/admin/users': () => ({ status: 200, body: listNow() }),
    [`GET ${base}`]: () => ({ status: 200, body: structuredClone(account) }),
  };
  serve();
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const panel = (root: Element) => root.querySelector<HTMLElement>('[data-suspension-panel]');
const labels = (el: Element | null) => (el ? [...el.querySelectorAll('button')].map((b) => b.textContent) : []);
const button = (el: Element, label: string) => {
  const b = [...el.querySelectorAll('button')].find((b) => b.textContent === label);
  if (!b) throw new Error(`no ${label} button among ${labels(el).join(', ')}`);
  return b;
};
const press = async (root: HTMLElement, label: string) => {
  await act(async () => button(panel(root)!, label).click());
  await settle(root);
};
const message = (root: Element) => root.querySelector('[data-suspension-message]')?.textContent ?? '';
const posts = (method = 'POST') => sent.filter((s) => s.method === method && s.url.startsWith(base + '/'));

async function openAlice(): Promise<HTMLElement> {
  const root = await renderAt('/admin/users');
  await act(async () => root.querySelector<HTMLButtonElement>('[data-user="alice"]')!.click());
  await settle(root);
  return root;
}

async function type(root: HTMLElement, selector: string, value: string) {
  const input = panel(root)!.querySelector<HTMLInputElement>(selector)!;
  await act(async () => {
    input.value = value;
    input.dispatchEvent(new Event('input'));
  });
}

async function check(root: HTMLElement, selector: string) {
  const input = panel(root)!.querySelector<HTMLInputElement>(selector)!;
  await act(async () => {
    input.checked = true;
    input.dispatchEvent(new Event('change'));
  });
}

// fill opens the suspend form and fills it in.
async function fill(root: HTMLElement, reason: string, ends: string, opts: { date?: string; hide?: boolean } = {}) {
  await press(root, 'Suspend');
  await type(root, '[data-reason]', reason);
  await check(root, `[data-ends="${ends}"]`);
  if (opts.date !== undefined) await type(root, '[data-until]', opts.date);
  if (opts.hide) await check(root, '[data-hide-posts]');
}

// suspendWith fills the form, presses Suspend, and confirms.
async function suspendWith(root: HTMLElement, reason: string, ends: string, opts: { date?: string; hide?: boolean } = {}) {
  await fill(root, reason, ends, opts);
  await press(root, 'Suspend');
  await press(root, 'Confirm');
  await settle(root);
}

describe('suspending', () => {
  beforeEach(() => {
    routes[`POST ${base}/suspend`] = (body) => {
      const b = body as { reason: string; until: number | null };
      account = suspended(b.until, b.reason);
      return noContent;
    };
  });

  it('suspends for 7 days, and only after the confirmation', async () => {
    const root = await openAlice();
    await fill(root, 'spam', '7');
    await press(root, 'Suspend');
    expect(panel(root)!.textContent).toContain('Suspend @alice for 7 days?');
    expect(posts()).toHaveLength(0);
    await press(root, 'Confirm');
    await settle(root);
    expect(posts()).toHaveLength(1);
    expect(JSON.stringify(posts()[0].body)).toBe(`{"reason":"spam","until":${now + 7 * day},"hide_posts":false}`);
    expect(message(root)).toBe('@alice is suspended.');
  });

  // TKT-01M3V7CFJ5: the form's opener goes when it opens, so before a plain
  // effect's frame focus is on nothing and a character typed is lost. The
  // press is outside act, which would run the effect.
  it('a character typed one microtask after opening reaches the reason', async () => {
    const root = await openAlice();
    const suspend = button(panel(root)!, 'Suspend');
    suspend.focus();
    try {
      suspend.click();
      await Promise.resolve();
      pressRaw('s');
      expect(panel(root)!.querySelector<HTMLInputElement>('[data-reason]')!.value).toBe('s');
    } finally {
      await raceSettle();
    }
  });

  it('a key one microtask after Cancel lands on Suspend again', async () => {
    const root = await openAlice();
    await press(root, 'Suspend');
    const cancel = button(panel(root)!, 'Cancel');
    cancel.focus();
    try {
      cancel.click();
      await Promise.resolve();
      expect(pressRaw('Enter')?.textContent).toBe('Suspend');
    } finally {
      await raceSettle();
    }
  });

  it('suspends for 1 and 30 days from now', async () => {
    let root = await openAlice();
    await suspendWith(root, 'spam', '1');
    expect((posts()[0].body as { until: number }).until).toBe(now + day);
    account = active();
    sent = [];
    root = await openAlice();
    await suspendWith(root, 'spam', '30');
    expect((posts()[0].body as { until: number }).until).toBe(now + 30 * day);
  });

  it('suspends until lifted', async () => {
    const root = await openAlice();
    await fill(root, 'spam', 'lifted');
    await press(root, 'Suspend');
    expect(panel(root)!.textContent).toContain('Suspend @alice until it is lifted?');
    await press(root, 'Confirm');
    expect(posts()[0].body).toEqual({ reason: 'spam', until: null, hide_posts: false });
  });

  it('sends a chosen end as that local moment, and refuses a past one beside the field', async () => {
    const root = await openAlice();
    await fill(root, 'spam', 'date', { date: '2026-09-30T09:00' });
    await press(root, 'Suspend');
    const field = panel(root)!.querySelector('[data-until]')!;
    expect(field.getAttribute('aria-invalid')).toBe('true');
    expect(panel(root)!.textContent).toContain('Choose a moment in the future.');
    expect(labels(panel(root))).not.toContain('Confirm');
    expect(posts()).toHaveLength(0);

    await type(root, '[data-until]', '2026-10-07T14:00');
    await press(root, 'Suspend');
    await press(root, 'Confirm');
    expect(posts()[0].body).toEqual({ reason: 'spam', until: new Date(2026, 9, 7, 14, 0).getTime(), hide_posts: false });
  });

  it('checks the reason beside the field and sends nothing', async () => {
    const root = await openAlice();
    await press(root, 'Suspend');
    await check(root, '[data-ends="7"]');
    for (const [reason, words] of [
      ['', 'Give a reason'],
      ['   ', 'Give a reason'],
      ['😀'.repeat(501), 'The reason is 501 characters'],
      ['spam\tand more', 'control character'],
    ]) {
      await type(root, '[data-reason]', reason);
      await press(root, 'Suspend');
      const input = panel(root)!.querySelector('[data-reason]')!;
      expect(input.getAttribute('aria-invalid'), JSON.stringify(reason)).toBe('true');
      const error = document.getElementById(input.getAttribute('aria-describedby')!.split(' ').at(-1)!)!;
      expect(error.textContent).toContain(words);
      expect(labels(panel(root))).not.toContain('Confirm');
    }
    expect(posts()).toHaveLength(0);

    await type(root, '[data-reason]', '😀'.repeat(500));
    expect(panel(root)!.querySelector('[data-reason-count]')!.textContent).toBe('500 of 500 characters');
    await press(root, 'Suspend');
    await press(root, 'Confirm');
    expect((posts()[0].body as { reason: string }).reason).toBe('😀'.repeat(500));
  });

  it('checks again at Confirm, for a reason emptied while the question was open', async () => {
    const root = await openAlice();
    await fill(root, 'spam', '7');
    await press(root, 'Suspend');
    await type(root, '[data-reason]', ' ');
    await press(root, 'Confirm');
    expect(posts()).toHaveLength(0);
    expect(panel(root)!.querySelector('[data-reason]')!.getAttribute('aria-invalid')).toBe('true');
  });

  it('asks when the suspension ends', async () => {
    const root = await openAlice();
    await press(root, 'Suspend');
    await type(root, '[data-reason]', 'spam');
    await press(root, 'Suspend');
    expect(panel(root)!.textContent).toContain('Choose when the suspension ends.');
    expect(posts()).toHaveLength(0);
  });

  it('hides their posts only when asked, and says what that means', async () => {
    const root = await openAlice();
    await press(root, 'Suspend');
    expect(panel(root)!.textContent).toContain("Members see 'removed by a moderator'; you can restore them later.");
    expect(panel(root)!.querySelector<HTMLInputElement>('[data-hide-posts]')!.checked).toBe(false);
    await type(root, '[data-reason]', 'spam');
    await check(root, '[data-ends="7"]');
    await check(root, '[data-hide-posts]');
    await press(root, 'Suspend');
    expect(panel(root)!.textContent).toContain('Their posts are hidden too.');
    await press(root, 'Confirm');
    expect(posts()[0].body).toEqual({ reason: 'spam', until: now + 7 * day, hide_posts: true });
  });

  it('warns that an agent loses its token for good', async () => {
    const agent = active();
    agent.user.user = { ...agent.user.user, kind: 'agent' };
    account = agent;
    const root = await openAlice();
    await fill(root, 'spam', '7');
    await press(root, 'Suspend');
    expect(panel(root)!.textContent).toContain('Its token is revoked, and unsuspending does not restore it.');
  });
});

describe('the status line', () => {
  it('shows the end and the reason, as text', async () => {
    account = suspended(new Date(2026, 9, 7, 14, 0).getTime(), '<b>spam</b> & worse');
    const root = await openAlice();
    const line = panel(root)!.querySelector('[data-suspension]')!;
    expect(line.textContent).toContain('Suspended until 2026-10-07 14:00');
    expect(line.textContent).toContain('Reason: <b>spam</b> & worse');
    expect(line.querySelector('b')).toBeNull();
  });

  it('says until lifted for a suspension with no end', async () => {
    account = suspended(null);
    const root = await openAlice();
    expect(panel(root)!.querySelector('[data-suspension]')!.textContent).toContain('Suspended until lifted');
  });

  it('reads the golden', async () => {
    account = structuredClone(suspendedModerator) as AdminUserDetail;
    account.user.user.handle = 'alice';
    const root = await openAlice();
    expect(panel(root)!.textContent).toContain('Reason: Posting the same link in every thread.');
  });
});

describe('unsuspending, hiding and restoring', () => {
  beforeEach(() => {
    account = suspended(now + day);
    routes[`POST ${base}/unsuspend`] = () => {
      account = active();
      return noContent;
    };
    routes[`POST ${base}/posts/hide`] = () => noContent;
    routes[`POST ${base}/posts/restore`] = () => noContent;
  });

  it('unsuspends without restoring the posts unless asked', async () => {
    const root = await openAlice();
    expect(panel(root)!.querySelector<HTMLInputElement>('[data-restore-posts]')!.checked).toBe(false);
    await press(root, 'Unsuspend');
    expect(posts()).toHaveLength(0);
    await press(root, 'Confirm');
    expect(posts()[0]).toMatchObject({ url: `${base}/unsuspend`, body: { restore_posts: false } });
    expect(message(root)).toBe('@alice is no longer suspended.');
  });

  it('unsuspends and restores the posts when asked', async () => {
    const root = await openAlice();
    await check(root, '[data-restore-posts]');
    await press(root, 'Unsuspend');
    await press(root, 'Confirm');
    expect(posts()[0].body).toEqual({ restore_posts: true });
  });

  it('hides and restores on their own, each one bodiless POST after the confirmation', async () => {
    const root = await openAlice();
    await press(root, 'Hide their posts');
    expect(panel(root)!.textContent).toContain('Hide every visible post by @alice in forums you moderate?');
    expect(posts()).toHaveLength(0);
    await press(root, 'Confirm');
    expect(posts()).toEqual([{ method: 'POST', url: `${base}/posts/hide`, body: undefined }]);

    await press(root, 'Restore hidden posts');
    expect(panel(root)!.textContent).toContain('Restore the posts a suspension of @alice hid, in forums you moderate?');
    await press(root, 'Confirm');
    expect(posts()[1]).toEqual({ method: 'POST', url: `${base}/posts/restore`, body: undefined });
    expect(message(root)).toBe('Posts a suspension hid are restored, where you moderate.');
  });

  it('offers Restore but not Hide on an active account never suspended', async () => {
    account = active();
    const root = await openAlice();
    const offered = labels(panel(root));
    expect(offered).toContain('Restore hidden posts');
    expect(offered).toContain('Suspend');
    expect(offered).not.toContain('Hide their posts');
    expect(offered).not.toContain('Unsuspend');
  });

  it('says a conflict on unsuspend in its own words', async () => {
    routes[`POST ${base}/unsuspend`] = () => {
      account = active();
      return refusal('conflict', 409);
    };
    const root = await openAlice();
    await press(root, 'Unsuspend');
    await press(root, 'Confirm');
    expect(message(root)).toBe('This account is no longer suspended.');
  });

  it('says how to finish a hide that could not reach every thread', async () => {
    routes[`POST ${base}/posts/hide`] = () => refusal('unavailable', 503);
    const root = await openAlice();
    await press(root, 'Hide their posts');
    await press(root, 'Confirm');
    expect(message(root)).toBe('Some threads could not be reached. Press Hide their posts again to finish them.');
  });
});

describe('who sees the controls', () => {
  const detail = (role: string, handle = 'alice', status = 'active') => {
    const d = active();
    d.user.user = { ...d.user.user, role, handle };
    d.user.status = status;
    return d;
  };
  const controls = ['Suspend', 'Restore hidden posts'];

  async function mount(d: AdminUserDetail) {
    const root = document.createElement('div');
    document.body.replaceChildren(root);
    await act(async () => {
      render(<SuspensionPanel d={d} url={base} refetch={async () => d} onListChanged={() => {}} />, root);
    });
    const offered = labels(root);
    await act(async () => render(null, root));
    return offered;
  }

  it('lets a moderator act on members only', async () => {
    viewer.value = as('moderator');
    expect(await mount(detail('member'))).toEqual(controls);
    for (const role of ['moderator', 'admin']) expect(await mount(detail(role)), role).toEqual([]);
    expect(await mount(detail('moderator', 'bob'))).toEqual([]);
    expect(await mount(detail('member', 'BOB'))).toEqual([]);
  });

  it('lets an admin act on anyone but themselves', async () => {
    viewer.value = as('admin');
    for (const role of ['member', 'moderator', 'admin']) expect(await mount(detail(role)), role).toEqual(controls);
    expect(await mount(detail('admin', 'bob'))).toEqual([]);
  });

  // Account actions keep their role gates (docs/08 "Decided 2026-09-30"):
  // moderating a forum through a group gives no authority over accounts.
  it('gives a moderator by grant no controls', async () => {
    const me = as('member');
    me.queue = 3;
    viewer.value = me;
    for (const role of ['member', 'moderator', 'admin']) expect(await mount(detail(role)), role).toEqual([]);
    expect(mayAct(me, detail('member'))).toBe(false);
  });

  it('offers nothing on a pending or anonymized account', async () => {
    viewer.value = as('admin');
    expect(await mount(detail('member', 'alice', 'pending'))).toEqual([]);
    expect(await mount(detail('member', 'deleted-user-7', 'deactivated'))).toEqual([]);
  });

  it('shows the status line to a viewer who may not act', async () => {
    viewer.value = as('moderator');
    const d = suspended(null);
    d.user.user = { ...d.user.user, role: 'admin' };
    const root = document.createElement('div');
    document.body.replaceChildren(root);
    await act(async () => {
      render(<SuspensionPanel d={d} url={base} refetch={async () => d} onListChanged={() => {}} />, root);
    });
    expect(root.querySelector('[data-suspension]')!.textContent).toContain('Suspended until lifted');
    expect(labels(root)).toEqual([]);
    await act(async () => render(null, root));
  });
});

describe('the answers', () => {
  // The suspension commits, then its hide fails: the server's account is
  // suspended and the answer is the hide step's.
  const hideStops = (status: number, type: string) => () => {
    account = suspended(now + 7 * day);
    return refusal(type, status);
  };

  for (const [status, type] of [
    [503, 'unavailable'],
    [500, 'internal'],
  ] as const) {
    it(`offers to finish a hide that stopped (${status})`, async () => {
      routes[`POST ${base}/suspend`] = hideStops(status, type);
      let answer!: () => void;
      const root = await openAlice();
      // The detail refetch after the answer waits until the test lets it go.
      routes[`GET ${base}`] = () =>
        new Promise<Answer>((r) => (answer = () => r({ status: 200, body: structuredClone(account) })));
      await fill(root, 'spam', '7', { hide: true });
      await press(root, 'Suspend');
      await press(root, 'Confirm');
      expect(message(root)).toBe('');
      await act(async () => answer());
      await settle(root);
      expect(message(root)).toBe(
        '@alice is now suspended, but the request did not finish, so some posts may not be hidden. Press Hide their posts to finish.',
      );
      expect(labels(panel(root))).toContain('Hide their posts');
      expect(root.querySelector('[data-user="alice"]')!.closest('tr')!.textContent).toContain('suspended');
    });
  }

  it('does not call already suspended a partial hide', async () => {
    routes[`POST ${base}/suspend`] = () => {
      account = suspended(null, 'someone else');
      return refusal('conflict', 409);
    };
    const root = await openAlice();
    await suspendWith(root, 'spam', '7', { hide: true });
    expect(message(root)).toBe('@alice is already suspended, perhaps by someone else. What is shown now is current.');
    expect(panel(root)!.textContent).toContain('Reason: someone else');
  });

  it('does not call a refused suspend a partial hide', async () => {
    routes[`POST ${base}/suspend`] = () => refusal('conflict', 409);
    const root = await openAlice();
    await suspendWith(root, 'spam', '7', { hide: true });
    expect(message(root)).toBe('This account cannot be suspended. It may be pending, anonymized, or the last active admin.');
    expect(labels(panel(root))).not.toContain('Hide their posts');
  });

  it('does not call a busy database a partial hide', async () => {
    routes[`POST ${base}/suspend`] = () => refusal('unavailable', 503);
    const root = await openAlice();
    await suspendWith(root, 'spam', '7', { hide: true });
    expect(message(root)).toBe('The server is briefly unavailable. Try again shortly.');
    expect(labels(panel(root))).not.toContain('Hide their posts');
  });

  // Terva review 1586, finding 2: a proxy's 502 or 504 is unavailable too,
  // but is not the hide step's answer.
  for (const status of [502, 504]) {
    it(`does not call a proxy's ${status} a partial hide`, async () => {
      routes[`POST ${base}/suspend`] = hideStops(status, 'unavailable');
      const root = await openAlice();
      await suspendWith(root, 'spam', '7', { hide: true });
      expect(message(root)).toBe('The server is briefly unavailable. Try again shortly.');
      expect(message(root)).not.toContain('hidden');
    });
  }

  // Terva review 1586, finding 1: a success is said as the refreshed account
  // stands, which another moderator may have changed in between.
  it('does not say suspended when the refreshed account is not', async () => {
    routes[`POST ${base}/suspend`] = () => noContent;
    const root = await openAlice();
    await suspendWith(root, 'spam', '7');
    expect(message(root)).toBe(
      '@alice was suspended, but is not suspended now. Someone else may have lifted it. What is shown now is current.',
    );
  });

  it('does not say unsuspended when the refreshed account is suspended', async () => {
    account = suspended(null);
    routes[`POST ${base}/unsuspend`] = () => {
      account = suspended(null, 'suspended again');
      return noContent;
    };
    const root = await openAlice();
    await press(root, 'Unsuspend');
    await press(root, 'Confirm');
    expect(message(root)).toBe(
      '@alice was unsuspended, but is suspended again now, perhaps by someone else. What is shown now is current.',
    );
  });

  it('does not call a failure without hide_posts a partial hide', async () => {
    routes[`POST ${base}/suspend`] = hideStops(503, 'unavailable');
    const root = await openAlice();
    await suspendWith(root, 'spam', '7');
    expect(message(root)).toBe('The server is briefly unavailable. Try again shortly.');
  });

  it('says nothing of a hide when the refetch fails', async () => {
    routes[`POST ${base}/suspend`] = () => {
      routes[`GET ${base}`] = () => refusal('internal', 500);
      return refusal('unavailable', 503);
    };
    const root = await openAlice();
    await suspendWith(root, 'spam', '7', { hide: true });
    expect(message(root)).toBe('The server is briefly unavailable. Try again shortly. Reload to see the account as it is now.');
  });

  it('says how to finish a restore that stopped after the unsuspension', async () => {
    account = suspended(null);
    routes[`POST ${base}/unsuspend`] = () => {
      account = active();
      return refusal('unavailable', 503);
    };
    const root = await openAlice();
    await check(root, '[data-restore-posts]');
    await press(root, 'Unsuspend');
    await press(root, 'Confirm');
    expect(message(root)).toBe(
      '@alice is no longer suspended, but the request did not finish, so some posts may still be hidden. Press Restore hidden posts to finish.',
    );
  });

  it('asks for the detail and the list again after a success and after a failure', async () => {
    routes[`POST ${base}/suspend`] = () => refusal('conflict', 409);
    const root = await openAlice();
    const count = () => ({
      detail: sent.filter((s) => s.method === 'GET' && s.url === base).length,
      list: sent.filter((s) => s.method === 'GET' && s.url === '/api/v1/admin/users').length,
    });
    const before = count();
    await suspendWith(root, 'spam', '7');
    expect(count()).toEqual({ detail: before.detail + 1, list: before.list + 1 });

    routes[`POST ${base}/suspend`] = () => {
      account = suspended(now + 7 * day);
      return noContent;
    };
    await press(root, 'Confirm');
    await settle(root);
    expect(count()).toEqual({ detail: before.detail + 2, list: before.list + 2 });
    expect(panel(root)!.querySelector('[data-suspension]')!.textContent).toContain('Suspended until');
    expect(root.querySelector('[data-detail] [data-status]')!.textContent).toBe('suspended');
    expect(root.querySelector('[data-user="alice"]')!.closest('tr')!.textContent).toContain('suspended');
  });

  it('keeps to one change at a time', async () => {
    account = suspended(null);
    let answer!: () => void;
    routes[`POST ${base}/posts/hide`] = () => new Promise<Answer>((r) => (answer = () => r(noContent)));
    routes[`POST ${base}/posts/restore`] = () => noContent;
    const root = await openAlice();
    await press(root, 'Restore hidden posts');
    await press(root, 'Hide their posts');
    // Hide's confirmation comes first in the panel, then Restore's.
    const confirms = () => [...panel(root)!.querySelectorAll('button')].filter((b) => b.textContent === 'Confirm');
    await act(async () => confirms()[0].click());
    await act(async () => confirms()[1].click());
    await settle(root);
    expect(message(root)).toBe('Another change to this account is still being made. Try again when it finishes.');
    expect(labels(panel(root)).filter((l) => l === 'Restore hidden posts')).toEqual([]);
    await act(async () => answer());
    await settle(root);
    expect(posts().map((p) => p.url)).toEqual([`${base}/posts/hide`]);
  });
});
