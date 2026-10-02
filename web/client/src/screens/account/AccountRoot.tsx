// The account screens (TKT-01M3V3CFMA): a member's profile and their posts.
// routes.tsx imports them into the reader's chunk; a lazy chunk of their own
// measured 2182 B more in all, since the budget counts every chunk but admin
// and the split added a shared one (scripts/bundle.mjs, docs/07 "Preact
// client"). The settings screens (TKT-01M29CXK4E) join this module, and use
// its page layout (account.css). Measured both ways for them too: inline,
// the reader is 74945 B; as a lazy chunk of their own, 77272 B, the split
// again adding a shared chunk.
import './account.css';
import './settings.css';

export { AccountScreen } from './AccountScreen';
export { IdentitiesScreen } from './IdentitiesScreen';
export { ProfileScreen } from './ProfileScreen';
export { ProfilePostsScreen } from './ProfilePostsScreen';
export { SessionsScreen } from './SessionsScreen';
export { SettingsScreen } from './SettingsScreen';
