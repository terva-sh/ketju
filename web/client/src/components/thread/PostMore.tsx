import { useEffect, useRef, useState } from 'preact/hooks';
import { Icon } from '../Icon';
import { permalink, type FlagHandler, type ThreadRef } from './Post';

// PostMore is a post's "more" control on a phone (docs/06 "Mobile"): link,
// flag, edit, delete, unread from here and history collapse under it, since
// six inline controls with 44 px targets do not fit the footer at 390 px. It
// is a disclosure, not a menu: link and history stay real anchors, which a
// reader can open in a new tab or copy. Escape, a press elsewhere, or
// choosing an item closes it.
//
// Flag is there only when the post hands it over (Post decides who may
// flag). Choosing it closes the list and hands the dialog this post's More
// button to return focus to: the flag item is gone by the time the dialog
// closes, and the button stays (Terva review 1525 on PR #181).
//
// Edit and delete follow flag when the post hands them over, on the
// viewer's own post while the page offers them (TKT-01M3PTT97X). Choosing
// one closes the list, and the editor or the confirmation opens in the
// post; when it closes, focus comes back to this button (useOwn).
//
// Unread from here is there only when the post hands it over too, and is
// disabled while its mark is out (unreadBusy). Choosing it closes the list
// and puts focus back on the button, as Escape does; a mark that succeeds
// then leaves the page, and one that fails is reported beside the post
// (TKT-01M3KFFFJR).
export function PostMore({
  thread,
  ordinal,
  onFlag,
  onEdit,
  onDelete,
  onUnread,
  unreadBusy = false,
}: {
  thread: ThreadRef;
  ordinal: number;
  onFlag?: FlagHandler;
  onEdit?: () => void;
  onDelete?: () => void;
  onUnread?: () => void;
  unreadBusy?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const list = `post-more-${ordinal}`;

  useEffect(() => {
    if (!open) return;
    // Focus was on an item, which is about to be hidden, so it goes back to
    // the button, as the shell's MoreMenu does.
    const away = (e: Event) => {
      if (root.current?.contains(e.target as Node)) return;
      if (root.current?.contains(document.activeElement)) button.current?.focus();
      setOpen(false);
    };
    const key = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || e.defaultPrevented) return;
      e.preventDefault();
      setOpen(false);
      button.current?.focus();
    };
    document.addEventListener('pointerdown', away);
    document.addEventListener('keydown', key);
    return () => {
      document.removeEventListener('pointerdown', away);
      document.removeEventListener('keydown', key);
    };
  }, [open]);

  const href = permalink(thread, ordinal);
  return (
    <div class="post-more" ref={root}>
      <button
        ref={button}
        type="button"
        class="hit"
        aria-label={`More actions for post #${ordinal}`}
        aria-expanded={open}
        aria-controls={list}
        data-post-more
        onClick={() => setOpen(!open)}
      >
        <Icon name="more" size={16} />
      </button>
      <ul id={list} class="post-more-list" hidden={!open}>
        <li>
          <a href={href} onClick={() => setOpen(false)}>
            link
          </a>
        </li>
        {onFlag && (
          <li>
            <button
              type="button"
              data-post-action="flag"
              onClick={() => {
                setOpen(false);
                onFlag(ordinal, button.current!);
              }}
            >
              flag
            </button>
          </li>
        )}
        {(
          [
            ['edit', onEdit],
            ['delete', onDelete],
          ] as const
        ).map(
          ([name, act]) =>
            act && (
              <li key={name}>
                <button
                  type="button"
                  data-post-action={name}
                  onClick={() => {
                    setOpen(false);
                    act();
                  }}
                >
                  {name}
                </button>
              </li>
            ),
        )}
        {onUnread && (
          <li>
            <button
              type="button"
              data-post-action="unread"
              disabled={unreadBusy}
              onClick={() => {
                setOpen(false);
                button.current?.focus();
                onUnread();
              }}
            >
              unread from here
            </button>
          </li>
        )}
        <li>
          <a href={`${href}/history`} onClick={() => setOpen(false)}>
            history
          </a>
        </li>
      </ul>
    </div>
  );
}
