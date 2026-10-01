import type { ComponentChildren } from 'preact';
import { useId, useRef } from 'preact/hooks';
import { Icon } from '../Icon';
import { isControl, useModal } from '../focus';

// Sheet is a phone's sheet over the thread page: the page picker (PageSheet)
// and the lens switcher (LensBar), docs/06 "Mobile", "Pages". It is a modal
// dialog through useModal, drawn as the forum picker is (forums.css) and
// docked to the foot of the screen: focus goes to start, or to the first
// link, Tab stays inside, and Escape, the close button and a press on the
// backdrop end it. Nothing is swipe-only.
//
// When it ends, focus goes back to opener if it is still in the page and
// focus is in the sheet or on the body. A choice that moved focus on, as
// First unread focusing a post on this page, keeps it there. The cleanup
// runs before Preact removes the sheet, so focus is still inside it then.
export function Sheet({
  title,
  opener,
  onClose,
  start,
  children,
}: {
  title: string;
  opener: { current: HTMLElement | null };
  onClose: () => void;
  start?: string;
  children: ComponentChildren;
}) {
  const root = useRef<HTMLDivElement>(null);
  const id = useId();
  useModal(root, onClose, (el) => {
    (el?.querySelector<HTMLElement>(start ?? 'a[href]') ?? el?.querySelector<HTMLElement>('a[href], button'))?.focus();
    return () => {
      const at = document.activeElement;
      const back = opener.current;
      if (isControl(back) && (!at || at === document.body || el?.contains(at))) back.focus();
    };
  });
  return (
    <div class="picker-backdrop sheet-backdrop" onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={root} class="forum-picker sheet" role="dialog" aria-modal="true" aria-labelledby={id}>
        <div class="picker-head">
          <h2 id={id}>{title}</h2>
          <button type="button" class="picker-close" aria-label="Close" onClick={onClose}>
            <Icon name="close" />
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}
