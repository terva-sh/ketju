import { act } from 'preact/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { PostHistory, PostRevision } from '../api/dto';
import golden from '../../../../internal/dto/testdata/golden/history.moderator.json';
import { renderAt, threadJSON, type Answer } from '../testing';

// The post history screen (TKT-01M29CP2Q3). It renders what the history
// endpoint serves, and these stub that endpoint with the golden's shape.

const id = '3yymw47g00001';
const slug = 'why-we-left-slack';
const at = '/t/' + id + '/' + slug + '/27/history';
const base = golden as PostHistory;
const [alice, bob] = [base.revisions[0].actor, base.revisions[1].actor];

function rev(revision: number, source: string, over: Partial<PostRevision> = {}): PostRevision {
  return { revision, actor: alice, at: 1757500000000 + revision * 60_000, reason: '', source, grace: false, ...over };
}

const history = (revisions: PostRevision[], over: Partial<PostHistory> = {}): PostHistory => ({
  ordinal: 27,
  deleted: false,
  redacted: false,
  revisions,
  ...over,
});

// serve answers the history endpoint with answer, and the thread page the
// route also asks for with the thread's header.
function serve(answer: Answer) {
  const urls: string[] = [];
  vi.stubGlobal('fetch', async (url: string) => {
    urls.push(url);
    if (url.endsWith('/history')) {
      return new Response(JSON.stringify(answer.body), {
        status: answer.status,
        headers: { 'Content-Type': answer.status >= 400 ? 'application/problem+json' : 'application/json' },
      });
    }
    return new Response(threadJSON({ id, slug, title: 'Why we left Slack' }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });
  });
  return urls;
}
const ok = (h: PostHistory): Answer => ({ status: 200, body: h });
const problem = (type: string, status: number): Answer => ({ status, body: { type, title: type, status } });

afterEach(() => vi.unstubAllGlobals());

const q = (root: HTMLElement, sel: string) => root.querySelector(sel);
const all = (root: HTMLElement, sel: string) => [...root.querySelectorAll(sel)];
const revisionsShown = (root: HTMLElement) => all(root, '.revision').map((r) => Number(r.getAttribute('data-revision')));
const pick = async (root: HTMLElement, label: string) => {
  const input = q(root, `input[aria-label="${label}"]`) as HTMLInputElement;
  expect(input).not.toBeNull();
  await act(async () => input.click());
};
const diff = (root: HTMLElement) =>
  [...(q(root, '.diff')?.childNodes ?? [])].map((n) => [n.nodeName.toLowerCase(), n.textContent]);

const three = [
  rev(1, 'We met on monday at the old place.'),
  rev(2, 'We met on tuesday at the old place.', { actor: bob, reason: 'wrong day' }),
  rev(3, 'We met on tuesday at the new place.', { reason: 'it moved' }),
];

describe('the post history screen', () => {
  it('asks the history endpoint, and lists every revision newest first with actor, time and reason', async () => {
    const urls = serve(ok(history(three)));
    const root = await renderAt(at);
    expect(urls).toContain(`/api/v1/threads/${id}/posts/27/history`);
    expect(q(root, '[data-screen]')?.getAttribute('data-screen')).toBe('post-history');
    expect(q(root, '.post-history h2')?.textContent).toBe('History of post #27');
    expect(revisionsShown(root)).toEqual([3, 2, 1]);

    const second = q(root, '.revision[data-revision="2"]')!;
    expect(second.querySelector('.author')?.textContent).toBe(bob.display_name);
    expect(second.querySelector('.role-badge')?.textContent).toBe('moderator');
    expect(second.querySelector('.revision-reason')?.textContent).toBe('Reason: wrong day');
    // The time is absolute, PostTime's, with the full moment in datetime.
    expect(second.querySelector('time')?.getAttribute('dateTime')).toBe(new Date(three[1].at).toISOString());
    expect(q(root, '.revision[data-revision="1"] .revision-reason')).toBeNull();
    expect(q(root, '.revision[data-revision="1"] .revision-number')?.textContent).toBe('Revision 1 (original)');
    expect(q(root, '.post-history a[href]')?.getAttribute('href')).toBe(`/t/${id}/${slug}/27`);
  });

  it('compares the newest with the one before it at first, word by word', async () => {
    serve(ok(history(three)));
    const root = await renderAt(at);
    expect(q(root, '.history-diff h3')?.textContent).toBe('Revision 2 to revision 3');
    expect(diff(root)).toEqual([
      ['#text', 'We met on tuesday at the '],
      ['del', 'old'],
      ['ins', 'new'],
      ['#text', ' place.'],
    ]);
  });

  it('compares any two revisions chosen', async () => {
    serve(ok(history(three)));
    const root = await renderAt(at);
    await pick(root, 'Compare from revision 1');
    expect(q(root, '.history-diff h3')?.textContent).toBe('Revision 1 to revision 3');
    expect(diff(root)).toEqual([
      ['#text', 'We met on '],
      ['del', 'monday'],
      ['ins', 'tuesday'],
      ['#text', ' at the '],
      ['del', 'old'],
      ['ins', 'new'],
      ['#text', ' place.'],
    ]);
    await pick(root, 'Compare to revision 2');
    expect(q(root, '.history-diff h3')?.textContent).toBe('Revision 1 to revision 2');
    expect(diff(root).filter(([n]) => n !== '#text')).toEqual([
      ['del', 'monday'],
      ['ins', 'tuesday'],
    ]);
    await pick(root, 'Compare from revision 2');
    expect(q(root, '.history-diff')?.textContent).toContain('Choose two different revisions to compare.');
    expect(q(root, '.diff')).toBeNull();
  });

  it('lists a grace edit with a grace note, and no other revision with one', async () => {
    serve(ok(history([rev(1, 'a typo'), rev(2, 'a typo, fixed', { grace: true }), rev(3, 'a later thought')])));
    const root = await renderAt(at);
    expect(revisionsShown(root)).toEqual([3, 2, 1]);
    expect(all(root, '[data-grace]').map((g) => g.closest('.revision')?.getAttribute('data-revision'))).toEqual(['2']);
  });

  it('says a post with one revision has not been edited, and offers no comparison', async () => {
    serve(ok(history([rev(1, 'only this')])));
    const root = await renderAt(at);
    expect(revisionsShown(root)).toEqual([1]);
    expect(q(root, 'input[type="radio"]')).toBeNull();
    expect(q(root, '.post-history')?.textContent).toContain('This post has not been edited.');
  });

  it.each([
    ['unauthenticated', 401, 'You need to sign in to see this.'],
    ['forbidden', 403, 'Your account does not have access to this.'],
  ])('renders %s through ErrorPanel', async (type, status, words) => {
    serve(problem(type, status));
    const root = await renderAt(at);
    const panel = q(root, '.post-history .error-panel')!;
    expect(panel.getAttribute('role')).toBe('alert');
    expect(panel.textContent).toContain("This post's history could not be loaded.");
    expect(panel.textContent).toContain(words);
    expect(panel.querySelector('[data-sign-in]') !== null).toBe(type === 'unauthenticated');
    expect(q(root, '.revisions')).toBeNull();
  });

  it("says a removed post's history is withheld when it is served with none", async () => {
    serve(ok(history([], { deleted: true })));
    const root = await renderAt(at);
    expect(q(root, '[data-history-withheld]')?.textContent).toBe('Post removed. Its history is not shown.');
    expect(q(root, '.revisions')).toBeNull();
    expect(q(root, '.history-diff')).toBeNull();
  });

  it('says a redacted post was removed permanently when it is served with no revisions', async () => {
    serve(ok(history([], { deleted: true, redacted: true })));
    const root = await renderAt(at);
    expect(q(root, '[data-history-withheld]')?.textContent).toBe('Post removed permanently');
    expect(q(root, '.revisions')).toBeNull();
  });

  it("lists a redacted post's revisions for a moderator under the notice, with no comparison", async () => {
    const blank = [rev(1, ''), rev(2, '', { actor: bob }), rev(3, '')];
    serve(ok(history(blank, { deleted: true, redacted: true })));
    const root = await renderAt(at);
    expect(q(root, '[data-history-redacted]')?.textContent).toBe(
      'Post removed permanently. Its revisions are listed without their content.',
    );
    expect(revisionsShown(root)).toEqual([3, 2, 1]);
    expect(q(root, 'input[type="radio"]')).toBeNull();
    expect(q(root, '.history-diff')).toBeNull();
  });

  it("gives a moderator a removed post's revisions to compare, and says the post is removed", async () => {
    serve(ok(history(three, { deleted: true })));
    const root = await renderAt(at);
    expect(q(root, '[data-history-removed]')).not.toBeNull();
    expect(revisionsShown(root)).toEqual([3, 2, 1]);
    await pick(root, 'Compare from revision 1');
    expect(diff(root).filter(([n]) => n !== '#text').length).toBe(4);
  });

  it('shows sources as text, never as markup', async () => {
    serve(ok(history([rev(1, 'plain'), rev(2, '<img src=x onerror=alert(1)> **bold**')])));
    const root = await renderAt(at);
    expect(q(root, '.diff img')).toBeNull();
    expect(q(root, '.diff ins')?.textContent).toBe('<img src=x onerror=alert(1)> **bold**');
  });
});
