import { describe, expect, it } from 'vitest';
import { maxEdits, myers, wordDiff, type DiffRun } from './wordDiff';

const side = (runs: DiffRun[], keep: 'del' | 'ins') =>
  runs
    .filter((r) => r.op === 'same' || r.op === keep)
    .map((r) => r.text)
    .join('');

// Each side of a diff gives back its source: nothing is lost or invented.
function expectFaithful(a: string, b: string, runs = wordDiff(a, b)) {
  expect(side(runs, 'del')).toBe(a);
  expect(side(runs, 'ins')).toBe(b);
  return runs;
}

describe('wordDiff', () => {
  it('marks a one-word change inside a long line as one deleted and one inserted word', () => {
    const words = Array.from({ length: 60 }, (_, i) => `word${i}`);
    const a = words.join(' ') + '.';
    const b = a.replace('word31', 'changed');
    const runs = expectFaithful(a, b);
    expect(runs.filter((r) => r.op === 'del')).toEqual([{ op: 'del', text: 'word31' }]);
    expect(runs.filter((r) => r.op === 'ins')).toEqual([{ op: 'ins', text: 'changed' }]);
    expect(runs.map((r) => r.op)).toEqual(['same', 'del', 'ins', 'same']);
  });

  it('keeps punctuation with its word', () => {
    const runs = expectFaithful('The bill was the *smaller* half of it.', 'The bill was the **smaller** half of it.');
    expect(runs).toEqual([
      { op: 'same', text: 'The bill was the ' },
      { op: 'del', text: '*smaller*' },
      { op: 'ins', text: '**smaller**' },
      { op: 'same', text: ' half of it.' },
    ]);
  });

  it('answers a pure insertion', () => {
    const runs = expectFaithful('one three', 'one two three');
    expect(runs.filter((r) => r.op !== 'same')).toEqual([{ op: 'ins', text: 'two ' }]);
  });

  it('answers a pure deletion', () => {
    const runs = expectFaithful('one two three', 'one three');
    expect(runs.filter((r) => r.op !== 'same')).toEqual([{ op: 'del', text: 'two ' }]);
  });

  it('answers identical sources as one unchanged run', () => {
    expect(wordDiff('the same words\nand lines', 'the same words\nand lines')).toEqual([
      { op: 'same', text: 'the same words\nand lines' },
    ]);
    expect(wordDiff('', '')).toEqual([]);
  });

  it('answers a whitespace-only change as a change of that whitespace', () => {
    const runs = expectFaithful('one two', 'one\n\ntwo');
    expect(runs).toEqual([
      { op: 'same', text: 'one' },
      { op: 'del', text: ' ' },
      { op: 'ins', text: '\n\n' },
      { op: 'same', text: 'two' },
    ]);
  });

  it('reads a phrase rewritten word for word as one deletion and one insertion', () => {
    const runs = expectFaithful('we met on monday at noon', 'we met on friday after lunch');
    expect(runs).toEqual([
      { op: 'same', text: 'we met on ' },
      { op: 'del', text: 'monday at noon' },
      { op: 'ins', text: 'friday after lunch' },
    ]);
  });

  it('answers from and to an empty source', () => {
    expect(expectFaithful('', 'new words')).toEqual([{ op: 'ins', text: 'new words' }]);
    expect(expectFaithful('old words', '')).toEqual([{ op: 'del', text: 'old words' }]);
  });

  // The edit script is a shortest one: as many edits as the tokens outside
  // a longest common subsequence, checked against the textbook table.
  it('finds a shortest edit script', () => {
    let seed = 7;
    const rand = (n: number) => {
      seed = (seed * 1103515245 + 12345) >>> 0;
      return seed % n;
    };
    const alphabet = ['a', 'b', 'c', 'd', ' '];
    const pick = () => Array.from({ length: rand(14) }, () => alphabet[rand(alphabet.length)]);
    for (let trial = 0; trial < 300; trial++) {
      const a = pick();
      const b = pick();
      const script = myers(a, b)!;
      expect(script.filter(([op]) => op !== 'ins').map(([, t]) => t)).toEqual(a);
      expect(script.filter(([op]) => op !== 'del').map(([, t]) => t)).toEqual(b);
      const edits = script.filter(([op]) => op !== 'same').length;
      expect(edits).toBe(a.length + b.length - 2 * lcs(a, b));
    }
  });

  it('shows revisions too far apart to compare word by word as a rewrite', () => {
    const a = Array.from({ length: maxEdits }, (_, i) => `a${i}`);
    const b = Array.from({ length: maxEdits }, (_, i) => `b${i}`);
    expect(myers(a, b)).toBeNull();
    const runs = expectFaithful(`start ${a.join(' ')} end`, `start ${b.join(' ')} end`);
    expect(runs.map((r) => r.op)).toEqual(['same', 'del', 'ins', 'same']);
  });

  // The most tokens a 64 KB source can hold is one-character words between
  // single spaces, about 65,536. A rewrite of one into another, and an edit
  // at each end of one, must not hand that many tokens to a single call as
  // arguments, which throws a RangeError (Terva review 1625).
  it('compares 64 KB sources of one-character words, rewritten or edited at both ends', () => {
    const rewrite = ['a '.repeat(32768), 'b '.repeat(32768)] as const;
    expectFaithful(...rewrite);
    const same = 'x '.repeat(32000);
    const ends = expectFaithful(`A ${same}B`, `C ${same}D`);
    expect(ends.filter((r) => r.op !== 'same').map((r) => r.text)).toEqual(['A', 'C', 'B', 'D']);
  });

  // Sources are at most 64 KB (docs/07). Two of them, edited in scattered
  // places or rewritten outright, compare within half a second.
  it('compares two 64 KB sources in under half a second', () => {
    const para = 'Sixty-four kilobytes of a long post, written over an evening and edited twice. ';
    const a = para.repeat(Math.floor(65536 / para.length));
    const words = a.split(' ');
    for (let i = 0; i < words.length; i += 97) words[i] = 'EDITED';
    const b = words.join(' ');
    const rewrite = 'Something else entirely, said again and again. '.repeat(Math.floor(65536 / 48));
    expect(a.length).toBeGreaterThan(65000);

    for (const [x, y] of [
      [a, b],
      [a, rewrite],
    ]) {
      const t = performance.now();
      const runs = wordDiff(x, y);
      expect(performance.now() - t).toBeLessThan(500);
      expectFaithful(x, y, runs);
    }
  });
});

function lcs(a: string[], b: string[]): number {
  const row = new Array(b.length + 1).fill(0);
  for (let i = 1; i <= a.length; i++) {
    let diag = 0;
    for (let j = 1; j <= b.length; j++) {
      const up = row[j];
      row[j] = a[i - 1] === b[j - 1] ? diag + 1 : Math.max(row[j], row[j - 1]);
      diag = up;
    }
  }
  return row[b.length];
}
