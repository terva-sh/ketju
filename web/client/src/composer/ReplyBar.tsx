import { Icon } from '../components/Icon';
import { composerFor } from '../state/composer';

// ReplyBar is the phone's bar at the foot of a thread (docs/06 "Mobile";
// the mockup's mobile_thread): the reply chip, and the draft's first line or
// an invitation to write, which opens the composer sheet. It takes the tab
// bar's place (Shell), and the thread page is padded by its height so it
// never covers the last post (composer.css).
//
// The mockup's round "Post reply" button is not here: posting is in the
// sheet's head (TKT-01M3ART992), where a refusal has the composer to be
// shown in; from the bar it would have nowhere to go.
export function ReplyBar({ thread }: { thread: string }) {
  const c = composerFor(thread);
  const replyTo = c.replyTo.value;
  const first = firstLine(c.source.value);
  return (
    <div class="replybar" data-shell="replybar">
      {replyTo !== null && (
        <span class="reply-chip" data-reply-chip={replyTo}>
          <Icon name="reply" size={12} />#{replyTo}
        </span>
      )}
      <button
        type="button"
        class={first ? 'replybar-open draft' : 'replybar-open'}
        aria-haspopup="dialog"
        aria-expanded={c.open.value}
        onClick={() => (c.open.value = true)}
      >
        <span class="visually-hidden">{first ? 'Continue your reply: ' : ''}</span>
        <span class="replybar-text">{first || 'Write a reply…'}</span>
      </button>
    </div>
  );
}

// firstLine is the first line of source with anything in it, trimmed.
export function firstLine(source: string): string {
  for (const line of source.split('\n')) {
    const t = line.trim();
    if (t) return t;
  }
  return '';
}
