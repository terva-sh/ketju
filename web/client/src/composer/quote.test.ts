import { afterEach, describe, expect, it } from 'vitest';
import { bodyText, continueQuote, escapeText, insertBlock, quoteMarkdown, selectionIn } from './quote';

// AC2's markdown: the form internal/render/quote.go reads.
describe('quoteMarkdown', () => {
  it('puts the attribution on the first line of the quotation', () => {
    expect(quoteMarkdown(37, 'the old board did this')).toBe('> [quote=#37]\n> the old board did this');
  });

  it('keeps every line, a blank one as a bare >, and normalises line endings', () => {
    expect(quoteMarkdown(4, 'one\r\ntwo\n\nthree\rfour')).toBe('> [quote=#4]\n> one\n> two\n>\n> three\n> four');
  });

  it('drops the spaces a selection ends lines with, and keeps leading ones', () => {
    expect(quoteMarkdown(5, 'the monthly \n  indented  ')).toBe('> [quote=#5]\n> the monthly\n>   indented');
  });

  it('drops blank lines at either end, and only there', () => {
    expect(quoteMarkdown(9, '\n  \nmiddle\n\nend\n \n')).toBe('> [quote=#9]\n> middle\n>\n> end');
  });

  it('is the attribution alone for nothing', () => {
    expect(quoteMarkdown(2, ' \n')).toBe('> [quote=#2]');
  });
});

describe('insertBlock', () => {
  it('into nothing, leaves the caret on a fresh paragraph', () => {
    expect(insertBlock('', 0, '> q')).toEqual({ source: '> q\n\n', caret: 5 });
  });

  it('after text, separates it by a blank line', () => {
    expect(insertBlock('I think', 7, '> q')).toEqual({ source: 'I think\n\n> q\n\n', caret: 14 });
    expect(insertBlock('I think\n', 8, '> q')).toEqual({ source: 'I think\n\n> q\n\n', caret: 14 });
    expect(insertBlock('I think\n\n', 9, '> q')).toEqual({ source: 'I think\n\n> q\n\n', caret: 14 });
  });

  it('in the middle, keeps what follows as its own paragraph', () => {
    const r = insertBlock('before after', 6, '> q');
    expect(r.source).toBe('before\n\n> q\n\nafter');
    expect(r.source.slice(r.caret)).toBe('after');
  });

  it('clamps a caret past the end', () => {
    expect(insertBlock('x', 99, '> q').source).toBe('x\n\n> q\n\n');
  });
});

describe('the text of a post', () => {
  afterEach(() => getSelection()?.removeAllRanges());

  const body = (html: string) => {
    const el = document.createElement('div');
    el.className = 'post-body';
    el.innerHTML = html;
    document.body.replaceChildren(el);
    return el;
  };

  it('is its blocks, without the quotations it holds', () => {
    const el = body(
      '<details class="quote" open=""><summary>Quote from #3</summary>' +
        '<blockquote><p class="quote-attribution"><a href="#post-3" class="quote-source">Post #3</a></p><p>their words</p></blockquote></details>\n' +
        '<p>My <em>first</em> point.</p>\n<ul><li>one</li><li>two</li></ul>\n<pre><code>x := 1\n</code></pre>',
    );
    expect(bodyText(el)).toBe('My first point.\n\none\ntwo\n\nx := 1');
  });

  it('reads a post as a reader sees it: line breaks, loose and nested lists, tables, headings (review 541)', () => {
    const el = body(
      '<h2>Plan</h2>\n<p>first line<br>\nsecond   line\nsoft</p>\n' +
        '<ul>\n<li>\n<p>loose one</p>\n<p>and more</p>\n</li>\n<li>two\n<ul>\n<li>nested</li>\n</ul>\n</li>\n</ul>\n' +
        '<table>\n<thead>\n<tr>\n<th>a</th>\n<th>b</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n<td>1</td>\n<td><em>2</em></td>\n</tr>\n</tbody>\n</table>\n' +
        '<blockquote>\n<p>a plain quote</p>\n</blockquote>\n<hr>\n<pre><code>  indented\n\nkept\n</code></pre>\n' +
        '<section class="footnotes">\n<hr>\n<ol>\n<li id="fn:1">\n<p>a note</p>\n</li>\n</ol>\n</section>',
    );
    expect(bodyText(el)).toBe(
      'Plan\n\nfirst line\nsecond line soft\n\nloose one\nand more\ntwo\nnested\n\na | b\n1 | 2\n\na plain quote\n\n  indented\n\nkept\n\na note',
    );
  });

  it('leaves out a quotation wherever it sits, and leaves the body as it was', () => {
    const quote = (n: number, words: string) =>
      `<details class="quote" open=""><summary>Quote from #${n}</summary>` +
      `<blockquote><p class="quote-attribution"><a href="#post-${n}" class="quote-source">Post #${n}</a></p><p>${words}</p></blockquote></details>`;
    const el = body(`<ul><li>${quote(3, 'their words')}mine</li><li>also mine</li></ul><div>${quote(4, 'more of theirs')}<p>and mine</p></div>`);
    expect(bodyText(el)).toBe('mine\nalso mine\n\nand mine');
    expect(el.querySelectorAll('details.quote')).toHaveLength(2);
  });

  it('is the selection when it lies inside the body, and nothing when it strays out', () => {
    const el = body('<p>alpha beta gamma</p>');
    const outside = document.createElement('p');
    outside.textContent = 'elsewhere';
    document.body.append(outside);
    const text = el.querySelector('p')!.firstChild!;

    expect(selectionIn(el)).toBeNull();
    const range = document.createRange();
    range.setStart(text, 6);
    range.setEnd(text, 10);
    getSelection()!.addRange(range);
    expect(selectionIn(el)).toBe('beta');

    getSelection()!.removeAllRanges();
    const across = document.createRange();
    across.setStart(text, 6);
    across.setEnd(outside.firstChild!, 4);
    getSelection()!.addRange(across);
    expect(selectionIn(el)).toBeNull();

    // Made backwards, from outside in: the anchor is the end outside.
    getSelection()!.removeAllRanges();
    getSelection()!.setBaseAndExtent(outside.firstChild!, 4, text, 6);
    expect(selectionIn(el)).toBeNull();
  });
});

// Reply as a new thread's pre-fill (TKT-01M29CYSC0): the member's own text,
// a plain blockquote with no attribution syntax, read from the rendered post
// as quote reads a body, its own quotations left out.
describe('continueQuote', () => {
  it('writes "@handle wrote:" and a plain blockquote, a paragraph break kept as a bare >', () => {
    expect(continueQuote('alice', '<p>The bill was the <em>smaller</em> half.</p><p>The export was the rest.</p>')).toBe(
      '@alice wrote:\n\n> The bill was the smaller half.\n>\n> The export was the rest.',
    );
  });

  it('carries no [quote=#N], and leaves out the quotations the post holds', () => {
    const html = '<details class="quote"><summary>#3</summary><blockquote><p>older words</p></blockquote></details><p>mine</p>';
    const q = continueQuote('carol', html);
    expect(q).toBe('@carol wrote:\n\n> mine');
    expect(q).not.toContain('[quote=');
  });

  // Terva review 2060: a code block that shows "[quote=#99]" is quoted as
  // that text, escaped, so the new thread's post names no post of its own,
  // and "*", "_", a leading ">" or "#" stay characters. This is the source
  // internal/render's TestEscapedQuoteMarkerStaysText renders as text.
  it('escapes what the post showed, so a literal quote marker stays text', () => {
    const html = '<pre><code>[quote=#99]\na*b* _c_\n\n&gt; [quote=#98]\n\n# not a heading\n</code></pre>';
    expect(continueQuote('bob', html)).toBe(
      '@bob wrote:\n\n> \\[quote=#99\\]\n> a\\*b\\* \\_c\\_\n>\n> \\> \\[quote=#98\\]\n>\n> \\# not a heading',
    );
  });
});

describe('escapeText', () => {
  it.each([
    ['plain words, a full stop and an apostrophe', "It's 3.5 hours.", "It's 3.5 hours."],
    ['inline marks', 'a `b` *c* _d_ [e](f) <g> &amp; ~h~ |i|', 'a \\`b\\` \\*c\\* \\_d\\_ \\[e\\](f) \\<g> \\&amp; \\~h\\~ \\|i\\|'],
    ['a backslash', 'C:\\dir', 'C:\\\\dir'],
    ['a heading', '## Title', '\\## Title'],
    ['a list item', '- item', '\\- item'],
    ['a plus item', '+ item', '\\+ item'],
    ['an ordered item', '12. item', '12\\. item'],
    ['an ordered item with a paren', '3) item', '3\\) item'],
    ['a setext underline', '===', '\\==='],
    ['a quotation', '> said', '\\> said'],
    ['code indentation, kept short of a code block', '        x = 1', '   x = 1'],
    ['a tab', '\tx', '   x'],
  ])('%s', (_, line, want) => {
    expect(escapeText(line)).toBe(want);
  });
});
