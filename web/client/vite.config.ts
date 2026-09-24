/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
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
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': goServer,
      '/auth': goServer,
      '/events': goServer,
      '/blobs': goServer,
    },
  },
});
