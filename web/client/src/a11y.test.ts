import { describe, expect, it } from 'vitest';
import base from './styles/base.css?raw';

// Two of the accessibility pass's rules that are about the source as a whole
// (TKT-01M29CGBXK). The rest are checked in the browser: e2e/a11y.spec.ts,
// e2e/targets.spec.ts and e2e/focus.spec.ts.

const sources = import.meta.glob(['./**/*.{ts,tsx}', '!./**/*.test.{ts,tsx}'], {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>;

// AC4, docs/06 "Targets and gestures": no action is swipe-only. Phase 1 has
// no gesture at all, so any handler that could read one is a failure unless
// it is listed here with the button that does the same thing.
const gestures = /\b(touchstart|touchmove|touchend|pointermove|onTouchStart|onTouchMove|onTouchEnd|onPointerMove|swipe)\b/i;
const allowed: Record<string, string> = {};

describe('gestures', () => {
  it('reads the sources', () => {
    expect(Object.keys(sources).length).toBeGreaterThan(30);
    expect(Object.keys(sources).some((p) => p.endsWith('composer/ComposerSheet.tsx'))).toBe(true);
  });

  it.each(Object.keys(sources))('%s has no swipe without a button', (p) => {
    const hits = sources[p]
      .split('\n')
      .map((line, i) => [i + 1, line] as const)
      .filter(([, line]) => gestures.test(line) && !/^\s*(\/\/|\*)/.test(line));
    if (allowed[p]) return;
    expect(hits.map(([n, line]) => `${p}:${n}: ${line.trim()}`)).toEqual([]);
  });

  it('would catch one', () => {
    expect(gestures.test("el.addEventListener('touchmove', drag)")).toBe(true);
    expect(gestures.test('<div onPointerMove={drag}>')).toBe(true);
    expect(gestures.test("addEventListener('pointerdown', away)")).toBe(false);
  });
});

// AC3, docs/07 "Accessibility": motion stops for a reader who asks.
describe('reduced motion', () => {
  const css = base.replace(/\/\*[\s\S]*?\*\//g, '');
  const block = /@media \(prefers-reduced-motion: reduce\) \{([\s\S]*?)\n\}/.exec(css)?.[1] ?? '';

  it('has a rule for everything, pseudo-elements included', () => {
    expect(block).toMatch(/\*,\s*\*::before,\s*\*::after \{/);
  });

  it.each(['animation-duration: 0.01ms !important', 'animation-iteration-count: 1 !important', 'transition-duration: 0.01ms !important', 'scroll-behavior: auto !important'])(
    'sets %s',
    (decl) => {
      expect(block).toContain(decl);
    },
  );

  it('asks for no smooth scrolling anywhere else', () => {
    const smooth = Object.entries(sources).filter(([, text]) => /behavior:\s*['"]smooth['"]/.test(text));
    expect(smooth.map(([p]) => p)).toEqual([]);
  });
});
