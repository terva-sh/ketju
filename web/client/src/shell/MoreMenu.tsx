import { useLayoutEffect, useRef, useState } from 'preact/hooks';
import { Icon } from '../components/Icon';
import type { ShellAction } from '../state/shell';

// MoreMenu is the phone top bar's overflow: the actions the desktop toolbar
// shows as buttons (docs/06 "Mobile"). Escape or a click elsewhere closes
// it, and focus goes back to the button that opened it.
//
// Focus and the listeners go in a layout effect, in the same task as the
// press that opened the menu, and come off in the commit that closes it. A
// plain effect waited a frame at both ends: an Escape pressed straight
// after opening was lost, and one pressed straight after choosing Lock was
// taken here, after the lock panel had closed, and put focus on this
// button rather than the thread's title (TKT-01M3TV4HBF). An Escape another
// handler has already taken is left alone.
export function MoreMenu({ actions }: { actions: ShellAction[] }) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);

  useLayoutEffect(() => {
    if (!open) return;
    root.current?.querySelector<HTMLElement>('[role=menuitem]')?.focus();
    // A press elsewhere closes the menu. Focus was on an item, which is about
    // to go, so it goes back to the button (review 490); a press on
    // something focusable then takes it from there as usual.
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

  if (actions.length === 0) return null;
  return (
    <div class="more" ref={root}>
      <button
        ref={button}
        type="button"
        class="tb-button"
        aria-label="More"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        <Icon name="more" size={20} />
      </button>
      {open && (
        <div class="menu" role="menu" data-shell="more-menu">
          {actions.map((a) => (
            <button
              key={a.id}
              type="button"
              role="menuitem"
              data-action={a.id}
              onClick={() => {
                setOpen(false);
                button.current?.focus();
                a.run();
              }}
            >
              {a.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
