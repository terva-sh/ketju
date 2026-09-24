import { afterEach, describe, expect, it } from 'vitest';
import { applyTheme, readThemePref, setThemePref, themePref } from './theme';

function memory(initial: Record<string, string> = {}) {
  const m = new Map(Object.entries(initial));
  return {
    m,
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
  };
}

afterEach(() => {
  document.documentElement.removeAttribute('data-theme');
  themePref.value = 'system';
});

describe('the theme choice', () => {
  it('follows the system until the reader chooses', () => {
    expect(readThemePref(memory())).toBe('system');
  });

  it.each(['light', 'dark', 'system'] as const)('reads a stored %s', (v) => {
    expect(readThemePref(memory({ 'ketju.theme': v }))).toBe(v);
  });

  it('treats a value it does not know as no choice', () => {
    expect(readThemePref(memory({ 'ketju.theme': 'sepia' }))).toBe('system');
  });

  it('treats storage that throws as no choice', () => {
    expect(
      readThemePref({
        getItem: () => {
          throw new DOMException('denied', 'SecurityError');
        },
      }),
    ).toBe('system');
  });

  it('puts an override on <html>, and takes it off for system', () => {
    applyTheme('dark');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    applyTheme('light');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
    applyTheme('system');
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false);
  });

  it('stores a choice, applies it, and forgets it for system', () => {
    const s = memory();
    setThemePref('dark', s);
    expect(s.m.get('ketju.theme')).toBe('dark');
    expect(themePref.value).toBe('dark');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    setThemePref('system', s);
    expect(s.m.has('ketju.theme')).toBe(false);
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false);
  });

  it('still applies a choice it cannot store', () => {
    setThemePref('light', {
      setItem: () => {
        throw new DOMException('full', 'QuotaExceededError');
      },
      removeItem: () => {},
    });
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
  });
});
