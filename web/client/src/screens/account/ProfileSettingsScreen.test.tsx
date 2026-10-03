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

function serve(routes: Record<string, (body: unknown) => Answer | Promise<Answer>> = {}) {
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
    const status = root.querySelector('[data-profile-form] [role=status]')!;
    expect(status.textContent).toContain('Saved.');
    expect(status.querySelector('a')?.getAttribute('href')).toBe('/u/alice');
  });

  // Terva review 2062, finding 1: what is typed while a save is on its way
  // is kept, and only the field still holding what was sent takes the
  // stored value.
  it('keeps an edit typed while the save is on its way', async () => {
    let answer!: (a: Answer) => void;
    const s = serve({ [`PATCH ${url}`]: () => new Promise<Answer>((r) => (answer = r)) });
    const root = await renderAt('/settings/profile');
    await type(root, bioField(root), 'Sent bio.');
    await type(root, nameField(root), 'Sent name');
    await submit(root);
    expect(s.sent.filter((r) => r.method === 'PATCH').map((r) => r.body)).toEqual([{ display_name: 'Sent name', bio: 'Sent bio.' }]);
    await type(root, bioField(root), 'Typed after Save.');
    await act(async () => answer({ status: 200, body: { ...stored(), display_name: 'Sent name', bio: 'Sent bio.' } }));
    await settle(root);
    expect(bioField(root).value).toBe('Typed after Save.');
    expect(nameField(root).value).toBe('Sent name');
    // The kept edit is still unsaved: Save sends it.
    await submit(root);
    expect(s.sent.filter((r) => r.method === 'PATCH').map((r) => r.body)[1]).toEqual({ bio: 'Typed after Save.' });
  });

  // Terva review 2062, finding 2: the name is limited in characters, as the
  // server counts them, not in UTF-16 units, so the input sets no maxlength.
  it('takes a name of 100 characters that are two UTF-16 units each', async () => {
    const s = serve({ [`PATCH ${url}`]: (b) => ({ status: 200, body: { ...stored(), ...(b as object) } }) });
    const root = await renderAt('/settings/profile');
    expect(nameField(root).hasAttribute('maxlength')).toBe(false);
    await type(root, nameField(root), '😀'.repeat(100));
    await submit(root);
    expect(s.sent.filter((r) => r.method === 'PATCH').map((r) => r.body)).toEqual([{ display_name: '😀'.repeat(100) }]);
    await type(root, nameField(root), '😀'.repeat(101));
    await submit(root);
    expect(s.sent.filter((r) => r.method === 'PATCH')).toHaveLength(1);
    expect(fieldError(root, 'display_name').textContent).toBe('A display name is at most 100 characters.');
  });

  it('any other refusal says why and keeps the edits', async () => {
    serve({ [`PATCH ${url}`]: () => problemAnswer('unavailable', 503) });
    const root = await renderAt('/settings/profile');
    await type(root, nameField(root), 'Alice L.');
    await submit(root);
    expect(root.querySelector('[data-profile-form] [role=alert]')?.textContent).toMatch(/briefly unavailable/);
    expect(nameField(root).value).toBe('Alice L.');
  });
});

// The avatar (TKT-01M3YSQ4CR, docs/05 "Display name and bio"): chosen,
// sent at once as PUT /settings/avatar, and removed with DELETE.
describe('the avatar', () => {
  const avatarUrl = '/api/v1/settings/avatar';
  const card = (root: HTMLElement) => root.querySelector<HTMLElement>('[data-avatar-card]')!;
  const avatarError = (root: HTMLElement) => card(root).querySelector<HTMLElement>('[data-field-error=avatar]')!;
  const image = (size = 10, type = 'image/png') => new File([new Uint8Array(size)], 'me.png', { type });

  async function pick(root: HTMLElement, f: File) {
    const input = card(root).querySelector<HTMLInputElement>('input[type=file]')!;
    Object.defineProperty(input, 'files', { value: [f], configurable: true });
    await act(() => void input.dispatchEvent(new Event('change', { bubbles: true })));
    for (let i = 0; i < 6; i++) await act(() => new Promise((r) => setTimeout(r, 0)));
    await settle(root);
  }

  it('draws the avatar the profile has, offers PNG and JPEG, and Remove', async () => {
    serve();
    const root = await renderAt('/settings/profile');
    const input = card(root).querySelector<HTMLInputElement>('input[type=file]')!;
    expect(input.accept).toBe('image/png,image/jpeg');
    expect(card(root).querySelector('label[for=profile-avatar]')?.textContent).toBe('Choose a new image');
    expect(card(root).querySelector('img.avatar')?.getAttribute('src')).toBe(`/avatars/${golden.avatar}`);
    expect(card(root).querySelector('[data-remove-avatar]')?.textContent?.trim()).toBe('Remove avatar');
  });

  it('sends a chosen image, shows the new one, and asks /me again', async () => {
    const withAvatar = structuredClone(memberMe) as Me;
    withAvatar.user!.avatar = 'f'.repeat(64);
    const s = serve({
      [`PUT ${avatarUrl}`]: () => ({ status: 200, body: { ...stored(), avatar: 'f'.repeat(64) } }),
      'GET /api/v1/me': () => ({ status: 200, body: withAvatar }),
    });
    const root = await renderAt('/settings/profile');
    await pick(root, image());
    const put = s.sent.find((r) => r.method === 'PUT');
    expect(put?.url).toBe(avatarUrl);
    expect((put?.body as FormData).get('file')).toBeInstanceOf(File);
    expect(card(root).querySelector('img.avatar')?.getAttribute('src')).toBe(`/avatars/${'f'.repeat(64)}`);
    expect(card(root).querySelector('[role=status]')?.textContent).toBe('Avatar saved.');
    expect(s.sent.map((r) => `${r.method} ${r.url}`)).toContain('GET /api/v1/me');
    expect(viewer.value.user?.avatar).toBe('f'.repeat(64));
  });

  it('Remove sends DELETE and draws the initial', async () => {
    const s = serve({ [`DELETE ${avatarUrl}`]: () => ({ status: 200, body: { ...stored(), avatar: '' } }) });
    const root = await renderAt('/settings/profile');
    await act(async () => card(root).querySelector<HTMLButtonElement>('[data-remove-avatar]')!.click());
    for (let i = 0; i < 4; i++) await act(() => new Promise((r) => setTimeout(r, 0)));
    await settle(root);
    expect(s.sent.filter((r) => r.method === 'DELETE').map((r) => r.url)).toEqual([avatarUrl]);
    expect(card(root).querySelector('img.avatar')).toBeNull();
    expect(card(root).querySelector('.avatar-fallback')).not.toBeNull();
    expect(card(root).querySelector('[data-remove-avatar]')).toBeNull();
    expect(card(root).querySelector('[role=status]')?.textContent).toBe('Avatar removed.');
  });

  // Terva review 2063: the avatar card and the profile form work side by
  // side, and each answer takes only the fields its own request set.
  it('keeps a profile save that lands while an avatar is on its way', async () => {
    let answer!: (a: Answer) => void;
    const s = serve({
      [`PUT ${avatarUrl}`]: () => new Promise<Answer>((r) => (answer = r)),
      [`PATCH ${url}`]: (b) => ({ status: 200, body: { ...stored(), ...(b as object) } }),
    });
    const root = await renderAt('/settings/profile');
    await pick(root, image());
    await type(root, bioField(root), 'Saved while the avatar went.');
    await submit(root);
    // The avatar's answer was written before the save, so it carries the old bio.
    await act(async () => answer({ status: 200, body: { ...stored(), avatar: 'f'.repeat(64) } }));
    for (let i = 0; i < 4; i++) await act(() => new Promise((r) => setTimeout(r, 0)));
    await settle(root);
    expect(bioField(root).value).toBe('Saved while the avatar went.');
    expect(card(root).querySelector('img.avatar')?.getAttribute('src')).toBe(`/avatars/${'f'.repeat(64)}`);
    // The saved bio is still the stored one: Save sends nothing.
    await submit(root);
    expect(s.sent.filter((r) => r.method === 'PATCH')).toHaveLength(1);
  });

  it('keeps an avatar set while a profile save is on its way', async () => {
    let answer!: (a: Answer) => void;
    serve({
      [`PATCH ${url}`]: () => new Promise<Answer>((r) => (answer = r)),
      [`PUT ${avatarUrl}`]: () => ({ status: 200, body: { ...stored(), avatar: 'f'.repeat(64) } }),
    });
    const root = await renderAt('/settings/profile');
    await type(root, bioField(root), 'A new bio.');
    await submit(root);
    await pick(root, image());
    expect(card(root).querySelector('img.avatar')?.getAttribute('src')).toBe(`/avatars/${'f'.repeat(64)}`);
    // The save's answer was written before the avatar, so it carries the old one.
    await act(async () => answer({ status: 200, body: { ...stored(), bio: 'A new bio.' } }));
    await settle(root);
    expect(card(root).querySelector('img.avatar')?.getAttribute('src')).toBe(`/avatars/${'f'.repeat(64)}`);
    expect(bioField(root).value).toBe('A new bio.');
  });

  it('refuses a file over 1 MB here, without sending', async () => {
    const s = serve();
    const root = await renderAt('/settings/profile');
    await pick(root, image((1 << 20) + 1));
    expect(s.sent.filter((r) => r.method === 'PUT')).toHaveLength(0);
    expect(avatarError(root).textContent).toBe('An avatar is at most 1 MB.');
  });

  it.each([
    ['type_not_allowed', 'An avatar is a PNG or JPEG image.'],
    ['image_too_large', 'This image is too large to use. Try a smaller one.'],
    ['blocked', "This image can't be used."],
    ['upload_budget', 'You have too many files waiting to be posted. Post them, or wait: files you remove are cleared within 7 days.'],
  ])('says why the server refused it: %s', async (code, words) => {
    serve({ [`PUT ${avatarUrl}`]: () => problemAnswer('validation-failed', 422, { errors: [{ field: 'file', code }] }) });
    const root = await renderAt('/settings/profile');
    await pick(root, image());
    expect(avatarError(root).textContent).toBe(words);
    expect(card(root).querySelector('input[type=file]')?.getAttribute('aria-invalid')).toBe('true');
    expect(card(root).querySelector('img.avatar')?.getAttribute('src')).toBe(`/avatars/${golden.avatar}`);
  });
});
