/// <reference types="vitest/config" />
import { defineConfig, searchForWorkspaceRoot } from 'vite';
import preact from '@preact/preset-vite';

// The Go server listens here in development (docs/07 "Dev").
const goServer = 'http://localhost:8080';

export default defineConfig({
  plugins: [preact()],
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
      // The client's own files, and the mockups' generator, which the token
      // test reads to hold the colours to it (src/styles/tokens.test.ts).
      allow: [searchForWorkspaceRoot(process.cwd()), '../../docs/mockups'],
    },
    proxy: {
      '/api': goServer,
      '/auth': goServer,
      '/events': goServer,
      '/blobs': goServer,
    },
  },
});
