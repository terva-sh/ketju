import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me, Profile, UserPosts } from '../../api/dto';
import { viewer } from '../../state/viewer';
import { problemAnswer, renderAt, route, screenOf, settle, stubServer, unmount, type Answer } from '../../testing';
import memberMe from '../../../../../internal/dto/testdata/golden/me.member.json';
import profile from '../../../../../internal/dto/testdata/golden/profile.member.json';
import memberPosts from '../../../../../internal/dto/testdata/golden/user-posts.member.json';
import ownPosts from '../../../../../internal/dto/testdata/golden/user-posts-own.member.json';

// A member's posts (TKT-01M3V3CFMA), as GET /users/{handle}/posts answers
// them: newest first, a page at a time by the server's cursor, each linked
// to its permalink, its thread and its forum. What a removed post shows is
// the server's to send; the screen draws what arrives. The viewer is the
// member golden's, alice, and the goldens are alice's posts.

let saved: Me;
beforeEach(() => {
  saved = viewer.value;
  viewer.value = structuredClone(memberMe) as Me;
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const ok = (body: unknown): Answer => ({ status: 200, body });
const cursor = (memberPosts as UserPosts).next_cursor;

function serve(posts: Record<string, unknown>, who: unknown = profile) {
  const routes: Record<string, () => Answer> = { 'GET /api/v1/users/alice': () => ok(who) };
  for (const [url, body] of Object.entries(posts)) routes[`GET ${url}`] = () => ok(body);
  return stubServer(routes);
}

const items = (root: HTMLElement) => [...root.querySelectorAll<HTMLElement>('.history-post')];

describe('the posts list', () => {
  it('posts list links each post to its permalink and its forum', async () => {
    serve({ '/api/v1/users/alice/posts': memberPosts });
    const root = await renderAt('/u/alice/posts');
    expect(screenOf(root)).toBe('profile-posts');
    expect(root.querySelector('h1')?.textContent).toBe('Posts by Alice Nakamura');
    const [first, second] = items(root);
    expect(items(root)).toHaveLength(2);
    // Newest first, in the server's order.
    expect(first.querySelector('.ordinal')?.getAttribute('href')).toBe('/t/3yymw47g00001/why-we-left-slack/62');
    expect(first.querySelector('.ordinal')?.textContent).toBe('#62');
    expect(first.querySelector('[data-forum-link]')?.getAttribute('href')).toBe('/f/general');
    expect(first.querySelector('[data-forum-link]')?.textContent).toBe('General');
    expect(first.querySelector('[data-thread-link]')?.getAttribute('href')).toBe('/t/3yymw47g00001/why-we-left-slack?page=1');
    expect(first.querySelector('[data-thread-link]')?.textContent).toBe('Why we left Slack');
    expect(first.querySelector('.post-body')?.innerHTML).toBe('<p>Summarising the thread so far.</p>');
    expect(first.querySelector('time')).not.toBeNull();
    expect(second.querySelector('.ordinal')?.getAttribute('href')).toBe('/t/3yymw47g00003/introductions/4');
    // A history, not a thread page: nothing to reply, quote, flag or expand.
    expect(root.querySelector('[data-post-action], .reply-expander, [data-post-more], .reply-to')).toBeNull();
    // No element ids, which two threads' ordinals would repeat.
    expect(root.querySelector('.history-posts [id]')).toBeNull();
  });

  it('Older posts carries next_cursor in the address', async () => {
    const last: UserPosts = { posts: (ownPosts as UserPosts).posts.slice(2), next_cursor: '' };
    const s = serve({
      '/api/v1/users/alice/posts': memberPosts,
      [`/api/v1/users/alice/posts?cursor=${encodeURIComponent(cursor)}`]: last,
    });
    const root = await renderAt('/u/alice/posts');
    const older = root.querySelector<HTMLAnchorElement>('[data-older]')!;
    expect(older.textContent).toBe('Older posts');
    expect(older.getAttribute('rel')).toBe('next');
    expect(older.getAttribute('href')).toBe(`/u/alice/posts?cursor=${encodeURIComponent(cursor)}`);
    // No Newest link on the newest page.
    expect(root.querySelector('.account-pages')?.textContent).toBe('Older posts');

    await route(older.getAttribute('href')!);
    await settle(root);
    await vi.waitFor(() => expect(items(root)).toHaveLength(1));
    expect(s.sent.map((r) => r.url)).toContain(`/api/v1/users/alice/posts?cursor=${encodeURIComponent(cursor)}`);
    // The end: no Older link, and Newest goes back to the first page.
    expect(root.querySelector('[data-older]')).toBeNull();
    const newest = [...root.querySelectorAll<HTMLAnchorElement>('.account-pages a')];
    expect(newest.map((a) => [a.textContent, a.getAttribute('href')])).toEqual([['Newest', '/u/alice/posts']]);
  });

  it('says when the viewer can see none of the posts', async () => {
    serve({ '/api/v1/users/alice/posts': { posts: [], next_cursor: '' } });
    const root = await renderAt('/u/alice/posts');
    expect(root.querySelector('[data-empty-list]')?.textContent).toBe('No posts you can see.');
    expect(root.querySelector('.account-pages')).toBeNull();
  });
});

// A quote's source link is #post-N in the quoting post's thread
// (internal/render quote.go); a history mixes threads, so it is pointed at
// the quoted post's permalink there (Terva review 1800).
it("points a quote's source at the quoted post in its own thread", async () => {
  const page = structuredClone(memberPosts) as UserPosts;
  page.posts[0].post.html =
    '<details class="quote" open><summary>Quote from #7</summary><blockquote><p>q</p></blockquote>' +
    '<p class="quote-attribution"><a href="#post-7" class="quote-source">Post #7</a></p></details><p>reply</p>';
  serve({ '/api/v1/users/alice/posts': page });
  const root = await renderAt('/u/alice/posts');
  const source = items(root)[0].querySelector<HTMLAnchorElement>('a.quote-source')!;
  expect(source.getAttribute('href')).toBe('/t/3yymw47g00001/why-we-left-slack/7');
  expect(root.querySelector('.history-posts a[href^="#"]')).toBeNull();
});

// The profile is a request of its own beside the list: its failure is shown
// with a retry, and the list's retry asks for it again (Terva review 1800).
describe('a failed profile request', () => {
  it('is reported with its own retry, which recovers it', async () => {
    let fail = true;
    stubServer({
      'GET /api/v1/users/alice': () => (fail ? problemAnswer('unavailable', 503) : ok(profile)),
      'GET /api/v1/users/alice/posts': () => ok(memberPosts),
    });
    const root = await renderAt('/u/alice/posts');
    expect(items(root)).toHaveLength(2);
    const panel = root.querySelector<HTMLElement>('[role=alert]')!;
    expect(panel.textContent).toContain("This member's profile could not be loaded.");
    fail = false;
    await act(async () => panel.querySelector('button')!.click());
    await vi.waitFor(() => expect(root.querySelector('h1')?.textContent).toBe('Posts by Alice Nakamura'));
    expect(root.querySelector('[role=alert]')).toBeNull();
  });

  it('is asked for again by the list’s retry when both failed', async () => {
    let fail = true;
    const s = stubServer({
      'GET /api/v1/users/alice': () => (fail ? problemAnswer('unavailable', 503) : ok(profile)),
      'GET /api/v1/users/alice/posts': () => (fail ? problemAnswer('unavailable', 503) : ok(memberPosts)),
    });
    const root = await renderAt('/u/alice/posts');
    const panels = root.querySelectorAll<HTMLElement>('[role=alert]');
    expect(panels).toHaveLength(1);
    expect(panels[0].textContent).toContain('These posts could not be loaded.');
    fail = false;
    await act(async () => panels[0].querySelector('button')!.click());
    await vi.waitFor(() => expect(items(root)).toHaveLength(2));
    await vi.waitFor(() => expect(root.querySelector('h1')?.textContent).toBe('Posts by Alice Nakamura'));
    expect(s.sent.filter((r) => r.url === '/api/v1/users/alice')).toHaveLength(2);
  });
});

// Rendering what the API sends (docs/07, docs/08 "Tombstones").
describe('removed posts', () => {
  it("an author's own deleted post is marked without a body", async () => {
    serve({ '/api/v1/users/alice/posts': ownPosts });
    const root = await renderAt('/u/alice/posts');
    const deleted = items(root)[1];
    expect(deleted.querySelector('.ordinal')?.textContent).toBe('#9');
    expect(deleted.querySelector('.history-tombstone')?.textContent).toBe('Post removed by author');
    expect(deleted.querySelector('.post-body')).toBeNull();
  });

  it('a moderator-hidden post is marked with its body', async () => {
    const page = structuredClone(ownPosts) as UserPosts;
    Object.assign(page.posts[1].post, { removal: 'moderator', html: '<p>Hidden, still here.</p>' });
    serve({ '/api/v1/users/alice/posts': page });
    const root = await renderAt('/u/alice/posts');
    const hidden = items(root)[1];
    expect(hidden.querySelector('.post')?.classList.contains('removed')).toBe(true);
    expect(hidden.querySelector('.deleted-mark')?.textContent).toBe('removed by a moderator');
    expect(hidden.querySelector('.post-body')?.innerHTML).toBe('<p>Hidden, still here.</p>');
    expect(hidden.querySelector('.history-tombstone')).toBeNull();
  });
});

describe('a history the viewer may not see', () => {
  it('not-found shows the not-found text', async () => {
    stubServer({
      'GET /api/v1/users/pending-one': () => problemAnswer('not-found', 404),
      'GET /api/v1/users/pending-one/posts': () => problemAnswer('not-found', 404),
    });
    const root = await renderAt('/u/pending-one/posts');
    expect(screenOf(root)).toBe('not-found');
  });

  it('unauthenticated offers sign in', async () => {
    stubServer({
      'GET /api/v1/users/alice': () => problemAnswer('unauthenticated', 401),
      'GET /api/v1/users/alice/posts': () => problemAnswer('unauthenticated', 401),
    });
    const root = await renderAt('/u/alice/posts');
    expect(screenOf(root)).toBe('profile-posts');
    expect(root.querySelector('[role=alert] [data-sign-in]')).not.toBeNull();
  });
});

it("replaces a renamed handle's address, keeping /posts and the query", async () => {
  const renamed = profile as Profile;
  const q = `?cursor=${encodeURIComponent(cursor)}`;
  stubServer({
    'GET /api/v1/users/alice-old': () => ok(renamed),
    [`GET /api/v1/users/alice-old/posts${q}`]: () => ok(memberPosts),
    'GET /api/v1/users/alice': () => ok(renamed),
    [`GET /api/v1/users/alice/posts${q}`]: () => ok(memberPosts),
  });
  const push = vi.spyOn(history, 'pushState');
  const root = await renderAt(`/u/alice-old/posts${q}`);
  await vi.waitFor(() => expect(location.pathname + location.search).toBe(`/u/alice/posts${q}`));
  expect(push).not.toHaveBeenCalled();
  await vi.waitFor(() => expect(items(root)).toHaveLength(2));
});
