import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { CreatedPost, Me, Post, ThreadPage } from '../../api/dto';
import member from '../../../../../internal/dto/testdata/golden/me.member.json';
import moderator from '../../../../../internal/dto/testdata/golden/me.moderator.json';
import { isPhone } from '../../state/layout';
import { viewer } from '../../state/viewer';
import { problemAnswer, renderAt, settle, stubServer, threadPage, unmount, type Answer, type StubServer } from '../../testing';
import { maxTimeout } from './deadline';

// Edit, Delete and Restore on the viewer's own posts, on the thread page
// (TKT-01M3PTT97X), against a stubbed server. The viewer is the member
// golden's, alice, and post #61 is hers, a minute old, unless a test says
// otherwise; #62 is somebody else's.

const id = '01k4z3j0w8x2c';
const url = `/api/v1/threads/${id}`;
const postUrl = `${url}/posts/61`;
const base = threadPage({ id, slug: 'a', title: 'Mine' });
const len = base.thread.log_length;
const minute = 60_000;
const day = 86_400_000;

function post(over: Partial<Post> = {}): Post {
  return {
    ...base.posts[0],
    ordinal: 61,
    author: { ...base.posts[0].author, handle: 'alice' },
    created_at: Date.now() - minute,
    edited_at: null,
    edited_by_moderator: false,
    reply_to: null,
    reply_count: 0,
    attachments: [],
    html: '<p>my words</p>',
    deleted: false,
    removal: null,
    redacted: false,
    moved_to: null,
    ...over,
  };
}
const other = (): Post => ({ ...post({ ordinal: 62 }), author: { ...base.posts[1].author } });

type Windows = ThreadPage['viewer']['author_windows'];
function page(p: Post = post(), windows: Windows = { edit_seconds: 600, delete_seconds: 300 }, over: Partial<ThreadPage['viewer']> = {}): ThreadPage {
  return {
    ...base,
    thread: { ...base.thread, locked: false },
    viewer: { ...base.viewer, can_reply: true, can_moderate: false, next_post_at: null, author_windows: windows, ...over },
    posts: [p, other()],
  };
}
const created = (log: number): Answer => ({
  status: 200,
  body: { post: post(), ordinal: 61, revision: 3, log_length: log } satisfies CreatedPost,
  headers: { 'Cache-Control': 'no-store' },
});

// pages answers the thread GETs in turn, the last one again for any later
// request; a Promise holds its answer until it settles.
let pages: (ThreadPage | Promise<ThreadPage>)[];
let routes: Record<string, (b: unknown) => Answer | Promise<Answer>>;
let server: StubServer;
const saved = viewer.value;
beforeEach(() => {
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  viewer.value = structuredClone(member) as Me;
  pages = [page()];
  routes = {
    [`GET ${postUrl}/source`]: () => ({ status: 200, body: { ordinal: 61, revision: 2, source: 'my **words**' }, headers: { 'Cache-Control': 'no-store' } }),
    [`PATCH ${postUrl}`]: () => created(len + 1),
    [`DELETE ${postUrl}`]: () => created(len + 1),
    [`POST ${postUrl}/restore`]: () => created(len + 1),
  };
  server = stubServer({
    [`GET ${url}`]: async () => ({ status: 200, body: await (pages.length > 1 ? pages.shift()! : pages[0]) }),
    'POST /api/v1/render': () => ({ status: 200, body: { html: '<p>preview</p>' } }),
    [`GET ${postUrl}/source`]: (b) => routes[`GET ${postUrl}/source`](b),
    [`PATCH ${postUrl}`]: (b) => routes[`PATCH ${postUrl}`](b),
    [`DELETE ${postUrl}`]: (b) => routes[`DELETE ${postUrl}`](b),
    [`POST ${postUrl}/restore`]: (b) => routes[`POST ${postUrl}/restore`](b),
  });
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  isPhone.value = false;
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const at = (root: HTMLElement, ordinal = 61) => root.querySelector<HTMLElement>(`#post-${ordinal}`)!;
// The actions in a post's footer, outside the phone's More list. Unread from
// here, which every signed-in member has on every post, is left out: it is
// TKT-01M3KFFFJR's, and ThreadScreen.test.tsx covers it.
const footer = (root: HTMLElement, ordinal = 61) =>
  [...at(root, ordinal).querySelectorAll(':scope .post-footer [data-post-action]')]
    .filter((b) => !b.closest('.post-more-list'))
    .map((b) => b.getAttribute('data-post-action'))
    .filter((a) => a !== 'unread');
const control = (root: HTMLElement, name: string, ordinal = 61) =>
  at(root, ordinal).querySelector<HTMLButtonElement>(`[data-post-action=${name}]`)!;
const gets = () => server.sent.filter((r) => r.method === 'GET' && r.url === url).length;
const sent = (method: string) => server.sent.filter((r) => r.method === method && r.url.startsWith(postUrl));
const wait = () => act(() => new Promise((r) => setTimeout(r, 10)));
async function click(el: HTMLElement) {
  await act(async () => el.click());
  await wait();
}
const confirmOf = (root: HTMLElement) => at(root).querySelector<HTMLElement>('[data-confirm-delete]');
const confirmButton = (root: HTMLElement, text: string) =>
  [...confirmOf(root)!.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent === text)!;
const editor = (root: HTMLElement) => at(root).querySelector<HTMLElement>('[data-editing]');
const area = (root: HTMLElement) => editor(root)!.querySelector<HTMLTextAreaElement>('textarea')!;
const save = (root: HTMLElement) => editor(root)!.querySelector<HTMLButtonElement>('[data-edit-save]')!;
async function type(root: HTMLElement, value: string) {
  area(root).value = value;
  await act(async () => void area(root).dispatchEvent(new Event('input', { bubbles: true })));
}

describe("the viewer's own post", () => {
  it('offers edit and delete on it and nothing on another', async () => {
    const root = await renderAt(`/t/${id}/a`);
    expect(footer(root)).toEqual(['quote', 'reply', 'edit', 'delete']);
    expect(footer(root, 62)).toEqual(['quote', 'reply', 'flag']);
    // After flag and before history, as docs/06 draws the footer, with
    // unread from here (TKT-01M3KFFFJR) between them and history.
    expect([...at(root).querySelectorAll('.post-footer > *')].map((e) => e.textContent)).toEqual([
      'quote',
      'reply',
      'link',
      'edit',
      'delete',
      'unread from here',
      'history',
    ]);
  });

  // Unread from here leaves the page, so it is not offered on a post whose
  // editor or delete confirmation is open, and comes back when it closes
  // (TKT-01M3KFFFJR).
  it('offers no unread from here on a post being edited or deleted', async () => {
    const root = await renderAt(`/t/${id}/a`);
    const unread = (o = 61) => at(root, o).querySelector('[data-post-action=unread]');
    expect(unread()).not.toBeNull();
    await click(control(root, 'edit'));
    expect(editor(root)).not.toBeNull();
    expect(unread()).toBeNull();
    expect(unread(62)).not.toBeNull();
    await click([...editor(root)!.querySelectorAll('button')].find((b) => b.textContent === 'Cancel')!);
    expect(unread()).not.toBeNull();
    await click(control(root, 'delete'));
    expect(unread()).toBeNull();
    await click(confirmButton(root, 'Cancel'));
    expect(unread()).not.toBeNull();
  });

  it('the controls disappear when the window passes', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    pages = [page(post({ created_at: Date.now() - 14 * minute }), { edit_seconds: 900, delete_seconds: 900 })];
    const root = await renderAt(`/t/${id}/a`);
    expect(footer(root)).toEqual(['quote', 'reply', 'edit', 'delete']);
    await act(async () => void vi.advanceTimersByTime(minute));
    expect(footer(root)).toEqual(['quote', 'reply']);
    // No request was needed to withdraw them.
    expect(gets()).toBe(1);
  });

  it('a long window sets no immediate timer', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const timers = vi.spyOn(globalThis, 'setTimeout');
    pages = [page(post(), { edit_seconds: 40 * 86_400, delete_seconds: 40 * 86_400 })];
    const root = await renderAt(`/t/${id}/a`);
    await act(async () => void vi.advanceTimersByTime(maxTimeout));
    expect(footer(root)).toEqual(['quote', 'reply', 'edit', 'delete']);
    const delays = timers.mock.calls.map(([, d]) => d ?? 0);
    expect(delays.filter((d) => d > maxTimeout)).toEqual([]);
    // Set for the bound, fired early once, and set once more for what is
    // left of the 40 days: not a loop.
    const long = delays.filter((d) => d > day);
    expect(long).toHaveLength(2);
    expect(long[0]).toBe(maxTimeout);
    expect(long[1]).toBeGreaterThan(15 * day);
    expect(long[1]).toBeLessThan(maxTimeout);
    timers.mockRestore();
  });

  it('edit loads the source and sends the new one', async () => {
    const root = await renderAt(`/t/${id}/a`);
    pages = [{ ...page(post({ html: '<p>new words</p>' })), thread: { ...base.thread, log_length: len + 1 } }];
    await click(control(root, 'edit'));
    expect(server.sent.filter((r) => r.url === `${postUrl}/source`)).toHaveLength(1);
    expect(area(root).value).toBe('my **words**');
    expect(document.activeElement).toBe(area(root));
    // The body is replaced, and quote and reply wait for the editor to close.
    expect(at(root).querySelector(':scope > .post-main > .post-body')).toBeNull();
    expect(footer(root)).toEqual([]);
    // Unchanged source is not saved.
    expect(save(root).disabled).toBe(true);
    await type(root, 'new words');
    await click(save(root));
    expect(sent('PATCH')).toEqual([{ method: 'PATCH', url: postUrl, body: { source: 'new words' } }]);
    await settle(root);
    expect(editor(root)).toBeNull();
    expect(gets()).toBe(2);
    expect(at(root).querySelector('.post-body')?.innerHTML).toBe('<p>new words</p>');
    // The page shows the change, so the controls are back.
    expect(footer(root)).toEqual(['quote', 'reply', 'edit', 'delete']);
  });

  it('cancel drops the text and returns focus to edit', async () => {
    const root = await renderAt(`/t/${id}/a`);
    await click(control(root, 'edit'));
    await type(root, 'never mind');
    await click([...editor(root)!.querySelectorAll('button')].find((b) => b.textContent === 'Cancel')!);
    expect(editor(root)).toBeNull();
    expect(document.activeElement).toBe(control(root, 'edit'));
    expect(sent('PATCH')).toEqual([]);
    // Opened again, it starts from the source, not the dropped text.
    await click(control(root, 'edit'));
    expect(area(root).value).toBe('my **words**');
  });

  it('a source that fails to load says so, with a retry', async () => {
    let fail = true;
    routes[`GET ${postUrl}/source`] = () =>
      fail ? problemAnswer('unavailable', 503) : { status: 200, body: { ordinal: 61, revision: 2, source: 'back' } };
    const root = await renderAt(`/t/${id}/a`);
    await click(control(root, 'edit'));
    expect(editor(root)?.querySelector('[role=alert]')?.textContent).toBe('The server is briefly unavailable. Try again shortly.');
    fail = false;
    await click([...editor(root)!.querySelectorAll('button')].find((b) => b.textContent === 'Retry')!);
    expect(area(root).value).toBe('back');
  });

  it('delete asks first', async () => {
    const first = page();
    pages = [first];
    const root = await renderAt(`/t/${id}/a`);
    await click(control(root, 'delete'));
    expect(sent('DELETE')).toEqual([]);
    const text = confirmOf(root)!.querySelector('p')!.textContent!;
    expect(text).toMatch(/^Delete this post\? You can restore it until .+\.$/);
    const until = new Date(first.posts[0].created_at + 300_000).toISOString();
    expect(confirmOf(root)!.querySelector('time')?.getAttribute('datetime')).toBe(until);
    // Cancel has focus, so a second Enter does not delete; it returns focus
    // to delete.
    expect(document.activeElement).toBe(confirmButton(root, 'Cancel'));
    await click(confirmButton(root, 'Cancel'));
    expect(confirmOf(root)).toBeNull();
    expect(document.activeElement).toBe(control(root, 'delete'));

    pages = [{ ...page(post({ deleted: true, removal: 'author', html: '' })), thread: { ...base.thread, log_length: len + 1 } }];
    await click(control(root, 'delete'));
    await click(confirmButton(root, 'Delete'));
    expect(sent('DELETE')).toEqual([{ method: 'DELETE', url: postUrl, body: undefined }]);
    await settle(root);
    expect(gets()).toBe(2);
    // The author's own delete comes back as a tombstone, with restore.
    expect(at(root).classList.contains('tombstone')).toBe(true);
    expect(footer(root)).toEqual(['restore']);
  });

  it('nothing more is offered until the page shows the change', async () => {
    const root = await renderAt(`/t/${id}/a`);
    let arrive!: (p: ThreadPage) => void;
    pages = [new Promise<ThreadPage>((r) => (arrive = r))];
    await click(control(root, 'delete'));
    await click(confirmButton(root, 'Delete'));
    expect(gets()).toBe(2);
    // The page still shows the post, and offers nothing on it: a second
    // delete would only be refused.
    expect(at(root).classList.contains('tombstone')).toBe(false);
    expect(footer(root)).toEqual(['quote', 'reply']);
    // Focus is on the post, since its delete is gone.
    expect(document.activeElement).toBe(at(root));
    await act(async () => arrive({ ...page(post({ deleted: true, removal: 'author', html: '' })), thread: { ...base.thread, log_length: len + 1 } }));
    await settle(root);
    expect(footer(root)).toEqual(['restore']);
  });

  it('no restore it cannot use: the words leave it out without a window', async () => {
    viewer.value = structuredClone(moderator) as Me;
    pages = [page(post({ author: { ...base.posts[0].author, handle: 'bob' }, created_at: Date.now() - 30 * day }), null, { can_moderate: true })];
    const root = await renderAt(`/t/${id}/a`);
    // Bound by no window, a month on.
    expect(footer(root)).toEqual(['quote', 'reply', 'edit', 'delete']);
    await click(control(root, 'delete'));
    expect(confirmOf(root)!.querySelector('p')!.textContent).toBe('Delete this post?');
  });

  it('restore sends POST …/restore from a tombstone for an author', async () => {
    pages = [page(post({ deleted: true, removal: 'author', html: '' }))];
    const root = await renderAt(`/t/${id}/a`);
    expect(at(root).classList.contains('tombstone')).toBe(true);
    pages = [{ ...page(), thread: { ...base.thread, log_length: len + 1 } }];
    await click(control(root, 'restore'));
    expect(sent('POST')).toEqual([{ method: 'POST', url: `${postUrl}/restore`, body: undefined }]);
    await settle(root);
    expect(gets()).toBe(2);
    expect(at(root).classList.contains('tombstone')).toBe(false);
    expect(footer(root)).toEqual(['quote', 'reply', 'edit', 'delete']);
  });

  it('restore sends POST …/restore from a removed post for a moderator-author', async () => {
    viewer.value = structuredClone(moderator) as Me;
    const bobs = post({ author: { ...base.posts[0].author, handle: 'bob' }, deleted: true, removal: 'author', created_at: Date.now() - 30 * day });
    pages = [page(bobs, null, { can_moderate: true })];
    const root = await renderAt(`/t/${id}/a`);
    expect(at(root).classList.contains('removed')).toBe(true);
    expect(footer(root)).toEqual(['quote', 'reply', 'restore']);
    await click(control(root, 'restore'));
    expect(sent('POST')).toEqual([{ method: 'POST', url: `${postUrl}/restore`, body: undefined }]);
    expect(gets()).toBe(2);
  });

  it('no restore on a post a moderator removed, or on a redacted one', async () => {
    for (const p of [
      post({ deleted: true, removal: 'moderator', html: '<p>my words</p>' }),
      post({ deleted: true, removal: 'review', html: '<p>my words</p>' }),
      post({ deleted: true, removal: 'author', redacted: true, html: '' }),
    ]) {
      pages = [page(p)];
      const root = await renderAt(`/t/${id}/a`);
      expect(at(root).querySelector('[data-post-action=restore], [data-post-action=edit], [data-post-action=delete]'), `${p.removal} ${p.redacted}`).toBeNull();
    }
  });

  it('on a phone edit and delete are under More', async () => {
    isPhone.value = true;
    const root = await renderAt(`/t/${id}/a`);
    expect(footer(root)).toEqual(['quote', 'reply']);
    const more = at(root).querySelector<HTMLButtonElement>('[data-post-more]')!;
    await click(more);
    expect([...at(root).querySelectorAll('.post-more-list li')].map((li) => li.textContent)).toEqual([
      'link',
      'edit',
      'delete',
      'unread from here',
      'history',
    ]);
    await click(at(root).querySelector<HTMLButtonElement>('.post-more-list [data-post-action=delete]')!);
    expect(at(root).querySelector<HTMLElement>('.post-more-list')!.hidden).toBe(true);
    expect(document.activeElement).toBe(confirmButton(root, 'Cancel'));
    await click(confirmButton(root, 'Cancel'));
    expect(document.activeElement).toBe(more);

    // The editor, too, gives focus back to More.
    await click(more);
    await click(at(root).querySelector<HTMLButtonElement>('.post-more-list [data-post-action=edit]')!);
    expect(document.activeElement).toBe(area(root));
    await act(async () => void area(root).dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    await click([...editor(root)!.querySelectorAll('button')].find((b) => b.textContent === 'Cancel')!);
    expect(document.activeElement).toBe(more);
  });

  it('a window refusal shows its words', async () => {
    routes[`PATCH ${postUrl}`] = () => problemAnswer('edit-window-closed', 403);
    routes[`DELETE ${postUrl}`] = () => problemAnswer('delete-window-closed', 403);
    const root = await renderAt(`/t/${id}/a`);
    await click(control(root, 'edit'));
    await type(root, 'too late');
    await click(save(root));
    expect(editor(root)!.querySelector('[role=alert]')?.textContent).toBe('The time for editing this post has passed.');
    // The text stays, and Save, which would only be refused again, is off.
    expect(area(root).value).toBe('too late');
    expect(save(root).disabled).toBe(true);
    await click([...editor(root)!.querySelectorAll('button')].find((b) => b.textContent === 'Cancel')!);
    // The server's clock is the one that counts: edit is withdrawn.
    expect(footer(root)).toEqual(['quote', 'reply', 'delete']);

    await click(control(root, 'delete'));
    await click(confirmButton(root, 'Delete'));
    expect(confirmOf(root)!.querySelector('[role=alert]')?.textContent).toBe('The time for deleting or restoring this post has passed.');
    expect(confirmButton(root, 'Delete').disabled).toBe(true);
    await click(confirmButton(root, 'Cancel'));
    expect(footer(root)).toEqual(['quote', 'reply']);
    // Neither refusal asked for the page again.
    expect(gets()).toBe(1);
  });

  // Terva review 1610, finding 1: Cancel would drop the text and the
  // answer's words while a save was in flight.
  it('a save in flight keeps the editor open', async () => {
    let answer!: (a: Answer) => void;
    routes[`PATCH ${postUrl}`] = () => new Promise<Answer>((r) => (answer = r));
    const root = await renderAt(`/t/${id}/a`);
    await click(control(root, 'edit'));
    await type(root, 'held words');
    await click(save(root));
    const cancel = [...editor(root)!.querySelectorAll<HTMLButtonElement>('.composer-foot button')].find((b) => b.textContent === 'Cancel')!;
    expect(cancel.disabled).toBe(true);
    expect(save(root).textContent).toBe('Saving…');
    expect(area(root).readOnly).toBe(true);
    await act(async () => answer(problemAnswer('validation-failed', 422)));
    await wait();
    expect(editor(root)!.querySelector('[role=alert]')?.textContent).toBe(
      'The post was not accepted: it may be empty, or quote a post that is not in this thread.',
    );
    expect(area(root).value).toBe('held words');
    expect(cancel.disabled).toBe(false);
  });

  it('a delete in flight keeps the confirmation open', async () => {
    let answer!: (a: Answer) => void;
    routes[`DELETE ${postUrl}`] = () => new Promise<Answer>((r) => (answer = r));
    const root = await renderAt(`/t/${id}/a`);
    await click(control(root, 'delete'));
    await click(confirmButton(root, 'Delete'));
    expect(confirmButton(root, 'Cancel').disabled).toBe(true);
    await act(async () => void confirmOf(root)!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    expect(confirmOf(root)).not.toBeNull();
    await act(async () => answer(problemAnswer('unavailable', 503)));
    await wait();
    expect(confirmOf(root)!.querySelector('[role=alert]')?.textContent).toBe('The server is briefly unavailable. Try again shortly.');
    expect(confirmButton(root, 'Cancel').disabled).toBe(false);
  });

  // Terva review 1610, finding 2.
  it('a stale refusal of the source asks for the page again', async () => {
    routes[`GET ${postUrl}/source`] = () => problemAnswer('forbidden', 403);
    const root = await renderAt(`/t/${id}/a`);
    await click(control(root, 'edit'));
    expect(editor(root)?.querySelector('[role=alert]')?.textContent).toBe('You may no longer edit this post.');
    expect(gets()).toBe(2);
  });

  it('a conflict says so and asks for the page again', async () => {
    routes[`POST ${postUrl}/restore`] = () => problemAnswer('conflict', 409);
    pages = [page(post({ deleted: true, removal: 'author', html: '' }))];
    const root = await renderAt(`/t/${id}/a`);
    await click(control(root, 'restore'));
    expect(at(root).querySelector('[role=alert]')?.textContent).toBe(
      'This post can no longer be restored. It may have changed, or its thread or forum been locked or archived.',
    );
    expect(gets()).toBe(2);
  });

  it('the editor goes when the viewer is no longer the author', async () => {
    const root = await renderAt(`/t/${id}/a`);
    await click(control(root, 'edit'));
    expect(area(root).value).toBe('my **words**');
    const carol = structuredClone(member) as Me;
    carol.user!.handle = 'carol';
    await act(async () => void (viewer.value = carol));
    expect(editor(root)).toBeNull();
    expect(root.textContent).not.toContain('my **words**');
    // Alice's post is somebody else's to carol, who may flag it.
    expect(footer(root)).toEqual(['quote', 'reply', 'flag']);
    // And when alice is back, the editor stays closed until she opens it.
    const sources = server.sent.filter((r) => r.url === `${postUrl}/source`).length;
    await act(async () => void (viewer.value = structuredClone(member) as Me));
    await wait();
    expect(editor(root)).toBeNull();
    expect(server.sent.filter((r) => r.url === `${postUrl}/source`)).toHaveLength(sources);
    expect(footer(root)).toEqual(['quote', 'reply', 'edit', 'delete']);
  });
});

// A moderator's controls on everybody's posts (TKT-01M3QHKD21). The viewer is
// the moderator golden's, bob, unless a test says otherwise, and post #61 is
// carol's, a month old: no window binds a moderator, and the page sends none.
describe("a moderator's controls", () => {
  const carols = (over: Partial<Post> = {}) =>
    post({ author: { ...base.posts[0].author, handle: 'carol' }, created_at: Date.now() - 30 * day, ...over });
  const moderating = (p: Post = carols()) => page(p, null, { can_moderate: true });
  beforeEach(() => {
    viewer.value = structuredClone(moderator) as Me;
    pages = [moderating()];
  });

  it("a moderator's delete of another's post says who sees what", async () => {
    const root = await renderAt(`/t/${id}/a`);
    expect(footer(root)).toEqual(['quote', 'reply', 'flag', 'edit', 'delete']);
    expect(footer(root, 62)).toEqual(['quote', 'reply', 'flag', 'edit', 'delete']);
    await click(control(root, 'delete'));
    expect(confirmOf(root)!.querySelector('p')!.textContent).toBe(
      'Remove this post? Members will see that a moderator removed it. Only a moderator can restore it.',
    );
    pages = [{ ...moderating(carols({ deleted: true, removal: 'moderator' })), thread: { ...base.thread, log_length: len + 1 } }];
    await click(confirmButton(root, 'Delete'));
    expect(sent('DELETE')).toEqual([{ method: 'DELETE', url: postUrl, body: undefined }]);
    await settle(root);
    expect(gets()).toBe(2);
    // The moderator is served the post with its body, marked, and may put it
    // back from its footer.
    expect(at(root).classList.contains('removed')).toBe(true);
    expect(footer(root)).toEqual(['quote', 'reply', 'restore']);
  });

  it('a moderator restores a hidden post', async () => {
    for (const removal of ['moderator', 'review', 'author']) {
      server.sent.length = 0;
      pages = [moderating(carols({ deleted: true, removal })), { ...moderating(), thread: { ...base.thread, log_length: len + 1 } }];
      const root = await renderAt(`/t/${id}/a`);
      expect(footer(root), removal).toEqual(['quote', 'reply', 'restore']);
      await click(control(root, 'restore'));
      expect(sent('POST'), removal).toEqual([{ method: 'POST', url: `${postUrl}/restore`, body: undefined }]);
      await settle(root);
      expect(gets(), removal).toBe(2);
      expect(footer(root), removal).toEqual(['quote', 'reply', 'flag', 'edit', 'delete']);
      await unmount();
    }
  });

  it('nothing on a redacted post, and no restore on a tombstone', async () => {
    pages = [moderating(carols({ deleted: true, removal: 'moderator', redacted: true, html: '' }))];
    const root = await renderAt(`/t/${id}/a`);
    expect(at(root).classList.contains('tombstone')).toBe(true);
    expect(at(root).querySelector('[data-post-action]')).toBeNull();
  });

  it('a group moderator gets the same controls', async () => {
    // alice's role is member; the page says she moderates this forum, as a
    // group grant makes it say.
    viewer.value = structuredClone(member) as Me;
    const root = await renderAt(`/t/${id}/a`);
    expect(footer(root)).toEqual(['quote', 'reply', 'flag', 'edit', 'delete']);
    await click(control(root, 'edit'));
    expect(area(root).value).toBe('my **words**');
    await type(root, "carol's words, tidied");
    pages = [{ ...moderating(carols({ edited_at: Date.now(), edited_by_moderator: true })), thread: { ...base.thread, log_length: len + 1 } }];
    await click(save(root));
    expect(sent('PATCH')).toEqual([{ method: 'PATCH', url: postUrl, body: { source: "carol's words, tidied" } }]);
    await settle(root);
    expect(editor(root)).toBeNull();
    expect(at(root).querySelector('.edited')?.textContent).toBe('(edited by a moderator)');
  });

  it('a member without moderate gets nothing on it', async () => {
    viewer.value = structuredClone(member) as Me;
    pages = [page(carols(), { edit_seconds: 600, delete_seconds: 300 })];
    const root = await renderAt(`/t/${id}/a`);
    expect(footer(root)).toEqual(['quote', 'reply', 'flag']);
  });

  it('the editor goes when the signed-in viewer changes', async () => {
    const root = await renderAt(`/t/${id}/a`);
    await click(control(root, 'edit'));
    expect(area(root).value).toBe('my **words**');
    // Someone else signs in in this tab. The page on screen still says
    // can_moderate; the source bob loaded goes all the same.
    const dana = structuredClone(moderator) as Me;
    dana.user!.handle = 'dana';
    await act(async () => void (viewer.value = dana));
    expect(editor(root)).toBeNull();
    expect(root.textContent).not.toContain('my **words**');
  });

  // Terva review 1626, finding 1: the old page's can_moderate is not the new
  // viewer's. Nothing is offered until the page asked for again answers, and
  // then what that page says is offered.
  it("a new viewer is offered nothing from the last viewer's page", async () => {
    const root = await renderAt(`/t/${id}/a`);
    expect(footer(root)).toEqual(['quote', 'reply', 'flag', 'edit', 'delete']);
    let arrive!: (p: ThreadPage) => void;
    pages = [new Promise<ThreadPage>((r) => (arrive = r))];
    const dana = structuredClone(member) as Me;
    dana.user!.handle = 'dana';
    await act(async () => void (viewer.value = dana));
    await wait();
    expect(gets()).toBe(2);
    expect(footer(root)).toEqual(['quote', 'reply', 'flag']);
    expect(footer(root, 62)).toEqual(['quote', 'reply', 'flag']);
    // dana's page says she does not moderate: still nothing.
    await act(async () => arrive(page(carols(), { edit_seconds: 600, delete_seconds: 300 })));
    await settle(root);
    expect(footer(root)).toEqual(['quote', 'reply', 'flag']);
    // bob again: his page is asked for, and once it answers his controls
    // are back.
    pages = [moderating()];
    await act(async () => void (viewer.value = structuredClone(moderator) as Me));
    await settle(root);
    expect(gets()).toBe(3);
    expect(footer(root)).toEqual(['quote', 'reply', 'flag', 'edit', 'delete']);
  });

  it('a failed page for the new viewer keeps the offers withheld', async () => {
    const root = await renderAt(`/t/${id}/a`);
    server.routes[`GET ${url}`] = () => problemAnswer('unavailable', 503);
    const dana = structuredClone(moderator) as Me;
    dana.user!.handle = 'dana';
    await act(async () => void (viewer.value = dana));
    await settle(root);
    expect(gets()).toBe(2);
    expect(footer(root)).toEqual(['quote', 'reply', 'flag']);
  });
});

// The confirmation and the editor's status block take focus in the press
// that opened them, not a frame later, so a key pressed straight away lands
// in them rather than on the control that opened them (TKT-01M3TV4HBF). The
// presses are dispatched outside act, which would run the effects and hide
// the race.
describe('a key pressed straight after opening', () => {
  // enter presses Enter where focus is, as a browser does: a button that
  // has focus and whose keydown nobody took is clicked.
  const enter = () => {
    const at = document.activeElement as HTMLElement;
    const taken = !at.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }));
    if (!taken && at instanceof HTMLButtonElement) at.click();
  };
  // lull lets the raced frame run before the test goes on.
  const lull = () => act(() => new Promise((r) => setTimeout(r, 50)));

  it('Enter one microtask after delete opens the confirmation lands on Cancel and deletes nothing', async () => {
    const root = await renderAt(`/t/${id}/a`);
    const del = control(root, 'delete');
    del.focus();
    try {
      del.click();
      await Promise.resolve();
      expect(confirmOf(root)).not.toBeNull();
      expect(document.activeElement).toBe(confirmButton(root, 'Cancel'));
      enter();
      await Promise.resolve();
      expect(confirmOf(root)).toBeNull();
    } finally {
      await lull();
    }
    expect(sent('DELETE')).toEqual([]);
  });

  it('while the source loads, a key one microtask after opening lands in the status block', async () => {
    routes[`GET ${postUrl}/source`] = () => new Promise<Answer>(() => {});
    const root = await renderAt(`/t/${id}/a`);
    const edit = control(root, 'edit');
    edit.focus();
    try {
      edit.click();
      await Promise.resolve();
      expect(editor(root)?.textContent).toContain('Loading the post…');
      expect(editor(root)!.contains(document.activeElement)).toBe(true);
      // Focus is on Cancel, the block's one button while the source loads.
      enter();
      await Promise.resolve();
      expect(editor(root)).toBeNull();
    } finally {
      await lull();
    }
    expect(sent('PATCH')).toEqual([]);
  });
});
