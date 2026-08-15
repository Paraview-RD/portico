/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'

// The build output goes into the Go package that embeds it, because go:embed
// can only reach files inside its own package directory.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: '../internal/web/dist',
    // Not emptied: internal/web/dist/.gitkeep is committed so that go:embed
    // has a file to match in a fresh clone, and emptying deletes it. The
    // prebuild script clears stale output instead.
    emptyOutDir: false,
  },
  // Component tests run against a DOM rather than a browser. They are not a
  // substitute for the browser pass — the Content-Security-Policy bug that
  // broke every SAML sign-in passed eleven Go tests and would pass these too
  // — but they hold the things a browser pass checks once and then forgets:
  // that a control is wired to the right identifier, that a toggle exposes
  // its state, that an error renders in the reader's language.
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    // Only files under src/test and *.test.tsx; nothing in the build.
    include: ['src/**/*.test.{ts,tsx}'],
    // Process CSS rather than stubbing it to an empty string, which is
    // vitest's default. One test reads theme.css to check that every design
    // token a screen names is actually defined — a `var(--font-size-2xl)`
    // that does not exist falls back to the inherited value rather than
    // failing, so nothing else can catch it. With CSS stubbed that test read
    // an empty file and would have passed no matter what.
    css: true,
  },
  server: {
    port: 5410,
    strictPort: true,
    // The API runs as a separate process in development; in production the
    // same binary serves both, so the frontend always uses relative paths.
    //
    // 8410 is where hack/dev.sh pins that process, and it is the port a
    // deployment defaults to — which is the point: one address, the one
    // already in muscle memory. This proxied 8140 while an instance of an
    // older release was parked on 8410, and the two numbers were a digit-swap
    // apart, so which window showed current code was a coin toss.
    proxy: {
      '/api': {
        target: `http://localhost:${process.env.PORTICO_DEV_PORT ?? 8410}`,
        changeOrigin: true,
      },
    },
  },
})
