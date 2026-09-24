import { useEffect, useRef, useState } from 'preact/hooks';
import { Icon } from '../components/Icon';
import type { ShellAction } from '../state/shell';

// MoreMenu is the phone top bar's overflow: the actions the desktop toolbar
// shows as buttons (docs/06 "Mobile"). Escape or a click elsewhere closes
// it, and focus goes back to the button that opened it.
export function MoreMenu({ actions }: { actions: ShellAction[] }) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    root.current?.querySelector<HTMLElement>('[role=menuitem]')?.focus();
    const away = (e: Event) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false);
    };
    const key = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
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
