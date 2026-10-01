---
schema: 3
id: TKT-01M29CGBYYTJCDV43WDP2FE0Z5
title: Add the web app manifest and icons
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-10-01T07:51:18Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-groom-pages
  name: ""
extensions: {}
---

## Description

A manifest and icon set so the forum can be added to a home screen and open without browser chrome.

No service worker in v1, so no offline caching and no push. The installable web app is the whole mobile story, and docs/10 puts native apps out of scope unless push or share-sheet integration ever forces the question.

## Acceptance criteria

- [ ] GET /manifest.webmanifest names the site by the site_name setting, with start_url /, display standalone, and 192, 512 and maskable 512 px icons that exist under /assets (TestManifest, TestManifestCarriesTheSiteName)
- [ ] e2e: Chromium's Page.getAppManifest reports no errors, and installability reports nothing beyond a missing service worker
- [ ] The shell carries an apple-touch-icon, the apple and mobile web-app meta tags, and theme-color for light and dark; a theme the reader chose sets both theme-color tags
- [ ] No service worker is registered (e2e) and the dist contains no serviceWorker.register
- [ ] No style depends on display-mode, so the existing safe-area check in e2e/thread.spec.ts covers the installed app; the status bar style is default
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
