import { useState } from 'preact/hooks';
import type { Post as PostDTO } from '../../api/dto';
import { ReplyTo, useHeldStill, type ThreadRef } from './Post';
import { TombstoneText } from './removal';
import { Replies, ReplyToggle } from './ReplyExpander';

// Tombstone stands where a removed or moved post was, keeping its ordinal,
// its anchor and its reply indicator, so the thread's numbering and every
// link into it still hold (docs/06, docs/08 "Tombstones"). Replies to it
// keep their reply_to, so its expander still opens them.
export function Tombstone({
  post,
  thread,
  unread,
  expander = false,
}: {
  post: PostDTO;
  thread: ThreadRef;
  unread: boolean;
  expander?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const toggle = useHeldStill(() => setOpen((o) => !o));
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
        {expander && (
          <div class="post-footer">
            <ReplyToggle post={post} open={open} onToggle={toggle} />
          </div>
        )}
        {expander && open && <Replies thread={thread} ordinal={post.ordinal} />}
      </div>
    </div>
  );
}
