import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  // Writable cache when node_modules is not owned by the current user.
  cacheDir: process.env.VITE_CACHE_DIR || 'node_modules/.vite',
  server: {
    port: 5173,
    proxy: {
      '/api': {
        // Override with VITE_PANEL_PROXY=http://127.0.0.1:3081 for local demos.
        target: process.env.VITE_PANEL_PROXY || 'http://127.0.0.1:3080',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})
