import { test as setup } from '@playwright/test';
import { signIn, stateFile } from './session';

// The setup project: sign each seeded reader in once, through the real flow,
// and keep the session for the specs that need one (stateFile).
for (const who of ['alice', 'carol']) {
  setup(`sign ${who} in`, async ({ page }) => {
    await signIn(page, who);
    await page.context().storageState({ path: stateFile(who) });
  });
}
