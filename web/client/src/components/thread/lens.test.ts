import { describe, expect, it } from 'vitest';
import { heldSearch, lensHref, lensParam, lensQuery, lensTitle, placeGaps, tracksUnder, utcDay } from './lens';

// The lens helpers (TKT-01M29CTY71, docs/06 "Lenses", docs/07 "the lens
// parameter").

const t = { id: '0369k573g0001', slug: 'why-we-left-slack' };

describe('lensParam', () => {
  it('is kind alone with no value, and kind:value otherwise', () => {
    expect(lensParam('mentions', '')).toBe('mentions');
    expect(lensParam('mod', '')).toBe('mod');
    expect(lensParam('unread', '')).toBe('unread');
    expect(lensParam('unread', '60')).toBe('unread:60');
    expect(lensParam('since', '1756684800000')).toBe('since:1756684800000');
    expect(lensParam('author', 'mara')).toBe('author:mara');
  });
});

describe('lensHref', () => {
  it('is the thread address with the lens and no page or ordinal', () => {
    expect(lensHref(t)).toBe('/t/0369k573g0001/why-we-left-slack');
    expect(lensHref(t, 'author:mara')).toBe('/t/0369k573g0001/why-we-left-slack?lens=author:mara');
    expect(lensHref(t, 'chain:42')).toBe('/t/0369k573g0001/why-we-left-slack?lens=chain:42');
  });

  it('escapes what a query would misread, a + offset above all', () => {
    expect(lensQuery('since:2026-09-01T00:00:00+02:00')).toBe('since:2026-09-01T00:00:00%2B02:00');
    expect(new URLSearchParams(`lens=${lensQuery('since:2026-09-01T00:00:00+02:00')}`).get('lens')).toBe(
      'since:2026-09-01T00:00:00+02:00',
    );
    expect(lensQuery('author:a&b')).toBe('author:a%26b');
  });
});

describe('placeGaps', () => {
  it('puts each gap before the first post past it, and one past the last after it', () => {
    const { before, end } = placeGaps(
      [3, 5, 9],
      [
        { after: 0, count: 2 },
        { after: 3, count: 1 },
        { after: 5, count: 3 },
        { after: 9, count: 51 },
      ],
    );
    expect([...before].map(([o, g]) => [o, g.count])).toEqual([
      [3, 2],
      [5, 1],
      [9, 3],
    ]);
    expect(end).toEqual({ after: 9, count: 51 });
  });

  it('draws a gap that opens a later page before its first post, and none on an empty page', () => {
    const { before, end } = placeGaps([30, 31], [{ after: 25, count: 4 }]);
    expect([...before.keys()]).toEqual([30]);
    expect(end).toBeNull();
    const empty = placeGaps([], [{ after: 0, count: 60 }]);
    expect(empty.before.size).toBe(0);
    expect(empty.end).toBeNull();
  });
});

describe('tracksUnder', () => {
  it('runs on the canonical view and under no filtering lens, since included', () => {
    expect(tracksUnder(null, 0)).toBe(true);
    for (const [kind, value] of [
      ['author', 'mara'],
      ['chain', '3'],
      ['mentions', ''],
      ['mod', ''],
      ['since', '1756684800000'],
    ])
      expect(tracksUnder({ kind, value }, 1000), kind).toBe(false);
  });

  // Terva review 1741: only from a boundary at or below the reader's place.
  it('runs under unread:B only when B is at or below the position', () => {
    expect(tracksUnder({ kind: 'unread', value: '10' }, 10)).toBe(true);
    expect(tracksUnder({ kind: 'unread', value: '5' }, 10)).toBe(true);
    expect(tracksUnder({ kind: 'unread', value: '99' }, 10)).toBe(false);
    expect(tracksUnder({ kind: 'unread', value: '' }, 10)).toBe(false);
  });
});

describe('heldSearch', () => {
  it('holds a bare unread at the boundary the server answered, keeping the rest', () => {
    expect(heldSearch('?page=2&lens=unread', { kind: 'unread', value: '25' })).toBe('?page=2&lens=unread:25');
    expect(heldSearch('?lens=unread', { kind: 'unread', value: '0' })).toBe('?lens=unread:0');
  });

  it('drops a lens the server answered as the full thread', () => {
    expect(heldSearch('?page=2&lens=nope', null)).toBe('?page=2');
    expect(heldSearch('?lens=nope', null)).toBe('');
  });

  it('leaves everything else as written', () => {
    expect(heldSearch('?page=2', null)).toBeNull();
    expect(heldSearch('?lens=unread:25', { kind: 'unread', value: '25' })).toBeNull();
    expect(heldSearch('?lens=since:2026-09-01', { kind: 'since', value: '1756684800000' })).toBeNull();
    // An anonymous reader's bare unread has no boundary to hold.
    expect(heldSearch('?lens=unread', { kind: 'unread', value: '' })).toBeNull();
  });
});

describe('lensTitle', () => {
  it('says what each lens filters', () => {
    expect(lensTitle({ kind: 'unread', value: '60' })).toBe('Unread after #60');
    expect(lensTitle({ kind: 'author', value: 'mara' })).toBe('Posts by mara');
    expect(lensTitle({ kind: 'chain', value: '42' })).toBe('Conversation around #42');
    expect(lensTitle({ kind: 'mentions', value: '' })).toBe('Mentioning you');
    expect(lensTitle({ kind: 'mod', value: '' })).toBe('Removed posts');
  });
});

describe('utcDay', () => {
  it('is the UTC day of a moment, and nothing for a value that is not one', () => {
    expect(utcDay(String(Date.UTC(2026, 8, 1)))).toBe('2026-09-01');
    expect(utcDay(String(Date.UTC(2026, 8, 1) - 1))).toBe('2026-08-31');
    expect(utcDay('x')).toBe('');
    expect(utcDay('9'.repeat(30))).toBe('');
  });
});
