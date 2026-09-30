import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ManagedForum, ManagedForumList, Me } from '../../api/dto';
import { viewer } from '../../state/viewer';
import { renderAt, settle } from '../../testing';
import moderatorMe from '../../../../../internal/dto/testdata/golden/me.moderator.json';
import forumList from '../../../../../internal/dto/testdata/golden/admin-forums.admin.json';
import { archiveText, saving, visibilityHint } from './ForumsScreen';

// The admin forum screen (TKT-01M29CXK19), against a stubbed server that
// answers with the golden: general (public) with books (members) under it,
// and attic (restricted, archived).

const asAdmin = () => {
  const me = structuredClone(moderatorMe) as Me;
  me.user = { ...me.user!, role: 'admin' };
  return me;
};

type Answer = { status: number; body?: unknown };
let routes: Record<string, (body: unknown) => Answer | Promise<Answer>>;
let sent: { method: string; url: string; body: unknown }[];
let saved: Me;

const golden = () => structuredClone(forumList) as ManagedForumList;
const forum = (l: ManagedForumList, slug: string) => l.forums.find((f) => f.slug === slug)!;

beforeEach(() => {
  sent = [];
  routes = { 'GET /api/v1/admin/forums': () => ({ status: 200, body: golden() }) };
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

const row = (root: HTMLElement, slug: string) => root.querySelector<HTMLElement>(`[data-forum="${slug}"]`)!;
const button = (root: HTMLElement, slug: string, name: 'up' | 'down') =>
  row(root, slug).querySelector<HTMLButtonElement>(`[data-control="${name}"]`)!;
const select = (root: HTMLElement, slug: string, name: 'parent' | 'visibility') =>
  row(root, slug).querySelector<HTMLSelectElement>(`[data-control="${name}"]`)!;
const order = (root: HTMLElement) => [...root.querySelectorAll('[data-forum]')].map((e) => e.getAttribute('data-forum'));
const writes = () => sent.filter((s) => s.method !== 'GET');

async function type(input: HTMLInputElement, value: string) {
  await act(async () => {
    input.value = value;
    input.dispatchEvent(new Event('input'));
  });
}

async function choose(select: HTMLSelectElement, value: string, root: HTMLElement) {
  await act(async () => {
    select.value = value;
    select.dispatchEvent(new Event('change'));
  });
  await settle(root);
}

async function click(el: HTMLElement, root: HTMLElement) {
  await act(async () => el.click());
  await settle(root);
}

describe('the forums screen', () => {
  it('draws the tree: each root with the forums under it, and what is archived', async () => {
    const root = await renderAt('/admin/forums');
    expect(order(root)).toEqual(['general', 'books', 'attic']);
    expect(row(root, 'general').closest('li')!.querySelector('ol [data-forum="books"]')).not.toBeNull();
    expect(row(root, 'attic').querySelector('[data-archived]')).not.toBeNull();
    expect(row(root, 'general').querySelector('[data-archived]')).toBeNull();
    expect(row(root, 'general').textContent).toContain('12 threads, 140 posts');
  });

  it('creates a forum and redraws from the answer', async () => {
    const after = golden();
    after.forums.push({ ...forum(after, 'attic'), id: 9, slug: 'lounge', name: 'Lounge', archived: false, position: 3072 });
    routes['POST /api/v1/admin/forums'] = () => ({ status: 201, body: after });
    const root = await renderAt('/admin/forums');
    const form = root.querySelector<HTMLFormElement>('[data-create-forum]')!;
    const inputs = form.querySelectorAll('input');
    await type(inputs[0], 'lounge');
    await type(inputs[1], 'Lounge');
    const selects = form.querySelectorAll('select');
    await choose(selects[0], '1', root);
    await choose(selects[1], 'public', root);
    await act(async () => form.requestSubmit());
    await settle(root);
    expect(writes()).toEqual([
      {
        method: 'POST',
        url: '/api/v1/admin/forums',
        body: { slug: 'lounge', name: 'Lounge', description: '', parent_id: 1, visibility: 'public' },
      },
    ]);
    expect(order(root)).toEqual(['general', 'books', 'attic', 'lounge']);
    expect(inputs[0].value).toBe('');
  });

  it('moves with the buttons, relative to a sibling, and keeps focus on the moved forum', async () => {
    const after = golden();
    after.forums = [forum(after, 'attic'), forum(after, 'general'), forum(after, 'books')];
    routes['POST /api/v1/admin/forums/6/move'] = () => ({ status: 200, body: after });
    const root = await renderAt('/admin/forums');
    expect(button(root, 'general', 'up').disabled).toBe(true);
    expect(button(root, 'attic', 'down').disabled).toBe(true);
    // books is alone under general.
    expect(button(root, 'books', 'up').disabled).toBe(true);
    expect(button(root, 'books', 'down').disabled).toBe(true);

    button(root, 'attic', 'up').focus();
    await click(button(root, 'attic', 'up'), root);
    expect(writes()).toEqual([{ method: 'POST', url: '/api/v1/admin/forums/6/move', body: { parent_id: null, before: 1 } }]);
    expect(order(root)).toEqual(['attic', 'general', 'books']);
    // attic is first now, so its Move up is disabled and focus is on Move down.
    expect(document.activeElement).toBe(button(root, 'attic', 'down'));
  });

  it('moves down after the next sibling, and places a forum under a root', async () => {
    routes['POST /api/v1/admin/forums/1/move'] = () => ({ status: 200, body: golden() });
    routes['POST /api/v1/admin/forums/5/move'] = () => ({ status: 200, body: golden() });
    const root = await renderAt('/admin/forums');
    await click(button(root, 'general', 'down'), root);
    await choose(select(root, 'books', 'parent'), '', root);
    expect(writes()).toEqual([
      { method: 'POST', url: '/api/v1/admin/forums/1/move', body: { parent_id: null, after: 6 } },
      { method: 'POST', url: '/api/v1/admin/forums/5/move', body: { parent_id: null } },
    ]);
    // general has books under it, so it cannot be placed under a root.
    expect(select(root, 'general', 'parent').disabled).toBe(true);
    expect(row(root, 'general').textContent).toContain('It has forums under it');
    // attic may go under general, and not under itself.
    const options = [...select(root, 'attic', 'parent').options].map((o) => o.textContent);
    expect(options).toEqual(['Top level', 'General']);
  });

  it('says a visibility change takes effect within a minute, and sends it', async () => {
    routes['PUT /api/v1/admin/forums/5/visibility'] = () => ({ status: 200, body: golden() });
    const root = await renderAt('/admin/forums');
    for (const slug of ['general', 'books', 'attic']) {
      const hint = select(root, slug, 'visibility').getAttribute('aria-describedby')!;
      expect(document.getElementById(hint)!.textContent).toBe(visibilityHint);
    }
    expect(visibilityHint).toBe('Takes effect within a minute.');
    await choose(select(root, 'books', 'visibility'), 'restricted', root);
    expect(writes()).toEqual([
      { method: 'PUT', url: '/api/v1/admin/forums/5/visibility', body: { visibility: 'restricted' } },
    ]);
  });

  // AC7: the words are the same for every visibility, and none says who can
  // read the forum, because archiving changes no read access.
  it('the archive text says read access is unchanged', async () => {
    const root = await renderAt('/admin/forums');
    const byVisibility: Record<string, string> = {};
    for (const f of golden().forums as ManagedForum[]) {
      byVisibility[f.visibility] = row(root, f.slug).querySelector('[data-archive-text]')!.textContent!;
    }
    expect(Object.keys(byVisibility).sort()).toEqual(['members', 'public', 'restricted']);
    for (const words of Object.values(byVisibility)) {
      expect(words).toBe(archiveText);
      expect(words.startsWith('Anyone who could read it still can. Only posting stops, except for admins.')).toBe(true);
      expect(words).not.toMatch(/\b(members?|public|restricted|groups?|everyone|signed[- ]in)\b/i);
    }
  });

  it('archives and unarchives after asking', async () => {
    const archived = golden();
    forum(archived, 'general').archived = true;
    routes['PUT /api/v1/admin/forums/1/archived'] = () => ({ status: 200, body: archived });
    routes['PUT /api/v1/admin/forums/6/archived'] = () => ({ status: 200, body: archived });
    const root = await renderAt('/admin/forums');
    const press = async (slug: string, label: string) => {
      const b = [...row(root, slug).querySelectorAll('button')].find((x) => x.textContent === label)!;
      await click(b, root);
    };
    await press('general', 'Archive');
    expect(writes()).toEqual([]);
    expect(row(root, 'general').textContent).toContain('Archive General?');
    await press('general', 'Confirm');
    await press('attic', 'Unarchive');
    await press('attic', 'Confirm');
    expect(writes()).toEqual([
      { method: 'PUT', url: '/api/v1/admin/forums/1/archived', body: { archived: true } },
      { method: 'PUT', url: '/api/v1/admin/forums/6/archived', body: { archived: false } },
    ]);
    expect(row(root, 'general').querySelector('[data-archived]')).not.toBeNull();
  });

  // Terva review 1574: every answer is the whole list, so two changes in
  // flight could answer out of order and the older list hide the newer
  // change. One change is in flight at a time.
  it('saves one change at a time: the others wait while it is in flight', async () => {
    let release: (a: Answer) => void = () => {};
    routes['POST /api/v1/admin/forums/1/move'] = () => new Promise<Answer>((r) => (release = r));
    const root = await renderAt('/admin/forums');
    await act(async () => button(root, 'general', 'down').click());

    expect(button(root, 'attic', 'up').disabled).toBe(true);
    expect(select(root, 'books', 'visibility').disabled).toBe(true);
    expect(select(root, 'attic', 'parent').disabled).toBe(true);
    const archive = [...row(root, 'general').querySelectorAll('button')].find((b) => b.textContent === 'Archive')!;
    expect(archive.disabled).toBe(true);
    expect(root.querySelector<HTMLButtonElement>('[data-create-forum] button[type="submit"]')!.disabled).toBe(true);

    // A change asked for anyway is refused, not sent.
    await act(async () => {
      const v = select(root, 'attic', 'visibility');
      v.value = 'public';
      v.dispatchEvent(new Event('change'));
    });
    expect(row(root, 'attic').querySelector('[role="alert"]')!.textContent).toBe(saving);
    expect(writes()).toEqual([{ method: 'POST', url: '/api/v1/admin/forums/1/move', body: { parent_id: null, after: 6 } }]);

    await act(async () => release({ status: 200, body: golden() }));
    await settle(root);
    expect(button(root, 'attic', 'up').disabled).toBe(false);
    expect(select(root, 'books', 'visibility').disabled).toBe(false);
  });

  it('saves an edited name, and shows a refusal beside the forum', async () => {
    routes['PATCH /api/v1/admin/forums/5'] = () => ({ status: 409, body: { type: 'conflict', status: 409 } });
    const root = await renderAt('/admin/forums');
    const name = row(root, 'books').querySelector<HTMLInputElement>('form input')!;
    expect(row(root, 'books').querySelector('button[type="submit"]')).toBeNull();
    await type(name, 'Reading');
    const form = row(root, 'books').querySelector<HTMLFormElement>('form')!;
    await act(async () => form.requestSubmit());
    await settle(root);
    expect(writes()).toEqual([{ method: 'PATCH', url: '/api/v1/admin/forums/5', body: { name: 'Reading' } }]);
    expect(row(root, 'books').querySelector('[role="alert"]')!.textContent).toContain('Someone else changed this');
  });
});
