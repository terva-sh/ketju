// The composer toolbar's edits: each takes the text and its selection and
// returns them changed, so the textarea and the tests see the same thing.

export interface Edit {
  text: string;
  start: number;
  end: number;
}

export type EditKind = 'bold' | 'italic' | 'code' | 'quote' | 'link' | 'mention';

export function applyEdit({ text, start, end }: Edit, kind: EditKind): Edit {
  const sel = text.slice(start, end);
  const put = (insert: string, selStart: number, selEnd: number): Edit => ({
    text: text.slice(0, start) + insert + text.slice(end),
    start: start + selStart,
    end: start + selEnd,
  });
  switch (kind) {
    case 'bold':
      return put(`**${sel}**`, 2, 2 + sel.length);
    case 'italic':
      return put(`*${sel}*`, 1, 1 + sel.length);
    case 'code':
      // A selection across lines is a block; anything else is a span.
      if (sel.includes('\n')) {
        const lead = start === 0 || text[start - 1] === '\n' ? '' : '\n';
        return put(`${lead}\`\`\`\n${sel}\n\`\`\`\n`, lead.length + 4, lead.length + 4 + sel.length);
      }
      return put(`\`${sel}\``, 1, 1 + sel.length);
    case 'quote': {
      // Whole lines: a quotation cannot start or end inside one. A
      // selection that ends just after a line break ends on the line before
      // it, not on the next, none of which was selected (review 555).
      const last = end > start && text[end - 1] === '\n' ? end - 1 : end;
      const lineStart = text.lastIndexOf('\n', start - 1) + 1;
      const next = text.indexOf('\n', last);
      const lineEnd = next === -1 ? text.length : next;
      const block = text.slice(lineStart, lineEnd);
      const quoted = block
        .split('\n')
        .map((l) => (l === '' ? '>' : `> ${l}`))
        .join('\n');
      return {
        text: text.slice(0, lineStart) + quoted + text.slice(lineEnd),
        start: lineStart,
        end: lineStart + quoted.length,
      };
    }
    case 'link': {
      // The address is selected, so typing replaces it.
      const label = sel === '' ? 'text' : sel;
      const url = 'https://';
      return put(`[${label}](${url})`, label.length + 3, label.length + 3 + url.length);
    }
    case 'mention': {
      // An @ that reads as a mention: after a space unless at a line start.
      const lead = start === 0 || /\s/.test(text[start - 1]) ? '' : ' ';
      return put(`${lead}@`, lead.length + 1, lead.length + 1);
    }
  }
}
