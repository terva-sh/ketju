import { useEffect, useRef } from 'preact/hooks';
import type { Forum } from '../api/dto';
import { Icon } from '../components/Icon';
import { trapTab } from '../components/focus';

// ForumPicker is where a new thread starts from the forum index: a thread
// needs a forum, and /f/{slug}/new is the one address for starting one
// (settled in grooming for TKT-01M29CEE5Y). It lists the forums the reader
// can see that are not archived, children under their parent, each linking
// to its new-thread screen.
//
// It is a modal dialog: focus starts on the first forum, Tab stays inside,
// and Escape or the close button ends it; the caller returns focus to what
// opened it. Whether the reader may start a thread in a forum is the
// server's to say when they post (create_thread); the tree does not carry it.
export function ForumPicker({ forums, onClose }: { forums: Forum[]; onClose: () => void }) {
  const root = useRef<HTMLDivElement>(null);
  // Focus goes inside the dialog, on the first forum or, with none to
  // offer, the close button, so Escape and Tab always reach the dialog's
  // handler (review on PR #86).
  useEffect(() => {
    const el = root.current;
    (el?.querySelector<HTMLElement>('a[href]') ?? el?.querySelector<HTMLElement>('button'))?.focus();
  }, []);
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Escape' && !e.defaultPrevented) {
      e.preventDefault();
      onClose();
    } else if (e.key === 'Tab') trapTab(e, root.current);
  };
  const choices = pickable(forums);
  return (
    <div class="picker-backdrop" onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div
        ref={root}
        class="forum-picker"
        role="dialog"
        aria-modal="true"
        aria-labelledby="forum-picker-title"
        onKeyDown={onKeyDown}
      >
        <div class="picker-head">
          <h2 id="forum-picker-title">Start a thread in…</h2>
          <button type="button" class="picker-close" aria-label="Close" onClick={onClose}>
            <Icon name="back" size={16} />
          </button>
        </div>
        <ul class="picker-list">
          {choices.map(({ f, children }) => (
            <li key={f.slug}>
              {f.archived ? <span class="picker-heading">{f.name}</span> : <Choice forum={f} onClose={onClose} />}
              {children.length > 0 && (
                <ul>
                  {children.map((c) => (
                    <li key={c.slug}>
                      <Choice forum={c} onClose={onClose} />
                    </li>
                  ))}
                </ul>
              )}
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

function Choice({ forum, onClose }: { forum: Forum; onClose: () => void }) {
  return (
    <a href={`/f/${encodeURIComponent(forum.slug)}/new`} onClick={onClose}>
      {forum.name}
    </a>
  );
}

// pickable is what the picker offers: every forum that is not archived. An
// archived root with open children stays as their heading, not as a place
// to post. The index offers the picker only when this has a forum in it.
export function pickable(forums: Forum[]) {
  return forums
    .map((f) => ({ f, children: f.children.filter((c) => !c.archived) }))
    .filter(({ f, children }) => !f.archived || children.length > 0);
}
