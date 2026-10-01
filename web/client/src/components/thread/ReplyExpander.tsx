import { apiGet } from '../../api/client';
import type { Post as PostDTO, PostReplies } from '../../api/dto';
import { useResource } from '../../api/resource';
import { ErrorPanel } from '../ErrorPanel';
import { Avatar } from './Avatar';
import { Attachments } from './Attachments';
import { AuthorLink, Edited, permalink, type ThreadRef } from './Post';
import { PostTime } from './PostTime';
import { isTombstone, removedMark, TombstoneText } from './removal';
import { RoleBadge } from './RoleBadge';

// The reply expander (docs/06 "Post anatomy"): "3 replies ▾" opens a post's
// direct replies inline, read-only and recessed, and closes them again. It
// gives the reply-to-reply relationship without indenting the thread.

// expanderShown is whether post offers the expander, given the post after it
// on this page, if there is one. It has replies, and it is not the case that
// its only reply is the post directly below: that one is already in view,
// and its own reply-to indicator is suppressed by the same rule (docs/06).
// A sole reply that opens the next page is not in view, so the expander
// stays at a page's foot.
export function expanderShown(post: Pick<PostDTO, 'ordinal' | 'reply_count'>, next?: Pick<PostDTO, 'ordinal' | 'reply_to'>) {
  if (post.reply_count < 1) return false;
  const below = next !== undefined && next.ordinal === post.ordinal + 1 && next.reply_to === post.ordinal;
  return !(post.reply_count === 1 && below);
}

export const repliesId = (ordinal: number) => `replies-${ordinal}`;

// ReplyToggle opens and closes the replies of the post it is in.
export function ReplyToggle({ post, open, onToggle }: { post: PostDTO; open: boolean; onToggle: (e: Event) => void }) {
  const n = post.reply_count;
  return (
    <button
      type="button"
      class="reply-expander hit"
      aria-expanded={open}
      aria-controls={open ? repliesId(post.ordinal) : undefined}
      data-replies={post.ordinal}
      onClick={onToggle}
    >
      {n} {n === 1 ? 'reply' : 'replies'}
      <svg class="chevron" width="12" height="12" viewBox="0 0 16 16" aria-hidden="true">
        <path d={open ? 'M4 10l4-4 4 4' : 'M4 6l4 4 4-4'} />
      </svg>
    </button>
  );
}

// Replies is the open expansion: the post's direct replies, in the order the
// server gives, which is ordinal order, wherever in the thread they are. The
// posts are copies, so they carry no id or data-ordinal: every lookup of a
// canonical post by its ordinal still finds only the canonical one.
export function Replies({ thread, ordinal }: { thread: ThreadRef; ordinal: number }) {
  const url = `/api/v1/threads/${encodeURIComponent(thread.id)}/posts/${ordinal}/replies`;
  const res = useResource(url, (signal) => apiGet<PostReplies>(url, { signal }));
  const posts = res.data?.posts;
  return (
    <section class="replies" id={repliesId(ordinal)} aria-label={`Replies to #${ordinal}`}>
      {res.error && <ErrorPanel error={res.error} what={`Replies to #${ordinal}`} onRetry={res.retry} />}
      {!posts && !res.error && <p class="loading">Loading replies…</p>}
      {posts?.map((p) =>
        isTombstone(p) ? (
          <div class="post post--recessed tombstone" key={p.ordinal} data-reply={p.ordinal}>
            <a class="ordinal hit" href={permalink(thread, p.ordinal)}>
              #{p.ordinal}
            </a>
            <TombstoneText post={p} />
          </div>
        ) : (
          <article class={'post post--recessed' + (p.deleted ? ' removed' : '')} key={p.ordinal} data-reply={p.ordinal}>
            <Avatar author={p.author} />
            <div class="post-main">
              <header class="post-header">
                <a class="ordinal hit" href={permalink(thread, p.ordinal)}>
                  #{p.ordinal}
                </a>
                <AuthorLink author={p.author} />
                <RoleBadge author={p.author} />
                <PostTime at={p.created_at} />
                <Edited post={p} />
                {p.deleted && <span class="deleted-mark">{removedMark(p)}</span>}
              </header>
              <div class="post-body" dangerouslySetInnerHTML={{ __html: p.html }} />
              <Attachments post={p} />
            </div>
          </article>
        ),
      )}
    </section>
  );
}
