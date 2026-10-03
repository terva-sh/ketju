import { renameComposerOwner } from './composer';
import { renameDraftOwner } from './drafts';
import { renameNewThreadOwner } from './newThread';
import { viewer } from './viewer';

// renameViewer gives the viewer's own account the handle to, in place, when
// a rename of it succeeds in this tab: the member's own change
// (screens/account/AccountScreen.tsx) or an admin renaming themselves
// (screens/admin/UsersScreen.tsx). The client knows its viewer by handle,
// and a change of handle otherwise reads as a change of person, which drops
// every composer and draft (state/drafts.ts startDrafts, state/composer.ts
// checkOwner). A rename is the same person, so their drafts move to the new
// handle first, and only then does the viewer take it (TKT-01M3V6XTYG,
// Terva review 2009).
export function renameViewer(to: string) {
  const me = viewer.peek();
  if (!me.user || me.user.handle === to) return;
  const from = me.user.handle;
  renameDraftOwner(from, to);
  renameComposerOwner(from, to);
  renameNewThreadOwner(from, to);
  viewer.value = { ...me, user: { ...me.user, handle: to } };
}
