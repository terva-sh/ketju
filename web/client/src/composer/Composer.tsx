import type { CreatedPost } from '../api/dto';
import { asProblem, problemMessage } from '../api/problem';
import { Icon } from '../components/Icon';
import { composerFor, discardReply, postReply } from '../state/composer';
import { draftNote } from '../state/drafts';
import { ComposerBody } from './ComposerBody';

// The delays live with the body they time, and are exported here too, where
// the reply composer's tests read them.
export { mentionDelay, previewDelay } from './ComposerBody';

// Composer is the reply composer at the foot of a thread (docs/06
// "Composer"; ThreadLight's composer): the shared ComposerBody (Write and
// Preview, the toolbar, @handle autocomplete, the server's preview) with the
// reply chip in its head and Post reply in its footer.
//
// Post reply sends the source (postReply) and, once it is posted, hands the
// answer to onPosted, which shows the new post; a refusal is said above the
// text, which stays. The source and the chip are autosaved as the viewer's
// draft (state/drafts.ts), and the footer says how that stands.
//
// In the phone's sheet (ComposerSheet, sheet set) it is the same composer
// laid out as a column that fills the sheet, toolbar last, and it takes
// focus when it opens, since the sheet opens because someone asked to write.
export function Composer({
  thread,
  authorOf,
  onPosted,
  sheet = false,
}: {
  thread: string;
  authorOf: (ordinal: number) => string | undefined;
  onPosted: (res: CreatedPost) => void;
  sheet?: boolean;
}) {
  const c = composerFor(thread);
  const source = c.source.value;
  const posting = c.posting.value;
  const failure = c.failure.value;
  const replyTo = c.replyTo.value;
  return (
    <ComposerBody
      state={c}
      thread={thread}
      replyTo={replyTo}
      name="Reply"
      label="Reply text"
      placeholder="Write a reply. Markdown works."
      sheet={sheet}
      focus={sheet}
      head={
        <>
          <h2>Reply</h2>
          {replyTo !== null && (
            <span class="reply-chip" data-reply-chip={replyTo}>
              <Icon name="reply" size={12} />#{replyTo}
              {authorOf(replyTo) && <span class="who">{authorOf(replyTo)}</span>}
              <button type="button" aria-label={`Stop replying to #${replyTo}`} disabled={posting} onClick={() => (c.replyTo.value = null)}>
                ×
              </button>
            </span>
          )}
        </>
      }
      notice={
        failure !== null && (
          <p class="post-failure" role="alert">
            {postFailure(failure)}
          </p>
        )
      }
      foot={
        <footer class="composer-foot">
          <span>Markdown supported</span>
          <span class="draft-note" role="status">
            {draftNote({ kind: 'reply', thread })}
          </span>
          <button type="button" class="discard" disabled={posting || (!source && replyTo === null)} onClick={() => discardReply(thread)}>
            Discard
          </button>
          <PostButton thread={thread} onPosted={onPosted} />
        </footer>
      }
    />
  );
}

// PostButton posts thread's composer. It is off while there is nothing to
// post and while a post is in flight, and postReply refuses a second post
// then too, so a double click posts once.
export function PostButton({ thread, onPosted, label = 'Post reply' }: { thread: string; onPosted: (res: CreatedPost) => void; label?: string }) {
  const c = composerFor(thread);
  const posting = c.posting.value;
  return (
    <button
      type="button"
      class="post-reply"
      data-post-reply
      disabled={posting || !c.source.value.trim()}
      aria-busy={posting}
      onClick={() => void postReply(thread).then((res) => res && onPosted(res))}
    >
      {posting ? 'Posting…' : label}
    </button>
  );
}

// postFailure says why a post was refused, in the words of posting rather
// than the generic ones (api/problem): the refusals POST /threads/{id}/posts
// names in docs/07.
export function postFailure(err: unknown): string {
  const p = asProblem(err);
  switch (p.type) {
    case 'conflict':
      return 'This thread is locked or archived, so your reply was not posted.';
    case 'forbidden':
      return 'You may not reply in this thread.';
    case 'rate-limited':
      return p.retryAfter
        ? `This thread is in slow mode. You can post again in ${p.retryAfter} second${p.retryAfter === 1 ? '' : 's'}.`
        : 'You are posting too quickly. Wait a moment and try again.';
    case 'validation-failed':
      return 'The reply was not accepted: it may be empty, or answer a post that is not in this thread.';
    case 'payload-too-large':
      return 'The reply is longer than the limit allows.';
    default:
      return problemMessage(err).text;
  }
}
