import { useLayoutEffect, useRef } from 'preact/hooks';
import { useLocation, useRoute } from 'preact-iso';
import { apiGet } from '../../api/client';
import type { UserPost, UserPosts } from '../../api/dto';
import { isProblem } from '../../api/problem';
import { useResource } from '../../api/resource';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Attachments } from '../../components/thread/Attachments';
import { Edited, permalink } from '../../components/thread/Post';
import { PostTime } from '../../components/thread/PostTime';
import { isTombstone, removedMark, TombstoneText } from '../../components/thread/removal';
import { threadHref } from '../../components/ThreadRow';
import { profileHref } from '../../shell/nav';
import { useScrollMemory } from '../../state/scroll';
import { useShellContext } from '../../state/shell';
import { NotFound } from '../Placeholder';
import { displayName, useProfile } from './profile';

// postsUrl is the API address for one page of handle's posts (docs/07).
export function postsUrl(handle: string, cursor?: string): string {
  return `/api/v1/users/${encodeURIComponent(handle)}/posts${cursor ? '?cursor=' + encodeURIComponent(cursor) : ''}`;
}

// pageHref is this screen's own address for a page.
const pageHref = (handle: string, cursor?: string) =>
  `${profileHref(handle)}/posts${cursor ? '?cursor=' + encodeURIComponent(cursor) : ''}`;

// ProfilePostsScreen is /u/{handle}/posts (TKT-01M3V3CFMA): the member's
// posts, newest first, across the forums this viewer may read, each with its
// forum and thread and a link to its permalink. Paging is the server's
// cursor, carried in the address as the thread list carries its own, so a
// page of history can be linked and the browser comes back to it.
//
// Each post is drawn as its thread page draws it for this viewer, which is
// what the server sends: a post its author deleted reaches the author as a
// tombstone, and one a moderator hid reaches the author and moderators
// marked, with its body. Other members are sent neither. The list is a
// history, not a thread, so it offers no reply, quote, flag or expander.
//
// The profile is fetched beside the list for the heading, and so that a
// renamed handle's address is replaced here too (useProfile). Its failure is
// reported with its own retry, and the list's retry asks for it again too,
// so neither request is left failed with no way to try it again (Terva
// review 1800).
export function ProfilePostsScreen() {
  const { params } = useRoute();
  const { query } = useLocation();
  const handle = params.handle;
  const cursor = query.cursor || undefined;
  const profile = useProfile(handle, '/posts');
  const key = postsUrl(handle, cursor);
  const res = useResource(key, (signal) => apiGet<UserPosts>(key, { signal }));
  const list = res.data;
  const name = displayName(profile.data, handle);
  useShellContext({ title: name, back: { href: profileHref(handle), label: 'Profile' }, actions: [] }, [name, handle]);
  useScrollMemory(key, !!list);

  if (isProblem(res.error, 'not-found') || isProblem(profile.error, 'not-found')) return <NotFound />;

  return (
    <main class="account" data-screen="profile-posts">
      <header class="account-head">
        <a class="account-up" href={profileHref(handle)}>
          Profile
        </a>
        <h1>Posts by {name}</h1>
      </header>
      {res.error && !list && (
        <ErrorPanel
          error={res.error}
          what="These posts"
          onRetry={() => {
            res.retry();
            if (profile.error) profile.retry();
          }}
        />
      )}
      {profile.error && !profile.data && !(res.error && !list) && (
        <ErrorPanel error={profile.error} what="This member's profile" onRetry={profile.retry} />
      )}
      {list && list.posts.length === 0 && (
        <p class="account-empty" data-empty-list>
          {cursor ? 'No more posts.' : 'No posts you can see.'}
        </p>
      )}
      {list && list.posts.length > 0 && (
        <ol class="history-posts">
          {list.posts.map((p) => (
            <HistoryPost key={`${p.thread.id}/${p.post.ordinal}`} item={p} />
          ))}
        </ol>
      )}
      {list && (cursor || list.next_cursor) && (
        <nav class="account-pages" aria-label="Pages">
          {cursor && <a href={pageHref(handle)}>Newest</a>}
          {list.next_cursor && (
            <a href={pageHref(handle, list.next_cursor)} rel="next" data-older>
              Older posts
            </a>
          )}
        </nav>
      )}
    </main>
  );
}

// HistoryPost is one post in a history: where it is, then the post as its
// thread page draws it, read-only.
//
// A quote's source link is written by the server as #post-N, an anchor in
// the quoting post's own thread (internal/render quote.go), which the thread
// page resolves. A history mixes threads and has no such anchors, so each
// one is pointed at the quoted post's permalink in this post's thread
// (Terva review 1800).
function HistoryPost({ item }: { item: UserPost }) {
  const { thread, forum, post } = item;
  const link = permalink(thread, post.ordinal);
  const body = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    for (const a of body.current?.querySelectorAll<HTMLAnchorElement>('a.quote-source') ?? []) {
      const o = /^#post-([1-9][0-9]*)$/.exec(a.getAttribute('href') ?? '')?.[1];
      if (o) a.setAttribute('href', permalink(thread, Number(o)));
    }
  }, [post.html, thread.id, thread.slug]);
  return (
    <li class="history-post" data-history-post={link}>
      <p class="history-where">
        <a href={`/f/${encodeURIComponent(forum.slug)}`} data-forum-link>
          {forum.name}
        </a>
        <span aria-hidden="true"> › </span>
        <a href={threadHref(thread)} data-thread-link>
          {thread.title}
        </a>
      </p>
      <article class={'post' + (post.deleted ? ' removed' : '')}>
        <div class="post-main">
          <header class="post-header">
            <a class="ordinal hit" href={link} aria-label={`#${post.ordinal} in ${thread.title}`}>
              #{post.ordinal}
            </a>
            <PostTime at={post.created_at} />
            <Edited post={post} />
            {post.deleted && !isTombstone(post) && <span class="deleted-mark">{removedMark(post)}</span>}
          </header>
          {isTombstone(post) ? (
            <p class="history-tombstone">
              <TombstoneText post={post} />
            </p>
          ) : (
            <>
              <div class="post-body" ref={body} dangerouslySetInnerHTML={{ __html: post.html }} />
              <Attachments post={post} />
            </>
          )}
        </div>
      </article>
    </li>
  );
}
