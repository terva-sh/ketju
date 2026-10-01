import { useLayoutEffect, useRef } from 'preact/hooks';
import { apiGet } from '../../api/client';
import type { Post as PostDTO } from '../../api/dto';
import { useResource } from '../../api/resource';
import { ErrorPanel } from '../ErrorPanel';
import { isControl } from '../focus';
import { Avatar } from './Avatar';
import { AuthorLink, permalink, type ThreadRef } from './Post';
import { PostTime } from './PostTime';
import { isTombstone, removedMark, TombstoneText } from './removal';
import { RoleBadge } from './RoleBadge';

// PostPopover shows a post that is not on this page, for a reply-to or quote
// link that points off it (docs/06: "otherwise shows it in a popover with a
// 'go to' link"). It asks the single-post endpoint, which answers with the
// same assembly the page uses.
export function PostPopover({ thread, ordinal, onClose }: { thread: ThreadRef; ordinal: number; onClose: () => void }) {
  const url = `/api/v1/threads/${encodeURIComponent(thread.id)}/posts/${ordinal}`;
  const res = useResource(url, (signal) => apiGet<PostDTO>(url, { signal }));
  const box = useRef<HTMLDivElement>(null);

  // Focus goes into the popover, and back on close to what opened it, the
  // reply-to or quote link, when focus was still inside (TKT-01M29CGBXK).
  // Safari does not focus a link on click, so that may be the body, and the
  // page's focus guard then takes it (components/focus.ts).
  //
  // All of it happens in a layout effect, in the same task as the press that
  // opened the popover, so a key pressed straight away reaches it rather
  // than the link (TKT-01M3TV4HBF). Escape is heard on the document, and one
  // a dialog has already taken is left alone.
  useLayoutEffect(() => {
    const opener = document.activeElement;
    box.current?.focus();
    const key = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || e.defaultPrevented) return;
      e.preventDefault();
      onClose();
    };
    document.addEventListener('keydown', key);
    const root = box.current;
    return () => {
      document.removeEventListener('keydown', key);
      const inside = !document.activeElement || document.activeElement === document.body || root?.contains(document.activeElement);
      if (inside && isControl(opener)) opener.focus({ preventScroll: true });
    };
  }, []);

  const post = res.data;
  return (
    <div class="post-popover" role="dialog" aria-label={`Post #${ordinal}`} tabIndex={-1} ref={box}>
      <div class="popover-bar">
        <a class="go-to hit" href={permalink(thread, ordinal)}>
          Go to #{ordinal}
        </a>
        <button type="button" class="close" onClick={onClose} aria-label="Close">
          ×
        </button>
      </div>
      {res.error && <ErrorPanel error={res.error} what={`Post #${ordinal}`} onRetry={res.retry} />}
      {!post && !res.error && <p class="loading">Loading #{ordinal}…</p>}
      {post &&
        (isTombstone(post) ? (
          <p>
            #{ordinal}: <TombstoneText post={post} />
          </p>
        ) : (
          <article class={'post popover-post' + (post.deleted ? ' removed' : '')} data-ordinal={ordinal}>
            <Avatar author={post.author} />
            <div class="post-main">
              <header class="post-header">
                <span class="ordinal">#{post.ordinal}</span>
                <AuthorLink author={post.author} />
                <RoleBadge author={post.author} />
                <PostTime at={post.created_at} />
                {post.deleted && <span class="deleted-mark">{removedMark(post)}</span>}
              </header>
              <div class="post-body" dangerouslySetInnerHTML={{ __html: post.html }} />
            </div>
          </article>
        ))}
    </div>
  );
}
