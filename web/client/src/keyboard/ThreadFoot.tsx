import { useEffect, useState } from 'preact/hooks';
import type { ThreadPage } from '../api/dto';
import { isPhone } from '../state/layout';
import { HelpOverlay } from './HelpOverlay';
import { goToOrdinal, helpOpen, hint, openNextUnread, tracksUnread } from './shortcuts';
import './keyboard.css';

// ThreadFoot is below a thread page's posts: the visible controls the
// keyboard shortcuts stand in for (shortcuts.ts), so a shortcut is never the
// only way to anything (docs/06 "Mobile"). Go to post is g, Next unread
// thread is n, and Keyboard shortcuts is ?, which is offered only where the
// shortcuts are, off the phone layout. First unread, u, is one of the
// thread's actions (threadActions). It also holds the status line the
// shortcuts and these controls report through.
export function ThreadFoot({ page, route }: { page: ThreadPage; route: (url: string) => void }) {
  const [ordinal, setOrdinal] = useState('');
  const signedIn = tracksUnread();
  const phone = isPhone.value;
  useEffect(
    () => () => {
      helpOpen.value = false;
    },
    [],
  );
  return (
    <>
      <nav class="thread-foot" aria-label="Thread navigation">
        <form
          class="goto-post"
          onSubmit={(e) => {
            e.preventDefault();
            if (ordinal.trim()) goToOrdinal(Number(ordinal), page, route);
          }}
        >
          <label for="goto-post-number">Go to post</label>
          <input
            id="goto-post-number"
            type="number"
            inputMode="numeric"
            min={1}
            max={page.thread.post_count}
            placeholder="#"
            value={ordinal}
            onInput={(e) => setOrdinal((e.currentTarget as HTMLInputElement).value)}
          />
          <button type="submit">Go</button>
        </form>
        {signedIn && (
          <button type="button" class="next-unread" onClick={() => void openNextUnread(page, route)}>
            Next unread thread
          </button>
        )}
        {!phone && (
          <button type="button" class="shortcuts-help" onClick={() => (helpOpen.value = true)}>
            Keyboard shortcuts
          </button>
        )}
      </nav>
      <p class={'shortcut-hint' + (hint.value ? ' shown' : '')} role="status">
        {hint.value}
      </p>
      {helpOpen.value && !phone && <HelpOverlay />}
    </>
  );
}
