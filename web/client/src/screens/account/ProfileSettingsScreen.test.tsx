import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me, ProfileSettings } from '../../api/dto';
import { viewer } from '../../state/viewer';
import { problemAnswer, renderAt, screenOf, settle, stubServer, unmount, type Answer } from '../../testing';
import memberMe from '../../../../../internal/dto/testdata/golden/me.member.json';
import golden from '../../../../../internal/dto/testdata/golden/profile-settings.member.json';

// /settings/profile (TKT-01M3V6XVVG, docs/05 "Display name and bio"): the
// member's display name and bio. Save sends only what changed. The member
// golden's handle is alice.

const url = '/api/v1/settings/profile';

let saved: Me;
beforeEach(() => {
  saved = viewer.value;
  viewer.value = structuredClone(memberMe) as Me;
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  vi.unstubAllGlobals();
});

const stored = (): ProfileSettings => structuredClone(golden);

function serve(routes: Record<string, (body: unknown) => Answer> = {}) {
  return stubServer({ [`GET ${url}`]: () => ({ status: 200, body: stored() }), ...routes });
}

const nameField = (root: HTMLElement) => root.querySelector<HTMLInputElement>('#profile-display_name')!;
const bioField = (root: HTMLElement) => root.querySelector<HTMLTextAreaElement>('#profile-bio')!;
const fieldError = (root: HTMLElement, key: string) => root.querySelector<HTMLElement>(`[data-field-error=${key}]`)!;
const count = (root: HTMLElement) => root.querySelector<HTMLElement>('[data-bio-count]')!;

async function type(root: HTMLElement, el: HTMLInputElement | HTMLTextAreaElement, value: string) {
  await act(async () => {
    el.value = value;
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await settle(root);
}

async function submit(root: HTMLElement) {
  await act(async () => root.querySelector<HTMLButtonElement>('[data-profile-form] button[type=submit]')!.click());
  await settle(root);
}

describe('the profile settings', () => {
  it('fills from GET and sends only the changed keys', async () => {
    const s = serve({ [`PATCH ${url}`]: (b) => ({ status: 200, body: { ...stored(), ...(b as object) } }) });
    const root = await renderAt('/settings/profile');
    expect(screenOf(root)).toBe('settings-profile');
    expect(nameField(root).value).toBe(golden.display_name);
    expect(bioField(root).value).toBe(golden.bio);

    await type(root, bioField(root), 'A new bio.');
    await submit(root);
    expect(s.sent.filter((r) => r.method === 'PATCH').map((r) => r.body)).toEqual([{ bio: 'A new bio.' }]);
    expect(bioField(root).value).toBe('A new bio.');

    // Nothing changed since: nothing is sent.
    await submit(root);
    expect(s.sent.filter((r) => r.method === 'PATCH')).toHaveLength(1);

    await type(root, nameField(root), '  Alice L.  ');
    await submit(root);
    expect(s.sent.filter((r) => r.method === 'PATCH').map((r) => r.body)[1]).toEqual({ display_name: '  Alice L.  ' });
  });

  it('a refused field shows its words and takes focus', async () => {
    serve({
      [`PATCH ${url}`]: () =>
        problemAnswer('validation-failed', 422, { errors: [{ field: 'bio', code: 'invalid_text' }] }),
    });
    const root = await renderAt('/settings/profile');
    await type(root, bioField(root), 'bad');
    await submit(root);
    expect(fieldError(root, 'bio').textContent).toBe('A bio cannot hold control characters.');
    expect(bioField(root).getAttribute('aria-invalid')).toBe('true');
    expect(document.activeElement).toBe(bioField(root));
    expect(fieldError(root, 'display_name').textContent).toBe('');
  });

  it('refuses a bio over the limit here, without sending', async () => {
    const s = serve();
    const root = await renderAt('/settings/profile');
    await type(root, bioField(root), 'ä'.repeat(2001));
    await submit(root);
    expect(s.sent.filter((r) => r.method === 'PATCH')).toHaveLength(0);
    expect(fieldError(root, 'bio').textContent).toBe('A bio is at most 2000 characters.');
    expect(document.activeElement).toBe(bioField(root));
  });

  it('the count speaks near the limit', async () => {
    serve();
    const root = await renderAt('/settings/profile');
    expect(count(root).getAttribute('aria-live')).toBe('polite');
    expect(count(root).textContent).toBe('');
    // Counted in characters, as the server counts: a two-unit emoji is one.
    await type(root, bioField(root), '😀'.repeat(1850));
    expect(count(root).textContent).toBe('150 characters left.');
    await type(root, bioField(root), 'x'.repeat(2003));
    expect(count(root).textContent).toBe('3 characters over.');
  });

  it('Saved refreshes the viewer and links to the profile', async () => {
    const renamed = structuredClone(memberMe) as Me;
    renamed.user!.display_name = 'Alice L.';
    const s = serve({
      [`PATCH ${url}`]: () => ({ status: 200, body: { ...stored(), display_name: 'Alice L.' } }),
      'GET /api/v1/me': () => ({ status: 200, body: renamed }),
    });
    const root = await renderAt('/settings/profile');
    await type(root, nameField(root), 'Alice L.');
    await submit(root);
    expect(s.sent.map((r) => `${r.method} ${r.url}`)).toContain('GET /api/v1/me');
    expect(viewer.value.user?.display_name).toBe('Alice L.');
    const status = root.querySelector('[role=status]')!;
    expect(status.textContent).toContain('Saved.');
    expect(status.querySelector('a')?.getAttribute('href')).toBe('/u/alice');
  });

  it('any other refusal says why and keeps the edits', async () => {
    serve({ [`PATCH ${url}`]: () => problemAnswer('unavailable', 503) });
    const root = await renderAt('/settings/profile');
    await type(root, nameField(root), 'Alice L.');
    await submit(root);
    expect(root.querySelector('[role=alert]')?.textContent).toMatch(/briefly unavailable/);
    expect(nameField(root).value).toBe('Alice L.');
  });
});
