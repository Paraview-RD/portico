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
  server: {
    port: 5410,
    strictPort: true,
    // The API runs as a separate process in development; in production the
    // same binary serves both, so the frontend always uses relative paths.
    proxy: {
      '/api': {
        target: 'http://localhost:8410',
        changeOrigin: true,
      },
    },
  },
})
