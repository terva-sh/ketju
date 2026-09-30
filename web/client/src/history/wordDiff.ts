// wordDiff compares two revisions of a post word by word (docs/08 "Post
// history"). It takes the markdown sources, never the HTML, and answers runs
// of text that are the same in both, deleted from the first, or inserted in
// the second. Concatenating the same and deleted runs gives back a; the same
// and inserted runs give back b.
//
// A token is a run of whitespace or a run of anything else, so punctuation
// stays with its word. The edit script is Myers' O(ND) greedy one over the
// tokens left once the common head and tail are set aside. It is hand-written
// rather than the `diff` package, which would weigh on the reader bundle for
// what fits here (TKT-01M29CP2Q3).

export type DiffOp = 'same' | 'del' | 'ins';
export interface DiffRun {
  op: DiffOp;
  text: string;
}

// maxEdits bounds the search. A source is at most 64 KB (docs/07), and the
// search costs time in proportion to the tokens times the edits, and memory
// in proportion to the edits squared. Two revisions further apart than this
// are a rewrite, and show as the whole middle deleted and the new one
// inserted: still a true account of the change, only not a word-level one.
export const maxEdits = 1000;

const tokens = (s: string) => s.match(/\s+|\S+/g) ?? [];

export function wordDiff(a: string, b: string): DiffRun[] {
  const x = tokens(a);
  const y = tokens(b);
  let head = 0;
  while (head < x.length && head < y.length && x[head] === y[head]) head++;
  let tail = 0;
  while (tail < x.length - head && tail < y.length - head && x[x.length - 1 - tail] === y[y.length - 1 - tail]) tail++;
  const xs = x.slice(head, x.length - tail);
  const ys = y.slice(head, y.length - tail);

  // Appended one at a time, never spread into a call: a 64 KB source can be
  // about 65,536 tokens, past what one call takes as arguments (Terva review
  // 1625).
  const ops: [DiffOp, string][] = x.slice(0, head).map((t) => ['same', t]);
  const middle = myers(xs, ys);
  if (middle) for (const op of middle) ops.push(op);
  else {
    for (const t of xs) ops.push(['del', t]);
    for (const t of ys) ops.push(['ins', t]);
  }
  for (const t of x.slice(x.length - tail)) ops.push(['same', t]);
  return runs(ops);
}

// myers answers the shortest edit script from a to b, token by token, or
// null when it needs more than maxEdits. It keeps, for each edit count d,
// the furthest point reached on each diagonal k in [-d, d], and walks those
// back from the end to recover the path.
export function myers(a: string[], b: string[]): [DiffOp, string][] | null {
  const n = a.length;
  const m = b.length;
  const limit = Math.min(n + m, maxEdits);
  const off = limit + 1;
  const v = new Int32Array(2 * limit + 3);
  const trace: Int32Array[] = [];
  let found = -1;
  for (let d = 0; d <= limit && found < 0; d++) {
    // What the walk back at d reads: diagonals -d-1 to d+1 before this step.
    trace.push(v.slice(off - d - 1, off + d + 2));
    for (let k = -d; k <= d; k += 2) {
      let i = k === -d || (k !== d && v[off + k - 1] < v[off + k + 1]) ? v[off + k + 1] : v[off + k - 1] + 1;
      let j = i - k;
      while (i < n && j < m && a[i] === b[j]) {
        i++;
        j++;
      }
      v[off + k] = i;
      if (i >= n && j >= m) {
        found = d;
        break;
      }
    }
  }
  if (found < 0) return null;

  const out: [DiffOp, string][] = [];
  let i = n;
  let j = m;
  for (let d = found; d >= 0; d--) {
    const t = trace[d];
    const at = (k: number) => t[k + d + 1];
    const k = i - j;
    // The step into diagonal k came down from k+1 (an insertion) or across
    // from k-1 (a deletion); at d = 0 there was none, and the path starts at
    // the origin.
    const prev = k === -d || (k !== d && at(k - 1) < at(k + 1)) ? k + 1 : k - 1;
    const pi = d === 0 ? 0 : at(prev);
    const pj = d === 0 ? 0 : pi - prev;
    while (i > pi && j > pj) {
      i--;
      j--;
      out.push(['same', a[i]]);
    }
    if (d > 0) {
      if (i === pi) out.push(['ins', b[--j]]);
      else out.push(['del', a[--i]]);
    }
  }
  return out.reverse();
}

// runs joins tokens into runs. Whitespace kept between a replacement and the
// next change is folded into both sides, so a phrase rewritten word for word
// reads as one deletion and one insertion rather than alternating fragments;
// and within each stretch of changes the deletion comes before the insertion.
function runs(ops: [DiffOp, string][]): DiffRun[] {
  const out: DiffRun[] = [];
  let del = '';
  let ins = '';
  const flush = () => {
    if (del) out.push({ op: 'del', text: del });
    if (ins) out.push({ op: 'ins', text: ins });
    del = ins = '';
  };
  for (let n = 0; n < ops.length; n++) {
    const [op, text] = ops[n];
    if (op === 'same') {
      const between = del && ins && /^\s+$/.test(text) && n + 1 < ops.length && ops[n + 1][0] !== 'same';
      if (between) {
        del += text;
        ins += text;
        continue;
      }
      flush();
      const last = out[out.length - 1];
      if (last?.op === 'same') last.text += text;
      else out.push({ op, text });
    } else if (op === 'del') del += text;
    else ins += text;
  }
  flush();
  return out;
}
