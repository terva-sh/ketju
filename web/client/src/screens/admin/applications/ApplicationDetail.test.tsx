import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { AdminApplication, ApplicationNote, Me } from '../../../api/dto';
import { viewer } from '../../../state/viewer';
import { problemAnswer, renderAt, settle, stubServer, unmount, type StubServer } from '../../../testing';
import { approvedApp, appUrl, asModerator, deniedApp, listPage, pendingApp } from './fixtures';

// One application at /admin/applications?id=N (TKT-01M3NRX2NN): the answers
// through the shared form renderer in read mode, the identity, earlier
// applications, notes, the decision, and the block.

let saved: Me;
let server: StubServer;
beforeEach(() => {
  saved = viewer.value;
  viewer.value = asModerator();
  server = stubServer({
    [`GET ${appUrl(42)}`]: () => ({ status: 200, body: pendingApp() }),
    [`GET ${appUrl(17)}`]: () => ({ status: 200, body: deniedApp() }),
  });
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  vi.unstubAllGlobals();
});

const button = (root: Element, name: string) =>
  [...root.querySelectorAll('button')].find((b) => b.textContent === name) ?? null;

async function click(root: HTMLElement, el: HTMLElement | null) {
  expect(el).not.toBeNull();
  await act(async () => el!.click());
  await settle(root);
}

describe('an application', () => {
  it('shows the answers through the form renderer, against the version they answered', async () => {
    // The application answered version 1, whose label for "who" has since
    // changed: the version it answered is what is shown, and no other
    // version is asked for.
    const app = pendingApp();
    app.form.fields[2].label = 'How did you hear of us? (the old wording)';
    server.routes[`GET ${appUrl(42)}`] = () => ({ status: 200, body: app });
    const root = await renderAt('/admin/applications?id=42');

    const read = root.querySelector('.form-read')!;
    expect(read).not.toBeNull();
    const answer = (key: string) => read.querySelector(`[data-field="${key}"]`)!;
    expect(answer('who').querySelector('dt')!.textContent).toBe('How did you hear of us? (the old wording)');
    expect(answer('who').querySelector('dd')!.textContent).toBe('Alice invited me');
    expect(answer('name').querySelector('dd')!.textContent).toBe('Carol Applicant');
    expect(answer('rules').querySelector('dd')!.textContent).toBe('Yes');
    expect(read.querySelector('input, textarea, select')).toBeNull();
    expect(root.querySelector('[data-form-version]')!.textContent).toContain('version 1');
    expect(server.sent.map((s) => s.url)).toEqual([appUrl(42)]);
  });

  it("shows the identity's provider, email and verified flag, earlier applications and notes", async () => {
    const root = await renderAt('/admin/applications?id=42');
    const detail = root.querySelector('[data-application="42"]')!;
    expect(detail.querySelector('h2')!.textContent).toContain('Application #42');
    const facts = detail.querySelector('.admin-facts')!.textContent;
    expect(facts).toContain('carol@example.com');
    expect(facts).toContain('verified');
    expect(facts).toContain('via github');
    expect(detail.querySelector('[data-blocked]')).toBeNull();

    const prior = detail.querySelector('[data-prior]')!;
    expect(prior.querySelector('a')!.getAttribute('href')).toBe('/admin/applications?id=17');
    expect(prior.textContent).toContain('denied');

    const notes = detail.querySelector('[data-notes]')!;
    expect(notes.textContent).toContain('@bob');
    expect(notes.textContent).toContain('Alice vouched for her in the book club.');
    expect(root.querySelector('[data-no-email]')).not.toBeNull();
  });

  it('adds a note to the end of the list', async () => {
    const note: ApplicationNote = {
      id: 10,
      author: { handle: 'mona', display_name: 'Mona', avatar: '', role: 'moderator', kind: 'human' },
      body: 'Checked with Alice.',
      created_at: 1757606000000,
    };
    server.routes[`POST ${appUrl(42)}/notes`] = () => ({ status: 201, body: note });
    const root = await renderAt('/admin/applications?id=42');
    const add = button(root, 'Add note')!;
    expect(add.disabled).toBe(true);
    const box = root.querySelector<HTMLTextAreaElement>('.app-add-note textarea')!;
    await act(async () => {
      box.value = 'Checked with Alice.';
      box.dispatchEvent(new Event('input'));
    });
    expect(add.disabled).toBe(false);
    await click(root, add);
    expect(server.sent.at(-1)).toEqual({ method: 'POST', url: `${appUrl(42)}/notes`, body: { body: 'Checked with Alice.' } });
    const items = [...root.querySelectorAll('[data-notes] li')].map((li) => li.textContent);
    expect(items).toHaveLength(2);
    expect(items[1]).toContain('@mona');
    expect(items[1]).toContain('Checked with Alice.');
    expect(root.querySelector<HTMLTextAreaElement>('.app-add-note textarea')!.value).toBe('');
  });

  it("says why a note was refused, beside the note's field", async () => {
    server.routes[`POST ${appUrl(42)}/notes`] = () =>
      problemAnswer('validation-failed', 422, { errors: [{ field: 'body', code: 'too_long' }] });
    const root = await renderAt('/admin/applications?id=42');
    const box = root.querySelector<HTMLTextAreaElement>('.app-add-note textarea')!;
    await act(async () => {
      box.value = 'x';
      box.dispatchEvent(new Event('input'));
    });
    await click(root, button(root, 'Add note'));
    expect(box.getAttribute('aria-invalid')).toBe('true');
    expect(root.querySelector('.app-add-note .admin-error')!.textContent).toBe('Keep the note to 2000 characters or fewer.');
  });

  it('shows a denial with who decided, the note marked as never shown to the applicant, and the block', async () => {
    const root = await renderAt('/admin/applications?id=17');
    const decided = root.querySelector('[data-decided]')!;
    expect(decided.textContent).toContain('Denied by @bob');
    expect(decided.textContent).toContain('The applicant never sees it.');
    expect(decided.textContent).toContain('Nobody here knows them.');
    expect(root.querySelector('[data-decision]')).toBeNull();
    expect(root.querySelector('[data-blocked]')!.textContent).toContain('Blocked');
    expect(button(root, 'Unblock')).not.toBeNull();
  });

  it('shows an approval with the member it made', async () => {
    server.routes[`GET ${appUrl(42)}`] = () => ({ status: 200, body: approvedApp() });
    const root = await renderAt('/admin/applications?id=42');
    const decided = root.querySelector('[data-decided]')!;
    expect(decided.textContent).toContain('Approved by @bob');
    expect(decided.querySelector('a')!.getAttribute('href')).toBe('/u/carol');
    expect(root.textContent).toContain('The sign-in this came from has been removed.');
  });

  it('unblocks, and the Blocked marker goes', async () => {
    const lifted = deniedApp();
    lifted.identity!.blocked = false;
    server.routes[`POST ${appUrl(17)}/unblock`] = () => ({ status: 200, body: lifted });
    const root = await renderAt('/admin/applications?id=17');
    await click(root, button(root, 'Unblock'));
    await click(root, button(root.querySelector('[data-blocked]')!, 'Unblock'));
    expect(server.sent.at(-1)).toMatchObject({ method: 'POST', url: `${appUrl(17)}/unblock` });
    expect(root.querySelector('[data-blocked]')).toBeNull();
    expect(root.querySelector('[data-notice]')!.textContent).toContain('Unblocked.');
  });

  it('says so when another denial set the block', async () => {
    server.routes[`POST ${appUrl(17)}/unblock`] = () => problemAnswer('conflict', 409);
    const root = await renderAt('/admin/applications?id=17');
    await click(root, button(root, 'Unblock'));
    await click(root, button(root.querySelector('[data-blocked]')!, 'Unblock'));
    expect(server.sent.map((s) => `${s.method} ${s.url}`)).toEqual([
      `GET ${appUrl(17)}`,
      `POST ${appUrl(17)}/unblock`,
      `GET ${appUrl(17)}`,
    ]);
    expect(root.querySelector('[data-notice]')!.textContent).toContain('another of their applications was denied');
  });

  it('opens the next pending application from a decided one', async () => {
    server.routes['GET /api/v1/admin/applications?status=pending'] = () => ({ status: 200, body: listPage() });
    const root = await renderAt('/admin/applications?id=17');
    await click(root, button(root, 'Next pending'));
    expect(location.search).toBe('?id=42');
    await settle(root);
    expect(root.querySelector('[data-application="42"]')).not.toBeNull();
  });

  it('says when nothing else is waiting', async () => {
    const empty: { applications: AdminApplication[]; next_cursor: string } = { applications: [], next_cursor: '' };
    server.routes['GET /api/v1/admin/applications?status=pending'] = () => ({ status: 200, body: empty });
    const root = await renderAt('/admin/applications?id=17');
    await click(root, button(root, 'Next pending'));
    expect(root.textContent).toContain('No other application is waiting.');
  });

  it('says there is no such application', async () => {
    const root = await renderAt('/admin/applications?id=99');
    expect(root.textContent).toContain('There is no application #99.');
  });
});
