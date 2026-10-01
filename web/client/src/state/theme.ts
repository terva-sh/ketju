import { signal } from '@preact/signals';
import type { Me } from '../api/dto';

// The reader's theme choice (TKT-01M29CCC0F). "system" follows
// prefers-color-scheme; "light" and "dark" override it. A member's choice
// is stored on the account, /me's settings.theme, and wins on every load:
// this device keeps a copy only so the first paint is not the wrong theme
// (docs/07 "Theme", decided by the run 2026-10-01, pending maintainer
// review; TKT-01M29CXK4E). A signed-out reader's choice stays here.

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

// syncThemeFromAccount makes a member's account theme this device's choice,
// stored and applied, replacing whatever copy the device held. It changes
// nothing for a signed-out reader or a pre-user, who have no account, nor
// for a theme it does not know. main.tsx calls it after the bootstrap, and
// refreshViewer after each /me.
export function syncThemeFromAccount(me: Me, storage?: Pick<Storage, 'setItem' | 'removeItem'>) {
  const t = me.settings.theme as ThemePref;
  if (me.user && prefs.includes(t)) setThemePref(t, storage);
}
