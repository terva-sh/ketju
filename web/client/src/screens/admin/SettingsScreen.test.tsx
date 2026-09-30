import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me, SiteSetting, SiteSettings } from '../../api/dto';
import { viewer } from '../../state/viewer';
import { renderAt, screenOf, settle } from '../../testing';
import moderatorMe from '../../../../../internal/dto/testdata/golden/me.moderator.json';
import listing from '../../../../../internal/dto/testdata/golden/settings.admin.json';
import { configLine, groups, lastChanged } from './SettingsScreen';
import { stamp } from './AuditScreen';

// The site settings screen (TKT-01M29CXK2H), against a stubbed server that
// answers with the golden: flags.auto_hide_threshold never set,
// registration.mode set from the command line, and site_name set by an
// admin from this screen. A boolean key is added, since the golden has
// none, so every kind of control is drawn.

const asAdmin = () => {
  const me = structuredClone(moderatorMe) as Me;
  me.user = { ...me.user!, role: 'admin' };
  return me;
};

const history: SiteSetting = {
  key: 'public_edit_history',
  doc: 'Whether members, not only moderators, may read a post’s revisions.',
  kind: 'boolean',
  allowed: 'true or false',
  choices: [],
  default: true,
  value: true,
  is_default: true,
  updated_at: null,
  updated_by: null,
};

const golden = (): SiteSettings => {
  const l = structuredClone(listing) as SiteSettings;
  l.settings.push(structuredClone(history));
  l.settings.sort((a, b) => (a.key < b.key ? -1 : 1));
  return l;
};

type Answer = { status: number; body?: unknown };
let routes: Record<string, (body: unknown) => Answer | Promise<Answer>>;
let sent: { method: string; url: string; body: unknown }[];
let saved: Me;

// answerAs is the server's answer to a PUT of key: the key from the golden
// with the value sent, set by root now.
const answerAs = (key: string) => (body: unknown): Answer => {
  const s = golden().settings.find((x) => x.key === key)!;
  const value = (body as { value: SiteSetting['value'] }).value;
  return {
    status: 200,
    body: {
      ...s,
      value,
      is_default: value === s.default,
      updated_at: 1757620000000,
      updated_by: { handle: 'root', display_name: 'Site Admin', avatar: '', role: 'admin', kind: 'human' },
    },
  };
};

beforeEach(() => {
  sent = [];
  routes = { 'GET /api/v1/admin/settings': () => ({ status: 200, body: golden() }) };
  vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
    const method = init.method ?? 'GET';
    const body = init.body ? JSON.parse(init.body as string) : undefined;
    sent.push({ method, url, body });
    const handler = routes[`${method} ${url}`];
    const a = handler ? await handler(body) : { status: 404, body: { type: 'not-found', status: 404 } };
    return new Response(a.body === undefined ? null : JSON.stringify(a.body), {
      status: a.status,
      headers: { 'Content-Type': a.status >= 400 ? 'application/problem+json' : 'application/json' },
    });
  });
  saved = viewer.value;
  viewer.value = asAdmin();
});
afterEach(() => {
  viewer.value = saved;
  vi.unstubAllGlobals();
});

const form = (root: HTMLElement, key: string) => root.querySelector<HTMLFormElement>(`[data-setting="${key}"]`)!;
const control = <T extends HTMLElement>(root: HTMLElement, key: string) =>
  form(root, key).querySelector<T>('input, select')!;
const writes = () => sent.filter((s) => s.method !== 'GET');
const status = (root: HTMLElement, key: string) => form(root, key).querySelector('[data-saved]')?.textContent ?? null;
const error = (root: HTMLElement, key: string) => form(root, key).querySelector('.admin-error')?.textContent ?? null;

async function open() {
  const root = await renderAt('/admin/settings');
  await settle(root);
  return root;
}

async function type(root: HTMLElement, key: string, value: string) {
  const input = control<HTMLInputElement>(root, key);
  await act(async () => {
    input.value = value;
    input.dispatchEvent(new Event('input'));
  });
}

async function submit(root: HTMLElement, key: string) {
  await act(async () => {
    form(root, key).requestSubmit();
  });
  await settle(root);
}

describe('the site settings screen', () => {
  it('lists every key the server lists, grouped by prefix, with a control for its kind', async () => {
    const root = await open();
    expect(screenOf(root)).toBe('admin-settings');
    const keys = [...root.querySelectorAll('[data-setting]')].map((e) => e.getAttribute('data-setting'));
    expect(keys.sort()).toEqual(golden().settings.map((s) => s.key).sort());
    expect([...root.querySelectorAll('[data-group]')].map((e) => e.getAttribute('data-group'))).toEqual([
      'General',
      'Flags',
      'Registration',
    ]);

    // A text key is a text field, an integer key a text field with a numeric
    // keyboard, a boolean a checkbox, and a key with choices a select.
    const name = control<HTMLInputElement>(root, 'site_name');
    expect([name.tagName, name.type, name.value]).toEqual(['INPUT', 'text', 'The Supper Club']);
    const threshold = control<HTMLInputElement>(root, 'flags.auto_hide_threshold');
    expect([threshold.type, threshold.inputMode, threshold.value]).toEqual(['text', 'numeric', '3']);
    const hist = control<HTMLInputElement>(root, 'public_edit_history');
    expect([hist.type, hist.checked]).toEqual(['checkbox', true]);
    const mode = control<HTMLSelectElement>(root, 'registration.mode');
    expect(mode.tagName).toBe('SELECT');
    expect([...mode.options].map((o) => o.value)).toEqual(['open', 'approval']);
    expect(mode.value).toBe('open');

    // Each field says what it is, what it takes and its default, from the
    // registry's own words, and the label names the key.
    const hint = form(root, 'flags.auto_hide_threshold').querySelector('.admin-hint')!.textContent;
    expect(hint).toBe('Open flags on a post that hide it pending review. It takes a whole number, 1 or more. Default: 3.');
    expect(form(root, 'registration.mode').querySelector('.admin-hint')!.textContent).toContain(
      'It takes one of "open", "approval". Default: "approval".',
    );
    expect(form(root, 'site_name').querySelector('label')!.textContent).toBe('site_name');
    // Reset is offered only for a key away from its default.
    const resets = [...root.querySelectorAll('[data-control="reset"]')].map((e) => e.closest('[data-setting]')!.getAttribute('data-setting'));
    expect(resets.sort()).toEqual(['registration.mode', 'site_name']);
  });

  it('says who last changed each key, by the three rules', async () => {
    const root = await open();
    const changed = (key: string) => form(root, key).querySelector('[data-changed]')?.textContent ?? null;
    // last changed from the screen: the admin and the time.
    expect(changed('site_name')).toBe(`Last changed by Site Admin (@root), ${stamp(1757610000000)}.`);
    // last changed from the command line: no account, and the time.
    expect(changed('registration.mode')).toBe(`Last changed from the command line, ${stamp(1757600000000)}.`);
    // never changed: nothing at all.
    expect(changed('flags.auto_hide_threshold')).toBeNull();
    expect(changed('public_edit_history')).toBeNull();
    expect(root.textContent!.split('from the command line').length - 1).toBe(1);
    // The rule itself, for a key set from the screen with no time, which
    // the server never sends: nothing rather than a misleading line.
    expect(lastChanged({ ...history, updated_by: null, updated_at: null })).toBe('');
  });

  it('saves one key, sending its value typed by kind, and redraws it from the answer', async () => {
    routes['PUT /api/v1/admin/settings/flags.auto_hide_threshold'] = answerAs('flags.auto_hide_threshold');
    routes['PUT /api/v1/admin/settings/public_edit_history'] = answerAs('public_edit_history');
    routes['PUT /api/v1/admin/settings/registration.mode'] = answerAs('registration.mode');
    const root = await open();

    await type(root, 'flags.auto_hide_threshold', ' 5 ');
    await submit(root, 'flags.auto_hide_threshold');
    const hist = control<HTMLInputElement>(root, 'public_edit_history');
    await act(async () => {
      hist.checked = false;
      hist.dispatchEvent(new Event('change'));
    });
    await submit(root, 'public_edit_history');
    const mode = control<HTMLSelectElement>(root, 'registration.mode');
    await act(async () => {
      mode.value = 'approval';
      mode.dispatchEvent(new Event('change'));
    });
    await submit(root, 'registration.mode');

    expect(writes()).toEqual([
      { method: 'PUT', url: '/api/v1/admin/settings/flags.auto_hide_threshold', body: { value: 5 } },
      { method: 'PUT', url: '/api/v1/admin/settings/public_edit_history', body: { value: false } },
      { method: 'PUT', url: '/api/v1/admin/settings/registration.mode', body: { value: 'approval' } },
    ]);
    expect(status(root, 'flags.auto_hide_threshold')).toBe('Saved.');
    expect(form(root, 'flags.auto_hide_threshold').querySelector('[data-changed]')!.textContent).toBe(
      `Last changed by Site Admin (@root), ${stamp(1757620000000)}.`,
    );
    // Back at its default, registration.mode offers no reset; the others,
    // now away from theirs, do.
    expect(form(root, 'registration.mode').querySelector('[data-control="reset"]')).toBeNull();
    expect(form(root, 'public_edit_history').querySelector('[data-control="reset"]')).not.toBeNull();
  });

  it('resets a key to its default', async () => {
    routes['PUT /api/v1/admin/settings/site_name'] = answerAs('site_name');
    const root = await open();
    await act(async () => {
      form(root, 'site_name').querySelector<HTMLButtonElement>('[data-control="reset"]')!.click();
    });
    await settle(root);
    expect(writes()).toEqual([{ method: 'PUT', url: '/api/v1/admin/settings/site_name', body: { value: 'Ketju' } }]);
    expect(control<HTMLInputElement>(root, 'site_name').value).toBe('Ketju');
    expect(status(root, 'site_name')).toBe('Saved.');
    expect(form(root, 'site_name').querySelector('[data-control="reset"]')).toBeNull();
  });

  it('shows a refusal with what the key takes, and refuses a non-number before sending', async () => {
    routes['PUT /api/v1/admin/settings/flags.auto_hide_threshold'] = () => ({
      status: 422,
      body: { type: 'validation-failed', title: 'Validation failed', status: 422 },
    });
    const root = await open();
    await type(root, 'flags.auto_hide_threshold', '0');
    await submit(root, 'flags.auto_hide_threshold');
    expect(error(root, 'flags.auto_hide_threshold')).toBe('That was not accepted. It takes a whole number, 1 or more.');
    expect(control<HTMLInputElement>(root, 'flags.auto_hide_threshold').getAttribute('aria-invalid')).toBe('true');
    expect(status(root, 'flags.auto_hide_threshold')).toBeNull();

    await type(root, 'flags.auto_hide_threshold', '1e3');
    await submit(root, 'flags.auto_hide_threshold');
    expect(writes()).toHaveLength(1);
    expect(error(root, 'flags.auto_hide_threshold')).toBe('Enter a whole number. It takes a whole number, 1 or more.');
  });

  it('clears Saved on an edit, and shows it for an answer only while the field holds what was sent', async () => {
    let release!: () => void;
    const answer = answerAs('site_name');
    routes['PUT /api/v1/admin/settings/site_name'] = (body) =>
      new Promise((resolve) => {
        release = () => resolve(answer(body));
      });
    const root = await open();

    // An edit while the save is in flight: the answer does not say Saved.
    await type(root, 'site_name', 'The Dinner Club');
    await act(async () => {
      form(root, 'site_name').requestSubmit();
    });
    await type(root, 'site_name', 'The Dinner Club!');
    await act(async () => release());
    await settle(root);
    expect(status(root, 'site_name')).toBeNull();
    expect(control<HTMLInputElement>(root, 'site_name').value).toBe('The Dinner Club!');

    // No edit in flight: Saved, until the next edit.
    await act(async () => {
      form(root, 'site_name').requestSubmit();
    });
    await act(async () => release());
    await settle(root);
    expect(status(root, 'site_name')).toBe('Saved.');
    await type(root, 'site_name', 'The Dinner Club?');
    expect(status(root, 'site_name')).toBeNull();
  });

  it('names each config.toml section with its reason and keys, and shows no values', async () => {
    const root = await open();
    const config = root.querySelector<HTMLElement>('[data-config]')!;
    expect(config.textContent).toContain(configLine);
    const sections = [...config.querySelectorAll<HTMLElement>('[data-section]')];
    expect(sections.map((s) => s.dataset.section)).toEqual(['listen', 'oidc']);
    expect(sections[1].textContent).toContain('It holds provider secrets, and is needed before anyone can sign in.');
    expect([...sections[1].querySelectorAll('code')].map((c) => c.textContent)).toEqual([
      'oidc',
      'oidc.providers[].key',
      'oidc.providers[].issuer',
      'oidc.providers[].client_secret',
    ]);
    expect(config.querySelector('input, select')).toBeNull();
  });

  it('groups by the part of the key before its first dot, General first', () => {
    const s = (key: string) => ({ ...history, key });
    expect(groups([s('posts.a'), s('agents.b'), s('site_name'), s('posts.c')]).map(([n, ks]) => [n, ks.map((k) => k.key)])).toEqual([
      ['General', ['site_name']],
      ['Agents', ['agents.b']],
      ['Posts', ['posts.a', 'posts.c']],
    ]);
  });
});
