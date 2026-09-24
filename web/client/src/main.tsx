import { render } from 'preact';
import { App } from './app';
import { applyTheme, themePref } from './state/theme';
import './styles/index.css';

// index.html's early script has already put a stored theme on <html>, before
// the first paint; this applies the same choice again through the one place
// that owns it, so the two agree from here on.
applyTheme(themePref.value);
render(<App />, document.getElementById('app')!);
