import { render } from 'preact';
import { App } from './app';
import { startDrafts } from './state/drafts';
import { applyTheme, themePref } from './state/theme';
import { guardViewer } from './state/viewerGuard';
import './styles/index.css';

// index.html's early script has already put a stored theme on <html>, before
// the first paint; this applies the same choice again through the one place
// that owns it, so the two agree from here on.
applyTheme(themePref.value);
// Another person's local draft copies go at startup (state/drafts.ts).
startDrafts();
// Another tab, or a restored page, of a viewer who has left reloads
// (state/viewerGuard.ts).
guardViewer();
render(<App />, document.getElementById('app')!);
