import { describe, expect, it } from 'vitest';
import { applyEdit, type EditKind } from './edit';

// ed runs an edit on "text" with the selection between [ and ], and returns
// the result written the same way.
function ed(s: string, kind: EditKind): string {
  const start = s.indexOf('[');
  const end = s.indexOf(']') - 1;
  const r = applyEdit({ text: s.replace('[', '').replace(']', ''), start, end }, kind);
  return r.text.slice(0, r.start) + '[' + r.text.slice(r.start, r.end) + ']' + r.text.slice(r.end);
}

describe('the toolbar edits', () => {
  it('wraps the selection, or leaves the caret between the marks', () => {
    expect(ed('a [word] b', 'bold')).toBe('a **[word]** b');
    expect(ed('a []b', 'bold')).toBe('a **[]**b');
    expect(ed('a [word] b', 'italic')).toBe('a *[word]* b');
    expect(ed('a [word] b', 'code')).toBe('a `[word]` b');
  });

  it('makes a block of code across lines', () => {
    expect(ed('x\n[a\nb]', 'code')).toBe('x\n```\n[a\nb]\n```\n');
    expect(ed('x [a\nb]', 'code')).toBe('x \n```\n[a\nb]\n```\n');
  });

  it('quotes every line the selection touches', () => {
    expect(ed('one\nt[wo\n\nthr]ee\nfour', 'quote')).toBe('one\n[> two\n>\n> three]\nfour');
    expect(ed('a[]b', 'quote')).toBe('[> ab]');
  });

  it('writes a link with its address selected to type over', () => {
    expect(ed('see [this]', 'link')).toBe('see [this]([https://])');
    expect(ed('[]', 'link')).toBe('[text]([https://])');
  });

  it('starts a mention where the parser will read one', () => {
    expect(ed('hi []', 'mention')).toBe('hi @[]');
    expect(ed('hi[]', 'mention')).toBe('hi @[]');
    expect(ed('[]', 'mention')).toBe('@[]');
  });
});
