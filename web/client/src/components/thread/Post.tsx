import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import type { Post as PostDTO } from '../../api/dto';
import { problemMessage } from '../../api/problem';
import { bodyText, selectionIn } from '../../composer/quote';
import { profileHref } from '../../shell/nav';
import { isPhone } from '../../state/layout';
import { viewer } from '../../state/viewer';
import { Attachments } from './Attachments';
import { Avatar } from './Avatar';
import { lensHref } from './lens';
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
// (TKT-01M3QQWA5Z). Unread from here is there when the page offers it, to a
// signed-in member, on its own rule for the same reason (TKT-01M3KFFFJR). The
// reply expander leads the footer when the page offers it (expanderShown). On
// a phone link, flag, edit, delete, unread from here and history collapse
// under PostMore (docs/06 "Mobile").
//
// own is handed over on the viewer's own post, and on every post to a
// viewer who moderates the forum, on the thread page only. Edit and delete
// follow flag, under PostMore on a phone, while the page offers them
// (postControls); edit opens the editor in the body's place, and delete asks
// first, below the body. Restore is in the footer of a removed post drawn
// with its body, which is how a removed post reaches a moderator
// (TKT-01M3PTT97X, TKT-01M3QHKD21). Unread from here follows them, and is
// not offered while the editor or the confirmation is open, since it leaves
// the page and would drop what is being edited.
//
// Conversation, the post's reply chain lens, follows link on a post that is
// a reply or has replies, under PostMore on a phone (TKT-01M29CTY71).
//
// On a phone the header stacks, the author and role badge over the ordinal,
// time and marks (docs/06 "Mobile"). The author's name is a link to their
// profile there, and the ordinal is text, as the phone mockup draws them:
// two links one over the other are too close for each to have its 44 px
// target, and the permalink is under More (TKT-01M3V3CFMA).
//
// A post whose current revision is a moderator's edit says so in place of
// "(edited)", naming nobody: the post's history names them (docs/08 "Post
// history").
export function Post({
  post,
  thread,
  unread,
  actions,
  onFlag,
  onUnread,
  own,
  expander = false,
}: {
  post: PostDTO;
  thread: ThreadRef;
  unread: boolean;
  actions?: PostActions;
  onFlag?: FlagHandler;
  onUnread?: UnreadHandler;
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
  const mark = useMarkFromHere(post.ordinal, onUnread);
  const markHere = idle ? mark.run : undefined;
  // Conversation is the post's reply chain lens, on a post that answers
  // another or has answers (docs/06 "the lens bar").
  const chain = post.reply_to !== null || post.reply_count > 0 ? lensHref(thread, `chain:${post.ordinal}`) : undefined;
  // On a phone the footer reads reply before quote, as the phone mockup draws
  // it; the desktop keeps quote first (docs/06 "Mobile" and "Post anatomy").
  // Each is built once and placed, so DOM, focus and visual order agree.
  const phone = isPhone.value;
  const quote = actions && (
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
  );
  const reply = actions && (
    <button type="button" class="hit" data-post-action="reply" onClick={() => actions.reply(post.ordinal)}>
      reply
    </button>
  );
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
          {isPhone.value ? (
            <span class="ordinal">#{post.ordinal}</span>
          ) : (
            <a class="ordinal hit" href={permalink(thread, post.ordinal)}>
              #{post.ordinal}
            </a>
          )}
          <AuthorLink author={post.author} />
          <RoleBadge author={post.author} />
          <PostTime at={post.created_at} />
          <Edited post={post} />
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
          {actions && o.mode !== 'edit' && (phone ? <>{reply}{quote}</> : <>{quote}{reply}</>)}
          {phone ? (
            <PostMore
              thread={thread}
              ordinal={post.ordinal}
              onFlag={flag}
              onEdit={edit}
              onDelete={del}
              onUnread={markHere}
              unreadBusy={mark.busy}
              chain={chain}
            />
          ) : (
            <>
              <a class="hit" href={permalink(thread, post.ordinal)}>
                link
              </a>
              {chain && (
                <a class="hit" href={chain} data-conversation>
                  conversation
                </a>
              )}
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
              {markHere && (
                <button type="button" class="hit" data-post-action="unread" disabled={mark.busy} onClick={markHere}>
                  unread from here
                </button>
              )}
              <a class="hit" href={`${permalink(thread, post.ordinal)}/history`}>
                history
              </a>
            </>
          )}
          <Restore o={o} />
        </footer>
        {mark.failed !== null && (
          <p class="unread-refused" role="alert" data-mark-unread-refused>
            The thread could not be marked unread from here. {problemMessage(mark.failed).text}
          </p>
        )}
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

// UnreadHandler marks the thread unread from a post, that post included, and
// settles when the mark has: it rejects with the failure, which the post
// shows beside its control.
export type UnreadHandler = (ordinal: number) => Promise<unknown>;

// useMarkFromHere is a post's unread from here: run, when the page offers it,
// and whether its mark is out (busy, when the control is disabled and a
// second press does nothing) or failed. A mark that succeeds has already
// left the page, so nothing is set on a post that is gone.
function useMarkFromHere(ordinal: number, onUnread: UnreadHandler | undefined) {
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState<unknown>(null);
  const out = useRef(false);
  const gone = useRef(false);
  useEffect(() => () => void (gone.current = true), []);
  const run = onUnread
    ? () => {
        if (out.current) return;
        out.current = true;
        setBusy(true);
        setFailed(null);
        onUnread(ordinal)
          .catch((err: unknown) => void (gone.current || setFailed(err)))
          .finally(() => {
            out.current = false;
            if (!gone.current) setBusy(false);
          });
      }
    : undefined;
  return { run, busy, failed };
}

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

// AuthorLink is a post header's author: the name, linking to their profile
// (docs/06 "Targets and gestures" lists author names among the inline
// links, and every mockup draws one). On the phone it takes the 44 px hit
// area. Whether the profile is served is the server's to say.
export function AuthorLink({ author }: { author: PostDTO['author'] }) {
  return (
    <a class="author hit" href={profileHref(author.handle)}>
      {author.display_name || author.handle}
    </a>
  );
}

// Edited is a post's edited marker, on the thread page and in the reply
// expander alike: "(edited by a moderator)" when its current revision is a
// moderator's edit, naming nobody, and "(edited)" otherwise (TKT-01M3QHKD21,
// Terva review 1626).
export function Edited({ post }: { post: Pick<PostDTO, 'edited_at' | 'edited_by_moderator'> }) {
  if (post.edited_at === null) return null;
  return (
    <span class="edited" data-edited-by={post.edited_by_moderator ? 'moderator' : undefined}>
      {post.edited_by_moderator ? '(edited by a moderator)' : '(edited)'}
    </span>
  );
}
