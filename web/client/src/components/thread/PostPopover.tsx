import { useEffect, useRef } from 'preact/hooks';
import { apiGet } from '../../api/client';
import type { Post as PostDTO } from '../../api/dto';
import { useResource } from '../../api/resource';
import { ErrorPanel } from '../ErrorPanel';
import { Avatar } from './Avatar';
import { permalink, type ThreadRef } from './Post';
import { PostTime } from './PostTime';
import { RoleBadge } from './RoleBadge';

// PostPopover shows a post that is not on this page, for a reply-to or quote
// link that points off it (docs/06: "otherwise shows it in a popover with a
// 'go to' link"). It asks the single-post endpoint, which answers with the
// same assembly the page uses.
export function PostPopover({ thread, ordinal, onClose }: { thread: ThreadRef; ordinal: number; onClose: () => void }) {
  const url = `/api/v1/threads/${encodeURIComponent(thread.id)}/posts/${ordinal}`;
  const res = useResource(url, (signal) => apiGet<PostDTO>(url, { signal }));
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    box.current?.focus();
    const key = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    addEventListener('keydown', key);
    return () => removeEventListener('keydown', key);
  }, []);

  const post = res.data;
  return (
    <div class="post-popover" role="dialog" aria-label={`Post #${ordinal}`} tabIndex={-1} ref={box}>
      <div class="popover-bar">
        <a class="go-to" href={permalink(thread, ordinal)}>
          Go to #{ordinal}
        </a>
        <button type="button" class="close" onClick={onClose} aria-label="Close">
          ×
        </button>
      </div>
      {res.error && <ErrorPanel error={res.error} what={`Post #${ordinal}`} onRetry={res.retry} />}
      {!post && !res.error && <p class="loading">Loading #{ordinal}…</p>}
      {post &&
        (post.deleted && !post.html ? (
          <p class="tombstone-text">#{ordinal}: Post removed.</p>
        ) : (
          <article class="post popover-post" data-ordinal={ordinal}>
            <Avatar author={post.author} />
            <div class="post-main">
              <header class="post-header">
                <span class="ordinal">#{post.ordinal}</span>
                <span class="author">{post.author.display_name || post.author.handle}</span>
                <RoleBadge author={post.author} />
                <PostTime at={post.created_at} />
              </header>
              <div class="post-body" dangerouslySetInnerHTML={{ __html: post.html }} />
            </div>
          </article>
        ))}
    </div>
  );
}
