# ketju client

The Preact client (docs/07 "Preact client"). It is built into
`internal/web/spa/dist` and embedded in the binary with `//go:embed`, so a
`go build` serves it with no separate step.

## Building

```sh
go generate ./internal/web/spa   # npm ci && npm run build, from here
```

The built `dist/` is committed. CI runs the same command and fails if the
result differs from what is committed, so a change here ships with its build.

`index.html` is a Go template, not a static page. The server renders it for
each response (`internal/web.Shell`) with:

- `{{.Nonce}}`: the CSP nonce. Vite writes it onto every tag it emits
  (`html.cspNonce`), and the policy allows no script or style without it.
- `{{.Title}}` and `{{.Description}}`: the page's title and description, for
  link unfurls.
- `{{.Bootstrap}}`: the viewer (`/me`) as a JSON data block, so the first paint
  needs no round trip.

Nothing in the client may use an inline script, an inline style or an `on*`
attribute. The policy blocks them, and `internal/web`'s
`TestTheShellHasNoInlineScriptOrStyle` checks the built shell.

## Size budget

The thread view's JavaScript and CSS must stay under 70 KB gzipped (docs/07).
`npm run build` ends by measuring the build, and `npm run budget` measures it
again on its own. It prints each file's gzipped size and the total, and fails
when the total is over the budget. CI runs it as its own step.

What counts: the scripts and stylesheets the built shell loads, every chunk
they import statically, every lazy chunk except the admin screens, and the CSS
of each. Fonts do not count, and neither does the admin bundle
(`scripts/bundle.mjs`).

The budget is `threadViewGzipBytes` in `budget.json`: 70000 bytes, the
stricter reading of 70 KB. It was 60000 until the maintainer raised it on
2026-09-30 (docs/07). No flag or environment variable overrides it. To
raise it, edit that file, and say why in the pull request.

## Developing

Run the Go server on `:8080`, then:

```sh
npm install
npm run dev
```

and open http://localhost:5173. Vite serves the client with hot reload and
proxies `/api`, `/auth`, `/events` and `/blobs` to Go. Because Vite serves the
page and not Go, the template placeholders stay unrendered in development, and
the client falls back to an anonymous viewer when the bootstrap block does not
parse.

## Routes

`src/routes.tsx` is the route table. It has one entry for every path in
docs/07's URL scheme, which the HTML client will serve too, so adding a
client-only route is a design change, not a detail. `routes.test.tsx` diffs
the table against the document in both directions.

A thread's page and lens are query parameters, and `useThreadView` in
`src/view.ts` is the only place that reads or writes them. A shared link
therefore reproduces the view.

The admin screens under `src/screens/admin` are reached only through a lazy
import, so they build to their own chunk. `internal/web/spa`'s
`TestTheAdminBundleIsSeparate` reads the manifest and fails if the reader's
entry chunk ever imports them statically.

## Testing

```sh
npm test
```

runs Vitest under happy-dom. CI runs it after rebuilding the client.

```sh
npx playwright test
```

runs the smoke suite in `e2e/` (TKT-01M29CGBY1). It signs in, reads a
thread, replies, and checks the unread divider in a real browser, at 390 px and
1440 px. The server is `ketju e2e`, which is `ketju serve` built with
`-tags e2e`. That build seeds a fresh data directory, starts the fake OpenID
provider on loopback, and signs in through the provider's own page. The config
builds and starts it; it needs Go and port 8787 (`KETJU_E2E_PORT`). The
server's log is `serve.log` beside the run's data, in a directory under
`$TMPDIR`. When a test fails, `scripts/evidence-reporter.mjs` prints its
`error-context.md` (the error, the locator, and the page's ARIA tree) and
the `serve.log` lines from while it ran into the run's output. In CI that
output is the only record that survives the job.

A `setup` project (`e2e/session.setup.ts`) signs the seeded readers in
once and saves their sessions (`stateFile` in `e2e/session.ts`), and both
widths depend on it. A spec that needs a signed-in reader, but is not testing
sign-in, starts from a saved session. That keeps the suite under the
`/auth/*` rate limit (docs/08), which allows a burst of ten requests.

`e2e/forum-index.spec.ts` also writes the screenshots committed in
`e2e/screenshots/` when run with `KETJU_SCREENSHOTS=1`. They are compared by
eye against `docs/mockups/` (TKT-01M29CEE5Y). Without the variable, as in CI,
nothing is written.

Set `PLAYWRIGHT_CHROMIUM_EXECUTABLE` to run a Chromium already on the machine;
CI does, with Alpine's. Without it, Playwright's own is used, from
`npx playwright install chromium`. The suite serves the embedded client, so
rebuild `internal/web/spa/dist` first (`go generate ./internal/web/spa`) when
the client has changed.

## Versions

Every dependency is pinned to an exact version, and `package-lock.json` is
committed, so a build is reproducible. CI relies on that when it diffs the
build.

**Preact is pinned at 10.x until ketju v1.0.** docs/10 (question 20) decides
the pin but does not say why. The reason recorded here, in TKT-01M29CCC02, is
this: `preact-iso`, `@preact/signals` and `@preact/preset-vite` are all built
against Preact 10. Moving to a new major while the client is still being
written would make every client ticket a migration as well. So the major
version is a deliberate change after v1.0, not something a routine update
brings in. A bump within 10.x is an ordinary dependency update.
