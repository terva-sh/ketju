import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { AdminUserDetail, AdminUserList, HandleChange, Me } from '../../api/dto';
import { viewer } from '../../state/viewer';
import { pressRaw, problemAnswer, raceSettle, renderAt, route, settle, stubServer, unmount, type StubServer } from '../../testing';
import moderatorMe from '../../../../../internal/dto/testdata/golden/me.moderator.json';
import userList from '../../../../../internal/dto/testdata/golden/admin-users.admin.json';
import userAdmin from '../../../../../internal/dto/testdata/golden/admin-user.admin.json';
import userModerator from '../../../../../internal/dto/testdata/golden/admin-user.moderator.json';
import handleChange from '../../../../../internal/dto/testdata/golden/handle-change.admin.json';
import { authorHref } from './queue/QueueScreen';

// The rename control on the admin users screen (TKT-01M3P0CYR3), and the
// address the users tab keeps its open account in, against a stubbed server
// that answers with the goldens.

const asModerator = () => structuredClone(moderatorMe) as Me;
const asAdmin = () => {
  const me = asModerator();
  me.user = { ...me.user!, role: 'admin' };
  return me;
};

// detail is the admin golden under another handle.
function detail(handle: string): AdminUserDetail {
  const d = structuredClone(userAdmin) as AdminUserDetail;
  d.user.user.handle = handle;
  return d;
}

function list(...handles: string[]): AdminUserList {
  const l = structuredClone(userList) as AdminUserList;
  l.users = handles.map((h, i) => {
    const u = structuredClone(userList.users[0]) as AdminUserList['users'][number];
    u.id = 100 + i;
    u.user.handle = h;
    return u;
  });
  l.next_cursor = '';
  return l;
}

const renamed = (handle: string, previous: string): HandleChange => ({ ...(handleChange as HandleChange), handle, previous });
const fieldRefusal = (code: string) =>
  problemAnswer('validation-failed', 422, { errors: [{ field: 'handle', code }] });

let server: StubServer;
let saved: Me;
beforeEach(() => {
  saved = viewer.value;
  viewer.value = asAdmin();
  server = stubServer({
    'GET /api/v1/admin/users': () => ({ status: 200, body: list('alice', 'bob') }),
    'GET /api/v1/admin/users/alice': () => ({ status: 200, body: detail('alice') }),
    'GET /api/v1/admin/users/bob': () => ({ status: 200, body: detail('bob') }),
  });
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  vi.unstubAllGlobals();
});

const button = (el: Element, text: string) => [...el.querySelectorAll('button')].find((b) => b.textContent === text)!;
const press = async (root: HTMLElement, el: Element, text: string) => {
  await act(async () => button(el, text).click());
  await settle(root);
};
const field = (root: HTMLElement) => root.querySelector<HTMLInputElement>('[data-rename] input');
async function type(input: HTMLInputElement, value: string) {
  await act(async () => {
    input.value = value;
    input.dispatchEvent(new Event('input'));
  });
}
async function open(root: HTMLElement, handle: string) {
  await act(async () => root.querySelector<HTMLButtonElement>(`[data-user="${handle}"]`)!.click());
  await settle(root);
}
// attempt types a handle into the field and confirms the rename.
async function attempt(root: HTMLElement, to: string) {
  const rename = root.querySelector('[data-rename]')!;
  await type(field(root)!, to);
  await press(root, rename, 'Rename');
  await press(root, rename, 'Confirm');
}

describe('renaming an account', () => {
  it('renames only after asking, replaces the address, and asks for the list again', async () => {
    server.routes['POST /api/v1/admin/users/alice/rename'] = () => ({ status: 200, body: renamed('alicia', 'alice') });
    server.routes['GET /api/v1/admin/users/alicia'] = () => ({ status: 200, body: detail('alicia') });
    const root = await renderAt('/admin/users');
    await open(root, 'alice');
    expect(location.pathname + location.search).toBe('/admin/users?user=alice');
    const depth = history.length;

    const rename = root.querySelector('[data-detail="alice"] [data-rename]')!;
    expect(button(rename, 'Rename').disabled).toBe(true);
    await type(field(root)!, 'alicia');
    await press(root, rename, 'Rename');
    expect(rename.textContent).toContain('Rename @alice to @alicia?');
    expect(server.sent.some((s) => s.method === 'POST')).toBe(false);

    server.routes['GET /api/v1/admin/users'] = () => ({ status: 200, body: list('alicia', 'bob') });
    await press(root, rename, 'Confirm');
    await settle(root);
    expect(server.sent.filter((s) => s.method === 'POST')).toStrictEqual([
      { method: 'POST', url: '/api/v1/admin/users/alice/rename', body: { handle: 'alicia' } },
    ]);
    expect(location.pathname + location.search).toBe('/admin/users?user=alicia');
    expect(history.length).toBe(depth);
    const now = root.querySelector('[data-detail="alicia"]')!;
    expect(now.querySelector('h2')!.textContent).toContain('@alicia');
    expect(now.querySelector('[data-renamed]')!.textContent).toBe(
      'Renamed from @alice. The old handle is kept for 90 days and sends visitors here.',
    );
    expect(document.activeElement).toBe(now.querySelector('h2'));
    expect(server.sent.filter((s) => s.url === '/api/v1/admin/users')).toHaveLength(2);
    expect(root.querySelector('[data-user="alicia"]')).not.toBeNull();
    expect(root.querySelector('[data-user="alice"]')).toBeNull();
  });

  // TKT-01M3V7CFJ5: the Rename button focus was on goes with the old
  // detail, so the new one's title takes focus once it has loaded. Its
  // answer is held and let go outside act, and the key goes one microtask
  // after the new detail is drawn.
  it('after a rename, a key one microtask after the new detail loads lands on its title', async () => {
    let answer: () => void = () => {};
    const held = new Promise<void>((r) => (answer = r));
    server.routes['POST /api/v1/admin/users/alice/rename'] = () => ({ status: 200, body: renamed('alicia', 'alice') });
    server.routes['GET /api/v1/admin/users/alicia'] = async () => {
      await held;
      return { status: 200, body: detail('alicia') };
    };
    const root = await renderAt('/admin/users');
    await open(root, 'alice');
    const rename = root.querySelector('[data-detail="alice"] [data-rename]')!;
    await type(field(root)!, 'alicia');
    await press(root, rename, 'Rename');
    await act(async () => button(rename, 'Confirm').click());
    // The new detail is mounted and waiting on its own answer.
    for (let i = 0; i < 200 && !server.sent.some((s) => s.url === '/api/v1/admin/users/alicia'); i++) {
      await act(() => new Promise((r) => setTimeout(r, 5)));
    }
    expect(root.querySelector('[data-detail-pending="alicia"]')).not.toBeNull();
    const title = () => root.querySelector<HTMLElement>('[data-detail="alicia"] [data-renamed]') && root.querySelector<HTMLElement>('[data-detail="alicia"] h2');
    try {
      answer();
      for (let i = 0; i < 2000 && !title(); i++) await Promise.resolve();
      expect(title()).not.toBeNull();
      await Promise.resolve();
      expect(pressRaw('x')).toBe(title());
    } finally {
      await raceSettle();
    }
  });

  // Renaming your own account must not make it look like someone else's:
  // the suspension controls are never offered on your own account
  // (SuspensionPanel's mayAct, account.mayActOn on the server).
  it('gives the viewer the new handle when an admin renames their own account', async () => {
    server.routes['GET /api/v1/admin/users/bob'] = () => ({ status: 200, body: detail('bob') });
    server.routes['POST /api/v1/admin/users/bob/rename'] = () => ({ status: 200, body: renamed('robert', 'bob') });
    server.routes['GET /api/v1/admin/users/robert'] = () => ({ status: 200, body: detail('robert') });
    let answerMe!: () => void;
    server.routes['GET /api/v1/me'] = () =>
      new Promise((r) => (answerMe = () => r({ status: 200, body: { ...asAdmin(), user: { ...asAdmin().user!, handle: 'robert' } } })));
    const root = await renderAt('/admin/users?user=bob');
    expect(viewer.value.user!.handle).toBe('bob');
    const own = root.querySelector('[data-detail="bob"]')!;
    expect(button(own, 'Suspend')).toBeUndefined();

    await attempt(root, 'robert');
    await settle(root);
    // /me has not answered yet, and the viewer already has the new handle.
    expect(viewer.value.user!.handle).toBe('robert');
    const now = root.querySelector('[data-detail="robert"]')!;
    expect(now).not.toBeNull();
    expect(button(now, 'Suspend')).toBeUndefined();
    expect(now.querySelector('[data-suspension-panel]')?.textContent ?? '').not.toContain('Suspend');
    expect(server.sent.some((s) => s.url === '/api/v1/me')).toBe(true);
    await act(async () => answerMe());
    expect(viewer.value.user!.handle).toBe('robert');
  });

  it('leaves the viewer alone when an admin renames someone else', async () => {
    server.routes['POST /api/v1/admin/users/alice/rename'] = () => ({ status: 200, body: renamed('alicia', 'alice') });
    server.routes['GET /api/v1/admin/users/alicia'] = () => ({ status: 200, body: detail('alicia') });
    const root = await renderAt('/admin/users?user=alice');
    await attempt(root, 'alicia');
    await settle(root);
    expect(viewer.value.user!.handle).toBe('bob');
    expect(server.sent.some((s) => s.url === '/api/v1/me')).toBe(false);
  });

  // Terva review 1597: an answer that arrives after the admin moved on
  // moves nobody back.
  it('leaves the admin where they went when the rename answers after they left the screen', async () => {
    let answer!: () => void;
    server.routes['POST /api/v1/admin/users/alice/rename'] = () =>
      new Promise((r) => (answer = () => r({ status: 200, body: renamed('alicia', 'alice') })));
    server.routes['GET /api/v1/admin/audit'] = () => ({ status: 200, body: { entries: [], next_cursor: '' } });
    const root = await renderAt('/admin/users?user=alice');
    await attempt(root, 'alicia');
    await route('/admin/audit');
    await settle(root);
    await act(async () => answer());
    await settle(root);
    expect(location.pathname + location.search).toBe('/admin/audit');
    expect(server.sent.some((s) => s.url === '/api/v1/admin/users/alicia')).toBe(false);
  });

  it('keeps the account the admin opened when the rename of another answers', async () => {
    let answer!: () => void;
    server.routes['POST /api/v1/admin/users/alice/rename'] = () =>
      new Promise((r) => (answer = () => r({ status: 200, body: renamed('alicia', 'alice') })));
    const root = await renderAt('/admin/users?user=alice');
    await attempt(root, 'alicia');
    await open(root, 'bob');
    server.routes['GET /api/v1/admin/users'] = () => ({ status: 200, body: list('alicia', 'bob') });
    await act(async () => answer());
    await settle(root);
    expect(location.search).toBe('?user=bob');
    expect(root.querySelector('[data-detail="bob"]')).not.toBeNull();
    expect(root.querySelector('[data-renamed]')).toBeNull();
    // The list is still asked for again, and shows the new handle.
    await settle(root);
    expect(server.sent.filter((s) => s.url === '/api/v1/admin/users')).toHaveLength(2);
    expect(root.querySelector('[data-user="alicia"]')).not.toBeNull();
  });

  it('says on the field that the old handle is kept and redirects', async () => {
    const root = await renderAt('/admin/users?user=alice');
    const input = field(root)!;
    const hint = document.getElementById(input.getAttribute('aria-describedby')!)!;
    expect(hint.textContent).toBe(
      'The old handle is kept for 90 days and sends visitors to the new one. Nobody else can take it until then.',
    );
  });

  it('offers a moderator no rename control', async () => {
    viewer.value = asModerator();
    server.routes['GET /api/v1/admin/users/alice'] = () => ({ status: 200, body: userModerator });
    const root = await renderAt('/admin/users?user=alice');
    const d = root.querySelector('[data-detail="alice"]')!;
    expect(d).not.toBeNull();
    expect(d.querySelector('[data-rename]')).toBeNull();
    expect(d.textContent).not.toContain('Change handle');
    expect(button(d, 'Rename')).toBeUndefined();
  });

  it('puts each handle code on the field, in the shared words, keeping what was typed', async () => {
    const words: Record<string, string> = {
      invalid_handle:
        'A handle is 3 to 30 characters: lowercase letters, digits, _ and -. It starts with a letter or digit and does not end with -.',
      reserved: 'That handle is kept for the site itself. Choose another.',
      taken: 'That handle is in use, or was given up too recently to reuse. Choose another.',
      same: "That is already this account's handle.",
    };
    const root = await renderAt('/admin/users?user=alice');
    for (const [code, text] of Object.entries(words)) {
      server.routes['POST /api/v1/admin/users/alice/rename'] = () => fieldRefusal(code);
      await attempt(root, `try-${code.replace('_', '-')}`);
      const input = field(root)!;
      const errorId = input.getAttribute('aria-describedby')!.split(' ').find((id) => id.endsWith('-error'))!;
      expect(document.getElementById(errorId)!.textContent, code).toBe(text);
      expect(input.getAttribute('aria-invalid')).toBe('true');
      expect(input.value).toBe(`try-${code.replace('_', '-')}`);
      // The question closed, and focus is on the field that says why.
      expect(button(root.querySelector('[data-rename]')!, 'Confirm')).toBeUndefined();
      expect(document.activeElement).toBe(input);
      expect(location.search).toBe('?user=alice');
    }
  });

  it('says a released handle is taken, and never that it was released', async () => {
    server.routes['POST /api/v1/admin/users/alice/rename'] = () => fieldRefusal('taken');
    const root = await renderAt('/admin/users?user=alice');
    await attempt(root, 'bobby');
    const rename = root.querySelector('[data-rename]')!;
    expect(rename.textContent).toContain('given up too recently');
    expect(root.textContent!.toLowerCase()).not.toContain('released');
  });

  it('says in words that the account cannot be renamed, or is not there', async () => {
    server.routes['POST /api/v1/admin/users/alice/rename'] = () => problemAnswer('conflict', 409);
    const root = await renderAt('/admin/users?user=alice');
    await attempt(root, 'alicia');
    const alert = () => root.querySelector('[data-rename] > [role="alert"]')!.textContent;
    expect(alert()).toBe(
      'This account cannot be renamed. It may have been anonymized, or renamed by someone else. Reload to see it as it is now.',
    );
    // The question stays open, so the admin can cancel.
    expect(button(root.querySelector('[data-rename]')!, 'Cancel')).toBeDefined();

    server.routes['POST /api/v1/admin/users/alice/rename'] = () => problemAnswer('not-found', 404);
    await press(root, root.querySelector('[data-rename]')!, 'Confirm');
    expect(alert()).toBe('No account has this handle now. It may have been renamed. Reload the list.');
    expect(location.search).toBe('?user=alice');
  });

  it('offers an anonymized account no field', async () => {
    server.routes['GET /api/v1/admin/users/deleted-user-7'] = () => ({ status: 200, body: detail('deleted-user-7') });
    const root = await renderAt('/admin/users?user=deleted-user-7');
    const d = root.querySelector('[data-detail="deleted-user-7"]')!;
    expect(d.querySelector('[data-rename-anonymized]')!.textContent).toBe('An anonymized account cannot be renamed.');
    expect(d.querySelector('[data-rename]')).toBeNull();
    expect(button(d, 'Rename')).toBeUndefined();
  });
});

describe('the users tab address', () => {
  it('opens the account ?user= names', async () => {
    const root = await renderAt('/admin/users?user=bob');
    expect(root.querySelector('[data-detail="bob"]')).not.toBeNull();
    expect(server.sent.some((s) => s.url === '/api/v1/admin/users/bob')).toBe(true);
  });

  // The moderator queue's author link for staff (authorHref, PR #185).
  it('filters the list by ?prefix=, which is where the queue links an author', async () => {
    viewer.value = asModerator();
    server.routes['GET /api/v1/admin/users?prefix=bo'] = () => ({ status: 200, body: list('bob') });
    const href = authorHref('bo');
    expect(href).toBe('/admin/users?prefix=bo');
    const root = await renderAt(href);
    expect(server.sent.map((s) => s.url)).toContain('/api/v1/admin/users?prefix=bo');
    expect(server.sent.map((s) => s.url)).not.toContain('/api/v1/admin/users');
    expect(root.querySelector<HTMLInputElement>('.admin-filters input[type="search"]')!.value).toBe('bo');
    expect([...root.querySelectorAll('[data-user]')].map((b) => b.textContent)).toEqual(['bob']);
  });

  // Terva review 1596: the address's prefix is followed while the tab stays
  // mounted, not only read as it mounts.
  it('follows a new ?prefix= in the address, going back included, and keeps what was typed otherwise', async () => {
    server.routes['GET /api/v1/admin/users?prefix=bo'] = () => ({ status: 200, body: list('bob') });
    server.routes['GET /api/v1/admin/users?prefix=al'] = () => ({ status: 200, body: list('alice') });
    server.routes['GET /api/v1/admin/users?prefix=a'] = () => ({ status: 200, body: list('alice') });
    const root = await renderAt('/admin/users?prefix=bo');
    const search = () => root.querySelector<HTMLInputElement>('.admin-filters input[type="search"]')!;
    const rows = () => [...root.querySelectorAll('[data-user]')].map((b) => b.textContent);

    await route('/admin/users?prefix=al');
    await settle(root);
    expect(search().value).toBe('al');
    expect(server.sent.at(-1)?.url).toBe('/api/v1/admin/users?prefix=al');
    expect(rows()).toEqual(['alice']);

    // Back is a popstate to the earlier address, as route() makes one.
    await route('/admin/users?prefix=bo');
    await settle(root);
    expect(location.search).toBe('?prefix=bo');
    expect(search().value).toBe('bo');
    expect(rows()).toEqual(['bob']);

    // Typing is not undone by opening an account, whose address keeps the
    // same prefix.
    await act(async () => {
      search().value = 'a';
      search().dispatchEvent(new Event('input'));
    });
    await settle(root);
    await open(root, 'alice');
    expect(location.search).toBe('?prefix=bo&user=alice');
    expect(search().value).toBe('a');
    expect(server.sent.at(-1)?.url).not.toBe('/api/v1/admin/users?prefix=bo');
  });

  it('keeps the prefix in the address when an account is opened and closed', async () => {
    server.routes['GET /api/v1/admin/users?prefix=bo'] = () => ({ status: 200, body: list('bob') });
    const root = await renderAt('/admin/users?prefix=bo');
    await open(root, 'bob');
    expect(location.search).toBe('?prefix=bo&user=bob');
    await press(root, root.querySelector('[data-detail="bob"]')!, 'Close');
    expect(location.search).toBe('?prefix=bo');
    expect(document.activeElement).toBe(root.querySelector('[data-user="bob"]'));
  });
});
