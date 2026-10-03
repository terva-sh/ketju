import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me } from '../api/dto';
import { isPhone } from '../state/layout';
import { draftDelay } from '../state/drafts';
import { resetNewThreads } from '../state/newThread';
import { shellContext } from '../state/shell';
import { viewer } from '../state/viewer';
import { problemAnswer, renderAt, screenOf, stubServer, threadPage, unmount, type Answer, type StubServer } from '../testing';
import forums from '../../../../internal/dto/testdata/golden/forums.member.json';
import created from '../../../../internal/dto/testdata/golden/created-thread.member.json';
import anon from '../../../../internal/dto/testdata/golden/me.anon.json';
import member from '../../../../internal/dto/testdata/golden/me.member.json';

// The new-thread screen (TKT-01M3NS16VH) against a stubbed server: the forum
// tree from the member golden, where general is open and archive-2024 is
// archived, and the forum draft and create routes as each test says.

const general = '/f/general/new';
const archived = '/f/archive-2024/new';
const thread = created.thread;

const none = (): Answer => ({ status: 204, headers: { 'Draft-Revision': '0' } });
// savedAt is the server's updated_at for every save it answers.
const savedAt = Date.UTC(2025, 8, 10, 9, 30);
const saved = (slug: string) => (b: unknown): Answer => ({
  status: 200,
  body: { forum: slug, ...(b as object), updated_at: savedAt, revision: 1 },
});
// savedNote is what the note says once the server has saved the draft at at.
const savedNote = (root: HTMLElement, at = savedAt) => {
  const time = root.querySelector('.draft-note time');
  expect(time?.getAttribute('datetime')).toBe(new Date(at).toISOString());
  return `Draft saved ${time!.textContent}`;
};

// serve stubs the forum tree, the draft routes and create for slug, and the
// thread page the screen goes to; over replaces any of them.
function serve(slug: string, over: StubServer['routes'] = {}): StubServer {
  return stubServer({
    'GET /api/v1/forums': () => ({ status: 200, body: forums }),
    [`GET /api/v1/forums/${slug}/draft`]: none,
    [`PUT /api/v1/forums/${slug}/draft`]: saved(slug),
    [`DELETE /api/v1/forums/${slug}/draft`]: () => ({ status: 204, headers: { 'Draft-Revision': '2' } }),
    [`POST /api/v1/forums/${slug}/threads`]: () => ({ status: 201, body: created }),
    'POST /api/v1/render': (b) => ({ status: 200, body: { html: `<p>${(b as { source: string }).source}</p>`, mentions: [], quotes: [], reply_to: null } }),
    [`GET /api/v1/threads/${thread.id}?page=1`]: () => ({ status: 200, body: threadPage(thread) }),
    ...over,
  });
}

const title = (root: HTMLElement) => root.querySelector<HTMLInputElement>('#new-thread-title')!;
const area = (root: HTMLElement) => root.querySelector<HTMLTextAreaElement>('.composer textarea')!;
const post = (root: HTMLElement) => root.querySelector<HTMLButtonElement>('[data-post-thread]')!;
const alert = (root: HTMLElement) => root.querySelector('[role=alert]')?.textContent ?? null;
const note = (root: HTMLElement) => root.querySelector('.draft-note')?.textContent ?? null;

async function fill(el: HTMLInputElement | HTMLTextAreaElement, text: string) {
  el.value = text;
  el.setSelectionRange(text.length, text.length);
  await act(() => void el.dispatchEvent(new Event('input', { bubbles: true })));
}
async function write(root: HTMLElement, t: string, body: string) {
  await fill(title(root), t);
  await fill(area(root), body);
}
const wait = (ms: number) => act(() => new Promise((r) => setTimeout(r, ms)));
const click = (el: Element | null) => act(() => (el as HTMLElement).click());
const sent = (s: StubServer, method: string, url: string) => s.sent.filter((r) => r.method === method && r.url === url);

let prior: Me;
beforeEach(() => {
  prior = viewer.value;
  viewer.value = structuredClone(member) as Me;
  isPhone.value = false;
  resetNewThreads();
  localStorage.clear();
});
afterEach(async () => {
  await unmount();
  vi.useRealTimers();
  resetNewThreads();
  viewer.value = prior;
  isPhone.value = false;
  vi.unstubAllGlobals();
});

describe('the new-thread screen', () => {
  it('answers /f/{forum}/new with a labelled title and composer', async () => {
    serve('general');
    const root = await renderAt(general);
    expect(screenOf(root)).toBe('new-thread');
    expect(root.querySelector('h1')!.textContent).toBe('New thread');
    const label = root.querySelector<HTMLLabelElement>('label[for=new-thread-title]')!;
    expect(label.textContent).toBe('Title');
    expect(title(root).maxLength).toBe(200);
    expect(area(root).getAttribute('aria-label')).toBe('Thread text');
    expect(root.querySelector('.new-thread-up')!.textContent).toBe('General');
  });

  it('keeps Post off while the title or the body is blank', async () => {
    serve('general');
    const root = await renderAt(general);
    expect(post(root).disabled).toBe(true);
    await fill(title(root), 'A title');
    expect(post(root).disabled).toBe(true);
    await fill(area(root), '   ');
    expect(post(root).disabled).toBe(true);
    await fill(area(root), 'Text');
    expect(post(root).disabled).toBe(false);
    await fill(title(root), '  ');
    expect(post(root).disabled).toBe(true);
  });

  it('creates the thread, deletes the draft, and goes to the thread', async () => {
    const s = serve('general');
    const root = await renderAt(general);
    await write(root, 'Why we left Slack', 'The bill was the smaller half of it.');
    await click(post(root));
    await wait(20);
    expect(sent(s, 'POST', '/api/v1/forums/general/threads').map((r) => r.body)).toEqual([
      { title: 'Why we left Slack', source: 'The bill was the smaller half of it.' },
    ]);
    expect(sent(s, 'DELETE', '/api/v1/forums/general/draft')).toHaveLength(1);
    expect(location.pathname + location.search).toBe(`/t/${thread.id}/${thread.slug}?page=1`);
    expect(screenOf(root)).toBe('thread');
  });

  // TKT-01M3V73J8K.
  it('a new thread sends its attachments with ?forum=', async () => {
    const shot = { hash: 'c'.repeat(64), size: 2048, mime: 'image/png', original_name: 'shot.png', width: 64, height: 48 };
    const s = serve('general', { 'POST /api/v1/blobs?forum=general': () => ({ status: 201, body: shot }) });
    const root = await renderAt(general);
    await write(root, 'Photos', 'From the build.');
    const input = root.querySelector<HTMLInputElement>('.composer [data-attach-input]')!;
    Object.defineProperty(input, 'files', { value: [new File(['x'], 'shot.png', { type: 'image/png' })], configurable: true });
    await act(() => void input.dispatchEvent(new Event('change', { bubbles: true })));
    await wait(5);
    const upload = sent(s, 'POST', '/api/v1/blobs?forum=general');
    expect(upload.map((r) => (r.body as FormData).get('file'))).toMatchObject([{ name: 'shot.png' }]);
    expect(root.querySelector(`[data-file="${shot.hash}"]`)?.textContent).toBe('shot.png2.0 KB×');
    await click(post(root));
    await wait(20);
    expect(sent(s, 'POST', '/api/v1/forums/general/threads').map((r) => r.body)).toEqual([
      { title: 'Photos', source: 'From the build.', attachments: [shot.hash] },
    ]);
  });

  it('offers Attach only in a forum the tree says the viewer may upload to', async () => {
    serve('general');
    let root = await renderAt(general);
    expect(root.querySelector('.composer [data-attach]')).not.toBeNull();
    await unmount();
    // general-events: a forum the golden tree says no to.
    serve('general-events');
    root = await renderAt('/f/general-events/new');
    expect(screenOf(root)).toBe('new-thread');
    expect(root.querySelector('.composer [data-attach]')).toBeNull();
  });

  it('loads the draft, title and body, into the empty form', async () => {
    serve('general', {
      'GET /api/v1/forums/general/draft': () => ({ status: 200, body: { forum: 'general', title: 'Saved title', source: 'Saved body', updated_at: 0, revision: 4 } }),
    });
    const root = await renderAt(general);
    await wait(5);
    expect(title(root).value).toBe('Saved title');
    expect(area(root).value).toBe('Saved body');
  });

  it('saves the title and body as a draft while typing, and says so', async () => {
    const s = serve('general');
    const root = await renderAt(general);
    await wait(5);
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    await write(root, 'Bonfire', 'Friday at eight.');
    expect(note(root)).toBe('Saving…');
    await act(async () => void (await vi.advanceTimersByTimeAsync(draftDelay)));
    expect(sent(s, 'PUT', '/api/v1/forums/general/draft').map((r) => r.body)).toEqual([{ title: 'Bonfire', source: 'Friday at eight.' }]);
    expect(note(root)).toBe(savedNote(root));
  });

  it('previews the body as a first post, with no thread', async () => {
    const s = serve('general');
    const root = await renderAt(general);
    await fill(area(root), 'Hello');
    await wait(450);
    expect(sent(s, 'POST', '/api/v1/render').map((r) => r.body)).toEqual([{ source: 'Hello' }]);
  });

  it('shows NotFound for a forum not in the tree', async () => {
    serve('nowhere');
    const root = await renderAt('/f/nowhere/new');
    await wait(5);
    expect(screenOf(root)).toBe('not-found');
  });

  it('asks a signed-out reader to sign in, and loads nothing', async () => {
    viewer.value = structuredClone(anon) as Me;
    const s = serve('general');
    const root = await renderAt(general);
    expect(root.querySelector('.new-thread-signin a')!.getAttribute('href')).toBe(`/auth/signin?return_to=${encodeURIComponent(general)}`);
    expect(root.querySelector('textarea')).toBeNull();
    expect(s.sent.filter((r) => r.url.includes('/draft'))).toEqual([]);
  });
});

// Criterion 9: each refusal in words, the text kept.
describe('refusals', () => {
  it.each([
    ['forbidden', 403, 'You may not start threads in this forum.', true],
    ['conflict', 409, 'This forum is archived, so your thread was not posted.', true],
    ['validation-failed', 422, 'The title must be 1 to 200 characters with no control characters, and the post cannot be empty.', false],
    ['payload-too-large', 413, 'The post is longer than the limit allows.', false],
    ['rate-limited', 429, 'You are posting too quickly. You can try again in 30 seconds.', false],
  ])('says %s in words and keeps the text', async (type, status, words, off) => {
    serve('general', {
      'POST /api/v1/forums/general/threads': () => problemAnswer(type, status, {}, type === 'rate-limited' ? { 'Retry-After': '30' } : undefined),
    });
    const root = await renderAt(general);
    await write(root, 'T', 'body');
    await click(post(root));
    await wait(5);
    expect(alert(root)).toBe(words);
    expect(title(root).value).toBe('T');
    expect(area(root).value).toBe('body');
    expect(location.pathname).toBe(general);
    // Only the create route's own forbidden or conflict turns Post off.
    expect(post(root).disabled).toBe(off);
  });

  it('marks the field a validation-failed answer names', async () => {
    serve('general', {
      'POST /api/v1/forums/general/threads': () => problemAnswer('validation-failed', 422, { errors: [{ field: 'title', code: 'too_long' }] }),
    });
    const root = await renderAt(general);
    await write(root, 'T', 'body');
    await click(post(root));
    await wait(5);
    expect(title(root).getAttribute('aria-invalid')).toBe('true');
    expect(title(root).getAttribute('aria-describedby')).toBe('new-thread-failure');
    expect(area(root).hasAttribute('aria-invalid')).toBe(false);
  });

  // TKT-01M3YSQPZX: one optional Tags field, sent as the list typed.
  const tags = (root: HTMLElement) => root.querySelector<HTMLInputElement>('#new-thread-tags')!;

  it('sends the tags typed, split on spaces and commas, and none when empty', async () => {
    const s = serve('general');
    let root = await renderAt(general);
    expect(root.querySelector('label[for=new-thread-tags]')!.textContent).toBe('Tags (optional)');
    const hint = root.querySelector('#new-thread-tags-hint')!;
    expect(hint.textContent).toBe('Separate tags with spaces or commas');
    expect(tags(root).getAttribute('aria-describedby')).toBe('new-thread-tags-hint');
    await write(root, 'Rain barrels', 'Which one?');
    await fill(tags(root), ' Garden, long-read  x,,');
    await click(post(root));
    await wait(20);
    expect(sent(s, 'POST', '/api/v1/forums/general/threads').map((r) => r.body)).toEqual([
      { title: 'Rain barrels', source: 'Which one?', tags: ['Garden', 'long-read', 'x'] },
    ]);

    // The field is cleared with the rest once the thread is created.
    root = await renderAt(general);
    expect(tags(root).value).toBe('');
    await write(root, 'Another', 'text');
    await fill(tags(root), ' , ');
    await click(post(root));
    await wait(20);
    expect(sent(s, 'POST', '/api/v1/forums/general/threads')[1].body).toEqual({ title: 'Another', source: 'text' });
  });

  // Terva review 2047: tags alone are something to discard. They are not
  // in the forum draft, so discarding them deletes no draft.
  it('discards a form of tags alone, and deletes no draft', async () => {
    const s = serve('general');
    const root = await renderAt(general);
    await wait(5);
    const discard = () => root.querySelector<HTMLButtonElement>('.composer-foot .discard')!;
    expect(discard().disabled).toBe(true);
    await fill(tags(root), 'garden');
    expect(discard().disabled).toBe(false);
    await click(discard());
    await wait(5);
    expect(tags(root).value).toBe('');
    expect(discard().disabled).toBe(true);
    expect(sent(s, 'DELETE', '/api/v1/forums/general/draft')).toHaveLength(0);

    // With a title too, Discard deletes the draft and the tags go with it.
    vi.stubGlobal('confirm', () => true);
    await fill(title(root), 'T');
    await fill(tags(root), 'garden');
    await click(discard());
    await wait(5);
    expect([title(root).value, tags(root).value]).toEqual(['', '']);
    expect(sent(s, 'DELETE', '/api/v1/forums/general/draft')).toHaveLength(1);
  });

  it('names each refused tag under the field and marks it invalid', async () => {
    serve('general', {
      'POST /api/v1/forums/general/threads': () =>
        problemAnswer('validation-failed', 422, {
          errors: [
            { field: 'tags.0', code: 'reserved_tag' },
            { field: 'tags.2', code: 'invalid_tag' },
          ],
        }),
    });
    const root = await renderAt(general);
    await write(root, 'T', 'body');
    await fill(tags(root), 'agent garden a--b');
    await click(post(root));
    await wait(5);
    expect(alert(root)).toBe('Your thread was not posted: a tag was refused. The Tags field says which.');
    const errs = root.querySelector('#new-thread-tags-error')!;
    expect([...errs.querySelectorAll('li')].map((l) => l.textContent)).toEqual([
      '“agent” is reserved for moderators and agent accounts.',
      '“a--b” is not a tag: use letters, digits and single hyphens between them.',
    ]);
    expect(tags(root).getAttribute('aria-invalid')).toBe('true');
    expect(tags(root).getAttribute('aria-describedby')).toBe('new-thread-tags-hint new-thread-tags-error');
    // What was typed stays, and the title is not blamed.
    expect(tags(root).value).toBe('agent garden a--b');
    expect(title(root).hasAttribute('aria-invalid')).toBe(false);
    expect(post(root).disabled).toBe(false);
  });

  it.each([
    ['forbidden', 403],
    ['conflict', 409],
  ])('a %s draft save turns autosave off, says Draft not saved, and leaves Post on', async (type, status) => {
    const s = serve('general', { 'PUT /api/v1/forums/general/draft': () => problemAnswer(type, status) });
    const root = await renderAt(general);
    await wait(5);
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    await write(root, 'T', 'body');
    await act(async () => void (await vi.advanceTimersByTimeAsync(draftDelay)));
    expect(note(root)).toBe('Draft not saved');
    expect(post(root).disabled).toBe(false);
    await fill(area(root), 'body and more');
    await act(async () => void (await vi.advanceTimersByTimeAsync(draftDelay * 2)));
    expect(sent(s, 'PUT', '/api/v1/forums/general/draft')).toHaveLength(1);
    vi.useRealTimers();
    await click(post(root));
    await wait(20);
    expect(location.pathname).toBe(`/t/${thread.id}/${thread.slug}`);
  });
});

// Criterion 5: the archived notice is a warning, never a gate.
describe('an archived forum', () => {
  it('shows its notice before anything is typed, and leaves Post to the API, for a member', async () => {
    serve('archive-2024', { 'POST /api/v1/forums/archive-2024/threads': () => problemAnswer('conflict', 409) });
    const root = await renderAt(archived);
    await wait(5);
    expect(root.querySelector('[data-archived]')!.textContent).toBe('This forum is archived. Only admins can start threads here.');
    await write(root, 'T', 'body');
    expect(post(root).disabled).toBe(false);
    await click(post(root));
    await wait(5);
    expect(alert(root)).toBe('This forum is archived, so your thread was not posted.');
    expect(post(root).disabled).toBe(true);
  });

  it("lets an admin's draft saves and Post succeed", async () => {
    viewer.value = { ...(structuredClone(member) as Me), user: { ...(member as Me).user!, role: 'admin' } };
    const s = serve('archive-2024');
    const root = await renderAt(archived);
    await wait(5);
    expect(root.querySelector('[data-archived]')).not.toBeNull();
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    await write(root, 'Archived notes', 'For the record.');
    expect(post(root).disabled).toBe(false);
    await act(async () => void (await vi.advanceTimersByTimeAsync(draftDelay)));
    expect(note(root)).toBe(savedNote(root));
    vi.useRealTimers();
    await click(post(root));
    await wait(20);
    expect(sent(s, 'POST', '/api/v1/forums/archive-2024/threads')).toHaveLength(1);
    expect(sent(s, 'DELETE', '/api/v1/forums/archive-2024/draft')).toHaveLength(1);
    expect(location.pathname).toBe(`/t/${thread.id}/${thread.slug}`);
  });
});

// Criterion 8, from the screen's side: Shell.test.tsx holds how the shell
// draws what the screen asks for.
describe('on a phone', () => {
  it('is the composer column, with Post in the top bar and no tab bar', async () => {
    isPhone.value = true;
    serve('general');
    const root = await renderAt(general);
    expect(root.querySelector('.composer.in-sheet')).not.toBeNull();
    expect(root.querySelector('[data-shell=tabbar]')).toBeNull();
    expect(shellContext.value).toMatchObject({ title: 'New thread', composing: true, back: { href: '/f/general', label: 'General' } });
    const primary = root.querySelector<HTMLButtonElement>('[data-shell-primary=post-thread]')!;
    expect(primary.textContent).toBe('Post');
    expect(primary.disabled).toBe(true);
    await write(root, 'From the phone', 'Posted from the top bar.');
    expect(primary.disabled).toBe(false);
    await click(primary);
    await wait(20);
    expect(location.pathname).toBe(`/t/${thread.id}/${thread.slug}`);
  });

  // Terva review 1591: Discard is offered only once the draft has loaded
  // and there is something to discard, so it never deletes one unseen.
  it('offers Discard in the more menu only once the draft has loaded', async () => {
    isPhone.value = true;
    let answer!: (a: Answer) => void;
    const s = serve('general', {
      'GET /api/v1/forums/general/draft': () => new Promise<Answer>((r) => (answer = r)),
    });
    const root = await renderAt(general);
    const actions = () => shellContext.value?.actions.map((a) => a.id) ?? [];
    expect(actions()).toEqual([]);
    expect(root.querySelector('[aria-label=More]')).toBeNull();
    await act(async () => answer({ status: 200, body: { forum: 'general', title: 'Saved', source: 'unseen', updated_at: 0, revision: 3 } }));
    await wait(5);
    expect(title(root).value).toBe('Saved');
    expect(actions()).toEqual(['discard-thread']);
    vi.stubGlobal('confirm', () => true);
    await act(() => shellContext.value!.actions[0].run());
    await wait(5);
    expect(sent(s, 'DELETE', '/api/v1/forums/general/draft')).toHaveLength(1);
    expect(title(root).value).toBe('');
    expect(actions()).toEqual([]);
  });
});
