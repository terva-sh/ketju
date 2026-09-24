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
//
// It reads the rendered blocks the way a reader sees them (review 541): a
// <br> is a line break, a list item or a table row a line, a table's cells
// are separated by " | ", code keeps its lines as they are, and whitespace
// elsewhere collapses as it does on screen. textContent alone runs lines
// joined by a <br> together, and every item of a loose list into one.
export function bodyText(body: Element): string {
  // On a copy, every quotation removed wherever it sits, a list item's
  // included (review 537), so no block below can carry one.
  const copy = body.cloneNode(true) as Element;
  for (const q of Array.from(copy.querySelectorAll('details.quote'))) q.remove();
  const blocks: string[] = [];
  collect(copy, blocks);
  return blocks.join('\n\n');
}

// The elements that start a block of their own.
const blockTag = /^(P|H[1-6]|PRE|UL|OL|TABLE|HR|BLOCKQUOTE|DIV|SECTION|DETAILS)$/;

// collect appends the blocks inside el to blocks, each one non-empty.
function collect(el: Element, blocks: string[]) {
  let inline = '';
  const flush = () => {
    const t = tidy(inline);
    if (t !== '') blocks.push(t);
    inline = '';
  };
  for (const n of Array.from(el.childNodes)) {
    if (!(n instanceof Element) || !blockTag.test(n.tagName)) {
      inline += inlineText(n);
      continue;
    }
    flush();
    let text: string;
    switch (n.tagName) {
      case 'PRE':
        text = (n.textContent ?? '').replace(/\n+$/, '');
        break;
      case 'UL':
      case 'OL':
        text = Array.from(n.children)
          .map((li) => within(li, '\n'))
          .filter((t) => t !== '')
          .join('\n');
        break;
      case 'TABLE':
        text = Array.from(n.querySelectorAll('tr'))
          .map((tr) => Array.from(tr.children, (cell) => within(cell, ' ')).join(' | '))
          .join('\n');
        break;
      case 'HR':
        text = '';
        break;
      default:
        if (/^(P|H[1-6])$/.test(n.tagName)) {
          text = tidy(inlineText(n));
        } else {
          // A container: its blocks are blocks here.
          collect(n, blocks);
          text = '';
        }
    }
    if (text !== '') blocks.push(text);
  }
  flush();
}

// within is el's blocks joined by sep: a list item's paragraphs and nested
// lists on lines of their own, a table cell's on one line.
function within(el: Element, sep: string): string {
  const parts: string[] = [];
  collect(el, parts);
  return parts.join(sep);
}

// inlineText is the text of an inline run, with a <br> as a line break and
// every other run of whitespace, a soft line break included, as a space.
function inlineText(n: Node): string {
  if (n instanceof Element) {
    if (n.tagName === 'BR') return '\n';
    return Array.from(n.childNodes, inlineText).join('');
  }
  return (n.textContent ?? '').replace(/\s+/g, ' ');
}

// tidy trims each line of an inline run and the run itself.
function tidy(s: string): string {
  return s
    .split('\n')
    .map((l) => l.trim())
    .join('\n')
    .trim();
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
