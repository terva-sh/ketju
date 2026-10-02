---
schema: 3
id: TKT-01M29CGBYYTJCDV43WDP2FE0Z5
title: Add the web app manifest and icons
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - area:mobile
  - effort:S
assignees: []
milestone: phase-3
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
blocks_on: none
references:
  - ref: design:mobile-installable
    path: docs/06-thread-views.md
  - ref: pr:346
    path: null
  - ref: pr:347
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-10-02T14:32:44Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
extensions: {}
---

## Description

A manifest and icon set so the forum can be added to a home screen and open without browser chrome.

No service worker in v1, so no offline caching and no push. The installable web app is the whole mobile story, and docs/10 puts native apps out of scope unless push or share-sheet integration ever forces the question.

## Acceptance criteria

- [x] GET /manifest.webmanifest names the site by the site_name setting, with start_url /, display standalone, and 192, 512 and maskable 512 px icons that exist under /assets (TestManifest, TestManifestCarriesTheSiteName)
- [x] e2e: Chromium's Page.getAppManifest reports no errors, and installability reports nothing beyond a missing service worker
- [x] The shell carries an apple-touch-icon, the apple and mobile web-app meta tags, and theme-color for light and dark; a theme the reader chose sets both theme-color tags
- [x] No service worker is registered (e2e) and the dist contains no serviceWorker.register
- [x] No style depends on display-mode, so the existing safe-area check in e2e/thread.spec.ts covers the installed app; the status bar style is default
- [ ] Tested at 390 and 1440 px with one axe scan per test; the dist is regenerated and npm run budget stays within 70000 B
- [ ] A person installed it on a real iOS and a real Android device and it opened standalone (not an agent's to tick)

## Implementation plan

### Settled in grooming

- There is no service worker, offline cache or push in v1 (docs/06 "Installable", docs/07 "Preact client", docs/10). The manifest and the icons are the whole of it.
- The icon is a placeholder the run draws, because the design names none: two linked rings in the light accent (`#2F5FB3`) and white, with PNGs at 180, 192 and 512 px and a maskable 512 px. Decided by the run, docs/06 "Installable" (2026-10-01, pending maintainer review).
- The manifest is served by the server at `/manifest.webmanifest`, with `name` and `short_name` from the `site_name` setting, read per request as `/me` reads it (TKT-01M29CXK2H). Decided by the run, docs/06 "Installable" (2026-10-01, pending maintainer review). `start_url` and `scope` are `/`, and `display` is `standalone`. `background_color` and `theme_color` are the light theme's `--bg` (`#F6F7FA`); the manifest takes one value, and the meta tags below carry both themes.
- `theme-color` is set per theme in the shell: `<meta name="theme-color" content="#F6F7FA" media="(prefers-color-scheme: light)">` and `#12161E` for dark, the `--bg` tokens in `styles/tokens.css`. A theme the reader chose overrides the media query, so `applyTheme` (`state/theme.ts`) sets both tags' `content` to that theme's colour, and `system` restores them.
- iOS: an `apple-touch-icon` link (180 px), `apple-mobile-web-app-capable` and `mobile-web-app-capable` set to `yes`, `apple-mobile-web-app-status-bar-style` set to `default` so nothing is drawn under the status bar, and `apple-mobile-web-app-title` set to the site name. `index.html` is a Go template, so `web.Shell` supplies the site name as it supplies the title.
- The safe-area insets are already honoured by the tab bar, the reply bar and the phone top bar (`styles/base.css`), and `e2e/thread.spec.ts` "the bars pad by the safe-area insets" proves it. No style depends on `display-mode`, so the installed app gets the same padding. This ticket adds no inset rule of its own. A new sheet docked to the bottom pads itself in its own ticket (TKT-01M29CGBZD).
- Bundle: nothing here is JavaScript or CSS the budget counts, apart from about 120 B in `state/theme.ts` for the meta update. The icons and the manifest are not `.js` or `.css`, and the shell is not counted. All of it stays in the reader bundle.

### Approach

1. **Icons.**
   - Draw `web/client/src/assets/icon/icon.svg`: two linked rings, white on a rounded `#2F5FB3` square, plus a full-bleed variant for maskable use with the mark inside the central 80% safe zone.
   - Render the PNGs (`icon-180.png`, `icon-192.png`, `icon-512.png`, `icon-maskable-512.png`) once with a small Go program using only the standard library's `image` and `image/png`. Ring geometry is circles, so no font or rasterizer dependency is needed. Put it at `web/client/scripts/icons/main.go` behind `//go:build ignore`, so `go build ./...` does not compile it and CI does not regenerate the icons.
   - Commit the PNGs. CI's dist rebuild then copies identical bytes.
2. **Emit them through Vite** so they get content-hashed `/assets/` names. `index.html` links `rel="icon"` (the SVG) and `rel="apple-touch-icon"` (180 px), and Vite rewrites those. The manifest needs the 192, 512 and maskable names too. Add a small Vite plugin in `vite.config.ts`, beside `earlyTheme`, that emits the three and writes their hashed names to `dist/icons.json`. The Go side reads that file.
3. **Server.**
   - `internal/web/spa` gains `Manifest(siteName string) []byte`, which builds the JSON from `icons.json` and the settled fields, and `ManifestHandler(settings)`, answering `GET /manifest.webmanifest` with `Content-Type: application/manifest+json` and `Cache-Control: no-cache`. A rename of the site then shows on the next check.
   - Register it in `internal/web/server.go` as an exact pattern, as `/events` and `/healthz` are, before the shell fallback.
   - Update `serverOwned` and `clientScope` in `web/client/src/routes.tsx` only if their tests require every server pattern. Nothing in the client links to the manifest.
   - The CSP's `default-src 'self'` already covers `manifest-src`, so nothing changes there.
4. **Shell.** Add the meta and link tags to `web/client/index.html`. Add `SiteName` to `web.Page` (`internal/web/shell.go`) for `apple-mobile-web-app-title`. Fill it where `Title` is filled.
5. **Theme colour.** `applyTheme` sets the two `theme-color` tags as settled above, with a test.
6. Run `go generate ./internal/web/spa` and commit the dist.

### Tests

- `TestManifest` (`internal/web/spa`): a golden of the JSON for the site name "Club", with the icon names read from the embedded `icons.json`. Every icon named exists under `dist/assets`.
- `TestManifestCarriesTheSiteName` (`internal/web`): set `site_name`, request `/manifest.webmanifest`, and check the name, the content type and `no-cache`. A request for `/manifest.webmanifest/x` still reaches the shell fallback.
- `TestShellHasInstallTags`: the rendered shell has the manifest link, both `theme-color` tags, the apple tags with the site name escaped, and no inline script other than the bootstrap JSON, so the CSP is unchanged.
- `theme.test.ts`: `applyTheme('dark')` sets both theme-color tags to `#12161E`, and `system` restores the media pair.
- `e2e/install.spec.ts` (both projects, one axe scan):
  - `the manifest Chromium reads has no errors`: CDP `Page.getAppManifest` returns no errors, along with the name, standalone display, and icons at 192 and 512 px plus a maskable one. `Page.getInstallabilityErrors` lists nothing beyond a missing service worker, should this Chromium still ask for one.
  - `no service worker is registered`: `navigator.serviceWorker.getRegistrations()` is empty after a thread page loads.
- `bundle.test.mjs` or a Vitest check: the built dist contains no `serviceWorker.register`.

### Out of scope

- A service worker, offline caching and push notifications: excluded from v1 (docs/10).
- The real-device check that iOS and Android install and open it standalone. A person makes that check; it is the last criterion, and no agent ticks it.
- A designed icon. The placeholder stands until the maintainer supplies one.

## Notes

**agent:claude-code/phase3-groom-pages** at 2026-10-01T07:51:18Z

run decision: pending maintainer review. The design names no icon, so the run draws a placeholder: two linked rings (ketju is Finnish for chain) in the light accent and white, SVG plus PNGs at 180, 192, 512 and a maskable 512. Recorded in docs/06 Mobile, Installable (2026-10-01).

**agent:claude-code/phase3-groom-pages** at 2026-10-01T07:51:18Z

run decision: pending maintainer review. The server writes the manifest at /manifest.webmanifest with name and short_name from the site_name setting, so a club's home-screen icon carries the club's name; theme-color is given for both themes and follows a chosen theme. Recorded in docs/06 Mobile, Installable (2026-10-01), with the alternatives.

**agent:claude-code/phase3-cgbyy** at 2026-10-01T19:36:32Z

Implementation notes (phase A, before the dist is committed). Where the build differs from the plan, and why:

- **One geometry for every icon.** `web/client/scripts/icons/main.go` (`//go:build ignore`) writes `icon.svg` as well as the four PNGs, from the same constants, so the vector and the rasters cannot drift. There is no separate full-bleed SVG: nothing would reference it, and the maskable PNG is the full-bleed variant. The mark sits within 0.3375 of the side from the middle, inside the 0.4 safe zone, so the rounded and the full-bleed icons share it. The 180 px apple-touch-icon is full-bleed too, because iOS draws its own corners and fills transparency with black.
- **The manifest carries `id: "/"`** beyond the settled fields, so the installed app stays the same app if `start_url` ever moves. It is served with `no-cache` and an ETag, so a check after a rename gets the new name and an unchanged manifest costs a 304.
- **The pre-user gate lets the manifest through.** `GET /manifest.webmanifest` is added to `web.PreUserRoutes`, beside `/assets/`, because the shell of `/apply` and `/apply/status` links it. It holds the site name and icon names, the same for every viewer. Without it, a browser that sends cookies with the manifest fetch would get the gate's 303 to the status page, an HTML page where the manifest should be. Alternative rejected: leave it off and rely on Chromium fetching the manifest without credentials. That holds for Chromium only, and it leaves an applicant's install broken in any browser that sends cookies. `TestPreUserRoutesAreExactlyTheAllowlist` and its walk carry the new entry.
- **No-service-worker check in Go, not in `bundle.test.mjs`.** `TestTheDistRegistersNoServiceWorker` (`internal/web/spa`) reads every script in the embedded dist and refuses any mention of `serviceWorker`. It runs on the build the binary carries.
- **`serverOwned` and `clientScope` are unchanged.** No routes test requires every server pattern. Nothing in the client links to the manifest, and `/healthz`, the other exact server path, is not listed either.
- **`TestTheBuildIsEmbedded`** now accepts hashed `.svg` and `.png` as well as `.js` and `.css`, because the shell links the two icons.
- **The theme colour.** `applyTheme` sets both `theme-color` tags by attribute: happy-dom has no `HTMLMetaElement.media`. The early theme script does not touch them. `main.tsx` calls `applyTheme` at boot, and the browser chrome colour does not need to beat first paint.
- **Settings and caching** (the rename check): `settings.SiteName.Get` reads the database on every request, with no cache in between. The manifest is `no-cache` and the shell `no-store`. `TestManifestCarriesTheSiteName` (`internal/web`) sets `site_name` and reads it back from the manifest and from the shell's `apple-mobile-web-app-title` on the next request. It also checks that the route carries the CSP and the security headers, and that `/manifest.webmanifest/x` reaches the shell.
- **The CSP is unchanged.** `default-src 'self'` covers `manifest-src`. `TestShellHasInstallTags` runs the inline-script check on the new shell.
- **docs/02's router diagram** names the new route.

Tests: `TestManifest`, `TestManifestEscapesTheSiteName`, `TestManifestHandler`, `TestTheDistRegistersNoServiceWorker` (spa); `TestManifestCarriesTheSiteName` and `TestShellHasInstallTags` (web); the manifest line in `TestStartupOrderAndShutdown` (cmd/ketju); theme.test.ts (both tags follow a chosen theme, system restores them, and the colours match `--bg`); e2e/install.spec.ts at 390 and 1440 px. The local Chromium (playwright chromium-1243) reported no installability errors at all.

Reader bundle: 66220 B before, 66299 B after (+79 B, the theme-color update), of 120000 B. The ticket's AC6 still says 70000 B, the budget when it was groomed; the maintainer raised it to 120000 B on 2026-10-01 (docs/14 "Amended during the run"), and the reader is within both.

**agent:claude-code/phase3-cgbyy** at 2026-10-02T05:17:41Z

Phase B, on main de37cccb. AC6 is left unticked. Everything in it holds except the number: both widths pass with one axe scan per test (full Playwright suite, 258 tests), the dist is regenerated, and npm run budget passes. The reader is 79088 B (79006 B on main before this change, +82 B), within the 120000 B budget the maintainer set on 2026-10-01 (docs/14) but over the 70000 B this criterion was groomed with. Earlier lanes took main past 70000 B; this ticket adds 82 B. The criterion's number is stale, and ticking it would claim something false. AC7 (real-device install) is for a person to tick. The rebase over #342 resolved theme.test.ts: syncThemeFromAccount calls setThemePref and then applyTheme, so the account's theme also sets both theme-color tags. A new test, 'colours the browser chrome by the account's theme', proves it.

**agent:claude-code/phase3-cgbyy** at 2026-10-02T05:41:53Z

Carried to run/cgbyy-manifest-2 after main moved to b7d9e58e. PR #346 is replaced. #346's CI (run 2326) failed only in e2e/install.spec.ts 'the manifest Chromium reads has no errors', at both widths. CI's Chromium 152.0.7977.82 (Alpine) reported the installability error in-incognito, because every Playwright context is off the record. The local Playwright Chromium (1243) did not, so this supersedes my earlier note that installability reports nothing. I did not add in-incognito to the accepted errors: that would hide anything else installability reports. The test now launches Chromium with a persistent profile in a temporary directory, which is not off the record. It reads the manifest anonymously, so the criterion is checked for real and the only accepted error is still a missing service worker.

**agent:claude-code/phase3-run-orchestrator** at 2026-10-02T14:32:44Z

Maintainer decision, 2026-10-02: the real-device install check (criterion 7) is deferred. It stays unticked until a person installs the app on a real iOS and a real Android device.

## Summary

The server serves /manifest.webmanifest (spa.ManifestHandler, wired in serve), named by site_name and read per request. It is no-cache with an ETag. start_url and scope are /, display is standalone, and it lists 192, 512 and maskable 512 px icons. The shell links the manifest, an SVG favicon and a 180 px apple-touch-icon. It carries theme-color for light and dark, the apple and mobile web-app tags with the site name as the iOS title, and the status bar style default. applyTheme sets both theme-color tags for a chosen theme, including the account's theme through syncThemeFromAccount. There is no service worker. The placeholder icon (two linked rings) comes from web/client/scripts/icons/main.go and is committed; a Vite plugin writes dist/icons.json for the server. GET /manifest.webmanifest is on web.PreUserRoutes because the application pages' shell links it, and it carries only site_name, the icons and a fixed colour. Reader bundle 79006 to 79088 B. AC6 is unticked only because its 70000 B figure is stale (the budget is now 120000 B). AC7 is a real-device check for a person.
