import { defineConfig } from 'vite'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'

// The build output goes into the Go package that embeds it, because go:embed
// can only reach files inside its own package directory.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: '../internal/web/dist',
    emptyOutDir: true,
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
