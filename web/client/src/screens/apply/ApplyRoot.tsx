// The application screens, one lazy chunk (TKT-01M29CP2NG): routes.tsx
// loads this module the first time /apply or /apply/status renders, so a
// member's first paint never downloads them. They still count toward the
// reader's bundle budget, as every chunk but the admin screens does
// (scripts/bundle.mjs).
import './apply.css';

export { ApplyScreen } from './ApplyScreen';
export { StatusScreen } from './StatusScreen';
