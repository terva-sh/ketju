import { useId, useState } from 'preact/hooks';
import { useLocation } from 'preact-iso';
import type { Pagination } from '../../api/dto';
import { SinceForm } from './LensMenu';
import { pageHref } from './Pager';
import type { ThreadRef } from './Post';
import { Sheet } from './Sheet';

// PageSheet is the phone's page picker, opened from the pager's "Page N of
// M" (docs/06 "Mobile", "Pages", decided by the run on 2026-10-01): First
// unread when the reader has posts they have not read, a grid of numbered
// page links with the current one marked and focused, a Go to page field
// past 60 pages, and a date field that opens the since lens. Every page is
// a link that keeps the lens (pageHref), and choosing anything closes it.
//
// firstUnread is the thread page's own handler, offered on its terms, so it
// goes where First unread in the thread actions goes (keyboard/shortcuts.ts
// firstUnread): the reader's position now plus one.
export function PageSheet({
  page,
  thread,
  lens,
  firstUnread,
  opener,
  onClose,
}: {
  page: Pagination;
  thread: ThreadRef;
  lens: string;
  firstUnread?: () => void;
  opener: { current: HTMLElement | null };
  onClose: () => void;
}) {
  const { route } = useLocation();
  const total = page.total_pages;
  const [to, setTo] = useState('');
  const id = useId();
  return (
    <Sheet title="Pages" opener={opener} onClose={onClose} start="a[aria-current]">
      {firstUnread && (
        <button
          type="button"
          class="sheet-action"
          data-sheet-first-unread
          onClick={() => {
            onClose();
            firstUnread();
          }}
        >
          First unread
        </button>
      )}
      {total > maxGrid && (
        <form
          class="lens-since"
          onSubmit={(e) => {
            e.preventDefault();
            const n = Number(to);
            if (!Number.isInteger(n) || n < 1 || n > total) return;
            onClose();
            route(pageHref(thread, n, lens));
          }}
        >
          <label for={id}>Go to page</label>
          <input
            id={id}
            type="number"
            inputMode="numeric"
            min={1}
            max={total}
            step={1}
            required
            value={to}
            onInput={(e) => setTo((e.currentTarget as HTMLInputElement).value)}
          />
          <button type="submit">Go</button>
        </form>
      )}
      <ul class="page-grid">
        {sheetPages(page.number, total).map((n, i) =>
          n === null ? (
            <li key={`gap${i}`} class="gap" aria-hidden="true">
              …
            </li>
          ) : (
            <li key={n}>
              <a
                href={pageHref(thread, n, lens)}
                aria-current={n === page.number ? 'page' : undefined}
                aria-label={`Page ${n}`}
                onClick={onClose}
              >
                {n}
              </a>
            </li>
          ),
        )}
      </ul>
      <SinceForm thread={thread} active={lens} close={onClose} label="Jump to date" id={`${id}d`} />
    </Sheet>
  );
}

// maxGrid is the most pages the sheet draws every one of.
const maxGrid = 60;

// sheetPages is the page links the sheet draws, with null where a run is
// skipped: every page up to maxGrid pages, and past that the first, the last
// and the 20 either side of current, so the sheet draws at most 43 links
// however long the thread is (Terva review 1691). The Go to page field
// reaches the rest.
export function sheetPages(current: number, total: number): (number | null)[] {
  if (total <= maxGrid) return Array.from({ length: total }, (_, i) => i + 1);
  const lo = Math.max(2, current - 20);
  const hi = Math.min(total - 1, current + 20);
  const out: (number | null)[] = [1];
  if (lo > 2) out.push(null);
  for (let n = lo; n <= hi; n++) out.push(n);
  if (hi < total - 1) out.push(null);
  out.push(total);
  return out;
}
