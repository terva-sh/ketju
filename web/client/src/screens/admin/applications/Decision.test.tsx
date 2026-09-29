import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me } from '../../../api/dto';
import { viewer } from '../../../state/viewer';
import { problemAnswer, renderAt, settle, stubServer, unmount, type StubServer } from '../../../testing';
import { approvedApp, appUrl, asModerator, pendingApp } from './fixtures';

// The approve and deny controls (TKT-01M3NRX2NN) on a pending application,
// at /admin/applications?id=42, against a stubbed server.

let saved: Me;
let server: StubServer;
beforeEach(() => {
  saved = viewer.value;
  viewer.value = asModerator();
  server = stubServer({ [`GET ${appUrl(42)}`]: () => ({ status: 200, body: pendingApp() }) });
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  vi.unstubAllGlobals();
});

const decision = (root: HTMLElement) => root.querySelector<HTMLElement>('[data-decision]')!;
const section = (root: HTMLElement, title: string) =>
  [...decision(root).querySelectorAll<HTMLElement>('section')].find((s) => s.querySelector('h3')?.textContent === title)!;
const button = (root: Element, name: string) =>
  [...root.querySelectorAll('button')].find((b) => b.textContent === name) ?? null;
const handleInput = (root: HTMLElement) => root.querySelector<HTMLInputElement>('[data-handle]')!;
const denyNote = (root: HTMLElement) => root.querySelector<HTMLTextAreaElement>('[data-deny-note]')!;

async function type(el: HTMLInputElement | HTMLTextAreaElement, value: string) {
  await act(async () => {
    el.value = value;
    el.dispatchEvent(new Event('input'));
  });
}

async function click(root: HTMLElement, el: HTMLElement | null) {
  expect(el).not.toBeNull();
  await act(async () => el!.click());
  await settle(root);
}

// confirm presses an action's button and then its Confirm, which carries
// the same name.
async function confirm(root: HTMLElement, title: string) {
  const s = section(root, title);
  await click(root, button(s, title));
  const buttons = [...s.querySelectorAll('button')];
  expect(buttons.map((b) => b.textContent)).toEqual([title, 'Cancel']);
  await click(root, buttons[0]);
}

const describedBy = (el: Element) =>
  (el.getAttribute('aria-describedby') ?? '')
    .split(' ')
    .filter(Boolean)
    .map((id) => document.getElementById(id)?.textContent);

describe('approving', () => {
  it('prefills the suggested handle, which stays editable', async () => {
    const root = await renderAt('/admin/applications?id=42');
    const h = handleInput(root);
    expect(h.value).toBe('carol');
    expect(h.readOnly).toBe(false);
    await type(h, 'carol_a');
    expect(handleInput(root).value).toBe('carol_a');
  });

  it('sends the handle as edited, with a note when one is written, and shows the approval', async () => {
    let answer = approvedApp();
    server.routes[`POST ${appUrl(42)}/approve`] = (body) => {
      answer = approvedApp();
      answer.id = 42;
      answer.decision!.user!.handle = (body as { handle: string }).handle;
      return { status: 200, body: answer };
    };
    const root = await renderAt('/admin/applications?id=42');
    await type(handleInput(root), ' carol_a ');
    await type(section(root, 'Approve').querySelector('textarea')!, 'Known to Alice.');
    await confirm(root, 'Approve');
    expect(server.sent.at(-1)).toEqual({
      method: 'POST',
      url: `${appUrl(42)}/approve`,
      body: { handle: 'carol_a', note: 'Known to Alice.' },
    });
    expect(root.querySelector('[data-notice]')!.textContent).toBe('Approved as @carol_a. They can sign in now.');
    expect(root.querySelector('[data-decision]')).toBeNull();
    expect(root.querySelector('[data-decided]')!.textContent).toContain('Approved by @bob');
    // The notice takes focus, since the button pressed has gone.
    expect(document.activeElement).toBe(root.querySelector('[data-notice]'));
  });

  it('leaves the note out when none is written', async () => {
    server.routes[`POST ${appUrl(42)}/approve`] = () => ({ status: 200, body: { ...approvedApp(), id: 42 } });
    const root = await renderAt('/admin/applications?id=42');
    await confirm(root, 'Approve');
    expect(server.sent.at(-1)!.body).toEqual({ handle: 'carol' });
  });

  it('cannot be asked for without a handle', async () => {
    const root = await renderAt('/admin/applications?id=42');
    await type(handleInput(root), '  ');
    expect(button(section(root, 'Approve'), 'Approve')!.disabled).toBe(true);
  });

  it.each([
    ['taken', 'That handle is in use'],
    ['reserved', 'That handle is kept for the site itself.'],
    ['invalid_handle', 'A handle is 3 to 30 characters'],
    ['something_new', 'That handle was not accepted.'],
  ])('attaches a %s handle refusal to the handle field', async (code, words) => {
    server.routes[`POST ${appUrl(42)}/approve`] = () =>
      problemAnswer('validation-failed', 422, { errors: [{ field: 'handle', code }] });
    const root = await renderAt('/admin/applications?id=42');
    await confirm(root, 'Approve');
    const h = handleInput(root);
    expect(h.getAttribute('aria-invalid')).toBe('true');
    expect(describedBy(h).join(' ')).toContain(words);
    // The confirmation stays open, to try again with another handle.
    expect(button(section(root, 'Approve'), 'Cancel')).not.toBeNull();
  });

  it('re-fetches on a conflict and names who decided', async () => {
    const taken = pendingApp();
    taken.status = 'denied';
    taken.decision = { by: { ...approvedApp().decision!.by!, handle: 'dave' }, at: 1757609000000, note: 'No.', user: null };
    let decided = false;
    server.routes[`GET ${appUrl(42)}`] = () => ({ status: 200, body: decided ? taken : pendingApp() });
    server.routes[`POST ${appUrl(42)}/approve`] = () => {
      decided = true;
      return problemAnswer('conflict', 409);
    };
    const root = await renderAt('/admin/applications?id=42');
    await confirm(root, 'Approve');
    expect(server.sent.map((s) => `${s.method} ${s.url}`)).toEqual([
      `GET ${appUrl(42)}`,
      `POST ${appUrl(42)}/approve`,
      `GET ${appUrl(42)}`,
    ]);
    expect(root.querySelector('[data-notice]')!.textContent).toBe('Already denied by @dave.');
    expect(root.querySelector('[data-decision]')).toBeNull();
    expect(root.querySelector('[data-decided]')!.textContent).toContain('Denied by @dave');
  });

  it('says so when the identity can no longer be approved', async () => {
    server.routes[`POST ${appUrl(42)}/approve`] = () => problemAnswer('conflict', 409);
    const root = await renderAt('/admin/applications?id=42');
    await confirm(root, 'Approve');
    expect(section(root, 'Approve').querySelector('[role="alert"]')!.textContent).toContain(
      'This identity can no longer be approved',
    );
  });

  it('says so when the viewer may no longer review', async () => {
    server.routes[`POST ${appUrl(42)}/approve`] = () => problemAnswer('forbidden', 403);
    const root = await renderAt('/admin/applications?id=42');
    await confirm(root, 'Approve');
    expect(section(root, 'Approve').querySelector('[role="alert"]')!.textContent).toBe(
      'Your account may no longer review applications.',
    );
  });
});

describe('denying', () => {
  it('says beside the note that the applicant never sees it', async () => {
    const root = await renderAt('/admin/applications?id=42');
    expect(describedBy(denyNote(root))).toContain('Moderators only. The applicant never sees this note.');
  });

  it('is off until a note is written', async () => {
    const root = await renderAt('/admin/applications?id=42');
    const deny = () => button(section(root, 'Deny'), 'Deny')!;
    expect(deny().disabled).toBe(true);
    await type(denyNote(root), '   ');
    expect(deny().disabled).toBe(true);
    await type(denyNote(root), 'Nobody knows them.');
    expect(deny().disabled).toBe(false);
  });

  it('sends the note and the block', async () => {
    const denied = { ...pendingApp(), status: 'denied' };
    denied.decision = { by: approvedApp().decision!.by, at: 1757609000000, note: 'Nobody knows them.', user: null };
    server.routes[`POST ${appUrl(42)}/deny`] = () => ({ status: 200, body: denied });
    const root = await renderAt('/admin/applications?id=42');
    await type(denyNote(root), 'Nobody knows them.');
    const check = section(root, 'Deny').querySelector<HTMLInputElement>('input[type="checkbox"]')!;
    expect(root.querySelector(`label[for="${check.id}"]`)!.textContent).toBe('Block this subject');
    await act(async () => check.click());
    await confirm(root, 'Deny');
    expect(server.sent.at(-1)).toEqual({
      method: 'POST',
      url: `${appUrl(42)}/deny`,
      body: { note: 'Nobody knows them.', block: true },
    });
    expect(root.querySelector('[data-notice]')!.textContent).toBe('Denied, and the identity is blocked from applying again.');
  });

  it('sends block false when the box is clear', async () => {
    server.routes[`POST ${appUrl(42)}/deny`] = () => ({ status: 200, body: { ...pendingApp(), status: 'denied' } });
    const root = await renderAt('/admin/applications?id=42');
    await type(denyNote(root), 'No.');
    await confirm(root, 'Deny');
    expect(server.sent.at(-1)!.body).toEqual({ note: 'No.', block: false });
    expect(root.querySelector('[data-notice]')!.textContent).toBe('Denied.');
  });

  it('asks to deny without blocking when the block cannot land', async () => {
    server.routes[`POST ${appUrl(42)}/deny`] = () => problemAnswer('conflict', 409);
    const root = await renderAt('/admin/applications?id=42');
    await type(denyNote(root), 'No.');
    await act(async () => section(root, 'Deny').querySelector<HTMLInputElement>('input[type="checkbox"]')!.click());
    await confirm(root, 'Deny');
    expect(section(root, 'Deny').querySelector('[role="alert"]')!.textContent).toBe(
      'This identity can no longer be blocked. Deny without blocking.',
    );
    // What was written is kept, to try again.
    expect(denyNote(root).value).toBe('No.');
  });

  it("attaches a note refusal to the deny note's field", async () => {
    server.routes[`POST ${appUrl(42)}/deny`] = () =>
      problemAnswer('validation-failed', 422, { errors: [{ field: 'note', code: 'invalid_text' }] });
    const root = await renderAt('/admin/applications?id=42');
    await type(denyNote(root), 'x');
    await confirm(root, 'Deny');
    expect(denyNote(root).getAttribute('aria-invalid')).toBe('true');
    expect(describedBy(denyNote(root))).toContain('The note holds a character that is not allowed.');
  });
});

describe('the keyboard', () => {
  // Every action on the page is a native control that takes focus in the
  // tab order, and a confirmation moves focus with it, so nothing is left
  // behind a pointer.
  it('reaches every action', async () => {
    const root = await renderAt('/admin/applications?id=42');
    // Deny is off until a note is written, and the note is typed.
    await type(denyNote(root), 'No.');
    await type(root.querySelector<HTMLTextAreaElement>('.app-add-note textarea')!, 'A note.');
    const detail = root.querySelector<HTMLElement>('[data-application="42"]')!;
    const actions = [...detail.querySelectorAll<HTMLElement>('button, input, textarea, select, a[href]')];
    expect(actions.length).toBeGreaterThan(6);
    for (const el of actions) {
      expect(el.tabIndex, el.outerHTML).toBeGreaterThanOrEqual(0);
      expect(el.getAttribute('aria-hidden')).toBeNull();
      el.focus();
      expect(document.activeElement, el.outerHTML).toBe(el);
    }
    const names = actions.map((el) => el.textContent || el.getAttribute('aria-label') || el.id);
    expect(names).toEqual(expect.arrayContaining(['Approve', 'Deny', 'Add note']));

    // Opening a confirmation focuses its Cancel; cancelling goes back.
    const approve = button(section(root, 'Approve'), 'Approve')!;
    approve.focus();
    await act(async () => approve.click());
    expect(document.activeElement?.textContent).toBe('Cancel');
    await act(async () => (document.activeElement as HTMLButtonElement).click());
    expect(document.activeElement).toBe(button(section(root, 'Approve'), 'Approve'));
  });
});
