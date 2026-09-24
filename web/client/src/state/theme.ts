import { signal } from '@preact/signals';

// The reader's theme choice (TKT-01M29CCC0F). "system" follows
// prefers-color-scheme; "light" and "dark" override it. It lives on this
// device only: syncing it to the account's settings waits for the settings
// endpoints (phase 4).

export type ThemePref = 'system' | 'light' | 'dark';

// themeKey and themePrefs are read before first paint by a script in
// index.html as well; theme-early.test.ts holds the two together.
export const themeKey = 'ketju.theme';
export const themePrefs: readonly ThemePref[] = ['system', 'light', 'dark'];
const key = themeKey;
const prefs = themePrefs;

// readThemePref is the stored choice, or system when there is none, it is
// not one of the three, or storage is unavailable (a private window can
// throw on access).
export function readThemePref(storage: Pick<Storage, 'getItem'> | undefined = safeStorage()): ThemePref {
  try {
    const v = storage?.getItem(key);
    return prefs.includes(v as ThemePref) ? (v as ThemePref) : 'system';
  } catch {
    return 'system';
  }
}

export const themePref = signal<ThemePref>(readThemePref());

// applyTheme puts the choice on <html>, where tokens.css reads it: no
// data-theme for system, so the media query decides.
export function applyTheme(pref: ThemePref, root: HTMLElement = document.documentElement) {
  if (pref === 'system') root.removeAttribute('data-theme');
  else root.setAttribute('data-theme', pref);
}

// setThemePref records a new choice, stores it, and applies it.
export function setThemePref(pref: ThemePref, storage: Pick<Storage, 'setItem' | 'removeItem'> | undefined = safeStorage()) {
  themePref.value = pref;
  try {
    if (pref === 'system') storage?.removeItem(key);
    else storage?.setItem(key, pref);
  } catch {
    // Not stored; it still holds for this page.
  }
  applyTheme(pref);
}

function safeStorage(): Storage | undefined {
  try {
    return globalThis.localStorage;
  } catch {
    return undefined;
  }
}
