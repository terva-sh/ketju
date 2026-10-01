import { useLayoutEffect, useRef, useState } from 'preact/hooks';
import type { Lens, ThreadPage } from '../../api/dto';
import { sameHandle } from '../../shell/nav';
import { lensHref, lensParam } from './lens';
import { LensMenu } from './LensMenu';
import { Sheet } from './Sheet';

// LensBar sits under the thread head at both widths, as the mockups draw it
// (docs/06 "the lens bar", TKT-01M29CTY71): Only <handle>, the author lens
// for the thread's author, and Lens, which opens the switcher. Each is a
// link or opens links, so every lens is an address a reader can open in a
// new tab or share.
//
// From 720 px the switcher is a disclosure of links, as PostMore is, not a
// modal.
// Escape and a press outside close it and return focus to Lens. The
// listeners go on in the layout effect of the press that opened it, as
// PostMore's do (TKT-01M3TV4HBF).
//
// Below 720 px Lens opens the same list in a sheet, a modal dialog
// (docs/06 "Mobile", "Pages"; TKT-01M29CGBZD). Both buttons are drawn and
// CSS shows one, as the pager's rows are, so the same component renders at
// every size; only one of the menu and the sheet is open at a time, since
// they share the list's ids.
export function LensBar({ page, lens }: { page: ThreadPage; lens: Lens | null }) {
  const [open, setOpen] = useState(false);
  const [sheet, setSheet] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const opener = useRef<HTMLButtonElement>(null);
  const author = page.thread.author.handle;
  const only = lens?.kind === 'author' && sameHandle(lens.value, author);
  const active = lens ? lensParam(lens.kind, lens.value) : '';

  useLayoutEffect(() => {
    if (!open) return;
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

  return (
    <div class="lens-bar" ref={root} data-lens-bar>
      <a
        class="lens-tool"
        href={lensHref(page.thread, lensParam('author', author))}
        aria-current={only ? 'true' : undefined}
        data-lens-only
      >
        Only {author}
      </a>
      <button
        ref={button}
        type="button"
        class="lens-tool"
        aria-expanded={open}
        aria-controls="lens-menu"
        data-lens-switch
        onClick={() => {
          setSheet(false);
          setOpen(!open);
        }}
      >
        Lens <span aria-hidden="true">▾</span>
      </button>
      <button
        ref={opener}
        type="button"
        class="lens-tool"
        aria-haspopup="dialog"
        aria-expanded={sheet}
        data-lens-sheet
        onClick={() => {
          setOpen(false);
          setSheet(true);
        }}
      >
        Lens <span aria-hidden="true">▾</span>
      </button>
      {open && <LensMenu page={page} active={active} close={() => setOpen(false)} />}
      {sheet && (
        <Sheet title="Lens" opener={opener} onClose={() => setSheet(false)}>
          <LensMenu page={page} active={active} close={() => setSheet(false)} />
        </Sheet>
      )}
    </div>
  );
}
