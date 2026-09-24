// The reader's stored theme, put on <html> before the first paint. This is a
// classic script at the top of <head> (vite.config.ts earlyTheme), so it runs
// before anything is drawn; the module that owns the theme (state/theme.ts)
// runs too late to prevent a flash of the default. It repeats that module's
// read, the same key and values, which theme-early.test.ts holds equal.
try {
  var t = localStorage.getItem('ketju.theme');
  if (t === 'light' || t === 'dark') document.documentElement.setAttribute('data-theme', t);
} catch (e) {
  // Storage can be unavailable; the system's theme applies.
}
