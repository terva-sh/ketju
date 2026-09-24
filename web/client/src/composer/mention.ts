// Finding the @handle being typed, by the rules internal/render/mention.go
// reads a mention with, so autocomplete offers itself exactly where the
// renderer would see a mention and nowhere else.

// handleByte is what the mention parser reads as part of a handle.
const handleByte = (c: string | undefined) => c !== undefined && /^[A-Za-z0-9_.-]$/.test(c);

// maxHandle is docs/04's longest handle: past it no handle can match.
const maxHandle = 30;

export interface MentionAt {
  // start is the index of the @.
  start: number;
  // typed is what follows the @ up to the caret: the prefix to look up.
  typed: string;
}

// mentionAt is the mention the caret is at the end of, or null.
//
// Null when the @ follows a handle byte (the parser's rule for an e-mail
// address, which is not a mention), when a handle byte follows the caret
// (inserting a handle there would leave the rest of the word after it),
// when nothing has been typed after the @ yet, when what is typed holds a
// '.' (the parser reads a trailing one as punctuation, and no handle has
// one), and when it is longer than any handle.
export function mentionAt(text: string, caret: number): MentionAt | null {
  if (handleByte(text[caret])) return null;
  let i = caret;
  while (i > 0 && handleByte(text[i - 1])) i--;
  if (i === 0 || text[i - 1] !== '@') return null;
  const start = i - 1;
  if (handleByte(text[start - 1])) return null;
  const typed = text.slice(i, caret);
  if (typed === '' || typed.includes('.') || typed.length > maxHandle) return null;
  return { start, typed };
}

// insertMention replaces the mention m, ending at caret, with the handle as
// the server spells it and a space, and returns the new text and caret.
// The space is not doubled when one already follows.
export function insertMention(text: string, m: MentionAt, caret: number, handle: string): { text: string; caret: number } {
  const after = text.slice(caret);
  const head = text.slice(0, m.start) + '@' + handle + ' ';
  return { text: head + (after.startsWith(' ') ? after.slice(1) : after), caret: head.length };
}
