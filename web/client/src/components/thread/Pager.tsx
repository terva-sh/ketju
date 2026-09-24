import type { Pagination } from '../../api/dto';
import type { ThreadRef } from './Post';

// Pager moves between a thread's pages (docs/06 "Paged, not infinite"). The
// desktop row numbers the pages around the current one; the phone row is
// "Page N of M" between previous and next. Both are rendered and CSS shows
// one, at the 720 px breakpoint. Every page is a link, so it can be opened in
// a new tab and the router follows it in this one; a link keeps the lens and
// leaves any permalink, since the page is now the reader's choice.
export function Pager({
  page,
  thread,
  lens,
  postCount,
}: {
  page: Pagination;
  thread: ThreadRef;
  lens: string;
  postCount: number;
}) {
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
        <span class="where">
          Page {page.number} of {page.total_pages}
        </span>
        <PageStep to={next} href={href} label="Next page" text="Next ›" />
      </div>
    </nav>
  );
}

function PageStep({ to, href, label, text }: { to: number | null; href: (n: number) => string; label: string; text: string }) {
  return to === null ? (
    <span class="step disabled" aria-hidden="true">
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
export function pageHref(thread: ThreadRef, n: number, lens: string): string {
  const q = new URLSearchParams();
  q.set('page', String(n));
  if (lens) q.set('lens', lens);
  const s = q.toString();
  return `/t/${encodeURIComponent(thread.id)}/${encodeURIComponent(thread.slug)}${s ? '?' + s : ''}`;
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
