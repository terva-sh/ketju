import type { Lens, LensGap } from '../../api/dto';
import type { ThreadRef } from './Post';

// The lens helpers (docs/06 "Lenses", docs/07 "the lens parameter",
// TKT-01M29CTY71). A lens is URL state: every switch is an address, and
// nothing here holds one anywhere else.

// lensParam is the lens an answer names, in the form that opens it again:
// kind alone when value is empty, as for mentions, mod and an anonymous
// viewer's bare unread, and kind:value otherwise (docs/07).
export const lensParam = (kind: string, value: string) => (value ? `${kind}:${value}` : kind);

// lensQuery writes a lens into a query string. The colon stays as it is, so
// the address reads ?lens=author:mara, and everything else is escaped: a
// since moment's + offset must go as %2B, or the server reads a space and
// the value is no lens (docs/07).
export const lensQuery = (lens: string) => encodeURIComponent(lens).replace(/%3A/gi, ':');

// lensHref is the thread's own address under lens, or with none, the
// thread's own address. It names no page and no ordinal: a switch starts the
// lens from its first page, and the bare address lands a signed-in reader
// where they left off (docs/06 "Landing on return").
export function lensHref(t: ThreadRef, lens = ''): string {
  const base = `/t/${encodeURIComponent(t.id)}/${encodeURIComponent(t.slug)}`;
  return lens ? `${base}?lens=${lensQuery(lens)}` : base;
}

// placeGaps places the gaps a lens page reports among its posts' ordinals:
// each gap goes before the first post past it, and a gap past the last post
// goes after it (end). A page with no posts draws none: its empty state
// says what there is instead.
export function placeGaps(ordinals: number[], gaps: LensGap[]): { before: Map<number, LensGap>; end: LensGap | null } {
  const before = new Map<number, LensGap>();
  let end: LensGap | null = null;
  if (ordinals.length === 0) return { before, end };
  for (const g of gaps) {
    const o = ordinals.find((n) => n > g.after);
    if (o === undefined) end = g;
    else if (!before.has(o)) before.set(o, g);
  }
  return { before, end };
}

// tracksUnder is whether the read beacon runs under lens, null for the
// canonical view, for a reader at position. It runs on the canonical view
// and under the unread lens alone. Every other lens skips posts, and the
// beacon sends the highest post seen, so it would mark what the lens skipped
// as read; since does too, across the gap before its first post (docs/06
// "read tracking under a lens", Terva review 1730). Under unread:B it runs
// only when B is at or below the reader's position, so the lens starts at or
// before their first unread post: a shared or typed unread:99 opened by a
// reader at #10 would otherwise mark #11 to #98 read unseen (Terva review
// 1741).
export function tracksUnder(lens: Pick<Lens, 'kind' | 'value'> | null, position: number): boolean {
  if (lens === null) return true;
  return lens.kind === 'unread' && /^[0-9]+$/.test(lens.value) && Number(lens.value) <= position;
}

// lensTitle is what a lens is filtering, for its header.
export function lensTitle(lens: Pick<Lens, 'kind' | 'value'>): string {
  switch (lens.kind) {
    case 'unread':
      return lens.value ? `Unread after #${lens.value}` : 'Unread';
    case 'author':
      return `Posts by ${lens.value}`;
    case 'chain':
      return `Conversation around #${lens.value}`;
    case 'since':
      return 'Since';
    case 'mentions':
      return 'Mentioning you';
    case 'mod':
      return 'Removed posts';
  }
  return lens.kind;
}

// heldSearch is the address's query once it follows what the server
// answered for its lens, or null when it already does. A bare unread the
// server resolved to unread:B becomes unread:B, so paging, reload and Back
// show the same posts while reading moves the position (docs/06 "the unread
// lens holds its boundary"). A lens the server answered as the full thread,
// lens null, leaves the address, so the address stays the view (docs/07).
// The page and anything else in it stay as written.
export function heldSearch(search: string, answer: Pick<Lens, 'kind' | 'value'> | null): string | null {
  const q = new URLSearchParams(search);
  const asked = q.get('lens');
  if (asked === null) return null;
  if (answer === null) q.delete('lens');
  else if (asked === 'unread' && answer.kind === 'unread' && answer.value) q.set('lens', `unread:${answer.value}`);
  else return null;
  const s = q.toString().replace(/(^|&)(lens=[^&]*)/, (_, a: string, l: string) => a + l.replace(/%3A/gi, ':'));
  return s ? `?${s}` : '';
}

// utcDay is the day a since lens's moment (Unix ms) falls on in UTC, as
// YYYY-MM-DD: the date the field shows for it. A chosen day goes into the
// address as written and the server reads it as midnight UTC (docs/07), so
// the field and a shared link select the same posts in every timezone.
export function utcDay(ms: string): string {
  const n = Number(ms);
  return /^[0-9]+$/.test(ms) && n <= 8.64e15 ? new Date(n).toISOString().slice(0, 10) : '';
}
