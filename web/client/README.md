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
