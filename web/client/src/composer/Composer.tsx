import { useLayoutEffect } from 'preact/hooks';
import type { CreatedPost } from '../api/dto';
import { asProblem, problemMessage } from '../api/problem';
import { Icon } from '../components/Icon';
import { composerFor, discardReply, holdAtLeast, postReply, type Slow } from '../state/composer';
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
// In slow mode (slow, from the thread page) it says the thread's interval,
// and while the viewer must wait, Post is off with a countdown beside it
// (TKT-01M3R0PM0K). The wait is the later of the page's next_post_at and
// what this composer has seen since: the viewer's own post, or a refusal's
// Retry-After (state/composer.ts). The client's clock decides when Post
// comes back; one that runs fast is refused, and the Retry-After starts the
// wait again.
//
// In the phone's sheet (ComposerSheet, sheet set) it is the same composer
// laid out as a column that fills the sheet, toolbar last, and it takes
// focus when it opens, since the sheet opens because someone asked to write.
export function Composer({
  thread,
  authorOf,
  onPosted,
  slow,
  sheet = false,
}: {
  thread: string;
  authorOf: (ordinal: number) => string | undefined;
  onPosted: (res: CreatedPost) => void;
  slow?: Slow;
  sheet?: boolean;
}) {
  const c = composerFor(thread);
  const nextPostAt = slow?.nextPostAt ?? null;
  // A layout effect, so Post is never shown on before the page's wait.
  useLayoutEffect(() => holdAtLeast(thread, nextPostAt), [thread, nextPostAt]);
  const left = c.left.value;
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
        <>
          {!!slow?.seconds && (
            <p class="slow-mode" data-slow-mode>
              {slowModeText(slow.seconds)}
            </p>
          )}
          {left > 0 && (
            <p class="slow-wait" id={`slow-wait-${thread}`} data-slow-wait>
              You can post again in {waitText(left)}
            </p>
          )}
          {failure !== null && (
            <p class="post-failure" role="alert">
              {postFailure(failure)}
            </p>
          )}
        </>
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
          <PostButton thread={thread} onPosted={onPosted} slow={slow} />
        </footer>
      }
    />
  );
}

// PostButton posts thread's composer. It is off while there is nothing to
// post, while a post is in flight, and while slow mode holds the viewer,
// when it points to the countdown. postReply refuses a second post while
// one is in flight, so a double click posts once.
export function PostButton({
  thread,
  onPosted,
  slow,
  label = 'Post reply',
}: {
  thread: string;
  onPosted: (res: CreatedPost) => void;
  slow?: Slow;
  label?: string;
}) {
  const c = composerFor(thread);
  const posting = c.posting.value;
  const held = c.left.value > 0;
  return (
    <button
      type="button"
      class="post-reply"
      data-post-reply
      disabled={posting || held || !c.source.value.trim()}
      aria-busy={posting}
      aria-describedby={held ? `slow-wait-${thread}` : undefined}
      onClick={() => void postReply(thread, slow).then((res) => res && onPosted(res))}
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

// slowModeText says a slow mode of seconds, in minutes or hours when they
// divide it evenly: "every 30 seconds", "every 5 minutes", "every hour".
export function slowModeText(seconds: number): string {
  const [n, unit] = seconds % 3600 ? (seconds % 60 ? [seconds, 'second'] : [seconds / 60, 'minute']) : [seconds / 3600, 'hour'];
  return `Slow mode: one post every ${n === 1 ? unit : `${n} ${unit}s`}`;
}

// waitText writes a wait of left seconds as m:ss, or as h:mm:ss from an hour
// up, with no upper bound on the hours.
export function waitText(left: number): string {
  const two = (n: number) => String(n).padStart(2, '0');
  const h = Math.floor(left / 3600);
  const m = Math.floor(left / 60) % 60;
  return `${h ? `${h}:${two(m)}` : m}:${two(left % 60)}`;
}
