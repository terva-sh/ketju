import { useLayoutEffect, useRef, useState } from 'preact/hooks';
import type { Lens, ThreadPage } from '../../api/dto';
import { sameHandle } from '../../shell/nav';
import { lensHref, lensParam } from './lens';
import { LensMenu } from './LensMenu';

// LensBar sits under the thread head at both widths, as the mockups draw it
// (docs/06 "the lens bar", TKT-01M29CTY71): Only <handle>, the author lens
// for the thread's author, and Lens, which opens the switcher. Each is a
// link or opens links, so every lens is an address a reader can open in a
// new tab or share. On phones TKT-01M29CGBZD later presents the switcher as
// a sheet; until then this one serves both widths.
//
// The switcher is a disclosure of links, as PostMore is, not a modal.
// Escape and a press outside close it and return focus to Lens. The
// listeners go on in the layout effect of the press that opened it, as
// PostMore's do (TKT-01M3TV4HBF).
export function LensBar({ page, lens }: { page: ThreadPage; lens: Lens | null }) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
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
        onClick={() => setOpen(!open)}
      >
        Lens <span aria-hidden="true">▾</span>
      </button>
      {open && <LensMenu page={page} active={active} close={() => setOpen(false)} />}
    </div>
  );
}
