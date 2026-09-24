// Quoting a post (docs/06 "Composer"). The markdown is the form
// internal/render/quote.go reads: a blockquote whose first line is the
// attribution [quote=#N], so a renderer that does not know the syntax still
// shows a quotation.

// quoteMarkdown is text quoted from post ordinal. Line endings are
// normalised, blank lines at either end and spaces at line ends dropped,
// and a blank line inside
// stays in the quotation as a bare >, so a quoted paragraph break is still
// one quotation rather than two.
export function quoteMarkdown(ordinal: number, text: string): string {
  const lines = text.replace(/\r\n?/g, '\n').split('\n');
  while (lines.length > 0 && lines[0].trim() === '') lines.shift();
  while (lines.length > 0 && lines[lines.length - 1].trim() === '') lines.pop();
  // Trailing spaces are dropped: selected text ends where the pointer let
  // go, often after a space, and two of them are a hard break in markdown.
  return [`> [quote=#${ordinal}]`, ...lines.map((l) => (l.trim() === '' ? '>' : `> ${l.trimEnd()}`))].join('\n');
}

// selectionIn is the text selected inside body, or null when nothing is, or
// when the selection reaches outside it: a selection that starts in one post
// and ends in another is not a quotation of either.
export function selectionIn(body: Element): string | null {
  const sel = getSelection();
  if (!sel || sel.isCollapsed || sel.rangeCount === 0) return null;
  if (!sel.anchorNode || !sel.focusNode || !body.contains(sel.anchorNode) || !body.contains(sel.focusNode)) return null;
  const text = sel.toString();
  return text.trim() === '' ? null : text;
}

// bodyText is a whole post body as plain text, one block per paragraph, for
// a quote with nothing selected. The post's own quotations are left out:
// they are somebody else's words, and quoting them again would attribute
// them to this post.
export function bodyText(body: Element): string {
  const blocks: string[] = [];
  for (const node of Array.from(body.childNodes)) {
    if (node instanceof Element) {
      if (node.matches('details.quote')) continue;
      if (node.matches('ul, ol')) {
        const items = Array.from(node.children).map((li) => (li.textContent ?? '').trim());
        blocks.push(items.filter((s) => s !== '').join('\n'));
        continue;
      }
      if (node.matches('pre')) {
        blocks.push((node.textContent ?? '').replace(/\n+$/, ''));
        continue;
      }
    }
    const text = (node.textContent ?? '').trim();
    if (text !== '') blocks.push(text);
  }
  return blocks.filter((b) => b !== '').join('\n\n');
}

// insertBlock puts block into source at caret as a paragraph of its own,
// with a blank line on each side, and returns the new source and where the
// caret goes: on the empty line after it, ready for the reply.
export function insertBlock(source: string, caret: number, block: string): { source: string; caret: number } {
  const at = Math.max(0, Math.min(caret, source.length));
  const before = source.slice(0, at).replace(/[ \t]+$/, '');
  const after = source.slice(at).replace(/^[ \t]*\n*/, '');
  const lead = before === '' || before.endsWith('\n\n') ? '' : before.endsWith('\n') ? '\n' : '\n\n';
  const head = before + lead + block + '\n\n';
  return { source: head + after, caret: head.length };
}
