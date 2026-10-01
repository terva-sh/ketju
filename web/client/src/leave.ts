// nav.leave hands the address to the server with a full load, in place of
// the entry the router just pushed: Fallback's server-owned addresses
// (routes.tsx), and / once the settings screens have signed this browser
// out (screens/account/settings.tsx). An object so a test can intercept it.
// It is a module of its own so a screen can use it without importing the
// route table, which imports the screens.
//
// nav.loadedAt is the address the server served this document at. Fallback
// never hands that one back: the server has already answered it with the
// shell, and handing it back would reload the page forever if the rules
// in routes.tsx and the server's ever disagree.
export const nav = {
  loadedAt: location.pathname + location.search,
  leave(url: string) {
    location.replace(url);
  },
};
