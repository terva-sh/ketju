import { useRef, useState } from 'preact/hooks';
import type { Pagination } from '../../api/dto';
import { Icon } from '../Icon';
import { lensQuery } from './lens';
import { PageSheet } from './PageSheet';
import type { ThreadRef } from './Post';

// Pager moves between a thread's pages (docs/06 "Paged, not infinite"). The
// desktop row numbers the pages around the current one; the phone row is
// "Page N of M" between previous and next. Both are rendered and CSS shows
// one, at the 720 px breakpoint. Every page is a link, so it can be opened in
// a new tab and the router follows it in this one; a link keeps the lens and
// leaves any permalink, since the page is now the reader's choice.
//
// On a phone the label is a button that opens the page sheet (PageSheet),
// which reaches every page, First unread (firstUnread, the thread page's
// handler, when it offers one) and a date (TKT-01M29CGBZD).
export function Pager({
  page,
  thread,
  lens,
  postCount,
  firstUnread,
}: {
  page: Pagination;
  thread: ThreadRef;
  lens: string;
  postCount: number;
  firstUnread?: () => void;
}) {
  const [sheet, setSheet] = useState(false);
  const opener = useRef<HTMLButtonElement>(null);
  if (page.total_pages <= 1) return null;
  const href = (n: number) => pageHref(thread, n, lens);
  const prev = page.number > 1 ? page.number - 1 : null;
  const next = page.number < page.total_pages ? page.number + 1 : null;
  const first = (page.number - 1) * page.size + 1;
  const last = Math.min(page.number * page.size, postCount);
  return (
    <nav class="pager" aria-label="Pages">
      <div class="pager-wide">
        <PageStep to={prev} href={href} label="Previous page" text="‹" />
        {pageWindow(page.number, page.total_pages).map((n, i) =>
          n === null ? (
            <span key={`gap${i}`} class="gap">
              …
            </span>
          ) : n === page.number ? (
            <span key={n} class="page current" aria-current="page">
              {n}
            </span>
          ) : (
            <a key={n} class="page" href={href(n)}>
              {n}
            </a>
          ),
        )}
        <PageStep to={next} href={href} label="Next page" text="›" />
        {!lens && (
          <span class="span">
            posts {first}–{last} of {postCount}
          </span>
        )}
      </div>
      <div class="pager-narrow">
        <PageStep to={prev} href={href} label="Previous page" text="‹ Prev" />
        <button
          ref={opener}
          type="button"
          class="where"
          aria-haspopup="dialog"
          aria-expanded={sheet}
          onClick={() => setSheet(true)}
        >
          Page {page.number} of {page.total_pages}
          <Icon name="down" size={12} />
        </button>
        <PageStep to={next} href={href} label="Next page" text="Next ›" />
      </div>
      {sheet && (
        <PageSheet
          page={page}
          thread={thread}
          lens={lens}
          firstUnread={firstUnread}
          opener={opener}
          onClose={() => setSheet(false)}
        />
      )}
    </nav>
  );
}

function PageStep({ to, href, label, text }: { to: number | null; href: (n: number) => string; label: string; text: string }) {
  return to === null ? (
    // An inactive step holds the place, so the pager does not shift between
    // pages. It is hidden from assistive technology and marked disabled,
    // which is what exempts its faded text from the contrast minimum (WCAG
    // 1.4.3, inactive components).
    <span class="step disabled" aria-hidden="true" aria-disabled="true">
      {text}
    </span>
  ) : (
    <a class="step" href={href(to)} aria-label={label} rel={label.startsWith('Next') ? 'next' : 'prev'}>
      {text}
    </a>
  );
}

// pageHref always names the page, the first included: an address with no
// page means "where I left off", which for a signed-in reader is their
// landing page, not page 1.
// The lens is written as lensQuery writes it, so the address reads
// lens=unread:25 as every other lens link does.
export function pageHref(thread: ThreadRef, n: number, lens: string): string {
  return `/t/${encodeURIComponent(thread.id)}/${encodeURIComponent(thread.slug)}?page=${n}${lens ? `&lens=${lensQuery(lens)}` : ''}`;
}

// pageWindow is the page numbers to show: the first, the last, and two either
// side of the current one, with null where a run is skipped. A gap of one
// page shows the page instead, since "…" would take the same room.
export function pageWindow(current: number, total: number): (number | null)[] {
  const keep = new Set([1, total]);
  for (let n = current - 2; n <= current + 2; n++) if (n >= 1 && n <= total) keep.add(n);
  const sorted = [...keep].sort((a, b) => a - b);
  const out: (number | null)[] = [];
  for (const n of sorted) {
    const prev = out.length ? out[out.length - 1] : null;
    if (prev !== null && n - prev === 2) out.push(n - 1);
    else if (prev !== null && n - prev > 2) out.push(null);
    out.push(n);
  }
  return out;
}
