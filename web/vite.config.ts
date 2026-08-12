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
  },
  server: {
    port: 5410,
    strictPort: true,
    // The API runs as a separate process in development; in production the
    // same binary serves both, so the frontend always uses relative paths.
    //
    // 8140 is where hack/dev.sh pins that process. It is deliberately not
    // 8410 — the port a deployment defaults to — because a developer's
    // machine tends to have one of those already running, and proxying into
    // it means editing a component and testing it against somebody else's
    // database.
    proxy: {
      '/api': {
        target: `http://localhost:${process.env.PORTICO_DEV_PORT ?? 8140}`,
        changeOrigin: true,
      },
    },
  },
})
