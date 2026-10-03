// A thread's tags as a reader types them, and what the server says about
// them (TKT-01M3YSQPZX): the new thread screen's Tags field and a
// moderator's Edit tags panel share both.

// maxTags and maxTagLength are the server's bounds (internal/domain tags.go
// MaxTags and MaxTagLen).
const maxTags = 8;
const maxTagLength = 24;

// splitTags is a Tags field's text as the list sent: tags are separated by
// spaces or commas, and an empty one between two separators is dropped. The
// server folds, checks and sorts them, so they go as typed.
export function splitTags(text: string): string[] {
  return text.split(/[\s,]+/).filter((t) => t !== '');
}

// foldTag is a tag as the server folds it before checking it: ASCII A-Z
// only (internal/domain NormalizeTag), so a character that folds to ASCII
// only through Unicode case rules, such as the Kelvin sign, stays as it is
// and is the server's to refuse (Terva review 2049).
export function foldTag(tag: string): string {
  return tag.replace(/[A-Z]/g, (c) => c.toLowerCase());
}

// tagMessages says, for each tag a validation-failed answer refused, which
// one and why, in the order sent, then the count if there were too many
// (docs/07 `POST /forums/{slug}/threads`: `tags.N` is the Nth tag sent).
// sent is the list the answer is to. The same words are in
// forms/messages.ts, without the tag's name, for the codes table; that file
// is in the apply chunk and this one in the reader's.
export function tagMessages(fields: Record<string, string>, sent: string[]): string[] {
  const out: [number, string][] = [];
  for (const [field, code] of Object.entries(fields)) {
    const m = /^tags\.(\d+)$/.exec(field);
    if (m) out.push([Number(m[1]), tagMessage(code, sent[Number(m[1])] ?? '')]);
  }
  out.sort((a, b) => a[0] - b[0]);
  const words = out.map(([, w]) => w);
  if (fields.tags === 'too_many') words.push(`A thread can have at most ${maxTags} tags.`);
  else if ('tags' in fields) words.push('The tags could not be read.');
  return words;
}

function tagMessage(code: string, tag: string): string {
  const name = `“${tag}”`;
  switch (code) {
    case 'invalid_tag':
      return tag.trim() === ''
        ? 'A tag cannot be empty.'
        : `${name} is not a tag: use letters, digits and single hyphens between them.`;
    case 'too_long':
      return `${name} is longer than ${maxTagLength} characters.`;
    case 'reserved_tag':
      return `${name} is reserved for moderators and agent accounts.`;
    default:
      return `${name} was refused.`;
  }
}

// hasTagErrors is whether a validation-failed answer named the tags.
export function hasTagErrors(fields: Record<string, string>): boolean {
  return Object.keys(fields).some((f) => f === 'tags' || f.startsWith('tags.'));
}
