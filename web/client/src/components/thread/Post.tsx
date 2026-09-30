import { useLayoutEffect, useRef, useState } from 'preact/hooks';
import type { Post as PostDTO } from '../../api/dto';
import { bodyText, selectionIn } from '../../composer/quote';
import { isPhone } from '../../state/layout';
import { viewer } from '../../state/viewer';
import { Avatar } from './Avatar';
import { isOwn } from './postControls';
import { DeleteConfirm, PostEditor, Restore, useOwn, type Own } from './OwnControls';
import { PostMore } from './PostMore';
import { PostTime } from './PostTime';
import { removedMark } from './removal';
import { Replies, ReplyToggle } from './ReplyExpander';
import { RoleBadge } from './RoleBadge';

export interface ThreadRef {
  id: string;
  slug: string;
}

// permalink is a post's own address, which resolves to the page holding it
// (docs/07 "URL scheme").
export const permalink = (t: ThreadRef, ordinal: number) =>
  `/t/${encodeURIComponent(t.id)}/${encodeURIComponent(t.slug)}/${ordinal}`;

// Post is one post, laid out as docs/06 "Post anatomy" and the mockups draw
// it. The body is the server's rendering, sanitized by internal/render; it is
// the one place the client inserts HTML.
//
// A removed post reaches here only when the server sent its body: to a
// moderator, or to its author when a moderator removed it (docs/07 "Removed
// posts"). It is drawn on a distinct background, class removed, with a label
// naming who removed it (docs/08 "Tombstones").
//
// The footer carries only what works today. Quote and reply are there when
// the page offers them, which is when the reader may reply and a composer is
// on the page to take them (TKT-01M29CEE79). Flag is there when the page
// offers it, to a signed-in member, and the post may be flagged (mayFlag);
// it does not depend on replying, since a locked thread still takes flags
// (TKT-01M3QQWA5Z). The reply expander leads the footer when the page
// offers it (expanderShown). On a phone link, flag and history collapse
// under PostMore (docs/06 "Mobile").
//
// own is handed over on the viewer's own post, on the thread page only.
// Edit and delete follow flag, under PostMore on a phone, while the page
// offers them (postControls); edit opens the editor in the body's place, and
// delete asks first, below the body. Restore is in the footer of a removed
// post drawn with its body, which is how a moderator's own deleted post
// reaches them (TKT-01M3PTT97X).
export function Post({
  post,
  thread,
  unread,
  actions,
  onFlag,
  own,
  expander = false,
}: {
  post: PostDTO;
  thread: ThreadRef;
  unread: boolean;
  actions?: PostActions;
  onFlag?: FlagHandler;
  own?: Own;
  expander?: boolean;
}) {
  const flag = onFlag && mayFlag(post) ? onFlag : undefined;
  const o = useOwn(own, post.ordinal);
  const idle = o.mode === null;
  const edit = idle && o.offer.edit ? () => o.open('edit') : undefined;
  const del = idle && o.offer.delete ? () => o.open('delete') : undefined;
  // What was selected in this post when the pointer went down on quote:
  // a click can clear a selection before its handler runs.
  const selected = useRef<string | null>(null);
  const [open, setOpen] = useState(false);
  const toggle = useHeldStill(() => setOpen((o) => !o));
  return (
    <article
      class={'post' + (unread ? ' unread' : '') + (post.deleted ? ' removed' : '')}
      id={`post-${post.ordinal}`}
      data-ordinal={post.ordinal}
      tabIndex={-1}
    >
      <Avatar author={post.author} />
      <div class="post-main">
        <header class="post-header">
          <a class="ordinal hit" href={permalink(thread, post.ordinal)}>
            #{post.ordinal}
          </a>
          <span class="author">{post.author.display_name || post.author.handle}</span>
          <RoleBadge author={post.author} />
          <PostTime at={post.created_at} />
          {post.edited_at !== null && <span class="edited">(edited)</span>}
          {post.deleted && <span class="deleted-mark">{removedMark(post)}</span>}
          <ReplyTo post={post} />
        </header>
        {o.mode === 'edit' ? (
          <PostEditor o={o} thread={thread.id} ordinal={post.ordinal} replyTo={post.reply_to} />
        ) : (
          <div class="post-body" dangerouslySetInnerHTML={{ __html: post.html }} />
        )}
        <Attachments post={post} />
        {o.mode === 'delete' && <DeleteConfirm o={o} ordinal={post.ordinal} />}
        <footer class="post-footer">
          {expander && <ReplyToggle post={post} open={open} onToggle={toggle} />}
          {actions && o.mode !== 'edit' && (
            <>
              <button
                type="button"
                class="hit"
                data-post-action="quote"
                onMouseDown={(e) => {
                  selected.current = selectionIn(bodyOf(e.currentTarget));
                }}
                onClick={(e) => {
                  const body = bodyOf(e.currentTarget);
                  actions.quote(post.ordinal, selected.current ?? selectionIn(body) ?? bodyText(body));
                  selected.current = null;
                }}
              >
                quote
              </button>
              <button type="button" class="hit" data-post-action="reply" onClick={() => actions.reply(post.ordinal)}>
                reply
              </button>
            </>
          )}
          {isPhone.value ? (
            <PostMore thread={thread} ordinal={post.ordinal} onFlag={flag} onEdit={edit} onDelete={del} />
          ) : (
            <>
              <a class="hit" href={permalink(thread, post.ordinal)}>
                link
              </a>
              {flag && (
                <button
                  type="button"
                  class="hit"
                  data-post-action="flag"
                  onClick={(e) => flag(post.ordinal, e.currentTarget)}
                >
                  flag
                </button>
              )}
              {edit && (
                <button type="button" class="hit" data-post-action="edit" onClick={edit}>
                  edit
                </button>
              )}
              {del && (
                <button type="button" class="hit" data-post-action="delete" onClick={del}>
                  delete
                </button>
              )}
              <a class="hit" href={`${permalink(thread, post.ordinal)}/history`}>
                history
              </a>
            </>
          )}
          <Restore o={o} />
        </footer>
        {expander && open && <Replies thread={thread} ordinal={post.ordinal} />}
      </div>
    </article>
  );
}

// PostActions are what a post's quote and reply do: hand the composer the
// quoted text, or the post to answer.
export interface PostActions {
  quote: (ordinal: number, text: string) => void;
  reply: (ordinal: number) => void;
}

// FlagHandler opens the flag dialog for a post. returnTo is the control
// focus goes back to when the dialog closes: the flag button on desktop, and
// on a phone the post's More button, since the flag item inside it is gone
// by then.
export type FlagHandler = (ordinal: number, returnTo: HTMLElement) => void;

// mayFlag is whether the viewer is offered flag on post: a signed-in member,
// on a post that is shown (not removed, not moved) and is not their own
// (maintainer decision 2026-09-29; the server refuses a self-flag too).
// Ownership is isOwn's, which the edit and delete offers use too.
export function mayFlag(post: PostDTO): boolean {
  return !!viewer.value.user && !post.deleted && post.moved_to === null && !isOwn(post);
}

// useHeldStill wraps change, a toggle's handler, so the toggle keeps its
// place on screen across the render the change causes. The replies open and
// close below the toggle, so the post and everything above it stay where
// they are however the browser anchors its scroll (docs/06: nothing about
// the canonical page moves); only what is below moves, by the height of the
// replies, and comes back when they close.
export function useHeldStill(change: () => void): (e: Event) => void {
  const held = useRef<{ el: Element; top: number } | null>(null);
  useLayoutEffect(() => {
    const h = held.current;
    if (!h) return;
    held.current = null;
    const moved = h.el.getBoundingClientRect().top - h.top;
    if (moved) scrollBy(0, moved);
  });
  return (e: Event) => {
    const el = e.currentTarget as Element;
    held.current = { el, top: el.getBoundingClientRect().top };
    change();
  };
}

// bodyOf is the body of the post a footer control is in.
const bodyOf = (el: Element) => el.closest('.post')!.querySelector('.post-body')!;

// ReplyTo is the "↩ #N" indicator. It is absent for a post that answers
// nothing, and for one whose parent is the post directly above: the server
// decides that (reply_to_suppressed), so the client never repeats the rule.
// What a click does is PostList's: scroll, or a popover.
export function ReplyTo({ post }: { post: PostDTO }) {
  if (post.reply_to === null || post.reply_to_suppressed) return null;
  return (
    <a class="reply-to hit" href={`#post-${post.reply_to}`} data-reply-to={post.reply_to}>
      ↩ #{post.reply_to}
    </a>
  );
}

// Attachments lists a post's files, each a link to its blob. The reply
// expander's copies show them too.
export function Attachments({ post }: { post: PostDTO }) {
  if (post.attachments.length === 0) return null;
  return (
    <ul class="attachments">
      {post.attachments.map((a) => (
        <li key={a.hash}>
          <a href={`/blobs/${encodeURIComponent(a.hash)}`} download={a.original_name}>
            {a.original_name}
          </a>{' '}
          <span class="size">{formatSize(a.size)}</span>
        </li>
      ))}
    </ul>
  );
}

export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
