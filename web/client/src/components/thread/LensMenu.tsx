import { useState } from 'preact/hooks';
import { useLocation } from 'preact-iso';
import type { ThreadPage } from '../../api/dto';
import { sameHandle } from '../../shell/nav';
import { viewer } from '../../state/viewer';
import { lensHref, utcDay } from './lens';

// LensMenu is the switcher's list (LensBar): the full thread; unread and
// mentioning me for a signed-in reader; by author for each author on the
// page; since a date; and removed posts for a viewer who moderates the
// forum (docs/06 "the lens bar"). Every item changes only the lens and drops
// the page and the ordinal (lensHref). active is the lens on screen, as
// lensParam writes it, and its item is marked current.
//
// The date field's day goes into the address as chosen, since:YYYY-MM-DD,
// which the server reads as midnight UTC (docs/07). The client never turns
// it into a local moment, so the field and a shared date link select the
// same posts whatever the viewer's timezone, and the field shows a since
// lens's moment as its UTC day for the same reason.
export function LensMenu({ page, active, close }: { page: ThreadPage; active: string; close: () => void }) {
  const { route } = useLocation();
  const t = page.thread;
  const signedIn = !!viewer.value.user;
  const [day, setDay] = useState(active.startsWith('since:') ? utcDay(active.slice(6)) : '');
  const authors: string[] = [];
  for (const p of page.posts) {
    const h = p.author.handle;
    if (!p.deleted && p.moved_to === null && !authors.some((a) => sameHandle(a, h))) authors.push(h);
  }
  const items: [string, string][] = [
    ['', 'Full thread'],
    ...(signedIn ? [['unread', 'Unread'] as [string, string]] : []),
    ...authors.map((h) => [`author:${h}`, `By ${h}`] as [string, string]),
    ...(signedIn ? [['mentions', 'Mentioning me'] as [string, string]] : []),
    ...(page.viewer.can_moderate ? [['mod', 'Removed posts'] as [string, string]] : []),
  ];
  // A held unread:B is the unread item's, and an author's handle matches in
  // any case.
  const current = (lens: string) =>
    lens === active ||
    (lens === 'unread' && active.startsWith('unread:')) ||
    (lens.startsWith('author:') && active.startsWith('author:') && sameHandle(lens.slice(7), active.slice(7)));
  return (
    <ul id="lens-menu" class="lens-menu">
      {items.map(([lens, label]) => (
        <li key={lens}>
          <a href={lensHref(t, lens)} aria-current={current(lens) ? 'true' : undefined} data-lens-item={lens} onClick={close}>
            {label}
          </a>
        </li>
      ))}
      <li>
        <form
          class="lens-since"
          onSubmit={(e) => {
            e.preventDefault();
            if (!/^[0-9]{4}-[0-9]{2}-[0-9]{2}$/.test(day)) return;
            close();
            route(lensHref(t, `since:${day}`));
          }}
        >
          <label for="lens-since-day">Since</label>
          <input
            id="lens-since-day"
            type="date"
            required
            value={day}
            onInput={(e) => setDay((e.currentTarget as HTMLInputElement).value)}
          />
          <button type="submit">Go</button>
        </form>
      </li>
    </ul>
  );
}
