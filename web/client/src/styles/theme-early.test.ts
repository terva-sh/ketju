import { afterEach, describe, expect, it } from 'vitest';
import builtShell from '../../../../internal/web/spa/dist/index.html?raw';
import { readThemePref, themeKey, themePrefs } from '../state/theme';
import early from '../theme-early.js?raw';

// A stored theme has to be on <html> before the browser first paints, which a
// module script cannot promise (review 423). theme-early.js does it from a
// blocking script at the top of <head>; this holds it to theme.ts, and runs it.

afterEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute('data-theme');
});

describe('the early theme script', () => {
  it('is a classic script in the built shell, ahead of the module and any stylesheet', () => {
    const head = builtShell.slice(0, builtShell.indexOf('</head>'));
    const tag = /<script src="\/assets\/theme-early-[\w-]+\.js"[^>]*><\/script>/.exec(head);
    expect(tag).not.toBeNull();
    expect(tag![0]).not.toMatch(/type=|async|defer/);
    expect(tag!.index).toBeLessThan(head.indexOf('type="module"'));
    const link = head.indexOf('<link');
    if (link >= 0) expect(tag!.index).toBeLessThan(link);
  });

  it('reads the key theme.ts writes', () => {
    expect(early).toContain(`'${themeKey}'`);
  });

  it.each(themePrefs.filter((p) => p !== 'system'))('applies a stored %s, as theme.ts would', (pref) => {
    localStorage.setItem(themeKey, pref);
    new Function(early)();
    expect(document.documentElement.getAttribute('data-theme')).toBe(pref);
    expect(readThemePref()).toBe(pref);
  });

  it.each(['system', 'sepia', ''])('leaves <html> alone for %j', (v) => {
    localStorage.setItem(themeKey, v);
    new Function(early)();
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false);
  });

  it('survives storage that throws', () => {
    const real = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')!;
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      get() {
        throw new DOMException('denied', 'SecurityError');
      },
    });
    try {
      expect(() => new Function(early)()).not.toThrow();
    } finally {
      Object.defineProperty(globalThis, 'localStorage', real);
    }
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false);
  });
});
