import { render } from 'preact';
import { App } from './app';
import { applyTheme, themePref } from './state/theme';
import './styles/index.css';

// The reader's theme goes on before the first render, so a chosen theme does
// not paint the system's first.
applyTheme(themePref.value);
render(<App />, document.getElementById('app')!);
