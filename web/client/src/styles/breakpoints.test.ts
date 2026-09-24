import { describe, expect, it } from 'vitest';
import indexHtml from '../../index.html?raw';
import { phoneQuery } from '../state/layout';
import base from './base.css?raw';

// docs/06's breakpoints are written literally wherever they are used, since a
// custom property cannot appear in a media query; this is what keeps every
// use to the same three forms.
const sheets = import.meta.glob('../**/*.css', { query: '?raw', import: 'default', eager: true }) as Record<string, string>;
const sources = import.meta.glob(['../**/*.{ts,tsx}', '!../**/*.test.{ts,tsx}'], {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>;

const allowed = new Set(['(max-width: 719.98px)', '(min-width: 720px) and (max-width: 1100px)', '(min-width: 1100.02px)']);

describe('the breakpoints', () => {
  it('reads the stylesheets, and reads them whole', () => {
    expect(Object.keys(sheets).length).toBeGreaterThan(3);
    for (const [p, text] of Object.entries(sheets)) expect(text.length, p).toBeGreaterThan(100);
    expect(Object.values(sheets).join('').match(/@media/g)?.length).toBeGreaterThan(2);
  });

  it.each(Object.keys(sheets))('%s uses only docs/06’s widths', (p) => {
    const css = sheets[p].replace(/\/\*[\s\S]*?\*\//g, '');
    for (const m of css.matchAll(/@media\s+([^{]+)\{/g)) {
      const q = m[1].trim();
      if (!/width/.test(q)) continue;
      expect(allowed.has(q), `${p}: @media ${q}`).toBe(true);
    }
  });

  it('gives the navigation shell the same phone width', () => {
    expect(allowed.has(phoneQuery)).toBe(true);
    expect(phoneQuery).toBe('(max-width: 719.98px)');
  });

  // The CSP allows no inline style attribute, so a component that set one
  // would render unstyled in production and styled in tests.
  it.each(Object.keys(sources))('%s sets no style attribute', (p) => {
    expect(sources[p]).not.toMatch(/\bstyle=\{/);
  });
});

// AC3's rules: the bottom bars pad clear of the home indicator, the top bar of
// the notch, and the page is allowed under both.
describe('the safe areas', () => {
  it('reads base.css whole', () => {
    expect(base.length).toBeGreaterThan(500);
  });

  it('lets the page run under the device’s insets', () => {
    expect(indexHtml).toMatch(/<meta name="viewport" content="[^"]*viewport-fit=cover/);
  });

  it.each([
    ['.tabbar', 'bottom'],
    ['.replybar', 'bottom'],
    ['.topbar', 'top'],
  ])('pads %s by the %s inset', (sel, side) => {
    const rule = new RegExp(`${sel.replace('.', '\\.')}[^{]*\\{([^}]*)\\}`).exec(base)?.[1] ?? '';
    expect(rule).toContain(`padding-${side}: env(safe-area-inset-${side}`);
    expect(rule).toContain('padding-left: env(safe-area-inset-left');
    expect(rule).toContain('padding-right: env(safe-area-inset-right');
  });
});
