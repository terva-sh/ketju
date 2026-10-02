import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me, Settings } from '../../api/dto';
import { shortcutsOn } from '../../keyboard/shortcuts';
import { storeThreadPage, threadCache } from '../../state/threads';
import { viewer } from '../../state/viewer';
import { problemAnswer, renderAt, route, screenOf, settle, stubServer, threadPage, unmount } from '../../testing';
import anonMe from '../../../../../internal/dto/testdata/golden/me.anon.json';
import memberMe from '../../../../../internal/dto/testdata/golden/me.member.json';

// /settings (TKT-01M29CXK4E): timezone, page size and theme, each sent as a
// PATCH of its own key when its field changes, and the answer made the
// viewer's at once. The viewer is the member golden's, alice, with the
// server's defaults: UTC, 25 posts a page, the system theme.

// defaults are the server's for an account that never chose (internal/
// service/users me.go).
const defaults = (): Settings => ({ timezone: 'UTC', locale: 'en', page_size: 25, theme: 'system', shortcuts: true });

let saved: Me;
beforeEach(() => {
  saved = viewer.value;
  viewer.value = { ...(structuredClone(memberMe) as Me), settings: defaults() };
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  document.documentElement.removeAttribute('data-theme');
  localStorage.clear();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});


// serve answers GET /settings with stored, and each PATCH with stored and
// the change, as the server does.
function serve(stored: Settings = defaults(), patch?: (body: Partial<Settings>) => ReturnType<typeof problemAnswer>) {
  return stubServer({
    'GET /api/v1/settings': () => ({ status: 200, body: stored }),
    'PATCH /api/v1/settings': (body) => patch?.(body as Partial<Settings>) ?? { status: 200, body: { ...stored, ...(body as object) } },
  });
}

const patches = (s: ReturnType<typeof stubServer>) => s.sent.filter((r) => r.method === 'PATCH').map((r) => r.body);

async function change(root: HTMLElement, el: HTMLInputElement | HTMLSelectElement, value: string) {
  await act(async () => {
    el.value = value;
    el.dispatchEvent(new Event('change', { bubbles: true }));
  });
  await settle(root);
}

const field = (root: HTMLElement, id: string) => root.querySelector<HTMLInputElement & HTMLSelectElement>(`#settings-${id}`)!;

describe('the settings form', () => {
  it('changing page size sends one PATCH and updates the viewer', async () => {
    const s = serve();
    const root = await renderAt('/settings');
    expect(screenOf(root)).toBe('settings');
    expect(field(root, 'page_size').value).toBe('25');
    await change(root, field(root, 'page_size'), '10');
    expect(patches(s)).toEqual([{ page_size: 10 }]);
    expect(viewer.value.settings.page_size).toBe(10);
    expect(root.querySelector('[role=status]')?.textContent).toBe('Saved.');
  });

  // A page cut at 25 is not shown beside pages cut at 10.
  it('drops the thread pages held at the old page size', async () => {
    serve();
    const root = await renderAt('/settings');
    storeThreadPage('t1', threadPage({ id: 't1', slug: 'a', title: 'A' }));
    expect(threadCache.value.size).toBe(1);
    await change(root, field(root, 'page_size'), '10');
    expect(threadCache.value.size).toBe(0);
  });

  it('changing the timezone sends one PATCH and updates the viewer', async () => {
    const s = serve();
    const root = await renderAt('/settings');
    await change(root, field(root, 'timezone'), 'Europe/Helsinki');
    expect(patches(s)).toEqual([{ timezone: 'Europe/Helsinki' }]);
    expect(viewer.value.settings.timezone).toBe('Europe/Helsinki');
    expect(field(root, 'timezone').value).toBe('Europe/Helsinki');
  });

  it('changing the theme sends one PATCH, applies it and stores it', async () => {
    const s = serve();
    const root = await renderAt('/settings');
    const dark = root.querySelector<HTMLInputElement>('input[name=theme][value=dark]')!;
    expect(root.querySelector<HTMLInputElement>('input[name=theme][value=system]')!.checked).toBe(true);
    await act(async () => dark.click());
    await settle(root);
    expect(patches(s)).toEqual([{ theme: 'dark' }]);
    expect(viewer.value.settings.theme).toBe('dark');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    expect(localStorage.getItem('ketju.theme')).toBe('dark');
  });

  // TKT-01M3V9DWTN: the single-key shortcuts' switch saves through the
  // same queue, and the answer turns them off at once.
  it('turning the shortcuts off sends one PATCH and turns them off', async () => {
    const s = serve();
    const root = await renderAt('/settings');
    const box = field(root, 'shortcuts');
    expect(box.type).toBe('checkbox');
    expect(box.checked).toBe(true);
    expect(box.closest('fieldset')?.querySelector('legend')?.textContent).toBe('Keyboard shortcuts');
    expect(box.closest('label')?.textContent).toBe('Use single-key shortcuts');
    await act(async () => box.click());
    await settle(root);
    expect(patches(s)).toEqual([{ shortcuts: false }]);
    expect(viewer.value.settings.shortcuts).toBe(false);
    expect(shortcutsOn.value).toBe(false);
    expect(field(root, 'shortcuts').checked).toBe(false);
    await act(async () => field(root, 'shortcuts').click());
    await settle(root);
    expect(patches(s)).toEqual([{ shortcuts: false }, { shortcuts: true }]);
    expect(shortcutsOn.value).toBe(true);
  });

  it('shows a refused shortcuts value beside the switch', async () => {
    serve(defaults(), () => problemAnswer('validation-failed', 422, { errors: [{ field: 'shortcuts', code: 'wrong_type' }] }));
    const root = await renderAt('/settings');
    await act(async () => field(root, 'shortcuts').click());
    await settle(root);
    expect(root.querySelector('[data-field-error=shortcuts]')?.textContent).toBe('Choose on or off.');
    expect(viewer.value.settings.shortcuts).toBe(true);
  });

  it('shows the value being saved until the answer comes', async () => {
    let answer: (a: ReturnType<typeof problemAnswer>) => void = () => {};
    stubServer({
      'GET /api/v1/settings': () => ({ status: 200, body: defaults() }),
      'PATCH /api/v1/settings': () => new Promise((r) => (answer = r)),
    });
    const root = await renderAt('/settings');
    const dark = root.querySelector<HTMLInputElement>('input[name=theme][value=dark]')!;
    await act(async () => dark.click());
    await settle(root);
    expect(dark.checked).toBe(true);
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false);
    await act(async () => answer({ status: 200, body: { ...defaults(), theme: 'dark' } }));
    await settle(root);
    expect(dark.checked).toBe(true);
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
  });

  it('shows a refused field error beside its field, and keeps the viewer', async () => {
    serve(defaults(), () => problemAnswer('validation-failed', 422, { errors: [{ field: 'timezone', code: 'not_an_option' }] }));
    const root = await renderAt('/settings');
    await change(root, field(root, 'timezone'), 'Europe/Helsinki');
    const err = root.querySelector('[data-field-error=timezone]')!;
    expect(err.textContent).toMatch(/not a timezone the server knows/);
    expect(field(root, 'timezone').getAttribute('aria-invalid')).toBe('true');
    expect(field(root, 'timezone').getAttribute('aria-describedby')).toContain(err.id);
    expect(root.querySelector('[data-field-error=page_size]')?.textContent).toBe('');
    expect(viewer.value.settings.timezone).toBe('UTC');
  });

  it('refuses a page size outside 10 to 50 without asking the server', async () => {
    const s = serve();
    const root = await renderAt('/settings');
    await change(root, field(root, 'page_size'), '51');
    expect(patches(s)).toEqual([]);
    expect(field(root, 'page_size').value).toBe('51');
    expect(root.querySelector('[data-field-error=page_size]')?.textContent).toBe('Choose a whole number from 10 to 50.');
  });

  it('takes a newer answer from GET /settings, as another device left it', async () => {
    serve({ ...defaults(), page_size: 40, theme: 'dark' });
    const root = await renderAt('/settings');
    expect(field(root, 'page_size').value).toBe('40');
    expect(viewer.value.settings.page_size).toBe(40);
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
  });

  // Terva review 1838: two saves to different fields, the second made while
  // the first is unanswered. The server here applies each PATCH as it
  // arrives and answers the account as it then stands, the first one late.
  // Sent at once, the theme's answer, without the zone, would arrive first.
  it('two overlapping saves to different fields leave the viewer as the account saved it', async () => {
    const account = defaults();
    const late: (() => void)[] = [];
    const s = stubServer({
      'GET /api/v1/settings': () => ({ status: 200, body: defaults() }),
      'PATCH /api/v1/settings': (body) => {
        Object.assign(account, body);
        const answer = { status: 200, body: { ...account } };
        if (late.length === 0 && 'timezone' in (body as object)) return new Promise((r) => late.push(() => r(answer)));
        return answer;
      },
    });
    const root = await renderAt('/settings');
    await change(root, field(root, 'timezone'), 'Europe/Helsinki');
    await act(async () => root.querySelector<HTMLInputElement>('input[name=theme][value=dark]')!.click());
    await settle(root);
    // The theme waits for the zone's answer.
    expect(patches(s)).toEqual([{ timezone: 'Europe/Helsinki' }]);
    await act(async () => late[0]());
    await settle(root);
    expect(patches(s)).toEqual([{ timezone: 'Europe/Helsinki' }, { theme: 'dark' }]);
    expect(viewer.value.settings).toEqual(account);
    expect(account).toEqual({ ...defaults(), timezone: 'Europe/Helsinki', theme: 'dark' });
    expect(field(root, 'timezone').value).toBe('Europe/Helsinki');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
  });

  // Terva review 1875: the queue outlives the screen, so a save made after
  // leaving /settings and coming back waits for one still in flight.
  it('a save after leaving and coming back waits for the one still out', async () => {
    const account = defaults();
    const late: (() => void)[] = [];
    const s = stubServer({
      'GET /api/v1/settings': () => ({ status: 200, body: { ...account } }),
      'GET /api/v1/settings/sessions': () => ({ status: 200, body: { sessions: [] } }),
      'PATCH /api/v1/settings': (body) => {
        Object.assign(account, body);
        const answer = { status: 200, body: { ...account } };
        if (late.length === 0) return new Promise((r) => late.push(() => r(answer)));
        return answer;
      },
    });
    let root = await renderAt('/settings');
    await change(root, field(root, 'timezone'), 'Europe/Helsinki');
    await route('/settings/sessions');
    await settle(root);
    expect(screenOf(root)).toBe('settings-sessions');
    await route('/settings');
    await settle(root);
    expect(screenOf(root)).toBe('settings');
    await act(async () => root.querySelector<HTMLInputElement>('input[name=theme][value=dark]')!.click());
    await settle(root);
    expect(patches(s)).toEqual([{ timezone: 'Europe/Helsinki' }]);
    await act(async () => late[0]());
    await settle(root);
    expect(patches(s)).toEqual([{ timezone: 'Europe/Helsinki' }, { theme: 'dark' }]);
    expect(viewer.value.settings).toEqual(account);
    expect(account).toEqual({ ...defaults(), timezone: 'Europe/Helsinki', theme: 'dark' });
  });

  it('coming back while a save is out, the GET answered after it does not undo it', async () => {
    const patch: (() => void)[] = [];
    const get: (() => void)[] = [];
    let gets = 0;
    stubServer({
      'GET /api/v1/settings': () =>
        ++gets === 1 ? { status: 200, body: defaults() } : new Promise((r) => get.push(() => r({ status: 200, body: defaults() }))),
      'GET /api/v1/settings/sessions': () => ({ status: 200, body: { sessions: [] } }),
      'PATCH /api/v1/settings': (body) =>
        new Promise((r) => patch.push(() => r({ status: 200, body: { ...defaults(), ...(body as object) } }))),
    });
    const root = await renderAt('/settings');
    await change(root, field(root, 'timezone'), 'Europe/Helsinki');
    await route('/settings/sessions');
    await settle(root);
    await route('/settings');
    await settle(root);
    await act(async () => patch[0]());
    await settle(root);
    expect(viewer.value.settings.timezone).toBe('Europe/Helsinki');
    await act(async () => get[0]());
    await settle(root);
    expect(viewer.value.settings.timezone).toBe('Europe/Helsinki');
  });

  it('an earlier save answering does not clear a value refused since', async () => {
    const late: (() => void)[] = [];
    stubServer({
      'GET /api/v1/settings': () => ({ status: 200, body: defaults() }),
      'PATCH /api/v1/settings': (body) =>
        new Promise((r) => late.push(() => r({ status: 200, body: { ...defaults(), ...(body as object) } }))),
    });
    const root = await renderAt('/settings');
    await change(root, field(root, 'page_size'), '10');
    await change(root, field(root, 'page_size'), '51');
    await act(async () => late[0]());
    await settle(root);
    expect(viewer.value.settings.page_size).toBe(10);
    expect(field(root, 'page_size').value).toBe('51');
    expect(root.querySelector('[data-field-error=page_size]')?.textContent).toBe('Choose a whole number from 10 to 50.');
  });

  it('a refused save does not hold up the next', async () => {
    serve(defaults(), (body) =>
      'timezone' in body
        ? problemAnswer('validation-failed', 422, { errors: [{ field: 'timezone', code: 'not_an_option' }] })
        : { status: 200, body: { ...defaults(), ...body } },
    );
    const root = await renderAt('/settings');
    await change(root, field(root, 'timezone'), 'Europe/Helsinki');
    await change(root, field(root, 'page_size'), '10');
    expect(viewer.value.settings.page_size).toBe(10);
    expect(viewer.value.settings.timezone).toBe('UTC');
    expect(root.querySelector('[data-field-error=timezone]')?.textContent).toMatch(/not a timezone/);
  });

  it('does not let a GET answered after a change undo it', async () => {
    let answer: (a: ReturnType<typeof problemAnswer>) => void = () => {};
    stubServer({
      'GET /api/v1/settings': () => new Promise((r) => (answer = r)),
      'PATCH /api/v1/settings': (body) => ({ status: 200, body: { ...defaults(), ...(body as object) } }),
    });
    const root = await renderAt('/settings');
    await change(root, field(root, 'page_size'), '10');
    expect(viewer.value.settings.page_size).toBe(10);
    await act(async () => answer({ status: 200, body: defaults() }));
    await settle(root);
    expect(viewer.value.settings.page_size).toBe(10);
    expect(field(root, 'page_size').value).toBe('10');
  });

  it('a signed-out reader is told to sign in', async () => {
    viewer.value = structuredClone(anonMe) as Me;
    stubServer({ 'GET /api/v1/settings': () => problemAnswer('unauthenticated', 401) });
    const root = await renderAt('/settings');
    expect(screenOf(root)).toBe('settings');
    expect(root.querySelector('[role=alert] [data-sign-in]')).not.toBeNull();
    expect(root.querySelector('form')).toBeNull();
  });

  it('links to the sessions, identities and account screens', async () => {
    serve();
    const root = await renderAt('/settings');
    const links = [...root.querySelectorAll<HTMLAnchorElement>('nav[aria-label=Settings] a')];
    expect(links.map((a) => a.getAttribute('href'))).toEqual([
      '/settings',
      '/settings/sessions',
      '/settings/identities',
      '/settings/account',
    ]);
    expect(links[0].getAttribute('aria-current')).toBe('page');
  });
});

describe('the timezone field', () => {
  const zone = (z: string) =>
    vi.spyOn(Intl.DateTimeFormat.prototype, 'resolvedOptions').mockReturnValue({
      ...new Intl.DateTimeFormat().resolvedOptions(),
      timeZone: z,
    });

  it("offers this device's timezone when the account has the default", async () => {
    zone('Europe/Helsinki');
    const s = serve();
    const root = await renderAt('/settings');
    const offer = root.querySelector<HTMLButtonElement>('[data-device-zone]')!;
    expect(offer.textContent).toBe("Use this device's timezone (Europe/Helsinki)");
    await act(async () => offer.click());
    await settle(root);
    expect(patches(s)).toEqual([{ timezone: 'Europe/Helsinki' }]);
    expect(root.querySelector('[data-device-zone]')).toBeNull();
  });

  it('offers nothing once the account has a zone of its own', async () => {
    zone('Europe/Helsinki');
    serve({ ...defaults(), timezone: 'Europe/Paris' });
    viewer.value.settings.timezone = 'Europe/Paris';
    const root = await renderAt('/settings');
    expect(field(root, 'timezone').value).toBe('Europe/Paris');
    expect(root.querySelector('[data-device-zone]')).toBeNull();
  });

  // Terva review 1691: the browser's list omits valid names the server
  // accepts, UTC among them.
  it.each(['UTC', 'Asia/Calcutta'])('the timezone select always holds the stored value, %s', async (stored) => {
    vi.spyOn(Intl, 'supportedValuesOf').mockReturnValue(['Asia/Kolkata', 'Europe/Helsinki']);
    const s = serve({ ...defaults(), timezone: stored });
    viewer.value.settings.timezone = stored;
    const root = await renderAt('/settings');
    const select = field(root, 'timezone');
    expect(select.tagName).toBe('SELECT');
    expect(select.value).toBe(stored);
    expect(select.selectedOptions[0]?.value).toBe(stored);
    const offered = [...select.options].map((o) => o.value);
    expect(offered).toContain('UTC');
    expect(offered).toContain('Europe/Helsinki');
    expect(s.sent.map((r) => r.method)).toEqual(['GET']);
  });

  it('is a text field where the browser cannot list its zones', async () => {
    vi.spyOn(Intl, 'supportedValuesOf').mockImplementation(() => {
      throw new TypeError('not supported');
    });
    const s = serve();
    const root = await renderAt('/settings');
    const input = field(root, 'timezone');
    expect(input.tagName).toBe('INPUT');
    expect(input.value).toBe('UTC');
    await change(root, input, ' Europe/Helsinki ');
    expect(patches(s)).toEqual([{ timezone: 'Europe/Helsinki' }]);
  });
});
