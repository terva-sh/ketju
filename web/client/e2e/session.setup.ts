import { test as setup } from '@playwright/test';
import { signIn, stateFile } from './session';

// The setup project: sign each seeded reader, the seeded moderator and the
// seeded admin in once, through the real flow, and keep the session for the
// specs that need one (stateFile). Each sign-in costs two of the ten /auth/*
// requests allowed in a burst (docs/08), so four leave two over.
for (const who of ['alice', 'carol', 'mona', 'ada']) {
  setup(`sign ${who} in`, async ({ page }) => {
    await signIn(page, who);
    await page.context().storageState({ path: stateFile(who) });
  });
}
