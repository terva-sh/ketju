/// <reference types="vitest/config" />
import { readFileSync } from 'node:fs';
import { defineConfig, searchForWorkspaceRoot, type Plugin } from 'vite';
import preact from '@preact/preset-vite';

// The Go server listens here in development (docs/07 "Dev").
const goServer = 'http://localhost:8080';

// earlyTheme puts src/theme-early.js at the top of <head> as a classic
// script, which blocks parsing and so runs before the first paint: a stored
// theme is on <html> before any colour is drawn (review 423). The module
// script cannot promise that, and the shell allows no inline script
// (internal/web security_test.go), so it is a file of its own: emitted as a
// hashed asset in a build, served from src in development.
function earlyTheme(): Plugin {
  const source = 'src/theme-early.js';
  // Only a build emits the asset: the dev server serves src as it is. A
  // watched build is still a build (review 427), so this is keyed on the
  // command, not on watch mode.
  let building = false;
  return {
    name: 'ketju-early-theme',
    configResolved(config) {
      building = config.command === 'build';
    },
    buildStart() {
      if (!building) return;
      this.emitFile({ type: 'asset', name: 'theme-early.js', source: readFileSync(source, 'utf8') });
    },
    transformIndexHtml(_html, ctx) {
      let src = `/${source}`;
      if (ctx.bundle) {
        const out = Object.values(ctx.bundle).find(
          (o) => o.type === 'asset' && (o.names ?? []).includes('theme-early.js'),
        );
        if (!out) throw new Error(`${source} was not emitted`);
        src = `/${out.fileName}`;
      }
      return [{ tag: 'script', attrs: { src }, injectTo: 'head-prepend' }];
    },
  };
}

export default defineConfig({
  plugins: [preact(), earlyTheme()],
  html: {
    // Every script, style and link tag Vite emits carries this attribute. The
    // built index.html is a Go template (internal/web/spa), so the placeholder
    // becomes this response's CSP nonce when the server renders it.
    cspNonce: '{{.Nonce}}',
  },
  build: {
    outDir: '../../internal/web/spa/dist',
    emptyOutDir: true,
    // The preload polyfill is an inline script, which the CSP refuses.
    modulePreload: { polyfill: false },
    // Read by the bundle budget check (TKT-01M29CGBYH).
    manifest: true,
  },
  test: {
    environment: 'happy-dom',
    include: ['src/**/*.test.{ts,tsx}'],
    // Without this Vitest hands every stylesheet over empty, even as ?raw,
    // and the tests that read the CSS system's sources would pass on nothing.
    css: true,
  },
  server: {
    port: 5173,
    strictPort: true,
    fs: {
      // The client's own files; the mockups' generator, which the token test
      // reads to hold the colours to it (src/styles/tokens.test.ts); and the
      // built shell, which theme-early.test.ts reads for the early script.
      allow: [searchForWorkspaceRoot(process.cwd()), '../../docs/mockups', '../../internal/web/spa/dist'],
    },
    proxy: {
      '/api': goServer,
      '/auth': goServer,
      '/events': goServer,
      '/blobs': goServer,
    },
  },
});
