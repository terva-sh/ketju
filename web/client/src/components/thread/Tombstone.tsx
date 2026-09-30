import { useState } from 'preact/hooks';
import type { Post as PostDTO } from '../../api/dto';
import { ReplyTo, useHeldStill, type ThreadRef } from './Post';
import { Restore, useOwn, type Own } from './OwnControls';
import { TombstoneText } from './removal';
import { Replies, ReplyToggle } from './ReplyExpander';

// Tombstone stands where a removed or moved post was, keeping its ordinal,
// its anchor and its reply indicator, so the thread's numbering and every
// link into it still hold (docs/06, docs/08 "Tombstones"). Replies to it
// keep their reply_to, so its expander still opens them.
//
// own is handed over on the viewer's own post, on the thread page only. A
// member's own deleted post reaches them as a tombstone, and while the page
// offers it (postControls), Restore is in its footer (TKT-01M3PTT97X).
export function Tombstone({
  post,
  thread,
  unread,
  own,
  expander = false,
}: {
  post: PostDTO;
  thread: ThreadRef;
  unread: boolean;
  own?: Own;
  expander?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const toggle = useHeldStill(() => setOpen((o) => !o));
  const o = useOwn(own, post.ordinal);
  return (
    <div
      class={'post tombstone' + (unread ? ' unread' : '')}
      id={`post-${post.ordinal}`}
      data-ordinal={post.ordinal}
      tabIndex={-1}
    >
      <span class="avatar avatar-empty" aria-hidden="true" />
      <div class="post-main">
        <div class="tombstone-line">
          <span class="ordinal">#{post.ordinal}</span>
          <TombstoneText post={post} />
          <ReplyTo post={post} />
        </div>
        {(expander || o.offer.restore || o.failure) && (
          <div class="post-footer">
            {expander && <ReplyToggle post={post} open={open} onToggle={toggle} />}
            <Restore o={o} />
          </div>
        )}
        {expander && open && <Replies thread={thread} ordinal={post.ordinal} />}
      </div>
    </div>
  );
}
